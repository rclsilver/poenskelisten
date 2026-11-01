package middlewares

import (
	"aunefyren/poenskelisten/logger"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	logrusTest "github.com/sirupsen/logrus/hooks/test"
)

// fakeClock lets a test move a limiter's time forward without sleeping.
type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time          { return c.now }
func (c *fakeClock) Advance(d time.Duration) { c.now = c.now.Add(d) }

func newTestLimiter(max int, window time.Duration) (*rateLimiter, *fakeClock) {
	clock := &fakeClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	limiter := newRateLimiter(max, window)
	limiter.now = clock.Now
	return limiter, clock
}

func TestRateLimiterAllowsUpToMax(t *testing.T) {
	limiter, _ := newTestLimiter(3, time.Minute)

	for i := 0; i < 3; i++ {
		if allowed, _, _ := limiter.allow("1.2.3.4"); !allowed {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
	if allowed, _, _ := limiter.allow("1.2.3.4"); allowed {
		t.Error("request over the limit should be blocked")
	}
}

func TestRateLimiterPerKey(t *testing.T) {
	limiter, _ := newTestLimiter(1, time.Minute)

	if allowed, _, _ := limiter.allow("a"); !allowed {
		t.Error("first request for key a should be allowed")
	}
	if allowed, _, _ := limiter.allow("a"); allowed {
		t.Error("second request for key a should be blocked")
	}
	// A different key has its own budget.
	if allowed, _, _ := limiter.allow("b"); !allowed {
		t.Error("first request for key b should be allowed")
	}
}

func TestRateLimiterWindowSlides(t *testing.T) {
	limiter, clock := newTestLimiter(2, time.Minute)

	limiter.allow("k")
	clock.Advance(40 * time.Second)
	limiter.allow("k")

	allowed, retryAfter, first := limiter.allow("k")
	if allowed {
		t.Fatal("third request within the window should be blocked")
	}
	if retryAfter != 20*time.Second {
		t.Errorf("retryAfter = %v, want 20s (until the first hit leaves the window)", retryAfter)
	}
	if !first {
		t.Error("the first refusal should be reported as such")
	}
	if _, _, first := limiter.allow("k"); first {
		t.Error("a second refusal in the same window shouldn't be reported as the first")
	}

	clock.Advance(21 * time.Second)
	if allowed, _, _ := limiter.allow("k"); !allowed {
		t.Error("once the first hit has left the window, a request should be allowed again")
	}
	if allowed, _, _ := limiter.allow("k"); allowed {
		t.Error("the second hit is still in the window, so the budget is full again")
	}
}

// Refused requests don't count: a client that keeps hammering is let back in
// as soon as its earlier, allowed requests age out.
func TestRateLimiterRefusalsDontExtendTheBlock(t *testing.T) {
	limiter, clock := newTestLimiter(1, time.Minute)

	limiter.allow("k")
	for i := 0; i < 5; i++ {
		clock.Advance(10 * time.Second)
		limiter.allow("k")
	}
	clock.Advance(11 * time.Second)
	if allowed, _, _ := limiter.allow("k"); !allowed {
		t.Error("request should be allowed once the only allowed hit has left the window")
	}
}

func TestRateLimiterSweepsStaleKeys(t *testing.T) {
	limiter, clock := newTestLimiter(5, time.Minute)

	for _, key := range []string{"a", "b", "c"} {
		limiter.allow(key)
	}
	clock.Advance(2 * time.Minute)
	limiter.allow("d")

	if len(limiter.entries) != 1 {
		t.Errorf("tracked keys = %d, want 1 after the others expired", len(limiter.entries))
	}
}

func TestRateLimiterRefusesNewKeysWhenFull(t *testing.T) {
	limiter, clock := newTestLimiter(5, time.Minute)
	for i := 0; i < maxTrackedKeys; i++ {
		limiter.entries[strconv.Itoa(i)] = &rateLimitEntry{hits: []time.Time{clock.Now()}}
	}
	limiter.nextSweep = clock.Now().Add(time.Minute)

	if allowed, _, _ := limiter.allow("newcomer"); allowed {
		t.Error("a new key should be refused while the limiter is full of live keys")
	}
	if allowed, _, _ := limiter.allow("0"); !allowed {
		t.Error("a tracked key should still be served while the limiter is full")
	}

	clock.Advance(2 * time.Minute)
	if allowed, _, _ := limiter.allow("newcomer"); !allowed {
		t.Error("a new key should be allowed once the stale keys are swept")
	}
}

func rateLimitTestContext(remoteAddr string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	ctx.Request.RemoteAddr = remoteAddr
	return ctx, w
}

func TestClientKeyGroupsIPv6ByNetwork(t *testing.T) {
	cases := []struct{ remoteAddr, want string }{
		{"203.0.113.1:12345", "203.0.113.1"},
		{"[2001:db8:1:2:aaaa::1]:443", "2001:db8:1:2::/64"},
		{"[2001:db8:1:2:bbbb::9]:443", "2001:db8:1:2::/64"},
		{"[2001:db8:1:3::1]:443", "2001:db8:1:3::/64"},
		{"[::ffff:203.0.113.1]:443", "203.0.113.1"},
	}
	for _, c := range cases {
		ctx, _ := rateLimitTestContext(c.remoteAddr)
		if got := clientKey(ctx); got != c.want {
			t.Errorf("clientKey(%s) = %q, want %q", c.remoteAddr, got, c.want)
		}
	}
}

func TestRateLimitMiddlewareAllowsWithinLimit(t *testing.T) {
	handler := RateLimit(2, time.Minute)

	ctx, _ := rateLimitTestContext("203.0.113.1:12345")
	handler(ctx)
	if ctx.IsAborted() {
		t.Error("first request should not be aborted")
	}
}

func TestRateLimitMiddlewareBlocksOverLimit(t *testing.T) {
	original := logger.Log
	t.Cleanup(func() { logger.Log = original })
	var hook *logrusTest.Hook
	logger.Log, hook = logrusTest.NewNullLogger()

	handler := RateLimit(1, time.Minute)

	first, _ := rateLimitTestContext("203.0.113.1:12345")
	handler(first)
	if first.IsAborted() {
		t.Fatal("first request should not be aborted")
	}

	// Same middleware instance (same limiter state), same client IP.
	for i := 0; i < 2; i++ {
		second, w := rateLimitTestContext("203.0.113.1:12345")
		handler(second)
		if !second.IsAborted() {
			t.Error("request over the limit should be aborted")
		}
		if w.Code != http.StatusTooManyRequests {
			t.Errorf("status = %d, want 429", w.Code)
		}
		if retry := w.Header().Get("Retry-After"); retry != "60" {
			t.Errorf("Retry-After = %q, want 60", retry)
		}
		var body map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if body["error"] != "rate_limited" {
			t.Errorf("error = %q, want the OAuth-style rate_limited", body["error"])
		}
	}

	warnings := 0
	for _, entry := range hook.AllEntries() {
		if entry.Level == logrus.WarnLevel {
			warnings++
		}
	}
	if warnings != 1 {
		t.Errorf("warnings = %d, want one per client and window", warnings)
	}
}

func TestAPIRateLimitHasReadableError(t *testing.T) {
	original := logger.Log
	t.Cleanup(func() { logger.Log = original })
	logger.Log, _ = logrusTest.NewNullLogger()

	handler := APIRateLimit(1, time.Minute)
	first, _ := rateLimitTestContext("203.0.113.1:12345")
	handler(first)

	second, w := rateLimitTestContext("203.0.113.1:12345")
	handler(second)
	var body map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if w.Code != http.StatusTooManyRequests || body["error"] == "" || body["error"] == "rate_limited" {
		t.Errorf("got %d %v, want 429 with a message the web app can show", w.Code, body)
	}

	// Another address isn't affected.
	other, _ := rateLimitTestContext("203.0.113.2:12345")
	handler(other)
	if other.IsAborted() {
		t.Error("a different client should have its own budget")
	}
}

// People behind one home connection each get their own upload budget.
func TestUploadRateLimitCountsPerAccount(t *testing.T) {
	setupMiddlewareConfig(t)
	original := logger.Log
	t.Cleanup(func() { logger.Log = original })
	logger.Log, _ = logrusTest.NewNullLogger()

	handler := NewUploadBudget(1, time.Minute).Limit()
	send := func(token string) *httptest.ResponseRecorder {
		ctx, w := rateLimitTestContext("203.0.113.1:12345")
		if token != "" {
			ctx.Request.Header.Set("Authorization", "Bearer "+token)
		}
		handler(ctx)
		if !ctx.IsAborted() {
			w.WriteHeader(http.StatusOK)
		}
		return w
	}

	alice, bob := apiTokenForUser(t, uuid.New(), false), apiTokenForUser(t, uuid.New(), false)
	if w := send(alice); w.Code != http.StatusOK {
		t.Fatalf("first upload = %d, want 200", w.Code)
	}
	w := send(alice)
	var body map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if w.Code != http.StatusTooManyRequests || body["error"] == "" {
		t.Errorf("second upload = %d %v, want 429 with a message the web app can show", w.Code, body)
	}
	if w := send(bob); w.Code != http.StatusOK {
		t.Errorf("another account from the same address = %d, want its own budget", w.Code)
	}
	if w := send(""); w.Code != http.StatusOK {
		t.Errorf("a request without a token = %d, want it counted by address", w.Code)
	}
}

// An image sent through MCP counts against the same budget as the web
// routes, for the same account only.
func TestUploadBudgetIsSharedByWebAndMCP(t *testing.T) {
	setupMiddlewareConfig(t)
	original := logger.Log
	t.Cleanup(func() { logger.Log = original })
	logger.Log, _ = logrusTest.NewNullLogger()

	budget := NewUploadBudget(2, time.Minute)
	alice, bob := uuid.New(), uuid.New()
	ctx, w := rateLimitTestContext("203.0.113.1:12345")
	ctx.Request.Header.Set("Authorization", "Bearer "+apiTokenForUser(t, alice, false))
	budget.Limit()(ctx)
	if ctx.IsAborted() {
		t.Fatalf("first web upload refused with %d", w.Code)
	}
	if !budget.Allow(alice, "update_wish") {
		t.Fatal("first MCP upload refused within the budget")
	}
	if budget.Allow(alice, "update_wish") {
		t.Error("a third upload from web and MCP together was allowed")
	}
	if !budget.Allow(bob, "update_wish") {
		t.Error("another account shares Alice's budget")
	}

	var off *UploadBudget
	if !off.Allow(alice, "update_wish") || NewUploadBudget(0, time.Minute) != nil {
		t.Error("a budget turned off must allow everything")
	}
}

func TestRateLimitOffWhenMaxIsZero(t *testing.T) {
	handler := APIRateLimit(0, time.Minute)
	for i := 0; i < 50; i++ {
		ctx, _ := rateLimitTestContext("203.0.113.1:12345")
		handler(ctx)
		if ctx.IsAborted() {
			t.Fatalf("request %d aborted with the limit off", i+1)
		}
	}
}

// Every route given the same handler shares one budget, which is how the
// login, MFA and reset endpoints are limited together.
func TestRateLimitSharedAcrossRoutes(t *testing.T) {
	original := logger.Log
	t.Cleanup(func() { logger.Log = original })
	logger.Log, _ = logrusTest.NewNullLogger()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	limit := APIRateLimit(2, time.Minute)
	ok := func(ctx *gin.Context) { ctx.Status(http.StatusOK) }
	router.POST("/login", limit, ok)
	router.POST("/mfa", limit, ok)

	codes := []int{}
	for _, path := range []string{"/login", "/mfa", "/login"} {
		w := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, path, nil)
		request.RemoteAddr = "203.0.113.1:12345"
		router.ServeHTTP(w, request)
		codes = append(codes, w.Code)
	}
	if codes[0] != 200 || codes[1] != 200 || codes[2] != 429 {
		t.Errorf("codes = %v, want [200 200 429]", codes)
	}
}

func TestAttemptLimiterLocksAfterFailures(t *testing.T) {
	attempts := NewAttemptLimiter(3, time.Minute)
	clock := &fakeClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	attempts.limiter.now = clock.Now

	locks := []bool{attempts.Failed("k"), attempts.Failed("k"), attempts.Failed("k")}
	if locks[0] || locks[1] || !locks[2] {
		t.Errorf("Failed reported locks %v, want only the third to lock", locks)
	}
	if locked, retryAfter := attempts.Blocked("k"); !locked || retryAfter != time.Minute {
		t.Errorf("Blocked = %v, %v; want locked for a minute", locked, retryAfter)
	}
	if attempts.Failed("k") {
		t.Error("a failure on an already locked key shouldn't report a new lock")
	}
	if locked, _ := attempts.Blocked("other"); locked {
		t.Error("another key shouldn't be locked")
	}

	clock.Advance(time.Minute + time.Second)
	if locked, _ := attempts.Blocked("k"); locked {
		t.Error("the lock should lift once the failures leave the window")
	}
}

// Failures of an account the limiter has no room for can't be counted, so it
// must not be left open to unlimited guessing meanwhile.
func TestAttemptLimiterLocksUntrackedKeysWhenFull(t *testing.T) {
	attempts := NewAttemptLimiter(3, time.Minute)
	clock := &fakeClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	attempts.limiter.now = clock.Now
	for i := 0; i < maxTrackedKeys; i++ {
		attempts.limiter.entries[strconv.Itoa(i)] = &rateLimitEntry{hits: []time.Time{clock.Now()}}
	}
	attempts.limiter.nextSweep = clock.Now().Add(time.Minute)

	for i := 0; i < 5; i++ {
		attempts.Failed("newcomer")
	}
	if locked, retryAfter := attempts.Blocked("newcomer"); !locked || retryAfter != time.Minute {
		t.Errorf("untracked key while full: Blocked = %v, %v; want locked for the window", locked, retryAfter)
	}
	if locked, _ := attempts.Blocked("0"); locked {
		t.Error("a tracked key below its budget shouldn't be locked")
	}

	clock.Advance(2 * time.Minute)
	if locked, _ := attempts.Blocked("newcomer"); locked {
		t.Error("the key should be open once the stale keys are swept")
	}
}

func TestAttemptLimiterSuccessClears(t *testing.T) {
	attempts := NewAttemptLimiter(2, time.Minute)
	attempts.Failed("k")
	attempts.Succeeded("k")
	attempts.Failed("k")
	if locked, _ := attempts.Blocked("k"); locked {
		t.Error("a success should clear earlier failures")
	}
}

func TestAttemptLimiterOff(t *testing.T) {
	attempts := NewAttemptLimiter(0, time.Minute)
	if attempts != nil {
		t.Fatal("a zero max should turn the limiter off (nil)")
	}
	for i := 0; i < 20; i++ {
		attempts.Failed("k")
	}
	attempts.Succeeded("k")
	if locked, _ := attempts.Blocked("k"); locked {
		t.Error("a disabled limiter never locks")
	}
}

func TestRefuseLockedAttempt(t *testing.T) {
	ctx, w := rateLimitTestContext("203.0.113.1:1")
	RefuseLockedAttempt(ctx, 90*time.Second+time.Millisecond)
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") != "91" || !ctx.IsAborted() {
		t.Errorf("got %d Retry-After %q aborted %v, want 429, 91, aborted", w.Code, w.Header().Get("Retry-After"), ctx.IsAborted())
	}
}
