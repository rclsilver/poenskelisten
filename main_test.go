package main

import (
	"aunefyren/poenskelisten/config"
	"aunefyren/poenskelisten/database"
	"aunefyren/poenskelisten/logger"
	"aunefyren/poenskelisten/middlewares"
	"aunefyren/poenskelisten/models"
	"database/sql"
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	textTemplate "text/template"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	logrusTest "github.com/sirupsen/logrus/hooks/test"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	_ "modernc.org/sqlite"
)

func init() {
	gin.SetMode(gin.TestMode)
	if logger.Log == nil {
		logger.Log = logrus.New()
	}
}

// withArgs swaps in a fresh flag.CommandLine and os.Args for one parseFlags
// call: parseFlags registers its flags on the global set, so calling it twice
// against the same set would panic on redefinition, and `go test` has already
// consumed os.Args for its own -test.* flags.
func withArgs(t *testing.T, args ...string) {
	t.Helper()
	origArgs, origCommandLine := os.Args, flag.CommandLine
	t.Cleanup(func() {
		os.Args, flag.CommandLine = origArgs, origCommandLine
	})
	os.Args = append([]string{"poenskelisten"}, args...)
	flag.CommandLine = flag.NewFlagSet("poenskelisten", flag.ContinueOnError)
}

func TestParseFlagsNoFlagsKeepsConfig(t *testing.T) {
	withArgs(t)
	logLevel := logger.Log.GetLevel()
	t.Cleanup(func() { logger.Log.SetLevel(logLevel) })

	in := models.ConfigStruct{
		PoenskelistenPort: 9000,
		PoenskelistenName: "Wishes",
		DBType:            "postgres",
		DBSSL:             true,
		SMTPEnabled:       true,
		MFAEnforced:       true,
	}

	out, actions, flagsProvided, err := parseFlags(in)
	if err != nil {
		t.Fatalf("parseFlags returned error: %v", err)
	}
	if flagsProvided {
		t.Error("flagsProvided = true with no flags on the command line")
	}
	if actions != (startupActions{}) {
		t.Errorf("actions = %+v with no flags on the command line", actions)
	}
	if out != in {
		t.Errorf("config changed with no flags provided:\n got %+v\nwant %+v", out, in)
	}
}

func TestParseFlagsRecoveryActions(t *testing.T) {
	cases := []struct {
		name        string
		args        []string
		want        startupActions
		wantErr     bool
		wantPersist bool
	}{
		{
			name: "both flags, cleaned",
			args: []string{"-resetpassword", "  'Admin@Example.com' ", "-resetmfa", "\tadmin@example.com\n"},
			want: startupActions{resetPasswordEmail: "Admin@Example.com", resetMFAEmail: "admin@example.com"},
		},
		{
			name: "empty value means not requested",
			args: []string{"-resetpassword", "", "-resetmfa", "   "},
		},
		{
			name: "action flags alone don't persist config",
			args: []string{"-generateinvite", "true", "-resetmfa", "a@b.c"},
			want: startupActions{generateInvite: true, resetMFAEmail: "a@b.c"},
		},
		{
			name:        "config flag alongside still persists",
			args:        []string{"-resetmfa", "a@b.c", "-port", "9000"},
			want:        startupActions{resetMFAEmail: "a@b.c"},
			wantPersist: true,
		},
		{
			name:    "embedded newline rejected",
			args:    []string{"-resetpassword", "a@b.c\nfake log line"},
			wantErr: true,
		},
		{
			name:    "not an address rejected",
			args:    []string{"-resetmfa", "admin"},
			wantErr: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withArgs(t, c.args...)

			_, actions, flagsProvided, err := parseFlags(models.ConfigStruct{})
			if c.wantErr {
				if err == nil {
					t.Errorf("expected an error, got actions %+v", actions)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseFlags returned error: %v", err)
			}
			if actions != c.want {
				t.Errorf("actions = %+v, want %+v", actions, c.want)
			}
			if flagsProvided != c.wantPersist {
				t.Errorf("flagsProvided = %v, want %v", flagsProvided, c.wantPersist)
			}
		})
	}
}

func TestParseFlagsZeroPortFallsBackTo8080(t *testing.T) {
	withArgs(t)

	out, _, _, err := parseFlags(models.ConfigStruct{})
	if err != nil {
		t.Fatalf("parseFlags returned error: %v", err)
	}
	if out.PoenskelistenPort != 8080 {
		t.Errorf("PoenskelistenPort = %d, want 8080", out.PoenskelistenPort)
	}
}

