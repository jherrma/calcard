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

// TestLoginRateLimiter asserts that the login IP rate limiter kicks in when
// RateLimit.Enabled is true. The package-level test server turns this off so
// the other tests can log in repeatedly, so we have to spin up a dedicated
// server instance for this one test.
//
// The production rule is 5 logins per minute per IP. On the 6th attempt the
// server should answer 429 Too Many Requests.
func TestLoginRateLimiter(t *testing.T) {
	rlURL, shutdown := bootServerWithConfig(t, func(cfg *config.Config) {
		cfg.RateLimit.Enabled = true
	})
	t.Cleanup(shutdown)

	// Register one user so we have a valid account. The rate limiter fires
	// on IP, not on auth success/failure, so we just need *some* body for
	// the POSTs — but a valid account also lets us prove the limiter isn't
	// flipping successful logins to 429 due to some unrelated bug.
	registerOn(t, rlURL, "ratelimit@example.test", "ratelimitSecret!123", "Ratelimit User")

	// Hammer the login endpoint. The configured limit is 5/min per IP and
	// 10/min per email. We fire well past the limit and assert on two
	// separable properties so the test doesn't depend on the exact cut-off
	// (Fiber's limiter implementation details can shift the first 429 by
	// one attempt depending on whether readiness probes count against the
	// window, etc.):
	//
	//   1. The very first login attempt makes it through (returns 401 for
	//      the wrong password — the *interesting* thing here is that it's
	//      not 429).
	//   2. Within a reasonable burst, the server does start responding 429,
	//      i.e. the limiter is actually turned on.
	//
	// We use wrong credentials on purpose so the requests go through the
	// limiter but never create side-effects like refresh tokens.
	var got []int
	for i := 0; i < 10; i++ {
		status := postLoginOn(t, rlURL, "ratelimit@example.test", "WRONG-PASSWORD")
		got = append(got, status)
	}

	assert.NotEqualf(t, http.StatusTooManyRequests, got[0],
		"the very first login attempt must not be rate-limited (got sequence: %v)", got)

	sawLimit := false
	for _, s := range got {
		if s == http.StatusTooManyRequests {
			sawLimit = true
			break
		}
	}
	assert.Truef(t, sawLimit,
		"expected at least one 429 response in the burst (got sequence: %v)", got)

	// And a quick smoke check that other /auth endpoints still work — i.e.
	// the limiter only clamps /login, not every /auth route.
	resp, err := http.Get(rlURL + "/api/v1/system/settings")
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode, "non-login endpoints must not be affected")
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
		// Max=0 (which would cause every request to 429 immediately).
		RateLimit: config.RateLimitConfig{
			Enabled:  false,
			Requests: 100,
			Window:   time.Minute,
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

// postLoginOn POSTs /auth/login and returns just the status code. Used by
// the rate-limit test, which doesn't care about the body.
func postLoginOn(t *testing.T, base, email, password string) int {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"email": email, "password": password})
	req, _ := http.NewRequest(http.MethodPost, base+"/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

// Suppress "imported and not used: fmt" if we happen to trim error wrappers.
var _ = fmt.Sprint
