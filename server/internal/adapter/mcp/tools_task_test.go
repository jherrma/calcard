package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jherrma/caldav-server/internal/adapter/http/dto"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	taskuc "github.com/jherrma/caldav-server/internal/usecase/task"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The task tools (#170) over the real task use cases and SQLite. The REST
// rendering (dto) of what the database holds is the reference for every
// success: MCP must answer a task exactly as the REST routes do.

// newCalendarWithSet inserts a calendar owned by userID that stores the given
// component set ("VTODO" for a task list).
func (e *testEnv) newCalendarWithSet(userID uint, name, set string) *calendar.Calendar {
	e.t.Helper()
	cal := &calendar.Calendar{
		UUID:                uuid.New().String(),
		UserID:              userID,
		Path:                name,
		Name:                name,
		Color:               "#16a34a",
		Timezone:            "UTC",
		SupportedComponents: set,
	}
	require.NoError(e.t, e.calRepo.Create(e.ctx(), cal))
	return cal
}

// storeTaskLines writes a VTODO with the given content lines (after UID and
// DTSTAMP) into cal the way a CalDAV PUT does and returns its UUID.
func (e *testEnv) storeTaskLines(cal *calendar.Calendar, uid string, lines ...string) string {
	e.t.Helper()
	obj := &calendar.CalendarObject{
		UUID:       uid + "-uuid",
		CalendarID: cal.ID,
		Path:       uid + ".ics",
		UID:        uid,
		ETag:       calendar.NewETag(),
		ICalData: "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\nBEGIN:VTODO\r\n" +
			"UID:" + uid + "\r\nDTSTAMP:20260301T000000Z\r\n" + strings.Join(lines, "\r\n") +
			"\r\nEND:VTODO\r\nEND:VCALENDAR\r\n",
	}
	require.NoError(e.t, obj.PopulateDenormFieldsFromICal())
	require.Equal(e.t, calendar.ComponentToDo, obj.ComponentType)
	require.NoError(e.t, e.calRepo.CreateCalendarObject(e.ctx(), obj))
	return obj.UUID
}

// storeTask writes the default fixture task (due 2026-03-06 09:00 UTC,
// "Renew passport") into cal the way a CalDAV PUT does and returns its UUID.
func (e *testEnv) storeTask(cal *calendar.Calendar, uid string) string {
	e.t.Helper()
	return e.storeTaskLines(cal, uid, "DUE:20260306T090000Z", "SUMMARY:Renew passport")
}

// storedTaskJSON is the REST rendering of the stored task, what
// GET /api/v1/calendars/:calendar_id/tasks/:task_id answers for it.
func (e *testEnv) storedTaskJSON(taskUUID string) string {
	e.t.Helper()
	obj, err := e.calRepo.GetCalendarObjectByUUID(e.ctx(), taskUUID)
	require.NoError(e.t, err)
	cal, err := e.calRepo.GetByID(e.ctx(), obj.CalendarID)
	require.NoError(e.t, err)
	return asJSON(e.t, dto.NewTaskResponse(obj, obj.TaskDetails(), cal.UUID))
}

func asJSON(t *testing.T, v interface{}) string {
	t.Helper()
	buf, err := json.Marshal(v)
	require.NoError(t, err)
	return string(buf)
}

// mustTask calls a task tool that must succeed and returns its "task" object.
func (e *testEnv) mustTask(userID uint, tool string, args map[string]interface{}) map[string]interface{} {
	e.t.Helper()
	payload, isErr := e.call(userID, tool, args)
	require.False(e.t, isErr, "%s %v failed: %v", tool, args, payload["error"])
	task, ok := payload["task"].(map[string]interface{})
	require.True(e.t, ok, "%s answered without a task: %v", tool, payload)
	return task
}

// refusal calls a tool that must answer a tool error and returns its text.
func (e *testEnv) refusal(userID uint, tool string, args map[string]interface{}) string {
	e.t.Helper()
	result := e.callRaw(userID, tool, args)
	require.True(e.t, result.IsError, "%s %v must be refused, got: %s", tool, args, result.Content[0].Text)
	return result.Content[0].Text
}

// renderedTaskList is list_tasks' reference: the ListAllTasksUseCase page for
// in, rendered as GET /api/v1/tasks renders it.
func (e *testEnv) renderedTaskList(in taskuc.ListAllTasksInput) string {
	e.t.Helper()
	out, err := e.server.deps.TaskListAll.Execute(e.ctx(), in)
	require.NoError(e.t, err)
	tasks := make([]dto.TaskWithCalendarResponse, len(out.Tasks))
	for i, t := range out.Tasks {
		tasks[i] = dto.TaskWithCalendarResponse{
			TaskResponse:  dto.NewTaskResponse(t.Object, t.Details, t.Calendar.UUID),
			CalendarName:  t.Calendar.Name,
			CalendarColor: t.Calendar.Color,
		}
	}
	return asJSON(e.t, dto.TaskWithCalendarListResponse{Tasks: tasks, Count: len(tasks), Total: out.Total})
}

