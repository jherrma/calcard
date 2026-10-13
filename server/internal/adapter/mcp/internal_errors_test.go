package mcp

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// storageFixture is one account with something in every collection type, plus
// a second calendar and address book so the last-collection guards never answer
// instead of the storage failure under test.
type storageFixture struct {
	e         *testEnv
	userID    uint
	calendar  string // UUID
	event     string // UUID
	book      string // UUID
	contact   string // UUID
	calName   string
	otherBook string
}

func newStorageFixture(t *testing.T) *storageFixture {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	cal := e.newCalendar(alice.ID, "Work")
	e.newCalendar(alice.ID, "Spare")
	ab := e.newAddressBook(alice.ID, "Friends")
	other := e.newAddressBook(alice.ID, "Spare")
	return &storageFixture{
		e:         e,
		userID:    alice.ID,
		calendar:  cal.UUID,
		calName:   cal.Name,
		event:     mustCreate(t, e, alice.ID, cal.UUID, "Standup", "2026-03-06T09:00:00Z", "2026-03-06T10:00:00Z"),
		book:      ab.UUID,
		contact:   mustCreateContact(t, e, alice.ID, ab.UUID, "Jane", "Smith"),
		otherBook: other.UUID,
	}
}

// storageFailureCase breaks one table under one tool. cause is what the
// injected failure says — the text that must reach the log and never the model.
type storageFailureCase struct {
	name  string // subtest name; the tool unless a tool has several cases
	tool  string
	brk   func(t *testing.T, f *storageFixture) (cause string)
	args  func(f *storageFixture) map[string]interface{}
	where string // non-empty: the answer names this caller-visible detail
}

func hide(table string) func(*testing.T, *storageFixture) string {
	return func(t *testing.T, f *storageFixture) string {
		f.e.hideTable(t, table)
		return "no such table: " + table
	}
}

func failWritesTo(table string) func(*testing.T, *storageFixture) string {
	return func(t *testing.T, f *storageFixture) string {
		f.e.failWrites(t, table)
		return injectedFailure
	}
}

func storageFailureCases() []storageFailureCase {
	window := func(*storageFixture) map[string]interface{} {
		return map[string]interface{}{
			"start": "2026-03-06T08:00:00Z", "end": "2026-03-06T18:00:00Z", "duration_minutes": 30,
		}
	}
	return []storageFailureCase{
		// calendars and events
		{tool: "list_calendars", brk: hide("calendars"),
			args: func(*storageFixture) map[string]interface{} { return map[string]interface{}{} }},
		{tool: "get_events", brk: hide("calendar_objects"),
			args: func(f *storageFixture) map[string]interface{} {
				return map[string]interface{}{"calendar_id": f.calendar}
			}},
		{tool: "create_event", brk: failWritesTo("calendar_objects"),
			args: func(f *storageFixture) map[string]interface{} {
				return map[string]interface{}{"calendar_id": f.calendar, "title": "New",
					"start": "2026-03-07T09:00:00Z", "end": "2026-03-07T10:00:00Z"}
			}},
		{tool: "update_event", brk: failWritesTo("calendar_objects"),
			args: func(f *storageFixture) map[string]interface{} {
				return map[string]interface{}{"event_id": f.event, "title": "Renamed"}
			}},
		{tool: "delete_event", brk: failWritesTo("calendar_objects"),
			args: func(f *storageFixture) map[string]interface{} { return map[string]interface{}{"event_id": f.event} }},
		{tool: "search_events", brk: hide("calendar_objects"),
			args: func(*storageFixture) map[string]interface{} { return map[string]interface{}{"query": "standup"} }},
		// address books and contacts
		{tool: "list_address_books", brk: hide("address_books"),
			args: func(*storageFixture) map[string]interface{} { return map[string]interface{}{} }},
		{tool: "get_contacts", brk: hide("address_objects"),
			args: func(f *storageFixture) map[string]interface{} {
				return map[string]interface{}{"address_book_id": f.book}
			}},
		{tool: "search_contacts", brk: hide("address_objects"),
			args: func(*storageFixture) map[string]interface{} { return map[string]interface{}{"query": "smith"} }},
		{tool: "create_contact", brk: failWritesTo("address_objects"),
			args: func(f *storageFixture) map[string]interface{} {
				return map[string]interface{}{"address_book_id": f.book, "first_name": "New"}
			}},
		{tool: "update_contact", brk: failWritesTo("address_objects"),
			args: func(f *storageFixture) map[string]interface{} {
				return map[string]interface{}{"contact_id": f.contact, "last_name": "Doe"}
			}},
		{tool: "delete_contact", brk: failWritesTo("address_objects"),
			args: func(f *storageFixture) map[string]interface{} { return map[string]interface{}{"contact_id": f.contact} }},
		// scheduling: the calendar list, then one calendar's events
		{name: "find_free_slots/calendar list", tool: "find_free_slots", brk: hide("calendars"), args: window},
		{name: "find_free_slots/one calendar's events", tool: "find_free_slots", brk: hide("calendar_objects"), args: window,
			where: `while it read the calendar "Work"`},
		// collections
		{tool: "create_calendar", brk: failWritesTo("calendars"),
			args: func(*storageFixture) map[string]interface{} { return map[string]interface{}{"name": "New"} }},
		{tool: "update_calendar", brk: failWritesTo("calendars"),
			args: func(f *storageFixture) map[string]interface{} {
				return map[string]interface{}{"calendar_id": f.calendar, "name": "New"}
			}},
		{tool: "delete_calendar", brk: failWritesTo("calendars"),
			args: func(f *storageFixture) map[string]interface{} {
				return map[string]interface{}{"calendar_id": f.calendar, "confirmation": "DELETE"}
			}},
		{tool: "create_address_book", brk: failWritesTo("address_books"),
			args: func(*storageFixture) map[string]interface{} { return map[string]interface{}{"name": "New"} }},
		{tool: "update_address_book", brk: failWritesTo("address_books"),
			args: func(f *storageFixture) map[string]interface{} {
				return map[string]interface{}{"address_book_id": f.book, "name": "New"}
			}},
		{tool: "delete_address_book", brk: failWritesTo("address_books"),
			args: func(f *storageFixture) map[string]interface{} {
				return map[string]interface{}{"address_book_id": f.otherBook, "confirmation": "DELETE"}
			}},
	}
}

