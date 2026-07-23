package http

import (
	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/jherrma/caldav-server/internal/usecase/sharing"
)

type CalendarShareHandler struct {
	createUC     *sharing.CreateCalendarShareUseCase
	listUC       *sharing.ListCalendarSharesUseCase
	updateUC     *sharing.UpdateCalendarShareUseCase
	revokeUC     *sharing.RevokeCalendarShareUseCase
	calendarRepo calendar.CalendarRepository
}

func NewCalendarShareHandler(
	createUC *sharing.CreateCalendarShareUseCase,
	listUC *sharing.ListCalendarSharesUseCase,
	updateUC *sharing.UpdateCalendarShareUseCase,
	revokeUC *sharing.RevokeCalendarShareUseCase,
	calendarRepo calendar.CalendarRepository,
) *CalendarShareHandler {
	return &CalendarShareHandler{
		createUC:     createUC,
		listUC:       listUC,
		updateUC:     updateUC,
		revokeUC:     revokeUC,
		calendarRepo: calendarRepo,
	}
}

// resolveOwnedCalendarID maps the :id path segment — a calendar UUID (#52) — to
// its internal numeric id, scoped to the caller: a missing or non-owned calendar
// yields (0, false), which callers turn into a 404 (never leaking existence).
func (h *CalendarShareHandler) resolveOwnedCalendarID(c fiber.Ctx, userID uint) (uint, bool) {
	cal, err := h.calendarRepo.GetByUUID(c.Context(), c.Params("id"))
	if err != nil || cal == nil || cal.UserID != userID {
		return 0, false
	}
	return cal.ID, true
}

// POST /api/v1/calendars/:id/shares
func (h *CalendarShareHandler) Create(c fiber.Ctx) error {
	u := c.Locals("user").(*user.User)
	calendarID, ok := h.resolveOwnedCalendarID(c, u.ID)
	if !ok {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "not_found"})
	}

	var req sharing.CreateCalendarShareInput
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_request"})
	}
	req.CalendarID = calendarID

	output, err := h.createUC.Execute(c.Context(), u.ID, req)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(fiber.StatusCreated).JSON(output)
}

// GET /api/v1/calendars/:id/shares
func (h *CalendarShareHandler) List(c fiber.Ctx) error {
	u := c.Locals("user").(*user.User)
	calendarID, ok := h.resolveOwnedCalendarID(c, u.ID)
	if !ok {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "not_found"})
	}

	output, err := h.listUC.Execute(c.Context(), u.ID, calendarID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"shares": output})
}

// PATCH /api/v1/calendars/:id/shares/:share_id
func (h *CalendarShareHandler) Update(c fiber.Ctx) error {
	u := c.Locals("user").(*user.User)
	calendarID, ok := h.resolveOwnedCalendarID(c, u.ID)
	if !ok {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "not_found"})
	}
	shareUUID := c.Params("share_id")

	var req sharing.UpdateCalendarShareInput
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_request"})
	}

	output, err := h.updateUC.Execute(c.Context(), u.ID, calendarID, shareUUID, req)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(output)
}

// DELETE /api/v1/calendars/:id/shares/:share_id
func (h *CalendarShareHandler) Revoke(c fiber.Ctx) error {
	u := c.Locals("user").(*user.User)
	calendarID, ok := h.resolveOwnedCalendarID(c, u.ID)
	if !ok {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "not_found"})
	}
	shareUUID := c.Params("share_id")

	if err := h.revokeUC.Execute(c.Context(), u.ID, calendarID, shareUUID); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	return c.SendStatus(fiber.StatusNoContent)
}
