package middlewares

import (
	"aunefyren/poenskelisten/logger"
	"math"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// maxTrackedKeys caps how many clients a limiter remembers at once, so a flood
// from many addresses can't grow its memory without bound. Once full, and
// after dropping everyone whose window has passed, new clients are refused
// until room frees up: the limits guard password and code checks, where
// failing closed is the safer side.
const maxTrackedKeys = 100000

// rateLimiter is a simple in-memory, per-key sliding-window limiter. State lives
// in the process, which is fine for a single-binary app.
type rateLimiter struct {
	mu        sync.Mutex
	entries   map[string]*rateLimitEntry
	max       int
	window    time.Duration
	nextSweep time.Time
	now       func() time.Time
}

type rateLimitEntry struct {
	hits []time.Time
	// warned is when a refusal for this key was last logged, so a client
	// hammering a limit logs once per window rather than once per request.
	warned time.Time
}

func newRateLimiter(max int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		entries: make(map[string]*rateLimitEntry),
		max:     max,
		window:  window,
		now:     time.Now,
	}
}

// allow records a hit for key and reports whether it is within the limit. When
// it isn't, retryAfter is how long until the oldest hit leaves the window, and
// firstRefusal tells whether this is the first refusal logged for key in a window.
func (r *rateLimiter) allow(key string) (allowed bool, retryAfter time.Duration, firstRefusal bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	entry, ok := r.liveEntry(key, now)
	if !ok {
		return false, r.window, false
	}

	if len(entry.hits) >= r.max {
		first := now.Sub(entry.warned) >= r.window
		if first {
			entry.warned = now
		}
		return false, entry.hits[0].Add(r.window).Sub(now), first
	}

	entry.hits = append(entry.hits, now)
	return true, 0, false
}

// blocked reports whether key has used up its budget, without recording a hit.
func (r *rateLimiter) blocked(key string) (bool, time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, exists := r.entries[key]
	if !exists {
		return false, 0
	}
	now := r.now()
	entry.hits = recentHits(entry.hits, now.Add(-r.window))
	if len(entry.hits) < r.max {
		return false, 0
	}
	return true, entry.hits[0].Add(r.window).Sub(now)
}

// record adds a hit for key and reports whether that is the hit that used up
// its budget. Only the latest max hits are kept, which is all blocked needs.
func (r *rateLimiter) record(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	entry, ok := r.liveEntry(key, now)
	if !ok {
		return false
	}
	wasBelow := len(entry.hits) < r.max
	entry.hits = append(entry.hits, now)
	if len(entry.hits) > r.max {
		entry.hits = entry.hits[len(entry.hits)-r.max:]
	}
	return wasBelow && len(entry.hits) >= r.max
}

// reset forgets every hit for key.
func (r *rateLimiter) reset(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, key)
}

// liveEntry returns key's entry with expired hits dropped, creating it if
// needed. It reports false when the limiter is full and key isn't tracked yet.
// Callers hold r.mu.
func (r *rateLimiter) liveEntry(key string, now time.Time) (*rateLimitEntry, bool) {
	if now.After(r.nextSweep) {
		r.sweep(now)
	}
	entry, exists := r.entries[key]
	if !exists {
		if len(r.entries) >= maxTrackedKeys {
			r.sweep(now)
			if len(r.entries) >= maxTrackedKeys {
				return nil, false
			}
		}
		entry = &rateLimitEntry{}
		r.entries[key] = entry
	}
	entry.hits = recentHits(entry.hits, now.Add(-r.window))
	return entry, true
}

// sweep drops every key with no hit left in the window. Callers hold r.mu.
func (r *rateLimiter) sweep(now time.Time) {
	cutoff := now.Add(-r.window)
	for key, entry := range r.entries {
		if len(entry.hits) == 0 || !entry.hits[len(entry.hits)-1].After(cutoff) {
			delete(r.entries, key)
		}
	}
	r.nextSweep = now.Add(r.window)
}

// recentHits drops the hits at or before cutoff. Hits are kept in order.
func recentHits(hits []time.Time, cutoff time.Time) []time.Time {
	for i, hit := range hits {
		if hit.After(cutoff) {
			return hits[i:]
		}
	}
	return hits[:0]
}