// TestToolsDoNotLeakStorageErrors breaks the schema under every tool and checks
// that the model gets the one generic sentence — no driver text, no table name
// — while the real cause is logged on the server (F28 over MCP).
func TestToolsDoNotLeakStorageErrors(t *testing.T) {
	for _, tc := range storageFailureCases() {
		name := tc.name
		if name == "" {
			name = tc.tool
		}
		t.Run(name, func(t *testing.T) {
			f := newStorageFixture(t)
			args := tc.args(f)
			logs := captureLogs(t)
			cause := tc.brk(t, f)

			result := f.e.callTool(f.userID, tc.tool, args)
			require.True(t, result.IsError, "%s must fail while its table is broken: %s", tc.tool, result.Content[0].Text)
			text := result.Content[0].Text

			want := internalErrorText(tc.tool)
			if tc.where != "" {
				want = internalErrorText(tc.tool + " " + tc.where)
			}
			assert.Equal(t, want, text)
			for _, leak := range []string{injectedFailure, "no such table", "sqlite", "SQL", "_objects", "_hidden"} {
				assert.NotContains(t, text, leak, "a storage error must not reach the model")
			}

			logged := logs.String()
			assert.Contains(t, logged, `"tool":"`+tc.tool+`"`, "the log must say which tool failed")
			assert.Contains(t, logged, cause, "the cause the model is not shown must be logged")
		})
	}
}

// TestStorageFailureCasesCoverEveryTool keeps the table above complete: a tool
// added later without a storage-failure case fails here, not in production.
func TestStorageFailureCasesCoverEveryTool(t *testing.T) {
	e := newTestEnv(t)
	covered := map[string]bool{}
	for _, tc := range storageFailureCases() {
		covered[tc.tool] = true
	}
	var missing []string
	for _, tool := range e.server.toolList {
		if !covered[tool.Name] {
			missing = append(missing, tool.Name)
		}
	}
	sort.Strings(missing)
	assert.Empty(t, missing, "every tool needs a storage-failure case in storageFailureCases")
}

// TestResourcesDoNotLeakStorageErrors is the same check for resources/read,
// which answers a failure as a JSON-RPC error rather than a tool result.
func TestResourcesDoNotLeakStorageErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		uri  func(f *storageFixture) string
		brk  func(t *testing.T, f *storageFixture) string
	}{
		{"calendar list", func(*storageFixture) string { return uriCalendarList }, hide("calendars")},
		{"address book list", func(*storageFixture) string { return uriContactList }, hide("address_books")},
		{"calendar events", func(f *storageFixture) string { return calendarURIStem + f.calendar + eventsURISuffix },
			hide("calendar_objects")},
		{"contact", func(f *storageFixture) string { return contactsURIStem + f.contact },
			// The contact is found and readable, but its stored vCard no longer
			// parses: a server-side failure, not a missing contact.
			func(t *testing.T, f *storageFixture) string {
				require.NoError(t, f.e.db.Model(&addressbook.AddressObject{}).Where("uuid = ?", f.contact).
					Update("VCardData", "BEGIN:VCARD\r\n"+injectedFailure).Error)
				return "failed to parse vcard"
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newStorageFixture(t)
			uri := tc.uri(f)
			logs := captureLogs(t)
			cause := tc.brk(t, f)

			resp := f.e.rpc(f.userID, MethodResourcesRead, map[string]interface{}{"uri": uri})
			require.NotNil(t, resp.Error, "reading %s must fail while its storage is broken", uri)
			assert.Equal(t, CodeInternalError, resp.Error.Code, "a server failure is not a missing resource")
			assert.Equal(t, internalErrorText(MethodResourcesRead), resp.Error.Message)
			for _, leak := range []string{injectedFailure, "no such table", "vcard", "sqlite"} {
				assert.NotContains(t, strings.ToLower(resp.Error.Message), strings.ToLower(leak))
			}

			logged := logs.String()
			assert.Contains(t, logged, `"uri":"`+uri+`"`)
			assert.Contains(t, logged, cause)
		})
	}
}

// TestJSONRPCErrorsDoNotNameGoTypes covers the two params decoders on the tool
// and resource paths: their message describes the expected shape, not the Go
// struct the decoder tripped over.
func TestJSONRPCErrorsDoNotNameGoTypes(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")

	for _, tc := range []struct {
		method string
		params string
	}{
		{MethodToolsCall, `{"name": 5}`},
		{MethodToolsCall, `[]`},
		{MethodResourcesRead, `{"uri": 5}`},
	} {
		resp := e.rpc(alice.ID, tc.method, json.RawMessage(tc.params))
		require.NotNil(t, resp.Error, "%s %s", tc.method, tc.params)
		assert.Equal(t, CodeInvalidParams, resp.Error.Code)
		for _, leak := range []string{"Go ", "struct", "toolCallParams", "resourceReadParams", "of type"} {
			assert.NotContains(t, resp.Error.Message, leak)
		}
	}
}
