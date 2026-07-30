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
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/contact"
	contactuc "github.com/jherrma/caldav-server/internal/usecase/contact"
)

// maxPageLimit caps client-supplied pagination limits to avoid unbounded result
// materialization (memory/CPU DoS).
const maxPageLimit = 200

type ContactHandler struct {
	createUC        *contactuc.CreateUseCase
	listUC          *contactuc.ListUseCase
	getUC           *contactuc.GetUseCase
	updateUC        *contactuc.UpdateUseCase
	deleteUC        *contactuc.DeleteUseCase
	searchUC        *contactuc.SearchUseCase
	moveUC          *contactuc.MoveUseCase
	photoUC         *contactuc.PhotoUseCase
	addressBookRepo addressbook.Repository
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
	addressBookRepo addressbook.Repository,
) *ContactHandler {
	return &ContactHandler{
		createUC:        createUC,
		listUC:          listUC,
		getUC:           getUC,
		updateUC:        updateUC,
		deleteUC:        deleteUC,
		searchUC:        searchUC,
		moveUC:          moveUC,
		photoUC:         photoUC,
		addressBookRepo: addressBookRepo,
	}
}

// addressBookByUUID resolves an address-book UUID — the canonical external
// identifier (#52) — to its internal numeric id together with the authenticated
// user's effective permission on it (#53). A UUID that doesn't resolve yields
// (0, PermissionNone), which is indistinguishable from "exists but you have no
// access": callers emit a 404 in both cases so existence isn't leaked.
//
// Permission covers ownership AND shares, so a read-write sharee can manage
// contacts over REST exactly as they already could over CardDAV. Callers must
// still check the level: reads need CanRead, writes need CanWrite.
func (h *ContactHandler) addressBookByUUID(c fiber.Ctx, uuid string) (uint, addressbook.AddressBookPermission) {
	ab, err := h.addressBookRepo.GetByUUID(c.Context(), uuid)
	if err != nil || ab == nil {
		return 0, addressbook.PermissionNone
	}
	return ab.ID, h.addressBookPermission(c, ab.ID)
}

// addressBookPermission returns the caller's effective permission on the book
// with the given numeric id, collapsing repository errors to PermissionNone
// (fail closed).
func (h *ContactHandler) addressBookPermission(c fiber.Ctx, abID uint) addressbook.AddressBookPermission {
	userID := c.Locals("user_id").(uint)
	perm, err := h.addressBookRepo.GetUserPermission(c.Context(), abID, userID)
	if err != nil {
		return addressbook.PermissionNone
	}
	return perm
}

// resolveAddressBook maps the :addressbook_id path segment (an address-book
// UUID) to its numeric id plus the caller's permission on it.
func (h *ContactHandler) resolveAddressBook(c fiber.Ctx) (uint, addressbook.AddressBookPermission) {
	return h.addressBookByUUID(c, c.Params("addressbook_id"))
}

// requireAddressBookRead resolves :addressbook_id and enforces read access.
// The BOOL decides — not the error. fiber's c.Status(...).JSON(...) returns nil
// when serialization succeeds, so an `if err != nil` check on the response would
// silently treat every rejection as a pass and let the request fall through.
// (Mirrors the ok/err convention EventHandler.ifMatchOK uses.) Callers:
//
//	abID, ok, errResp := h.requireAddressBookRead(c)
//	if !ok {
//		return errResp
//	}
//
// The refusal is always a 404: "no such book" and "not shared with you" must be
// indistinguishable so existence isn't leaked.
func (h *ContactHandler) requireAddressBookRead(c fiber.Ctx) (uint, bool, error) {
	abID, perm := h.resolveAddressBook(c)
	if !perm.CanRead() {
		return 0, false, c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "not_found"})
	}
	return abID, true, nil
}

// requireAddressBookWrite resolves :addressbook_id and enforces write access,
// with the same bool-decides contract as requireAddressBookRead. It preserves
// the 404-vs-403 split used by the event handler: no access at all is a 404
// (don't leak existence), while a book you can genuinely see but only read is an
// honest 403 so the UI can explain why the write was refused.
func (h *ContactHandler) requireAddressBookWrite(c fiber.Ctx) (uint, bool, error) {
	abID, perm := h.resolveAddressBook(c)
	if !perm.CanRead() {
		return 0, false, c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "not_found"})
	}
	if !perm.CanWrite() {
		return 0, false, c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "You have read-only access to this address book"})
	}
	return abID, true, nil
}

