package http

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/adapter/http/dto"
	"github.com/jherrma/caldav-server/internal/domain/contact"
	contactuc "github.com/jherrma/caldav-server/internal/usecase/contact"
)

type ContactHandler struct {
	createUC *contactuc.CreateUseCase
	listUC   *contactuc.ListUseCase
	getUC    *contactuc.GetUseCase
	updateUC *contactuc.UpdateUseCase
	deleteUC *contactuc.DeleteUseCase
	searchUC *contactuc.SearchUseCase
	moveUC   *contactuc.MoveUseCase
	photoUC  *contactuc.PhotoUseCase
}

func NewContactHandler(
	createUC *contactuc.CreateUseCase,
	listUC *contactuc.ListUseCase,
	getUC *contactuc.GetUseCase,
	updateUC *contactuc.UpdateUseCase,
	deleteUC *contactuc.DeleteUseCase,
	searchUC *contactuc.SearchUseCase,
	moveUC *contactuc.MoveUseCase,
	photoUC *contactuc.PhotoUseCase,
) *ContactHandler {
	return &ContactHandler{
		createUC: createUC,
		listUC:   listUC,
		getUC:    getUC,
		updateUC: updateUC,
		deleteUC: deleteUC,
		searchUC: searchUC,
		moveUC:   moveUC,
		photoUC:  photoUC,
	}
}

// List godoc
// @Summary      List contacts
// @Description  Get contacts from address book
// @Tags         Contacts
// @Produce      json
// @Param        addressbook_id  path      integer  true  "Address Book ID"
// @Param        limit           query     integer  false "Limit (default 50)"
// @Param        offset          query     integer  false "Offset (default 0)"
// @Param        sort            query     string   false "Sort field (default name)"
// @Param        order           query     string   false "Sort order (asc/desc)"
// @Success      200             {object}  contactuc.ListOutput
// @Failure      400             {object}  ErrorResponseBody
// @Failure      500             {object}  ErrorResponseBody
// @Security     BearerAuth
// @Router       /addressbooks/{addressbook_id}/contacts [get]
func (h *ContactHandler) List(c fiber.Ctx) error {
	abID, err := strconv.ParseUint(c.Params("addressbook_id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid addressbook ID"})
	}

	limit := 50
	if l := c.Query("limit"); l != "" {
		if val, err := strconv.Atoi(l); err == nil {
			limit = val
		}
	}
	offset := 0
	if o := c.Query("offset"); o != "" {
		if val, err := strconv.Atoi(o); err == nil {
			offset = val
		}
	}
	sort := c.Query("sort", "name")
	order := c.Query("order", "asc")

	output, err := h.listUC.Execute(c.Context(), contactuc.ListInput{
		AddressBookID: uint(abID),
		Limit:         limit,
		Offset:        offset,
		Sort:          sort,
		Order:         order,
	})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(output)
}

// Get godoc
// @Summary      Get contact
// @Description  Get contact by ID
// @Tags         Contacts
// @Produce      json
// @Param        addressbook_id  path      integer  true  "Address Book ID"
// @Param        contact_id      path      string   true  "Contact UUID"
// @Success      200             {object}  contact.Contact
// @Failure      400             {object}  ErrorResponseBody
// @Failure      404             {object}  ErrorResponseBody
// @Failure      500             {object}  ErrorResponseBody
// @Security     BearerAuth
// @Router       /addressbooks/{addressbook_id}/contacts/{contact_id} [get]
func (h *ContactHandler) Get(c fiber.Ctx) error {
	abID, err := strconv.ParseUint(c.Params("addressbook_id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid addressbook ID"})
	}
	contactID := c.Params("contact_id")

	res, err := h.getUC.Execute(c.Context(), uint(abID), contactID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if res == nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Contact not found"})
	}

	// Populate PhotoURL for separate loading

	if res.Photo != "" {
		res.PhotoURL = fmt.Sprintf("/api/v1/addressbooks/%d/contacts/%s/photo", abID, contactID)
		res.Photo = "" // Clear base64 data to avoid bloating JSON response
	}

	return c.JSON(res)
}

// Create godoc
// @Summary      Create contact
// @Description  Create a new contact
// @Tags         Contacts
// @Accept       json
// @Produce      json
// @Param        addressbook_id  path      integer          true  "Address Book ID"
// @Param        contact         body      contact.Contact  true  "Contact details"
// @Success      201             {object}  contact.Contact
// @Failure      400             {object}  ErrorResponseBody
// @Failure      500             {object}  ErrorResponseBody
// @Security     BearerAuth
// @Router       /addressbooks/{addressbook_id}/contacts [post]
func (h *ContactHandler) Create(c fiber.Ctx) error {
	abID, err := strconv.ParseUint(c.Params("addressbook_id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid addressbook ID"})
	}

	var input contact.Contact
	if err := json.Unmarshal(c.Body(), &input); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid input"})
	}

	userID := c.Locals("user_id").(uint)
	res, err := h.createUC.Execute(c.Context(), userID, uint(abID), &input)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(fiber.StatusCreated).JSON(res)
}

