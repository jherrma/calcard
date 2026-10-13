package mcp

import (
	"encoding/json"
	"errors"

	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	addressbookuc "github.com/jherrma/caldav-server/internal/usecase/addressbook"
	calendaruc "github.com/jherrma/caldav-server/internal/usecase/calendar"
)

// The collection tools create, change and delete calendars and address books
// themselves, as opposed to the events and contacts inside them.
//
// Managing a collection is the OWNER's prerogative, exactly as over REST: a
// calendar or address book that is unknown, belongs to someone else or is only
// shared with the caller answers the same "not owned by you" error, so no tool
// reveals that another user's collection exists (F05/F12). That decision is
// not made here — the calendar use cases resolve ownership themselves
// (LoadOwnedCalendar), and the address-book tools resolve it through
// resolveOwnedAddressBook, the mirror of the REST handler's check, before the
// use case checks it again.
//
// Ownership is deliberately NOT derived from calendarPermission: that caps a
// subscribed calendar (story 100) at read, which is right for its events but
// would lock the owner out of renaming or deleting their own subscription.

// deleteConfirmation is the literal the delete tools require, the same one the
// REST DELETE routes take in their body.
const deleteConfirmation = "DELETE"

func (s *Server) registerCollectionTools() {
	s.register(Tool{
		Name: "create_calendar",
		Description: "Create a new calendar owned by the signed-in user. Returns the calendar, " +
			"including its id (a UUID) for create_event and the other calendar tools. Without " +
			"color a random one is picked; without timezone the calendar uses UTC.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "name": {"type": "string", "description": "Calendar name, 1 to 255 characters"},
    "description": {"type": "string", "description": "Optional, at most 1000 characters"},
    "color": {"type": "string", "description": "Hex colour #RRGGBB, e.g. #3b82f6"},
    "timezone": {"type": "string", "description": "IANA timezone, e.g. Europe/Berlin. Events created without a timezone default to it"}
  },
  "required": ["name"],
  "additionalProperties": false
}`),
	}, s.toolCreateCalendar)

	s.register(Tool{
		Name: "update_calendar",
		Description: "Rename a calendar or change its description, color or timezone. Only the " +
			"fields you pass are changed; omitted fields keep their stored value. Only the " +
			"calendar's owner can do this: a calendar shared with the user (permission read or " +
			"read-write in list_calendars) cannot be changed here.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "calendar_id": {"type": "string", "description": "UUID of a calendar the user owns, from list_calendars (permission owner)"},
    "name": {"type": "string", "description": "New name, 1 to 255 characters"},
    "description": {"type": "string", "description": "New description, at most 1000 characters; an empty string clears it"},
    "color": {"type": "string", "description": "Hex colour #RRGGBB"},
    "timezone": {"type": "string", "description": "IANA timezone, e.g. Europe/Berlin"}
  },
  "required": ["calendar_id"],
  "additionalProperties": false
}`),
	}, s.toolUpdateCalendar)

	s.register(Tool{
		Name: "delete_calendar",
		Description: "PERMANENTLY delete a calendar the user owns together with EVERY event in " +
			"it. This cannot be undone, and everyone the calendar is shared with loses it too. " +
			"Always tell the user which calendar (by name) and how many events will be lost, and " +
			"call this only after they have explicitly agreed. Calendars shared with the user " +
			"cannot be deleted here, and the user's last remaining calendar cannot be deleted.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "calendar_id": {"type": "string", "description": "UUID of a calendar the user owns, from list_calendars (permission owner)"},
    "confirmation": {"type": "string", "enum": ["DELETE"], "description": "Must be the literal string DELETE. Send it only after the user has confirmed the deletion"}
  },
  "required": ["calendar_id", "confirmation"],
  "additionalProperties": false
}`),
	}, s.toolDeleteCalendar)

	s.register(Tool{
		Name: "create_address_book",
		Description: "Create a new address book owned by the signed-in user. Returns the book, " +
			"including its id (a UUID) for create_contact and the other contact tools.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "name": {"type": "string", "description": "Address book name"},
    "description": {"type": "string", "description": "Optional"}
  },
  "required": ["name"],
  "additionalProperties": false
}`),
	}, s.toolCreateAddressBook)

	s.register(Tool{
		Name: "update_address_book",
		Description: "Rename an address book or change its description. Only the fields you pass " +
			"are changed; omitted fields keep their stored value. Only the book's owner can do " +
			"this: a book shared with the user (permission read or read-write in " +
			"list_address_books) cannot be changed here.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "address_book_id": {"type": "string", "description": "UUID of an address book the user owns, from list_address_books (permission owner)"},
    "name": {"type": "string", "description": "New name"},
    "description": {"type": "string", "description": "New description; an empty string clears it"}
  },
  "required": ["address_book_id"],
  "additionalProperties": false
}`),
	}, s.toolUpdateAddressBook)

	s.register(Tool{
		Name: "delete_address_book",
		Description: "PERMANENTLY delete an address book the user owns together with EVERY " +
			"contact in it. This cannot be undone, and everyone the book is shared with loses it " +
			"too. Always tell the user which address book (by name) will be lost, and call this " +
			"only after they have explicitly agreed. Books shared with the user cannot be deleted " +
			"here, and the user's last remaining address book cannot be deleted.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "address_book_id": {"type": "string", "description": "UUID of an address book the user owns, from list_address_books (permission owner)"},
    "confirmation": {"type": "string", "enum": ["DELETE"], "description": "Must be the literal string DELETE. Send it only after the user has confirmed the deletion"}
  },
  "required": ["address_book_id", "confirmation"],
  "additionalProperties": false
}`),
	}, s.toolDeleteAddressBook)
}

