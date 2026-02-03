package http

import (
	"io"
	"strconv"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/usecase/importexport"
)

const maxImportFileSize = 10 * 1024 * 1024 // 10MB

// ImportHandler handles import HTTP requests
type ImportHandler struct {
	calendarImportUC *importexport.CalendarImportUseCase
	contactImportUC  *importexport.ContactImportUseCase
}

// NewImportHandler creates a new import handler
func NewImportHandler(
	calendarImportUC *importexport.CalendarImportUseCase,
	contactImportUC *importexport.ContactImportUseCase,
) *ImportHandler {
	return &ImportHandler{
		calendarImportUC: calendarImportUC,
		contactImportUC:  contactImportUC,
	}
}

// ImportCalendar handles POST /api/v1/calendars/:id/import
func (h *ImportHandler) ImportCalendar(c fiber.Ctx) error {
	userID := c.Locals("user_id").(uint)
	calendarUUID := c.Params("id")

	// Get import options
	opts := importexport.ImportOptions{
		DuplicateHandling: c.Query("duplicate_handling", "skip"),
	}

	// Try to get data from file upload first
	data, err := h.getImportData(c)
	if err != nil {
		return ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	result, err := h.calendarImportUC.Execute(c.Context(), userID, calendarUUID, data, opts)
	if err != nil {
		return ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return c.JSON(result)
}

// ImportContact handles POST /api/v1/addressbooks/:id/import
func (h *ImportHandler) ImportContact(c fiber.Ctx) error {
	userID, err := GetUserIDFromContext(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_id"})
	}

	// Get import options
	opts := importexport.ImportOptions{
		DuplicateHandling: c.Query("duplicate_handling", "skip"),
	}

	// Try to get data from file upload first
	data, err := h.getImportData(c)
	if err != nil {
		return ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	result, err := h.contactImportUC.Execute(c.Context(), userID, uint(id), data, opts)
	if err != nil {
		return ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return c.JSON(result)
}

// getImportData extracts import data from file upload or JSON body
func (h *ImportHandler) getImportData(c fiber.Ctx) ([]byte, error) {
	// Check for multipart file upload
	file, err := c.FormFile("file")
	if err == nil && file != nil {
		// Check file size
		if file.Size > maxImportFileSize {
			return nil, fiber.NewError(fiber.StatusRequestEntityTooLarge, "File exceeds maximum size of 10MB")
		}

		f, err := file.Open()
		if err != nil {
			return nil, fiber.NewError(fiber.StatusBadRequest, "Failed to open uploaded file")
		}
		defer f.Close()

		data, err := io.ReadAll(f)
		if err != nil {
			return nil, fiber.NewError(fiber.StatusBadRequest, "Failed to read uploaded file")
		}
		return data, nil
	}

	// Check for raw data in JSON body
	var req struct {
		Data string `json:"data"`
	}
	if err := c.Bind().JSON(&req); err == nil && req.Data != "" {
		return []byte(req.Data), nil
	}

	// Check for raw body (text/calendar or text/vcard content type)
	contentType := c.Get("Content-Type")
	if contentType == "text/calendar" || contentType == "text/vcard" {
		return c.Body(), nil
	}

	return nil, fiber.NewError(fiber.StatusBadRequest, "No import data provided. Upload a file or send data in request body.")
}
