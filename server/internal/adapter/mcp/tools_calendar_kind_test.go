package mcp

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// kindsByName maps each listed calendar's name to its kind.
func kindsByName(t *testing.T, listed map[string]interface{}) map[string]interface{} {
	t.Helper()
	out := map[string]interface{}{}
	for _, raw := range listed["calendars"].([]interface{}) {
		view := raw.(map[string]interface{})
		require.Contains(t, view, "kind", "every calendar view carries its kind")
		out[view["name"].(string)] = view["kind"]
	}
	return out
}

// TestCalendarOutputCarriesKind pins `kind` on every MCP calendar output (#170):
// list_calendars, the calendars://list resource and the collection tools. A
// task list holds no events, so without the kind a model reads it as an empty
// calendar and tries to put events into it.
func TestCalendarOutputCarriesKind(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	bob := e.newUser("bob@example.com")

	// newCalendar stores "VEVENT"; the others get their set written directly.
	withSet := func(cal *calendar.Calendar, set string) *calendar.Calendar {
		require.NoError(t, e.db.Model(&calendar.Calendar{}).Where("id = ?", cal.ID).
			Update("supported_components", set).Error)
		return cal
	}
	e.newCalendar(alice.ID, "Work")
	errands := withSet(e.newCalendar(alice.ID, "Errands"), "VTODO")
	withSet(e.newCalendar(alice.ID, "Mixed"), "VEVENT,VTODO")
	withSet(e.newCalendar(alice.ID, "Legacy"), "")
	chores := withSet(e.newCalendar(bob.ID, "Chores"), "VTODO")
	e.shareCalendar(chores, alice.ID, "read")

	want := map[string]interface{}{
		"Work":    "events",
		"Errands": "tasks",
		"Mixed":   "both",
		"Legacy":  "both",
		"Chores":  "tasks",
	}

	t.Run("list_calendars", func(t *testing.T) {
		listed, isErr := e.call(alice.ID, "list_calendars", nil)
		require.False(t, isErr, "list_calendars failed: %v", listed["error"])
		assert.Equal(t, want, kindsByName(t, listed))
	})

	t.Run("calendars://list", func(t *testing.T) {
		assert.Equal(t, want, kindsByName(t, readResourceJSON(t, e, alice.ID, uriCalendarList)))
	})

	t.Run("create_calendar makes an event calendar", func(t *testing.T) {
		// The tool has no kind argument; the use case's default applies.
		created, isErr := e.call(alice.ID, "create_calendar", map[string]interface{}{"name": "Climbing"})
		require.False(t, isErr, "create_calendar failed: %v", created["error"])
		view := created["calendar"].(map[string]interface{})
		assert.Equal(t, "events", view["kind"])

		stored, err := e.calRepo.GetByUUID(e.ctx(), view["id"].(string))
		require.NoError(t, err)
		assert.Equal(t, "VEVENT", stored.SupportedComponents)
	})

	t.Run("update_calendar reports the kind it leaves unchanged", func(t *testing.T) {
		updated, isErr := e.call(alice.ID, "update_calendar", map[string]interface{}{
			"calendar_id": errands.UUID, "name": "Errands and chores",
		})
		require.False(t, isErr, "update_calendar failed: %v", updated["error"])
		assert.Equal(t, "tasks", updated["calendar"].(map[string]interface{})["kind"])

		stored, err := e.calRepo.GetByUUID(e.ctx(), errands.UUID)
		require.NoError(t, err)
		assert.Equal(t, "VTODO", stored.SupportedComponents)
	})
}

// TestCalendarToolErrorMapsConflict pins the MCP side of calendar.ErrConflict
// (#170): the collection tools (update_calendar among them) answer a conflict
// as REST's 409 does, with the use case's own message as a tool error — never
// as the generic internal error, and nothing is logged as a server failure.
// update_calendar has no kind argument, so the narrowing refusal cannot reach
// it today; the mapping makes sure a conflict never turns into a 500 there.
func TestCalendarToolErrorMapsConflict(t *testing.T) {
	logs := captureLogs(t)
	for _, err := range []error{
		calendar.Conflictf("Cannot narrow a calendar's component set"),
		fmt.Errorf("update: %w", calendar.Conflictf("Cannot narrow a calendar's component set")),
	} {
		res := calendarToolError("update_calendar", "cal-uuid", err)
		require.True(t, res.IsError)
		require.Len(t, res.Content, 1)
		assert.Equal(t, err.Error(), res.Content[0].Text)
		assert.NotEqual(t, internalErrorText("update_calendar"), res.Content[0].Text)
	}
	assert.Empty(t, logs.String(), "a conflict is not a server failure")

	// The other mappings are unchanged.
	assert.Equal(t, calendarNotOwned("cal-uuid"), calendarToolError("update_calendar", "cal-uuid", calendar.ErrNotFound))
	assert.Equal(t, "bad colour", calendarToolError("update_calendar", "cal-uuid", calendar.Invalidf("bad colour")).Content[0].Text)
	assert.Equal(t, internalErrorText("update_calendar"),
		calendarToolError("update_calendar", "cal-uuid", errors.New("disk full")).Content[0].Text)
}
