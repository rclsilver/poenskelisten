package main

import (
	"aunefyren/poenskelisten/auth"
	"aunefyren/poenskelisten/config"
	"aunefyren/poenskelisten/controllers"
	"aunefyren/poenskelisten/database"
	"aunefyren/poenskelisten/logger"
	"aunefyren/poenskelisten/middlewares"
	"aunefyren/poenskelisten/models"
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"flag"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	textTemplate "text/template"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sirupsen/logrus"
	logrusTest "github.com/sirupsen/logrus/hooks/test"
	"golang.org/x/crypto/bcrypt"
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
		"-defaultlanguage", " FR ",
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
		"-ratelimitupload", "6/5m",
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
		DefaultLanguage:             "fr",
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
		RateLimitUpload:             "6/5m",
	}
	if out != want {
		t.Errorf("parsed config mismatch:\n got %+v\nwant %+v", out, want)
	}
	if logger.Log.GetLevel() != logrus.DebugLevel {
		t.Errorf("logger level = %v, want debug", logger.Log.GetLevel())
	}
}

// A language the app isn't translated into stops startup, rather than
// leaving pages in a language nobody chose.
func TestParseFlagsDefaultLanguage(t *testing.T) {
	withArgs(t, "-defaultlanguage", "auto")
	out, _, _, err := parseFlags(models.ConfigStruct{DefaultLanguage: "fr"})
	if err != nil || out.DefaultLanguage != "" {
		t.Errorf("auto = %q (error %v), want \"\" to follow the browser", out.DefaultLanguage, err)
	}

	withArgs(t, "-defaultlanguage", "de")
	_, _, _, err = parseFlags(models.ConfigStruct{DefaultLanguage: "fr"})
	if err == nil || !strings.Contains(err.Error(), "defaultlanguage") || !strings.Contains(err.Error(), "en, fr or auto") {
		t.Errorf("parseFlags error = %v, want an invalid -defaultlanguage error listing the choices", err)
	}
}

func TestParseFlagsInvalidAdditionalURLsFails(t *testing.T) {
	withArgs(t, "-additionalurls", "http://wish.lan,192.168.1.10:8080")

	_, _, _, err := parseFlags(models.ConfigStruct{PoenskelistenAdditionalURLs: "http://kept.lan"})
	if err == nil || !strings.Contains(err.Error(), "additionalurls") {
		t.Errorf("parseFlags error = %v, want an invalid -additionalurls error", err)
	}
}

// The secrets come from the environment, which entrypoint.sh no longer
// copies into the process's arguments; a flag given explicitly wins.
func TestParseFlagsReadsSecretsFromTheEnvironment(t *testing.T) {
	t.Setenv("dbpassword", "db-from-env")
	t.Setenv("smtppassword", "smtp-from-env")
	t.Setenv("oidcclientsecret", "oidc-from-env")

	withArgs(t)
	out, _, provided, err := parseFlags(models.ConfigStruct{DBPassword: "db-from-config"})
	if err != nil {
		t.Fatalf("parseFlags returned error: %v", err)
	}
	if out.DBPassword != "db-from-env" || out.SMTPPassword != "smtp-from-env" || out.OIDCClientSecret != "oidc-from-env" || !provided {
		t.Errorf("secrets = %q %q %q (provided %v), want the environment's, to be saved", out.DBPassword, out.SMTPPassword, out.OIDCClientSecret, provided)
	}

	withArgs(t, "-dbpassword", "db-from-flag")
	out, _, _, err = parseFlags(models.ConfigStruct{})
	if err != nil {
		t.Fatalf("parseFlags returned error: %v", err)
	}
	if out.DBPassword != "db-from-flag" || out.SMTPPassword != "smtp-from-env" {
		t.Errorf("secrets = %q %q, want the flag's, then the environment's", out.DBPassword, out.SMTPPassword)
	}
}