// Update godoc
// @Summary      Update contact
// @Description  Update contact details
// @Tags         Contacts
// @Accept       json
// @Produce      json
// @Param        addressbook_id  path      integer                true  "Address Book ID"
// @Param        contact_id      path      string                 true  "Contact UUID"
// @Param        contact         body      contactuc.UpdateInput  true  "Contact updates"
// @Success      200             {object}  contact.Contact
// @Failure      400             {object}  ErrorResponseBody
// @Failure      404             {object}  ErrorResponseBody
// @Failure      500             {object}  ErrorResponseBody
// @Security     BearerAuth
// @Router       /addressbooks/{addressbook_id}/contacts/{contact_id} [put]
func (h *ContactHandler) Update(c fiber.Ctx) error {
	abID, err := strconv.ParseUint(c.Params("addressbook_id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid addressbook ID"})
	}
	contactID := c.Params("contact_id")

	var input contactuc.UpdateInput
	if err := json.Unmarshal(c.Body(), &input); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid input"})
	}

	res, err := h.updateUC.Execute(c.Context(), uint(abID), contactID, input)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if res == nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Contact not found"})
	}

	return c.JSON(res)
}

// Delete godoc
// @Summary      Delete contact
// @Description  Delete a contact
// @Tags         Contacts
// @Param        addressbook_id  path      integer  true  "Address Book ID"
// @Param        contact_id      path      string   true  "Contact UUID"
// @Success      204
// @Failure      400  {object}  ErrorResponseBody
// @Failure      500  {object}  ErrorResponseBody
// @Security     BearerAuth
// @Router       /addressbooks/{addressbook_id}/contacts/{contact_id} [delete]
func (h *ContactHandler) Delete(c fiber.Ctx) error {
	abID, err := strconv.ParseUint(c.Params("addressbook_id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid addressbook ID"})
	}
	contactID := c.Params("contact_id")

	if err := h.deleteUC.Execute(c.Context(), uint(abID), contactID); err != nil {
		// Can distinguish not found vs others by error type if needed
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// Search godoc
// @Summary      Search contacts
// @Description  Search for contacts by query
// @Tags         Contacts
// @Produce      json
// @Param        q               query     string   true   "Search query"
// @Param        limit           query     integer  false  "Limit (default 20)"
// @Param        addressbook_id  query     integer  false  "Address Book ID filter"
// @Success      200             {object}  contactuc.SearchOutput
// @Failure      400             {object}  ErrorResponseBody
// @Failure      500             {object}  ErrorResponseBody
// @Security     BearerAuth
// @Router       /contacts/search [get]
func (h *ContactHandler) Search(c fiber.Ctx) error {
	userID := c.Locals("user_id").(uint) // Get from middleware
	query := c.Query("q")
	if query == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Query parameter 'q' is required"})
	}
	limit := 20
	if l := c.Query("limit"); l != "" {
		if val, err := strconv.Atoi(l); err == nil {
			limit = val
		}
	}

	var abID *uint
	if val := c.Query("addressbook_id"); val != "" {
		id, err := strconv.ParseUint(val, 10, 32)
		if err == nil {
			idUint := uint(id)
			abID = &idUint
		}
	}

	input := contactuc.SearchInput{
		UserID:        userID,
		Query:         query,
		Limit:         limit,
		AddressBookID: abID,
	}

	output, err := h.searchUC.Execute(c.Context(), input)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(output)
}

// Move godoc
// @Summary      Move contact
// @Description  Move contact to another address book
// @Tags         Contacts
// @Accept       json
// @Produce      json
// @Param        contact_id  path      string                  true  "Contact UUID"
// @Param        request     body      dto.MoveContactRequest  true  "Target address book"
// @Success      200         {object}  contact.Contact
// @Failure      400         {object}  ErrorResponseBody
// @Failure      500         {object}  ErrorResponseBody
// @Security     BearerAuth
// @Router       /contacts/{contact_id}/move [post]
func (h *ContactHandler) Move(c fiber.Ctx) error {
	userID := c.Locals("user_id").(uint)
	contactID := c.Params("contact_id")

	// We don't strictly need addressbook_id from URL but standard REST often includes it.
	// We rely on object lookup.

	var input dto.MoveContactRequest
	if err := json.Unmarshal(c.Body(), &input); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid input"})
	}

	// TargetAddressBookID is expected to be the internal integer ID.

	targetID, err := strconv.ParseUint(input.TargetAddressBookID, 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid target addressbook ID (must be integer)"})
	}

	res, err := h.moveUC.Execute(c.Context(), userID, contactID, uint(targetID))
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(res)
}

