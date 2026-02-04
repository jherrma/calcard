package server

import (
	"fmt"
	"time"

	"github.com/jherrma/caldav-server/internal/adapter/middleware"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/helmet"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/gofiber/fiber/v3/middleware/requestid"
	"github.com/jherrma/caldav-server/internal/config"
)

// SetupMiddleware configures global middleware for the Fiber app
func SetupMiddleware(app *fiber.App, cfg *config.Config) {
	// Request ID
	app.Use(requestid.New())

	// Logger
	app.Use(logger.New(logger.Config{
		Format:     "[${time}] ${status} - ${latency} ${method} ${path}\n",
		TimeFormat: time.RFC3339,
		TimeZone:   "UTC",
	}))

	// Recover from panics
	app.Use(recover.New())

	// Security Headers
	if cfg.Security.Enabled {
		// Helmet
		app.Use(helmet.New(helmet.Config{
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
		}))

		// HSTS
		if cfg.Security.HSTSEnabled {
			app.Use(func(c fiber.Ctx) error {
				if c.Protocol() == "https" {
					c.Set("Strict-Transport-Security", fmt.Sprintf("max-age=%d; includeSubDomains", cfg.Security.HSTSMaxAge))
				}
				return c.Next()
			})
		}

		// Permissions Policy
		app.Use(func(c fiber.Ctx) error {
			c.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=(), payment=()")
			return c.Next()
		})
	}

	// CORS
	app.Use(middleware.CORSMiddleware(cfg.CORS))

	// Rate Limiting
	app.Use(middleware.GlobalRateLimiter(cfg.RateLimit))
}
