package http

import (
	"github.com/gofiber/fiber/v3"
)

// Response represents a standard API response
type Response struct {
	Status  string      `json:"status"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// SuccessResponse sends a success response with data
func SuccessResponse(c fiber.Ctx, data interface{}) error {
	return c.Status(fiber.StatusOK).JSON(Response{
		Status: "ok",
		Data:   data,
	})
}

// ErrorResponse sends an error response
func ErrorResponse(c fiber.Ctx, status int, message string) error {
	return c.Status(status).JSON(Response{
		Status:  "error",
		Message: message,
	})
}
