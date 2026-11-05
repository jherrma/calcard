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
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	taskuc "github.com/jherrma/caldav-server/internal/usecase/task"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests for PATCH …/tasks/:task_id (story 170-11).

// patch sends PATCH /api/v1/calendars/<path> with a JSON body and optional
// extra headers.
func (f *taskFixture) patch(t *testing.T, token, path, body string, headers map[string]string) (int, string) {
	t.Helper()
	h := map[string]string{"Content-Type": "application/json"}
	for k, v := range headers {
		h[k] = v
	}
	return f.send(t, http.MethodPatch, token, path, body, h)
}

// TestTaskHandlerUpdate: PATCH answers 200 with the edited TaskResponse,
// edits the stored VTODO in place (the fields sent, SEQUENCE, LAST-MODIFIED
// and DTSTAMP from the clock in UTC; a phone's alarm, parent link and X-
// properties kept byte for byte), gives it a new ETag and advances the
// calendar's sync token with a "modified" change; GET then answers the same.
func TestTaskHandlerUpdate(t *testing.T) {
	f := setupTaskHandlerTest(t)
	task := f.clientTask(t, f.taskList, "passport")
	token, _, changes := f.syncState(t, f.taskList)

	code, body := f.patch(t, f.ownerToken, taskPath(f.taskList, task),
		`{"summary":"Renew ID card","description":"Two photos","priority":2,"due":"2030-11-06T16:00:00Z","timezone":"Europe/Berlin","recurrence":"FREQ=DAILY"}`, nil)
	require.Equal(t, fiber.StatusOK, code, body)
	got := decodeTask(t, body)
	assert.Equal(t, task.UUID, got.ID)
	assert.Equal(t, "Renew ID card", got.Summary)
	assert.Equal(t, "Two photos", got.Description)
	assert.Equal(t, intPtr(2), got.Priority)
	assert.Equal(t, intPtr(40), got.PercentComplete, "absent: unchanged")
	assert.Equal(t, "in-process", got.Status, "absent: unchanged")
	assert.Contains(t, body, `"due":"2030-11-06T16:00:00Z"`, "UTC on the wire")
	assert.False(t, got.IsRecurring, "an unknown recurrence field is ignored")
	assert.NotEqual(t, task.ETag, got.ETag)

	stored := f.reload(t, task)
	assert.Equal(t, got.ETag, stored.ETag)
	for _, line := range []string{"SUMMARY:Renew ID card", "DESCRIPTION:Two photos", "PRIORITY:2",
		"DUE;TZID=Europe/Berlin:20301106T170000", "SEQUENCE:4", "LAST-MODIFIED:" + handlerStamp, "DTSTAMP:" + handlerStamp,
		"PERCENT-COMPLETE:40", "STATUS:IN-PROCESS"} {
		assert.Contains(t, stored.ICalData, "\r\n"+line+"\r\n")
	}
	for _, line := range preservedLines {
		if strings.HasPrefix(line, "SUMMARY:") || strings.HasPrefix(line, "DUE;") {
			continue
		}
		assert.Contains(t, stored.ICalData, "\r\n"+line+"\r\n", "kept as the client stored it")
	}
	assert.NotContains(t, stored.ICalData, "FREQ=DAILY", "no recurrence written")
	assert.Equal(t, 1, strings.Count(stored.ICalData, "BEGIN:VTIMEZONE\r\nTZID:Europe/Berlin\r\n"),
		"the client's TZID due had no VTIMEZONE; the edited due brought one")

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
	assert.JSONEq(t, body, getBody, "GET answers what PATCH answered")

	t.Run("the same values again write nothing", func(t *testing.T) {
		for _, req := range []string{`{}`, `{"summary":"Renew ID card","priority":2,"due":"2030-11-06T17:00:00+01:00"}`, `{"summary":null}`} {
			code, again := f.patch(t, f.ownerToken, taskPath(f.taskList, task), req, nil)
			require.Equal(t, fiber.StatusOK, code, again)
			assert.JSONEq(t, body, again, "same task, same ETag: %s", req)
		}
		assert.Equal(t, stored, f.reload(t, task), "nothing written")
		sameToken, _, sameChanges := f.syncState(t, f.taskList)
		assert.Equal(t, newToken, sameToken)
		assert.Equal(t, newChanges, sameChanges)
	})

	t.Run("by a read-write sharee", func(t *testing.T) {
		shared := f.todo(t, f.sharedRW, "shared", "Shared chore")
		code, body := f.patch(t, f.shareeToken, taskPath(f.sharedRW, shared), `{"status":"in-process"}`, nil)
		require.Equal(t, fiber.StatusOK, code, body)
		assert.Equal(t, "in-process", decodeTask(t, body).Status)
	})
}

