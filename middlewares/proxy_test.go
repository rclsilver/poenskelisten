package middlewares

import (
	"aunefyren/poenskelisten/logger"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	logrusTest "github.com/sirupsen/logrus/hooks/test"
)

func TestWarnUntrustedForwardedForWarnsOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	original := logger.Log
	t.Cleanup(func() { logger.Log = original })
	var hook *logrusTest.Hook
	logger.Log, hook = logrusTest.NewNullLogger()

	router := gin.New()
	if err := router.SetTrustedProxies([]string{"10.0.0.0/8"}); err != nil {
		t.Fatalf("SetTrustedProxies: %v", err)
	}
	router.Use(WarnUntrustedForwardedFor())
	router.GET("/", func(ctx *gin.Context) { ctx.Status(http.StatusOK) })

	send := func(remoteAddr string, forwardedFor string) {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.RemoteAddr = remoteAddr
		if forwardedFor != "" {
			request.Header.Set("X-Forwarded-For", forwardedFor)
		}
		router.ServeHTTP(httptest.NewRecorder(), request)
	}

	send("10.1.2.3:1000", "203.0.113.7") // from the trusted proxy: used, no warning
	send("198.51.100.1:1000", "")        // no header: nothing to warn about
	if len(hook.AllEntries()) != 0 {
		t.Fatalf("unexpected warnings: %v", hook.AllEntries())
	}

	send("198.51.100.1:1000", "203.0.113.7")
	send("198.51.100.2:1000", "203.0.113.8")
	entries := hook.AllEntries()
	if len(entries) != 1 || entries[0].Level != logrus.WarnLevel {
		t.Fatalf("entries = %v, want exactly one warning", entries)
	}
}
