package http

import (
	"errors"
	"fmt"
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
	// Use the shared helper (like ImportContact) instead of a raw
	// c.Locals(...).(uint) type-assertion, which would panic if the auth
	// middleware contract ever changes the stored type.
	userID, err := GetUserIDFromContext(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	calendarUUID := c.Params("id")

	// Get import options
	opts := importexport.ImportOptions{
		DuplicateHandling: c.Query("duplicate_handling", "skip"),
	}

	// Try to get data from file upload first
	data, err := h.getImportData(c)
	if err != nil {
		return writeImportDataError(c, err)
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
		return writeImportDataError(c, err)
	}

	result, err := h.contactImportUC.Execute(c.Context(), userID, uint(id), data, opts)
	if err != nil {
		return ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return c.JSON(result)
}

// getImportData extracts import data from a multipart file upload, a JSON
// `data` field, or a raw text/calendar|text/vcard body, and enforces the
// import size limit on ALL of those paths.
func (h *ImportHandler) getImportData(c fiber.Ctx) ([]byte, error) {
	data, err := h.readImportData(c)
	if err != nil {
		return nil, err
	}

	// Defense-in-depth (issue #72): enforce the import size limit on EVERY
	// input path, not just multipart. The global Fiber BodyLimit happens to
	// cap the JSON and raw-body paths today, but that is a transport-level
	// setting, not an import policy — it would silently stop protecting these
	// paths the day BodyLimit is raised. Checking the assembled bytes here
	// keeps the limit authoritative regardless of how the data arrived.
	if len(data) > maxImportFileSize {
		return nil, fiber.NewError(fiber.StatusRequestEntityTooLarge,
			fmt.Sprintf("import data exceeds maximum size of %d bytes", maxImportFileSize))
	}

	return data, nil
}

// readImportData pulls the raw import bytes from whichever input form the
// request used. The multipart path keeps its early file.Size fast-fail so an
// oversize upload is rejected before it is read into memory; the assembled
// bytes are still re-checked by getImportData.
func (h *ImportHandler) readImportData(c fiber.Ctx) ([]byte, error) {
	// Check for multipart file upload
	file, err := c.FormFile("file")
	if err == nil && file != nil {
		// Fast-fail on the client-declared size before reading the whole file
		// into memory.
		if file.Size > maxImportFileSize {
			return nil, fiber.NewError(fiber.StatusRequestEntityTooLarge,
				fmt.Sprintf("import data exceeds maximum size of %d bytes", maxImportFileSize))
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

// writeImportDataError maps a getImportData error onto an HTTP response. It
// honors an explicit *fiber.Error status code (e.g. 413 Payload Too Large for
// oversize input) and falls back to 400 Bad Request otherwise, so an oversize
// import surfaces as a clean 4xx rather than a 500.
func writeImportDataError(c fiber.Ctx, err error) error {
	var fe *fiber.Error
	if errors.As(err, &fe) {
		return ErrorResponse(c, fe.Code, fe.Message)
	}
	return ErrorResponse(c, fiber.StatusBadRequest, err.Error())
}
