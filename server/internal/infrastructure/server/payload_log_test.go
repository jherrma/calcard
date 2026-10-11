package server

import (
	"bytes"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SCOPE: PayloadLogger. DAV bodies must be logged so odd client payloads can be
// inspected; credentials must never be; non-DAV bodies (login passwords) must
// not be.
func TestPayloadLogger(t *testing.T) {
	var buf bytes.Buffer
	app := fiber.New(fiber.Config{RequestMethods: append(fiber.DefaultMethods, "PROPFIND")})
	app.Use(PayloadLogger(slog.New(slog.NewTextHandler(&buf, nil))))
	app.All("/*", func(c fiber.Ctx) error { return c.SendStatus(207) })

	do := func(method, path, body string) {
		req, _ := http.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Basic c2VjcmV0")
		req.Header.Set("User-Agent", "DAVx5/4.4")
		_, err := app.Test(req)
		require.NoError(t, err)
	}

	do("PROPFIND", "/dav/calendars/u/", "<propfind>weird-ns</propfind>")
	out := buf.String()
	assert.Contains(t, out, "weird-ns")
	assert.Contains(t, out, "PROPFIND")
	assert.Contains(t, out, "DAVx5/4.4")
	assert.Contains(t, out, "status=207")
	assert.NotContains(t, out, "c2VjcmV0", "Authorization must be redacted")

	buf.Reset()
	do("POST", "/api/v1/auth/login", `{"password":"hunter2"}`)
	assert.NotContains(t, buf.String(), "hunter2", "non-DAV bodies must not be logged")
	assert.Contains(t, buf.String(), "/api/v1/auth/login")
}
