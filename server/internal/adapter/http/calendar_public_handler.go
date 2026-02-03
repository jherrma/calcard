package http

import (
	"strconv"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/domain/user"
	calendarusecase "github.com/jherrma/caldav-server/internal/usecase/calendar"
)

type CalendarPublicHandler struct {
	enablePublicUC    *calendarusecase.EnablePublicUseCase
	getPublicStatusUC *calendarusecase.GetPublicStatusUseCase
	regenerateTokenUC *calendarusecase.RegenerateTokenUseCase
}

func NewCalendarPublicHandler(
	enablePublicUC *calendarusecase.EnablePublicUseCase,
	getPublicStatusUC *calendarusecase.GetPublicStatusUseCase,
	regenerateTokenUC *calendarusecase.RegenerateTokenUseCase,
) *CalendarPublicHandler {
	return &CalendarPublicHandler{
		enablePublicUC:    enablePublicUC,
		getPublicStatusUC: getPublicStatusUC,
		regenerateTokenUC: regenerateTokenUC,
	}
}

// POST /api/v1/calendars/:id/public
func (h *CalendarPublicHandler) EnablePublic(c fiber.Ctx) error {
	u := c.Locals("user").(*user.User)
	calendarID, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_calendar_id"})
	}

	var req calendarusecase.EnablePublicInput
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_request"})
	}

	output, err := h.enablePublicUC.Execute(c.Context(), u.ID, uint(calendarID), req)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(output)
}

// GET /api/v1/calendars/:id/public
func (h *CalendarPublicHandler) GetPublicStatus(c fiber.Ctx) error {
	u := c.Locals("user").(*user.User)
	calendarID, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_calendar_id"})
	}

	output, err := h.getPublicStatusUC.Execute(c.Context(), u.ID, uint(calendarID))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(output)
}

// POST /api/v1/calendars/:id/public/regenerate
func (h *CalendarPublicHandler) RegenerateToken(c fiber.Ctx) error {
	u := c.Locals("user").(*user.User)
	calendarID, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_calendar_id"})
	}

	output, err := h.regenerateTokenUC.Execute(c.Context(), u.ID, uint(calendarID))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(output)
}
