//go:build integration

package integration_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/jherrma/caldav-server/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSecurityHeaders spins up a dedicated server with Security.Enabled=true
// and verifies that the helmet-configured response headers actually appear
// on responses. Without this, a regression in middleware ordering (e.g.
// app.Use order, early-returns, etc.) can silently strip these headers.
func TestSecurityHeaders(t *testing.T) {
	base, shutdown := bootServerWithConfig(t, func(cfg *config.Config) {
		cfg.Security.Enabled = true
		cfg.Security.HSTSEnabled = false // not asserted — requires TLS
	})
	t.Cleanup(shutdown)

	resp, err := http.Get(base + "/health")
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Each of these comes from the helmet config in `infrastructure/
	// server/middleware.go`. If you disable helmet or change its config,
	// update this assertion rather than deleting it — the test guards the
	// production default, not every specific value.
	assert.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"),
		"X-Content-Type-Options must be nosniff")
	assert.Equal(t, "DENY", resp.Header.Get("X-Frame-Options"),
		"X-Frame-Options must be DENY")
	assert.Equal(t, "strict-origin-when-cross-origin", resp.Header.Get("Referrer-Policy"))
	assert.Contains(t, resp.Header.Get("Permissions-Policy"), "geolocation=",
		"Permissions-Policy must restrict at least geolocation")

	// Cross-origin embedding headers.
	assert.Equal(t, "same-origin", resp.Header.Get("Cross-Origin-Opener-Policy"))
	assert.Equal(t, "require-corp", resp.Header.Get("Cross-Origin-Embedder-Policy"))
}

// TestCORSPreflightAndSimpleRequest proves the CORS middleware (a) returns
// the configured Allow-Origin on a real request and (b) answers OPTIONS
// preflights with the full Access-Control-* set needed for browser clients.
func TestCORSPreflightAndSimpleRequest(t *testing.T) {
	allowedOrigin := "https://webinterface.example"
	base, shutdown := bootServerWithConfig(t, func(cfg *config.Config) {
		cfg.CORS.Enabled = true
		cfg.CORS.AllowedOrigins = []string{allowedOrigin}
		cfg.CORS.AllowCredentials = true
		cfg.CORS.ExposeHeaders = []string{"ETag", "DAV"}
		cfg.CORS.MaxAge = 3600
	})
	t.Cleanup(shutdown)

	// --- Preflight ------------------------------------------------------
	req, _ := http.NewRequest(http.MethodOptions, base+"/api/v1/system/settings", nil)
	req.Header.Set("Origin", allowedOrigin)
	req.Header.Set("Access-Control-Request-Method", "GET")
	req.Header.Set("Access-Control-Request-Headers", "Authorization, Content-Type")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()

	// CORS preflights normally come back 204 No Content.
	assert.Equalf(t, http.StatusNoContent, resp.StatusCode,
		"OPTIONS preflight should be 204, got %d", resp.StatusCode)
	assert.Equal(t, allowedOrigin, resp.Header.Get("Access-Control-Allow-Origin"),
		"Allow-Origin must echo the configured origin")
	assert.Equal(t, "true", resp.Header.Get("Access-Control-Allow-Credentials"))
	assert.Contains(t, resp.Header.Get("Access-Control-Allow-Methods"), "PROPFIND",
		"Allow-Methods must include WebDAV methods so DAV clients can preflight")

	// --- Simple request -------------------------------------------------
	req, _ = http.NewRequest(http.MethodGet, base+"/api/v1/system/settings", nil)
	req.Header.Set("Origin", allowedOrigin)
	resp, err = client.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, allowedOrigin, resp.Header.Get("Access-Control-Allow-Origin"),
		"Allow-Origin must be set on the actual response too, not just preflights")
	// Expose-Headers must contain the ones we configured so the frontend
	// can read ETag / DAV off the fetch response.
	expose := resp.Header.Get("Access-Control-Expose-Headers")
	assert.Contains(t, expose, "ETag")
	assert.Contains(t, expose, "DAV")
}
