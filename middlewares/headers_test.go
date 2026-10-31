package middlewares

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSecurityHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(SecurityHeaders())
	router.GET("/", func(ctx *gin.Context) { ctx.String(http.StatusOK, "ok") })

	for _, path := range []string{"/", "/missing"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		want := map[string]string{
			"X-Content-Type-Options":  "nosniff",
			"X-Frame-Options":         "DENY",
			"Content-Security-Policy": ContentSecurityPolicy,
			"Referrer-Policy":         "same-origin",
		}
		for name, value := range want {
			if got := w.Header().Get(name); got != value {
				t.Errorf("%s %s = %q, want %q", path, name, got, value)
			}
		}
	}
}

// The policy must stay strict: no inline or eval'd code, nothing from other
// sites, and still no framing.
func TestContentSecurityPolicyIsStrict(t *testing.T) {
	for _, forbidden := range []string{"unsafe-inline", "unsafe-eval", "*", "http:", "https:"} {
		if strings.Contains(ContentSecurityPolicy, forbidden) {
			t.Errorf("policy allows %q: %s", forbidden, ContentSecurityPolicy)
		}
	}
	for _, directive := range []string{"default-src 'self'", "script-src 'self'", "object-src 'none'", "base-uri 'none'", "frame-ancestors 'none'"} {
		if !strings.Contains(ContentSecurityPolicy, directive) {
			t.Errorf("policy lacks %q", directive)
		}
	}
	// A form-action directive would block the consent page's redirect to
	// third-party clients.
	if strings.Contains(ContentSecurityPolicy, "form-action") {
		t.Error("policy restricts form-action")
	}
}