// calendarNotOwned is the one answer for a calendar the caller may not manage:
// unknown, another user's, or only shared with them. One text for all three,
// like REST's single 404 body, so the answer reveals nothing about whether the
// id exists.
func calendarNotOwned(id string) *toolCallResult {
	return errorResult("No calendar with id " + id + " is owned by you. Only a calendar's owner can change or delete it.")
}

// addressBookNotOwned is calendarNotOwned for address books.
func addressBookNotOwned(id string) *toolCallResult {
	return errorResult("No address book with id " + id + " is owned by you. Only an address book's owner can change or delete it.")
}

// calendarToolError maps a calendar use-case error the way REST's
// calendarError does: not found (missing or not owned) → the "not owned"
// answer, ErrInvalidInput → its bare message (validation, the DELETE
// confirmation, the last-calendar guard), anything else → a generic internal
// error (toolInternalError, errors.go).
func calendarToolError(tool, calendarID string, err error) *toolCallResult {
	switch {
	case errors.Is(err, calendar.ErrNotFound):
		return calendarNotOwned(calendarID)
	case errors.Is(err, calendar.ErrInvalidInput):
		return errorResult(err.Error())
	}
	return toolInternalError(tool, err)
}

// ownedCalendarView renders a calendar the caller owns, as the collection tools
// return it. eventCount is nil when the use case did not count events.
func ownedCalendarView(cal *calendar.Calendar, eventCount *int64) calendarView {
	return calendarView{
		ID:          cal.UUID,
		Name:        cal.Name,
		Description: cal.Description,
		Color:       cal.Color,
		Timezone:    cal.Timezone,
		EventCount:  eventCount,
		Permission:  permissionLabel(calendar.PermissionOwner),
		Shared:      false,
	}
}

// ownedAddressBookView renders an address book the caller owns.
func ownedAddressBookView(ab *addressbook.AddressBook) addressBookView {
	return addressBookView{
		ID:          ab.UUID,
		Name:        ab.Name,
		Description: ab.Description,
		Permission:  "owner",
		Shared:      false,
	}
}

func (s *Server) toolCreateCalendar(cc *callContext, args json.RawMessage) (*toolCallResult, *RPCError) {
	var in struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Color       string `json:"color"`
		Timezone    string `json:"timezone"`
	}
	if bad := decodeArgs(args, &in); bad != nil {
		return bad, nil
	}

	// Validation (name, length limits, colour, IANA zone) and the defaults are
	// the use case's, the same instance POST /api/v1/calendars calls.
	cal, err := s.deps.CalendarCreate.Execute(cc.ctx, cc.userID, calendaruc.CreateCalendarRequest{
		Name:        in.Name,
		Description: in.Description,
		Color:       in.Color,
		Timezone:    in.Timezone,
	})
	if err != nil {
		return calendarToolError("create_calendar", "", err), nil
	}

	// A calendar that was created a moment ago holds no events; that is known
	// without asking the repository.
	var none int64
	return jsonText(map[string]interface{}{
		"created":  true,
		"calendar": ownedCalendarView(cal, &none),
	})
}

func (s *Server) toolUpdateCalendar(cc *callContext, args json.RawMessage) (*toolCallResult, *RPCError) {
	var in struct {
		CalendarID  string  `json:"calendar_id"`
		Name        *string `json:"name"`
		Description *string `json:"description"`
		Color       *string `json:"color"`
		Timezone    *string `json:"timezone"`
	}
	if bad := decodeArgs(args, &in); bad != nil {
		return bad, nil
	}
	if in.CalendarID == "" {
		return errorResult("calendar_id is required."), nil
	}

	// The use case resolves ownership before it validates any field, so a
	// calendar the caller does not own is "not owned" whatever is passed.
	cal, err := s.deps.CalendarUpdate.Execute(cc.ctx, cc.userID, in.CalendarID, calendaruc.UpdateCalendarRequest{
		Name:        in.Name,
		Description: in.Description,
		Color:       in.Color,
		Timezone:    in.Timezone,
	})
	if err != nil {
		return calendarToolError("update_calendar", in.CalendarID, err), nil
	}

	return jsonText(map[string]interface{}{
		"updated":  true,
		"calendar": ownedCalendarView(cal, nil),
	})
}