// UploadPhoto godoc
// @Summary      Upload contact photo
// @Description  Upload a photo for a contact (JPEG, PNG, GIF)
// @Tags         Contacts
// @Accept       image/jpeg,image/png,image/gif
// @Param        addressbook_id  path  integer  true  "Address Book ID"
// @Param        contact_id      path  string   true  "Contact UUID"
// @Param        file            body  []byte   true  "Photo data"
// @Success      204
// @Failure      400            {object}  ErrorResponseBody
// @Failure      413            {object}  ErrorResponseBody  "Photo too large"
// @Failure      415            {object}  ErrorResponseBody  "Unsupported file type"
// @Failure      500            {object}  ErrorResponseBody
// @Security     BearerAuth
// @Router       /addressbooks/{addressbook_id}/contacts/{contact_id}/photo [put]
func (h *ContactHandler) UploadPhoto(c fiber.Ctx) error {
	abID, err := strconv.ParseUint(c.Params("addressbook_id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid addressbook ID"})
	}
	contactID := c.Params("contact_id")

	data := c.Body()

	// Max size check usually in middleware or config, but check length here explicitly
	if len(data) > 1024*1024 { // 1MB
		return c.Status(fiber.StatusRequestEntityTooLarge).JSON(fiber.Map{"error": "Photo too large (max 1MB)"})
	}

	// Validate file type
	contentType := http.DetectContentType(data)
	allowedTypes := map[string]bool{
		"image/jpeg": true,
		"image/png":  true,
		"image/gif":  true,
	}

	isValid := false
	for t := range allowedTypes {
		if strings.HasPrefix(contentType, t) {
			isValid = true
			break
		}
	}
	if !isValid {
		return c.Status(fiber.StatusUnsupportedMediaType).JSON(fiber.Map{
			"error": fmt.Sprintf("Unsupported file type: %s. Allowed: JPEG, PNG, GIF", contentType),
		})
	}

	if err := h.photoUC.Upload(c.Context(), uint(abID), contactID, data); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// DeletePhoto godoc
// @Summary      Delete contact photo
// @Description  Remove photo from contact
// @Tags         Contacts
// @Param        addressbook_id  path  integer  true  "Address Book ID"
// @Param        contact_id      path  string   true  "Contact UUID"
// @Success      204
// @Failure      400  {object}  ErrorResponseBody
// @Failure      500  {object}  ErrorResponseBody
// @Security     BearerAuth
// @Router       /addressbooks/{addressbook_id}/contacts/{contact_id}/photo [delete]
func (h *ContactHandler) DeletePhoto(c fiber.Ctx) error {
	abID, err := strconv.ParseUint(c.Params("addressbook_id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid addressbook ID"})
	}
	contactID := c.Params("contact_id")

	if err := h.photoUC.Delete(c.Context(), uint(abID), contactID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// ServePhoto godoc
// @Summary      Get contact photo
// @Description  Get contact's photo content
// @Tags         Contacts
// @Produce      image/jpeg,image/png,image/gif
// @Param        addressbook_id  path  integer  true  "Address Book ID"
// @Param        contact_id      path  string   true  "Contact UUID"
// @Success      200             {file}    file
// @Failure      400             {object}  ErrorResponseBody
// @Failure      404             {object}  ErrorResponseBody
// @Failure      500             {object}  ErrorResponseBody
// @Security     BearerAuth
// @Router       /addressbooks/{addressbook_id}/contacts/{contact_id}/photo [get]
func (h *ContactHandler) ServePhoto(c fiber.Ctx) error {
	abID, err := strconv.ParseUint(c.Params("addressbook_id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid addressbook ID"})
	}
	contactID := c.Params("contact_id")

	res, err := h.getUC.Execute(c.Context(), uint(abID), contactID)
	if err != nil || res == nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Photo not found"})
	}

	if res.Photo == "" {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Photo not found"})
	}

	data, err := base64.StdEncoding.DecodeString(res.Photo)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to decode photo"})
	}

	contentType := "image/jpeg" // fallback
	if res.PhotoType != "" {
		contentType = "image/" + strings.ToLower(res.PhotoType)
	}
	c.Set("Content-Type", contentType)
	_, err = c.Write(data)
	return err
}