// TestTaskHandlerUpdateAbsentVsNull: an explicit null removes description,
// due, start, priority and percent_complete; the same key absent leaves the
// property as stored (dto.OptionalField). A value of the wrong JSON type is a
// bad body.
func TestTaskHandlerUpdateAbsentVsNull(t *testing.T) {
	f := setupTaskHandlerTest(t)
	props := []string{"DESCRIPTION:Notes", "DTSTART:20301104T080000Z", "DUE:20301105T080000Z", "PRIORITY:3", "PERCENT-COMPLETE:20"}
	present := func(t *testing.T, resp map[string]any, key string) bool {
		t.Helper()
		v, ok := resp[key]
		require.True(t, ok, "%s is always in the body", key)
		return v != nil && v != ""
	}

	for _, tc := range []struct{ key, prop string }{
		{"description", "DESCRIPTION"}, {"due", "DUE"}, {"start", "DTSTART"},
		{"priority", "PRIORITY"}, {"percent_complete", "PERCENT-COMPLETE"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			task := f.todo(t, f.taskList, "t-"+tc.key, "Task", props...)

			code, body := f.patch(t, f.ownerToken, taskPath(f.taskList, task), `{"summary":"Renamed"}`, nil)
			require.Equal(t, fiber.StatusOK, code, body)
			resp := decodeJSON(t, body)
			assert.True(t, present(t, resp, tc.key), "absent: %s kept", tc.key)
			assert.Contains(t, f.reload(t, task).ICalData, "\r\n"+tc.prop+":")

			code, body = f.patch(t, f.ownerToken, taskPath(f.taskList, task), `{"`+tc.key+`":null}`, nil)
			require.Equal(t, fiber.StatusOK, code, body)
			resp = decodeJSON(t, body)
			assert.False(t, present(t, resp, tc.key), "null: %s removed", tc.key)
			assert.Equal(t, "Renamed", resp["summary"])
			data := f.reload(t, task).ICalData
			assert.NotContains(t, data, "\r\n"+tc.prop+":")
			for _, other := range props {
				if !strings.HasPrefix(other, tc.prop+":") {
					assert.Contains(t, data, "\r\n"+other+"\r\n", "the other fields are untouched")
				}
			}
		})
	}

	t.Run("wrong JSON types", func(t *testing.T) {
		task := f.todo(t, f.taskList, "typed", "Task", props...)
		before := f.reload(t, task)
		for _, req := range []string{`{"priority":"high"}`, `{"due":5}`, `{"percent_complete":[1]}`, `{"description":true}`,
			`{"summary":1}`, `{"due_all_day":"yes"}`, `not json`, ``} {
			code, body := f.patch(t, f.ownerToken, taskPath(f.taskList, task), req, nil)
			assert.Equal(t, fiber.StatusBadRequest, code, req)
			assert.JSONEq(t, errorBody("Invalid request body"), body, req)
		}
		assert.Equal(t, before, f.reload(t, task))
	})
}

func decodeJSON(t *testing.T, body string) map[string]any {
	t.Helper()
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &out), body)
	return out
}

// TestTaskHandlerUpdateStatus: status completed on a task that does not
// repeat is complete (completed_at from the clock, percent 100); another
// status reopens it like uncomplete.
func TestTaskHandlerUpdateStatus(t *testing.T) {
	f := setupTaskHandlerTest(t)
	task := f.clientTask(t, f.taskList, "passport")

	code, body := f.patch(t, f.ownerToken, taskPath(f.taskList, task), `{"status":"completed","percent_complete":10}`, nil)
	require.Equal(t, fiber.StatusOK, code, body)
	got := decodeTask(t, body)
	assert.True(t, got.Completed)
	assert.Contains(t, body, `"completed_at":"2026-11-03T09:30:15Z"`)
	assert.Equal(t, intPtr(100), got.PercentComplete)
	assert.Contains(t, f.reload(t, task).ICalData, "\r\nCOMPLETED:"+handlerStamp+"\r\n")

	code, body = f.patch(t, f.ownerToken, taskPath(f.taskList, task), `{"status":"needs-action"}`, nil)
	require.Equal(t, fiber.StatusOK, code, body)
	got = decodeTask(t, body)
	assert.Equal(t, "needs-action", got.Status)
	assert.Nil(t, got.CompletedAt)
	assert.Equal(t, intPtr(0), got.PercentComplete)
	assert.NotContains(t, f.reload(t, task).ICalData, "\r\nCOMPLETED:")
}

