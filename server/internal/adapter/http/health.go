package http

import (
	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/infrastructure/database"
)

// HealthHandler handles health check requests
type HealthHandler struct {
	db database.Database
}

// NewHealthHandler creates a new HealthHandler
func NewHealthHandler(db database.Database) *HealthHandler {
	return &HealthHandler{db: db}
}

// Liveness probe (GET /health)
func (h *HealthHandler) Liveness(c fiber.Ctx) error {
	return SuccessResponse(c, nil)
}

// Readiness probe (GET /health/ready)
func (h *HealthHandler) Readiness(c fiber.Ctx) error {
	checks := make(map[string]string)
	status := "ok"

	// Check database connection
	if err := h.db.Ping(); err != nil {
		checks["database"] = "failed"
		status = "degraded"
	} else {
		checks["database"] = "ok"
	}

	response := Response{
		Status: status,
		Data:   map[string]interface{}{"checks": checks},
	}

	if status != "ok" {
		return c.Status(fiber.StatusServiceUnavailable).JSON(response)
	}

	return c.Status(fiber.StatusOK).JSON(response)
}