func TestParseFlagsOverridesEveryField(t *testing.T) {
	withArgs(t,
		"-port", "9001",
		"-externalurl", "https://wish.example.com",
		"-additionalurls", "http://192.168.1.10:8080/, HTTP://Wish.LAN",
		"-trustedproxies", "10.244.1.1/16, 127.0.0.1",
		"-timezone", "Europe/Oslo",
		"-environment", "test",
		"-testemail", "test@example.com",
		"-name", "Wishes",
		"-description", "A wishlist app",
		"-loglevel", "debug",
		"-dbport", "5432",
		"-dbtype", "postgres",
		"-dbusername", "dbuser",
		"-dbpassword", "dbpass",
		"-dbname", "wishes",
		"-dbip", "10.0.0.2",
		"-dbssl", "TRUE",
		"-dblocation", "/data/db.sqlite",
		"-disablesmtp", "false",
		"-smtphost", "smtp.example.com",
		"-smtpport", "587",
		"-smtpusername", "smtpuser",
		"-smtppassword", "smtppass",
		"-smtpfrom", "noreply@example.com",
		"-mfaenforced", "true",
		"-mfarecoverycodes", "true",
		"-oidcenabled", "true",
		"-oidcprovidername", "Authentik",
		"-oidcissuerurl", "https://idp.example.com",
		"-oidcclientid", "client",
		"-oidcclientsecret", "secret",
		"-oidcredirecturl", "https://wish.example.com/api/open/oidc/callback",
		"-oidcautocreateusers", "true",
		"-disablelocallogin", "true",
		"-mcpenabled", "true",
		"-ratelimitlogin", " 5/1m ",
		"-ratelimitoauth", "off",
		"-ratelimitregister", "3/2h",
		"-ratelimitaccount", "4/30m",
		"-generateinvite", "true",
	)
	logLevel := logger.Log.GetLevel()
	t.Cleanup(func() { logger.Log.SetLevel(logLevel) })

	out, actions, flagsProvided, err := parseFlags(models.ConfigStruct{})
	if err != nil {
		t.Fatalf("parseFlags returned error: %v", err)
	}
	if !flagsProvided {
		t.Error("flagsProvided = false with flags on the command line")
	}
	if !actions.generateInvite {
		t.Error("generateInvite = false with -generateinvite true")
	}

	want := models.ConfigStruct{
		PoenskelistenPort:           9001,
		PoenskelistenExternalURL:    "https://wish.example.com",
		PoenskelistenAdditionalURLs: "http://192.168.1.10:8080,http://wish.lan",
		PoenskelistenTrustedProxies: "10.244.0.0/16,127.0.0.1",
		Timezone:                    "Europe/Oslo",
		PoenskelistenEnvironment:    "test",
		PoenskelistenTestEmail:      "test@example.com",
		PoenskelistenName:           "Wishes",
		PoenskelistenDescription:    "A wishlist app",
		PoenskelistenLogLevel:       "debug",
		DBPort:                      5432,
		DBType:                      "postgres",
		DBUsername:                  "dbuser",
		DBPassword:                  "dbpass",
		DBName:                      "wishes",
		DBIP:                        "10.0.0.2",
		DBSSL:                       true,
		DBLocation:                  "/data/db.sqlite",
		SMTPEnabled:                 true,
		SMTPHost:                    "smtp.example.com",
		SMTPPort:                    587,
		SMTPUsername:                "smtpuser",
		SMTPPassword:                "smtppass",
		SMTPFrom:                    "noreply@example.com",
		MFAEnforced:                 true,
		MFARecoveryCodesEnabled:     true,
		OIDCEnabled:                 true,
		OIDCProviderName:            "Authentik",
		OIDCIssuerURL:               "https://idp.example.com",
		OIDCClientID:                "client",
		OIDCClientSecret:            "secret",
		OIDCRedirectURL:             "https://wish.example.com/api/open/oidc/callback",
		OIDCAutoCreateUsers:         true,
		LocalLoginDisabled:          true,
		MCPEnabled:                  true,
		RateLimitLogin:              "5/1m",
		RateLimitOAuth:              "off",
		RateLimitRegister:           "3/2h",
		RateLimitAccount:            "4/30m",
	}
	if out != want {
		t.Errorf("parsed config mismatch:\n got %+v\nwant %+v", out, want)
	}
	if logger.Log.GetLevel() != logrus.DebugLevel {
		t.Errorf("logger level = %v, want debug", logger.Log.GetLevel())
	}
}

func TestParseFlagsInvalidAdditionalURLsFails(t *testing.T) {
	withArgs(t, "-additionalurls", "http://wish.lan,192.168.1.10:8080")

	_, _, _, err := parseFlags(models.ConfigStruct{PoenskelistenAdditionalURLs: "http://kept.lan"})
	if err == nil || !strings.Contains(err.Error(), "additionalurls") {
		t.Errorf("parseFlags error = %v, want an invalid -additionalurls error", err)
	}
}

func TestParseFlagsDisableSMTPTrueDisablesSMTP(t *testing.T) {
	withArgs(t, "-disablesmtp", "true")

	out, _, _, err := parseFlags(models.ConfigStruct{SMTPEnabled: true})
	if err != nil {
		t.Fatalf("parseFlags returned error: %v", err)
	}
	if out.SMTPEnabled {
		t.Error("SMTPEnabled = true after -disablesmtp true")
	}
}

