package middleware

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/config"
)

func status(t *testing.T, app *fiber.App, method, path string) int {
	t.Helper()
	resp, err := app.Test(httptest.NewRequest(method, path, nil))
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode
}

func TestGlobalRateLimiterSkipsStaticAssets(t *testing.T) {
	app := fiber.New()
	app.Use(GlobalRateLimiter(config.RateLimitConfig{Enabled: true, Requests: 3, Window: time.Minute}))
	app.All("/*", func(c fiber.Ctx) error { return c.SendString("ok") })

	for i := 0; i < 20; i++ {
		if got := status(t, app, "GET", "/_nuxt/app.js"); got != 200 {
			t.Fatalf("static asset request %d limited: %d", i, got)
		}
	}
	for i := 0; i < 3; i++ {
		if got := status(t, app, "POST", "/api/v1/auth/login"); got != 200 {
			t.Fatalf("api request %d: %d", i, got)
		}
	}
	if got := status(t, app, "POST", "/api/v1/auth/login"); got != 429 {
		t.Fatalf("api request over the limit: want 429, got %d", got)
	}
	if got := status(t, app, "GET", "/api/v1/calendars"); got != 429 {
		t.Fatalf("api GET must stay limited: got %d", got)
	}
}
