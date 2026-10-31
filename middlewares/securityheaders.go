package middlewares

import (
	"github.com/gin-gonic/gin"
)

// ContentSecurityPolicy is sent on every response, and only lets pages run and
// load what the app serves itself. script-src is 'self' only: the frontend
// carries no inline <script> blocks, on* handler attributes or javascript:
// URLs (TestFrontendHasNoInlineScript keeps it that way), so an injected
// handler or script tag is refused by the browser rather than run. style-src is
// 'self' only too, since the pages have no inline style (main_test.go checks
// it), and nothing comes from another site, fonts included. Images are
// same-origin or data: URLs (wish and profile pictures, QR codes).
// connect-src keeps fetch/XHR/beacons on this origin; object-src 'none' and
// base-uri 'none' shut off plugin and <base> tricks; frame-ancestors stops
// clickjacking.
//
// form-action is deliberately absent: the OAuth consent form POSTs to
// /oauth/consent, which redirects to a third-party client's redirect_uri, and
// browsers apply form-action to that redirect.
const ContentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self'; " +
	"style-src 'self'; " +
	"img-src 'self' data:; " +
	"font-src 'self'; " +
	"connect-src 'self'; " +
	"manifest-src 'self'; " +
	"worker-src 'self'; " +
	"object-src 'none'; " +
	"base-uri 'none'; " +
	"frame-ancestors 'none'"

// SecurityHeaders sets the CSP plus the companion headers that harden the
// browser side of every page. Referrer-Policy is same-origin rather than the
// browser default because public wishlist URLs carry their access hash, and
// clicking a wish link from such a page must not leak it to the shop.
func SecurityHeaders() gin.HandlerFunc {
	return func(context *gin.Context) {
		header := context.Writer.Header()
		header.Set("Content-Security-Policy", ContentSecurityPolicy)
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("X-Frame-Options", "DENY")
		header.Set("Referrer-Policy", "same-origin")
		context.Next()
	}
}
