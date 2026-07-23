//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/infrastructure/database"
	infraserver "github.com/jherrma/caldav-server/internal/infrastructure/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoginRateLimiter asserts the login limiters kick in when
// RateLimit.Enabled is true. The package-level test server turns this off so
// the other tests can log in repeatedly, so each subtest spins up a dedicated
// server instance (a fresh server means fresh, isolated limiter windows).
//
// Regression guard for #105: the per-IP allowance must sit ABOVE the per-email
// allowance so the tighter per-account (per-email) control is actually
// reachable from a single IP. When IP <= email the IP limiter always trips
// first and the per-email limiter is dead code from any one source address
// (which, behind a NAT/reverse proxy, is every client). The two subtests pin
// small explicit thresholds and assert that BOTH limiters are reachable and
// that each returns its own distinct 429 message.
func TestLoginRateLimiter(t *testing.T) {
	// The two limiters carry distinct messages (see rate_limiter.go); we key
	// off them to prove which limiter actually fired.
	const emailLimitMsg = "this account"   // NewEmailRateLimiter: per-account
	const ipLimitMsg = "Too many attempts" // NewIPRateLimiter: per-IP

	t.Run("per-email limiter reachable from a single IP", func(t *testing.T) {
		// IP=20 well above email=3, so from one IP the email limiter is the
		// first to trip for a stream of same-email requests.
		rlURL, shutdown := bootServerWithConfig(t, func(cfg *config.Config) {
			cfg.RateLimit.Enabled = true
			cfg.RateLimit.AuthIPRequests = 20
			cfg.RateLimit.AuthEmailRequests = 3
		})
		t.Cleanup(shutdown)

		// A valid account also proves the limiter isn't flipping otherwise-401
		// responses to 429 for some unrelated reason. We log in with the WRONG
		// password so requests traverse the limiter chain without creating
		// side-effects (refresh tokens).
		registerOn(t, rlURL, "ratelimit@example.test", "ratelimitSecret!123", "Ratelimit User")

		// Fire AuthEmailRequests+1 same-email attempts. The IP cap (20) is far
		// away, so the only limiter that can fire in this burst is the email
		// one — request 4 must be the first 429 and it must carry the
		// per-account message.
		var statuses []int
		var firstLimitedBody string
		for i := 0; i < 5; i++ {
			status, body := postLoginOn(t, rlURL, "ratelimit@example.test", "WRONG-PASSWORD")
			statuses = append(statuses, status)
			if status == http.StatusTooManyRequests && firstLimitedBody == "" {
				firstLimitedBody = string(body)
			}
		}

		assert.NotEqualf(t, http.StatusTooManyRequests, statuses[0],
			"the very first login attempt must not be rate-limited (got sequence: %v)", statuses)
		require.Containsf(t, statuses, http.StatusTooManyRequests,
			"expected the per-email limiter to fire within the burst (got sequence: %v)", statuses)
		assert.Containsf(t, firstLimitedBody, emailLimitMsg,
			"the 429 must come from the per-EMAIL limiter (got body: %q)", firstLimitedBody)
		assert.NotContainsf(t, firstLimitedBody, ipLimitMsg,
			"the IP limiter (allowance 20) must not have fired at request <=5 (got body: %q)", firstLimitedBody)
	})

	t.Run("IP limiter still trips on a spray across distinct emails", func(t *testing.T) {
		// Distinct emails never accumulate on any single per-email key, so the
		// per-IP limiter is the only thing that can stop an account-spraying
		// attacker. Pin a small IP cap so the test stays fast.
		rlURL, shutdown := bootServerWithConfig(t, func(cfg *config.Config) {
			cfg.RateLimit.Enabled = true
			cfg.RateLimit.AuthIPRequests = 5
			cfg.RateLimit.AuthEmailRequests = 10
		})
		t.Cleanup(shutdown)

		var statuses []int
		var firstLimitedBody string
		for i := 0; i < 7; i++ {
			email := fmt.Sprintf("spray-%d@example.test", i)
			status, body := postLoginOn(t, rlURL, email, "WRONG-PASSWORD")
			statuses = append(statuses, status)
			if status == http.StatusTooManyRequests && firstLimitedBody == "" {
				firstLimitedBody = string(body)
			}
		}

		assert.NotEqualf(t, http.StatusTooManyRequests, statuses[0],
			"the very first login attempt must not be rate-limited (got sequence: %v)", statuses)
		require.Containsf(t, statuses, http.StatusTooManyRequests,
			"expected the per-IP limiter to fire within the spray (got sequence: %v)", statuses)
		assert.Containsf(t, firstLimitedBody, ipLimitMsg,
			"the 429 must come from the per-IP limiter (got body: %q)", firstLimitedBody)
		assert.NotContainsf(t, firstLimitedBody, emailLimitMsg,
			"distinct-email spray must not trip the per-email limiter (got body: %q)", firstLimitedBody)

		// A quick smoke check that other /auth endpoints still work — i.e. the
		// limiter only clamps /login, not every /auth route.
		resp, err := http.Get(rlURL + "/api/v1/system/settings")
		require.NoError(t, err)
		_ = resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode, "non-login endpoints must not be affected")
	})
}

