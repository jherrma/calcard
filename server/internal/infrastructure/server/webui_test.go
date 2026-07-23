package server

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SCOPE: registerWebUI, the single-container SPA serving added for #99. It must
// (a) do nothing when ./public/index.html is absent (dev / unit-test path), and
// (b) when present, serve static assets, fall back to index.html for unknown
// client routes (SPA deep-linking), yet keep API/DAV/well-known paths 404-ing so
// they never leak HTML. An /api route registered before it must still win.
func TestRegisterWebUI_NoopWhenMissing(t *testing.T) {
	app := fiber.New()
	registered := registerWebUI(app, filepath.Join(t.TempDir(), "does-not-exist"))
	assert.False(t, registered, "must not register static serving without an index.html")
}

func TestRegisterWebUI_ServesSPAWithApiFallthrough(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html><title>App</title>"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "_nuxt"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "_nuxt", "app.js"), []byte("console.log(1)"), 0o644))

	app := fiber.New()
	// An API route registered BEFORE the catch-all must still win.
	app.Get("/api/v1/health", func(c fiber.Ctx) error { return c.JSON(fiber.Map{"ok": true}) })

	require.True(t, registerWebUI(app, dir))

	do := func(path string) *http.Response {
		req, _ := http.NewRequest("GET", path, nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		return resp
	}

	t.Run("real asset is served", func(t *testing.T) {
		resp := do("/_nuxt/app.js")
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)
	})

	t.Run("root serves index.html", func(t *testing.T) {
		resp := do("/")
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)
		assert.Contains(t, resp.Header.Get("Content-Type"), "text/html")
	})

	t.Run("unknown client route falls back to index.html", func(t *testing.T) {
		resp := do("/contacts/42")
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)
		assert.Contains(t, resp.Header.Get("Content-Type"), "text/html")
	})

	t.Run("registered API route still wins", func(t *testing.T) {
		resp := do("/api/v1/health")
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)
		assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")
	})

	t.Run("unknown API path 404s, never HTML", func(t *testing.T) {
		resp := do("/api/v1/nonexistent")
		assert.Equal(t, fiber.StatusNotFound, resp.StatusCode)
		assert.NotContains(t, resp.Header.Get("Content-Type"), "text/html")
	})

	t.Run("unknown DAV path 404s, never HTML", func(t *testing.T) {
		resp := do("/dav/whatever")
		assert.Equal(t, fiber.StatusNotFound, resp.StatusCode)
		assert.NotContains(t, resp.Header.Get("Content-Type"), "text/html")
	})

	t.Run("unknown well-known path 404s, never HTML", func(t *testing.T) {
		resp := do("/.well-known/carddav")
		assert.Equal(t, fiber.StatusNotFound, resp.StatusCode)
		assert.NotContains(t, resp.Header.Get("Content-Type"), "text/html")
	})
}