// Any user who can see a process can read its arguments, so entrypoint.sh
// must leave the secrets in the environment.
func TestEntrypointKeepsSecretsOutOfArguments(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("as root, entrypoint.sh changes file owners and switches user")
	}
	script, err := os.ReadFile("entrypoint.sh")
	if err != nil {
		t.Fatalf("failed to read entrypoint.sh: %v", err)
	}
	printsArguments := strings.Replace(string(script), "set -- /app/poenskelisten", "set -- echo", 1)
	path := filepath.Join(t.TempDir(), "entrypoint.sh")
	if err := os.WriteFile(path, []byte(printsArguments), 0o700); err != nil {
		t.Fatalf("failed to write the script: %v", err)
	}

	cmd := exec.Command("sh", path, "--port", "9000")
	cmd.Env = append(os.Environ(), "dbusername=wishlist", "dbpassword=db-secret", "smtppassword=smtp-secret", "oidcclientsecret=oidc-secret")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("entrypoint.sh failed: %v", err)
	}
	arguments := string(out)
	if !strings.Contains(arguments, "--dbusername wishlist") || !strings.HasSuffix(strings.TrimSpace(arguments), "--port 9000") {
		t.Fatalf("arguments = %q, want the environment as flags, then the container's", arguments)
	}
	if strings.Contains(arguments, "secret") {
		t.Errorf("arguments = %q, want no secret", arguments)
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

// Every page tells the web app the instance's default language, empty when
// each browser decides; a wrong value from a hand-edited config.json too.
func TestInitRouterTellsPagesTheDefaultLanguage(t *testing.T) {
	for configured, want := range map[string]string{"fr": "fr", "": "", "de": ""} {
		router := initRouter(models.ConfigStruct{DefaultLanguage: configured})
		for _, page := range []string{"/login", "/"} {
			body := serve(router, http.MethodGet, page).Body.String()
			if !strings.Contains(body, `<meta name="default-language" content="`+want+`">`) {
				t.Errorf("default_language %q: GET %s lacks the meta tag with %q", configured, page, want)
			}
		}
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

// Creating and editing wishes and updating one's account can carry an image,
// so they share one budget per account, whatever address it comes from.
func TestInitRouterRateLimitsUploadsPerAccount(t *testing.T) {
	_, token := setupRouterTest(t)
	config.ConfigFile.RateLimitUpload = "2/1m"
	router := initRouter(config.ConfigFile)

	now := time.Now()
	lists := map[string]models.Wishlist{}
	tokens := map[string]string{}
	for _, name := range []string{"Alice", "Bob"} {
		user := createRouterUser(t, name, false)
		list := models.Wishlist{Name: name + " list", Enabled: true, OwnerID: user.ID, Date: &now}
		list.ID = uuid.New()
		createRouterRecord(t, &list)
		lists[name], tokens[name] = list, token(user.ID, false)
	}
	send := func(name, path, body string) int {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.RemoteAddr = "203.0.113.20:5000"
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", tokens[name])
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w.Code
	}

	aliceWishes := "/api/auth/wishes?wishlist=" + lists["Alice"].ID.String()
	codes := []int{
		send("Alice", aliceWishes, `{"name":"First gift"}`),
		send("Alice", aliceWishes, `{"name":"Second gift"}`),
		send("Alice", aliceWishes, `{"name":"Third gift"}`),
		send("Alice", "/api/auth/users/update", `{}`),
	}
	if codes[0] != http.StatusCreated || codes[1] != http.StatusCreated || codes[2] != http.StatusTooManyRequests || codes[3] != http.StatusTooManyRequests {
		t.Errorf("Alice's codes = %v, want [201 201 429 429]", codes)
	}
	if code := send("Bob", "/api/auth/wishes?wishlist="+lists["Bob"].ID.String(), `{"name":"First gift"}`); code != http.StatusCreated {
		t.Errorf("Bob from the same address = %d, want 201 with his own budget", code)
	}
}

// bearerTransport adds an access token to every request it sends.
type bearerTransport struct{ token string }

func (b bearerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	request.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(request)
}

// mcpSession connects to the router's /mcp as userID with the given scopes.
func mcpSession(t *testing.T, router *gin.Engine, userID uuid.UUID, scopes string) *sdkmcp.ClientSession {
	t.Helper()
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	token, err := auth.GenerateOAuthAccessToken(userID, "mcp-client", config.MCPResource(), scopes, false, true)
	if err != nil {
		t.Fatalf("failed to sign MCP token: %v", err)
	}
	transport := &sdkmcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: bearerTransport{token}}}
	session, err := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil).Connect(context.Background(), transport, nil)
	if err != nil {
		t.Fatalf("failed to connect to /mcp: %v", err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

// An image sent through MCP is an upload like one from the web app: both
// count against the account's one rate_limit_upload budget, and MCP calls
// without an image don't.
func TestInitRouterCountsMCPImagesInTheUploadBudget(t *testing.T) {
	_, token := setupRouterTest(t)
	config.ConfigFile.RateLimitUpload = "3/1m"
	config.ConfigFile.MCPEnabled = true
	t.Cleanup(controllers.UseWishImageDir(t.TempDir()))
	router := initRouter(config.ConfigFile)

	user := createRouterUser(t, "Alice", false)
	now := time.Now()
	list := models.Wishlist{Name: "Alice list", Enabled: true, OwnerID: user.ID, Date: &now}
	list.ID = uuid.New()
	createRouterRecord(t, &list)
	wish := models.Wish{Name: "Headphones", Enabled: true, OwnerID: user.ID, WishlistID: list.ID}
	wish.ID = uuid.New()
	createRouterRecord(t, &wish)

	var picture bytes.Buffer
	if err := png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatalf("failed to encode test image: %v", err)
	}
	dataURI := "data:image/png;base64," + base64.StdEncoding.EncodeToString(picture.Bytes())

	webWishes := 0
	createOnWeb := func() int {
		webWishes++
		body := `{"name":"Web gift ` + strconv.Itoa(webWishes) + `"}`
		request := httptest.NewRequest(http.MethodPost, "/api/auth/wishes?wishlist="+list.ID.String(), strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", token(user.ID, false))
		w := httptest.NewRecorder()
		router.ServeHTTP(w, request)
		return w.Code
	}
	session := mcpSession(t, router, user.ID, "mcp:wishlists.read mcp:wishlists.write")
	updateOnMCP := func(args map[string]any) string {
		args["wish_id"] = wish.ID.String()
		result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{Name: "update_wish", Arguments: args})
		if err != nil {
			t.Fatalf("update_wish failed: %v", err)
		}
		if !result.IsError {
			return ""
		}
		return result.Content[0].(*sdkmcp.TextContent).Text
	}

	if a, b := createOnWeb(), createOnWeb(); a != http.StatusCreated || b != http.StatusCreated {
		t.Fatalf("web uploads = %d %d, want 201 201", a, b)
	}
	if refusal := updateOnMCP(map[string]any{"image": dataURI}); refusal != "" {
		t.Fatalf("third upload, through MCP, refused: %s", refusal)
	}
	if refusal := updateOnMCP(map[string]any{"image": dataURI}); refusal != middlewares.TooManyUploads {
		t.Errorf("fourth upload, through MCP = %q, want %q", refusal, middlewares.TooManyUploads)
	}
	if code := createOnWeb(); code != http.StatusTooManyRequests {
		t.Errorf("fifth upload, on the web = %d, want 429", code)
	}
	if refusal := updateOnMCP(map[string]any{"name": "Wireless headphones"}); refusal != "" {
		t.Errorf("an MCP edit without an image was refused: %s", refusal)
	}
}

// Only a request with a valid MCP token may send /mcp a body large enough for
// an image; any other is held to the usual limit before anything reads it.
func TestInitRouterLimitsMCPBodiesWithoutAToken(t *testing.T) {
	_, token := setupRouterTest(t)
	config.ConfigFile.MCPEnabled = true
	router := initRouter(config.ConfigFile)
	user := createRouterUser(t, "Alice", false)
	mcpToken, err := auth.GenerateOAuthAccessToken(user.ID, "mcp-client", config.MCPResource(), "mcp:wishlists.read", false, true)
	if err != nil {
		t.Fatalf("failed to sign MCP token: %v", err)
	}

	body := strings.Repeat("x", int(middlewares.DefaultMaxBodyBytes)+1)
	post := func(authorization string) int {
		request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json, text/event-stream")
		if authorization != "" {
			request.Header.Set("Authorization", authorization)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, request)
		return w.Code
	}

	for name, authorization := range map[string]string{"no token": "", "a forged token": "Bearer not-a-token", "a web API token": token(user.ID, false)} {
		if code := post(authorization); code != http.StatusRequestEntityTooLarge {
			t.Errorf("%s: status = %d, want 413", name, code)
		}
	}
	if code := post("Bearer " + mcpToken); code == http.StatusRequestEntityTooLarge {
		t.Error("a valid MCP token: the body was refused as too large")
	}
}

// The MCP tools take an image of up to 10 MB, more than the 4 MiB the SDK
// reads by default: one of about 5 MB must go through, and a body over the
// app's own limit must still be refused.
func TestInitRouterTakesLargeMCPImages(t *testing.T) {
	setupRouterTest(t)
	config.ConfigFile.MCPEnabled = true
	t.Cleanup(controllers.UseWishImageDir(t.TempDir()))
	router := initRouter(config.ConfigFile)

	user := createRouterUser(t, "Alice", false)
	now := time.Now()
	list := models.Wishlist{Name: "Alice list", Enabled: true, OwnerID: user.ID, Date: &now}
	list.ID = uuid.New()
	createRouterRecord(t, &list)
	wish := models.Wish{Name: "Headphones", Enabled: true, OwnerID: user.ID, WishlistID: list.ID}
	wish.ID = uuid.New()
	createRouterRecord(t, &wish)

	// Noise, stored uncompressed: about 5 MB of PNG, 1.7 megapixels.
	noise := image.NewNRGBA(image.Rect(0, 0, 1300, 1300))
	if _, err := rand.Read(noise.Pix); err != nil {
		t.Fatalf("failed to make the image: %v", err)
	}
	for i := 3; i < len(noise.Pix); i += 4 {
		noise.Pix[i] = 255
	}
	var picture bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.NoCompression}).Encode(&picture, noise); err != nil {
		t.Fatalf("failed to encode the image: %v", err)
	}
	if picture.Len() < 5_000_000 {
		t.Fatalf("test image is %d bytes, want about 5 MB", picture.Len())
	}
	dataURI := "data:image/png;base64," + base64.StdEncoding.EncodeToString(picture.Bytes())

	session := mcpSession(t, router, user.ID, "mcp:wishlists.read mcp:wishlists.write")
	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{Name: "update_wish", Arguments: map[string]any{"wish_id": wish.ID.String(), "image": dataURI}})
	if err != nil || result.IsError {
		t.Fatalf("a %d-byte image through MCP was refused: %v %+v", picture.Len(), err, result)
	}
	if stored, err := controllers.CheckIfWishImageExists(wish.ID); err != nil || !stored {
		t.Errorf("the image wasn't stored (%v)", err)
	}

	token, err := auth.GenerateOAuthAccessToken(user.ID, "mcp-client", config.MCPResource(), "mcp:wishlists.write", false, true)
	if err != nil {
		t.Fatalf("failed to sign MCP token: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(strings.Repeat("x", int(middlewares.ImageUploadMaxBodyBytes)+1)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, request)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("a body over 15 MB with a valid token = %d, want 413", w.Code)
	}
}

