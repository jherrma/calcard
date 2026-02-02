package http

import (
	"strconv"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/jherrma/caldav-server/internal/usecase/sharing"
)

type CalendarShareHandler struct {
	createUC *sharing.CreateCalendarShareUseCase
	listUC   *sharing.ListCalendarSharesUseCase
	updateUC *sharing.UpdateCalendarShareUseCase
	revokeUC *sharing.RevokeCalendarShareUseCase
}

func NewCalendarShareHandler(
	createUC *sharing.CreateCalendarShareUseCase,
	listUC *sharing.ListCalendarSharesUseCase,
	updateUC *sharing.UpdateCalendarShareUseCase,
	revokeUC *sharing.RevokeCalendarShareUseCase,
) *CalendarShareHandler {
	return &CalendarShareHandler{
		createUC: createUC,
		listUC:   listUC,
		updateUC: updateUC,
		revokeUC: revokeUC,
	}
}

// POST /api/v1/calendars/:id/shares
func (h *CalendarShareHandler) Create(c fiber.Ctx) error {
	u := c.Locals("user").(*user.User)
	calendarID, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_calendar_id"})
	}

	var req sharing.CreateCalendarShareInput
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_request"})
	}
	req.CalendarID = uint(calendarID)

	output, err := h.createUC.Execute(c.Context(), u.ID, req)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(fiber.StatusCreated).JSON(output)
}

// GET /api/v1/calendars/:id/shares
func (h *CalendarShareHandler) List(c fiber.Ctx) error {
	u := c.Locals("user").(*user.User)
	calendarID, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_calendar_id"})
	}

	output, err := h.listUC.Execute(c.Context(), u.ID, uint(calendarID))
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"shares": output})
}

// PATCH /api/v1/calendars/:id/shares/:share_id
func (h *CalendarShareHandler) Update(c fiber.Ctx) error {
	u := c.Locals("user").(*user.User)
	calendarID, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_calendar_id"})
	}
	shareUUID := c.Params("share_id")

	var req sharing.UpdateCalendarShareInput
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_request"})
	}

	output, err := h.updateUC.Execute(c.Context(), u.ID, uint(calendarID), shareUUID, req)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(output)
}

// DELETE /api/v1/calendars/:id/shares/:share_id
func (h *CalendarShareHandler) Revoke(c fiber.Ctx) error {
	u := c.Locals("user").(*user.User)
	calendarID, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_calendar_id"})
	}
	shareUUID := c.Params("share_id")

	if err := h.revokeUC.Execute(c.Context(), u.ID, uint(calendarID), shareUUID); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	return c.SendStatus(fiber.StatusNoContent)
}