// writeTaskTools are the tools that change an existing task, each with
// arguments that would change the task were it allowed.
var writeTaskTools = []struct {
	tool string
	args func(id string) map[string]interface{}
}{
	{"update_task", func(id string) map[string]interface{} {
		return map[string]interface{}{"task_id": id, "summary": "Hijacked", "priority": 42}
	}},
	{"complete_task", func(id string) map[string]interface{} { return map[string]interface{}{"task_id": id} }},
	{"uncomplete_task", func(id string) map[string]interface{} { return map[string]interface{}{"task_id": id} }},
	{"delete_task", func(id string) map[string]interface{} { return map[string]interface{}{"task_id": id} }},
}

// TestTaskToolsSucceed drives every task tool once on the happy path and
// compares each answer with the REST rendering of what was stored.
func TestTaskToolsSucceed(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	chores := e.newCalendarWithSet(alice.ID, "Chores", "VTODO")
	e.newCalendar(alice.ID, "Work")

	var taskID, etag string

	t.Run("create_task", func(t *testing.T) {
		task := e.mustTask(alice.ID, "create_task", map[string]interface{}{
			"calendar_id": chores.UUID, "summary": "Renew passport, ID; photos",
			"description": "Bring the old one\nand a photo", "due": "2026-03-10T17:00:00+01:00",
			"start": "2026-03-09T09:00:00+01:00", "priority": 1, "percent_complete": 20,
			"status": "in-process", "timezone": "Europe/Berlin",
		})
		taskID, etag = task["id"].(string), task["etag"].(string)
		assert.JSONEq(t, e.storedTaskJSON(taskID), asJSON(t, task), "create_task answers the stored task as REST renders it")
		// Plain text and UTC on the wire, as REST.
		assert.Equal(t, "Renew passport, ID; photos", task["summary"])
		assert.Equal(t, "2026-03-10T16:00:00Z", task["due"])
		assert.Equal(t, chores.UUID, task["calendar_uuid"])

		payload, _ := e.call(alice.ID, "create_task", map[string]interface{}{"calendar_id": chores.UUID, "summary": "Second"})
		assert.Equal(t, true, payload["created"])
	})

	t.Run("update_task", func(t *testing.T) {
		payload, isErr := e.call(alice.ID, "update_task", map[string]interface{}{"task_id": taskID, "summary": "Renew passport"})
		require.False(t, isErr, "%v", payload["error"])
		assert.Equal(t, true, payload["updated"])
		task := payload["task"].(map[string]interface{})
		assert.JSONEq(t, e.storedTaskJSON(taskID), asJSON(t, task))
		assert.Equal(t, "Renew passport", task["summary"])
		assert.NotEqual(t, etag, task["etag"], "a change is a new revision")
		etag = task["etag"].(string)
	})

	t.Run("complete_task is idempotent", func(t *testing.T) {
		task := e.mustTask(alice.ID, "complete_task", map[string]interface{}{"task_id": taskID})
		assert.JSONEq(t, e.storedTaskJSON(taskID), asJSON(t, task))
		assert.Equal(t, "completed", task["status"])
		assert.Equal(t, "2026-03-04T09:00:00Z", task["completed_at"], "the injected clock, in UTC")
		assert.EqualValues(t, 100, task["percent_complete"])
		assert.NotEqual(t, etag, task["etag"])
		etag = task["etag"].(string)

		before := e.dbSnapshot()
		again := e.mustTask(alice.ID, "complete_task", map[string]interface{}{"task_id": taskID})
		assert.Equal(t, etag, again["etag"], "completing a completed task writes nothing")
		assert.Equal(t, before, e.dbSnapshot())
	})

	t.Run("uncomplete_task is idempotent", func(t *testing.T) {
		task := e.mustTask(alice.ID, "uncomplete_task", map[string]interface{}{"task_id": taskID})
		assert.JSONEq(t, e.storedTaskJSON(taskID), asJSON(t, task))
		assert.Equal(t, "needs-action", task["status"])
		assert.Nil(t, task["completed_at"])
		assert.EqualValues(t, 0, task["percent_complete"], "100 goes back to 0, not to the 20 before")
		etag = task["etag"].(string)

		before := e.dbSnapshot()
		again := e.mustTask(alice.ID, "uncomplete_task", map[string]interface{}{"task_id": taskID})
		assert.Equal(t, etag, again["etag"])
		assert.Equal(t, before, e.dbSnapshot())
	})

	t.Run("list_tasks", func(t *testing.T) {
		for _, tc := range []struct {
			args map[string]interface{}
			in   taskuc.ListAllTasksInput
		}{
			{map[string]interface{}{}, taskuc.ListAllTasksInput{Limit: 50}},
			{map[string]interface{}{"calendar_id": chores.UUID, "status": "all", "query": "PASSPORT"},
				taskuc.ListAllTasksInput{CalendarUUID: chores.UUID, Status: "all", Query: "PASSPORT", Limit: 50}},
			{map[string]interface{}{"limit": 1, "offset": 1}, taskuc.ListAllTasksInput{Limit: 1, Offset: 1}},
		} {
			payload, isErr := e.call(alice.ID, "list_tasks", tc.args)
			require.False(t, isErr, "%v", payload["error"])
			tc.in.UserID = alice.ID
			assert.JSONEq(t, e.renderedTaskList(tc.in), asJSON(t, payload), "list_tasks %v", tc.args)
		}

		payload, _ := e.call(alice.ID, "list_tasks", map[string]interface{}{"query": "passport"})
		tasks := payload["tasks"].([]interface{})
		require.Len(t, tasks, 1)
		listed := tasks[0].(map[string]interface{})
		assert.Equal(t, "Chores", listed["calendar_name"])
		assert.Equal(t, "#16a34a", listed["calendar_color"])
	})

	t.Run("get_task_summary", func(t *testing.T) {
		const asOf = "2026-03-10T20:00:00+01:00"
		payload, isErr := e.call(alice.ID, "get_task_summary", map[string]interface{}{"as_of": asOf})
		require.False(t, isErr, "%v", payload["error"])

		at, err := time.Parse(time.RFC3339, asOf)
		require.NoError(t, err)
		out, err := e.server.deps.TaskSummary.Execute(e.ctx(), taskuc.TaskSummaryInput{UserID: alice.ID, AsOf: at})
		require.NoError(t, err)
		assert.JSONEq(t, asJSON(t, dto.NewTaskSummaryResponse(out.Total, out.ByCalendar)), asJSON(t, payload),
			"the REST body for the same as_of")
		assert.EqualValues(t, 2, payload["open"])
		assert.EqualValues(t, 1, payload["overdue"], "due 17:00+01:00, asked at 20:00+01:00")
	})

	t.Run("delete_task is permanent", func(t *testing.T) {
		payload, isErr := e.call(alice.ID, "delete_task", map[string]interface{}{"task_id": taskID})
		require.False(t, isErr, "%v", payload["error"])
		assert.Equal(t, map[string]interface{}{"deleted": true, "task_id": taskID}, payload)

		var rows int64
		require.NoError(t, e.db.Unscoped().Model(&calendar.CalendarObject{}).Where("uuid = ?", taskID).Count(&rows).Error)
		assert.Zero(t, rows, "a task delete is a hard delete: no tombstone row")

		assert.Equal(t, "No task with id "+taskID+" is readable by you.",
			e.refusal(alice.ID, "delete_task", map[string]interface{}{"task_id": taskID}))
	})
}

