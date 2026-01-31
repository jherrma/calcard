package http

import (
	"strconv"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/usecase/addressbook"
)

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

func (h *AddressBookHandler) Create(c fiber.Ctx) error {
	userID, err := GetUserIDFromContext(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}

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

func (h *AddressBookHandler) Update(c fiber.Ctx) error {
	userID, err := GetUserIDFromContext(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_id"})
	}

	var req struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
	}

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

func (h *AddressBookHandler) Delete(c fiber.Ctx) error {
	userID, err := GetUserIDFromContext(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_id"})
	}

	var req struct {
		Confirmation string `json:"confirmation"`
	}

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

func (h *AddressBookHandler) CreateContact(c fiber.Ctx) error {
	userID, err := GetUserIDFromContext(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_id"})
	}

	var req struct {
		VCardData string `json:"vcard_data"`
	}

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