// --- local helpers ---------------------------------------------------------

// bootServerWithConfig spawns a fresh in-process server on a random port,
// applying the caller-provided tweak to the config before starting. Returns
// the base URL (http://host:port) and a shutdown function the caller should
// invoke via t.Cleanup. All state lives in a temp directory that's removed
// on shutdown so this helper is safe to call repeatedly in one test run.
func bootServerWithConfig(t *testing.T, tweak func(*config.Config)) (string, func()) {
	t.Helper()
	dataDir, err := os.MkdirTemp("", "calcard-ratelimit-*")
	require.NoError(t, err, "mkdir temp")

	cfg := &config.Config{
		Server:   config.ServerConfig{Host: "127.0.0.1", Port: "0"},
		Database: config.DatabaseConfig{Driver: "sqlite", AutoMigrate: true},
		DataDir:  dataDir,
		BaseURL:  "http://127.0.0.1",
		LogLevel: "error",
		SMTP:     config.SMTPConfig{},
		JWT: config.JWTConfig{
			Secret:        "ratelimit-test-secret-change-me",
			AccessExpiry:  time.Hour,
			RefreshExpiry: 24 * time.Hour,
			ResetExpiry:   15 * time.Minute,
		},
		// Global middleware limiter: 100/min/IP matches production defaults.
		// The caller's `tweak` can still flip Enabled on. Setting the numeric
		// fields ensures that a tweak enabling this limiter doesn't leave
		// Max=0. The auth-specific allowances mirror the production defaults
		// (per-IP ABOVE per-email so the per-email limiter is reachable from a
		// single IP); rate-limit tests override them to pin exact thresholds.
		RateLimit: config.RateLimitConfig{
			Enabled:           false,
			Requests:          100,
			Window:            time.Minute,
			AuthIPRequests:    20,
			AuthEmailRequests: 10,
		},
		Security: config.SecurityConfig{
			MaxRequestSize: 10 * 1024 * 1024,
			RequestTimeout: 30 * time.Second,
		},
	}
	if tweak != nil {
		tweak(cfg)
	}

	db, err := database.New(cfg)
	require.NoError(t, err)
	require.NoError(t, db.Migrate(database.Models()...))

	srv := infraserver.New(cfg, db)
	addr, err := srv.Start("127.0.0.1:0")
	require.NoError(t, err)
	base := "http://" + addr
	cfg.BaseURL = base

	require.NoError(t, waitForReady(base+"/health", 5*time.Second))

	shutdown := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		_ = db.Close()
		os.RemoveAll(dataDir)
	}
	return base, shutdown
}

// registerOn POSTs /auth/register against the given base URL.
func registerOn(t *testing.T, base, email, password, displayName string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"email": email, "password": password, "display_name": displayName,
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/api/v1/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equalf(t, http.StatusOK, resp.StatusCode, "register on %s", base)
	_, _ = io.Copy(io.Discard, resp.Body)
}

// postLoginOn POSTs /auth/login and returns the status code and raw response
// body. The rate-limit test inspects the body to tell the per-IP and per-email
// limiters apart by their distinct 429 messages.
func postLoginOn(t *testing.T, base, email, password string) (int, []byte) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"email": email, "password": password})
	req, _ := http.NewRequest(http.MethodPost, base+"/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}