// TestTaskToolsUnknownTask: every task the caller cannot reach — an unknown
// id, an event's UUID, another user's task, a task in a calendar shared with
// someone else — gets the same sentence from every write tool, before any
// other check, and nothing is written.
func TestTaskToolsUnknownTask(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	bob := e.newUser("bob@example.com")
	carol := e.newUser("carol@example.com")

	work := e.newCalendar(alice.ID, "Work")
	e.newCalendarWithSet(alice.ID, "Chores", "VTODO")
	bobs := e.newCalendarWithSet(bob.ID, "Bob's list", "VTODO")
	carols := e.newCalendarWithSet(bob.ID, "Shared with Carol", "VTODO")
	e.shareCalendar(carols, carol.ID, "read-write")
	// An event in a calendar alice may only read: not a task, so the
	// unknown-task answer — not the read-only one — must come first.
	bobsEvents := e.newCalendar(bob.ID, "Bob's events")
	e.shareCalendar(bobsEvents, alice.ID, "read")

	ids := map[string]string{
		"unknown id":                      "00000000-0000-4000-8000-000000000001",
		"an event's UUID":                 mustCreate(t, e, alice.ID, work.UUID, "Standup", "2026-03-06T09:00:00Z", "2026-03-06T10:00:00Z"),
		"an event's UUID on a read share": mustCreate(t, e, bob.ID, bobsEvents.UUID, "Bob's standup", "2026-03-06T09:00:00Z", "2026-03-06T10:00:00Z"),
		"another user's task":             e.storeTaskLines(bobs, "bob-task", "SUMMARY:Bob's"),
		"a task in an unshared calendar":  e.storeTaskLines(carols, "carol-task", "SUMMARY:Carol's", "STATUS:COMPLETED"),
	}
	before := e.dbSnapshot()

	for what, id := range ids {
		for _, w := range writeTaskTools {
			assert.Equal(t, "No task with id "+id+" is readable by you.", e.refusal(alice.ID, w.tool, w.args(id)),
				"%s on %s", w.tool, what)
		}
	}
	for _, w := range writeTaskTools {
		assert.Equal(t, "task_id is required.", e.refusal(alice.ID, w.tool, map[string]interface{}{}), w.tool)
	}
	assert.Equal(t, before, e.dbSnapshot(), "a refused call writes nothing")
}