// TestTaskHandlerUpdateRecurring (spec §1c, M8): on a repeating task status
// completed or cancelled and percent_complete 100 are the 409 of complete and
// write nothing; a title edit goes through and keeps the rule.
func TestTaskHandlerUpdateRecurring(t *testing.T) {
	f := setupTaskHandlerTest(t)
	series := f.todo(t, f.taskList, "series", "Take out the bins", "DUE:20301108T060000Z", "RRULE:FREQ=WEEKLY",
		"BEGIN:VALARM", "ACTION:DISPLAY", "DESCRIPTION:Bins", "TRIGGER:-PT1H", "END:VALARM")
	before := f.reload(t, series)
	_, _, changes := f.syncState(t, f.taskList)

	for _, req := range []string{`{"status":"completed"}`, `{"status":"cancelled"}`, `{"percent_complete":100}`,
		`{"summary":"Bins","status":"completed"}`} {
		code, body := f.patch(t, f.ownerToken, taskPath(f.taskList, series), req, nil)
		assert.Equal(t, fiber.StatusConflict, code, req)
		assert.JSONEq(t, recurringBody, body, req)
	}
	assert.Equal(t, before, f.reload(t, series), "nothing written")
	_, _, after := f.syncState(t, f.taskList)
	assert.Equal(t, changes, after)

	code, body := f.patch(t, f.ownerToken, taskPath(f.taskList, series),
		`{"summary":"Take out the recycling","priority":1,"due":"2030-11-09T06:00:00Z","status":"in-process","percent_complete":50}`, nil)
	require.Equal(t, fiber.StatusOK, code, body)
	got := decodeTask(t, body)
	assert.True(t, got.IsRecurring)
	assert.Equal(t, "in-process", got.Status)
	data := f.reload(t, series).ICalData
	for _, line := range []string{"RRULE:FREQ=WEEKLY", "SUMMARY:Take out the recycling", "DUE:20301109T060000Z",
		"BEGIN:VALARM\r\nACTION:DISPLAY\r\nDESCRIPTION:Bins\r\nTRIGGER:-PT1H\r\nEND:VALARM"} {
		assert.Contains(t, data, "\r\n"+line+"\r\n")
	}
}