// clientKey is the address the caller is rate-limited under: its IP address,
// or for IPv6 its /64 network, since a single household or server is usually
// handed a whole /64 and could otherwise move to a fresh address per request.
// It relies on gin's ClientIP, so it only follows X-Forwarded-For from the
// trusted proxies.
func clientKey(ctx *gin.Context) string {
	address := ctx.ClientIP()
	ip := net.ParseIP(address)
	if ip == nil {
		return address
	}
	if ip.To4() != nil {
		return ip.String()
	}
	return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
}

// setRetryAfter tells the client how many whole seconds to wait.
func setRetryAfter(ctx *gin.Context, wait time.Duration) {
	seconds := int(math.Ceil(wait.Seconds()))
	if seconds < 1 {
		seconds = 1
	}
	ctx.Header("Retry-After", strconv.Itoa(seconds))
}

// limitRequests builds a middleware that limits requests per clientKey and
// answers refused ones with respond. A non-positive max turns it off. Every
// route given the same returned handler shares one budget.
func limitRequests(max int, window time.Duration, respond func(*gin.Context)) gin.HandlerFunc {
	if max <= 0 {
		return func(ctx *gin.Context) { ctx.Next() }
	}
	limiter := newRateLimiter(max, window)
	return func(ctx *gin.Context) {
		key := clientKey(ctx)
		allowed, retryAfter, firstRefusal := limiter.allow(key)
		if !allowed {
			if firstRefusal {
				logger.Log.Warn("rate limit reached for " + key + " on " + ctx.Request.Method + " " + ctx.FullPath() + "; refusing its requests for up to " + window.String() + ".")
			}
			setRetryAfter(ctx, retryAfter)
			respond(ctx)
			ctx.Abort()
			return
		}
		ctx.Next()
	}
}

// RateLimit limits requests per client to max within a sliding window,
// returning 429 with an OAuth-style error body (for the /oauth endpoints).
func RateLimit(max int, window time.Duration) gin.HandlerFunc {
	return limitRequests(max, window, func(ctx *gin.Context) {
		ctx.JSON(http.StatusTooManyRequests, gin.H{
			"error":             "rate_limited",
			"error_description": "Too many requests. Please try again later.",
		})
	})
}

// APIRateLimit is RateLimit for the JSON API: the web app shows the "error"
// field of a failed request to the user, so it carries a readable message.
func APIRateLimit(max int, window time.Duration) gin.HandlerFunc {
	return limitRequests(max, window, func(ctx *gin.Context) {
		ctx.JSON(http.StatusTooManyRequests, gin.H{"error": "Too many attempts. Please wait a few minutes and try again."})
	})
}

// AttemptLimiter counts failed attempts per account rather than per address,
// so a password or a one-time code can't be guessed by spreading the attempts
// over many addresses. Once an account has used up its budget, sign-in for it
// is refused without checking anything until its oldest failure leaves the
// window. That also locks out the real user meanwhile, which is the price of
// stopping the guessing; the window keeps it short. A nil *AttemptLimiter
// (the limit turned off) never blocks.
type AttemptLimiter struct {
	limiter *rateLimiter
}

// NewAttemptLimiter allows max failures per key within window; a non-positive
// max turns it off.
func NewAttemptLimiter(max int, window time.Duration) *AttemptLimiter {
	if max <= 0 {
		return nil
	}
	return &AttemptLimiter{limiter: newRateLimiter(max, window)}
}

// Blocked reports whether key is locked, and for how long.
func (a *AttemptLimiter) Blocked(key string) (bool, time.Duration) {
	if a == nil {
		return false, 0
	}
	return a.limiter.blocked(key)
}

// Failed records a failed attempt for key, and reports whether this one
// locked it.
func (a *AttemptLimiter) Failed(key string) bool {
	if a == nil {
		return false
	}
	return a.limiter.record(key)
}

// Succeeded clears key's failures after a successful attempt.
func (a *AttemptLimiter) Succeeded(key string) {
	if a == nil {
		return
	}
	a.limiter.reset(key)
}

// RefuseLockedAttempt answers a request for a locked account with 429. The
// message doesn't say whether the account exists: unknown ones lock the same way.
func RefuseLockedAttempt(ctx *gin.Context, retryAfter time.Duration) {
	setRetryAfter(ctx, retryAfter)
	ctx.JSON(http.StatusTooManyRequests, gin.H{"error": "Too many failed attempts. Please wait a few minutes and try again."})
	ctx.Abort()
}
