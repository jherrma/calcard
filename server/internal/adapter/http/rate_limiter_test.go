package http

import (
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// FailedAuthLimiter (#193): what the DAV integration test cannot reach —
// attempts still being checked already count, a valid outcome gives them
// back, and a spent key opens again when its window ends.
func TestFailedAuthLimiter(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	l := NewFailedAuthLimiter(4, 2, time.Minute)
	l.now = func() time.Time { return now }

	var inFlight []func(bool)
	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error {
		settle, err := l.Admit(c, c.Get("X-Identifier"))
		if settle == nil {
			return err
		}
		switch c.Get("X-Outcome") {
		case "valid":
			settle(true)
		case "invalid":
			settle(false)
		default: // still being checked
			inFlight = append(inFlight, settle)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	attempt := func(identifier, outcome string) (int, string, string) {
		t.Helper()
		req := httptest.NewRequest(fiber.MethodGet, "/", nil)
		req.Header.Set("X-Identifier", identifier)
		req.Header.Set("X-Outcome", outcome)
		resp, err := app.Test(req)
		require.NoError(t, err)
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, resp.Header.Get(fiber.HeaderRetryAfter), string(body)
	}

	t.Run("parallel attempts count before they are checked", func(t *testing.T) {
		attempt("a@x", "")
		attempt("A@x ", "")
		status, _, body := attempt("a@x", "valid")
		assert.Equal(t, fiber.StatusTooManyRequests, status, "two attempts in flight spend the identifier")
		assert.Contains(t, body, tooManyAttemptsAccountMessage)

		for _, settle := range inFlight {
			settle(true)
		}
		status, _, _ = attempt("a@x", "valid")
		assert.Equal(t, fiber.StatusNoContent, status, "valid attempts are given back")
	})

	t.Run("a spent identifier opens when its window ends", func(t *testing.T) {
		attempt("b@x", "invalid")
		attempt("b@x", "invalid")
		now = now.Add(20 * time.Second)
		status, retryAfter, _ := attempt("b@x", "valid")
		assert.Equal(t, fiber.StatusTooManyRequests, status)
		assert.Equal(t, "40", retryAfter, "seconds left in the window")

		now = now.Add(40 * time.Second)
		status, _, _ = attempt("b@x", "valid")
		assert.Equal(t, fiber.StatusNoContent, status)
	})

	t.Run("the IP allowance covers every identifier", func(t *testing.T) {
		for _, id := range []string{"c1@x", "c2@x", "c3@x", "c4@x"} {
			status, _, _ := attempt(id, "invalid")
			require.Equal(t, fiber.StatusNoContent, status)
		}
		status, _, body := attempt("c5@x", "valid")
		assert.Equal(t, fiber.StatusTooManyRequests, status)
		assert.Contains(t, body, tooManyAttemptsIPMessage)
	})
}
