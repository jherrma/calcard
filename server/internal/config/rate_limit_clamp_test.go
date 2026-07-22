package config

import (
	"testing"

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
