package http

import (
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/limiter"
)

// RateLimiterConfig defines the configuration for rate limiting
type RateLimiterConfig struct {
	Enabled bool
}

// NewIPRateLimiter creates a rate limiter based on IP address
func NewIPRateLimiter(max int, expiration time.Duration) fiber.Handler {
	return limiter.New(limiter.Config{
		Max:        max,
		Expiration: expiration,
		KeyGenerator: func(c fiber.Ctx) string {
			return c.IP()
		},
		LimitReached: func(c fiber.Ctx) error {
			return ErrorResponse(c, fiber.StatusTooManyRequests, "Too many attempts. Please try again later.")
		},
	})
}

// ExtractEmailMiddleware parses the email from the request body and stores it in Locals
func ExtractEmailMiddleware() fiber.Handler {
	return func(c fiber.Ctx) error {
		var body struct {
			Email string `json:"email"`
		}
		// Use Body() and json.Unmarshal to avoid consuming the stream if needed,
		// but Fiber v3 Bind might be okay if it supports multiple reads or if we reset it.
		// Actually, let's just use c.Body() and simple parsing.
		if err := c.Bind().JSON(&body); err == nil {
			c.Locals("login_email", body.Email)
		}
		return c.Next()
	}
}

// NewEmailRateLimiter creates a rate limiter based on email address
func NewEmailRateLimiter(max int, expiration time.Duration) fiber.Handler {
	return limiter.New(limiter.Config{
		Max:        max,
		Expiration: expiration,
		KeyGenerator: func(c fiber.Ctx) string {
			email, ok := c.Locals("login_email").(string)
			if !ok || email == "" {
				return c.IP() // Fallback to IP if email missing
			}
			return email
		},
		LimitReached: func(c fiber.Ctx) error {
			return ErrorResponse(c, fiber.StatusTooManyRequests, "Too many login attempts for this account. Please try again later.")
		},
	})
}
