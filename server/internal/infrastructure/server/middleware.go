package server

import (
	"time"

	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/gofiber/fiber/v3/middleware/requestid"
	"github.com/jherrma/caldav-server/internal/adapter/middleware"
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
	app.Use(middleware.SecurityHeadersMiddleware(cfg.Security))

	// CORS
	app.Use(middleware.CORSMiddleware(cfg.CORS))

	// Rate Limiting
	app.Use(middleware.GlobalRateLimiter(cfg.RateLimit))
}
