package http

import (
	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/jherrma/caldav-server/internal/usecase/sharing"
)

type AddressBookShareHandler struct {
	createUC        *sharing.CreateAddressBookShareUseCase
	listUC          *sharing.ListAddressBookSharesUseCase
	updateUC        *sharing.UpdateAddressBookShareUseCase
	revokeUC        *sharing.RevokeAddressBookShareUseCase
	addressBookRepo addressbook.Repository
}

func NewAddressBookShareHandler(
	createUC *sharing.CreateAddressBookShareUseCase,
	listUC *sharing.ListAddressBookSharesUseCase,
	updateUC *sharing.UpdateAddressBookShareUseCase,
	revokeUC *sharing.RevokeAddressBookShareUseCase,
	addressBookRepo addressbook.Repository,
) *AddressBookShareHandler {
	return &AddressBookShareHandler{
		createUC:        createUC,
		listUC:          listUC,
		updateUC:        updateUC,
		revokeUC:        revokeUC,
		addressBookRepo: addressBookRepo,
	}
}

// resolveOwnedAddressBookID maps the :id path segment — an address-book UUID
// (#52) — to its internal numeric id, scoped to the caller: a missing or
// non-owned address book yields (0, false), which callers turn into a 404
// (never leaking existence).
func (h *AddressBookShareHandler) resolveOwnedAddressBookID(c fiber.Ctx, userID uint) (uint, bool) {
	ab, err := h.addressBookRepo.GetByUUID(c.Context(), c.Params("id"))
	if err != nil || ab == nil || ab.UserID != userID {
		return 0, false
	}
	return ab.ID, true
}

// POST /api/v1/addressbooks/:id/shares
func (h *AddressBookShareHandler) Create(c fiber.Ctx) error {
	u := c.Locals("user").(*user.User)
	addressBookID, ok := h.resolveOwnedAddressBookID(c, u.ID)
	if !ok {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "not_found"})
	}

	var req sharing.CreateAddressBookShareInput
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_request"})
	}
	req.AddressBookID = addressBookID

	output, err := h.createUC.Execute(c.Context(), u.ID, req)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(fiber.StatusCreated).JSON(output)
}

// GET /api/v1/addressbooks/:id/shares
func (h *AddressBookShareHandler) List(c fiber.Ctx) error {
	u := c.Locals("user").(*user.User)
	addressBookID, ok := h.resolveOwnedAddressBookID(c, u.ID)
	if !ok {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "not_found"})
	}

	output, err := h.listUC.Execute(c.Context(), u.ID, addressBookID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(output)
}

// PATCH /api/v1/addressbooks/:id/shares/:share_id
func (h *AddressBookShareHandler) Update(c fiber.Ctx) error {
	u := c.Locals("user").(*user.User)
	addressBookID, ok := h.resolveOwnedAddressBookID(c, u.ID)
	if !ok {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "not_found"})
	}
	shareUUID := c.Params("share_id")

	var req sharing.UpdateAddressBookShareInput
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_request"})
	}

	output, err := h.updateUC.Execute(c.Context(), u.ID, addressBookID, shareUUID, req)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(output)
}

// DELETE /api/v1/addressbooks/:id/shares/:share_id
func (h *AddressBookShareHandler) Revoke(c fiber.Ctx) error {
	u := c.Locals("user").(*user.User)
	addressBookID, ok := h.resolveOwnedAddressBookID(c, u.ID)
	if !ok {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "not_found"})
	}
	shareUUID := c.Params("share_id")

	if err := h.revokeUC.Execute(c.Context(), u.ID, addressBookID, shareUUID); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	return c.SendStatus(fiber.StatusNoContent)
}