func TestParseFlagsInvalidLogLevelKeepsConfigured(t *testing.T) {
	withArgs(t, "-loglevel", "not-a-level")
	logLevel := logger.Log.GetLevel()
	t.Cleanup(func() { logger.Log.SetLevel(logLevel) })

	out, _, _, err := parseFlags(models.ConfigStruct{PoenskelistenLogLevel: "info"})
	if err != nil {
		t.Fatalf("parseFlags returned error: %v", err)
	}
	if out.PoenskelistenLogLevel != "info" {
		t.Errorf("PoenskelistenLogLevel = %q, want unchanged %q", out.PoenskelistenLogLevel, "info")
	}
	if logger.Log.GetLevel() != logLevel {
		t.Errorf("logger level changed to %v on an unparseable -loglevel", logger.Log.GetLevel())
	}
}

func serve(router *gin.Engine, method, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	return w
}

// TestInitRouterServesTemplatedFrontend renders every kind of templated static
// file through the real router, which also proves each template executes
// against the data initRouter hands it (a broken template would 500 here
// rather than only in a browser).
func TestInitRouterServesTemplatedFrontend(t *testing.T) {
	router := initRouter(models.ConfigStruct{
		PoenskelistenName:        "Wishes",
		PoenskelistenCurrency:    "NOK",
		PoenskelistenDescription: "A wishlist app",
	})

	cases := []struct {
		path        string
		contentType string
	}{
		{"/", "text/html"},
		{"/login", "text/html"},
		{"/login/mfa", "text/html"},
		{"/groups/abc", "text/html"},
		{"/wishlists/abc", "text/html"},
		{"/wishlists/public/abc", "text/html"},
		{"/oauth/callback", "text/html"},
		{"/enroll", "text/html"},
		{"/account", "text/html"},
		{"/js/login.js", "application/javascript"},
		{"/js/core.js", "application/javascript"},
		{"/js/session.js", "application/javascript"},
		{"/register", "text/html"},
		{"/verify", "text/html"},
		{"/json/lang-en.json", "application/json"},
		{"/json/lang-fr.json", "application/json"},
		{"/css/app.css", "text/css"},
		{"/service-worker.js", "application/javascript"},
		{"/manifest.json", "application/json"},
		{"/robots.txt", "text/plain"},
		{"/offline", "text/html"},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			w := serve(router, http.MethodGet, c.path)
			if w.Code != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200; body: %s", c.path, w.Code, w.Body.String())
			}
			if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, c.contentType) {
				t.Errorf("GET %s Content-Type = %q, want prefix %q", c.path, got, c.contentType)
			}
		})
	}
}

func TestInitRouterInjectsTemplateData(t *testing.T) {
	router := initRouter(models.ConfigStruct{PoenskelistenName: "Distinctive App Name"})

	w := serve(router, http.MethodGet, "/manifest.json")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /manifest.json = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Distinctive App Name") {
		t.Errorf("manifest.json does not contain the configured app name; body: %s", w.Body.String())
	}
}

func TestInitRouterProtectsAuthAndAdminGroups(t *testing.T) {
	router := initRouter(models.ConfigStruct{})

	for _, path := range []string{"/api/auth/me", "/api/admin/invites"} {
		w := serve(router, http.MethodGet, path)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("GET %s without a token = %d, want 401", path, w.Code)
		}
	}
}

// Verifies the body-size override table in initRouter matches real routes: a
// renamed image route would otherwise silently fall back to the 1 MB default.
func TestInitRouterBodySizeLimits(t *testing.T) {
	router := initRouter(models.ConfigStruct{})

	cases := []struct {
		path          string
		contentLength int64
		want          int
	}{
		// Under the image limit: passes the size check, stopped by auth instead.
		{"/api/auth/wishes", 5 << 20, http.StatusUnauthorized},
		{"/api/auth/wishes/00000000-0000-0000-0000-000000000001", 5 << 20, http.StatusUnauthorized},
		{"/api/auth/users/update", 5 << 20, http.StatusUnauthorized},
		{"/api/auth/users/update", 16 << 20, http.StatusRequestEntityTooLarge},
		// Routes without an override keep the default.
		{"/api/auth/groups", 2 << 20, http.StatusRequestEntityTooLarge},
		{"/api/open/users", 2 << 20, http.StatusRequestEntityTooLarge},
	}
	for _, c := range cases {
		request := httptest.NewRequest(http.MethodPost, c.path, strings.NewReader("{}"))
		request.ContentLength = c.contentLength
		w := httptest.NewRecorder()
		router.ServeHTTP(w, request)
		if w.Code != c.want {
			t.Errorf("POST %s with Content-Length %d = %d, want %d", c.path, c.contentLength, w.Code, c.want)
		}
	}
}

func TestInitRouterUnknownPathIs404(t *testing.T) {
	router := initRouter(models.ConfigStruct{})

	if w := serve(router, http.MethodGet, "/no-such-page"); w.Code != http.StatusNotFound {
		t.Errorf("GET /no-such-page = %d, want 404", w.Code)
	}
}

func TestRegisterTemplatedStaticFilesForDirectoryMissingDirRegistersNothing(t *testing.T) {
	router := gin.New()

	got, err := registerTemplatedStaticFilesForDirectory(router, "/js", true, "./does-not-exist", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Routes()) != 0 {
		t.Errorf("registered %d routes for a missing directory, want 0", len(got.Routes()))
	}
}

