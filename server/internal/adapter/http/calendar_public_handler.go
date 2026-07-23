package http

import (
	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/user"
	calendarusecase "github.com/jherrma/caldav-server/internal/usecase/calendar"
)

type CalendarPublicHandler struct {
	enablePublicUC    *calendarusecase.EnablePublicUseCase
	getPublicStatusUC *calendarusecase.GetPublicStatusUseCase
	regenerateTokenUC *calendarusecase.RegenerateTokenUseCase
	calendarRepo      calendar.CalendarRepository
}

func NewCalendarPublicHandler(
	enablePublicUC *calendarusecase.EnablePublicUseCase,
	getPublicStatusUC *calendarusecase.GetPublicStatusUseCase,
	regenerateTokenUC *calendarusecase.RegenerateTokenUseCase,
	calendarRepo calendar.CalendarRepository,
) *CalendarPublicHandler {
	return &CalendarPublicHandler{
		enablePublicUC:    enablePublicUC,
		getPublicStatusUC: getPublicStatusUC,
		regenerateTokenUC: regenerateTokenUC,
		calendarRepo:      calendarRepo,
	}
}

// resolveOwnedCalendarID maps the :id path segment — a calendar UUID (#52) — to
// its internal numeric id, scoped to the caller (missing/non-owned → 404).
func (h *CalendarPublicHandler) resolveOwnedCalendarID(c fiber.Ctx, userID uint) (uint, bool) {
	cal, err := h.calendarRepo.GetByUUID(c.Context(), c.Params("id"))
	if err != nil || cal == nil || cal.UserID != userID {
		return 0, false
	}
	return cal.ID, true
}

// POST /api/v1/calendars/:id/public
func (h *CalendarPublicHandler) EnablePublic(c fiber.Ctx) error {
	u := c.Locals("user").(*user.User)
	calendarID, ok := h.resolveOwnedCalendarID(c, u.ID)
	if !ok {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "not_found"})
	}

	var req calendarusecase.EnablePublicInput
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_request"})
	}

	output, err := h.enablePublicUC.Execute(c.Context(), u.ID, calendarID, req)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(output)
}

// GET /api/v1/calendars/:id/public
func (h *CalendarPublicHandler) GetPublicStatus(c fiber.Ctx) error {
	u := c.Locals("user").(*user.User)
	calendarID, ok := h.resolveOwnedCalendarID(c, u.ID)
	if !ok {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "not_found"})
	}

	output, err := h.getPublicStatusUC.Execute(c.Context(), u.ID, calendarID)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(output)
}

// POST /api/v1/calendars/:id/public/regenerate
func (h *CalendarPublicHandler) RegenerateToken(c fiber.Ctx) error {
	u := c.Locals("user").(*user.User)
	calendarID, ok := h.resolveOwnedCalendarID(c, u.ID)
	if !ok {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "not_found"})
	}

	output, err := h.regenerateTokenUC.Execute(c.Context(), u.ID, calendarID)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(output)
}
