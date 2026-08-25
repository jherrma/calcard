package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListCalendarsReturnsOwnedAndShared(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	bob := e.newUser("bob@example.com")

	own := e.newCalendar(alice.ID, "Work")
	bobs := e.newCalendar(bob.ID, "Bob private")
	shared := e.newCalendar(bob.ID, "Bob shared")
	e.shareCalendar(shared, alice.ID, "read")

	payload, isErr := e.call(alice.ID, "list_calendars", nil)
	require.False(t, isErr)

	byID := map[string]map[string]interface{}{}
	for _, raw := range payload["calendars"].([]interface{}) {
		c := raw.(map[string]interface{})
		byID[c["id"].(string)] = c
	}

	require.Contains(t, byID, own.UUID, "the caller's own calendar must be listed")
	assert.Equal(t, "owner", byID[own.UUID]["permission"])
	assert.Equal(t, false, byID[own.UUID]["shared"])

	require.Contains(t, byID, shared.UUID, "a calendar shared with the caller must be listed")
	assert.Equal(t, "read", byID[shared.UUID]["permission"])
	assert.Equal(t, true, byID[shared.UUID]["shared"])

	// The load-bearing assertion: MCP must not widen what the user can see.
	assert.NotContains(t, byID, bobs.UUID, "another user's unshared calendar must not be listed")
}

func TestCreateEventThenGetEvents(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	cal := e.newCalendar(alice.ID, "Work")

	created, isErr := e.call(alice.ID, "create_event", map[string]interface{}{
		"calendar_id": cal.UUID,
		"title":       "Team Standup",
		"start":       "2026-03-06T09:00:00Z",
		"end":         "2026-03-06T09:30:00Z",
		"location":    "Room 1",
	})
	require.False(t, isErr, "create_event failed: %v", created["error"])
	assert.Equal(t, true, created["created"])

	event := created["event"].(map[string]interface{})
	eventID := event["id"].(string)
	require.NotEmpty(t, eventID, "a created event must carry the id needed to update or delete it")
	assert.Equal(t, cal.UUID, event["calendar_id"],
		"the event must report the calendar UUID the caller passed, not an internal id")

	listed, isErr := e.call(alice.ID, "get_events", map[string]interface{}{
		"calendar_id": cal.UUID,
		"start":       "2026-03-01T00:00:00Z",
		"end":         "2026-03-31T00:00:00Z",
	})
	require.False(t, isErr)
	events := listed["events"].([]interface{})
	require.Len(t, events, 1)
	assert.Equal(t, "Team Standup", events[0].(map[string]interface{})["title"])
}

func TestCreateEventRejectedOnReadOnlyShare(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	bob := e.newUser("bob@example.com")
	cal := e.newCalendar(bob.ID, "Bob")
	e.shareCalendar(cal, alice.ID, "read")

	result := e.callRaw(alice.ID, "create_event", map[string]interface{}{
		"calendar_id": cal.UUID,
		"title":       "Sneaky",
		"start":       "2026-03-06T09:00:00Z",
		"end":         "2026-03-06T10:00:00Z",
	})
	require.True(t, result.IsError, "a read-only share must refuse a write")
	assert.Contains(t, result.Content[0].Text, "read-only")

	// And nothing was written.
	listed, _ := e.call(bob.ID, "get_events", map[string]interface{}{"calendar_id": cal.UUID})
	assert.Empty(t, listed["events"])
}

func TestReadWriteShareCanCreateEvent(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	bob := e.newUser("bob@example.com")
	cal := e.newCalendar(bob.ID, "Bob")
	e.shareCalendar(cal, alice.ID, "read-write")

	created, isErr := e.call(alice.ID, "create_event", map[string]interface{}{
		"calendar_id": cal.UUID,
		"title":       "Joint review",
		"start":       "2026-03-06T09:00:00Z",
		"end":         "2026-03-06T10:00:00Z",
	})
	require.False(t, isErr, "a read-write share must permit a write: %v", created["error"])
	assert.Equal(t, true, created["created"])
}

func TestEventToolsRefuseForeignCalendar(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	bob := e.newUser("bob@example.com")
	bobsCal := e.newCalendar(bob.ID, "Bob private")

	for _, tc := range []struct {
		tool string
		args map[string]interface{}
	}{
		{"get_events", map[string]interface{}{"calendar_id": bobsCal.UUID}},
		{"create_event", map[string]interface{}{
			"calendar_id": bobsCal.UUID, "title": "x",
			"start": "2026-03-06T09:00:00Z", "end": "2026-03-06T10:00:00Z",
		}},
	} {
		result := e.callRaw(alice.ID, tc.tool, tc.args)
		assert.True(t, result.IsError, "%s must refuse another user's calendar", tc.tool)
	}
}

