package http

import (
	"github.com/gofiber/fiber/v3"
	domaincalendar "github.com/jherrma/caldav-server/internal/domain/calendar"
	calendaruc "github.com/jherrma/caldav-server/internal/usecase/calendar"
)

var _ = domaincalendar.Calendar{}

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

// Create godoc
// @Summary      Create a new calendar
// @Description  Create a calendar for the authenticated user
// @Tags         Calendars
// @Accept       json
// @Produce      json
// @Param        calendar  body      calendaruc.CreateCalendarRequest  true  "Calendar details"
// @Success      201       {object}  domaincalendar.Calendar
// @Failure      400       {object}  ErrorResponseBody
// @Failure      401       {object}  ErrorResponseBody
// @Security     BearerAuth
// @Router       /calendars [post]
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

// List godoc
// @Summary      List all calendars
// @Description  Get all calendars for the authenticated user
// @Tags         Calendars
// @Produce      json
// @Success      200  {object}  object{calendars=[]domaincalendar.Calendar}
// @Failure      401  {object}  ErrorResponseBody
// @Failure      500  {object}  ErrorResponseBody
// @Security     BearerAuth
// @Router       /calendars [get]
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

// Get godoc
// @Summary      Get calendar by ID
// @Description  Get a specific calendar by UUID
// @Tags         Calendars
// @Produce      json
// @Param        id   path      string  true  "Calendar UUID"
// @Success      200  {object}  domaincalendar.Calendar
// @Failure      401  {object}  ErrorResponseBody
// @Failure      404  {object}  ErrorResponseBody
// @Security     BearerAuth
// @Router       /calendars/{id} [get]
func (h *CalendarHandler) Get(c fiber.Ctx) error {
	userID := c.Locals("user_id").(uint)
	calendarUUID := c.Params("id")

	calendar, err := h.getUC.Execute(c.Context(), userID, calendarUUID)
	if err != nil {
		return ErrorResponse(c, fiber.StatusNotFound, "Calendar not found")
	}

	return c.JSON(calendar)
}

// Update godoc
// @Summary      Update calendar
// @Description  Update a calendar's properties
// @Tags         Calendars
// @Accept       json
// @Produce      json
// @Param        id        path      string                            true  "Calendar UUID"
// @Param        calendar  body      calendaruc.UpdateCalendarRequest  true  "Updated calendar details"
// @Success      200       {object}  domaincalendar.Calendar
// @Failure      400       {object}  ErrorResponseBody
// @Failure      401       {object}  ErrorResponseBody
// @Security     BearerAuth
// @Router       /calendars/{id} [patch]
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

// Delete godoc
// @Summary      Delete calendar
// @Description  Delete a calendar and all its events
// @Tags         Calendars
// @Accept       json
// @Param        id       path  string                            true  "Calendar UUID"
// @Param        confirm  body  calendaruc.DeleteCalendarRequest  true  "Delete confirmation"
// @Success      204
// @Failure      400  {object}  ErrorResponseBody
// @Failure      401  {object}  ErrorResponseBody
// @Security     BearerAuth
// @Router       /calendars/{id} [delete]
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

// Export godoc
// @Summary      Export calendar as iCalendar
// @Description  Download calendar as .ics file
// @Tags         Import/Export
// @Produce      text/calendar
// @Param        id  path      string  true  "Calendar UUID"
// @Success      200  {file}    file
// @Failure      401  {object}  ErrorResponseBody
// @Failure      404  {object}  ErrorResponseBody
// @Security     BearerAuth
// @Router       /calendars/{id}/export [get]
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
