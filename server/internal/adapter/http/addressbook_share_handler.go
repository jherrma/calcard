package http

import (
	"strconv"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/jherrma/caldav-server/internal/usecase/sharing"
)

type AddressBookShareHandler struct {
	createUC *sharing.CreateAddressBookShareUseCase
	listUC   *sharing.ListAddressBookSharesUseCase
	updateUC *sharing.UpdateAddressBookShareUseCase
	revokeUC *sharing.RevokeAddressBookShareUseCase
}

func NewAddressBookShareHandler(
	createUC *sharing.CreateAddressBookShareUseCase,
	listUC *sharing.ListAddressBookSharesUseCase,
	updateUC *sharing.UpdateAddressBookShareUseCase,
	revokeUC *sharing.RevokeAddressBookShareUseCase,
) *AddressBookShareHandler {
	return &AddressBookShareHandler{
		createUC: createUC,
		listUC:   listUC,
		updateUC: updateUC,
		revokeUC: revokeUC,
	}
}

// POST /api/v1/addressbooks/:id/shares
func (h *AddressBookShareHandler) Create(c fiber.Ctx) error {
	u := c.Locals("user").(*user.User)
	addressBookID, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_addressbook_id"})
	}

	var req sharing.CreateAddressBookShareInput
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_request"})
	}
	req.AddressBookID = uint(addressBookID)

	output, err := h.createUC.Execute(c.Context(), u.ID, req)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(fiber.StatusCreated).JSON(output)
}

// GET /api/v1/addressbooks/:id/shares
func (h *AddressBookShareHandler) List(c fiber.Ctx) error {
	u := c.Locals("user").(*user.User)
	addressBookID, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_addressbook_id"})
	}

	output, err := h.listUC.Execute(c.Context(), u.ID, uint(addressBookID))
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(output)
}

// PATCH /api/v1/addressbooks/:id/shares/:share_id
func (h *AddressBookShareHandler) Update(c fiber.Ctx) error {
	u := c.Locals("user").(*user.User)
	addressBookID, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_addressbook_id"})
	}
	shareUUID := c.Params("share_id")

	var req sharing.UpdateAddressBookShareInput
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_request"})
	}

	output, err := h.updateUC.Execute(c.Context(), u.ID, uint(addressBookID), shareUUID, req)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(output)
}

// DELETE /api/v1/addressbooks/:id/shares/:share_id
func (h *AddressBookShareHandler) Revoke(c fiber.Ctx) error {
	u := c.Locals("user").(*user.User)
	addressBookID, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_addressbook_id"})
	}
	shareUUID := c.Params("share_id")

	if err := h.revokeUC.Execute(c.Context(), u.ID, uint(addressBookID), shareUUID); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	return c.SendStatus(fiber.StatusNoContent)
}
