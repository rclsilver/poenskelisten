package main

import (
	"aunefyren/poenskelisten/config"
	"aunefyren/poenskelisten/controllers"
	"aunefyren/poenskelisten/database"
	"aunefyren/poenskelisten/logger"
	"aunefyren/poenskelisten/mcpserver"
	"aunefyren/poenskelisten/middlewares"
	"aunefyren/poenskelisten/models"
	"aunefyren/poenskelisten/utilities"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	textTemplate "text/template"

	"strconv"
	"time"

	_ "time/tzdata"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func main() {
	utilities.PrintASCII()
	gin.SetMode(gin.ReleaseMode)

	// Create files directory. It holds secrets (config.json, the SQLite
	// database), so it is kept to the app's own user, older installs included.
	newPath := filepath.Join(".", "files")
	err := os.MkdirAll(newPath, config.OwnerOnlyDirMode)
	if err != nil {
		fmt.Println("failed to create 'files' directory. error: " + err.Error())
		os.Exit(1)
	}
	if changed, err := config.RestrictPermissions(newPath, config.OwnerOnlyDirMode); err != nil {
		fmt.Println("warning: 'files' directory may be readable by other users, and restricting it failed. error: " + err.Error())
	} else if changed {
		fmt.Println("directory 'files' was readable by other users; restricted it to its owner.")
	}
	fmt.Println("directory 'files' valid")

	// Load config file
	err = config.LoadConfig()
	if err != nil {
		fmt.Println("failed to load configuration file. error: " + err.Error())
		os.Exit(1)
	}
	fmt.Println("configuration file loaded")

	// Create and define file for logging
	logger.InitLogger(config.ConfigFile)

	logger.Log.Info("running Pønskelisten version: " + config.ConfigFile.PoenskelistenVersion)

	// change the config to respect flags
	var actions startupActions
	flagsProvided := false
	config.ConfigFile, actions, flagsProvided, err = parseFlags(config.ConfigFile)
	if err != nil {
		logger.Log.Error("failed to parse input flags. error: " + err.Error())
		os.Exit(1)
	}
	logger.Log.Info("flags parsed")

	if _, err := config.ParseAdditionalURLs(config.ConfigFile.PoenskelistenAdditionalURLs); err != nil {
		logger.Log.Warn("ignoring invalid entry in poenskelisten_additional_urls: " + err.Error())
	}
	logger.Log.Info("login allowed from: " + strings.Join(config.AllowedOrigins(), ", "))
	// Unlike a bad login origin, a bad proxy list can't be skipped entry by
	// entry: it decides which addresses rate limits apply to.
	trustedProxies, err := config.ParseTrustedProxies(config.ConfigFile.PoenskelistenTrustedProxies)
	if err != nil {
		logger.Log.Error("invalid poenskelisten_trusted_proxies. error: " + err.Error())
		os.Exit(1)
	}
	if len(trustedProxies) == 0 {
		logger.Log.Info("no trusted proxies: client addresses are taken from the connection, and X-Forwarded-For is ignored")
	} else {
		logger.Log.Info("trusting X-Forwarded-For from: " + strings.Join(trustedProxies, ", "))
	}
	if err := config.ValidateRateLimits(config.ConfigFile); err != nil {
		logger.Log.Error("invalid rate limit. error: " + err.Error())
		os.Exit(1)
	}
	if strings.TrimSpace(config.ConfigFile.PoenskelistenExternalURL) == "" {
		logger.Log.Warn("externalurl is not set, so " + config.OAuthIssuer() + " is used for links, single sign-on and as the main login address. Set externalurl to the address users open Pønskelisten on, and additionalurls for any others.")
	}

	if config.ConfigFile.LocalLoginDisabled && config.LocalLoginEnabled() {
		logger.Log.Warn("local login is set to disabled, but OIDC is not enabled and configured; keeping password login on so the instance stays reachable")
	} else if !config.LocalLoginEnabled() {
		logger.Log.Info("local login disabled; users can only sign in with OIDC")
	}

	// Persist the config only when flags/ENV actually overrode something, to
	// avoid rewriting config.json on every startup.
	if flagsProvided {
		err = config.SaveConfig()
		if err != nil {
			logger.Log.Error("failed to save new config. error: " + err.Error())
			os.Exit(1)
		}
	}

	// set time zone from config if it is not empty
	loc, err := time.LoadLocation(config.ConfigFile.Timezone)
	if err != nil {
		logger.Log.Error("Failed to set time zone from config. Error: " + err.Error())
		logger.Log.Warn("removing value...")

		config.ConfigFile.Timezone = "Europe/Paris"
		err = config.SaveConfig()
		if err != nil {
			logger.Log.Error("failed to set new time zone in the config. error: " + err.Error())
			os.Exit(1)
		}

	} else {
		time.Local = loc
	}
	logger.Log.Info("timezone set")

	// Initialize Database
	logger.Log.Info("connecting to database...")

	err = database.Connect(config.ConfigFile.DBType, config.ConfigFile.Timezone, config.ConfigFile.DBUsername, config.ConfigFile.DBPassword, config.ConfigFile.DBIP, config.ConfigFile.DBPort, config.ConfigFile.DBName, config.ConfigFile.DBSSL, config.ConfigFile.DBLocation)
	if err != nil {
		logger.Log.Error("failed to connect to database. error: " + err.Error())
		os.Exit(1)
	}
	database.Migrate()
	logger.Log.Info("database connected")

	if actions.generateInvite {
		invite, err := database.GenerateRandomInvite()
		if err != nil {
			logger.Log.Error("failed to generate random invitation code. error: " + err.Error())
			os.Exit(1)
		}
		logger.Log.Info("generated new invite code. code: " + invite)
	}

	runAccountRecoveryActions(actions)

	// Initialize Router
	router := initRouter(config.ConfigFile)
	logger.Log.Info("router initialized. starting Pønskelisten at http://*:" + strconv.Itoa(config.ConfigFile.PoenskelistenPort))
	log.Fatal(newServer(config.ConfigFile.PoenskelistenPort, router).ListenAndServe())
}

// newServer bounds how long a client may take over each part of a request,
// which router.Run doesn't: a client sending or reading slowly, or not at
// all, would keep its connection open for as long as it likes. The read
// limit leaves time to upload the largest image request, 15 MiB, at about
// 0.7 Mbit/s; the write limit runs from the same start.
func newServer(port int, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              ":" + strconv.Itoa(port),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       3 * time.Minute,
		WriteTimeout:      4 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}
}