// Decoding a request that carries an image takes about ten copies of its
// body, so a burst of them could exhaust the container's memory. Only two
// large bodies, from the web and MCP alike, are read at a time: a third
// waits for one of them to finish before anything reads it.
func TestInitRouterReadsTwoLargeBodiesAtATime(t *testing.T) {
	_, token := setupRouterTest(t)
	config.ConfigFile.MCPEnabled = true
	router := initRouter(config.ConfigFile)
	user := createRouterUser(t, "Alice", false)
	now := time.Now()
	list := models.Wishlist{Name: "Alice list", Enabled: true, OwnerID: user.ID, Date: &now}
	list.ID = uuid.New()
	createRouterRecord(t, &list)
	mcpToken, err := auth.GenerateOAuthAccessToken(user.ID, "mcp-client", config.MCPResource(), "mcp:wishlists.write", false, true)
	if err != nil {
		t.Fatalf("failed to sign MCP token: %v", err)
	}

	// Each request's body is a pipe: a write to it returns only once the
	// handler reads it.
	var handled sync.WaitGroup
	start := func(path, authorization string) *io.PipeWriter {
		body, write := io.Pipe()
		request := httptest.NewRequest(http.MethodPost, path, body)
		request.ContentLength = 2 << 20
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json, text/event-stream")
		request.Header.Set("Authorization", authorization)
		handled.Add(1)
		go func() {
			defer handled.Done()
			router.ServeHTTP(httptest.NewRecorder(), request)
		}()
		return write
	}
	read := func(write *io.PipeWriter, within time.Duration) bool {
		written := make(chan error, 1)
		go func() {
			_, err := write.Write([]byte("{"))
			written <- err
		}()
		select {
		case err := <-written:
			return err == nil
		case <-time.After(within):
			return false
		}
	}

	first := start("/mcp", "Bearer "+mcpToken)
	second := start("/mcp", "Bearer "+mcpToken)
	if !read(first, 5*time.Second) || !read(second, 5*time.Second) {
		t.Fatal("the first two large bodies weren't read")
	}
	third := start("/api/auth/wishes?wishlist="+list.ID.String(), token(user.ID, false))
	if read(third, 300*time.Millisecond) {
		t.Error("a third large body was read while two others were")
	}

	first.CloseWithError(io.ErrUnexpectedEOF)
	if !read(third, 5*time.Second) {
		t.Error("the third large body wasn't read once a slot freed")
	}
	for _, write := range []*io.PipeWriter{second, third} {
		write.CloseWithError(io.ErrUnexpectedEOF)
	}
	handled.Wait()
}

