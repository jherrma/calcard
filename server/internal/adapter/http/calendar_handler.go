package http

import (
	"errors"
	"log/slog"

	"github.com/gofiber/fiber/v3"
	domaincalendar "github.com/jherrma/caldav-server/internal/domain/calendar"
	calendaruc "github.com/jherrma/caldav-server/internal/usecase/calendar"
)

var _ = domaincalendar.Calendar{}

// calendarError maps a calendar use-case error onto a response. A missing
// calendar and one the caller does not own are both domaincalendar.ErrNotFound
// and answer the same 404 body GET always gave, so no verb tells a caller that
// another user's calendar exists (F12). A rejected request (validation, DELETE
// confirmation, last-calendar guard, unparseable import) is
// domaincalendar.ErrInvalidInput: 400 with its message. Anything else is a
// server failure (F28): 500 with a generic message, the cause only in the log.
func calendarError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, domaincalendar.ErrNotFound):
		return ErrorResponse(c, fiber.StatusNotFound, "Calendar not found")
	case errors.Is(err, domaincalendar.ErrInvalidInput):
		return ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}
	logCalendarFailure(c, err)
	return ErrorResponse(c, fiber.StatusInternalServerError, "Internal server error")
}

// logCalendarFailure records a server-side failure of a calendar request. The
// client gets a generic 500; storage details belong in the log only.
func logCalendarFailure(c fiber.Ctx, err error) {
	slog.Error("calendar request failed",
		slog.String("method", c.Method()),
		slog.String("path", c.Path()),
		slog.Any("error", err),
	)
}

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

// POST /api/v1/calendars
func (h *CalendarHandler) Create(c fiber.Ctx) error {
	userID := c.Locals("user_id").(uint)

	var req calendaruc.CreateCalendarRequest
	if err := c.Bind().JSON(&req); err != nil {
		return ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	calendar, err := h.createUC.Execute(c.Context(), userID, req)
	if err != nil {
		return calendarError(c, err)
	}

	return c.Status(fiber.StatusCreated).JSON(calendar)
}

// GET /api/v1/calendars
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

// GET /api/v1/calendars/:id
func (h *CalendarHandler) Get(c fiber.Ctx) error {
	userID := c.Locals("user_id").(uint)
	calendarUUID := c.Params("id")

	calendar, err := h.getUC.Execute(c.Context(), userID, calendarUUID)
	if err != nil {
		return calendarError(c, err)
	}

	return c.JSON(calendar)
}

// PATCH /api/v1/calendars/:id
func (h *CalendarHandler) Update(c fiber.Ctx) error {
	userID := c.Locals("user_id").(uint)
	calendarUUID := c.Params("id")

	// Resolve the calendar before reading the body: an unknown or foreign
	// calendar is the same 404 whatever the request carries (F28).
	if _, err := h.getUC.Execute(c.Context(), userID, calendarUUID); err != nil {
		return calendarError(c, err)
	}

	var req calendaruc.UpdateCalendarRequest
	if err := c.Bind().JSON(&req); err != nil {
		return ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	calendar, err := h.updateUC.Execute(c.Context(), userID, calendarUUID, req)
	if err != nil {
		return calendarError(c, err)
	}

	return c.JSON(calendar)
}

// DELETE /api/v1/calendars/:id
func (h *CalendarHandler) Delete(c fiber.Ctx) error {
	userID := c.Locals("user_id").(uint)
	calendarUUID := c.Params("id")

	// Resolve first, as in Update: the confirmation is only looked at for a
	// calendar the caller owns.
	if _, err := h.getUC.Execute(c.Context(), userID, calendarUUID); err != nil {
		return calendarError(c, err)
	}

	var req calendaruc.DeleteCalendarRequest
	if err := c.Bind().JSON(&req); err != nil {
		return ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if err := h.deleteUC.Execute(c.Context(), userID, calendarUUID, req); err != nil {
		return calendarError(c, err)
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// GET /api/v1/calendars/:id/export
func (h *CalendarHandler) Export(c fiber.Ctx) error {
	userID := c.Locals("user_id").(uint)
	calendarUUID := c.Params("id")

	content, filename, err := h.exportUC.Execute(c.Context(), userID, calendarUUID)
	if err != nil {
		return calendarError(c, err)
	}

	c.Set("Content-Type", "text/calendar; charset=utf-8")
	c.Set("Content-Disposition", `attachment; filename="`+filename+`"`)

	return c.SendString(content)
}