// TestTaskHandlerUpdateRefusals: PATCH shares the refusals of the other task
// writes, all answered before the body is read: every miss is the 404 of an
// unknown task (an event's UUID too), a read share and a subscribed calendar
// 403, a stale If-Match 412. Then a body that does not bind and every invalid
// field are 400 with their message; a task left in an events-only calendar is
// 400 "This calendar does not store tasks". Nothing is written by any of them;
// the task's own ETag and "*" as If-Match let the edit through.
func TestTaskHandlerUpdateRefusals(t *testing.T) {
	f := setupTaskHandlerTest(t)
	const unknownCalendar = "00000000-0000-4000-8000-00000000dead"
	var none map[string]string
	task := f.todo(t, f.taskList, "t", "Owner's task", "DUE:20301105T080000Z")
	readTask := f.todo(t, f.sharedRead, "r", "Read-shared task")
	subbedTask := f.todo(t, f.subbed, "s", "Subscribed task")
	shareeTask := f.todo(t, f.shareeOwnTaskList, "o", "Sharee's own task")
	eventInEvents := f.event(t, f.eventCal, "e1")
	eventInMixed := f.event(t, f.sharedRW, "e2")
	stray := &calendar.CalendarObject{UUID: "stray-uuid", CalendarID: f.eventCal.ID, Path: "stray.ics", UID: "stray",
		ETag: "e", ICalData: "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//t//EN\r\nBEGIN:VTODO\r\nUID:stray\r\n" +
			"DTSTAMP:20300101T000000Z\r\nSUMMARY:stray\r\nEND:VTODO\r\nEND:VCALENDAR\r\n"}
	require.NoError(t, stray.PopulateDenormFieldsFromICal())
	require.NoError(t, f.db.DB().Create(stray).Error)
	stale := map[string]string{"If-Match": `"stale"`}

	all := []*calendar.CalendarObject{task, readTask, subbedTask, shareeTask, eventInEvents, eventInMixed, stray}
	snapshot := func() map[string]calendar.CalendarObject {
		out := map[string]calendar.CalendarObject{}
		for _, o := range all {
			out[o.UUID] = f.reload(t, o)
		}
		return out
	}
	before := snapshot()
	objects, changes := f.counts(t)

	const valid = `{"summary":"Changed"}`
	const broken = `{"summary":`
	for _, tc := range []struct {
		name, token, path, body string
		headers                 map[string]string
		code                    int
		want                    string
	}{
		{"outsider", f.outsiderToken, taskPath(f.taskList, task), valid, none, 404, taskNotFoundBody},
		{"outsider, broken body", f.outsiderToken, taskPath(f.taskList, task), broken, none, 404, taskNotFoundBody},
		{"outsider, stale If-Match", f.outsiderToken, taskPath(f.taskList, task), valid, stale, 404, taskNotFoundBody},
		{"sharee on an unshared list", f.shareeToken, taskPath(f.taskList, task), valid, none, 404, taskNotFoundBody},
		{"unknown calendar", f.ownerToken, unknownCalendar + "/tasks/" + task.UUID, valid, none, 404, taskNotFoundBody},
		{"unknown task", f.ownerToken, f.taskList.UUID + "/tasks/00000000-0000-4000-8000-000000000001", valid, none, 404, taskNotFoundBody},
		{"task under another list", f.ownerToken, taskPath(f.otherList, task), valid, none, 404, taskNotFoundBody},
		{"another user's task under the caller's list", f.ownerToken, f.taskList.UUID + "/tasks/" + shareeTask.UUID, valid, none, 404, taskNotFoundBody},
		{"event UUID in its own calendar", f.ownerToken, taskPath(f.eventCal, eventInEvents), valid, none, 404, taskNotFoundBody},
		{"event UUID in a mixed calendar", f.shareeToken, taskPath(f.sharedRW, eventInMixed), valid, none, 404, taskNotFoundBody},
		{"event UUID with its own ETag as If-Match", f.ownerToken, taskPath(f.eventCal, eventInEvents), valid,
			map[string]string{"If-Match": `"` + eventInEvents.ETag + `"`}, 404, taskNotFoundBody},
		{"read sharee", f.shareeToken, taskPath(f.sharedRead, readTask), valid, none, 403, readOnlyBody},
		{"read sharee, broken body", f.shareeToken, taskPath(f.sharedRead, readTask), broken, none, 403, readOnlyBody},
		{"read sharee, stale If-Match", f.shareeToken, taskPath(f.sharedRead, readTask), valid, stale, 403, readOnlyBody},
		{"subscribed calendar owner", f.ownerToken, taskPath(f.subbed, subbedTask), valid, none, 403, readOnlyBody},
		{"stale If-Match", f.ownerToken, taskPath(f.taskList, task), valid, stale, 412, preconditionFailedBody},
		{"stale If-Match, broken body", f.ownerToken, taskPath(f.taskList, task), broken, stale, 412, preconditionFailedBody},
		{"another task's ETag as If-Match", f.ownerToken, taskPath(f.taskList, task), valid,
			map[string]string{"If-Match": `"` + shareeTask.ETag + `"`}, 412, preconditionFailedBody},
		{"broken body", f.ownerToken, taskPath(f.taskList, task), broken, none, 400, errorBody("Invalid request body")},
		{"blank summary", f.ownerToken, taskPath(f.taskList, task), `{"summary":"  "}`, none, 400, errorBody("summary is required")},
		{"unknown status", f.ownerToken, taskPath(f.taskList, task), `{"status":"done"}`, none, 400,
			errorBody(`status must be "needs-action", "in-process", "completed" or "cancelled"`)},
		{"priority 10", f.ownerToken, taskPath(f.taskList, task), `{"priority":10}`, none, 400, errorBody("priority must be between 0 and 9")},
		{"percent 101", f.ownerToken, taskPath(f.taskList, task), `{"percent_complete":101}`, none, 400,
			errorBody("percent_complete must be between 0 and 100")},
		{"due before start", f.ownerToken, taskPath(f.taskList, task), `{"start":"2030-11-06T08:00:00Z"}`, none, 400,
			errorBody("due must not be before start")},
		{"date on a timed task", f.ownerToken, taskPath(f.taskList, task), `{"due":"2030-11-06"}`, none, 400,
			errorBody("due must be an RFC 3339 date-time unless due_all_day is true")},
		{"due_all_day against the stored due", f.ownerToken, taskPath(f.taskList, task), `{"due_all_day":true}`, none, 400,
			errorBody("due_all_day is true, but the task's due is a date-time: send due as a date (YYYY-MM-DD) or null")},
		{"task in an events-only calendar", f.ownerToken, taskPath(f.eventCal, stray), valid, none, 400, noTasksBody},
		{"task in an events-only calendar, invalid field", f.ownerToken, taskPath(f.eventCal, stray), `{"priority":42}`, none, 400, noTasksBody},
	} {
		code, body := f.patch(t, tc.token, tc.path, tc.body, tc.headers)
		assert.Equal(t, tc.code, code, tc.name)
		assert.JSONEq(t, tc.want, body, tc.name)
	}
	assert.Equal(t, before, snapshot(), "no refusal wrote anything")
	o, c := f.counts(t)
	assert.Equal(t, objects, o)
	assert.Equal(t, changes, c)

	for _, tc := range []struct {
		obj     *calendar.CalendarObject
		ifMatch string
	}{
		{task, `"` + task.ETag + `"`},
		{f.todo(t, f.taskList, "t2", "Another task"), "*"},
	} {
		_, changesBefore := f.counts(t)
		code, body := f.patch(t, f.ownerToken, taskPath(f.taskList, tc.obj), valid, map[string]string{"If-Match": tc.ifMatch})
		assert.Equal(t, fiber.StatusOK, code, body)
		_, changesAfter := f.counts(t)
		assert.Equal(t, changesBefore+1, changesAfter, "If-Match %s: written", tc.ifMatch)
	}
}