func TestMustLoadTemplatesNoMatchReturnsNil(t *testing.T) {
	if tmpl := MustLoadTemplates("./does-not-exist/*.js"); tmpl != nil {
		t.Errorf("MustLoadTemplates on an empty glob = %v, want nil", tmpl)
	}
}

func TestRenderTemplateHandlers(t *testing.T) {
	tmpl := textTemplate.Must(textTemplate.New("ok").Parse("hello {{.}}"))

	cases := []struct {
		name        string
		handler     func(*textTemplate.Template, string, any) gin.HandlerFunc
		contentType string
	}{
		{"js", RenderJSTemplate, "application/javascript; charset=utf-8"},
		{"json", RenderJSONTemplate, "application/json; charset=utf-8"},
		{"text", RenderTextTemplate, "text/plain; charset=utf-8"},
	}
	for _, c := range cases {
		t.Run(c.name+"/ok", func(t *testing.T) {
			router := gin.New()
			router.GET("/", c.handler(tmpl, "ok", "world"))

			w := serve(router, http.MethodGet, "/")
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", w.Code)
			}
			if w.Body.String() != "hello world" {
				t.Errorf("body = %q, want %q", w.Body.String(), "hello world")
			}
			if got := w.Header().Get("Content-Type"); got != c.contentType {
				t.Errorf("Content-Type = %q, want %q", got, c.contentType)
			}
		})
		t.Run(c.name+"/missing template", func(t *testing.T) {
			router := gin.New()
			router.GET("/", c.handler(tmpl, "missing", nil))

			w := serve(router, http.MethodGet, "/")
			if w.Code != http.StatusInternalServerError {
				t.Errorf("status = %d, want 500", w.Code)
			}
			if !strings.Contains(w.Body.String(), "template error") {
				t.Errorf("body = %q, want a template error", w.Body.String())
			}
		})
	}
}

