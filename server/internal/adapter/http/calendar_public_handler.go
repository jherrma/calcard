package http

import (
	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/user"
	calendarusecase "github.com/jherrma/caldav-server/internal/usecase/calendar"
)

type CalendarPublicHandler struct {
	enablePublicUC    *calendarusecase.EnablePublicUseCase
	getPublicStatusUC *calendarusecase.GetPublicStatusUseCase
	regenerateTokenUC *calendarusecase.RegenerateTokenUseCase
	calendarRepo      calendar.CalendarRepository
	cfg               *config.Config
}

func NewCalendarPublicHandler(
	enablePublicUC *calendarusecase.EnablePublicUseCase,
	getPublicStatusUC *calendarusecase.GetPublicStatusUseCase,
	regenerateTokenUC *calendarusecase.RegenerateTokenUseCase,
	calendarRepo calendar.CalendarRepository,
	cfg *config.Config,
) *CalendarPublicHandler {
	return &CalendarPublicHandler{
		enablePublicUC:    enablePublicUC,
		getPublicStatusUC: getPublicStatusUC,
		regenerateTokenUC: regenerateTokenUC,
		calendarRepo:      calendarRepo,
		cfg:               cfg,
	}
}

// The :id path segment is a calendar UUID (#52), resolved by
// resolveOwnedCalendarID (calendar_share_handler.go): missing/non-owned → 404.
// Errors go through calendarSubresourceError, like the share routes.

// POST /api/v1/calendars/:id/public
func (h *CalendarPublicHandler) EnablePublic(c fiber.Ctx) error {
	u := c.Locals("user").(*user.User)
	calendarID, err := resolveOwnedCalendarID(c, h.calendarRepo, u.ID)
	if err != nil {
		return calendarSubresourceError(c, err)
	}

	var req calendarusecase.EnablePublicInput
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_request"})
	}

	output, err := h.enablePublicUC.Execute(c.Context(), u.ID, calendarID, req, RequestBaseURL(c, h.cfg))
	if err != nil {
		return calendarSubresourceError(c, err)
	}

	return c.JSON(output)
}

// GET /api/v1/calendars/:id/public
func (h *CalendarPublicHandler) GetPublicStatus(c fiber.Ctx) error {
	u := c.Locals("user").(*user.User)
	calendarID, err := resolveOwnedCalendarID(c, h.calendarRepo, u.ID)
	if err != nil {
		return calendarSubresourceError(c, err)
	}

	output, err := h.getPublicStatusUC.Execute(c.Context(), u.ID, calendarID, RequestBaseURL(c, h.cfg))
	if err != nil {
		return calendarSubresourceError(c, err)
	}

	return c.JSON(output)
}

// POST /api/v1/calendars/:id/public/regenerate
func (h *CalendarPublicHandler) RegenerateToken(c fiber.Ctx) error {
	u := c.Locals("user").(*user.User)
	calendarID, err := resolveOwnedCalendarID(c, h.calendarRepo, u.ID)
	if err != nil {
		return calendarSubresourceError(c, err)
	}

	output, err := h.regenerateTokenUC.Execute(c.Context(), u.ID, calendarID, RequestBaseURL(c, h.cfg))
	if err != nil {
		return calendarSubresourceError(c, err)
	}

	return c.JSON(output)
}