func TestInitRouterRateLimitsCanBeTurnedOff(t *testing.T) {
	router := initRouter(models.ConfigStruct{RateLimitLogin: "off"})
	for i := 0; i < 30; i++ {
		if code := postFrom(router, "/api/open/tokens/register", "203.0.113.12:5000", "application/json", "{"); code == http.StatusTooManyRequests {
			t.Fatalf("request %d refused with the login limit off", i+1)
		}
	}
}

// The access log goes to stdout, so to the container's logs: whoever reads
// them mustn't find a code there that signs in, resets a password or opens
// a public wishlist. Other IDs are kept, for tracing a request.
func TestInitRouterLogsNoCodes(t *testing.T) {
	var logged strings.Builder
	original := gin.DefaultWriter
	t.Cleanup(func() { gin.DefaultWriter = original })
	gin.DefaultWriter = &logged
	router, _ := setupRouterTest(t)

	listID := uuid.NewString()
	requests := []struct{ method, path string }{
		{http.MethodGet, "/api/open/users/reset/RESET-CODE-SECRET"},
		{http.MethodPost, "/api/open/users/verify/VERIFY-CODE-SECRET"},
		{http.MethodGet, "/api/open/wishlists/public/PUBLIC-HASH-SECRET"},
		{http.MethodGet, "/wishlists/public/PUBLIC-PAGE-SECRET"},
		{http.MethodGet, "/login?reset_code=LINK-CODE-SECRET"},
		{http.MethodGet, "/oauth/callback?code=OAUTH-CODE-SECRET&state=STATE-SECRET"},
		{http.MethodGet, "/api/open/oidc/callback?code=OIDC-CODE-SECRET&state=OIDC-STATE-SECRET"},
		{http.MethodGet, "/api/auth/wishlists/" + listID},
	}
	for _, request := range requests {
		serve(router, request.method, request.path)
	}

	output := logged.String()
	if strings.Contains(output, "SECRET") {
		t.Errorf("the access log holds a code:\n%s", output)
	}
	for _, want := range []string{`"/api/open/users/reset/:resetCode"`, `"/wishlists/public/:wishlist_hash"`, `"/login"`, `"/api/auth/wishlists/` + listID + `"`} {
		if !strings.Contains(output, want) {
			t.Errorf("the access log lacks %s:\n%s", want, output)
		}
	}
	if lines := strings.Count(output, "[GIN] "); lines != len(requests) {
		t.Errorf("%d access log lines, want one per request (%d):\n%s", lines, len(requests), output)
	}
}

