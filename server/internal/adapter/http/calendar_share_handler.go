package http

import (
	"errors"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/user"
	calendaruc "github.com/jherrma/caldav-server/internal/usecase/calendar"
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
// is calendar.ErrNotFound, which calendarSubresourceError turns into a 404
// (never leaking existence); a storage failure is a 500.
func resolveOwnedCalendarID(c fiber.Ctx, repo calendar.CalendarRepository, userID uint) (uint, error) {
	cal, err := calendaruc.LoadOwnedCalendar(c.Context(), repo, userID, c.Params("id"))
	if err != nil {
		return 0, err
	}
	return cal.ID, nil
}

// calendarSubresourceError maps an error of the calendar share and public-link
// routes, which answer in the {"error": ...} shape: a calendar that is missing
// or not the caller's is the 404 these routes always gave, an unknown share id
// a 404 too, a rejected request (unknown user, self-share, invalid permission,
// public access not enabled) a 400 with its message, and anything else a
// server failure (F28): logged, and a 500 that does not describe it.
func calendarSubresourceError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, calendar.ErrNotFound):
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "not_found"})
	case errors.Is(err, sharing.ErrCalendarShareNotFound):
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": err.Error()})
	case errors.Is(err, calendar.ErrInvalidInput):
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	logCalendarFailure(c, err)
	return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal_error"})
}

// POST /api/v1/calendars/:id/shares
func (h *CalendarShareHandler) Create(c fiber.Ctx) error {
	u := c.Locals("user").(*user.User)
	calendarID, err := resolveOwnedCalendarID(c, h.calendarRepo, u.ID)
	if err != nil {
		return calendarSubresourceError(c, err)
	}

	var req sharing.CreateCalendarShareInput
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_request"})
	}
	req.CalendarID = calendarID

	output, err := h.createUC.Execute(c.Context(), u.ID, req)
	if err != nil {
		return calendarSubresourceError(c, err)
	}

	return c.Status(fiber.StatusCreated).JSON(output)
}

// GET /api/v1/calendars/:id/shares
func (h *CalendarShareHandler) List(c fiber.Ctx) error {
	u := c.Locals("user").(*user.User)
	calendarID, err := resolveOwnedCalendarID(c, h.calendarRepo, u.ID)
	if err != nil {
		return calendarSubresourceError(c, err)
	}

	output, err := h.listUC.Execute(c.Context(), u.ID, calendarID)
	if err != nil {
		return calendarSubresourceError(c, err)
	}

	return c.JSON(fiber.Map{"shares": output})
}

// PATCH /api/v1/calendars/:id/shares/:share_id
func (h *CalendarShareHandler) Update(c fiber.Ctx) error {
	u := c.Locals("user").(*user.User)
	calendarID, err := resolveOwnedCalendarID(c, h.calendarRepo, u.ID)
	if err != nil {
		return calendarSubresourceError(c, err)
	}
	shareUUID := c.Params("share_id")

	var req sharing.UpdateCalendarShareInput
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_request"})
	}

	output, err := h.updateUC.Execute(c.Context(), u.ID, calendarID, shareUUID, req)
	if err != nil {
		return calendarSubresourceError(c, err)
	}

	return c.JSON(output)
}

// DELETE /api/v1/calendars/:id/shares/:share_id
func (h *CalendarShareHandler) Revoke(c fiber.Ctx) error {
	u := c.Locals("user").(*user.User)
	calendarID, err := resolveOwnedCalendarID(c, h.calendarRepo, u.ID)
	if err != nil {
		return calendarSubresourceError(c, err)
	}
	shareUUID := c.Params("share_id")

	if err := h.revokeUC.Execute(c.Context(), u.ID, calendarID, shareUUID); err != nil {
		return calendarSubresourceError(c, err)
	}

	return c.SendStatus(fiber.StatusNoContent)
}
