package middlewares

import (
	"net/http"
	"net/http/httptest"
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
			"Content-Security-Policy": "frame-ancestors 'none'",
			"Referrer-Policy":         "same-origin",
		}
		for name, value := range want {
			if got := w.Header().Get(name); got != value {
				t.Errorf("%s %s = %q, want %q", path, name, got, value)
			}
		}
	}
}
