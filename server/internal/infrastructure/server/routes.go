package server

import (
	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/adapter/http"
	"github.com/jherrma/caldav-server/internal/infrastructure/database"
)

// SetupRoutes registers all application routes
func SetupRoutes(app *fiber.App, db database.Database) {
	healthHandler := http.NewHealthHandler(db)

	health := app.Group("/health")
	health.Get("/", healthHandler.Liveness)
	health.Get("/ready", healthHandler.Readiness)
}
