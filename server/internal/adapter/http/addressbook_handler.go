package http

import (
	"errors"

	"github.com/jherrma/caldav-server/internal/adapter/http/dto"

	"github.com/gofiber/fiber/v3"
	domainaddressbook "github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/usecase/addressbook"
)

var _ = domainaddressbook.AddressBook{}

type AddressBookHandler struct {
	createUC        *addressbook.CreateUseCase
	listUC          *addressbook.ListUseCase
	getUC           *addressbook.GetUseCase
	updateUC        *addressbook.UpdateUseCase
	deleteUC        *addressbook.DeleteUseCase
	exportUC        *addressbook.ExportUseCase
	addressBookRepo domainaddressbook.Repository
}

func NewAddressBookHandler(
	createUC *addressbook.CreateUseCase,
	listUC *addressbook.ListUseCase,
	getUC *addressbook.GetUseCase,
	updateUC *addressbook.UpdateUseCase,
	deleteUC *addressbook.DeleteUseCase,
	exportUC *addressbook.ExportUseCase,
	addressBookRepo domainaddressbook.Repository,
) *AddressBookHandler {
	return &AddressBookHandler{
		createUC:        createUC,
		listUC:          listUC,
		getUC:           getUC,
		updateUC:        updateUC,
		deleteUC:        deleteUC,
		exportUC:        exportUC,
		addressBookRepo: addressBookRepo,
	}
}

// resolveAddressBookID maps the :id path segment — an address-book UUID, the
// canonical external identifier (#52) — to its internal numeric id. Returns
// (0, false) when the UUID doesn't resolve; callers turn that into a 404 so
// existence isn't leaked (the use cases still enforce ownership via userID).
func (h *AddressBookHandler) resolveAddressBookID(c fiber.Ctx) (uint, bool) {
	ab, err := h.addressBookRepo.GetByUUID(c.Context(), c.Params("id"))
	if err != nil || ab == nil {
		return 0, false
	}
	return ab.ID, true
}

// POST /api/v1/addressbooks
func (h *AddressBookHandler) Create(c fiber.Ctx) error {
	userID, err := GetUserIDFromContext(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	var req dto.CreateAddressBookRequest
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_request"})
	}

	input := addressbook.CreateInput{
		UserID:      userID,
		Name:        req.Name,
		Description: req.Description,
	}

	ab, err := h.createUC.Execute(c.Context(), input)
	if err != nil {
		// User-input validation failures map to 400; genuine repository
		// errors keep 500 so real failures are still easy to spot in logs.
		if errors.Is(err, addressbook.ErrNameRequired) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(fiber.StatusCreated).JSON(ab)
}

// GET /api/v1/addressbooks
func (h *AddressBookHandler) List(c fiber.Ctx) error {
	userID, err := GetUserIDFromContext(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	list, err := h.listUC.Execute(c.Context(), userID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"addressbooks": list})
}

// GET /api/v1/addressbooks/:id
func (h *AddressBookHandler) Get(c fiber.Ctx) error {
	userID, err := GetUserIDFromContext(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	id, ok := h.resolveAddressBookID(c)
	if !ok {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "not_found"})
	}

	ab, err := h.getUC.Execute(c.Context(), id, userID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if ab == nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "not_found"})
	}

	return c.JSON(ab)
}

// PATCH /api/v1/addressbooks/:id
func (h *AddressBookHandler) Update(c fiber.Ctx) error {
	userID, err := GetUserIDFromContext(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	id, ok := h.resolveAddressBookID(c)
	if !ok {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "not_found"})
	}

	var req dto.UpdateAddressBookRequest
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_request"})
	}

	input := addressbook.UpdateInput{
		ID:          id,
		UserID:      userID,
		Name:        req.Name,
		Description: req.Description,
	}

	ab, err := h.updateUC.Execute(c.Context(), input)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(ab)
}

// DELETE /api/v1/addressbooks/:id
func (h *AddressBookHandler) Delete(c fiber.Ctx) error {
	userID, err := GetUserIDFromContext(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	id, ok := h.resolveAddressBookID(c)
	if !ok {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "not_found"})
	}

	var req dto.DeleteAddressBookRequest
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_request"})
	}

	if req.Confirmation != "DELETE" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "confirmation_required"})
	}

	if err := h.deleteUC.Execute(c.Context(), id, userID); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// GET /api/v1/addressbooks/:id/export
func (h *AddressBookHandler) Export(c fiber.Ctx) error {
	userID, err := GetUserIDFromContext(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	id, ok := h.resolveAddressBookID(c)
	if !ok {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "not_found"})
	}

	data, filename, err := h.exportUC.Execute(c.Context(), id, userID)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	c.Set("Content-Type", "text/vcard")
	c.Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	return c.Send(data)
}
