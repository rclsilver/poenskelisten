package middlewares

import (
	"aunefyren/poenskelisten/logger"
	"sync"

	"github.com/gin-gonic/gin"
)

// WarnUntrustedForwardedFor logs, once per process, a request whose
// X-Forwarded-For header was ignored because it didn't come from a trusted
// proxy. On an instance behind a reverse proxy that means trustedproxies isn't
// set (or doesn't match the proxy), so every visitor is seen with the proxy's
// address and shares its rate limits.
func WarnUntrustedForwardedFor() gin.HandlerFunc {
	var once sync.Once
	return func(ctx *gin.Context) {
		if ctx.GetHeader("X-Forwarded-For") != "" && ctx.ClientIP() == ctx.RemoteIP() {
			once.Do(func() {
				logger.Log.Warn("ignored an X-Forwarded-For header from " + ctx.RemoteIP() + ", which isn't a trusted proxy. If Pønskelisten is behind a reverse proxy, set trustedproxies to its address, or every visitor shares the proxy's address for rate limiting.")
			})
		}
		ctx.Next()
	}
}
