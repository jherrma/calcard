package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/adapter/http/dto"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	taskuc "github.com/jherrma/caldav-server/internal/usecase/task"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests for DELETE …/tasks/:task_id, POST …/complete and POST …/uncomplete
// (story 170-10).

const (
	preconditionFailedBody = `{"status":"error","message":"ETag precondition failed"}`
	recurringBody          = `{"status":"error","message":"Repeating tasks cannot be completed here yet. Complete it in your task app."}`
	noTasksBody            = `{"status":"error","message":"This calendar does not store tasks"}`
	// handlerStamp is taskHandlerClock in UTC, as the iCalendar data and the
	// wire carry it.
	handlerStamp = "20261103T093015Z"
)

// send sends method /api/v1/calendars/<path> with optional headers and body
// and returns status and body.
func (f *taskFixture) send(t *testing.T, method, token, path, body string, headers map[string]string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(method, "/api/v1/calendars/"+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := f.app.Test(req)
	require.NoError(t, err)
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	return resp.StatusCode, buf.String()
}

func taskPath(cal *calendar.Calendar, obj *calendar.CalendarObject) string {
	return cal.UUID + "/tasks/" + obj.UUID
}

// reload returns the object row as stored now (soft-deleted rows included).
func (f *taskFixture) reload(t *testing.T, obj *calendar.CalendarObject) calendar.CalendarObject {
	t.Helper()
	var got calendar.CalendarObject
	require.NoError(t, f.db.DB().Unscoped().Where("uuid = ?", obj.UUID).First(&got).Error)
	return got
}

// syncState returns the calendar's sync token and ctag and its number of
// change-log rows.
func (f *taskFixture) syncState(t *testing.T, cal *calendar.Calendar) (token, ctag string, changes int64) {
	t.Helper()
	got, err := f.repo.GetByID(context.Background(), cal.ID)
	require.NoError(t, err)
	require.NoError(t, f.db.DB().Model(&calendar.SyncChangeLog{}).Where("calendar_id = ?", cal.ID).Count(&changes).Error)
	return got.SyncToken, got.CTag, changes
}

// lastChange returns the calendar's newest change-log row.
func (f *taskFixture) lastChange(t *testing.T, cal *calendar.Calendar) calendar.SyncChangeLog {
	t.Helper()
	var row calendar.SyncChangeLog
	require.NoError(t, f.db.DB().Where("calendar_id = ?", cal.ID).Order("id DESC").First(&row).Error)
	return row
}

func decodeTask(t *testing.T, body string) dto.TaskResponse {
	t.Helper()
	var got dto.TaskResponse
	require.NoError(t, json.Unmarshal([]byte(body), &got), body)
	return got
}

// clientTask stores a VTODO as a phone's task app writes it: an alarm, a
// parent link and X- properties the server must keep.
func (f *taskFixture) clientTask(t *testing.T, cal *calendar.Calendar, uid string, props ...string) *calendar.CalendarObject {
	t.Helper()
	lines := []string{"BEGIN:VTODO", "UID:" + uid, "DTSTAMP:20260102T080000Z", "SUMMARY:Renew passport",
		"LAST-MODIFIED:20260102T080000Z", "SEQUENCE:3", "PERCENT-COMPLETE:40", "STATUS:IN-PROCESS",
		"RELATED-TO;RELTYPE=PARENT:parent-uid", "X-FOO;X-PARAM=1:bar", "X-APPLE-SORT-ORDER:12",
		"DUE;TZID=Europe/Berlin:20301105T170000"}
	lines = append(lines, props...)
	lines = append(lines, "BEGIN:VALARM", "ACTION:DISPLAY", "DESCRIPTION:Reminder", "TRIGGER:-PT15M", "END:VALARM", "END:VTODO")
	return f.store(t, cal, uid, lines...)
}

// preservedLines are the clientTask lines complete and uncomplete must leave
// byte for byte.
var preservedLines = []string{
	"RELATED-TO;RELTYPE=PARENT:parent-uid", "X-FOO;X-PARAM=1:bar", "X-APPLE-SORT-ORDER:12",
	"DUE;TZID=Europe/Berlin:20301105T170000", "SUMMARY:Renew passport",
	"BEGIN:VALARM\r\nACTION:DISPLAY\r\nDESCRIPTION:Reminder\r\nTRIGGER:-PT15M\r\nEND:VALARM",
}

// TestTaskHandlerComplete: POST …/complete answers 200 with the completed
// TaskResponse, edits the stored VTODO in place (the three properties,
// SEQUENCE, LAST-MODIFIED; everything else kept), gives it a new ETag and
// advances the calendar's sync token with a "modified" change. A second
// complete is a no-op: same body, same ETag, nothing written. Any body is
// ignored.
func TestTaskHandlerComplete(t *testing.T) {
	f := setupTaskHandlerTest(t)
	task := f.clientTask(t, f.taskList, "passport")
	token, _, changes := f.syncState(t, f.taskList)

	code, body := f.send(t, http.MethodPost, f.ownerToken, taskPath(f.taskList, task)+"/complete", "", nil)
	require.Equal(t, fiber.StatusOK, code, body)
	got := decodeTask(t, body)
	assert.Equal(t, task.UUID, got.ID)
	assert.Equal(t, "completed", got.Status)
	assert.True(t, got.Completed)
	require.NotNil(t, got.CompletedAt)
	assert.Equal(t, taskHandlerClock.UTC(), *got.CompletedAt)
	assert.Contains(t, body, `"completed_at":"2026-11-03T09:30:15Z"`, "UTC on the wire")
	assert.Equal(t, intPtr(100), got.PercentComplete)
	assert.NotEqual(t, task.ETag, got.ETag, "a new ETag")

	stored := f.reload(t, task)
	assert.Equal(t, got.ETag, stored.ETag)
	assert.Equal(t, calendar.TaskStatusCompleted, stored.Status)
	for _, line := range []string{"STATUS:COMPLETED", "COMPLETED:" + handlerStamp, "PERCENT-COMPLETE:100",
		"SEQUENCE:4", "LAST-MODIFIED:" + handlerStamp, "DTSTAMP:" + handlerStamp} {
		assert.Contains(t, stored.ICalData, "\r\n"+line+"\r\n")
	}
	for _, line := range preservedLines {
		assert.Contains(t, stored.ICalData, "\r\n"+line+"\r\n", "kept as the client stored it")
	}
	assert.NotContains(t, stored.ICalData, "STATUS:IN-PROCESS")

	newToken, ctag, newChanges := f.syncState(t, f.taskList)
	assert.NotEqual(t, token, newToken, "sync token advances")
	assert.Equal(t, newToken, ctag)
	assert.Equal(t, changes+1, newChanges)
	change := f.lastChange(t, f.taskList)
	assert.Equal(t, "modified", change.ChangeType)
	assert.Equal(t, newToken, change.SyncToken)
	assert.Equal(t, "passport.ics", change.ResourcePath)
	assert.Equal(t, "passport", change.ResourceUID)

	code, getBody := f.get(t, f.ownerToken, taskPath(f.taskList, task))
	require.Equal(t, fiber.StatusOK, code)
	assert.JSONEq(t, body, getBody, "GET answers what complete answered")

	t.Run("again is a no-op", func(t *testing.T) {
		for _, reqBody := range []string{"", `{"anything":1}`, "not json"} {
			code, again := f.send(t, http.MethodPost, f.ownerToken, taskPath(f.taskList, task)+"/complete", reqBody,
				map[string]string{"Content-Type": "application/json"})
			require.Equal(t, fiber.StatusOK, code, again)
			assert.JSONEq(t, body, again, "same task, same ETag")
		}
		after := f.reload(t, task)
		assert.Equal(t, stored, after, "nothing written")
		sameToken, _, sameChanges := f.syncState(t, f.taskList)
		assert.Equal(t, newToken, sameToken)
		assert.Equal(t, newChanges, sameChanges)
	})

	t.Run("by a read-write sharee, with a body", func(t *testing.T) {
		shared := f.todo(t, f.sharedRW, "shared", "Shared chore")
		code, body := f.send(t, http.MethodPost, f.shareeToken, taskPath(f.sharedRW, shared)+"/complete",
			`{"status":"needs-action"}`, map[string]string{"Content-Type": "application/json"})
		require.Equal(t, fiber.StatusOK, code, body)
		assert.True(t, decodeTask(t, body).Completed)
		assert.Equal(t, calendar.TaskStatusCompleted, f.reload(t, shared).Status)
	})
}

// TestTaskHandlerUncomplete: POST …/uncomplete reopens a completed task in
// place (COMPLETED removed, NEEDS-ACTION, percent 100 -> 0) with the same
// bookkeeping; on a task that is not completed it answers 200 and writes
// nothing.
func TestTaskHandlerUncomplete(t *testing.T) {
	f := setupTaskHandlerTest(t)
	task := f.clientTask(t, f.taskList, "passport")
	code, completedBody := f.send(t, http.MethodPost, f.ownerToken, taskPath(f.taskList, task)+"/complete", "", nil)
	require.Equal(t, fiber.StatusOK, code, completedBody)
	completedETag := decodeTask(t, completedBody).ETag
	token, _, changes := f.syncState(t, f.taskList)

	code, body := f.send(t, http.MethodPost, f.ownerToken, taskPath(f.taskList, task)+"/uncomplete", "", nil)
	require.Equal(t, fiber.StatusOK, code, body)
	got := decodeTask(t, body)
	assert.Equal(t, "needs-action", got.Status)
	assert.False(t, got.Completed)
	assert.Nil(t, got.CompletedAt)
	assert.Equal(t, intPtr(0), got.PercentComplete, "100 is reset; the 40 from before is not restored")
	assert.NotEqual(t, completedETag, got.ETag)

	stored := f.reload(t, task)
	assert.Equal(t, got.ETag, stored.ETag)
	assert.NotContains(t, stored.ICalData, "\r\nCOMPLETED:")
	for _, line := range []string{"STATUS:NEEDS-ACTION", "PERCENT-COMPLETE:0", "SEQUENCE:5", "LAST-MODIFIED:" + handlerStamp} {
		assert.Contains(t, stored.ICalData, "\r\n"+line+"\r\n")
	}
	for _, line := range preservedLines {
		assert.Contains(t, stored.ICalData, "\r\n"+line+"\r\n")
	}
	newToken, _, newChanges := f.syncState(t, f.taskList)
	assert.NotEqual(t, token, newToken)
	assert.Equal(t, changes+1, newChanges)
	assert.Equal(t, "modified", f.lastChange(t, f.taskList).ChangeType)

	t.Run("again is a no-op", func(t *testing.T) {
		code, again := f.send(t, http.MethodPost, f.ownerToken, taskPath(f.taskList, task)+"/uncomplete", "", nil)
		require.Equal(t, fiber.StatusOK, code, again)
		assert.JSONEq(t, body, again)
		assert.Equal(t, stored, f.reload(t, task))
		_, _, sameChanges := f.syncState(t, f.taskList)
		assert.Equal(t, newChanges, sameChanges)
	})

	t.Run("an in-process task stays as it is", func(t *testing.T) {
		open := f.todo(t, f.taskList, "open", "Open task", "STATUS:IN-PROCESS", "PERCENT-COMPLETE:100")
		before := f.reload(t, open)
		code, body := f.send(t, http.MethodPost, f.ownerToken, taskPath(f.taskList, open)+"/uncomplete", "", nil)
		require.Equal(t, fiber.StatusOK, code, body)
		assert.Equal(t, "in-process", decodeTask(t, body).Status)
		assert.Equal(t, before, f.reload(t, open))
	})
}

// TestTaskHandlerCompleteRecurring (spec §1c): complete on a recurring task is
// 409 with the exact sentence and writes nothing, also when the task is
// already completed; uncomplete of a recurring task is allowed.
func TestTaskHandlerCompleteRecurring(t *testing.T) {
	f := setupTaskHandlerTest(t)
	open := f.todo(t, f.taskList, "series", "Take out the bins", "DUE:20301108T060000Z", "RRULE:FREQ=WEEKLY")
	done := f.todo(t, f.taskList, "done-series", "Weekly review", "DTSTART:20301108T060000Z",
		"RDATE:20301115T060000Z", "STATUS:COMPLETED", "COMPLETED:20301101T100000Z")
	_, _, changes := f.syncState(t, f.taskList)

	for _, obj := range []*calendar.CalendarObject{open, done} {
		before := f.reload(t, obj)
		code, body := f.send(t, http.MethodPost, f.ownerToken, taskPath(f.taskList, obj)+"/complete", "", nil)
		assert.Equal(t, fiber.StatusConflict, code, obj.UID)
		assert.JSONEq(t, recurringBody, body, obj.UID)
		assert.Equal(t, before, f.reload(t, obj), "nothing written")
	}
	_, _, after := f.syncState(t, f.taskList)
	assert.Equal(t, changes, after)

	code, body := f.send(t, http.MethodPost, f.ownerToken, taskPath(f.taskList, done)+"/uncomplete", "", nil)
	require.Equal(t, fiber.StatusOK, code, body)
	got := decodeTask(t, body)
	assert.Equal(t, "needs-action", got.Status)
	assert.True(t, got.IsRecurring)
	assert.Contains(t, f.reload(t, done).ICalData, "\r\nRDATE:20301115T060000Z\r\n")
}

// TestTaskHandlerDelete: DELETE answers 204 with no body, the task is gone
// from GET and the list, and the calendar's change log has a "deleted" row
// with the new sync token; a second DELETE is 404.
func TestTaskHandlerDelete(t *testing.T) {
	f := setupTaskHandlerTest(t)
	task := f.todo(t, f.taskList, "gone", "Delete me")
	keep := f.todo(t, f.taskList, "keep", "Keep me")
	token, _, changes := f.syncState(t, f.taskList)

	code, body := f.send(t, http.MethodDelete, f.ownerToken, taskPath(f.taskList, task), "", nil)
	require.Equal(t, fiber.StatusNoContent, code, body)
	assert.Empty(t, body)

	code, body = f.get(t, f.ownerToken, taskPath(f.taskList, task))
	assert.Equal(t, fiber.StatusNotFound, code)
	assert.JSONEq(t, taskNotFoundBody, body)
	assert.Equal(t, []string{"Keep me"}, taskSummaries(f.list(t, f.ownerToken, f.taskList, "?status=all")))
	_, err := f.repo.GetCalendarObjectByUUID(context.Background(), task.UUID)
	assert.Error(t, err, "no longer found by the repository")
	_, err = f.repo.GetCalendarObjectByUUID(context.Background(), keep.UUID)
	assert.NoError(t, err)
	// A hard delete (user decision after story 23): no soft-deleted row is
	// left behind, not even one only an Unscoped read would see.
	var rows int64
	require.NoError(t, f.db.DB().Unscoped().Model(&calendar.CalendarObject{}).Where("uuid = ?", task.UUID).Count(&rows).Error)
	assert.Zero(t, rows, "the row is gone for good")

	newToken, ctag, newChanges := f.syncState(t, f.taskList)
	assert.NotEqual(t, token, newToken)
	assert.Equal(t, newToken, ctag)
	assert.Equal(t, changes+1, newChanges)
	change := f.lastChange(t, f.taskList)
	assert.Equal(t, "deleted", change.ChangeType)
	assert.Equal(t, newToken, change.SyncToken)
	assert.Equal(t, "gone.ics", change.ResourcePath)
	assert.Equal(t, "gone", change.ResourceUID)

	code, body = f.send(t, http.MethodDelete, f.ownerToken, taskPath(f.taskList, task), "", nil)
	assert.Equal(t, fiber.StatusNotFound, code)
	assert.JSONEq(t, taskNotFoundBody, body)

	t.Run("by a read-write sharee, a recurring task", func(t *testing.T) {
		shared := f.todo(t, f.sharedRW, "shared-series", "Bins", "DUE:20301108T060000Z", "RRULE:FREQ=WEEKLY")
		code, body := f.send(t, http.MethodDelete, f.shareeToken, taskPath(f.sharedRW, shared), "", nil)
		require.Equal(t, fiber.StatusNoContent, code, body)
		code, _ = f.get(t, f.shareeToken, taskPath(f.sharedRW, shared))
		assert.Equal(t, fiber.StatusNotFound, code)
	})
}

// TestTaskHandlerWriteRefusals: the three routes share their refusals, all
// answered before anything is written. Every miss is the 404 of an unknown
// task (also for an event's UUID, also for a read sharee); a read share and a
// subscribed calendar are 403; a stale If-Match is 412, checked after the
// permission; a matching If-Match (or "*") lets the write through.
func TestTaskHandlerWriteRefusals(t *testing.T) {
	// Each route gets tasks in the state it would change (open for complete,
	// completed for uncomplete), so a refusal that slipped through would show
	// as a write.
	routes := []struct {
		name, method, suffix, status string
		ok                           int
	}{
		{"delete", http.MethodDelete, "", "STATUS:COMPLETED", fiber.StatusNoContent},
		{"complete", http.MethodPost, "/complete", "STATUS:NEEDS-ACTION", fiber.StatusOK},
		{"uncomplete", http.MethodPost, "/uncomplete", "STATUS:COMPLETED", fiber.StatusOK},
	}
	for _, route := range routes {
		t.Run(route.name, func(t *testing.T) {
			f := setupTaskHandlerTest(t)
			const unknownCalendar = "00000000-0000-4000-8000-00000000dead"
			var none map[string]string // no extra headers
			task := f.todo(t, f.taskList, "t", "Owner's task", route.status)
			readTask := f.todo(t, f.sharedRead, "r", "Read-shared task", route.status)
			subbedTask := f.todo(t, f.subbed, "s", "Subscribed task", route.status)
			shareeTask := f.todo(t, f.shareeOwnTaskList, "o", "Sharee's own task", route.status)
			eventInEvents := f.event(t, f.eventCal, "e1")
			eventInMixed := f.event(t, f.sharedRW, "e2")
			eventInRead := f.store(t, f.sharedRead, "e3", "BEGIN:VEVENT", "UID:e3", "DTSTAMP:20300101T000000Z",
				"DTSTART:20301105T090000Z", "DTEND:20301105T100000Z", "SUMMARY:Standup", "END:VEVENT")
			staleIfMatch := map[string]string{"If-Match": `"stale"`}

			objects, changes := f.counts(t)
			snapshot := func() map[string]calendar.CalendarObject {
				out := map[string]calendar.CalendarObject{}
				for _, o := range []*calendar.CalendarObject{task, readTask, subbedTask, shareeTask, eventInEvents, eventInMixed, eventInRead} {
					out[o.UUID] = f.reload(t, o)
				}
				return out
			}
			before := snapshot()

			for _, tc := range []struct {
				name, token, path string
				headers           map[string]string
				code              int
				body              string
			}{
				{"outsider", f.outsiderToken, taskPath(f.taskList, task), none, 404, taskNotFoundBody},
				{"outsider with a stale If-Match", f.outsiderToken, taskPath(f.taskList, task), staleIfMatch, 404, taskNotFoundBody},
				{"sharee on an unshared list", f.shareeToken, taskPath(f.taskList, task), none, 404, taskNotFoundBody},
				{"unknown calendar", f.ownerToken, unknownCalendar + "/tasks/" + task.UUID, none, 404, taskNotFoundBody},
				{"unknown task", f.ownerToken, f.taskList.UUID + "/tasks/00000000-0000-4000-8000-000000000001", none, 404, taskNotFoundBody},
				{"task under another list", f.ownerToken, taskPath(f.otherList, task), none, 404, taskNotFoundBody},
				{"another user's task under the caller's list", f.ownerToken, f.taskList.UUID + "/tasks/" + shareeTask.UUID, none, 404, taskNotFoundBody},
				{"event UUID in its own calendar", f.ownerToken, taskPath(f.eventCal, eventInEvents), none, 404, taskNotFoundBody},
				{"event UUID in a mixed calendar", f.shareeToken, taskPath(f.sharedRW, eventInMixed), none, 404, taskNotFoundBody},
				{"event UUID in a read share", f.shareeToken, taskPath(f.sharedRead, eventInRead), none, 404, taskNotFoundBody},
				{"event UUID with its own ETag as If-Match", f.ownerToken, taskPath(f.eventCal, eventInEvents),
					map[string]string{"If-Match": `"` + eventInEvents.ETag + `"`}, 404, taskNotFoundBody},
				{"read sharee", f.shareeToken, taskPath(f.sharedRead, readTask), none, 403, readOnlyBody},
				{"read sharee with a stale If-Match", f.shareeToken, taskPath(f.sharedRead, readTask), staleIfMatch, 403, readOnlyBody},
				{"subscribed calendar owner", f.ownerToken, taskPath(f.subbed, subbedTask), none, 403, readOnlyBody},
				{"stale If-Match", f.ownerToken, taskPath(f.taskList, task), staleIfMatch, 412, preconditionFailedBody},
				{"another task's ETag as If-Match", f.ownerToken, taskPath(f.taskList, task),
					map[string]string{"If-Match": `"` + shareeTask.ETag + `"`}, 412, preconditionFailedBody},
			} {
				code, body := f.send(t, route.method, tc.token, tc.path+route.suffix, "", tc.headers)
				assert.Equal(t, tc.code, code, tc.name)
				assert.JSONEq(t, tc.body, body, tc.name)
			}
			assert.Equal(t, before, snapshot(), "no refusal wrote anything")
			o, c := f.counts(t)
			assert.Equal(t, objects, o)
			assert.Equal(t, changes, c)

			// The task's own ETag, and "*", pass the precondition and the
			// write happens.
			for _, tc := range []struct {
				obj     *calendar.CalendarObject
				ifMatch string
			}{
				{task, `"` + task.ETag + `"`},
				{f.todo(t, f.taskList, "t2", "Another task", route.status), "*"},
			} {
				_, changesBefore := f.counts(t)
				code, body := f.send(t, route.method, f.ownerToken, taskPath(f.taskList, tc.obj)+route.suffix, "",
					map[string]string{"If-Match": tc.ifMatch})
				assert.Equal(t, route.ok, code, body)
				_, changesAfter := f.counts(t)
				assert.Equal(t, changesBefore+1, changesAfter, "If-Match %s: written", tc.ifMatch)
			}
		})
	}
}

// TestTaskHandlerWritesOnAnEventsCalendar: a VTODO left in a calendar that
// stores events only (possible only from old data) can be read and deleted,
// but not completed or reopened: 400 "This calendar does not store tasks",
// as create answers there.
func TestTaskHandlerWritesOnAnEventsCalendar(t *testing.T) {
	f := setupTaskHandlerTest(t)
	stray := &calendar.CalendarObject{UUID: "stray-uuid", CalendarID: f.eventCal.ID, Path: "stray.ics", UID: "stray",
		ETag: "e", ICalData: "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//t//EN\r\nBEGIN:VTODO\r\nUID:stray\r\n" +
			"DTSTAMP:20300101T000000Z\r\nSUMMARY:stray\r\nSTATUS:COMPLETED\r\nEND:VTODO\r\nEND:VCALENDAR\r\n"}
	require.NoError(t, stray.PopulateDenormFieldsFromICal())
	require.NoError(t, f.db.DB().Create(stray).Error)
	before := f.reload(t, stray)

	for _, suffix := range []string{"/complete", "/uncomplete"} {
		code, body := f.send(t, http.MethodPost, f.ownerToken, taskPath(f.eventCal, stray)+suffix, "", nil)
		assert.Equal(t, fiber.StatusBadRequest, code, suffix)
		assert.JSONEq(t, noTasksBody, body, suffix)
	}
	assert.Equal(t, before, f.reload(t, stray))

	code, body := f.send(t, http.MethodDelete, f.ownerToken, taskPath(f.eventCal, stray), "", nil)
	assert.Equal(t, fiber.StatusNoContent, code, body)
}

// failingWriteRepo fails the object writes of the task use cases while
// everything else works.
type failingWriteRepo struct {
	calendar.CalendarRepository
	err error
}

func (r failingWriteRepo) UpdateCalendarObjectIfETag(context.Context, *calendar.CalendarObject, string) error {
	return r.err
}

func (r failingWriteRepo) HardDeleteCalendarObject(context.Context, *calendar.CalendarObject) error {
	return r.err
}

// TestTaskHandlerWriteServerErrors (F28): a failed lookup or write in the use
// case is a 500 with a generic body and the cause in the log only; an object
// that turned into an event after the handler's check is the 404 of an
// unknown task.
func TestTaskHandlerWriteServerErrors(t *testing.T) {
	f := setupTaskHandlerTest(t)
	task := f.todo(t, f.taskList, "t", "Renew passport", "STATUS:IN-PROCESS")

	var logBuf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, nil)))
	defer slog.SetDefault(prev)

	serve := func(useCaseRepo calendar.CalendarRepository) *fiber.App {
		handler := NewTaskHandler(taskuc.NewListTasksUseCase(useCaseRepo), taskuc.NewGetTaskUseCase(useCaseRepo),
			taskuc.NewCreateTaskUseCase(useCaseRepo, nil), taskuc.NewUpdateTaskUseCase(useCaseRepo, nil), taskuc.NewDeleteTaskUseCase(useCaseRepo),
			taskuc.NewCompleteTaskUseCase(useCaseRepo, nil), taskuc.NewUncompleteTaskUseCase(useCaseRepo, nil), taskuc.NewListAllTasksUseCase(useCaseRepo),
			taskuc.NewTaskSummaryUseCase(useCaseRepo), f.repo)
		app := fiber.New()
		g := app.Group("/api/v1").Group("/calendars", Authenticate(f.jwt, repository.NewUserRepository(f.db.DB()))).Group("/:calendar_id/tasks")
		g.Delete("/:task_id", handler.Delete)
		g.Post("/:task_id/complete", handler.Complete)
		g.Post("/:task_id/uncomplete", handler.Uncomplete)
		return app
	}
	call := func(app *fiber.App, method, suffix string) (int, string) {
		req, _ := http.NewRequest(method, "/api/v1/calendars/"+taskPath(f.taskList, task)+suffix, nil)
		req.Header.Set("Authorization", "Bearer "+f.ownerToken)
		resp, err := app.Test(req)
		require.NoError(t, err)
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(resp.Body)
		return resp.StatusCode, buf.String()
	}
	routes := []struct{ method, suffix string }{
		{http.MethodDelete, ""}, {http.MethodPost, "/complete"}, {http.MethodPost, "/uncomplete"},
	}

	t.Run("lookup fails in the use case", func(t *testing.T) {
		app := serve(failingObjectRepo{CalendarRepository: f.repo, err: errors.New("disk I/O error (injected)")})
		for _, r := range routes {
			logBuf.Reset()
			code, body := call(app, r.method, r.suffix)
			assert.Equal(t, fiber.StatusInternalServerError, code, r.suffix)
			assert.JSONEq(t, internalErrorBody, body, r.suffix)
			assert.Contains(t, logBuf.String(), "disk I/O error (injected)", r.suffix)
			assert.Contains(t, logBuf.String(), "task request failed", r.suffix)
		}
	})

	t.Run("the write fails", func(t *testing.T) {
		app := serve(failingWriteRepo{CalendarRepository: f.repo, err: errors.New("database is locked (injected)")})
		// Uncomplete of an in-process task writes nothing, so complete first
		// for real, then make uncomplete fail.
		for _, r := range []struct{ method, suffix string }{{http.MethodDelete, ""}, {http.MethodPost, "/complete"}} {
			logBuf.Reset()
			code, body := call(app, r.method, r.suffix)
			assert.Equal(t, fiber.StatusInternalServerError, code, r.suffix)
			assert.JSONEq(t, internalErrorBody, body, r.suffix)
			assert.NotContains(t, body, "locked")
			assert.Contains(t, logBuf.String(), "database is locked (injected)", r.suffix)
		}
		code, body := f.send(t, http.MethodPost, f.ownerToken, taskPath(f.taskList, task)+"/complete", "", nil)
		require.Equal(t, fiber.StatusOK, code, body)
		logBuf.Reset()
		code, body = call(app, http.MethodPost, "/uncomplete")
		assert.Equal(t, fiber.StatusInternalServerError, code)
		assert.JSONEq(t, internalErrorBody, body)
		assert.Contains(t, logBuf.String(), "database is locked (injected)")
		assert.Equal(t, calendar.TaskStatusCompleted, f.reload(t, task).Status)
	})

	t.Run("the task became an event meanwhile", func(t *testing.T) {
		app := serve(failingObjectRepo{CalendarRepository: f.repo, asEvent: true})
		for _, r := range routes {
			code, body := call(app, r.method, r.suffix)
			assert.Equal(t, fiber.StatusNotFound, code, r.suffix)
			assert.JSONEq(t, taskNotFoundBody, body, r.suffix)
		}
		_, err := f.repo.GetCalendarObjectByUUID(context.Background(), task.UUID)
		assert.NoError(t, err, "not deleted")
	})
}
