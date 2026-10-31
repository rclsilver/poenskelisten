package middlewares

import "github.com/gin-gonic/gin"

// form-action is left out: the consent form is answered with a redirect to the
// third-party client, which form-action would block.
const ContentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; " +
	"font-src 'self'; connect-src 'self'; manifest-src 'self'; worker-src 'self'; " +
	"object-src 'none'; base-uri 'none'; frame-ancestors 'none'"

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
		header.Set("Content-Security-Policy", ContentSecurityPolicy)
		header.Set("Referrer-Policy", "same-origin")
		ctx.Next()
	}
}
