package http

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resolveBaseURL runs RequestBaseURL inside a real Fiber request carrying the
// given Host and extra headers, and returns what it produced.
func resolveBaseURL(t *testing.T, cfg *config.Config, host string, headers map[string]string) string {
	t.Helper()
	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error {
		return c.SendString(RequestBaseURL(c, cfg))
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = host
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(b)
}

func TestRequestBaseURL_ConfiguredValueWins(t *testing.T) {
	cfg := &config.Config{BaseURL: "https://cal.example.com/"}
	got := resolveBaseURL(t, cfg, "attacker.example", map[string]string{
		"X-Forwarded-Proto": "http",
		"X-Forwarded-Host":  "attacker.example",
	})
	assert.Equal(t, "https://cal.example.com", got, "configured base_url, trailing slash trimmed, request ignored")
}

func TestRequestBaseURL_DerivedFromHostWhenUnset(t *testing.T) {
	cfg := &config.Config{}
	assert.Equal(t, "http://192.168.178.36:8751", resolveBaseURL(t, cfg, "192.168.178.36:8751", nil))
	assert.Equal(t, "http://nas.local", resolveBaseURL(t, cfg, "nas.local", nil))
	assert.Equal(t, "http://[fd00::1]:8751", resolveBaseURL(t, cfg, "[fd00::1]:8751", nil))
}

// Forwarded headers are client-controlled unless a proxy overwrites them, so
// by default they must not influence the URL at all.
func TestRequestBaseURL_IgnoresForwardedHeadersByDefault(t *testing.T) {
	cfg := &config.Config{}
	got := resolveBaseURL(t, cfg, "nas.local:8751", map[string]string{
		"X-Forwarded-Proto": "https",
		"X-Forwarded-Host":  "attacker.example",
	})
	assert.Equal(t, "http://nas.local:8751", got)
}

func TestRequestBaseURL_HonoursForwardedHeadersWhenTrusted(t *testing.T) {
	cfg := &config.Config{TrustForwardedHeaders: true}

	got := resolveBaseURL(t, cfg, "calcard:8080", map[string]string{
		"X-Forwarded-Proto": "HTTPS",
		"X-Forwarded-Host":  "cal.example.com, internal-proxy",
	})
	assert.Equal(t, "https://cal.example.com", got, "first (client-facing) entry of each header")

	got = resolveBaseURL(t, cfg, "calcard:8080", map[string]string{"X-Forwarded-Proto": "https"})
	assert.Equal(t, "https://calcard:8080", got, "proto alone keeps the Host header")

	got = resolveBaseURL(t, cfg, "calcard:8080", map[string]string{
		"X-Forwarded-Proto": "javascript",
		"X-Forwarded-Host":  "evil.example/path@x",
	})
	assert.Equal(t, "http://calcard:8080", got, "unusable forwarded values are ignored")
}

// Go's HTTP client refuses to send most malformed Host headers, so the
// pattern that guards both Host and X-Forwarded-Host is checked directly.
func TestHostPattern(t *testing.T) {
	for _, ok := range []string{"nas.local", "nas.local:8751", "192.168.178.36:8751", "[fd00::1]", "[fd00::1]:443", "localhost"} {
		assert.Truef(t, hostPattern.MatchString(ok), "%q should be accepted", ok)
	}
	for _, bad := range []string{"", "evil.example/x", "user@evil.example", "a b", "-bad.example", "evil.example\\x", "host:port", "host:123456", "a.example,b.example"} {
		assert.Falsef(t, hostPattern.MatchString(bad), "%q should be rejected", bad)
	}
}