// contactPermission returns the caller's permission on the address book that
// contains the given contact (PermissionNone when the contact doesn't exist or
// its book is neither owned by nor shared with the caller).
func (h *ContactHandler) contactPermission(c fiber.Ctx, contactUUID string) addressbook.AddressBookPermission {
	obj, err := h.addressBookRepo.GetObjectByUUID(c.Context(), contactUUID)
	if err != nil || obj == nil {
		return addressbook.PermissionNone
	}
	return h.addressBookPermission(c, obj.AddressBookID)
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
	abID, ok, errResp := h.requireAddressBookRead(c)
	if !ok {
		return errResp
	}

	limit := 50
	if l := c.Query("limit"); l != "" {
		if val, err := strconv.Atoi(l); err == nil {
			limit = val
		}
	}
	if limit > maxPageLimit {
		limit = maxPageLimit
	}
	offset := 0
	if o := c.Query("offset"); o != "" {
		if val, err := strconv.Atoi(o); err == nil && val >= 0 {
			offset = val
		}
	}
	sort := c.Query("sort", "name")
	order := c.Query("order", "asc")

	output, err := h.listUC.Execute(c.Context(), contactuc.ListInput{
		AddressBookID: abID,
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
	abID, ok, errResp := h.requireAddressBookRead(c)
	if !ok {
		return errResp
	}
	contactID := c.Params("contact_id")

	res, err := h.getUC.Execute(c.Context(), abID, contactID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if res == nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Contact not found"})
	}

	// Populate PhotoURL for separate loading. The address-book segment is the
	// UUID (#52) — the same canonical id the photo route now expects — taken
	// straight from the request path rather than the internal numeric id.
	if res.Photo != "" {
		res.PhotoURL = fmt.Sprintf("/api/v1/addressbooks/%s/contacts/%s/photo", c.Params("addressbook_id"), contactID)
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
// @Failure      403             {object}  ErrorResponseBody  "Read-only access to the address book"
// @Failure      400             {object}  ErrorResponseBody
// @Failure      500             {object}  ErrorResponseBody
// @Security     BearerAuth
// @Router       /addressbooks/{addressbook_id}/contacts [post]
func (h *ContactHandler) Create(c fiber.Ctx) error {
	abID, ok, errResp := h.requireAddressBookWrite(c)
	if !ok {
		return errResp
	}

	var input contact.Contact
	if err := json.Unmarshal(c.Body(), &input); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid input"})
	}

	userID := c.Locals("user_id").(uint)
	res, err := h.createUC.Execute(c.Context(), userID, abID, &input)
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
// @Failure      403             {object}  ErrorResponseBody  "Read-only access to the address book"
// @Failure      400             {object}  ErrorResponseBody
// @Failure      404             {object}  ErrorResponseBody
// @Failure      500             {object}  ErrorResponseBody
// @Security     BearerAuth
// @Router       /addressbooks/{addressbook_id}/contacts/{contact_id} [put]
func (h *ContactHandler) Update(c fiber.Ctx) error {
	abID, ok, errResp := h.requireAddressBookWrite(c)
	if !ok {
		return errResp
	}
	contactID := c.Params("contact_id")

	var input contactuc.UpdateInput
	if err := json.Unmarshal(c.Body(), &input); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid input"})
	}

	res, err := h.updateUC.Execute(c.Context(), abID, contactID, input)
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
// @Failure      403             {object}  ErrorResponseBody  "Read-only access to the address book"
// @Failure      400  {object}  ErrorResponseBody
// @Failure      500  {object}  ErrorResponseBody
// @Security     BearerAuth
// @Router       /addressbooks/{addressbook_id}/contacts/{contact_id} [delete]
func (h *ContactHandler) Delete(c fiber.Ctx) error {
	abID, ok, errResp := h.requireAddressBookWrite(c)
	if !ok {
		return errResp
	}
	contactID := c.Params("contact_id")

	if err := h.deleteUC.Execute(c.Context(), abID, contactID); err != nil {
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
	if limit > maxPageLimit {
		limit = maxPageLimit
	}

	// The optional addressbook_id filter is an address-book UUID (#52); resolve
	// it to the internal numeric id, requiring read access. An unresolvable or
	// inaccessible value simply leaves the filter unset (search stays user-wide)
	// rather than erroring — it's an optional narrowing hint.
	//
	// Note the search corpus itself is still owner-scoped: SearchObjects filters
	// by user_id, so contacts in books merely shared with the caller never match.
	// Widening global search to shared books is deliberately out of scope here
	// (#53 is about managing contacts in a book you were given access to); it
	// needs its own repository-level change.
	var abID *uint
	if val := c.Query("addressbook_id"); val != "" {
		if id, perm := h.addressBookByUUID(c, val); perm.CanRead() {
			abID = &id
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
// @Param        addressbook_id  path  string                  true  "Source address book UUID"
// @Param        contact_id  path      string                  true  "Contact UUID"
// @Param        request     body      dto.MoveContactRequest  true  "Target address book"
// @Success      200         {object}  contact.Contact
// @Failure      400         {object}  ErrorResponseBody
// @Failure      403         {object}  ErrorResponseBody  "Read-only access to the source or target address book"
// @Failure      404         {object}  ErrorResponseBody
// @Failure      500         {object}  ErrorResponseBody
// @Security     BearerAuth
// @Router       /addressbooks/{addressbook_id}/contacts/{contact_id}/move [post]
func (h *ContactHandler) Move(c fiber.Ctx) error {
	userID := c.Locals("user_id").(uint)
	contactID := c.Params("contact_id")

	// A move is a write on BOTH collections, so each side is gated separately —
	// mirroring EventHandler.Move. The source is checked before the body is even
	// parsed so a caller with no access learns nothing from a malformed payload.
	srcPerm := h.contactPermission(c, contactID)
	if !srcPerm.CanRead() {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "not_found"})
	}
	if !srcPerm.CanWrite() {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "You have read-only access to the source address book"})
	}

	var input dto.MoveContactRequest
	if err := json.Unmarshal(c.Body(), &input); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid input"})
	}

	// TargetAddressBookID is the target address book's UUID (#52); resolve it to
	// the internal numeric id here and require write access on the target too —
	// otherwise a read-only sharee could push contacts into a book they may only
	// read (or an invisible one could be probed via the response code).
	targetID, targetPerm := h.addressBookByUUID(c, input.TargetAddressBookID)
	if !targetPerm.CanRead() {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "not_found"})
	}
	if !targetPerm.CanWrite() {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "You have read-only access to the target address book"})
	}

	res, err := h.moveUC.Execute(c.Context(), userID, contactID, targetID)
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
// @Failure      403             {object}  ErrorResponseBody  "Read-only access to the address book"
// @Failure      400            {object}  ErrorResponseBody
// @Failure      413            {object}  ErrorResponseBody  "Photo too large"
// @Failure      415            {object}  ErrorResponseBody  "Unsupported file type"
// @Failure      500            {object}  ErrorResponseBody
// @Security     BearerAuth
// @Router       /addressbooks/{addressbook_id}/contacts/{contact_id}/photo [put]
func (h *ContactHandler) UploadPhoto(c fiber.Ctx) error {
	abID, ok, errResp := h.requireAddressBookWrite(c)
	if !ok {
		return errResp
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

	if err := h.photoUC.Upload(c.Context(), abID, contactID, data); err != nil {
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
// @Failure      403             {object}  ErrorResponseBody  "Read-only access to the address book"
// @Failure      400  {object}  ErrorResponseBody
// @Failure      500  {object}  ErrorResponseBody
// @Security     BearerAuth
// @Router       /addressbooks/{addressbook_id}/contacts/{contact_id}/photo [delete]
func (h *ContactHandler) DeletePhoto(c fiber.Ctx) error {
	abID, ok, errResp := h.requireAddressBookWrite(c)
	if !ok {
		return errResp
	}
	contactID := c.Params("contact_id")

	if err := h.photoUC.Delete(c.Context(), abID, contactID); err != nil {
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
	abID, ok, errResp := h.requireAddressBookRead(c)
	if !ok {
		return errResp
	}
	contactID := c.Params("contact_id")

	res, err := h.getUC.Execute(c.Context(), abID, contactID)
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