// TestTaskToolsReadOnly: a task in a calendar shared read-only, and one in a
// subscribed calendar (read-only to its owner too), can be listed, but every
// write tool refuses it — and create_task refuses both calendars.
func TestTaskToolsReadOnly(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	bob := e.newUser("bob@example.com")

	shared := e.newCalendarWithSet(bob.ID, "Bob's list", "VTODO")
	e.shareCalendar(shared, alice.ID, "read")
	subscribed := e.newCalendarWithSet(alice.ID, "Feed", "VEVENT,VTODO")
	require.NoError(t, e.db.Model(&calendar.Calendar{}).Where("id = ?", subscribed.ID).Update("subscribed", true).Error)

	tasks := map[string]string{
		"read share":            e.storeTaskLines(shared, "shared-task", "SUMMARY:Shared", "STATUS:COMPLETED"),
		"subscribed (as owner)": e.storeTaskLines(subscribed, "feed-task", "SUMMARY:From the feed", "STATUS:COMPLETED"),
	}
	before := e.dbSnapshot()

	for where, id := range tasks {
		for _, w := range writeTaskTools {
			assert.Equal(t, "You have read-only access to the task list holding that task.",
				e.refusal(alice.ID, w.tool, w.args(id)), "%s (%s)", w.tool, where)
		}
	}
	for where, cal := range map[string]*calendar.Calendar{"read share": shared, "subscribed": subscribed} {
		assert.Equal(t, "You have read-only access to that calendar, so tasks cannot be created in it.",
			e.refusal(alice.ID, "create_task", map[string]interface{}{"calendar_id": cal.UUID, "summary": "x"}), where)
	}
	assert.Equal(t, before, e.dbSnapshot(), "a refused call writes nothing")

	// Reading them is allowed.
	payload, isErr := e.call(alice.ID, "list_tasks", map[string]interface{}{"status": "all"})
	require.False(t, isErr)
	assert.EqualValues(t, 2, payload["total"])
}

// TestCreateTaskCalendarAccess: create_task resolves its calendar like
// POST …/tasks — unknown and unreadable calendars get get_events' sentence,
// a read-write share works.
func TestCreateTaskCalendarAccess(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	bob := e.newUser("bob@example.com")
	private := e.newCalendarWithSet(bob.ID, "Bob's list", "VTODO")
	readWrite := e.newCalendarWithSet(bob.ID, "Household", "VTODO")
	e.shareCalendar(readWrite, alice.ID, "read-write")
	before := e.dbSnapshot()

	for _, id := range []string{"00000000-0000-4000-8000-000000000002", private.UUID} {
		assert.Equal(t, "No calendar with id "+id+" is readable by you.",
			e.refusal(alice.ID, "create_task", map[string]interface{}{"calendar_id": id, "summary": "x", "priority": 99}))
	}
	assert.Equal(t, "calendar_id is required.", e.refusal(alice.ID, "create_task", map[string]interface{}{"summary": "x"}))
	assert.Equal(t, before, e.dbSnapshot())

	task := e.mustTask(alice.ID, "create_task", map[string]interface{}{"calendar_id": readWrite.UUID, "summary": "Take out the bins"})
	assert.JSONEq(t, e.storedTaskJSON(task["id"].(string)), asJSON(t, task))
	assert.Equal(t, readWrite.UUID, task["calendar_uuid"])
}