// setupRecoveryDB points database.Instance at a fresh in-memory SQLite DB
// holding the tables the account-recovery actions touch, restoring the
// previous instance afterwards.
func setupRecoveryDB(t *testing.T) {
	t.Helper()
	dbSQL, err := sql.Open("sqlite", "file:"+uuid.NewString()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("failed to open in-memory sqlite: %v", err)
	}
	t.Cleanup(func() { dbSQL.Close() })
	dbSQL.SetMaxOpenConns(1)

	instance, err := gorm.Open(sqlite.Dialector{Conn: dbSQL}, &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open gorm: %v", err)
	}
	if err := instance.AutoMigrate(&models.User{}, &models.MFARecoveryCode{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	origInstance := database.Instance
	t.Cleanup(func() { database.Instance = origInstance })
	database.Instance = instance
}

// createRecoveryUser inserts an enabled user with MFA switched on.
func createRecoveryUser(t *testing.T, email string) models.User {
	t.Helper()
	enabled, mfaEnabled, secret := true, true, "encrypted-secret"
	user := models.User{FirstName: "Ada", Email: &email, Enabled: &enabled, MFAEnabled: &mfaEnabled, MFASecret: &secret}
	user.ID = uuid.New()
	if r := database.Instance.Create(&user); r.Error != nil {
		t.Fatalf("failed to create user: %v", r.Error)
	}
	return user
}

// captureLogs attaches a test hook to logger.Log for the duration of the test.
func captureLogs(t *testing.T) *logrusTest.Hook {
	t.Helper()
	hook := logrusTest.NewLocal(logger.Log)
	t.Cleanup(func() { logger.Log.ReplaceHooks(make(logrus.LevelHooks)) })
	return hook
}

// logged reports whether an entry at level containing substr was captured.
func logged(hook *logrusTest.Hook, level logrus.Level, substr string) bool {
	for _, entry := range hook.AllEntries() {
		if entry.Level == level && strings.Contains(entry.Message, substr) {
			return true
		}
	}
	return false
}

func TestRunAccountRecoveryActionsNoneIsNoop(t *testing.T) {
	hook := captureLogs(t)
	// No DB is set up: with no actions requested, nothing may touch it.
	runAccountRecoveryActions(startupActions{generateInvite: true})
	if n := len(hook.AllEntries()); n != 0 {
		t.Errorf("logged %d entries with no recovery actions, want 0: %+v", n, hook.AllEntries())
	}
}

func TestRunAccountRecoveryActionsResetMFA(t *testing.T) {
	setupRecoveryDB(t)
	user := createRecoveryUser(t, "ada@example.com")
	hook := captureLogs(t)

	runAccountRecoveryActions(startupActions{resetMFAEmail: "ada@example.com"})

	var got models.User
	if r := database.Instance.First(&got, "id = ?", user.ID); r.Error != nil {
		t.Fatalf("failed to reload user: %v", r.Error)
	}
	if got.MFAEnabled == nil || *got.MFAEnabled || got.MFASecret != nil {
		t.Errorf("after resetmfa MFAEnabled = %v, MFASecret = %v; want false and nil", got.MFAEnabled, got.MFASecret)
	}
	if !logged(hook, logrus.WarnLevel, "removed MFA for user "+user.ID.String()) {
		t.Errorf("missing resetmfa success warning; entries: %+v", hook.AllEntries())
	}
}

func TestRunAccountRecoveryActionsUnknownEmailLogsErrors(t *testing.T) {
	setupRecoveryDB(t)
	hook := captureLogs(t)

	// Must log and carry on rather than exit: these flags often linger as env
	// vars after the user they named is gone.
	runAccountRecoveryActions(startupActions{resetMFAEmail: "ghost@example.com", resetPasswordEmail: "ghost@example.com"})

	if !logged(hook, logrus.ErrorLevel, "resetmfa: failed to remove MFA for 'ghost@example.com'") {
		t.Errorf("missing resetmfa error; entries: %+v", hook.AllEntries())
	}
	if !logged(hook, logrus.ErrorLevel, "resetpassword: failed to issue a reset link for 'ghost@example.com'") {
		t.Errorf("missing resetpassword error; entries: %+v", hook.AllEntries())
	}
}

func TestRunAccountRecoveryActionsResetPassword(t *testing.T) {
	cases := []struct {
		name          string
		oidcOnly      bool
		wantOIDCNotes bool
	}{
		{"local login enabled", false, false},
		{"OIDC-only instance warns link is refused", true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			origConfig := config.ConfigFile
			t.Cleanup(func() { config.ConfigFile = origConfig })
			config.ConfigFile.PoenskelistenExternalURL = "https://wishes.example.com"
			config.ConfigFile.LocalLoginDisabled = c.oidcOnly
			config.ConfigFile.OIDCEnabled = c.oidcOnly
			config.ConfigFile.OIDCIssuerURL = "https://idp.example.com"
			config.ConfigFile.OIDCClientID = "client"

			setupRecoveryDB(t)
			user := createRecoveryUser(t, "ada@example.com")
			hook := captureLogs(t)

			runAccountRecoveryActions(startupActions{resetPasswordEmail: "ada@example.com"})

			var got models.User
			if r := database.Instance.First(&got, "id = ?", user.ID); r.Error != nil {
				t.Fatalf("failed to reload user: %v", r.Error)
			}
			if got.ResetCode == nil || *got.ResetCode == "" {
				t.Fatal("no reset code was stored for the user")
			}
			wantLink := "https://wishes.example.com/login?reset_code=" + *got.ResetCode
			if !logged(hook, logrus.WarnLevel, wantLink) {
				t.Errorf("missing warning containing %q; entries: %+v", wantLink, hook.AllEntries())
			}
			if gotNote := logged(hook, logrus.WarnLevel, "password login is disabled"); gotNote != c.wantOIDCNotes {
				t.Errorf("OIDC-only warning logged = %v, want %v", gotNote, c.wantOIDCNotes)
			}
		})
	}
}

func TestParseFlagsCleansAppText(t *testing.T) {
	withArgs(t, "-name", "   ", "-description", "  A wishlist app \n")

	out, _, _, err := parseFlags(models.ConfigStruct{PoenskelistenName: "Wishes"})
	if err != nil {
		t.Fatalf("parseFlags returned error: %v", err)
	}
	if out.PoenskelistenName != "Pønskelisten" {
		t.Errorf("PoenskelistenName = %q, want the default for a blank -name", out.PoenskelistenName)
	}
	if out.PoenskelistenDescription != "A wishlist app" {
		t.Errorf("PoenskelistenDescription = %q, want it trimmed", out.PoenskelistenDescription)
	}
}

// TestInitRouterEscapesAppText checks that an app name full of characters
// special to HTML, JS template literals and JSON can't break out of the
// text/template-rendered JS and JSON files.
func TestInitRouterEscapesAppText(t *testing.T) {
	name := "A`${alert(1)}\\ & <b>\"'"
	router := initRouter(models.ConfigStruct{PoenskelistenName: name, PoenskelistenDescription: name})

	// The scripts read the name from the page, but any JS template that
	// places it in a template literal gets this escaping.
	escaped := "A&#96;&#36;{alert(1)}&#92; &amp; &lt;b&gt;&#34;&#39;"
	if got := withAppText(gin.H{"appName": name}, escapeForJSHTML)["appName"]; got != escaped {
		t.Errorf("escaped JS app name = %q, want %q", got, escaped)
	}
	scripts, err := filepath.Glob("web/js/*.js")
	if err != nil || len(scripts) == 0 {
		t.Fatalf("no scripts found (err %v)", err)
	}
	for _, script := range scripts {
		path := "/js/" + filepath.Base(script)
		if path == "/js/service-worker.js" {
			path = "/service-worker.js"
		}
		w := serve(router, http.MethodGet, path)
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", path, w.Code)
		}
		if strings.Contains(w.Body.String(), name) || strings.Contains(w.Body.String(), "${alert") {
			t.Errorf("%s contains the raw app name", path)
		}
	}

	w := serve(router, http.MethodGet, "/")
	if strings.Contains(w.Body.String(), "<b>") || !strings.Contains(w.Body.String(), `content="A`) {
		t.Errorf("/ doesn't carry the escaped app name; body: %s", w.Body.String())
	}

	w = serve(router, http.MethodGet, "/manifest.json")
	var manifest struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &manifest); err != nil {
		t.Fatalf("manifest.json is not valid JSON: %v\n%s", err, w.Body.String())
	}
	if manifest.Name != name || manifest.Description != name {
		t.Errorf("manifest.json name/description = %q, %q; want %q", manifest.Name, manifest.Description, name)
	}
}

