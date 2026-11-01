package config

import (
	"aunefyren/poenskelisten/models"
	"errors"
	"strconv"
	"strings"
	"time"
)

// RateLimit is a budget of at most Max requests per Window. A zero Max means
// no limit.
type RateLimit struct {
	Max    int
	Window time.Duration
}

// Defaults for the rate limits, as written in the configuration. They are
// applied at runtime rather than persisted, so a later release can change them.
const (
	// DefaultLoginRateLimit is shared, per client address, by the password and
	// code entry points: login, the MFA step, registration, password reset and
	// e-mail verification. A login with MFA takes two requests.
	DefaultLoginRateLimit = "20/15m"
	// DefaultOAuthRateLimit is shared, per client address, by /oauth/authorize,
	// /oauth/consent, /oauth/token and /oauth/revoke. The web app refreshes
	// its token every ten minutes per open tab.
	DefaultOAuthRateLimit = "60/1m"
	// DefaultRegisterRateLimit applies, per client address, to dynamic client
	// registration (/oauth/register).
	DefaultRegisterRateLimit = "10/1h"
	// DefaultAccountRateLimit is how many failed sign-in attempts an account
	// takes before it is locked for the rest of the window: wrong passwords
	// per e-mail, and wrong second factors per user, from any address.
	DefaultAccountRateLimit = "10/15m"
	// DefaultUploadRateLimit is shared, per account, by the routes that can
	// carry an image: creating and editing a wish, and updating one's account.
	// Each image takes a moment of CPU and tens of megabytes to process.
	DefaultUploadRateLimit = "30/10m"
)

// ParseRateLimit reads a limit written as "<requests>/<duration>", e.g.
// "20/15m" or "10/1h" (durations as in Go: s, m, h). "off" or "0" turns the
// limit off.
func ParseRateLimit(raw string) (RateLimit, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "off" || value == "0" {
		return RateLimit{}, nil
	}
	count, duration, found := strings.Cut(value, "/")
	if !found {
		return RateLimit{}, errors.New("'" + raw + "' must look like 20/15m, or be off")
	}
	max, err := strconv.Atoi(strings.TrimSpace(count))
	if err != nil || max < 1 {
		return RateLimit{}, errors.New("'" + raw + "' must start with a number of requests of at least 1")
	}
	window, err := time.ParseDuration(strings.TrimSpace(duration))
	if err != nil || window < time.Second {
		return RateLimit{}, errors.New("'" + raw + "' must end with a duration of at least 1s, like 30s, 15m or 1h")
	}
	return RateLimit{Max: max, Window: window}, nil
}

// rateLimitOrDefault parses a configured limit, falling back to the default
// when it's unset. Invalid values are refused at startup (ValidateRateLimits),
// so the fallback for them only matters to code that skips that check.
func rateLimitOrDefault(configured string, fallback string) RateLimit {
	if strings.TrimSpace(configured) != "" {
		if limit, err := ParseRateLimit(configured); err == nil {
			return limit
		}
	}
	limit, _ := ParseRateLimit(fallback)
	return limit
}

// LoginRateLimit is the per-address limit on the password and code entry points.
func LoginRateLimit(configFile models.ConfigStruct) RateLimit {
	return rateLimitOrDefault(configFile.RateLimitLogin, DefaultLoginRateLimit)
}

// OAuthRateLimit is the per-address limit on the OAuth authorization and token endpoints.
func OAuthRateLimit(configFile models.ConfigStruct) RateLimit {
	return rateLimitOrDefault(configFile.RateLimitOAuth, DefaultOAuthRateLimit)
}

// RegisterRateLimit is the per-address limit on dynamic client registration.
func RegisterRateLimit(configFile models.ConfigStruct) RateLimit {
	return rateLimitOrDefault(configFile.RateLimitRegister, DefaultRegisterRateLimit)
}

// ValidateRateLimits reports the first configured rate limit that can't be
// parsed, named after its config.json entry.
func ValidateRateLimits(configFile models.ConfigStruct) error {
	settings := []struct{ name, value string }{
		{"rate_limit_login", configFile.RateLimitLogin},
		{"rate_limit_oauth", configFile.RateLimitOAuth},
		{"rate_limit_register", configFile.RateLimitRegister},
		{"rate_limit_account", configFile.RateLimitAccount},
		{"rate_limit_upload", configFile.RateLimitUpload},
	}
	for _, setting := range settings {
		if strings.TrimSpace(setting.value) == "" {
			continue
		}
		if _, err := ParseRateLimit(setting.value); err != nil {
			return errors.New(setting.name + ": " + err.Error())
		}
	}
	return nil
}

// AccountRateLimit is the per-account limit on failed sign-in attempts.
func AccountRateLimit(configFile models.ConfigStruct) RateLimit {
	return rateLimitOrDefault(configFile.RateLimitAccount, DefaultAccountRateLimit)
}

// UploadRateLimit is the per-account limit on the routes that can carry an image.
func UploadRateLimit(configFile models.ConfigStruct) RateLimit {
	return rateLimitOrDefault(configFile.RateLimitUpload, DefaultUploadRateLimit)
}