// TestListTasksArguments: list_tasks parses its arguments like GET
// /api/v1/tasks parses its query — before the use case's status check and its
// calendar check — and narrows with calendar_id like calendar_uuid.
func TestListTasksArguments(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	bob := e.newUser("bob@example.com")
	chores := e.newCalendarWithSet(alice.ID, "Chores", "VTODO")
	work := e.newCalendar(alice.ID, "Work")
	private := e.newCalendarWithSet(bob.ID, "Bob's list", "VTODO")
	for i := 0; i < 55; i++ {
		e.storeTaskLines(chores, fmt.Sprintf("bulk-%02d", i), fmt.Sprintf("SUMMARY:Bulk %02d", i),
			fmt.Sprintf("DUE:202603%02dT090000Z", 1+i%28))
	}
	unknown := "00000000-0000-4000-8000-000000000003"

	for _, tc := range []struct {
		args map[string]interface{}
		want string
	}{
		{map[string]interface{}{"limit": -1}, "limit must not be negative."},
		{map[string]interface{}{"offset": -1}, "offset must not be negative."},
		{map[string]interface{}{"due_after": "2026-03-05"}, `due_after must be an RFC 3339 timestamp with a UTC offset, got "2026-03-05".`},
		{map[string]interface{}{"due_before": "2026-03-05T00:00:00"}, `due_before must be an RFC 3339 timestamp with a UTC offset, got "2026-03-05T00:00:00".`},
		// Parse errors come first, as on REST.
		{map[string]interface{}{"calendar_id": unknown, "status": "done", "limit": -5}, "limit must not be negative."},
		{map[string]interface{}{"calendar_id": unknown, "due_after": "tomorrow"}, `due_after must be an RFC 3339 timestamp with a UTC offset, got "tomorrow".`},
		// Then the use case: status before the calendar.
		{map[string]interface{}{"calendar_id": unknown, "status": "done"}, `status must be "open", "completed" or "all"`},
		{map[string]interface{}{"calendar_id": unknown}, "No calendar with id " + unknown + " is readable by you."},
		{map[string]interface{}{"calendar_id": private.UUID}, "No calendar with id " + private.UUID + " is readable by you."},
	} {
		assert.Equal(t, tc.want, e.refusal(alice.ID, "list_tasks", tc.args), "%v", tc.args)
	}

	page := func(args map[string]interface{}) map[string]interface{} {
		payload, isErr := e.call(alice.ID, "list_tasks", args)
		require.False(t, isErr, "%v", payload["error"])
		return payload
	}
	got := page(map[string]interface{}{})
	assert.EqualValues(t, 50, got["count"], "default limit 50")
	assert.EqualValues(t, 55, got["total"], "total tells that more exist")
	got = page(map[string]interface{}{"offset": 50})
	assert.EqualValues(t, 5, got["count"])
	got = page(map[string]interface{}{"limit": 3})
	assert.EqualValues(t, 3, got["count"])
	got = page(map[string]interface{}{"due_after": "2026-03-02T00:00:00Z", "due_before": "2026-03-03T00:00:00+00:00"})
	assert.EqualValues(t, 2, got["total"], "bulk 01 and 29 are due on the 2nd")
	got = page(map[string]interface{}{"calendar_id": work.UUID})
	assert.EqualValues(t, 0, got["total"], "a readable calendar without tasks is an empty page, as on REST")
	assert.Equal(t, []interface{}{}, got["tasks"])
}

// TestGetTaskSummaryNeedsAnOffset: as_of is required and must carry its UTC
// offset — the offset is what decides "today".
func TestGetTaskSummaryNeedsAnOffset(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")

	required := "as_of is required: pass the user's current time in RFC 3339 with their UTC offset, e.g. 2026-11-04T09:00:00+01:00."
	assert.Equal(t, required, e.refusal(alice.ID, "get_task_summary", map[string]interface{}{}))
	assert.Equal(t, required, e.refusal(alice.ID, "get_task_summary", map[string]interface{}{"as_of": ""}))
	for _, v := range []string{"2026-03-04", "2026-03-04T10:00:00", "now"} {
		assert.Equal(t, `as_of must be an RFC 3339 timestamp with a UTC offset, got "`+v+`".`,
			e.refusal(alice.ID, "get_task_summary", map[string]interface{}{"as_of": v}))
	}

	payload, isErr := e.call(alice.ID, "get_task_summary", map[string]interface{}{"as_of": "2026-03-04T10:00:00Z"})
	require.False(t, isErr)
	assert.Equal(t, map[string]interface{}{"open": 0.0, "overdue": 0.0, "due_today": 0.0, "completed": 0.0,
		"by_calendar": map[string]interface{}{}}, payload, "no task list: zero counts")
}

