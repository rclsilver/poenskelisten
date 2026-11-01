package middlewares

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// LargeBodies lets at most slots requests with a body over threshold, or of
// unknown length, be handled at once. A handler that decodes JSON holds
// several copies of a body while it does, so a burst of large ones could use
// up the memory of the whole app. The others wait for a slot, up to wait,
// before anything reads their body, then are answered 503.
func LargeBodies(slots int, threshold int64, wait time.Duration) gin.HandlerFunc {
	inUse := make(chan struct{}, slots)
	return func(ctx *gin.Context) {
		if length := ctx.Request.ContentLength; length >= 0 && length <= threshold {
			ctx.Next()
			return
		}
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case inUse <- struct{}{}:
			defer func() { <-inUse }()
			ctx.Next()
		case <-timer.C:
			setRetryAfter(ctx, 10*time.Second)
			ctx.JSON(http.StatusServiceUnavailable, gin.H{"error": "Too many large requests at once. Please try again in a moment."})
			ctx.Abort()
		case <-ctx.Request.Context().Done():
			ctx.Abort()
		}
	}
}