func TestUpdateAndDeleteEvent(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	cal := e.newCalendar(alice.ID, "Work")

	created, _ := e.call(alice.ID, "create_event", map[string]interface{}{
		"calendar_id": cal.UUID,
		"title":       "Draft",
		"start":       "2026-03-06T09:00:00Z",
		"end":         "2026-03-06T10:00:00Z",
	})
	eventID := created["event"].(map[string]interface{})["id"].(string)

	updated, isErr := e.call(alice.ID, "update_event", map[string]interface{}{
		"event_id": eventID,
		"title":    "Final",
		"location": "Room 2",
	})
	require.False(t, isErr, "update_event failed: %v", updated["error"])
	event := updated["event"].(map[string]interface{})
	assert.Equal(t, "Final", event["title"])
	assert.Equal(t, "Room 2", event["location"])
	assert.Equal(t, "2026-03-06T09:00:00Z", event["start"],
		"a partial update must leave untouched fields alone")

	deleted, isErr := e.call(alice.ID, "delete_event", map[string]interface{}{"event_id": eventID})
	require.False(t, isErr, "delete_event failed: %v", deleted["error"])
	assert.Equal(t, true, deleted["deleted"])

	listed, _ := e.call(alice.ID, "get_events", map[string]interface{}{"calendar_id": cal.UUID})
	assert.Empty(t, listed["events"])
}

func TestUpdateEventRejectsMalformedTime(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	cal := e.newCalendar(alice.ID, "Work")
	created, _ := e.call(alice.ID, "create_event", map[string]interface{}{
		"calendar_id": cal.UUID, "title": "x",
		"start": "2026-03-06T09:00:00Z", "end": "2026-03-06T10:00:00Z",
	})
	eventID := created["event"].(map[string]interface{})["id"].(string)

	result := e.callRaw(alice.ID, "update_event", map[string]interface{}{
		"event_id": eventID,
		"start":    "tomorrow morning",
	})
	require.True(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "start",
		"the error must name the offending field so the model can fix the right one")
}

func TestSearchEventsSpansCalendarsAndIgnoresDateWindow(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	bob := e.newUser("bob@example.com")
	own := e.newCalendar(alice.ID, "Work")
	shared := e.newCalendar(bob.ID, "Bob shared")
	e.shareCalendar(shared, alice.ID, "read")
	hidden := e.newCalendar(bob.ID, "Bob private")

	// One match well in the past, one in a shared calendar, one unreachable.
	mustCreate(t, e, alice.ID, own.UUID, "Budget review 2019", "2019-05-06T09:00:00Z", "2019-05-06T10:00:00Z")
	mustCreate(t, e, bob.ID, shared.UUID, "Budget planning", "2026-05-06T09:00:00Z", "2026-05-06T10:00:00Z")
	mustCreate(t, e, bob.ID, hidden.UUID, "Budget secret", "2026-05-07T09:00:00Z", "2026-05-07T10:00:00Z")

	payload, isErr := e.call(alice.ID, "search_events", map[string]interface{}{"query": "budget"})
	require.False(t, isErr, "search_events failed: %v", payload["error"])

	titles := map[string]bool{}
	for _, raw := range payload["matches"].([]interface{}) {
		m := raw.(map[string]interface{})
		titles[m["event"].(map[string]interface{})["title"].(string)] = true
	}

	assert.True(t, titles["Budget review 2019"], "search has no implicit date bound, so a 2019 event must be found")
	assert.True(t, titles["Budget planning"], "a shared calendar must be searched")
	assert.False(t, titles["Budget secret"], "an unshared calendar must not be searched")
}

func TestSearchEventsRejectsShortQuery(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")

	result := e.callRaw(alice.ID, "search_events", map[string]interface{}{"query": "a"})
	require.True(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "2 characters")
}

// mustCreate creates an event through the tool surface and fails the test if it
// does not land.
func mustCreate(t *testing.T, e *testEnv, userID uint, calendarUUID, title, start, end string) string {
	t.Helper()
	created, isErr := e.call(userID, "create_event", map[string]interface{}{
		"calendar_id": calendarUUID,
		"title":       title,
		"start":       start,
		"end":         end,
	})
	require.False(t, isErr, "create_event(%s) failed: %v", title, created["error"])
	return created["event"].(map[string]interface{})["id"].(string)
}
