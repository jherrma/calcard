package server

import (
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/cors"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/gofiber/fiber/v3/middleware/requestid"
)

// SetupMiddleware configures global middleware for the Fiber app
func SetupMiddleware(app *fiber.App) {
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

	// CORS
	app.Use(cors.New(cors.Config{
		AllowOrigins: []string{"*"}, // TODO: Configure from config
		AllowMethods: []string{"GET", "POST", "HEAD", "PUT", "DELETE", "PATCH", "PROPFIND", "PROPPATCH", "MKCOL", "COPY", "MOVE", "LOCK", "UNLOCK", "REPORT", "OPTIONS"},
		AllowHeaders: []string{"Origin", "Content-Type", "Accept", "Authorization", "Depth", "User-Agent", "X-File-Size", "X-Requested-With", "If-Modified-Since", "X-File-Name", "Cache-Control"},
	}))
}
