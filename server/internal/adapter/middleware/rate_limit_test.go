package middleware

import (
	"encoding/json"
	"net/http/httptest"
	"strconv"
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

// TestGlobalRateLimiterRetryAfterIsTimeLeft: Retry-After (and the body's
// retry_after) must be the seconds left in the current window, not the whole
// window. The handler used to overwrite Fiber's accurate value with the full
// window, so a client throttled seconds before the reset was told to wait a
// minute and the web UI never retried.
func TestGlobalRateLimiterRetryAfterIsTimeLeft(t *testing.T) {
	const window = 4 * time.Second
	app := fiber.New()
	app.Use(GlobalRateLimiter(config.RateLimitConfig{Enabled: true, Requests: 1, Window: window}))
	app.All("/*", func(c fiber.Ctx) error { return c.SendString("ok") })

	if got := status(t, app, "GET", "/api/v1/calendars"); got != 200 {
		t.Fatalf("first request: %d", got)
	}
	// Let part of the window elapse (Fiber's limiter clock ticks per second).
	time.Sleep(2100 * time.Millisecond)

	resp, err := app.Test(httptest.NewRequest("GET", "/api/v1/calendars", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 429 {
		t.Fatalf("second request: want 429, got %d", resp.StatusCode)
	}
	header, err := strconv.Atoi(resp.Header.Get("Retry-After"))
	if err != nil {
		t.Fatalf("Retry-After %q: %v", resp.Header.Get("Retry-After"), err)
	}
	if header < 1 || header >= int(window.Seconds()) {
		t.Fatalf("Retry-After = %d, want the time left in the window (1..%d)", header, int(window.Seconds())-1)
	}
	var body struct {
		RetryAfter int `json:"retry_after"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.RetryAfter != header {
		t.Fatalf("body retry_after = %d, header = %d; they must agree", body.RetryAfter, header)
	}
}
