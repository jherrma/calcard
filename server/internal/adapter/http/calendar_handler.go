package http

import (
	"github.com/gofiber/fiber/v3"
	calendaruc "github.com/jherrma/caldav-server/internal/usecase/calendar"
)

// CalendarHandler handles calendar HTTP requests
type CalendarHandler struct {
	createUC *calendaruc.CreateCalendarUseCase
	listUC   *calendaruc.ListCalendarsUseCase
	getUC    *calendaruc.GetCalendarUseCase
	updateUC *calendaruc.UpdateCalendarUseCase
	deleteUC *calendaruc.DeleteCalendarUseCase
	exportUC *calendaruc.ExportCalendarUseCase
}

// NewCalendarHandler creates a new calendar handler
func NewCalendarHandler(
	createUC *calendaruc.CreateCalendarUseCase,
	listUC *calendaruc.ListCalendarsUseCase,
	getUC *calendaruc.GetCalendarUseCase,
	updateUC *calendaruc.UpdateCalendarUseCase,
	deleteUC *calendaruc.DeleteCalendarUseCase,
	exportUC *calendaruc.ExportCalendarUseCase,
) *CalendarHandler {
	return &CalendarHandler{
		createUC: createUC,
		listUC:   listUC,
		getUC:    getUC,
		updateUC: updateUC,
		deleteUC: deleteUC,
		exportUC: exportUC,
	}
}

// Create handles POST /api/v1/calendars
func (h *CalendarHandler) Create(c fiber.Ctx) error {
	userID := c.Locals("user_id").(uint)

	var req calendaruc.CreateCalendarRequest
	if err := c.Bind().JSON(&req); err != nil {
		return ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	calendar, err := h.createUC.Execute(c.Context(), userID, req)
	if err != nil {
		return ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return c.Status(fiber.StatusCreated).JSON(calendar)
}

// List handles GET /api/v1/calendars
func (h *CalendarHandler) List(c fiber.Ctx) error {
	userID := c.Locals("user_id").(uint)

	calendars, err := h.listUC.Execute(c.Context(), userID)
	if err != nil {
		return ErrorResponse(c, fiber.StatusInternalServerError, "Failed to list calendars")
	}

	return c.JSON(fiber.Map{
		"calendars": calendars,
	})
}

// Get handles GET /api/v1/calendars/:id
func (h *CalendarHandler) Get(c fiber.Ctx) error {
	userID := c.Locals("user_id").(uint)
	calendarUUID := c.Params("id")

	calendar, err := h.getUC.Execute(c.Context(), userID, calendarUUID)
	if err != nil {
		return ErrorResponse(c, fiber.StatusNotFound, "Calendar not found")
	}

	return c.JSON(calendar)
}

// Update handles PATCH /api/v1/calendars/:id
func (h *CalendarHandler) Update(c fiber.Ctx) error {
	userID := c.Locals("user_id").(uint)
	calendarUUID := c.Params("id")

	var req calendaruc.UpdateCalendarRequest
	if err := c.Bind().JSON(&req); err != nil {
		return ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	calendar, err := h.updateUC.Execute(c.Context(), userID, calendarUUID, req)
	if err != nil {
		return ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return c.JSON(calendar)
}

// Delete handles DELETE /api/v1/calendars/:id
func (h *CalendarHandler) Delete(c fiber.Ctx) error {
	userID := c.Locals("user_id").(uint)
	calendarUUID := c.Params("id")

	var req calendaruc.DeleteCalendarRequest
	if err := c.Bind().JSON(&req); err != nil {
		return ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if err := h.deleteUC.Execute(c.Context(), userID, calendarUUID, req); err != nil {
		return ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// Export handles GET /api/v1/calendars/:id/export
func (h *CalendarHandler) Export(c fiber.Ctx) error {
	userID := c.Locals("user_id").(uint)
	calendarUUID := c.Params("id")

	content, filename, err := h.exportUC.Execute(c.Context(), userID, calendarUUID)
	if err != nil {
		return ErrorResponse(c, fiber.StatusNotFound, "Calendar not found")
	}

	c.Set("Content-Type", "text/calendar; charset=utf-8")
	c.Set("Content-Disposition", `attachment; filename="`+filename+`"`)

	return c.SendString(content)
}
