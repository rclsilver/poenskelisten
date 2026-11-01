package config

import (
	"strings"
	"testing"
	"time"
)

func TestParseRateLimit(t *testing.T) {
	cases := []struct {
		raw     string
		want    RateLimit
		wantErr bool
	}{
		{"20/15m", RateLimit{Max: 20, Window: 15 * time.Minute}, false},
		{" 10 / 1h ", RateLimit{Max: 10, Window: time.Hour}, false},
		{"5/30s", RateLimit{Max: 5, Window: 30 * time.Second}, false},
		{"off", RateLimit{}, false},
		{"OFF", RateLimit{}, false},
		{"0", RateLimit{}, false},
		{"20", RateLimit{}, true},
		{"0/15m", RateLimit{}, true},
		{"-1/15m", RateLimit{}, true},
		{"many/15m", RateLimit{}, true},
		{"20/forever", RateLimit{}, true},
		{"20/100ms", RateLimit{}, true},
		{"", RateLimit{}, true},
	}
	for _, c := range cases {
		t.Run(c.raw, func(t *testing.T) {
			got, err := ParseRateLimit(c.raw)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, c.wantErr)
			}
			if got != c.want {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestRateLimitsFallBackToDefaults(t *testing.T) {
	withTempConfigFile(t)

	if got := LoginRateLimit(ConfigFile); got != (RateLimit{Max: 20, Window: 15 * time.Minute}) {
		t.Errorf("LoginRateLimit = %+v, want the 20/15m default", got)
	}
	if got := OAuthRateLimit(ConfigFile); got != (RateLimit{Max: 60, Window: time.Minute}) {
		t.Errorf("OAuthRateLimit = %+v, want the 60/1m default", got)
	}
	if got := AccountRateLimit(ConfigFile); got != (RateLimit{Max: 10, Window: 15 * time.Minute}) {
		t.Errorf("AccountRateLimit = %+v, want the 10/15m default", got)
	}
	if got := UploadRateLimit(ConfigFile); got != (RateLimit{Max: 30, Window: 10 * time.Minute}) {
		t.Errorf("UploadRateLimit = %+v, want the 30/10m default", got)
	}
	if got := RegisterRateLimit(ConfigFile); got != (RateLimit{Max: 10, Window: time.Hour}) {
		t.Errorf("RegisterRateLimit = %+v, want the 10/1h default", got)
	}

	ConfigFile.RateLimitLogin = "5/1m"
	ConfigFile.RateLimitOAuth = "off"
	if got := LoginRateLimit(ConfigFile); got != (RateLimit{Max: 5, Window: time.Minute}) {
		t.Errorf("LoginRateLimit = %+v, want the configured 5/1m", got)
	}
	if got := OAuthRateLimit(ConfigFile); got.Max != 0 {
		t.Errorf("OAuthRateLimit = %+v, want it off", got)
	}
}

func TestValidateRateLimits(t *testing.T) {
	withTempConfigFile(t)
	if err := ValidateRateLimits(ConfigFile); err != nil {
		t.Errorf("unset limits should be valid, got %v", err)
	}

	ConfigFile.RateLimitRegister = "lots"
	err := ValidateRateLimits(ConfigFile)
	if err == nil || !strings.Contains(err.Error(), "rate_limit_register") {
		t.Errorf("err = %v, want one naming rate_limit_register", err)
	}
}
