package mcp

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRefusalTextsAreExact pins, word for word, every answer a caller CAN act
// on — validation, a refused permission, not found. These are the texts that
// must reach the model unchanged; only failures the caller cannot fix are
// replaced by the generic internal-error sentence. (The collection tools' texts
// are pinned in tools_collection_test.go.)
func TestRefusalTextsAreExact(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	bob := e.newUser("bob@example.com")

	cal := e.newCalendar(alice.ID, "Work")
	event := mustCreate(t, e, alice.ID, cal.UUID, "Standup", "2026-03-06T09:00:00Z", "2026-03-06T10:00:00Z")
	ab := e.newAddressBook(alice.ID, "Friends")
	contact := mustCreateContact(t, e, alice.ID, ab.UUID, "Jane", "Smith")

	readOnlyCal := e.newCalendar(bob.ID, "Bob read-only")
	e.shareCalendar(readOnlyCal, alice.ID, "read")
	bobsEvent := mustCreate(t, e, bob.ID, readOnlyCal.UUID, "Bob's", "2026-03-06T09:00:00Z", "2026-03-06T10:00:00Z")
	readOnlyBook := e.newAddressBook(bob.ID, "Bob read-only")
	e.shareAddressBook(readOnlyBook, alice.ID, "read")
	bobsContact := mustCreateContact(t, e, bob.ID, readOnlyBook.UUID, "Bob", "Contact")

	unknown := uuid.New().String()
	window := func(start, end string, minutes int) map[string]interface{} {
		return map[string]interface{}{"start": start, "end": end, "duration_minutes": minutes}
	}

	for _, tc := range []struct {
		tool string
		args map[string]interface{}
		want string
	}{
		// get_events
		{"get_events", map[string]interface{}{"calendar_id": unknown},
			"No calendar with id " + unknown + " is readable by you."},
		{"get_events", map[string]interface{}{"calendar_id": cal.UUID, "start": "soon"},
			`start must be an RFC 3339 timestamp (e.g. 2026-03-06T09:00:00Z) or a YYYY-MM-DD date, got "soon"`},
		{"get_events", map[string]interface{}{"calendar_id": cal.UUID, "start": "2026-03-07", "end": "2026-03-06"},
			"end must not be before start."},
		// create_event
		{"create_event", map[string]interface{}{"calendar_id": cal.UUID, "start": "2026-03-06T09:00:00Z", "end": "2026-03-06T10:00:00Z"},
			"title is required."},
		{"create_event", map[string]interface{}{"calendar_id": unknown, "title": "x", "start": "2026-03-06T09:00:00Z", "end": "2026-03-06T10:00:00Z"},
			"No calendar with id " + unknown + " is readable by you."},
		{"create_event", map[string]interface{}{"calendar_id": readOnlyCal.UUID, "title": "x", "start": "2026-03-06T09:00:00Z", "end": "2026-03-06T10:00:00Z"},
			"You have read-only access to that calendar, so events cannot be created in it."},
		{"create_event", map[string]interface{}{"calendar_id": cal.UUID, "title": "x", "start": "2026-03-06T10:00:00Z", "end": "2026-03-06T09:00:00Z"},
			"invalid event input: end time is before start time"},
		// update_event
		{"update_event", map[string]interface{}{"event_id": unknown, "title": "x"},
			"No event with id " + unknown + " is readable by you."},
		{"update_event", map[string]interface{}{"event_id": bobsEvent, "title": "x"},
			"You have read-only access to the calendar holding that event."},
		{"update_event", map[string]interface{}{"event_id": event, "start": "tomorrow morning"},
			"start must be an RFC 3339 timestamp, got tomorrow morning"},
		{"update_event", map[string]interface{}{"event_id": event, "start": "2026-03-06T11:00:00Z", "end": "2026-03-06T10:00:00Z"},
			"invalid event input: end time is before start time"},
		// delete_event
		{"delete_event", map[string]interface{}{"event_id": unknown},
			"No event with id " + unknown + " is readable by you."},
		{"delete_event", map[string]interface{}{"event_id": bobsEvent},
			"You have read-only access to the calendar holding that event."},
		{"delete_event", map[string]interface{}{"event_id": event, "scope": "this"},
			"invalid event input: invalid scope or recurrence_id for deletion"},
		// search_events
		{"search_events", map[string]interface{}{"query": "a"}, "query must be at least 2 characters."},
		{"search_events", map[string]interface{}{"query": "standup", "end": "later"},
			`end must be an RFC 3339 timestamp (e.g. 2026-03-06T09:00:00Z) or a YYYY-MM-DD date, got "later"`},
		// get_contacts / search_contacts
		{"get_contacts", map[string]interface{}{"address_book_id": unknown},
			"No address book with id " + unknown + " is readable by you."},
		{"search_contacts", map[string]interface{}{"query": "j"}, "query must be at least 2 characters."},
		// create_contact
		{"create_contact", map[string]interface{}{"address_book_id": unknown, "first_name": "x"},
			"No address book with id " + unknown + " is readable by you."},
		{"create_contact", map[string]interface{}{"address_book_id": readOnlyBook.UUID, "first_name": "x"},
			"You have read-only access to that address book, so contacts cannot be created in it."},
		{"create_contact", map[string]interface{}{"address_book_id": ab.UUID, "notes": "no name"},
			"Provide at least one of first_name, last_name or organization."},
		// update_contact / delete_contact
		{"update_contact", map[string]interface{}{"contact_id": unknown, "notes": "x"},
			"No contact with id " + unknown + " is readable by you."},
		{"update_contact", map[string]interface{}{"contact_id": bobsContact, "notes": "x"},
			"You have read-only access to the address book holding that contact."},
		{"delete_contact", map[string]interface{}{"contact_id": unknown},
			"No contact with id " + unknown + " is readable by you."},
		{"delete_contact", map[string]interface{}{"contact_id": bobsContact},
			"You have read-only access to the address book holding that contact."},
		// find_free_slots
		{"find_free_slots", window("2026-03-06T13:00:00Z", "2026-03-06T09:00:00Z", 60), "end must be after start."},
		{"find_free_slots", window("2026-03-06T09:00:00Z", "2027-03-06T09:00:00Z", 60),
			"The window may span at most 90 days; narrow start/end."},
		{"find_free_slots", window("2026-03-06T09:00:00Z", "2026-03-06T13:00:00Z", 0),
			"duration_minutes must be a positive number of minutes."},
		{"find_free_slots", window("2026-03-06T09:00:00Z", "2026-03-06T10:00:00Z", 120),
			"duration_minutes is longer than the whole window."},
		{"find_free_slots", map[string]interface{}{"start": "2026-03-06T09:00:00Z", "end": "2026-03-06T13:00:00Z",
			"duration_minutes": 60, "calendar_ids": []string{unknown}},
			"None of the given calendar_ids are readable by you."},
		{"find_free_slots", window("now", "2026-03-06T13:00:00Z", 60),
			`start must be an RFC 3339 timestamp (e.g. 2026-03-06T09:00:00Z) or a YYYY-MM-DD date, got "now"`},
	} {
		result := e.callRaw(alice.ID, tc.tool, tc.args)
		require.True(t, result.IsError, "%s %v must be refused", tc.tool, tc.args)
		assert.Equal(t, tc.want, result.Content[0].Text, "%s %v", tc.tool, tc.args)
		assert.False(t, strings.Contains(result.Content[0].Text, "internal server error"),
			"%s: a refusal the caller can act on is not an internal error", tc.tool)
	}

	// None of it touched the data.
	stored, err := e.calRepo.GetCalendarObjectByUUID(e.ctx(), event)
	require.NoError(t, err)
	assert.Equal(t, "Standup", stored.Summary)
	got, err := e.abRepo.GetObjectByUUID(e.ctx(), contact)
	require.NoError(t, err)
	require.NotNil(t, got)
}
