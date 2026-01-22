package http

import (
	"fmt"

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

// UnauthorizedResponse sends a 401 Unauthorized response
func UnauthorizedResponse(c fiber.Ctx, message string) error {
	return ErrorResponse(c, fiber.StatusUnauthorized, message)
}

// BadRequestResponse sends a 400 Bad Request response
func BadRequestResponse(c fiber.Ctx, message string) error {
	return ErrorResponse(c, fiber.StatusBadRequest, message)
}

// ForbiddenResponse sends a 403 Forbidden response
func ForbiddenResponse(c fiber.Ctx, message string) error {
	return ErrorResponse(c, fiber.StatusForbidden, message)
}

// ConflictResponse sends a 409 Conflict response
func ConflictResponse(c fiber.Ctx, message string) error {
	return ErrorResponse(c, fiber.StatusConflict, message)
}

// TooManyRequestsResponse sends a 429 Too Many Requests response
func TooManyRequestsResponse(c fiber.Ctx, message string, retryAfter int) error {
	if retryAfter > 0 {
		c.Set("Retry-After", fmt.Sprintf("%d", retryAfter))
	}
	return ErrorResponse(c, fiber.StatusTooManyRequests, message)
}
