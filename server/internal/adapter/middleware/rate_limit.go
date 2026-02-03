package middleware

import (
	"fmt"
	"time"

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

// LoginRateLimiter creates a strict rate limiter for login attempts
func LoginRateLimiter() fiber.Handler {
	return limiter.New(limiter.Config{
		Max:        5,
		Expiration: 1 * time.Minute,
		KeyGenerator: func(c fiber.Ctx) string {
			return c.IP()
		},
		LimitReached: func(c fiber.Ctx) error {
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"error":   "rate_limit_exceeded",
				"message": "Too many login attempts. Please try again later.",
			})
		},
	})
}

// PasswordResetRateLimiter creates a strict rate limiter for password reset requests
func PasswordResetRateLimiter() fiber.Handler {
	return limiter.New(limiter.Config{
		Max:        3,
		Expiration: 1 * time.Hour,
		KeyGenerator: func(c fiber.Ctx) string {
			// Rate limit by IP for simplicity, could parse body for email
			return c.IP()
		},
		LimitReached: func(c fiber.Ctx) error {
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"error":   "rate_limit_exceeded",
				"message": "Too many password reset attempts. Please try again later.",
			})
		},
	})
}