// TestTaskHandlerUpdateServerErrors (F28): a failed lookup or write in the use
// case is a 500 with a generic body and the cause in the log only; an object
// that turned into an event after the handler's check is the 404 of an
// unknown task.
func TestTaskHandlerUpdateServerErrors(t *testing.T) {
	f := setupTaskHandlerTest(t)
	task := f.todo(t, f.taskList, "t", "Renew passport")

	var logBuf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, nil)))
	defer slog.SetDefault(prev)

	call := func(useCaseRepo calendar.CalendarRepository) (int, string) {
		handler := NewTaskHandler(taskuc.NewListTasksUseCase(useCaseRepo), taskuc.NewGetTaskUseCase(useCaseRepo),
			taskuc.NewCreateTaskUseCase(useCaseRepo, nil), taskuc.NewUpdateTaskUseCase(useCaseRepo, nil),
			taskuc.NewDeleteTaskUseCase(useCaseRepo), taskuc.NewCompleteTaskUseCase(useCaseRepo, nil),
			taskuc.NewUncompleteTaskUseCase(useCaseRepo, nil), taskuc.NewListAllTasksUseCase(useCaseRepo),
			taskuc.NewTaskSummaryUseCase(useCaseRepo), f.repo)
		app := fiber.New()
		app.Group("/api/v1").Group("/calendars", Authenticate(f.jwt, repository.NewUserRepository(f.db.DB()))).
			Group("/:calendar_id/tasks").Patch("/:task_id", handler.Update)
		req, _ := http.NewRequest(http.MethodPatch, "/api/v1/calendars/"+taskPath(f.taskList, task), strings.NewReader(`{"summary":"x"}`))
		req.Header.Set("Authorization", "Bearer "+f.ownerToken)
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(resp.Body)
		return resp.StatusCode, buf.String()
	}

	t.Run("lookup fails in the use case", func(t *testing.T) {
		logBuf.Reset()
		code, body := call(failingObjectRepo{CalendarRepository: f.repo, err: errors.New("disk I/O error (injected)")})
		assert.Equal(t, fiber.StatusInternalServerError, code)
		assert.JSONEq(t, internalErrorBody, body)
		assert.Contains(t, logBuf.String(), "disk I/O error (injected)")
		assert.Contains(t, logBuf.String(), "task request failed")
		assert.Contains(t, logBuf.String(), "method=PATCH")
	})

	t.Run("the write fails", func(t *testing.T) {
		logBuf.Reset()
		code, body := call(failingWriteRepo{CalendarRepository: f.repo, err: errors.New("database is locked (injected)")})
		assert.Equal(t, fiber.StatusInternalServerError, code)
		assert.JSONEq(t, internalErrorBody, body)
		assert.NotContains(t, body, "locked")
		assert.Contains(t, logBuf.String(), "database is locked (injected)")
		got, err := f.repo.GetCalendarObjectByUUID(context.Background(), task.UUID)
		require.NoError(t, err)
		assert.Equal(t, "Renew passport", got.Summary)
	})

	t.Run("the task became an event meanwhile", func(t *testing.T) {
		code, body := call(failingObjectRepo{CalendarRepository: f.repo, asEvent: true})
		assert.Equal(t, fiber.StatusNotFound, code)
		assert.JSONEq(t, taskNotFoundBody, body)
	})
}