// TestTaskToolsRefuseLikeREST: what the use cases refuse reaches the model
// with REST's sentence, and nothing is written.
func TestTaskToolsRefuseLikeREST(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	chores := e.newCalendarWithSet(alice.ID, "Chores", "VTODO")
	work := e.newCalendar(alice.ID, "Work")
	const recurringRefusal = "Repeating tasks cannot be completed here yet. Complete it in your task app."

	repeating := e.storeTaskLines(chores, "weekly", "SUMMARY:Water plants", "DUE:20260306T090000Z", "RRULE:FREQ=WEEKLY")
	repeatingDone := e.storeTaskLines(chores, "weekly-done", "SUMMARY:Clean", "DUE:20260306T090000Z",
		"RRULE:FREQ=WEEKLY", "STATUS:COMPLETED", "PERCENT-COMPLETE:100")
	// A task left in a calendar that stores events only (narrowed after a
	// client wrote it).
	stray := e.storeTaskLines(work, "stray", "SUMMARY:Stray")
	open := e.storeTaskLines(chores, "open", "SUMMARY:Open")
	before := e.dbSnapshot()

	for _, tc := range []struct {
		tool string
		args map[string]interface{}
		want string
	}{
		{"complete_task", map[string]interface{}{"task_id": repeating}, recurringRefusal},
		{"complete_task", map[string]interface{}{"task_id": repeatingDone}, recurringRefusal},
		{"update_task", map[string]interface{}{"task_id": repeating, "status": "completed"}, recurringRefusal},
		{"update_task", map[string]interface{}{"task_id": repeating, "status": "cancelled"}, recurringRefusal},
		{"update_task", map[string]interface{}{"task_id": repeating, "percent_complete": 100}, recurringRefusal},
		{"update_task", map[string]interface{}{"task_id": repeatingDone, "status": "completed"}, recurringRefusal},
		// A calendar without VTODO.
		{"create_task", map[string]interface{}{"calendar_id": work.UUID, "summary": "x"}, "This calendar does not store tasks"},
		{"update_task", map[string]interface{}{"task_id": stray, "summary": "x"}, "This calendar does not store tasks"},
		{"complete_task", map[string]interface{}{"task_id": stray}, "This calendar does not store tasks"},
		// One field rule per tool that has fields.
		{"create_task", map[string]interface{}{"calendar_id": chores.UUID, "summary": "  "}, "summary is required"},
		{"create_task", map[string]interface{}{"calendar_id": chores.UUID, "summary": "x", "priority": 10}, "priority must be between 0 and 9"},
		{"update_task", map[string]interface{}{"task_id": open, "status": "done"}, `status must be "needs-action", "in-process", "completed" or "cancelled"`},
		{"update_task", map[string]interface{}{"task_id": open, "due": "tomorrow"}, "due must be an RFC 3339 date-time unless due_all_day is true"},
		{"list_tasks", map[string]interface{}{"status": "cancelled"}, `status must be "open", "completed" or "all"`},
	} {
		assert.Equal(t, tc.want, e.refusal(alice.ID, tc.tool, tc.args), "%s %v", tc.tool, tc.args)
	}
	assert.Equal(t, before, e.dbSnapshot(), "a refused call writes nothing")

	// Allowed like REST: reopening a repeating task, other edits of one, and
	// deleting a task left in an events-only calendar.
	reopened := e.mustTask(alice.ID, "uncomplete_task", map[string]interface{}{"task_id": repeatingDone})
	assert.Equal(t, "needs-action", reopened["status"])
	assert.Equal(t, true, reopened["is_recurring"])
	renamed := e.mustTask(alice.ID, "update_task", map[string]interface{}{"task_id": repeating, "summary": "Water the plants", "percent_complete": 50})
	assert.Equal(t, "Water the plants", renamed["summary"])
	assert.Contains(t, e.storedICal(repeating), "RRULE:FREQ=WEEKLY", "the repeat rule survives")
	deleted, isErr := e.call(alice.ID, "delete_task", map[string]interface{}{"task_id": stray})
	require.False(t, isErr, "%v", deleted["error"])
}

func (e *testEnv) storedICal(taskUUID string) string {
	e.t.Helper()
	obj, err := e.calRepo.GetCalendarObjectByUUID(e.ctx(), taskUUID)
	require.NoError(e.t, err)
	return obj.ICalData
}

// becomesEventRepo hands the task use cases every object as a VEVENT: what a
// CalDAV PUT leaves when it rewrites a task's resource with an event (the
// UUID is kept) after the tool's taskPermission looked at it.
type becomesEventRepo struct{ calendar.CalendarRepository }

func (r becomesEventRepo) GetCalendarObjectByUUID(ctx context.Context, uuid string) (*calendar.CalendarObject, error) {
	obj, err := r.CalendarRepository.GetCalendarObjectByUUID(ctx, uuid)
	if err == nil && obj != nil {
		obj.ComponentType = calendar.ComponentEvent
	}
	return obj, err
}

