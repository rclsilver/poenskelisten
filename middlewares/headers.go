package middlewares

import "github.com/gin-gonic/gin"

// SecurityHeaders sets the headers every response should carry:
//   - X-Content-Type-Options: nosniff, so a browser never guesses a type and,
//     say, runs an uploaded image as a script.
//   - X-Frame-Options: DENY and CSP frame-ancestors 'none' (its modern form),
//     so no other site can frame the app, e.g. to trick a click on the
//     consent page's Allow button. Embedding the app in a dashboard isn't
//     possible as a result.
//   - Referrer-Policy: same-origin, so the paths and queries of the app's
//     URLs (public wishlist links, password reset codes, OAuth codes) never
//     leave in a Referer header, e.g. when following a wish's link to a shop.
func SecurityHeaders() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		header := ctx.Writer.Header()
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("X-Frame-Options", "DENY")
		header.Set("Content-Security-Policy", "frame-ancestors 'none'")
		header.Set("Referrer-Policy", "same-origin")
		ctx.Next()
	}
}