// TestTaskHandlerReminders_Write (#169 story 12): `reminders` on create and
// PATCH (absent / null / [] / array) and the 400s with their texts.
func TestTaskHandlerReminders_Write(t *testing.T) {
	f := setupTaskHandlerTest(t)

	got, _ := f.create(t, f.ownerToken, f.taskList, `{"summary":"Pay","due":"2030-11-05T17:00:00Z","reminders":[{"offset_seconds":-1800}]}`)
	require.Len(t, got.Reminders, 1)
	assert.Equal(t, int64(-1800), *got.Reminders[0].OffsetSeconds)
	assert.Equal(t, "end", *got.Reminders[0].Related)
	assert.True(t, got.Reminders[0].Editable)
	stored := f.reload(t, &calendar.CalendarObject{UUID: got.ID})
	assert.Contains(t, stored.ICalData, "TRIGGER;RELATED=END:-PT30M")

	path := f.taskList.UUID + "/tasks/" + got.ID
	patch := func(body string) (int, string) {
		return f.patch(t, f.ownerToken, path, body, nil)
	}

	// absent: untouched
	code, body := patch(`{"summary":"Pay rent"}`)
	require.Equal(t, fiber.StatusOK, code, body)
	assert.Len(t, decodeTask(t, body).Reminders, 1)
	// the same list: nothing written
	etag := decodeTask(t, body).ETag
	code, body = patch(`{"reminders":[{"offset_seconds":-1800}]}`)
	require.Equal(t, fiber.StatusOK, code, body)
	assert.Equal(t, etag, decodeTask(t, body).ETag)
	// array
	soon := time.Now().UTC().Add(48 * time.Hour).Format(time.RFC3339)
	code, body = patch(`{"reminders":[{"offset_seconds":-3600},{"at":"` + soon + `"}]}`)
	require.Equal(t, fiber.StatusOK, code, body)
	assert.Len(t, decodeTask(t, body).Reminders, 2)
	assert.NotEqual(t, etag, decodeTask(t, body).ETag)
	// null removes
	code, body = patch(`{"reminders":null}`)
	require.Equal(t, fiber.StatusOK, code, body)
	assert.Empty(t, decodeTask(t, body).Reminders)

	for name, tc := range map[string]struct{ body, msg string }{
		"shape":  {`{"reminders":[{}]}`, "a reminder needs exactly one of offset_seconds and at"},
		"time":   {`{"reminders":[{"at":"tomorrow"}]}`, "Invalid reminder time"},
		"offset": {`{"reminders":[{"offset_seconds":99999999999}]}`, "reminder offset out of range"},
	} {
		code, body := patch(tc.body)
		assert.Equal(t, fiber.StatusBadRequest, code, name)
		assert.JSONEq(t, errorBody(tc.msg), body, name)
	}

	code, body = f.post(t, f.ownerToken, f.taskList.UUID, `{"summary":"No due","reminders":[{"offset_seconds":-60}]}`)
	assert.Equal(t, fiber.StatusBadRequest, code)
	assert.JSONEq(t, errorBody("A reminder needs a due date"), body)
	code, _ = f.post(t, f.ownerToken, f.taskList.UUID, `{"summary":"At","reminders":[{"at":"2099-01-01T00:00:00Z"}]}`)
	assert.Equal(t, fiber.StatusBadRequest, code, "400 years away")
}