func TestParseFlagsInvalidTrustedProxiesFails(t *testing.T) {
	withArgs(t, "-trustedproxies", "10.0.0.1,traefik")

	_, _, _, err := parseFlags(models.ConfigStruct{PoenskelistenTrustedProxies: "10.0.0.1"})
	if err == nil || !strings.Contains(err.Error(), "trustedproxies") {
		t.Errorf("parseFlags error = %v, want an invalid -trustedproxies error", err)
	}
}

// clientIPAs returns the client address the router sees for a request from
// remoteAddr carrying the given X-Forwarded-For header.
func clientIPAs(router *gin.Engine, remoteAddr string, forwardedFor string) string {
	w := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/test-client-ip", nil)
	request.RemoteAddr = remoteAddr
	if forwardedFor != "" {
		request.Header.Set("X-Forwarded-For", forwardedFor)
		request.Header.Set("X-Real-IP", "192.0.2.99")
	}
	router.ServeHTTP(w, request)
	return w.Body.String()
}

func TestInitRouterClientIPHonorsOnlyTrustedProxies(t *testing.T) {
	echo := func(ctx *gin.Context) { ctx.String(http.StatusOK, ctx.ClientIP()) }

	untrusting := initRouter(models.ConfigStruct{})
	untrusting.GET("/test-client-ip", echo)
	trusting := initRouter(models.ConfigStruct{PoenskelistenTrustedProxies: "10.244.0.0/16"})
	trusting.GET("/test-client-ip", echo)

	cases := []struct {
		name       string
		router     *gin.Engine
		remoteAddr string
		forwarded  string
		want       string
	}{
		{"no proxy trusted: header ignored", untrusting, "10.244.1.5:4000", "203.0.113.7", "10.244.1.5"},
		{"no header: peer address", trusting, "10.244.1.5:4000", "", "10.244.1.5"},
		{"trusted proxy: forwarded client", trusting, "10.244.1.5:4000", "203.0.113.7", "203.0.113.7"},
		{"untrusted peer: header ignored", trusting, "198.51.100.9:4000", "203.0.113.7", "198.51.100.9"},
		// A proxy appends the address it saw, so entries a client wrote itself
		// sit to the left of it and are never reached.
		{"spoofed entries before the real one", trusting, "10.244.1.5:4000", "1.1.1.1, 203.0.113.7", "203.0.113.7"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := clientIPAs(c.router, c.remoteAddr, c.forwarded); got != c.want {
				t.Errorf("client IP = %q, want %q", got, c.want)
			}
		})
	}
}

func TestParseFlagsInvalidRateLimitFails(t *testing.T) {
	withArgs(t, "-ratelimitoauth", "sixty")

	_, _, _, err := parseFlags(models.ConfigStruct{})
	if err == nil || !strings.Contains(err.Error(), "ratelimitoauth") {
		t.Errorf("parseFlags error = %v, want an invalid -ratelimitoauth error", err)
	}
}

// postFrom sends a POST with the given body from remoteAddr and returns the status.
func postFrom(router *gin.Engine, path string, remoteAddr string, contentType string, body string) int {
	w := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", contentType)
	request.RemoteAddr = remoteAddr
	router.ServeHTTP(w, request)
	return w.Code
}

// The password and code entry points share one budget per address, and the
// OAuth endpoints another. Requests are malformed so the handlers stop before
// touching the database: the limit runs first either way.
func TestInitRouterRateLimitsLoginAndOAuth(t *testing.T) {
	router := initRouter(models.ConfigStruct{RateLimitLogin: "2/1m", RateLimitOAuth: "1/1m"})
	const client, other = "203.0.113.10:5000", "203.0.113.11:5000"
	jsonType, formType := "application/json", "application/x-www-form-urlencoded"

	codes := []int{
		postFrom(router, "/api/open/tokens/register", client, jsonType, "{"),
		postFrom(router, "/api/open/tokens/mfa", client, jsonType, "{"),
		postFrom(router, "/api/open/users/reset", client, jsonType, "{"),
	}
	if codes[0] == http.StatusTooManyRequests || codes[1] == http.StatusTooManyRequests || codes[2] != http.StatusTooManyRequests {
		t.Errorf("login, MFA, reset codes = %v, want the third refused with 429", codes)
	}
	if code := postFrom(router, "/api/open/tokens/register", other, jsonType, "{"); code == http.StatusTooManyRequests {
		t.Error("another address shouldn't share the budget")
	}

	if code := postFrom(router, "/oauth/token", client, formType, "grant_type=nope"); code != http.StatusBadRequest {
		t.Errorf("first token request = %d, want 400 (unsupported grant)", code)
	}
	if code := postFrom(router, "/oauth/revoke", client, formType, ""); code != http.StatusTooManyRequests {
		t.Errorf("second OAuth request = %d, want 429", code)
	}
}

