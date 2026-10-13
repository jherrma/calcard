package http

import (
	"io"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The per-e-mail limiter keys on the address as the account lookups see it,
// trimmed and lower-cased, so case and whitespace variants of one address
// share its bucket (#193 item 3).
func TestEmailRateLimiterNormalisesTheAddress(t *testing.T) {
	app := fiber.New()
	app.Post("/login", ExtractEmailMiddleware(), NewEmailRateLimiter(2, time.Minute), func(c fiber.Ctx) error {
		return c.SendStatus(fiber.StatusNoContent)
	})
	login := func(email string) (int, string) {
		t.Helper()
		req := httptest.NewRequest(fiber.MethodPost, "/login", strings.NewReader(`{"email":`+strconv.Quote(email)+`,"password":"x"}`))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}

	status, _ := login("Foo@x")
	assert.Equal(t, fiber.StatusNoContent, status)
	status, _ = login("foo@x")
	assert.Equal(t, fiber.StatusNoContent, status)
	status, body := login(" foo@x ")
	assert.Equal(t, fiber.StatusTooManyRequests, status, "the third variant of one address is over its budget of 2")
	assert.Contains(t, body, tooManyAttemptsAccountMessage)

	status, _ = login("bar@x")
	assert.Equal(t, fiber.StatusNoContent, status, "another address keeps its own bucket")
}

// FailedAuthLimiter (#193): only settled failures count, checks in flight are
// capped separately, and a spent identifier opens again when its window ends.
// A check "in flight" is one admitted whose settle func has not been called
// yet, which the handler below models with an empty X-Outcome.
func TestFailedAuthLimiter(t *testing.T) {
	const max = 4 // failures per window; the pending cap is 2×max = 8
	now := time.Unix(1_700_000_000, 0)
	l := NewFailedAuthLimiter(max, time.Minute)
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
	// settleInFlight settles every check in flight with one outcome.
	settleInFlight := func(valid bool) {
		for _, settle := range inFlight {
			settle(valid)
		}
		inFlight = nil
	}

	t.Run("concurrent correct logins are not refused by failure accounting", func(t *testing.T) {
		// Twice the failure budget in flight at once, in case variants of one
		// identifier: every one is admitted.
		for i := 0; i < 2*max; i++ {
			id := "a@x"
			if i%2 == 1 {
				id = " A@X"
			}
			status, _, body := attempt(id, "")
			require.Equalf(t, fiber.StatusNoContent, status, "check %d in flight must be admitted: %s", i+1, body)
		}
		settleInFlight(true)

		// They counted nothing: the whole budget of failures is still there.
		for i := 0; i < max; i++ {
			status, _, _ := attempt("a@x", "invalid")
			require.Equalf(t, fiber.StatusNoContent, status, "failure %d is within the budget", i+1)
		}
		status, _, body := attempt("a@x", "valid")
		assert.Equal(t, fiber.StatusTooManyRequests, status, "max settled failures spend the identifier")
		assert.Contains(t, body, tooManyAttemptsAccountMessage)
	})

	t.Run("a failure counts when it settles, not while it is in flight", func(t *testing.T) {
		for i := 0; i < max; i++ {
			attempt("b@x", "")
		}
		status, _, _ := attempt("b@x", "valid")
		assert.Equal(t, fiber.StatusNoContent, status, "max checks in flight do not spend the identifier")

		settleInFlight(false)
		status, retryAfter, body := attempt("b@x", "valid")
		assert.Equal(t, fiber.StatusTooManyRequests, status, "once they fail they do, a correct password included")
		assert.Contains(t, body, tooManyAttemptsAccountMessage)
		assert.Equal(t, "60", retryAfter, "the window opened with the first failure")
	})

	t.Run("the pending cap bounds checks in flight", func(t *testing.T) {
		for i := 0; i < 2*max; i++ {
			status, _, _ := attempt("c@x", "")
			require.Equal(t, fiber.StatusNoContent, status)
		}
		status, retryAfter, body := attempt("c@x", "valid")
		assert.Equal(t, fiber.StatusTooManyRequests, status, "a check over the pending cap is refused")
		assert.Contains(t, body, tooManyAttemptsAccountMessage)
		assert.Equal(t, "1", retryAfter)
		status, _, _ = attempt("other@x", "valid")
		assert.Equal(t, fiber.StatusNoContent, status, "the cap is per identifier")

		inFlight[0](true)
		inFlight = inFlight[1:]
		status, _, _ = attempt("c@x", "valid")
		assert.Equal(t, fiber.StatusNoContent, status, "a settled check frees its place")
		settleInFlight(true)
	})

	t.Run("a fresh window resets the budget", func(t *testing.T) {
		for i := 0; i < max; i++ {
			attempt("d@x", "invalid")
		}
		now = now.Add(20 * time.Second)
		status, retryAfter, _ := attempt("d@x", "valid")
		assert.Equal(t, fiber.StatusTooManyRequests, status)
		assert.Equal(t, "40", retryAfter, "seconds left in the window")

		now = now.Add(40 * time.Second)
		status, _, _ = attempt("d@x", "valid")
		assert.Equal(t, fiber.StatusNoContent, status, "the window is over")
		for i := 0; i < max; i++ {
			status, _, _ := attempt("d@x", "invalid")
			require.Equalf(t, fiber.StatusNoContent, status, "failure %d of the new window", i+1)
		}
		status, _, _ = attempt("d@x", "valid")
		assert.Equal(t, fiber.StatusTooManyRequests, status, "the new window has its own budget of max")
	})

	t.Run("idle identifiers are swept", func(t *testing.T) {
		now = now.Add(2 * time.Minute)
		attempt("e@x", "valid")
		l.mu.Lock()
		defer l.mu.Unlock()
		assert.Empty(t, l.entries, "nothing in flight and no open window: no entry is kept")
	})
}
