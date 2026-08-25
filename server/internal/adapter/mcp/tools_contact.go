package mcp

import (
	"encoding/json"
	"strings"

	domaincontact "github.com/jherrma/caldav-server/internal/domain/contact"
	contactuc "github.com/jherrma/caldav-server/internal/usecase/contact"
	searchuc "github.com/jherrma/caldav-server/internal/usecase/search"
)

// maxContactResults caps one page of contacts, for the same reason
// maxEventResults caps events.
const maxContactResults = 200

// addressBookView is how an address book is rendered to the model.
type addressBookView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Permission  string `json:"permission"`
	Shared      bool   `json:"shared"`
	OwnerName   string `json:"owner_name,omitempty"`
}

// contactView is how a contact is rendered to the model.
//
// It exists rather than serializing domaincontact.Contact directly because that
// struct's AddressBookID is the numeric id rendered as a string — despite its
// doc comment claiming a UUID — and handing the model a number it cannot pass
// back to any tool is exactly the kind of dead end this layer must not create.
type contactView struct {
	ID            string   `json:"id"`
	AddressBookID string   `json:"address_book_id"`
	Name          string   `json:"name"`
	GivenName     string   `json:"first_name,omitempty"`
	FamilyName    string   `json:"last_name,omitempty"`
	Organization  string   `json:"organization,omitempty"`
	Title         string   `json:"title,omitempty"`
	Emails        []string `json:"emails,omitempty"`
	Phones        []string `json:"phones,omitempty"`
	Birthday      string   `json:"birthday,omitempty"`
	Notes         string   `json:"notes,omitempty"`
}

func contactViewOf(c *domaincontact.Contact, addressBookUUID string) contactView {
	v := contactView{
		ID:            c.ID,
		AddressBookID: addressBookUUID,
		Name:          displayName(c),
		GivenName:     c.GivenName,
		FamilyName:    c.FamilyName,
		Organization:  c.Organization,
		Title:         c.Title,
		Birthday:      c.Birthday,
		Notes:         c.Notes,
	}
	for _, e := range c.Emails {
		v.Emails = append(v.Emails, e.Value)
	}
	for _, p := range c.Phones {
		v.Phones = append(v.Phones, p.Value)
	}
	return v
}

// displayName picks the best human label available. A contact with only an
// organization (a company entry) would otherwise render as an empty string.
func displayName(c *domaincontact.Contact) string {
	if c.FormattedName != "" {
		return c.FormattedName
	}
	parts := []string{}
	for _, p := range []string{c.GivenName, c.MiddleName, c.FamilyName} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, " ")
	}
	if c.Organization != "" {
		return c.Organization
	}
	return c.Nickname
}