func TestInitRouterRateLimitsCanBeTurnedOff(t *testing.T) {
	router := initRouter(models.ConfigStruct{RateLimitLogin: "off"})
	for i := 0; i < 30; i++ {
		if code := postFrom(router, "/api/open/tokens/register", "203.0.113.12:5000", "application/json", "{"); code == http.StatusTooManyRequests {
			t.Fatalf("request %d refused with the login limit off", i+1)
		}
	}
}

// The headers come from a global middleware, so they are on pages, the API
// and the OAuth endpoints alike.
func TestInitRouterSetsSecurityHeaders(t *testing.T) {
	router := initRouter(models.ConfigStruct{})
	for _, path := range []string{"/login", "/api/auth/me", "/.well-known/oauth-authorization-server", "/no-such-page"} {
		w := serve(router, http.MethodGet, path)
		if w.Header().Get("X-Frame-Options") != "DENY" || w.Header().Get("X-Content-Type-Options") != "nosniff" ||
			w.Header().Get("Content-Security-Policy") != middlewares.ContentSecurityPolicy || w.Header().Get("Referrer-Policy") != "same-origin" {
			t.Errorf("GET %s headers = %v, want the security headers", path, w.Header())
		}
	}
}

func loadDictionaries(t *testing.T) map[string]map[string]string {
	t.Helper()
	paths, err := filepath.Glob("web/json/lang-*.json")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no dictionaries found (err %v)", err)
	}
	dicts := map[string]map[string]string{}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		dict := map[string]string{}
		if err := json.Unmarshal(raw, &dict); err != nil {
			t.Fatalf("%s is not a flat JSON object of strings: %v", path, err)
		}
		lang := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(path), "lang-"), ".json")
		dicts[lang] = dict
	}
	return dicts
}

var dictionaryPlaceholder = regexp.MustCompile(`\{(\w+)\}`)

func placeholders(text string) string {
	found := []string{}
	for _, match := range dictionaryPlaceholder.FindAllStringSubmatch(text, -1) {
		found = append(found, match[1])
	}
	sort.Strings(found)
	return strings.Join(found, ",")
}

// Every language must translate every key, with the same {placeholders}, so
// switching language never shows a raw key or drops a name from a sentence.
func TestFrontendDictionariesMatch(t *testing.T) {
	dicts := loadDictionaries(t)
	reference, ok := dicts["en"]
	if !ok || len(dicts) < 2 {
		t.Fatalf("want English plus at least one other language, got %d dictionaries", len(dicts))
	}
	for lang, dict := range dicts {
		for key, english := range reference {
			text, found := dict[key]
			if !found {
				t.Errorf("%s: missing key %q", lang, key)
				continue
			}
			if strings.TrimSpace(text) == "" {
				t.Errorf("%s: empty translation for %q", lang, key)
			}
			if placeholders(text) != placeholders(english) {
				t.Errorf("%s: %q has placeholders {%s}, English has {%s}", lang, key, placeholders(text), placeholders(english))
			}
		}
		for key := range dict {
			if _, found := reference[key]; !found {
				t.Errorf("%s: key %q is not in the English dictionary", lang, key)
			}
		}
	}
}

var (
	translationCall  = regexp.MustCompile(`P\.(t|tn)\(\s*"([A-Za-z0-9_.]+)"`)
	translationTitle = regexp.MustCompile(`title:\s*"([a-z][A-Za-z0-9]*\.[A-Za-z0-9_.]+)"`)
	translationText  = regexp.MustCompile(`data-i18n="([A-Za-z0-9_.]+)"`)
	translationAttrs = regexp.MustCompile(`data-i18n-attr="([^"]+)"`)
)

// Every key the pages ask for must exist, so no raw key reaches the screen.
func TestFrontendTranslationKeysExist(t *testing.T) {
	dict := loadDictionaries(t)["en"]
	files := []string{}
	for _, pattern := range []string{"web/js/*.js", "web/html/*.html", "web/templates/*.html"} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, matches...)
	}
	used := 0
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		source := string(raw)
		keys := []string{}
		for _, match := range translationCall.FindAllStringSubmatch(source, -1) {
			if match[1] == "tn" {
				keys = append(keys, match[2]+".one", match[2]+".other")
			} else {
				keys = append(keys, match[2])
			}
		}
		for _, match := range translationTitle.FindAllStringSubmatch(source, -1) {
			keys = append(keys, match[1])
		}
		for _, match := range translationText.FindAllStringSubmatch(source, -1) {
			keys = append(keys, match[1])
		}
		for _, match := range translationAttrs.FindAllStringSubmatch(source, -1) {
			for _, pair := range strings.Split(match[1], ";") {
				if _, key, found := strings.Cut(pair, ":"); found {
					keys = append(keys, strings.TrimSpace(key))
				}
			}
		}
		for _, key := range keys {
			used++
			if _, found := dict[key]; !found {
				t.Errorf("%s uses translation key %q, which the dictionaries lack", file, key)
			}
		}
	}
	if used == 0 {
		t.Fatal("found no translation keys at all; the patterns are out of date")
	}
}

