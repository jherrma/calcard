package middleware

import (
	"fmt"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/limiter"
	"github.com/jherrma/caldav-server/internal/config"
)

// GlobalRateLimiter creates a global rate limiter middleware
func GlobalRateLimiter(cfg config.RateLimitConfig) fiber.Handler {
	if !cfg.Enabled {
		return func(c fiber.Ctx) error {
			return c.Next()
		}
	}

	return limiter.New(limiter.Config{
		Max:        cfg.Requests,
		Expiration: cfg.Window,
		KeyGenerator: func(c fiber.Ctx) string {
			return c.IP()
		},
		LimitReached: func(c fiber.Ctx) error {
			retryAfter := cfg.Window.Seconds()
			c.Set("Retry-After", fmt.Sprintf("%.0f", retryAfter))
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"error":       "rate_limit_exceeded",
				"message":     "Too many requests. Please try again later.",
				"retry_after": retryAfter,
			})
		},
	})
}
