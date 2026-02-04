package http

import (
	"strconv"

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
	createContactUC *addressbook.CreateContactUseCase
}

func NewAddressBookHandler(
	createUC *addressbook.CreateUseCase,
	listUC *addressbook.ListUseCase,
	getUC *addressbook.GetUseCase,
	updateUC *addressbook.UpdateUseCase,
	deleteUC *addressbook.DeleteUseCase,
	exportUC *addressbook.ExportUseCase,
	createContactUC *addressbook.CreateContactUseCase,
) *AddressBookHandler {
	return &AddressBookHandler{
		createUC:        createUC,
		listUC:          listUC,
		getUC:           getUC,
		updateUC:        updateUC,
		deleteUC:        deleteUC,
		exportUC:        exportUC,
		createContactUC: createContactUC,
	}
}

// Create godoc
// @Summary      Create address book
// @Description  Create a new address book
// @Tags         AddressBooks
// @Accept       json
// @Produce      json
// @Param        request  body      dto.CreateAddressBookRequest  true  "Address book details"
// @Success      201      {object}  domainaddressbook.AddressBook
// @Failure      400      {object}  ErrorResponseBody
// @Failure      401      {object}  ErrorResponseBody
// @Failure      500      {object}  ErrorResponseBody
// @Security     BearerAuth
// @Router       /addressbooks [post]
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
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(fiber.StatusCreated).JSON(ab)
}

// List godoc
// @Summary      List address books
// @Description  Get all address books for the user
// @Tags         AddressBooks
// @Produce      json
// @Success      200  {object}  object{addressbooks=[]domainaddressbook.AddressBook}
// @Failure      401  {object}  ErrorResponseBody
// @Failure      500  {object}  ErrorResponseBody
// @Security     BearerAuth
// @Router       /addressbooks [get]
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

// Get godoc
// @Summary      Get address book
// @Description  Get address book by ID
// @Tags         AddressBooks
// @Produce      json
// @Param        id   path      integer  true  "Address Book ID"
// @Success      200  {object}  domainaddressbook.AddressBook
// @Failure      400  {object}  ErrorResponseBody
// @Failure      401  {object}  ErrorResponseBody
// @Failure      404  {object}  ErrorResponseBody
// @Failure      500  {object}  ErrorResponseBody
// @Security     BearerAuth
// @Router       /addressbooks/{id} [get]
func (h *AddressBookHandler) Get(c fiber.Ctx) error {
	userID, err := GetUserIDFromContext(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_id"})
	}

	ab, err := h.getUC.Execute(c.Context(), uint(id), userID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if ab == nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "not_found"})
	}

	return c.JSON(ab)
}

// Update godoc
// @Summary      Update address book
// @Description  Update address book details
// @Tags         AddressBooks
// @Accept       json
// @Produce      json
// @Param        id       path      integer                       true  "Address Book ID"
// @Param        request  body      dto.UpdateAddressBookRequest  true  "Update details"
// @Success      200      {object}  domainaddressbook.AddressBook
// @Failure      400      {object}  ErrorResponseBody
// @Failure      401      {object}  ErrorResponseBody
// @Failure      500      {object}  ErrorResponseBody
// @Security     BearerAuth
// @Router       /addressbooks/{id} [patch]
func (h *AddressBookHandler) Update(c fiber.Ctx) error {
	userID, err := GetUserIDFromContext(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_id"})
	}

	var req dto.UpdateAddressBookRequest
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_request"})
	}

	input := addressbook.UpdateInput{
		ID:          uint(id),
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

// Delete godoc
// @Summary      Delete address book
// @Description  Delete address book
// @Tags         AddressBooks
// @Accept       json
// @Param        id       path      integer                       true  "Address Book ID"
// @Param        request  body      dto.DeleteAddressBookRequest  true  "Confirmation"
// @Success      204
// @Failure      400  {object}  ErrorResponseBody
// @Failure      401  {object}  ErrorResponseBody
// @Security     BearerAuth
// @Router       /addressbooks/{id} [delete]
func (h *AddressBookHandler) Delete(c fiber.Ctx) error {
	userID, err := GetUserIDFromContext(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_id"})
	}

	var req dto.DeleteAddressBookRequest
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_request"})
	}

	if req.Confirmation != "DELETE" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "confirmation_required"})
	}

	if err := h.deleteUC.Execute(c.Context(), uint(id), userID); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// Export godoc
// @Summary      Export address book
// @Description  Export address book as vCard
// @Tags         Import/Export
// @Produce      text/vcard
// @Param        id   path      integer  true  "Address Book ID"
// @Success      200  {file}    file
// @Failure      400  {object}  ErrorResponseBody
// @Failure      401  {object}  ErrorResponseBody
// @Security     BearerAuth
// @Router       /addressbooks/{id}/export [get]
func (h *AddressBookHandler) Export(c fiber.Ctx) error {
	userID, err := GetUserIDFromContext(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_id"})
	}

	data, filename, err := h.exportUC.Execute(c.Context(), uint(id), userID)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	c.Set("Content-Type", "text/vcard")
	c.Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	return c.Send(data)
}

// CreateContact godoc
// @Summary      Create contact
// @Description  Create a new contact in address book
// @Tags         Contacts
// @Accept       json
// @Produce      json
// @Param        id       path      integer                   true  "Address Book ID"
// @Param        request  body      dto.CreateContactRequest  true  "Contact VCard"
// @Success      201      {object}  object                    "Ref: domain.AddressObject"
// @Failure      400      {object}  ErrorResponseBody
// @Failure      401      {object}  ErrorResponseBody
// @Failure      500      {object}  ErrorResponseBody
// @Security     BearerAuth
// @Router       /addressbooks/{id}/contacts [post]
func (h *AddressBookHandler) CreateContact(c fiber.Ctx) error {
	userID, err := GetUserIDFromContext(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_id"})
	}

	var req dto.CreateContactRequest
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_request"})
	}

	if req.VCardData == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "vcard_data_required"})
	}

	input := addressbook.CreateContactInput{
		AddressBookID: uint(id),
		UserID:        userID,
		VCardData:     req.VCardData,
	}

	obj, err := h.createContactUC.Execute(c.Context(), input)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(fiber.StatusCreated).JSON(obj)
}