// The application name is free text from the operator.
func TestFrontendPagesShowEscapedAppName(t *testing.T) {
	name := `Ma "liste" <b>&</b>`
	router := initRouter(models.ConfigStruct{PoenskelistenName: name})
	for _, path := range []string{"/login", "/login/mfa", "/register", "/verify", "/enroll", "/oauth/callback",
		"/wishlists", "/wishlists/abc", "/wishlists/new", "/wishlists/abc/edit", "/wishlists/public/abc", "/wishes/new", "/wishes/abc/edit", "/groups", "/groups/abc", "/account", "/admin", "/", "/offline"} {
		w := serve(router, http.MethodGet, path)
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", path, w.Code)
		}
		body := w.Body.String()
		if strings.Contains(body, "<b>&</b>") {
			t.Errorf("GET %s contains the raw app name", path)
		}
		if !strings.Contains(body, `<meta name="application-name" content="Ma &#34;liste&#34; &lt;b&gt;&amp;&lt;/b&gt;">`) {
			t.Errorf("GET %s lacks the escaped application-name meta tag", path)
		}
		if !strings.Contains(body, "<title>Ma &#34;liste&#34; &lt;b&gt;&amp;&lt;/b&gt;</title>") {
			t.Errorf("GET %s lacks the escaped title", path)
		}
		if !strings.Contains(body, `<script defer src="/js/core.js"></script>`) {
			t.Errorf("GET %s doesn't load the shared scripts", path)
		}
	}
}

// The service worker fails to install if one of the files it precaches is
// missing, and then there is no offline page at all.
func TestServiceWorkerPrecachesServedFiles(t *testing.T) {
	raw, err := os.ReadFile("web/js/service-worker.js")
	if err != nil {
		t.Fatal(err)
	}
	list := regexp.MustCompile(`(?s)const urlsToCache = \[(.*?)\];`).FindSubmatch(raw)
	if list == nil {
		t.Fatal("urlsToCache not found in service-worker.js")
	}
	paths := regexp.MustCompile(`'([^']+)'|OFFLINE_URL`).FindAllSubmatch(list[1], -1)
	if len(paths) < 2 {
		t.Fatalf("found %d precached paths, want the offline page and its files", len(paths))
	}
	router := initRouter(models.ConfigStruct{})
	for _, match := range paths {
		path := string(match[1])
		if path == "" {
			path = "/offline"
		}
		if w := serve(router, http.MethodGet, path); w.Code != http.StatusOK {
			t.Errorf("service worker precaches %s, which answers %d", path, w.Code)
		}
	}
}

func frontendFiles(t *testing.T, patterns ...string) []string {
	t.Helper()
	files := []string{}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, matches...)
	}
	if len(files) == 0 {
		t.Fatal("no front-end files found")
	}
	return files
}

// Nothing is loaded from another site: no CDN, web font or tracker, so the
// pages work offline and leak nothing about their visitors. The SVG
// namespace is an identifier, not a request.
func TestFrontendLoadsNothingFromOtherSites(t *testing.T) {
	remote := regexp.MustCompile(`(?i)(https?:)?//[a-z0-9][a-z0-9.-]*\.[a-z]{2,}`)
	for _, file := range frontendFiles(t, "web/html/*.html", "web/templates/*.html", "web/css/*.css", "web/js/*.js", "web/json/*.json") {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range remote.FindAllString(string(raw), -1) {
			if strings.HasSuffix(match, "www.w3.org") {
				continue
			}
			t.Errorf("%s refers to another site: %s", file, match)
		}
	}
}

// The pages carry no inline script, event handler or style, and the scripts
// never inject markup: every user value goes through textContent (core.js's
// P.h). This is also what lets a strict Content-Security-Policy work.
func TestFrontendHasNoInlineCodeOrMarkupInjection(t *testing.T) {
	inlineScript := regexp.MustCompile(`(?i)<script(?:\s[^>]*)?>\s*[^<\s]`)
	scriptTag := regexp.MustCompile(`(?i)<script\b[^>]*>`)
	handler := regexp.MustCompile(`(?i)\son[a-z]+\s*=`)
	style := regexp.MustCompile(`(?i)<style\b|\sstyle\s*=`)
	for _, file := range frontendFiles(t, "web/html/*.html", "web/templates/*.html") {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		source := string(raw)
		if inlineScript.MatchString(source) {
			t.Errorf("%s has an inline script", file)
		}
		for _, tag := range scriptTag.FindAllString(source, -1) {
			if !strings.Contains(tag, ` src="/js/`) {
				t.Errorf("%s has a script without a same-origin src: %s", file, tag)
			}
		}
		if handler.MatchString(source) {
			t.Errorf("%s has an inline event handler", file)
		}
		if style.MatchString(source) {
			t.Errorf("%s has inline styles", file)
		}
	}

	injection := regexp.MustCompile(`\.(innerHTML|outerHTML)\s*[+]?=|insertAdjacentHTML|document\.write|\beval\(|new Function\(|\.setAttribute\(\s*["']on`)
	for _, file := range frontendFiles(t, "web/js/*.js") {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if match := injection.FindString(string(raw)); match != "" {
			t.Errorf("%s uses %q; build elements with P.h instead", file, match)
		}
	}
}
