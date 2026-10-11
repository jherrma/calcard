package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoadClampsNonPositiveAuthRateLimits verifies that a misconfigured
// non-positive auth rate-limit threshold (0 or negative) is clamped back to
// its intended default instead of falling through to Fiber's Max<=0 -> 5
// fallback, which would collapse both auth limiters to 5/5 and break the
// per-IP > per-email ordering guarantee.
func TestLoadClampsNonPositiveAuthRateLimits(t *testing.T) {
	// Neutralize any DB env leaked by sibling (non-hermetic) tests so Load("")
	// stays on the sqlite defaults and doesn't hit the postgres validation.
	// t.Setenv keeps this hermetic — the values are restored on test cleanup.
	t.Setenv("CALDAV_DB_DRIVER", "sqlite")
	t.Setenv("CALDAV_DB_HOST", "")

	t.Run("zero clamps to defaults", func(t *testing.T) {
		t.Setenv("CALDAV_RATE_LIMIT_AUTH_IP_REQUESTS", "0")
		t.Setenv("CALDAV_RATE_LIMIT_AUTH_EMAIL_REQUESTS", "0")

		cfg, err := Load("")
		require.NoError(t, err)
		assert.Equal(t, 20, cfg.RateLimit.AuthIPRequests)
		assert.Equal(t, 10, cfg.RateLimit.AuthEmailRequests)
		// Ordering guarantee: per-IP must stay strictly above per-email.
		assert.Greater(t, cfg.RateLimit.AuthIPRequests, cfg.RateLimit.AuthEmailRequests)
	})

	t.Run("negative clamps to defaults", func(t *testing.T) {
		t.Setenv("CALDAV_RATE_LIMIT_AUTH_IP_REQUESTS", "-5")
		t.Setenv("CALDAV_RATE_LIMIT_AUTH_EMAIL_REQUESTS", "-1")

		cfg, err := Load("")
		require.NoError(t, err)
		assert.Equal(t, 20, cfg.RateLimit.AuthIPRequests)
		assert.Equal(t, 10, cfg.RateLimit.AuthEmailRequests)
		assert.Greater(t, cfg.RateLimit.AuthIPRequests, cfg.RateLimit.AuthEmailRequests)
	})

	t.Run("explicit positive values are preserved", func(t *testing.T) {
		t.Setenv("CALDAV_RATE_LIMIT_AUTH_IP_REQUESTS", "30")
		t.Setenv("CALDAV_RATE_LIMIT_AUTH_EMAIL_REQUESTS", "15")

		cfg, err := Load("")
		require.NoError(t, err)
		assert.Equal(t, 30, cfg.RateLimit.AuthIPRequests)
		assert.Equal(t, 15, cfg.RateLimit.AuthEmailRequests)
	})
}

// TestLoadGlobalRateLimitDefault pins the global per-IP default. It used to be
// 100/min, which every client behind one NAT shares; the web UI alone spends
// several requests per page, so households hit 429 during normal use.
func TestLoadGlobalRateLimitDefault(t *testing.T) {
	t.Setenv("CALDAV_DB_DRIVER", "sqlite")
	t.Setenv("CALDAV_DB_HOST", "")
	t.Setenv("CALDAV_RATE_LIMIT_REQUESTS", "")

	cfg, err := Load("")
	require.NoError(t, err)
	assert.True(t, cfg.RateLimit.Enabled)
	assert.Equal(t, 5000, cfg.RateLimit.Requests)
	assert.Equal(t, time.Minute, cfg.RateLimit.Window)
	// The auth limiters are unaffected by the global default.
	assert.Equal(t, 20, cfg.RateLimit.AuthIPRequests)
	assert.Equal(t, 10, cfg.RateLimit.AuthEmailRequests)
}

// TestLoadClampsNonPositiveGlobalRateLimit: Fiber's limiter treats Max<=0 as
// 5, so an operator writing 0 would get the tightest limit, not none.
func TestLoadClampsNonPositiveGlobalRateLimit(t *testing.T) {
	t.Setenv("CALDAV_DB_DRIVER", "sqlite")
	t.Setenv("CALDAV_DB_HOST", "")

	for _, v := range []string{"0", "-1"} {
		t.Run(v, func(t *testing.T) {
			t.Setenv("CALDAV_RATE_LIMIT_REQUESTS", v)
			cfg, err := Load("")
			require.NoError(t, err)
			assert.Equal(t, DefaultRateLimitRequests, cfg.RateLimit.Requests)
		})
	}

	t.Run("explicit positive value is preserved", func(t *testing.T) {
		t.Setenv("CALDAV_RATE_LIMIT_REQUESTS", "250")
		cfg, err := Load("")
		require.NoError(t, err)
		assert.Equal(t, 250, cfg.RateLimit.Requests)
	})
}
