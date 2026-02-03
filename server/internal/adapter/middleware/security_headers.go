package middleware

import (
	"fmt"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/helmet"
	"github.com/jherrma/caldav-server/internal/config"
)

// SecurityHeadersMiddleware creates a middleware that sets security headers
func SecurityHeadersMiddleware(cfg config.SecurityConfig) fiber.Handler {
	if !cfg.Enabled {
		return func(c fiber.Ctx) error {
			return c.Next()
		}
	}

	hstshHandler := func(c fiber.Ctx) error { return c.Next() }

	if cfg.HSTSEnabled {
		hstshHandler = func(c fiber.Ctx) error {
			if c.Protocol() == "https" {
				c.Set("Strict-Transport-Security", fmt.Sprintf("max-age=%d; includeSubDomains", cfg.HSTSMaxAge))
			}
			return c.Next()
		}
	}

	helmetHandler := helmet.New(helmet.Config{
		XSSProtection:             "1; mode=block",
		ContentTypeNosniff:        "nosniff",
		XFrameOptions:             "DENY",
		ReferrerPolicy:            "strict-origin-when-cross-origin",
		CrossOriginEmbedderPolicy: "require-corp",
		CrossOriginOpenerPolicy:   "same-origin",
		CrossOriginResourcePolicy: "same-site",
		OriginAgentCluster:        "?1",
		XDNSPrefetchControl:       "off",
		XDownloadOptions:          "noopen",
	})

	return func(c fiber.Ctx) error {
		if err := hstshHandler(c); err != nil {
			return err
		}
		if err := helmetHandler(c); err != nil {
			return err
		}
		// Custom headers
		c.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=(), payment=()")
		return c.Next()
	}
}
