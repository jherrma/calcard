package middleware

import (
	"fmt"
	"strings"

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
		// The web UI is served from the same listener. One page load fetches
		// dozens of scripts, styles and images, so counting them would burn
		// the allowance before the user has done anything (the first
		// dashboard load after setup tripped it).
		Next:       isStaticAssetRequest,
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

// isStaticAssetRequest reports whether the request is a GET/HEAD for the
// bundled web UI rather than an API, DAV, MCP or public-feed endpoint. It is
// deliberately an allow-list of what stays limited, so a new endpoint prefix
// is limited by default only if it is added here.
func isStaticAssetRequest(c fiber.Ctx) bool {
	if m := c.Method(); m != fiber.MethodGet && m != fiber.MethodHead {
		return false
	}
	p := c.Path()
	for _, prefix := range []string{"/api/", "/dav/", "/.well-known/", "/public/", "/mcp"} {
		if strings.HasPrefix(p, prefix) {
			return false
		}
	}
	return p != "/health"
}
