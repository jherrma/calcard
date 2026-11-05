package calendar_test

import (
	"errors"
	"testing"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSupportsComponent pins the one component-set rule (#170) for every value
// supported_components holds: "VEVENT" (calendar), "VTODO" (task list),
// "VEVENT,VTODO" (mixed) and the legacy "" (both, as calendarComponentSet has
// always advertised). Nothing else is stored, so nothing else is supported.
func TestSupportsComponent(t *testing.T) {
	cases := []struct {
		stored      string
		event, todo bool
	}{
		{"VEVENT", true, false},
		{"VTODO", false, true},
		{"VEVENT,VTODO", true, true},
		{"", true, true},
	}
	for _, tc := range cases {
		t.Run("stored="+tc.stored, func(t *testing.T) {
			cal := &calendar.Calendar{SupportedComponents: tc.stored}
			assert.Equal(t, tc.event, calendar.SupportsComponent(cal, calendar.ComponentEvent), "VEVENT")
			assert.Equal(t, tc.todo, calendar.SupportsComponent(cal, calendar.ComponentToDo), "VTODO")
			assert.False(t, calendar.SupportsComponent(cal, "VJOURNAL"), "no stored value supports VJOURNAL")
			assert.False(t, calendar.SupportsComponent(cal, ""), "an empty component type is never supported")
		})
	}

	t.Run("nil calendar stores nothing", func(t *testing.T) {
		assert.False(t, calendar.SupportsComponent(nil, calendar.ComponentEvent))
		assert.False(t, calendar.SupportsComponent(nil, calendar.ComponentToDo))
	})
}

// TestSupportedComponentsForKind pins the closed kind enum of
// POST /api/v1/calendars (#170): each kind maps to the one value MKCALENDAR
// would store for the same set, and anything else, near misses included, is a
// 400-class ErrInvalidInput with one fixed message.
func TestSupportedComponentsForKind(t *testing.T) {
	for kind, want := range map[string]string{
		calendar.KindEvents: "VEVENT",
		calendar.KindTasks:  "VTODO",
		calendar.KindBoth:   "VEVENT,VTODO",
	} {
		got, err := calendar.SupportedComponentsForKind(kind)
		require.NoError(t, err, kind)
		assert.Equal(t, want, got, kind)
		// And back: the stored value reports the kind it was created from.
		assert.Equal(t, kind, calendar.KindOf(&calendar.Calendar{SupportedComponents: got}), "round trip of %q", kind)
	}

	for _, kind := range []string{"", "Tasks", "EVENTS", " tasks", "tasks ", "task", "todo", "VTODO",
		"VEVENT,VTODO", "journal", "all", "events,tasks"} {
		got, err := calendar.SupportedComponentsForKind(kind)
		require.Error(t, err, "kind %q", kind)
		assert.Empty(t, got, "kind %q", kind)
		assert.True(t, errors.Is(err, calendar.ErrInvalidInput), "kind %q must be a 400, not a server failure", kind)
		assert.Equal(t, `kind must be "events", "tasks" or "both"`, err.Error(), "the message is the REST body, verbatim")
	}
}

// TestKindOf pins the read-only kind every calendar response carries (#170)
// for every value supported_components holds, including the legacy "" (both,
// as it advertises both over CalDAV).
func TestKindOf(t *testing.T) {
	for stored, want := range map[string]string{
		"VEVENT":       calendar.KindEvents,
		"VTODO":        calendar.KindTasks,
		"VEVENT,VTODO": calendar.KindBoth,
		"":             calendar.KindBoth,
	} {
		assert.Equal(t, want, calendar.KindOf(&calendar.Calendar{SupportedComponents: stored}), "stored=%q", stored)
	}
	// Never written by any code path; pinned so the answer stays a valid kind.
	assert.Equal(t, calendar.KindEvents, calendar.KindOf(&calendar.Calendar{SupportedComponents: "VJOURNAL"}))
	assert.Equal(t, calendar.KindEvents, calendar.KindOf(nil))
}

// TestCalendarObjectIsEvent pins which objects the event routes, the MCP event
// tools and the event use cases serve (#170): VEVENT objects only.
func TestCalendarObjectIsEvent(t *testing.T) {
	for _, tc := range []struct {
		componentType string
		want          bool
	}{
		{calendar.ComponentEvent, true},
		{calendar.ComponentToDo, false},
		{"VJOURNAL", false},
		{"", false},
		{"vevent", false}, // never stored: PopulateDenormFieldsFromICal copies go-ical's upper-case name
	} {
		obj := &calendar.CalendarObject{ComponentType: tc.componentType}
		assert.Equal(t, tc.want, obj.IsEvent(), "ComponentType %q", tc.componentType)
	}
	var none *calendar.CalendarObject
	assert.False(t, none.IsEvent(), "a nil object is not an event")
}

// TestRequireComponent pins the refusal every write path reports: the REST 400
// bodies of spec §3.1, typed so a handler maps it without matching text.
func TestRequireComponent(t *testing.T) {
	taskList := &calendar.Calendar{SupportedComponents: "VTODO"}
	eventCalendar := &calendar.Calendar{SupportedComponents: "VEVENT"}
	mixed := &calendar.Calendar{SupportedComponents: "VEVENT,VTODO"}
	legacy := &calendar.Calendar{SupportedComponents: ""}

	for _, cal := range []*calendar.Calendar{eventCalendar, mixed, legacy} {
		assert.NoError(t, calendar.RequireComponent(cal, calendar.ComponentEvent), "stored=%q", cal.SupportedComponents)
	}
	for _, cal := range []*calendar.Calendar{taskList, mixed, legacy} {
		assert.NoError(t, calendar.RequireComponent(cal, calendar.ComponentToDo), "stored=%q", cal.SupportedComponents)
	}

	for _, tc := range []struct {
		cal       *calendar.Calendar
		component string
		message   string
	}{
		{taskList, calendar.ComponentEvent, "This calendar does not store events"},
		{eventCalendar, calendar.ComponentToDo, "This calendar does not store tasks"},
	} {
		err := calendar.RequireComponent(tc.cal, tc.component)
		require.Error(t, err)
		assert.Equal(t, tc.message, err.Error(), "the message is the REST body, verbatim")
		assert.True(t, errors.Is(err, calendar.ErrComponentNotStored))
		assert.True(t, errors.Is(err, calendar.ErrInvalidInput), "a refusal the caller can correct (400), not a server failure")
	}
}

// TestConflictf pins the typed conflict (#170) the REST layer answers with 409
// and MCP with a tool error: the message is exactly the text (no sentinel
// prefix, the response body depends on it), it is an ErrConflict, and it is
// neither an ErrInvalidInput (400) nor an ErrNotFound (404).
func TestConflictf(t *testing.T) {
	err := calendar.Conflictf("Cannot narrow a calendar's component set")
	assert.Equal(t, "Cannot narrow a calendar's component set", err.Error())
	assert.True(t, errors.Is(err, calendar.ErrConflict))
	assert.False(t, errors.Is(err, calendar.ErrInvalidInput), "a conflict is not a 400")
	assert.False(t, errors.Is(err, calendar.ErrNotFound), "a conflict is not a 404")

	// %w is honoured, and a wrapped conflict is still a conflict.
	cause := errors.New("cause")
	wrapped := calendar.Conflictf("busy: %w", cause)
	assert.Equal(t, "busy: cause", wrapped.Error())
	assert.True(t, errors.Is(wrapped, cause))
	assert.True(t, errors.Is(wrapped, calendar.ErrConflict))
	assert.True(t, errors.Is(errors.Join(errors.New("outer"), wrapped), calendar.ErrConflict))
}

// TestReadableCalendarSupportsComponent (#170): the slim row
// ListReadableCalendarIDs returns applies the one component-set rule
// (SupportsComponent), the legacy "" included.
func TestReadableCalendarSupportsComponent(t *testing.T) {
	for _, set := range []string{"VEVENT", "VTODO", "VEVENT,VTODO", "", "VJOURNAL", " VTODO "} {
		rc := calendar.ReadableCalendar{SupportedComponents: set}
		full := &calendar.Calendar{SupportedComponents: set}
		for _, comp := range []string{calendar.ComponentEvent, calendar.ComponentToDo, "VJOURNAL"} {
			assert.Equal(t, calendar.SupportsComponent(full, comp), rc.SupportsComponent(comp), "%q %s", set, comp)
		}
	}
	assert.True(t, calendar.ReadableCalendar{SupportedComponents: ""}.SupportsComponent(calendar.ComponentToDo))
	assert.True(t, calendar.ReadableCalendar{SupportedComponents: "VTODO"}.SupportsComponent(calendar.ComponentToDo))
	assert.False(t, calendar.ReadableCalendar{SupportedComponents: "VEVENT"}.SupportsComponent(calendar.ComponentToDo))
}