// TestTaskToolsMapUseCaseNotFound: when the use case, not the tool's own
// check, finds no task (taskuc.ErrNotFound), the write tools give the
// unknown-task sentence, not the internal error, and write nothing.
func TestTaskToolsMapUseCaseNotFound(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	chores := e.newCalendarWithSet(alice.ID, "Chores", "VTODO")
	taskID := e.storeTaskLines(chores, "flips", "SUMMARY:Flips", "STATUS:COMPLETED")

	repo := becomesEventRepo{e.calRepo}
	e.server.deps.TaskUpdate = taskuc.NewUpdateTaskUseCase(repo, nil)
	e.server.deps.TaskComplete = taskuc.NewCompleteTaskUseCase(repo, nil)
	e.server.deps.TaskUncomplete = taskuc.NewUncompleteTaskUseCase(repo, nil)
	e.server.deps.TaskDelete = taskuc.NewDeleteTaskUseCase(repo)
	logs := captureLogs(t)
	before := e.dbSnapshot()

	for _, w := range writeTaskTools {
		args := w.args(taskID)
		if w.tool == "update_task" {
			args = map[string]interface{}{"task_id": taskID, "summary": "Renamed"}
		}
		assert.Equal(t, "No task with id "+taskID+" is readable by you.", e.refusal(alice.ID, w.tool, args), w.tool)
	}
	assert.Equal(t, before, e.dbSnapshot(), "nothing may be written")
	assert.Empty(t, logs.String(), "a task that is gone is not a server failure")
}

// TestUpdateTaskAbsentAndNull: an absent argument keeps the property, null
// removes each of the five removable ones, a call with only task_id writes
// nothing, and a wrongly typed argument is refused with its expected type.
func TestUpdateTaskAbsentAndNull(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	chores := e.newCalendarWithSet(alice.ID, "Chores", "VTODO")

	full := func() map[string]interface{} {
		return e.mustTask(alice.ID, "create_task", map[string]interface{}{
			"calendar_id": chores.UUID, "summary": "Full", "description": "Notes",
			"due": "2026-03-10T17:00:00Z", "start": "2026-03-09T09:00:00Z", "priority": 5, "percent_complete": 40,
		})
	}

	for _, tc := range []struct {
		arg  string // also the response key
		prop string // the iCalendar property that must go
	}{
		{"description", "DESCRIPTION"},
		{"due", "DUE"},
		{"start", "DTSTART"},
		{"priority", "PRIORITY"},
		{"percent_complete", "PERCENT-COMPLETE"},
	} {
		t.Run(tc.arg+": null removes it", func(t *testing.T) {
			before := full()
			id := before["id"].(string)
			require.Contains(t, e.storedICal(id), "\r\n"+tc.prop)
			after := e.mustTask(alice.ID, "update_task", map[string]interface{}{"task_id": id, tc.arg: nil})
			assert.JSONEq(t, e.storedTaskJSON(id), asJSON(t, after))
			assert.NotContains(t, e.storedICal(id), "\r\n"+tc.prop, "%s must be removed", tc.prop)
			if tc.arg == "description" {
				assert.Equal(t, "", after[tc.arg])
			} else {
				assert.Nil(t, after[tc.arg])
			}

			// Everything else is as it was.
			for _, k := range []string{tc.arg, "etag", "updated_at"} {
				delete(before, k)
				delete(after, k)
			}
			assert.Equal(t, before, after, "only %s changes", tc.arg)
		})
	}

	t.Run("absent keeps, only task_id writes nothing", func(t *testing.T) {
		task := full()
		id := task["id"].(string)
		before := e.dbSnapshot()
		same := e.mustTask(alice.ID, "update_task", map[string]interface{}{"task_id": id})
		assert.Equal(t, task["etag"], same["etag"])
		assert.Equal(t, task, same)
		assert.Equal(t, before, e.dbSnapshot())

		// The stored values sent back write nothing either.
		same = e.mustTask(alice.ID, "update_task", map[string]interface{}{"task_id": id, "summary": "Full", "priority": 5})
		assert.Equal(t, task["etag"], same["etag"])
		assert.Equal(t, before, e.dbSnapshot())
	})

	t.Run("a wrongly typed argument", func(t *testing.T) {
		id := full()["id"].(string)
		before := e.dbSnapshot()
		for arg, want := range map[string]string{
			"priority":         `Argument "priority" must be an integer or null, got a string; the tool did not run.`,
			"percent_complete": `Argument "percent_complete" must be an integer or null, got a string; the tool did not run.`,
			"due":              `Argument "due" must be a string or null, got a number; the tool did not run.`,
		} {
			var value interface{} = "high"
			if arg == "due" {
				value = 20260310
			}
			assert.Equal(t, want, e.refusal(alice.ID, "update_task", map[string]interface{}{"task_id": id, arg: value}))
		}
		assert.Equal(t, before, e.dbSnapshot())
	})
}