func (s *Server) toolDeleteCalendar(cc *callContext, args json.RawMessage) (*toolCallResult, *RPCError) {
	var in struct {
		CalendarID   string `json:"calendar_id"`
		Confirmation string `json:"confirmation"`
	}
	if bad := decodeArgs(args, &in); bad != nil {
		return bad, nil
	}
	if in.CalendarID == "" {
		return errorResult("calendar_id is required."), nil
	}

	// The confirmation is checked by the use case, after ownership — the same
	// order as DELETE /api/v1/calendars/:id (F28).
	if err := s.deps.CalendarDelete.Execute(cc.ctx, cc.userID, in.CalendarID, calendaruc.DeleteCalendarRequest{
		Confirmation: in.Confirmation,
	}); err != nil {
		return calendarToolError("delete_calendar", in.CalendarID, err), nil
	}

	// The use case reports neither the name nor how many events went with the
	// calendar, and looking them up here would be an MCP-only query.
	return jsonText(map[string]interface{}{
		"deleted":     true,
		"calendar_id": in.CalendarID,
	})
}

func (s *Server) toolCreateAddressBook(cc *callContext, args json.RawMessage) (*toolCallResult, *RPCError) {
	var in struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if bad := decodeArgs(args, &in); bad != nil {
		return bad, nil
	}

	ab, err := s.deps.AddressBookCreate.Execute(cc.ctx, addressbookuc.CreateInput{
		UserID:      cc.userID,
		Name:        in.Name,
		Description: in.Description,
	})
	if err != nil {
		if errors.Is(err, addressbookuc.ErrNameRequired) {
			return errorResult(err.Error()), nil
		}
		return toolInternalError("create_address_book", err), nil
	}

	return jsonText(map[string]interface{}{
		"created":      true,
		"address_book": ownedAddressBookView(ab),
	})
}

func (s *Server) toolUpdateAddressBook(cc *callContext, args json.RawMessage) (*toolCallResult, *RPCError) {
	var in struct {
		AddressBookID string  `json:"address_book_id"`
		Name          *string `json:"name"`
		Description   *string `json:"description"`
	}
	if bad := decodeArgs(args, &in); bad != nil {
		return bad, nil
	}
	if in.AddressBookID == "" {
		return errorResult("address_book_id is required."), nil
	}

	owned, ok := s.resolveOwnedAddressBook(cc, in.AddressBookID)
	if !ok {
		return addressBookNotOwned(in.AddressBookID), nil
	}

	ab, err := s.deps.AddressBookUpdate.Execute(cc.ctx, addressbookuc.UpdateInput{
		ID:          owned.ID,
		UserID:      cc.userID,
		Name:        in.Name,
		Description: in.Description,
	})
	if err != nil {
		if errors.Is(err, addressbookuc.ErrNotFound) {
			return addressBookNotOwned(in.AddressBookID), nil
		}
		return toolInternalError("update_address_book", err), nil
	}

	return jsonText(map[string]interface{}{
		"updated":      true,
		"address_book": ownedAddressBookView(ab),
	})
}

func (s *Server) toolDeleteAddressBook(cc *callContext, args json.RawMessage) (*toolCallResult, *RPCError) {
	var in struct {
		AddressBookID string `json:"address_book_id"`
		Confirmation  string `json:"confirmation"`
	}
	if bad := decodeArgs(args, &in); bad != nil {
		return bad, nil
	}
	if in.AddressBookID == "" {
		return errorResult("address_book_id is required."), nil
	}

	// Ownership first, then the confirmation — the order of
	// DELETE /api/v1/addressbooks/:id, so a book the caller does not own is
	// "not owned" whatever the confirmation says (F05).
	owned, ok := s.resolveOwnedAddressBook(cc, in.AddressBookID)
	if !ok {
		return addressBookNotOwned(in.AddressBookID), nil
	}
	// REST checks the confirmation in its handler rather than the use case;
	// this is the same check, worded like the calendar use case's.
	if in.Confirmation != deleteConfirmation {
		return errorResult("please type DELETE to confirm address book deletion"), nil
	}

	if err := s.deps.AddressBookDelete.Execute(cc.ctx, owned.ID, cc.userID); err != nil {
		switch {
		case errors.Is(err, addressbookuc.ErrNotFound):
			return addressBookNotOwned(in.AddressBookID), nil
		case errors.Is(err, addressbookuc.ErrLastAddressBook):
			return errorResult(err.Error()), nil
		}
		return toolInternalError("delete_address_book", err), nil
	}

	return jsonText(map[string]interface{}{
		"deleted":         true,
		"address_book_id": in.AddressBookID,
	})
}