func initRouter(configFile models.ConfigStruct) *gin.Engine {
	// Not gin.Default(): its logger writes whole URLs, codes included.
	router := gin.New()
	router.Use(middlewares.AccessLog(gin.DefaultWriter), gin.Recovery())

	// Only the configured reverse proxies may set the client address with
	// X-Forwarded-For or X-Real-IP. Gin trusts every peer by default, which let
	// any client choose the address its rate limits are counted against.
	// main() has already refused an invalid list; trust nobody if one gets here.
	if proxies, proxyErr := config.ParseTrustedProxies(configFile.PoenskelistenTrustedProxies); proxyErr != nil || router.SetTrustedProxies(proxies) != nil {
		logger.Log.Error("invalid trusted proxy list; trusting no proxy")
		_ = router.SetTrustedProxies(nil)
	}
	router.Use(middlewares.WarnUntrustedForwardedFor())
	router.Use(middlewares.SecurityHeaders())

	// Routes whose JSON body can carry a base64 image get a raised limit.
	router.Use(middlewares.MaxBodySize(middlewares.DefaultMaxBodyBytes, map[string]int64{
		"/api/auth/wishes":          middlewares.ImageUploadMaxBodyBytes,
		"/api/auth/wishes/:wish_id": middlewares.ImageUploadMaxBodyBytes,
		"/api/auth/users/update":    middlewares.ImageUploadMaxBodyBytes,
	}))

	// Every endpoint that checks a password or a one-time code (or e-mails
	// one) shares one budget per client address, so guessing can't move from
	// one to the next. The OAuth endpoints share another.
	loginRateLimit := config.LoginRateLimit(configFile)
	loginLimit := middlewares.APIRateLimit(loginRateLimit.Max, loginRateLimit.Window)
	oauthRateLimit := config.OAuthRateLimit(configFile)
	oauthLimit := middlewares.RateLimit(oauthRateLimit.Max, oauthRateLimit.Window)
	registerRateLimit := config.RegisterRateLimit(configFile)
	// Failed passwords and codes are also counted per account, from any address.
	controllers.SetSignInAttemptLimit(config.AccountRateLimit(configFile))
	// The routes that can carry an image share a budget per account.
	uploadRateLimit := config.UploadRateLimit(configFile)
	uploadLimit := middlewares.UploadRateLimit(uploadRateLimit.Max, uploadRateLimit.Window)
	// What turns a password, a code or the sign-in cookie into a session or
	// a consent only takes requests from Pønskelisten's own pages.
	sameOrigin := middlewares.SameOrigin()

	// API endpoint
	api := router.Group("/api")
	{
		open := api.Group("/open")
		{
			// Password-based entry points; all refused when local login is
			// disabled (OIDC-only), see middlewares.RequireLocalLogin.
			localLogin := middlewares.RequireLocalLogin()
			open.POST("/tokens/register", loginLimit, sameOrigin, localLogin, controllers.GenerateToken)

			open.POST("/users", loginLimit, localLogin, controllers.RegisterUser)
			open.POST("/users/reset", loginLimit, localLogin, controllers.APIResetPassword)
			open.GET("/users/reset/:resetCode", loginLimit, localLogin, controllers.APIVerifyResetCode)
			open.POST("/users/password", loginLimit, localLogin, controllers.APIChangePassword)
			open.POST("/users/verify/:code", loginLimit, sameOrigin, controllers.VerifyUser)
			open.POST("/users/verification", loginLimit, controllers.SendUserVerificationCode)

			// MFA enrollment is reachable during the login gate (SSO-authed), so it
			// lives under /open: a user forced to enroll has no access token yet.
			open.POST("/users/mfa/enroll", loginLimit, sameOrigin, controllers.APIEnrollMFA)
			open.POST("/users/mfa/activate", loginLimit, sameOrigin, controllers.APIActivateMFA)

			open.POST("/tokens/mfa", loginLimit, sameOrigin, localLogin, controllers.APIValidateMFA)

			open.GET("/oidc/config", controllers.APIGetOIDCConfig)
			open.GET("/oidc/login", controllers.OIDCLogin)
			open.GET("/oidc/callback", controllers.OIDCCallback)

			open.GET("/wishlists/public/:wishlist_hash", controllers.GetPublicWishlist)
		}

		both := api.Group("/both")
		{
			both.GET("/wishes/:wish_id/image", controllers.APIGetWishImage)
		}

		auth := api.Group("/auth").Use(middlewares.Auth(false))
		{
			auth.GET("/me", controllers.APICurrentUser)
			auth.POST("/tokens/logout-all", controllers.APILogoutAll)

			auth.GET("/connected-apps", controllers.APIListConnectedApps)
			auth.DELETE("/connected-apps/:client_id", controllers.APIRevokeConnectedApp)

			auth.GET("/currency", controllers.APIGetCurrency)

			auth.POST("/groups", controllers.RegisterGroup)
			auth.DELETE("/groups/:group_id", controllers.DeleteGroup)
			auth.POST("/groups/:group_id/join", controllers.JoinGroup)
			auth.POST("/groups/:group_id/leave", controllers.RemoveSelfFromGroup)
			auth.POST("/groups/:group_id/remove", controllers.RemoveFromGroup)
			auth.GET("/groups", controllers.GetGroups)
			auth.GET("/groups/:group_id", controllers.GetGroup)
			auth.GET("/groups/:group_id/members", controllers.GetGroupMembers)
			auth.POST("/groups/:group_id", controllers.APIUpdateGroup)
			auth.POST("/groups/:group_id/add", controllers.APIAddWishlistsToGroup)

			auth.POST("/wishlists", controllers.RegisterWishlist)
			auth.GET("/wishlists", controllers.GetWishlists)
			auth.GET("/wishlists/:wishlist_id", controllers.GetWishlist)
			auth.DELETE("/wishlists/:wishlist_id", controllers.DeleteWishlist)
			auth.POST("/wishlists/:wishlist_id/join", controllers.JoinWishlist)
			auth.POST("/wishlists/:wishlist_id/collaborate", controllers.APICollaborateWishlist)
			auth.POST("/wishlists/:wishlist_id/remove", controllers.RemoveFromWishlist)
			auth.POST("/wishlists/:wishlist_id/un-collaborate", controllers.APIUnCollaborateWishlist)
			auth.POST("/wishlists/:wishlist_id", controllers.APIUpdateWishlist)

			auth.GET("/wishlists/:wishlist_id/categories", controllers.APIGetWishlistCategories)

			auth.GET("/wishes", controllers.GetWishesFromWishlist)
			auth.POST("/wishes", uploadLimit, controllers.RegisterWish)
			auth.DELETE("/wishes/:wish_id", controllers.DeleteWish)
			auth.POST("/wishes/:wish_id/claim", controllers.RegisterWishClaim)
			auth.POST("/wishes/:wish_id/unclaim", controllers.RemoveWishClaim)
			auth.POST("/wishes/:wish_id", uploadLimit, controllers.APIUpdateWish)
			auth.GET("/wishes/:wish_id", controllers.APIGetWish)

			auth.GET("/news", controllers.GetNews)
			auth.GET("/news/:news_id", controllers.GetNewsPost)

			auth.GET("/users/:user_id", controllers.GetUser)
			auth.GET("/users/:user_id/image", controllers.APIGetUserProfileImage)
			auth.GET("/users", controllers.GetUsers)
			// These two check the current password.
			auth.POST("/users/update", loginLimit, uploadLimit, controllers.UpdateUser)

			auth.POST("/users/mfa/disable", loginLimit, controllers.APIDisableMFA)
		}

		admin := api.Group("/admin").Use(middlewares.Auth(true))
		{
			admin.POST("/currency/update", controllers.APIUpdateCurrency)
			admin.POST("/server/settings", controllers.APIUpdateServerSettings)

			admin.DELETE("/users/:user_id", controllers.APIDeleteUser)
			admin.DELETE("/users/:user_id/mfa", controllers.APIAdminDeleteUserMFA)
			admin.DELETE("/users/:user_id/sessions", controllers.APIAdminRevokeUserSessions)

			admin.GET("/oauth/clients", controllers.APIAdminListOAuthClients)
			admin.DELETE("/oauth/clients/:client_id", controllers.APIAdminRevokeOAuthClient)

			admin.POST("/invites", controllers.RegisterInvite)
			admin.GET("/invites", controllers.APIGetAllInvites)
			admin.DELETE("/invites/:invite_id", controllers.APIDeleteInvite)

			admin.POST("/news", controllers.RegisterNewsPost)
			admin.DELETE("/news/:news_id", controllers.DeleteNewsPost)
			admin.POST("/news/:news_id", controllers.APIEditNewsPost)

			admin.POST("/server/info", controllers.APIGetServerInfo)
		}

	}

	router.Use(cors.New(cors.Config{
		AllowOrigins: []string{"*"},
		// AllowAllOrigins:  true,
		AllowMethods:     []string{"GET", "POST"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization", "Access-Control-Allow-Origin"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		AllowOriginFunc:  func(origin string) bool { return true },
		MaxAge:           12 * time.Hour,
	}))

	// load HTML blobs
	router.LoadHTMLGlob("./web/*/*.html")

	// Static endpoint for different directories
	router.Static("/assets", "./web/assets")
	router.Static("/css", "./web/css")

	// JSON-encoded so it's a safe JS string literal: JS templates are text/template
	// and don't escape. Marshalling a plain string can't fail.
	oidcProviderNameJSON, _ := json.Marshal(config.OIDCDisplayName())

	// Create template for HTML variables
	templateData := gin.H{
		"appName":        configFile.PoenskelistenName,
		"currency":       configFile.PoenskelistenCurrency,
		"appDescription": configFile.PoenskelistenDescription,
		// Rendered into the JS so the login/register pages never flash a
		// password form on an OIDC-only instance.
		"localLoginEnabled":    config.LocalLoginEnabled(),
		"oidcProviderNameJSON": string(oidcProviderNameJSON),
	}

	// endpoint handler building for JS
	router, err := registerTemplatedStaticFilesForDirectory(router, "/js", true, "./web/js", withAppText(templateData, escapeForJSHTML))
	if err != nil {
		logger.Log.Error("failed to build JS paths. error: " + err.Error())
	}

	// endpoint handler building for HTML
	router, err = registerTemplatedStaticFilesForDirectory(router, "", false, "./web/html", templateData)
	if err != nil {
		logger.Log.Error("failed to build HTML paths. error: " + err.Error())
	}

	// The MFA step of login.js navigates here (a real page load, not a DOM
	// swap) so password managers like Bitwarden re-inject their content
	// script and offer one-time-code autofill on the new field. Serve the
	// same template used for /login; login.js reads the challenge token
	// back out of sessionStorage.
	router.GET("/login/mfa", func(c *gin.Context) {
		c.HTML(http.StatusOK, "login.html", templateData)
	})

	// The wish and wishlist forms serve both creating (their own URL, from
	// the path table) and editing.
	router.GET("/wishes/:wish_id/edit", func(c *gin.Context) {
		c.HTML(http.StatusOK, "wishform.html", templateData)
	})
	router.GET("/wishlists/:wishlist_id/edit", func(c *gin.Context) {
		c.HTML(http.StatusOK, "wishlistform.html", templateData)
	})

	// endpoint handler building for JSON
	router, err = registerTemplatedStaticFilesForDirectory(router, "/json", true, "./web/json", withAppText(templateData, escapeForJSONString))
	if err != nil {
		logger.Log.Error("failed to build JSON paths. error: " + err.Error())
	}

	// endpoint handler building for TXT
	router, err = registerTemplatedStaticFilesForDirectory(router, "/txt", true, "./web/txt", templateData)
	if err != nil {
		logger.Log.Error("failed to build TXT paths. error: " + err.Error())
	}

	// OAuth 2.1 / MCP discovery documents live at the domain root, outside /api.
	router.GET("/.well-known/oauth-authorization-server", controllers.APIOAuthAuthorizationServerMetadata)
	router.GET("/.well-known/oauth-protected-resource", controllers.APIOAuthProtectedResourceMetadata)
	router.GET("/.well-known/jwks.json", controllers.APIOAuthJWKS)

	// OAuth 2.1 authorization server endpoints (also outside /api).
	router.GET("/oauth/authorize", oauthLimit, controllers.APIOAuthAuthorize)
	router.POST("/oauth/consent", oauthLimit, sameOrigin, controllers.APIOAuthConsent)
	router.POST("/oauth/token", oauthLimit, controllers.APIOAuthToken)
	router.POST("/oauth/revoke", oauthLimit, controllers.APIOAuthRevoke)
	// Open dynamic client registration (RFC 7591), rate-limited per IP.
	router.POST("/oauth/register", middlewares.RateLimit(registerRateLimit.Max, registerRateLimit.Window), controllers.APIOAuthRegister)

	// MCP resource server (self-gates on MCPEnabled; OAuth-protected).
	router.Any("/mcp", mcpserver.Handler())

	return router
}

// withAppText copies templateData with the app name and description passed
// through escape. JS, JSON and TXT files are text/template, which, unlike the
// html/template pages, doesn't escape anything, and the name and description
// are free text from the operator.
func withAppText(templateData gin.H, escape func(string) string) gin.H {
	escaped := gin.H{}
	for key, value := range templateData {
		escaped[key] = value
	}
	for _, key := range []string{"appName", "appDescription"} {
		if value, ok := escaped[key].(string); ok {
			escaped[key] = escape(value)
		}
	}
	return escaped
}

// jsHTMLEscaper turns every character that is special in HTML or in a JS
// template literal into an HTML entity. The JS files place {{.appName}} and
// {{.appDescription}} inside backtick literals that end up as innerHTML, so
// the entities render as the original characters, while no backtick, ${ or
// backslash is left to end the literal or interpolate.
var jsHTMLEscaper = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	`"`, "&#34;",
	"'", "&#39;",
	"`", "&#96;",
	"$", "&#36;",
	`\`, "&#92;",
)

func escapeForJSHTML(value string) string {
	return jsHTMLEscaper.Replace(value)
}

// escapeForJSONString escapes value for use between the quotes of a JSON
// string, e.g. "name": "{{.appName}}" in manifest.json.
func escapeForJSONString(value string) string {
	// Marshalling a plain string can't fail.
	encoded, _ := json.Marshal(value)
	return string(encoded[1 : len(encoded)-1])
}

// startupActions are one-off tasks requested with startup flags. They aren't
// configuration: they are never written to config.json, and they run on every
// start for as long as the flag (or its environment variable) is set.
type startupActions struct {
	generateInvite bool
	// Already cleaned with utilities.CleanConsoleEmail; empty when not set.
	resetPasswordEmail string
	resetMFAEmail      string
}

// actionFlags are the flags that request a startupAction rather than change
// configuration, so they don't count towards flagsProvided.
var actionFlags = map[string]bool{"generateinvite": true, "resetpassword": true, "resetmfa": true}

func parseFlags(configFile models.ConfigStruct) (models.ConfigStruct, startupActions, bool, error) {
	var actions startupActions

	// boolean values
	SMTPBool := "true"
	if configFile.SMTPEnabled {
		SMTPBool = "false"
	}

	// reverse the config bool
	dbSSLBool := "true"
	if !configFile.DBSSL {
		dbSSLBool = "false"
	}

	// Define flag variables with the configuration file as default values
	var port = flag.Int("port", configFile.PoenskelistenPort, "The port Pønskelisten is listening on.")
	var externalURL = flag.String("externalurl", configFile.PoenskelistenExternalURL, "The URL others would use to access Pønskelisten.")
	var additionalURLs = flag.String("additionalurls", configFile.PoenskelistenAdditionalURLs, "Comma-separated extra URLs Pønskelisten can be logged in from, e.g. a LAN address.")
	var trustedProxies = flag.String("trustedproxies", configFile.PoenskelistenTrustedProxies, "Comma-separated IP addresses or CIDR ranges of reverse proxies whose X-Forwarded-For header is trusted for the client address.")
	var timezone = flag.String("timezone", configFile.Timezone, "The timezone Pønskelisten is running in.")
	var environment = flag.String("environment", configFile.PoenskelistenEnvironment, "The environment Pønskelisten is running in. It will behave differently in 'test'.")
	var testemail = flag.String("testemail", configFile.PoenskelistenTestEmail, "The email all emails are sent to in test mode.")
	var name = flag.String("name", configFile.PoenskelistenName, "The name of the application. Replaces 'Pønskelisten'.")
	var description = flag.String("description", configFile.PoenskelistenDescription, "The description of the application.")
	var logLevel = flag.String("loglevel", configFile.PoenskelistenLogLevel, "The log level of the application. Default 'info'.")

	// DB values
	var dbPort = flag.Int("dbport", configFile.DBPort, "The port the database is listening on.")
	var dbType = flag.String("dbtype", configFile.DBType, "The type of database Pønskelisten is interacting with.")
	var dbUsername = flag.String("dbusername", configFile.DBUsername, "The username used to interact with the database.")
	var dbPassword = flag.String("dbpassword", configFile.DBPassword, "The password used to interact with the database.")
	var dbName = flag.String("dbname", configFile.DBName, "The database table used within the database.")
	var dbIP = flag.String("dbip", configFile.DBIP, "The IP address used to reach the database.")
	var dbSSL = flag.String("dbssl", dbSSLBool, "If the database connection uses SSL.")
	var dbLocation = flag.String("dblocation", configFile.DBLocation, "The database is a local file, what is the system file path.")

	// SMTP values
	var smtpDisabled = flag.String("disablesmtp", SMTPBool, "Disables user verification using e-mail.")
	var smtpHost = flag.String("smtphost", configFile.SMTPHost, "The SMTP server which sends e-mail.")
	var smtpPort = flag.Int("smtpport", configFile.SMTPPort, "The SMTP server port.")
	var smtpUsername = flag.String("smtpusername", configFile.SMTPUsername, "The username used to verify against the SMTP server.")
	var smtpPassword = flag.String("smtppassword", configFile.SMTPPassword, "The password used to verify against the SMTP server.")
	var smtpFrom = flag.String("smtpfrom", configFile.SMTPFrom, "The sender address when sending e-mail from Pønskelisten.")

	// Security values
	var mfaEnforced = flag.String("mfaenforced", strconv.FormatBool(configFile.MFAEnforced), "If all local users must enroll in multi-factor authentication.")
	var mfaRecoveryCodes = flag.String("mfarecoverycodes", strconv.FormatBool(configFile.MFARecoveryCodesEnabled), "If users are issued single-use recovery codes when enrolling in MFA.")

	// OIDC values
	var oidcEnabled = flag.String("oidcenabled", strconv.FormatBool(configFile.OIDCEnabled), "If OpenID Connect single sign-on is enabled.")
	var oidcProviderName = flag.String("oidcprovidername", configFile.OIDCProviderName, "The display name of the OIDC provider, shown on the login button.")
	var oidcIssuerURL = flag.String("oidcissuerurl", configFile.OIDCIssuerURL, "The OIDC issuer URL (used for discovery).")
	var oidcClientID = flag.String("oidcclientid", configFile.OIDCClientID, "The OIDC client ID.")
	var oidcClientSecret = flag.String("oidcclientsecret", configFile.OIDCClientSecret, "The OIDC client secret.")
	var oidcRedirectURL = flag.String("oidcredirecturl", configFile.OIDCRedirectURL, "The OIDC redirect/callback URL registered with the provider.")
	var oidcAutoCreate = flag.String("oidcautocreateusers", strconv.FormatBool(configFile.OIDCAutoCreateUsers), "If unknown OIDC users are automatically provisioned an account.")
	var disableLocalLogin = flag.String("disablelocallogin", strconv.FormatBool(configFile.LocalLoginDisabled), "Disables password login, registration and reset so users can only sign in with OIDC. Ignored unless OIDC is enabled and configured.")

	// MCP toggle. The OAuth issuer/algorithm and the API/MCP resource identifiers
	// auto-derive from the external URL and can be hand-edited in config.json if a
	// deployment ever needs to override them.
	var mcpEnabled = flag.String("mcpenabled", strconv.FormatBool(configFile.MCPEnabled), "If the MCP resource server is enabled.")

	// Rate limits, written as "<requests>/<duration>" (e.g. 20/15m), or "off".
	var rateLimitLogin = flag.String("ratelimitlogin", configFile.RateLimitLogin, "Requests per client address to login, MFA, registration, password reset and e-mail verification, e.g. 20/15m, or off. Default "+config.DefaultLoginRateLimit+".")
	var rateLimitOAuth = flag.String("ratelimitoauth", configFile.RateLimitOAuth, "Requests per client address to the OAuth authorize, consent, token and revoke endpoints, e.g. 60/1m, or off. Default "+config.DefaultOAuthRateLimit+".")
	var rateLimitRegister = flag.String("ratelimitregister", configFile.RateLimitRegister, "OAuth client registrations per client address, e.g. 10/1h, or off. Default "+config.DefaultRegisterRateLimit+".")
	var rateLimitAccount = flag.String("ratelimitaccount", configFile.RateLimitAccount, "Failed sign-in attempts per account (wrong passwords per e-mail, wrong codes per user) before it is locked for the rest of the window, e.g. 10/15m, or off. Default "+config.DefaultAccountRateLimit+".")
	var rateLimitUpload = flag.String("ratelimitupload", configFile.RateLimitUpload, "Requests per account to the routes that can carry an image (creating or editing a wish, updating one's account), e.g. 30/10m, or off. Default "+config.DefaultUploadRateLimit+".")

	// Generate invite
	var generateInvite = flag.String("generateinvite", "false", "If an invite code should be automatically generate on startup.")

	// Account recovery for operators with server access; see runAccountRecoveryActions.
	var resetPassword = flag.String("resetpassword", "", "E-mail of a user to issue a password reset link for on startup. The link is written to the log.")
	var resetMFA = flag.String("resetmfa", "", "E-mail of a user to remove multi-factor authentication for on startup.")

	// Parse flags
	flag.Parse()

	// flag pointers are never nil after Parse(), so the only reliable way to
	// tell a deliberately-set flag from one left at its config default is to
	// record which flags were actually provided on the command line. Only
	// provided flags override the loaded configuration.
	provided := map[string]bool{}
	flagsProvided := false
	flag.Visit(func(f *flag.Flag) {
		provided[f.Name] = true
		if !actionFlags[f.Name] {
			flagsProvided = true
		}
	})

	// entrypoint.sh turns the other environment variables into flags, but
	// not the secrets: any user who can see a process can read its arguments,
	// while its environment is readable only by its own user and root. A flag
	// given explicitly still wins.
	for name, value := range map[string]*string{"dbpassword": dbPassword, "smtppassword": smtpPassword, "oidcclientsecret": oidcClientSecret} {
		if secret := os.Getenv(name); secret != "" && !provided[name] {
			*value = secret
			provided[name] = true
			flagsProvided = true
		}
	}

	if provided["port"] {
		configFile.PoenskelistenPort = *port
	}

	if provided["externalurl"] {
		configFile.PoenskelistenExternalURL = *externalURL
	}

	// Validated here so a typo stops startup instead of silently leaving an
	// origin unable to log in. Stored normalized, as a comma-separated list.
	if provided["additionalurls"] {
		origins, err := config.ParseAdditionalURLs(*additionalURLs)
		if err != nil {
			return configFile, actions, flagsProvided, errors.New("invalid -additionalurls value: " + err.Error())
		}
		configFile.PoenskelistenAdditionalURLs = strings.Join(origins, ",")
	}

	if provided["trustedproxies"] {
		proxies, err := config.ParseTrustedProxies(*trustedProxies)
		if err != nil {
			return configFile, actions, flagsProvided, errors.New("invalid -trustedproxies value: " + err.Error())
		}
		configFile.PoenskelistenTrustedProxies = strings.Join(proxies, ",")
	}

	if provided["timezone"] {
		configFile.Timezone = *timezone
	}

	if provided["environment"] {
		configFile.PoenskelistenEnvironment = *environment
	}

	if provided["testemail"] {
		configFile.PoenskelistenTestEmail = *testemail
	}

	if provided["description"] {
		configFile.PoenskelistenDescription = *description
	}

	if provided["name"] {
		configFile.PoenskelistenName = *name
	}

	if provided["name"] || provided["description"] {
		config.CleanAppText(&configFile)
	}

	if provided["loglevel"] {
		parsedLogLevel, parseErr := logrus.ParseLevel(*logLevel)
		if parseErr == nil {
			configFile.PoenskelistenLogLevel = parsedLogLevel.String()
			logger.Log.SetLevel(parsedLogLevel)
			logger.Log.Info("Log level changed to: " + parsedLogLevel.String())
		} else {
			logger.Log.Warn("Failed to parse log level: " + *logLevel)
		}
	}

	if provided["dbport"] {
		configFile.DBPort = *dbPort
	}

	if provided["dbtype"] {
		configFile.DBType = *dbType
	}

	if provided["dbusername"] {
		configFile.DBUsername = *dbUsername
	}

	if provided["dbpassword"] {
		configFile.DBPassword = *dbPassword
	}

	if provided["dbname"] {
		configFile.DBName = *dbName
	}

	if provided["dbip"] {
		configFile.DBIP = *dbIP
	}

	// Parsed as a string so the "--dbssl true/false" calling convention keeps working.
	if provided["dbssl"] {
		configFile.DBSSL = strings.ToLower(*dbSSL) == "true"
	}

	if provided["dblocation"] {
		configFile.DBLocation = *dbLocation
	}

	// disablesmtp=true disables e-mail; disablesmtp=false enables it. Parsed as
	// a string so the "--disablesmtp true/false" calling convention keeps working.
	if provided["disablesmtp"] {
		configFile.SMTPEnabled = strings.ToLower(*smtpDisabled) != "true"
	}

	if provided["smtphost"] {
		configFile.SMTPHost = *smtpHost
	}

	if provided["smtpport"] {
		configFile.SMTPPort = *smtpPort
	}

	if provided["smtpusername"] {
		configFile.SMTPUsername = *smtpUsername
	}

	if provided["smtppassword"] {
		configFile.SMTPPassword = *smtpPassword
	}

	if provided["smtpfrom"] {
		configFile.SMTPFrom = *smtpFrom
	}

	// Parsed as a string so the "--mfaenforced true/false" calling convention
	// matches the other boolean flags.
	if provided["mfaenforced"] {
		configFile.MFAEnforced = strings.ToLower(*mfaEnforced) == "true"
	}

	if provided["mfarecoverycodes"] {
		configFile.MFARecoveryCodesEnabled = strings.ToLower(*mfaRecoveryCodes) == "true"
	}

	if provided["oidcenabled"] {
		configFile.OIDCEnabled = strings.ToLower(*oidcEnabled) == "true"
	}

	if provided["oidcprovidername"] {
		configFile.OIDCProviderName = *oidcProviderName
	}

	if provided["oidcissuerurl"] {
		configFile.OIDCIssuerURL = *oidcIssuerURL
	}

	if provided["oidcclientid"] {
		configFile.OIDCClientID = *oidcClientID
	}

	if provided["oidcclientsecret"] {
		configFile.OIDCClientSecret = *oidcClientSecret
	}

	if provided["oidcredirecturl"] {
		configFile.OIDCRedirectURL = *oidcRedirectURL
	}

	if provided["oidcautocreateusers"] {
		configFile.OIDCAutoCreateUsers = strings.ToLower(*oidcAutoCreate) == "true"
	}

	if provided["disablelocallogin"] {
		configFile.LocalLoginDisabled = strings.ToLower(*disableLocalLogin) == "true"
	}

	if provided["mcpenabled"] {
		configFile.MCPEnabled = strings.ToLower(*mcpEnabled) == "true"
	}

	// Validated here so a typo stops startup rather than silently falling back
	// to the default (or to no limit).
	rateLimitFlags := []struct {
		name   string
		value  *string
		target *string
	}{
		{"ratelimitlogin", rateLimitLogin, &configFile.RateLimitLogin},
		{"ratelimitoauth", rateLimitOAuth, &configFile.RateLimitOAuth},
		{"ratelimitregister", rateLimitRegister, &configFile.RateLimitRegister},
		{"ratelimitaccount", rateLimitAccount, &configFile.RateLimitAccount},
		{"ratelimitupload", rateLimitUpload, &configFile.RateLimitUpload},
	}
	for _, rateLimitFlag := range rateLimitFlags {
		if !provided[rateLimitFlag.name] {
			continue
		}
		value := strings.TrimSpace(*rateLimitFlag.value)
		if value != "" {
			if _, err := config.ParseRateLimit(value); err != nil {
				return configFile, actions, flagsProvided, errors.New("invalid -" + rateLimitFlag.name + " value: " + err.Error())
			}
		}
		*rateLimitFlag.target = value
	}

	// Runtime-only actions, never persisted to config.
	if provided["generateinvite"] {
		actions.generateInvite = strings.ToLower(*generateInvite) == "true"
	}

	// An empty value (e.g. an unset variable expanded by the entrypoint) means
	// "not requested"; anything else must be a clean address, or startup
	// stops before touching any account.
	if provided["resetpassword"] && strings.TrimSpace(*resetPassword) != "" {
		email, err := utilities.CleanConsoleEmail(*resetPassword)
		if err != nil {
			return configFile, actions, flagsProvided, errors.New("invalid -resetpassword value: " + err.Error())
		}
		actions.resetPasswordEmail = email
	}

	if provided["resetmfa"] && strings.TrimSpace(*resetMFA) != "" {
		email, err := utilities.CleanConsoleEmail(*resetMFA)
		if err != nil {
			return configFile, actions, flagsProvided, errors.New("invalid -resetmfa value: " + err.Error())
		}
		actions.resetMFAEmail = email
	}

	// Failsafe, if port is 0, set to default 8080
	if configFile.PoenskelistenPort == 0 {
		configFile.PoenskelistenPort = 8080
	}

	return configFile, actions, flagsProvided, nil
}

// runAccountRecoveryActions performs the -resetmfa and -resetpassword
// actions. Failures are logged rather than fatal: these flags are often left
// set as environment variables, and a user deleted later shouldn't stop the
// instance from starting.
func runAccountRecoveryActions(actions startupActions) {
	if actions.resetMFAEmail != "" {
		user, err := controllers.ResetUserMFA(actions.resetMFAEmail)
		if err != nil {
			logger.Log.Error("resetmfa: failed to remove MFA for '" + actions.resetMFAEmail + "'. error: " + err.Error())
		} else {
			logger.Log.Warn("resetmfa: removed MFA for user " + user.ID.String() + " ('" + actions.resetMFAEmail + "'). Remove the resetmfa flag/variable now, or MFA is removed again on every start.")
		}
	}

	if actions.resetPasswordEmail != "" {
		user, link, err := controllers.IssuePasswordResetLink(actions.resetPasswordEmail)
		if err != nil {
			logger.Log.Error("resetpassword: failed to issue a reset link for '" + actions.resetPasswordEmail + "'. error: " + err.Error())
		} else {
			logger.Log.Warn("resetpassword: password reset link for user " + user.ID.String() + " ('" + actions.resetPasswordEmail + "'), valid for 24 hours: " + link + " Remove the resetpassword flag/variable now, or a new link is issued on every start.")
			if !config.LocalLoginEnabled() {
				logger.Log.Warn("resetpassword: password login is disabled (OIDC-only), so the reset link will be refused until disablelocallogin is set to false.")
			}
		}
	}
}

func registerTemplatedStaticFilesForDirectory(
	r *gin.Engine,
	urlPrefix string,
	keepFileExtension bool,
	fileDirectory string,
	templateData any,
) (
	newRouter *gin.Engine,
	err error,
) {
	type filePathTable struct {
		urlPath string
	}
	filePathTableList := map[string]*filePathTable{}
	filePathTableList["frontpage.html"] = &filePathTable{urlPath: "/"}
	filePathTableList["group.html"] = &filePathTable{urlPath: "/groups/:group_id"}
	filePathTableList["wishlist.html"] = &filePathTable{urlPath: "/wishlists/:wishlist_id"}
	filePathTableList["public.html"] = &filePathTable{urlPath: "/wishlists/public/:wishlist_hash"}
	filePathTableList["wishlistform.html"] = &filePathTable{urlPath: "/wishlists/new"}
	filePathTableList["wishform.html"] = &filePathTable{urlPath: "/wishes/new"}
	filePathTableList["callback.html"] = &filePathTable{urlPath: "/oauth/callback"}
	filePathTableList["enroll.html"] = &filePathTable{urlPath: "/enroll"}
	filePathTableList["manifest.json"] = &filePathTable{urlPath: "/manifest.json"}
	filePathTableList["service-worker.js"] = &filePathTable{urlPath: "/service-worker.js"}
	filePathTableList["robots.txt"] = &filePathTable{urlPath: "/robots.txt"}

	root := os.DirFS(fileDirectory)
	jsTemplates := MustLoadTemplates("./web/js/*.js")
	jsonTemplates := MustLoadTemplates("./web/json/*.json")
	txtTemplates := MustLoadTemplates("./web/txt/*.txt")

	foundFiles, err := fs.Glob(root, "*")

	if err != nil {
		logger.Log.Error("failed to load directory. error: " + err.Error())
		return
	}

	logger.Log.Debug("found " + strconv.Itoa(len(foundFiles)) + " files for endpoint mapping using path: " + fileDirectory)

	for _, file := range foundFiles {
		fileWithoutExtension := file
		extension := filepath.Ext(file)

		if !keepFileExtension && strings.Contains(file, ".") {
			fileWithoutExtension = strings.TrimSuffix(file, extension)
		}

		path := urlPrefix + "/" + fileWithoutExtension

		if filePathTableList[file] != nil {
			path = filePathTableList[file].urlPath
		}

		switch strings.ToLower(extension) {
		case ".html":
			r.GET(path, func(c *gin.Context) {
				c.HTML(http.StatusOK, file, templateData)
			})
			logger.Log.Debug("registered HTML '" + file + "' to path '" + path + "'")
		case ".js":
			r.GET(path, RenderJSTemplate(jsTemplates, file, templateData))
			logger.Log.Debug("registered JS '" + file + "' to path '" + path + "'")
		case ".json":
			r.GET(path, RenderJSONTemplate(jsonTemplates, file, templateData))
			logger.Log.Debug("registered JSON '" + file + "' to path '" + path + "'")
		case ".txt":
			r.GET(path, RenderTextTemplate(txtTemplates, file, templateData))
			logger.Log.Debug("registered TXT '" + file + "' to path '" + path + "'")
		}
	}

	return r, err
}

func MustLoadTemplates(glob string) *textTemplate.Template {
	t, err := textTemplate.ParseGlob(glob)
	if err != nil {
		logger.Log.Warn("failed to parse file: " + glob)
	}
	return t
}

func RenderJSTemplate(jsTemplates *textTemplate.Template, name string, data any) gin.HandlerFunc {
	return func(c *gin.Context) {
		var buf bytes.Buffer
		if err := jsTemplates.ExecuteTemplate(&buf, name, data); err != nil {
			c.String(http.StatusInternalServerError, "template error: %v", err)
			return
		}

		c.Data(http.StatusOK, "application/javascript; charset=utf-8", buf.Bytes())
	}
}

func RenderJSONTemplate(jsTemplates *textTemplate.Template, name string, data any) gin.HandlerFunc {
	return func(c *gin.Context) {
		var buf bytes.Buffer
		if err := jsTemplates.ExecuteTemplate(&buf, name, data); err != nil {
			c.String(http.StatusInternalServerError, "template error: %v", err)
			return
		}

		c.Data(http.StatusOK, "application/json; charset=utf-8", buf.Bytes())
	}
}

func RenderTextTemplate(jsTemplates *textTemplate.Template, name string, data any) gin.HandlerFunc {
	return func(c *gin.Context) {
		var buf bytes.Buffer
		if err := jsTemplates.ExecuteTemplate(&buf, name, data); err != nil {
			c.String(http.StatusInternalServerError, "template error: %v", err)
			return
		}

		c.Data(http.StatusOK, "text/plain; charset=utf-8", buf.Bytes())
	}
}