// TestTaskToolDescriptions pins the facts a model must read before it acts
// (#170): what is permanent, what is refused, what only widens.
func TestTaskToolDescriptions(t *testing.T) {
	e := newTestEnv(t)
	desc := map[string]string{}
	for _, tool := range e.server.toolList {
		desc[tool.Name] = tool.Description
	}

	for tool, facts := range map[string][]string{
		"delete_task":      {"PERMANENTLY", "cannot be undone", "every device", "all its occurrences", "only after they agreed"},
		"complete_task":    {"Idempotent", "repeating task", "is_recurring", "refused"},
		"uncomplete_task":  {"only if it was 100", "repeating tasks too"},
		"update_task":      {"Only the fields you pass change", "null", "summary, status and due_all_day cannot be removed", "alarms", "subtask links", "repeat rule"},
		"create_task":      {"kind tasks or both", "cannot repeat", "YYYY-MM-DD", "RFC 3339"},
		"list_tasks":       {"completed AND cancelled", "default 50", "total"},
		"get_task_summary": {"offset decides", "completed (completed AND cancelled)", "overlap"},
		"delete_calendar":  {"EVERY event or task"},
		"create_calendar":  {"events", "tasks", "both", "can only widen"},
		"update_calendar":  {"events", "tasks", "both", "can only widen", "cannot be undone"},
		"create_event":     {"a task list (kind tasks) refuses events"},
		"get_events":       {"a task list (kind tasks) has no events, use list_tasks"},
		"list_calendars":   {"list_tasks"},
	} {
		for _, fact := range facts {
			assert.Contains(t, desc[tool], fact, "the %s description must say %q", tool, fact)
		}
	}
	for _, fact := range []string{"list_tasks", "get_task_summary", "cannot be completed over MCP",
		"Deleting a task is permanent", "every\nevent or task"} {
		assert.Contains(t, instructions, fact)
	}
}

// TestTaskToolTextsNameTheRealLimits: the tool descriptions are prose in
// JSON-schema literals, so the numbers they state are checked against the
// constants that enforce them.
func TestTaskToolTextsNameTheRealLimits(t *testing.T) {
	limits := fmt.Sprintf("default %d, max %d", defaultTaskLimit, taskuc.MaxLimit)
	assert.Contains(t, listTasksTool.Description, "(default 50, max 1000)", "the description's wording")
	assert.Contains(t, listTasksTool.Description, "("+limits+")")
	assert.Contains(t, string(listTasksTool.InputSchema), limits)

	title := fmt.Sprintf("at most %d characters", taskuc.MaxSummaryRunes)
	assert.Contains(t, string(createTaskTool.InputSchema), title)
	assert.Contains(t, string(updateTaskTool.InputSchema), title)
}

// TestUpdateTaskEmptyString pins the answer to "" for each update_task field
// (the review's open question): "" is never stored as a value. Text and time
// fields read "" as null and remove the property, as REST's PATCH does; a
// summary cannot be removed and is refused; the numbers read "" as null too.
func TestUpdateTaskEmptyString(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	chores := e.newCalendarWithSet(alice.ID, "Chores", "VTODO")
	full := func() map[string]interface{} {
		return e.mustTask(alice.ID, "create_task", map[string]interface{}{
			"calendar_id": chores.UUID, "summary": "Full", "description": "Notes",
			"due": "2026-03-10T17:00:00Z", "start": "2026-03-09T09:00:00Z", "priority": 5, "percent_complete": 40,
		})
	}

	t.Run("summary is refused and nothing is written", func(t *testing.T) {
		id := full()["id"].(string)
		before := e.dbSnapshot()
		assert.Equal(t, "summary is required", e.refusal(alice.ID, "update_task", map[string]interface{}{"task_id": id, "summary": ""}))
		assert.Equal(t, before, e.dbSnapshot())
	})

	for _, tc := range []struct {
		arg  string
		prop string
	}{
		{"description", "DESCRIPTION"},
		{"due", "DUE"},
		{"start", "DTSTART"},
		{"priority", "PRIORITY"},
		{"percent_complete", "PERCENT-COMPLETE"},
	} {
		t.Run(tc.arg+": \"\" removes it, like null", func(t *testing.T) {
			id := full()["id"].(string)
			require.Contains(t, e.storedICal(id), "\r\n"+tc.prop)
			after := e.mustTask(alice.ID, "update_task", map[string]interface{}{"task_id": id, tc.arg: ""})
			assert.NotContains(t, e.storedICal(id), "\r\n"+tc.prop, "%s must be removed", tc.prop)
			assert.JSONEq(t, e.storedTaskJSON(id), asJSON(t, after))
		})
	}
}