func (s *Server) registerContactTools() {
	s.register(Tool{
		Name: "list_address_books",
		Description: "List the address books the signed-in user can see, including books shared " +
			"with them. Returns each book's id (a UUID, required by the contact tools) and the " +
			"caller's permission on it.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
	}, s.toolListAddressBooks)

	s.register(Tool{
		Name: "get_contacts",
		Description: "List contacts in one address book, ordered by name. Use limit/offset to " +
			"page; prefer search_contacts when looking for someone specific.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "address_book_id": {"type": "string", "description": "Address book UUID from list_address_books"},
    "limit": {"type": "integer", "description": "Page size, default 50, max 200"},
    "offset": {"type": "integer", "description": "Contacts to skip, default 0"}
  },
  "required": ["address_book_id"],
  "additionalProperties": false
}`),
	}, s.toolGetContacts)

	s.register(Tool{
		Name: "search_contacts",
		Description: "Search contacts by name, email, phone or organization across every address " +
			"book the user can read, owned and shared.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {"type": "string", "description": "Search text, at least 2 characters"},
    "limit": {"type": "integer", "description": "Maximum matches, default 20, max 100"}
  },
  "required": ["query"],
  "additionalProperties": false
}`),
	}, s.toolSearchContacts)

	s.register(Tool{
		Name:        "create_contact",
		Description: "Create a contact in an address book.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "address_book_id": {"type": "string", "description": "Address book UUID from list_address_books"},
    "first_name": {"type": "string"},
    "last_name": {"type": "string"},
    "email": {"type": "string"},
    "phone": {"type": "string"},
    "organization": {"type": "string"},
    "title": {"type": "string", "description": "Job title"},
    "birthday": {"type": "string", "description": "YYYY-MM-DD"},
    "notes": {"type": "string"}
  },
  "required": ["address_book_id"],
  "additionalProperties": false
}`),
	}, s.toolCreateContact)

	s.register(Tool{
		Name: "update_contact",
		Description: "Update a contact. Only the fields you pass are changed. Note that email and " +
			"phone REPLACE the contact's entire email or phone list — a contact with several " +
			"addresses is better edited in the web UI.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "contact_id": {"type": "string", "description": "Contact UUID from get_contacts or search_contacts"},
    "first_name": {"type": "string"},
    "last_name": {"type": "string"},
    "email": {"type": "string", "description": "Replaces all stored email addresses"},
    "phone": {"type": "string", "description": "Replaces all stored phone numbers"},
    "organization": {"type": "string"},
    "title": {"type": "string"},
    "birthday": {"type": "string", "description": "YYYY-MM-DD"},
    "notes": {"type": "string"}
  },
  "required": ["contact_id"],
  "additionalProperties": false
}`),
	}, s.toolUpdateContact)

	s.register(Tool{
		Name:        "delete_contact",
		Description: "Delete a contact permanently.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "contact_id": {"type": "string", "description": "Contact UUID"}
  },
  "required": ["contact_id"],
  "additionalProperties": false
}`),
	}, s.toolDeleteContact)
}

func (s *Server) toolListAddressBooks(cc *callContext, _ json.RawMessage) (*toolCallResult, *RPCError) {
	books, err := s.deps.AddressBookList.Execute(cc.ctx, cc.userID)
	if err != nil {
		return errorResult("Failed to list address books: " + err.Error()), nil
	}

	views := make([]addressBookView, 0, len(books))
	for _, b := range books {
		v := addressBookView{
			ID:          b.UUID,
			Name:        b.Name,
			Description: b.Description,
			Permission:  b.Permission,
			Shared:      b.Shared,
		}
		if b.Owner != nil {
			v.OwnerName = b.Owner.DisplayName
		}
		views = append(views, v)
	}
	return jsonText(map[string]interface{}{"address_books": views, "count": len(views)})
}

func (s *Server) toolGetContacts(cc *callContext, args json.RawMessage) (*toolCallResult, *RPCError) {
	var in struct {
		AddressBookID string `json:"address_book_id"`
		Limit         int    `json:"limit"`
		Offset        int    `json:"offset"`
	}
	if rpcErr := decodeArgs(args, &in); rpcErr != nil {
		return nil, rpcErr
	}

	abID, perm := s.resolveAddressBook(cc, in.AddressBookID)
	if !perm.CanRead() {
		return errorResult("No address book with id " + in.AddressBookID + " is readable by you."), nil
	}

	limit := in.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > maxContactResults {
		limit = maxContactResults
	}
	offset := in.Offset
	if offset < 0 {
		offset = 0
	}

	out, err := s.deps.ContactList.Execute(cc.ctx, contactuc.ListInput{
		AddressBookID: abID,
		Limit:         limit,
		Offset:        offset,
	})
	if err != nil {
		return errorResult("Failed to list contacts: " + err.Error()), nil
	}

	views := make([]contactView, 0, len(out.Contacts))
	for _, c := range out.Contacts {
		views = append(views, contactViewOf(c, in.AddressBookID))
	}
	return jsonText(map[string]interface{}{
		"contacts":        views,
		"count":           len(views),
		"total":           out.Total,
		"limit":           out.Limit,
		"offset":          out.Offset,
		"address_book_id": in.AddressBookID,
	})
}

func (s *Server) toolSearchContacts(cc *callContext, args json.RawMessage) (*toolCallResult, *RPCError) {
	var in struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if rpcErr := decodeArgs(args, &in); rpcErr != nil {
		return nil, rpcErr
	}
	if len(in.Query) < searchuc.MinQueryLength {
		return errorResult("query must be at least 2 characters."), nil
	}

	out, err := s.deps.Search.Execute(cc.ctx, searchuc.Input{
		UserID: cc.userID,
		Query:  in.Query,
		Types:  []string{searchuc.TypeContacts},
		Limit:  in.Limit,
		Now:    cc.now,
	})
	if err != nil {
		return errorResult("Search failed: " + err.Error()), nil
	}

	views := make([]map[string]interface{}, 0, len(out.Contacts.Items))
	for _, hit := range out.Contacts.Items {
		views = append(views, map[string]interface{}{
			"contact":           contactViewOf(hit.Contact, hit.AddressBookUUID),
			"address_book_name": hit.AddressBookName,
		})
	}

	return jsonText(map[string]interface{}{
		"query":    out.Query,
		"matches":  views,
		"count":    out.Contacts.Count,
		"has_more": out.Contacts.HasMore,
	})
}

func (s *Server) toolCreateContact(cc *callContext, args json.RawMessage) (*toolCallResult, *RPCError) {
	var in struct {
		AddressBookID string `json:"address_book_id"`
		FirstName     string `json:"first_name"`
		LastName      string `json:"last_name"`
		Email         string `json:"email"`
		Phone         string `json:"phone"`
		Organization  string `json:"organization"`
		Title         string `json:"title"`
		Birthday      string `json:"birthday"`
		Notes         string `json:"notes"`
	}
	if rpcErr := decodeArgs(args, &in); rpcErr != nil {
		return nil, rpcErr
	}

	abID, perm := s.resolveAddressBook(cc, in.AddressBookID)
	if !perm.CanRead() {
		return errorResult("No address book with id " + in.AddressBookID + " is readable by you."), nil
	}
	if !perm.CanWrite() {
		return errorResult("You have read-only access to that address book, so contacts cannot be created in it."), nil
	}

	// A contact with no name at all is a row nobody can find again. Require
	// something identifying rather than storing a blank card.
	if in.FirstName == "" && in.LastName == "" && in.Organization == "" {
		return errorResult("Provide at least one of first_name, last_name or organization."), nil
	}

	c := &domaincontact.Contact{
		GivenName:    in.FirstName,
		FamilyName:   in.LastName,
		Organization: in.Organization,
		Title:        in.Title,
		Birthday:     in.Birthday,
		Notes:        in.Notes,
	}
	c.FormattedName = displayName(c)
	if in.Email != "" {
		c.Emails = []domaincontact.Email{{Type: "home", Value: in.Email, Primary: true}}
	}
	if in.Phone != "" {
		c.Phones = []domaincontact.Phone{{Type: "cell", Value: in.Phone, Primary: true}}
	}

	created, err := s.deps.ContactCreate.Execute(cc.ctx, cc.userID, abID, c)
	if err != nil {
		return errorResult("Failed to create the contact: " + err.Error()), nil
	}

	return jsonText(map[string]interface{}{
		"created": true,
		"contact": contactViewOf(created, in.AddressBookID),
	})
}

func (s *Server) toolUpdateContact(cc *callContext, args json.RawMessage) (*toolCallResult, *RPCError) {
	var in struct {
		ContactID    string  `json:"contact_id"`
		FirstName    *string `json:"first_name"`
		LastName     *string `json:"last_name"`
		Email        *string `json:"email"`
		Phone        *string `json:"phone"`
		Organization *string `json:"organization"`
		Title        *string `json:"title"`
		Birthday     *string `json:"birthday"`
		Notes        *string `json:"notes"`
	}
	if rpcErr := decodeArgs(args, &in); rpcErr != nil {
		return nil, rpcErr
	}

	abID, perm := s.contactPermission(cc, in.ContactID)
	if !perm.CanRead() {
		return errorResult("No contact with id " + in.ContactID + " is readable by you."), nil
	}
	if !perm.CanWrite() {
		return errorResult("You have read-only access to the address book holding that contact."), nil
	}

	input := contactuc.UpdateInput{
		GivenName:    in.FirstName,
		FamilyName:   in.LastName,
		Organization: in.Organization,
		Title:        in.Title,
		Birthday:     in.Birthday,
		Notes:        in.Notes,
	}
	if in.Email != nil {
		emails := []domaincontact.Email{}
		if *in.Email != "" {
			emails = append(emails, domaincontact.Email{Type: "home", Value: *in.Email, Primary: true})
		}
		input.Emails = &emails
	}
	if in.Phone != nil {
		phones := []domaincontact.Phone{}
		if *in.Phone != "" {
			phones = append(phones, domaincontact.Phone{Type: "cell", Value: *in.Phone, Primary: true})
		}
		input.Phones = &phones
	}
	// The formatted name is what clients display. Leaving it stale after a
	// name change would make the contact still read as its old name in every
	// DAV client, so it is recomputed whenever a name component moves.
	if in.FirstName != nil || in.LastName != nil || in.Organization != nil {
		if current, err := s.deps.ContactGet.Execute(cc.ctx, abID, in.ContactID); err == nil && current != nil {
			merged := *current
			if in.FirstName != nil {
				merged.GivenName = *in.FirstName
			}
			if in.LastName != nil {
				merged.FamilyName = *in.LastName
			}
			if in.Organization != nil {
				merged.Organization = *in.Organization
			}
			merged.FormattedName = ""
			name := displayName(&merged)
			input.FormattedName = &name
		}
	}

	updated, err := s.deps.ContactUpdate.Execute(cc.ctx, abID, in.ContactID, input)
	if err != nil {
		return errorResult("Failed to update the contact: " + err.Error()), nil
	}

	return jsonText(map[string]interface{}{
		"updated": true,
		"contact": contactViewOf(updated, s.addressBookUUID(cc, abID)),
	})
}

func (s *Server) toolDeleteContact(cc *callContext, args json.RawMessage) (*toolCallResult, *RPCError) {
	var in struct {
		ContactID string `json:"contact_id"`
	}
	if rpcErr := decodeArgs(args, &in); rpcErr != nil {
		return nil, rpcErr
	}

	abID, perm := s.contactPermission(cc, in.ContactID)
	if !perm.CanRead() {
		return errorResult("No contact with id " + in.ContactID + " is readable by you."), nil
	}
	if !perm.CanWrite() {
		return errorResult("You have read-only access to the address book holding that contact."), nil
	}

	if err := s.deps.ContactDelete.Execute(cc.ctx, abID, in.ContactID); err != nil {
		return errorResult("Failed to delete the contact: " + err.Error()), nil
	}

	return jsonText(map[string]interface{}{"deleted": true, "contact_id": in.ContactID})
}

// addressBookUUID maps a numeric book id back to the UUID the model uses, with
// the same "" -on-failure contract as calendarUUID.
func (s *Server) addressBookUUID(cc *callContext, abID uint) string {
	ab, err := s.deps.AddressBookRepo.GetByID(cc.ctx, abID)
	if err != nil || ab == nil {
		return ""
	}
	return ab.UUID
}
