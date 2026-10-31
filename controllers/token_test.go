package controllers

import (
	"aunefyren/poenskelisten/config"
	"aunefyren/poenskelisten/database"
	"aunefyren/poenskelisten/models"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

func postGenerateToken(body string) (int, map[string]interface{}) {
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest("POST", "/api/open/tokens/register", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	GenerateToken(ctx)

	var parsed map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &parsed)
	return w.Code, parsed
}

func TestGenerateTokenBadJSON(t *testing.T) {
	setupControllersDB(t)
	if code, _ := postGenerateToken("not json"); code != 400 {
		t.Errorf("status = %d, want 400 for malformed JSON", code)
	}
}

func TestGenerateTokenUnknownEmail(t *testing.T) {
	setupControllersDB(t)

	// The same status and message as a wrong password, so the response doesn't
	// tell whether the account exists.
	code, body := postGenerateToken(`{"email":"nobody@example.com","password":"whatever"}`)
	if code != 401 {
		t.Fatalf("status = %d, want 401 for an unknown e-mail; body=%v", code, body)
	}
	if body["error"] != "Invalid credentials." {
		t.Errorf("error = %v, want a generic invalid-credentials message", body["error"])
	}
}

func TestGenerateTokenWrongPassword(t *testing.T) {
	setupControllersDB(t)
	user := createTestUserWithPassword(t, "correct-horse-battery-staple")

	code, body := postGenerateToken(`{"email":"` + *user.Email + `","password":"wrong-password"}`)
	if code != 401 {
		t.Fatalf("status = %d, want 401 for a wrong password; body=%v", code, body)
	}
}

func TestGenerateTokenSuccess(t *testing.T) {
	setupControllersDB(t)
	enablePrivateKey(t)
	user := createTestUserWithPassword(t, "correct-horse-battery-staple")

	code, body := postGenerateToken(`{"email":"` + *user.Email + `","password":"correct-horse-battery-staple"}`)
	if code != 200 {
		t.Fatalf("status = %d, want 200; body=%v", code, body)
	}
	if body["message"] != "Logged in!" {
		t.Errorf("message = %v", body["message"])
	}
	if body["mfa_required"] != nil {
		t.Errorf("mfa_required = %v, want absent for a user without MFA", body["mfa_required"])
	}
}

func TestGenerateTokenMFARequired(t *testing.T) {
	setupControllersDB(t)
	// The MFA challenge token is HS256-signed; don't rely on an earlier test
	// having left a key configured.
	origKey := config.ConfigFile.PrivateKey
	t.Cleanup(func() { config.ConfigFile.PrivateKey = origKey })
	enablePrivateKey(t)
	user := createTestUserWithPassword(t, "correct-horse-battery-staple")

	user.MFAEnabled = boolPtr(true)
	user.MFASecret = strPtr("JBSWY3DPEHPK3PXP")
	updated, err := database.UpdateUserInDB(user)
	if err != nil {
		t.Fatalf("failed to enable MFA on test user: %v", err)
	}

	code, body := postGenerateToken(`{"email":"` + *updated.Email + `","password":"correct-horse-battery-staple"}`)
	if code != 200 {
		t.Fatalf("status = %d, want 200; body=%v", code, body)
	}
	if body["mfa_required"] != true {
		t.Errorf("mfa_required = %v, want true", body["mfa_required"])
	}
	if body["mfa_token"] == nil || body["mfa_token"] == "" {
		t.Error("expected a non-empty mfa_token")
	}
}

func strPtr(s string) *string { return &s }

// tokenTestClearPrivateKey unsets the HS256 signing key for the rest of the
// test, so minting the MFA challenge token or SSO cookie fails.
func tokenTestClearPrivateKey(t *testing.T) {
	t.Helper()
	orig := config.ConfigFile.PrivateKey
	t.Cleanup(func() { config.ConfigFile.PrivateKey = orig })
	config.ConfigFile.PrivateKey = ""
}

func TestGenerateTokenSessionFailure(t *testing.T) {
	setupControllersDB(t)
	user := createTestUserWithPassword(t, "correct-horse-battery-staple")
	tokenTestClearPrivateKey(t)

	code, body := postGenerateToken(`{"email":"` + *user.Email + `","password":"correct-horse-battery-staple"}`)
	if code != 500 {
		t.Fatalf("status = %d, want 500 when the session cookie can't be signed; body=%v", code, body)
	}
	if body["error"] != "Failed to log in." {
		t.Errorf("error = %v", body["error"])
	}
}

func TestGenerateTokenMFAChallengeFailure(t *testing.T) {
	setupControllersDB(t)
	user := createTestUserWithPassword(t, "correct-horse-battery-staple")
	user.MFAEnabled = boolPtr(true)
	user.MFASecret = strPtr("JBSWY3DPEHPK3PXP")
	if _, err := database.UpdateUserInDB(user); err != nil {
		t.Fatalf("failed to enable MFA on test user: %v", err)
	}
	tokenTestClearPrivateKey(t)

	code, body := postGenerateToken(`{"email":"` + *user.Email + `","password":"correct-horse-battery-staple"}`)
	if code != 500 {
		t.Fatalf("status = %d, want 500 when the MFA challenge token can't be signed; body=%v", code, body)
	}
	if body["mfa_token"] != nil {
		t.Errorf("mfa_token = %v, want absent", body["mfa_token"])
	}
}

// An OIDC-only account has no password; it is refused like any wrong
// password rather than with a quicker or different answer.
func TestGenerateTokenPasswordlessAccount(t *testing.T) {
	setupControllersDB(t)
	user := createTestUser(t)
	user.Password = nil
	if _, err := database.UpdateUserInDB(user); err != nil {
		t.Fatalf("failed to clear password: %v", err)
	}

	code, body := postGenerateToken(`{"email":"` + *user.Email + `","password":"anything"}`)
	if code != 401 || body["error"] != "Invalid credentials." {
		t.Errorf("got %d %v, want 401 invalid credentials", code, body)
	}
}

func TestGenerateTokenDatabaseFailure(t *testing.T) {
	setupControllersDB(t)
	breakControllersDB(t)

	code, body := postGenerateToken(`{"email":"nobody@example.com","password":"whatever"}`)
	if code != 500 || body["error"] != "Failed to log in." {
		t.Errorf("got %d %v, want 500 when the lookup itself fails", code, body)
	}
}

// The placeholder must cost what a real password check costs, or the time
// taken would still tell unknown e-mails apart.
func TestDummyPasswordHashUsesTheRealCost(t *testing.T) {
	cost, err := bcrypt.Cost(dummyPasswordHash())
	if err != nil {
		t.Fatalf("placeholder hash is invalid: %v", err)
	}
	if cost != models.BcryptCost {
		t.Errorf("cost = %d, want models.BcryptCost (%d)", cost, models.BcryptCost)
	}
	if passwordMatches(models.User{}, false, "no account has this password") {
		t.Error("passwordMatches must never accept a login without an account")
	}
}

// limitSignInAttempts sets a small per-account budget for one test.
func limitSignInAttempts(t *testing.T, max int) {
	t.Helper()
	SetSignInAttemptLimit(config.RateLimit{Max: max, Window: time.Minute})
	t.Cleanup(func() { SetSignInAttemptLimit(config.AccountRateLimit(models.ConfigStruct{})) })
}

func postGenerateTokenRecorder(body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest("POST", "/api/open/tokens/register", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	GenerateToken(ctx)
	return w
}

func TestGenerateTokenLocksAccountAfterFailures(t *testing.T) {
	setupControllersDB(t)
	enablePrivateKey(t)
	limitSignInAttempts(t, 3)
	user := createTestUserWithPassword(t, "Correct-Passw0rd")
	other := createTestUserWithPassword(t, "Other-Passw0rd")

	for i := 0; i < 3; i++ {
		if code, _ := postGenerateToken(`{"email":"` + *user.Email + `","password":"wrong"}`); code != 401 {
			t.Fatalf("attempt %d = %d, want 401", i+1, code)
		}
	}

	// Locked: even the right password is refused, the e-mail's case doesn't
	// matter, and nothing tells it apart from a wrong one.
	w := postGenerateTokenRecorder(`{"email":"` + strings.ToUpper(*user.Email) + `","password":"Correct-Passw0rd"}`)
	if w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Errorf("locked attempt = %d (Retry-After %q), want 429 with Retry-After", w.Code, w.Header().Get("Retry-After"))
	}
	if code, _ := postGenerateToken(`{"email":"` + *user.Email + `","password":"Correct-Passw0rd"}`); code != 429 {
		t.Errorf("locked attempt with the right password = %d, want 429", code)
	}

	// Another account isn't affected.
	if code, _ := postGenerateToken(`{"email":"` + *other.Email + `","password":"Other-Passw0rd"}`); code != 200 {
		t.Errorf("other account = %d, want 200", code)
	}
}

// An e-mail without an account locks exactly like a real one, so the lock
// itself doesn't reveal which addresses exist.
func TestGenerateTokenUnknownEmailLocksTheSameWay(t *testing.T) {
	setupControllersDB(t)
	limitSignInAttempts(t, 2)

	codes := []int{}
	for i := 0; i < 3; i++ {
		code, _ := postGenerateToken(`{"email":"nobody@example.com","password":"wrong"}`)
		codes = append(codes, code)
	}
	if codes[0] != 401 || codes[1] != 401 || codes[2] != 429 {
		t.Errorf("codes = %v, want [401 401 429]", codes)
	}
}

func TestGenerateTokenSuccessClearsFailures(t *testing.T) {
	setupControllersDB(t)
	enablePrivateKey(t)
	limitSignInAttempts(t, 3)
	user := createTestUserWithPassword(t, "Correct-Passw0rd")
	wrong := `{"email":"` + *user.Email + `","password":"wrong"}`

	postGenerateToken(wrong)
	postGenerateToken(wrong)
	if code, _ := postGenerateToken(`{"email":"` + *user.Email + `","password":"Correct-Passw0rd"}`); code != 200 {
		t.Fatalf("login = %d, want 200", code)
	}
	postGenerateToken(wrong)
	postGenerateToken(wrong)
	if code, _ := postGenerateToken(wrong); code != 401 {
		t.Errorf("third failure after a success = %d, want 401 (the success reset the count)", code)
	}
}

func TestSignInAttemptLimitOff(t *testing.T) {
	setupControllersDB(t)
	SetSignInAttemptLimit(config.RateLimit{})
	t.Cleanup(func() { SetSignInAttemptLimit(config.AccountRateLimit(models.ConfigStruct{})) })

	for i := 0; i < 15; i++ {
		if code, _ := postGenerateToken(`{"email":"nobody@example.com","password":"wrong"}`); code != 401 {
			t.Fatalf("attempt %d = %d, want 401 with the limit off", i+1, code)
		}
	}
}