// A client sending or reading slowly mustn't keep its connection open for as
// long as it likes, but the largest image request must still have time to
// upload over a slow line.
func TestNewServerBoundsSlowClients(t *testing.T) {
	server := newServer(8080, http.NotFoundHandler())
	if server.Addr != ":8080" || server.Handler == nil {
		t.Fatalf("server = %+v, want it on :8080 with the handler", server)
	}
	if server.ReadHeaderTimeout <= 0 || server.ReadHeaderTimeout > 30*time.Second {
		t.Errorf("ReadHeaderTimeout = %v, want a few seconds", server.ReadHeaderTimeout)
	}
	slowestUpload := time.Duration(middlewares.ImageUploadMaxBodyBytes*8/700_000) * time.Second
	if server.ReadTimeout < slowestUpload || server.ReadTimeout > 5*time.Minute {
		t.Errorf("ReadTimeout = %v, want between %v and 5m", server.ReadTimeout, slowestUpload)
	}
	if server.WriteTimeout <= server.ReadTimeout || server.WriteTimeout > 5*time.Minute {
		t.Errorf("WriteTimeout = %v, want longer than ReadTimeout (%v), at most 5m", server.WriteTimeout, server.ReadTimeout)
	}
	if server.IdleTimeout <= 0 || server.IdleTimeout > 5*time.Minute {
		t.Errorf("IdleTimeout = %v, want a few minutes", server.IdleTimeout)
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

// A page of another site can make a visitor's browser post the attacker's
// password to the sign-in endpoint, the JSON in a text/plain body so no
// preflight stops it, and leave the visitor signed in to the attacker's
// account. The sign-in, MFA and consent entry points take requests only from
// Pønskelisten's own pages, which keep signing in.
func TestInitRouterRefusesSignInFromOtherSites(t *testing.T) {
	setupRouterTest(t)
	key, err := config.GenerateSecureKey(64)
	if err != nil {
		t.Fatalf("failed to generate private key: %v", err)
	}
	config.ConfigFile.PrivateKey = key
	router := initRouter(config.ConfigFile)
	cost := models.BcryptCost
	t.Cleanup(func() { models.BcryptCost = cost })
	models.BcryptCost = bcrypt.MinCost

	email := "mallory-" + uuid.NewString() + "@example.com"
	enabled, mfa := true, false
	user := models.User{FirstName: "Mallory", LastName: "Test", Email: &email, Password: new(string), Enabled: &enabled, Verified: &enabled, MFAEnabled: &mfa}
	user.ID = uuid.New()
	if err := user.HashPassword("correct horse battery"); err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}
	createRouterRecord(t, &user)

	post := func(path, origin, fetchSite string) *httptest.ResponseRecorder {
		body := `{"email":"` + email + `","password":"correct horse battery"}`
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "text/plain;charset=UTF-8")
		req.Header.Set("Origin", origin)
		req.Header.Set("Sec-Fetch-Site", fetchSite)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	for _, path := range []string{
		"/api/open/tokens/register",
		"/api/open/tokens/mfa",
		"/api/open/users/mfa/enroll",
		"/api/open/users/mfa/activate",
		"/api/open/users/verify/123456",
		"/oauth/consent",
	} {
		w := post(path, "https://evil.example", "cross-site")
		if w.Code != http.StatusForbidden || w.Header().Get("Set-Cookie") != "" {
			t.Errorf("POST %s from another site = %d, Set-Cookie %q, want 403 and no cookie", path, w.Code, w.Header().Get("Set-Cookie"))
		}
	}

	w := post("/api/open/tokens/register", "https://wishlist.example.com", "same-origin")
	if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Set-Cookie"), "poenskelisten_sso=") {
		t.Errorf("sign-in from the app = %d, Set-Cookie %q, want 200 and the sign-in cookie", w.Code, w.Header().Get("Set-Cookie"))
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

// routerTestModels is the full schema, for tests that run requests through
// the real router.
var routerTestModels = []interface{}{
	&models.User{}, &models.Invite{}, &models.Group{}, &models.GroupMembership{},
	&models.Wishlist{}, &models.WishlistMembership{}, &models.WishlistCollaborator{},
	&models.WishCategory{}, &models.Wish{}, &models.WishClaim{}, &models.News{},
	&models.MFARecoveryCode{}, &models.Session{}, &models.OAuthClient{},
	&models.AuthorizationCode{}, &models.OAuthConsent{},
}

// setupRouterTest gives the real router a fresh database and an OAuth signing
// key, and returns a function minting first-party access tokens.
func setupRouterTest(t *testing.T) (*gin.Engine, func(userID uuid.UUID, admin bool) string) {
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
	if err := instance.AutoMigrate(routerTestModels...); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	origInstance, origConfig := database.Instance, config.ConfigFile
	t.Cleanup(func() { database.Instance, config.ConfigFile = origInstance, origConfig })
	database.Instance = instance

	keyPEM, kid, err := config.GenerateOAuthSigningKey(config.OAuthAlgES256)
	if err != nil {
		t.Fatalf("failed to generate signing key: %v", err)
	}
	config.ConfigFile.PoenskelistenExternalURL = "https://wishlist.example.com"
	config.ConfigFile.OAuthSigningKey = keyPEM
	config.ConfigFile.OAuthSigningKeyID = kid
	config.ConfigFile.RateLimitLogin = "off"

	token := func(userID uuid.UUID, admin bool) string {
		signed, err := auth.GenerateOAuthAccessToken(userID, models.FirstPartyClientID, config.APIResource(), "openid profile email", admin, true)
		if err != nil {
			t.Fatalf("failed to sign access token: %v", err)
		}
		return "Bearer " + signed
	}
	return initRouter(config.ConfigFile), token
}

// snapshotRouterTables dumps every row of every table, deleted ones included,
// so a test can check that nothing at all was written.
func snapshotRouterTables(t *testing.T) string {
	t.Helper()
	dump := map[string][]map[string]interface{}{}
	for _, model := range routerTestModels {
		statement := &gorm.Statement{DB: database.Instance}
		if err := statement.Parse(model); err != nil {
			t.Fatalf("failed to parse model: %v", err)
		}
		rows := []map[string]interface{}{}
		if err := database.Instance.Table(statement.Schema.Table).Order("id").Find(&rows).Error; err != nil {
			t.Fatalf("failed to read %s: %v", statement.Schema.Table, err)
		}
		dump[statement.Schema.Table] = rows
	}
	raw, err := json.Marshal(dump)
	if err != nil {
		t.Fatalf("failed to encode snapshot: %v", err)
	}
	return string(raw)
}

func createRouterRecord(t *testing.T, record interface{}) {
	t.Helper()
	if err := database.Instance.Create(record).Error; err != nil {
		t.Fatalf("failed to create %T: %v", record, err)
	}
}

func createRouterUser(t *testing.T, name string, admin bool) models.User {
	t.Helper()
	email := strings.ToLower(name) + "-" + uuid.NewString() + "@example.com"
	enabled, mfa := true, true
	user := models.User{FirstName: name, LastName: "Test", Email: &email, Enabled: &enabled, Verified: &enabled, MFAEnabled: &mfa, Admin: admin}
	user.ID = uuid.New()
	createRouterRecord(t, &user)
	return user
}

// The nil UUID once dropped the ID from GORM's struct conditions, so any
// account that owned one wishlist or group could read everyone's wishlists
// and claims, rename every group or unshare other people's wishlists, and an
// admin clearing one account's MFA cleared it for all. Every route must now
// refuse it, wherever the request carries it, without answering with anyone's
// data and without writing anything.
func TestRoutesRefuseNilID(t *testing.T) {
	router, token := setupRouterTest(t)
	now := time.Now()
	truth := true

	admin := createRouterUser(t, "Alice", true)
	victim := createRouterUser(t, "Victim", false)
	claimer := createRouterUser(t, "Claimer", false)
	attacker := createRouterUser(t, "Erin", false)

	victimGroup := models.Group{Name: "Victim group", Description: "Victim group", Enabled: true, OwnerID: victim.ID}
	victimGroup.ID = uuid.New()
	createRouterRecord(t, &victimGroup)
	victimList := models.Wishlist{Name: "Victim list", Enabled: true, OwnerID: victim.ID, Date: &now, Claimable: &truth}
	victimList.ID = uuid.New()
	createRouterRecord(t, &victimList)
	publicList := models.Wishlist{Name: "Victim public list", Enabled: true, OwnerID: victim.ID, Date: &now, Public: &truth, PublicHash: uuid.New()}
	publicList.ID = uuid.New()
	createRouterRecord(t, &publicList)
	victimWish := models.Wish{Name: "Victim wish", Enabled: true, OwnerID: victim.ID, WishlistID: victimList.ID}
	victimWish.ID = uuid.New()
	createRouterRecord(t, &victimWish)
	victimCategory := models.WishCategory{Name: "Victim category", Enabled: true, OwnerID: victim.ID, WishlistID: victimList.ID}
	victimCategory.ID = uuid.New()
	createRouterRecord(t, &victimCategory)
	for _, memberID := range []uuid.UUID{victim.ID, claimer.ID} {
		membership := models.GroupMembership{GroupID: victimGroup.ID, MemberID: memberID, Enabled: true}
		membership.ID = uuid.New()
		createRouterRecord(t, &membership)
	}
	share := models.WishlistMembership{GroupID: victimGroup.ID, WishlistID: victimList.ID, Enabled: true}
	share.ID = uuid.New()
	createRouterRecord(t, &share)
	claim := models.WishClaim{WishID: victimWish.ID, UserID: claimer.ID, Enabled: true}
	claim.ID = uuid.New()
	createRouterRecord(t, &claim)
	collaborator := models.WishlistCollaborator{WishlistID: victimList.ID, UserID: claimer.ID, Enabled: true}
	collaborator.ID = uuid.New()
	createRouterRecord(t, &collaborator)
	for _, user := range []models.User{victim, claimer} {
		code := models.MFARecoveryCode{UserID: user.ID, CodeHash: "hash"}
		code.ID = uuid.New()
		createRouterRecord(t, &code)
		session := models.Session{UserID: user.ID, RefreshTokenHash: uuid.NewString(), Kind: models.SessionKindWebRefresh, IssuedAt: now, ExpiresAt: now.Add(time.Hour)}
		session.ID = uuid.New()
		createRouterRecord(t, &session)
	}
	invite := models.Invite{Code: "INVITECODE", Enabled: &truth}
	invite.ID = uuid.New()
	createRouterRecord(t, &invite)
	news := models.News{Title: "News", Body: "Body", Enabled: true, Date: now}
	news.ID = uuid.New()
	createRouterRecord(t, &news)

	// The attacker owns a wishlist, a wish and a group: all the old bug needed
	// for their ownership and membership checks to pass on the nil UUID.
	attackerGroup := models.Group{Name: "Erin group", Enabled: true, OwnerID: attacker.ID}
	attackerGroup.ID = uuid.New()
	createRouterRecord(t, &attackerGroup)
	attackerMembership := models.GroupMembership{GroupID: attackerGroup.ID, MemberID: attacker.ID, Enabled: true}
	attackerMembership.ID = uuid.New()
	createRouterRecord(t, &attackerMembership)
	attackerList := models.Wishlist{Name: "Erin list", Enabled: true, OwnerID: attacker.ID, Date: &now, Claimable: &truth}
	attackerList.ID = uuid.New()
	createRouterRecord(t, &attackerList)
	attackerWish := models.Wish{Name: "Erin wish", Enabled: true, OwnerID: attacker.ID, WishlistID: attackerList.ID}
	attackerWish.ID = uuid.New()
	createRouterRecord(t, &attackerWish)

	nilID := uuid.Nil.String()
	future := now.AddDate(1, 0, 0).Format("2006-01-02")
	erin, alice := token(attacker.ID, false), token(admin.ID, true)
	ownList, ownGroup, ownWish := attackerList.ID.String(), attackerGroup.ID.String(), attackerWish.ID.String()

	cases := []struct {
		token, method, path, body string
	}{
		{erin, "GET", "/api/auth/wishes?wishlist=" + nilID, ""},
		{erin, "GET", "/api/auth/wishes?wishlist=00000000000000000000000000000000", ""},
		{erin, "GET", "/api/auth/wishes?wishlist=" + url.QueryEscape("{"+nilID+"}"), ""},
		{erin, "GET", "/api/auth/wishes?wishlist=" + url.QueryEscape("urn:uuid:"+nilID), ""},
		{erin, "POST", "/api/auth/wishes?wishlist=" + nilID, `{"name":"Nil wish name"}`},
		{erin, "POST", "/api/auth/wishes?wishlist=" + ownList, `{"name":"Nil category wish","category_id":"` + nilID + `"}`},
		{erin, "GET", "/api/auth/wishes/" + nilID, ""},
		{erin, "POST", "/api/auth/wishes/" + nilID, `{"name":"Renamed wish"}`},
		{erin, "POST", "/api/auth/wishes/" + ownWish, `{"name":"Erin wish","category_id":"` + nilID + `"}`},
		{erin, "DELETE", "/api/auth/wishes/" + nilID, ""},
		{erin, "POST", "/api/auth/wishes/" + nilID + "/claim", `{}`},
		{erin, "POST", "/api/auth/wishes/" + nilID + "/unclaim", `{}`},
		{erin, "POST", "/api/auth/wishes/" + victimWish.ID.String() + "/claim", `{"wishlist_id":"` + nilID + `"}`},
		{erin, "GET", "/api/both/wishes/" + nilID + "/image", ""},
		{"", "GET", "/api/both/wishes/" + nilID + "/image", ""},
		{"", "GET", "/api/open/wishlists/public/" + nilID, ""},
		{erin, "GET", "/api/auth/wishlists/" + nilID, ""},
		{erin, "POST", "/api/auth/wishlists/" + nilID, `{"name":"Renamed list","date":"` + future + `"}`},
		{erin, "DELETE", "/api/auth/wishlists/" + nilID, ""},
		{erin, "DELETE", "/api/auth/wishlists/" + ownList + "?group=" + nilID, ""},
		{erin, "POST", "/api/auth/wishlists/" + nilID + "/join", `{"groups":["` + ownGroup + `"]}`},
		{erin, "POST", "/api/auth/wishlists/" + ownList + "/join", `{"groups":["` + nilID + `"]}`},
		{erin, "POST", "/api/auth/wishlists/" + nilID + "/remove", `{"group_id":"` + victimGroup.ID.String() + `"}`},
		{erin, "POST", "/api/auth/wishlists/" + ownList + "/remove", `{"group_id":"` + nilID + `"}`},
		{erin, "POST", "/api/auth/wishlists/" + ownList + "/remove", `{}`},
		{erin, "POST", "/api/auth/wishlists/" + nilID + "/collaborate", `{"users":["` + admin.ID.String() + `"]}`},
		{erin, "POST", "/api/auth/wishlists/" + ownList + "/collaborate", `{"users":["` + nilID + `"]}`},
		{erin, "POST", "/api/auth/wishlists/" + nilID + "/un-collaborate", `{"user_id":"` + claimer.ID.String() + `"}`},
		{erin, "POST", "/api/auth/wishlists/" + ownList + "/un-collaborate", `{"user_id":"` + nilID + `"}`},
		{erin, "GET", "/api/auth/wishlists/" + nilID + "/categories", ""},
		{erin, "GET", "/api/auth/wishlists?group=" + nilID, ""},
		{erin, "GET", "/api/auth/wishlists?notAMemberOfGroupID=" + nilID, ""},
		{erin, "POST", "/api/auth/wishlists", `{"name":"Nil group list","date":"` + future + `","groups":["` + nilID + `"]}`},
		{erin, "POST", "/api/auth/wishlists?groupContextID=" + nilID, `{"name":"Nil context list","date":"` + future + `"}`},
		{erin, "GET", "/api/auth/groups/" + nilID, ""},
		{erin, "GET", "/api/auth/groups/" + nilID + "/members", ""},
		{erin, "POST", "/api/auth/groups/" + nilID, `{"name":"Renamed group","description":"Renamed"}`},
		{erin, "DELETE", "/api/auth/groups/" + nilID, ""},
		{erin, "POST", "/api/auth/groups/" + nilID + "/join", `{"members":["` + admin.ID.String() + `"]}`},
		{erin, "POST", "/api/auth/groups/" + ownGroup + "/join", `{"members":["` + nilID + `"]}`},
		{erin, "POST", "/api/auth/groups/" + nilID + "/leave", ""},
		{erin, "POST", "/api/auth/groups/" + nilID + "/remove", `{"member_id":"` + claimer.ID.String() + `"}`},
		{erin, "POST", "/api/auth/groups/" + ownGroup + "/remove", `{"member_id":"` + nilID + `"}`},
		{erin, "POST", "/api/auth/groups/" + nilID + "/add", `{"wishlists":["` + ownList + `"]}`},
		{erin, "POST", "/api/auth/groups/" + ownGroup + "/add", `{"wishlists":["` + nilID + `"]}`},
		{erin, "POST", "/api/auth/groups", `{"name":"Nil member group","members":["` + nilID + `"]}`},
		{erin, "GET", "/api/auth/groups?memberOfWishlistID=" + nilID, ""},
		{erin, "GET", "/api/auth/groups?notAMemberOfWishlistID=" + nilID, ""},
		{erin, "GET", "/api/auth/news/" + nilID, ""},
		{erin, "GET", "/api/auth/users/" + nilID, ""},
		{erin, "GET", "/api/auth/users/" + nilID + "/image", ""},
		{erin, "GET", "/api/auth/users?notAMemberOfGroupID=" + nilID, ""},
		{erin, "GET", "/api/auth/users?notACollaboratorOfWishlistID=" + nilID, ""},
		{alice, "DELETE", "/api/admin/users/" + nilID, ""},
		{alice, "DELETE", "/api/admin/users/" + nilID + "/mfa", ""},
		{alice, "DELETE", "/api/admin/users/" + nilID + "/sessions", ""},
		{alice, "DELETE", "/api/admin/invites/" + nilID, ""},
		{alice, "DELETE", "/api/admin/news/" + nilID, ""},
		{alice, "POST", "/api/admin/news/" + nilID, `{"title":"Renamed news","body":"Renamed"}`},
		// A token for the nil UUID can't be forged without the signing key,
		// but it must not stand for every account either.
		{token(uuid.Nil, false), "GET", "/api/auth/wishlists", ""},
		{token(uuid.Nil, false), "GET", "/api/auth/wishes?wishlist=" + victimList.ID.String(), ""},
		{token(uuid.Nil, false), "GET", "/api/auth/groups", ""},
	}

	send := func(token, method, path, body string) *httptest.ResponseRecorder {
		var reader io.Reader
		if body != "" {
			reader = strings.NewReader(body)
		}
		req := httptest.NewRequest(method, path, reader)
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", token)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	// The same requests with real IDs work, so the refusals below come from
	// the nil UUID and not from a broken setup.
	for _, control := range []struct{ token, path, want string }{
		{erin, "/api/auth/wishes?wishlist=" + ownList, "Erin wish"},
		{erin, "/api/auth/groups/" + ownGroup, "Erin group"},
		{alice, "/api/admin/invites", "INVITECODE"},
		{"", "/api/open/wishlists/public/" + publicList.PublicHash.String(), "Victim public list"},
	} {
		if w := send(control.token, "GET", control.path, ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), control.want) {
			t.Fatalf("GET %s = %d %s, want 200 with %q", control.path, w.Code, w.Body.String(), control.want)
		}
	}

	secrets := []string{*victim.Email, *claimer.Email, "Victim", victimList.ID.String(), publicList.PublicHash.String()}
	before := snapshotRouterTables(t)
	for _, c := range cases {
		w := send(c.token, c.method, c.path, c.body)
		switch w.Code {
		case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
		default:
			t.Errorf("%s %s = %d, want 400 or 404: %s", c.method, c.path, w.Code, w.Body.String())
		}
		for _, secret := range secrets {
			if strings.Contains(w.Body.String(), secret) {
				t.Errorf("%s %s leaked %q: %s", c.method, c.path, secret, w.Body.String())
			}
		}
	}
	if after := snapshotRouterTables(t); after != before {
		t.Errorf("requests naming the nil UUID changed the database:\nbefore %s\nafter  %s", before, after)
	}
}

// Any account can put any other in a group or make it a collaborator, without
// its say. Other users, wherever they show up, are only an ID and a name: no
// e-mail address, no admin or MFA flag.
func TestResponsesShowOtherUsersOnlyByName(t *testing.T) {
	router, token := setupRouterTest(t)
	now := time.Now()
	truth := true

	erin := createRouterUser(t, "Erin", false)
	erin.MFAEnabled = new(bool)
	if err := database.Instance.Save(&erin).Error; err != nil {
		t.Fatal(err)
	}
	dave := createRouterUser(t, "Dave", true)
	alice := createRouterUser(t, "Alice", true)

	family := models.Group{Name: "Family", Enabled: true, OwnerID: alice.ID}
	family.ID = uuid.New()
	createRouterRecord(t, &family)
	for _, memberID := range []uuid.UUID{alice.ID, erin.ID, dave.ID} {
		membership := models.GroupMembership{GroupID: family.ID, MemberID: memberID, Enabled: true}
		membership.ID = uuid.New()
		createRouterRecord(t, &membership)
	}
	shared := models.Wishlist{Name: "Alice list", Enabled: true, OwnerID: alice.ID, Date: &now, Claimable: &truth}
	shared.ID = uuid.New()
	createRouterRecord(t, &shared)
	share := models.WishlistMembership{GroupID: family.ID, WishlistID: shared.ID, Enabled: true}
	share.ID = uuid.New()
	createRouterRecord(t, &share)
	wish := models.Wish{Name: "Alice wish", Enabled: true, OwnerID: alice.ID, WishlistID: shared.ID}
	wish.ID = uuid.New()
	createRouterRecord(t, &wish)
	claim := models.WishClaim{WishID: wish.ID, UserID: dave.ID, Enabled: true}
	claim.ID = uuid.New()
	createRouterRecord(t, &claim)
	own := models.Wishlist{Name: "Erin list", Enabled: true, OwnerID: erin.ID, Date: &now}
	own.ID = uuid.New()
	createRouterRecord(t, &own)

	erinToken := token(erin.ID, false)
	send := func(method, path, body string) string {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", erinToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code >= 300 {
			t.Fatalf("%s %s = %d: %s", method, path, w.Code, w.Body.String())
		}
		return w.Body.String()
	}
	created := send("POST", "/api/auth/groups", `{"name":"Throwaway group","members":["`+dave.ID.String()+`"]}`)
	var groups struct {
		Groups []struct{ ID string } `json:"groups"`
	}
	if err := json.Unmarshal([]byte(created), &groups); err != nil || len(groups.Groups) == 0 {
		t.Fatalf("no group in %s", created)
	}
	responses := map[string]string{
		"new group":       created,
		"collaborator":    send("POST", "/api/auth/wishlists/"+own.ID.String()+"/collaborate", `{"users":["`+dave.ID.String()+`"]}`),
		"group members":   send("GET", "/api/auth/groups/"+family.ID.String()+"/members", ""),
		"groups":          send("GET", "/api/auth/groups", ""),
		"directory":       send("GET", "/api/auth/users", ""),
		"user":            send("GET", "/api/auth/users/"+dave.ID.String(), ""),
		"shared wishlist": send("GET", "/api/auth/wishlists/"+shared.ID.String(), ""),
		"wishlists":       send("GET", "/api/auth/wishlists", ""),
		"wishes":          send("GET", "/api/auth/wishes?wishlist="+shared.ID.String(), ""),
		"wish":            send("GET", "/api/auth/wishes/"+wish.ID.String(), ""),
	}
	for name, body := range responses {
		for _, leak := range []string{*dave.Email, *alice.Email, `"admin":true`, `"mfa_enabled":true`} {
			if strings.Contains(body, leak) {
				t.Errorf("%s response carries %s: %s", name, leak, body)
			}
		}
	}
}

// GORM leaves zero values out of a struct condition, so a nil UUID or an
// empty string there silently drops the condition. Queries name their
// columns instead.
func TestDatabaseQueriesNameTheirColumns(t *testing.T) {
	structCondition := regexp.MustCompile(`\b(Where|Not|Or|First|Find|Take|Last|Delete)\(\s*&?models\.[A-Za-z]+\{[^}]`)
	files, err := filepath.Glob("database/*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("no database sources found: %v", err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range structCondition.FindAllString(string(raw), -1) {
			t.Errorf("%s uses a struct condition: %s", file, match)
		}
	}
}
