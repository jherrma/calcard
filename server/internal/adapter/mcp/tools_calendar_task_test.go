package mcp

import (
	"context"
	"testing"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
	eventuc "github.com/jherrma/caldav-server/internal/usecase/event"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// storeTask writes a task into cal the way a CalDAV PUT does (the MCP surface
// has no task tools) and returns its UUID.
func (e *testEnv) storeTask(cal *calendar.Calendar, uid string) string {
	e.t.Helper()
	obj := &calendar.CalendarObject{
		UUID:       uid + "-uuid",
		CalendarID: cal.ID,
		Path:       uid + ".ics",
		UID:        uid,
		ETag:       calendar.NewETag(),
		ICalData: "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\nBEGIN:VTODO\r\n" +
			"UID:" + uid + "\r\nDTSTAMP:20260301T000000Z\r\nDUE:20260306T090000Z\r\n" +
			"SUMMARY:Renew passport\r\nEND:VTODO\r\nEND:VCALENDAR\r\n",
	}
	require.NoError(e.t, obj.PopulateDenormFieldsFromICal())
	require.Equal(e.t, calendar.ComponentToDo, obj.ComponentType)
	require.NoError(e.t, e.calRepo.CreateCalendarObject(e.ctx(), obj))
	return obj.UUID
}

// TestEventToolsTreatTasksAsUnknown (#170, spec §3.1): update_event and
// delete_event answer a task's UUID with the very text an unknown event id
// gets, write nothing, and never reach the use case's "no VEVENT found". The
// not-found answer comes first, so neither a read-only share, a malformed time
// nor a bad scope gives the task away.
func TestEventToolsTreatTasksAsUnknown(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	bob := e.newUser("bob@example.com")

	narrow := func(cal *calendar.Calendar, components string) *calendar.Calendar {
		require.NoError(t, e.db.Model(cal).Update("supported_components", components).Error)
		cal.SupportedComponents = components
		return cal
	}
	mixed := narrow(e.newCalendar(alice.ID, "Mixed"), "VEVENT,VTODO")
	taskList := narrow(e.newCalendar(alice.ID, "Tasks"), "VTODO")
	readOnly := narrow(e.newCalendar(bob.ID, "Bob read-only"), "VEVENT,VTODO")
	e.shareCalendar(readOnly, alice.ID, "read")
	readWrite := narrow(e.newCalendar(bob.ID, "Bob read-write"), "VEVENT,VTODO")
	e.shareCalendar(readWrite, alice.ID, "read-write")

	tasks := map[string]string{
		"own mixed calendar": e.storeTask(mixed, "task-mixed"),
		"own task list":      e.storeTask(taskList, "task-list"),
		"read-only share":    e.storeTask(readOnly, "task-read-only"),
		"read-write share":   e.storeTask(readWrite, "task-read-write"),
	}
	before := e.dbSnapshot()

	for where, task := range tasks {
		want := "No event with id " + task + " is readable by you."
		for _, tc := range []struct {
			tool string
			args map[string]interface{}
		}{
			{"update_event", map[string]interface{}{"event_id": task, "title": "Hijacked"}},
			{"update_event", map[string]interface{}{"event_id": task, "start": "tomorrow morning"}},
			{"update_event", map[string]interface{}{"event_id": task, "title": "x", "scope": "this", "recurrence_id": "20260306T090000Z"}},
			{"delete_event", map[string]interface{}{"event_id": task}},
			{"delete_event", map[string]interface{}{"event_id": task, "scope": "this"}},
			{"delete_event", map[string]interface{}{"event_id": task, "scope": "this", "recurrence_id": "20260306T090000Z"}},
		} {
			result := e.callRaw(alice.ID, tc.tool, tc.args)
			require.True(t, result.IsError, "%s on a task (%s) must be refused", tc.tool, where)
			assert.Equal(t, want, result.Content[0].Text, "%s %v (%s) must answer like an unknown event", tc.tool, tc.args, where)
		}
	}
	assert.Equal(t, before, e.dbSnapshot(), "no task may be changed or deleted")

	// The reference: an unknown id gets the same sentence.
	unknown := "00000000-0000-4000-8000-000000000001"
	result := e.callRaw(alice.ID, "update_event", map[string]interface{}{"event_id": unknown, "title": "x"})
	assert.Equal(t, "No event with id "+unknown+" is readable by you.", result.Content[0].Text)

	// Events next to the tasks are served as before.
	eventID := mustCreate(t, e, alice.ID, mixed.UUID, "Standup", "2026-03-06T09:00:00Z", "2026-03-06T10:00:00Z")
	updated, isErr := e.call(alice.ID, "update_event", map[string]interface{}{"event_id": eventID, "title": "Daily standup"})
	require.False(t, isErr, "update_event on an event failed: %v", updated["error"])
	deleted, isErr := e.call(alice.ID, "delete_event", map[string]interface{}{"event_id": eventID})
	require.False(t, isErr, "delete_event on an event failed: %v", deleted["error"])
}

// becomesTaskRepo hands the event use cases every object as a VTODO: the
// state a CalDAV PUT leaves when it rewrites an event's resource with a task
// (the UUID is kept) after the tool's eventPermission looked at it.
type becomesTaskRepo struct{ calendar.CalendarRepository }

func (r becomesTaskRepo) GetCalendarObjectByUUID(ctx context.Context, uuid string) (*calendar.CalendarObject, error) {
	obj, err := r.CalendarRepository.GetCalendarObjectByUUID(ctx, uuid)
	if err == nil && obj != nil {
		obj.ComponentType = calendar.ComponentToDo
	}
	return obj, err
}

// TestEventToolsMapUseCaseNotFound: when the use case, not the tool's own
// check, finds a task (eventuc.ErrNotFound), update_event and delete_event
// still give the not-found sentence, not the generic internal error, and write
// nothing.
func TestEventToolsMapUseCaseNotFound(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	cal := e.newCalendar(alice.ID, "Work")
	eventID := mustCreate(t, e, alice.ID, cal.UUID, "Standup", "2026-03-06T09:00:00Z", "2026-03-06T10:00:00Z")

	e.server.deps.EventUpdate = eventuc.NewUpdateEventUseCase(becomesTaskRepo{e.calRepo})
	e.server.deps.EventDelete = eventuc.NewDeleteEventUseCase(becomesTaskRepo{e.calRepo})
	before := e.dbSnapshot()

	want := "No event with id " + eventID + " is readable by you."
	for _, tc := range []struct {
		tool string
		args map[string]interface{}
	}{
		{"update_event", map[string]interface{}{"event_id": eventID, "title": "Hijacked"}},
		{"delete_event", map[string]interface{}{"event_id": eventID}},
	} {
		result := e.callRaw(alice.ID, tc.tool, tc.args)
		require.True(t, result.IsError, tc.tool)
		assert.Equal(t, want, result.Content[0].Text, tc.tool)
	}
	assert.Equal(t, before, e.dbSnapshot(), "nothing may be written")
}
