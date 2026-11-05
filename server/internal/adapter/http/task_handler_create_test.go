package http

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/jherrma/caldav-server/internal/adapter/http/dto"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const readOnlyBody = `{"status":"error","message":"You have read-only access to this calendar"}`

// post sends POST /api/v1/calendars/<calendarUUID>/tasks with a JSON body
// and returns status and body.
func (f *taskFixture) post(t *testing.T, token, calendarUUID, body string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/calendars/"+calendarUUID+"/tasks", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := f.app.Test(req)
	require.NoError(t, err)
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	return resp.StatusCode, buf.String()
}

// create posts a task that must be created and decodes the 201 body.
func (f *taskFixture) create(t *testing.T, token string, cal *calendar.Calendar, body string) (dto.TaskResponse, string) {
	t.Helper()
	code, raw := f.post(t, token, cal.UUID, body)
	require.Equal(t, fiber.StatusCreated, code, raw)
	var got dto.TaskResponse
	require.NoError(t, json.Unmarshal([]byte(raw), &got))
	return got, raw
}

// counts returns the number of stored calendar objects (soft-deleted ones
// included) and change-log rows.
func (f *taskFixture) counts(t *testing.T) (objects, changes int64) {
	t.Helper()
	require.NoError(t, f.db.DB().Unscoped().Model(&calendar.CalendarObject{}).Count(&objects).Error)
	require.NoError(t, f.db.DB().Model(&calendar.SyncChangeLog{}).Count(&changes).Error)
	return objects, changes
}

func errorBody(message string) string {
	b, _ := json.Marshal(map[string]string{"status": "error", "message": message})
	return string(b)
}

// TestTaskHandlerCreate: POST answers 201 with the TaskResponse of a stored
// VTODO: path <uuid>.ics, the response's ETag, the denormalized columns, a
// "created" change-log row and a new sync token/ctag on the calendar; GET and
// the list then return the same task.
func TestTaskHandlerCreate(t *testing.T) {
	f := setupTaskHandlerTest(t)
	ctx := context.Background()

	t.Run("full task by the owner", func(t *testing.T) {
		before, err := f.repo.GetByID(ctx, f.taskList.ID)
		require.NoError(t, err)
		start := time.Now()

		got, raw := f.create(t, f.ownerToken, f.taskList, `{"summary":"Renew passport","description":"Bring two photos",
			"due":"2026-11-05T17:00:00+01:00","due_all_day":false,"start":"2026-11-04T09:00:00+01:00","priority":3,
			"percent_complete":40,"status":"in-process","timezone":"Europe/Berlin"}`)

		_, err = uuid.Parse(got.ID)
		require.NoError(t, err)
		assert.Equal(t, f.taskList.ID, got.CalendarID)
		assert.Equal(t, f.taskList.UUID, got.CalendarUUID)
		assert.Equal(t, got.ID+"@calcard.io", got.UID)
		assert.Equal(t, "Renew passport", got.Summary)
		assert.Equal(t, "Bring two photos", got.Description)
		assert.Equal(t, "in-process", got.Status)
		assert.False(t, got.Completed)
		assert.Nil(t, got.CompletedAt)
		assert.Equal(t, intPtr(40), got.PercentComplete)
		assert.Equal(t, intPtr(3), got.Priority)
		require.NotNil(t, got.Due)
		assert.Equal(t, time.Date(2026, 11, 5, 16, 0, 0, 0, time.UTC), *got.Due)
		assert.Nil(t, got.DueDate)
		assert.False(t, got.DueAllDay)
		require.NotNil(t, got.Start)
		assert.Equal(t, time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC), *got.Start)
		assert.False(t, got.IsRecurring)
		assert.Nil(t, got.RRule)
		assert.NotEmpty(t, got.ETag)
		assert.WithinDuration(t, start, got.CreatedAt, time.Minute)
		assert.Contains(t, raw, `"due":"2026-11-05T16:00:00Z"`, "due is UTC on the wire")

		// The stored object.
		obj, err := f.repo.GetCalendarObjectByUUID(ctx, got.ID)
		require.NoError(t, err)
		assert.Equal(t, got.ID+".ics", obj.Path)
		assert.Equal(t, got.ETag, obj.ETag)
		assert.Equal(t, got.UID, obj.UID)
		assert.Equal(t, calendar.ComponentToDo, obj.ComponentType)
		assert.Equal(t, calendar.TaskStatusInProcess, obj.Status)
		require.NotNil(t, obj.DueAt)
		assert.True(t, obj.DueAt.Equal(time.Date(2026, 11, 5, 16, 0, 0, 0, time.UTC)))
		assert.Equal(t, intPtr(3), obj.Priority)
		assert.Equal(t, intPtr(40), obj.PercentComplete)
		for _, line := range []string{"PRODID:-//CalCard//EN", "VERSION:2.0", "UID:" + got.UID, "SEQUENCE:0",
			"DUE;TZID=Europe/Berlin:20261105T170000", "DTSTART;TZID=Europe/Berlin:20261104T090000",
			"BEGIN:VTIMEZONE", "TZID:Europe/Berlin", "STATUS:IN-PROCESS"} {
			assert.Contains(t, obj.ICalData, line+"\r\n")
		}
		for _, name := range []string{"DTSTAMP", "CREATED", "LAST-MODIFIED"} {
			assert.Regexp(t, "\r\n"+name+`:\d{8}T\d{6}Z`+"\r\n", obj.ICalData, "%s is written in UTC", name)
		}

		// The change log and the calendar's sync token / ctag.
		after, err := f.repo.GetByID(ctx, f.taskList.ID)
		require.NoError(t, err)
		assert.NotEqual(t, before.SyncToken, after.SyncToken)
		assert.Equal(t, after.SyncToken, after.CTag)
		var change calendar.SyncChangeLog
		require.NoError(t, f.db.DB().Where("calendar_id = ? AND resource_path = ?", f.taskList.ID, obj.Path).First(&change).Error)
		assert.Equal(t, "created", change.ChangeType)
		assert.Equal(t, got.UID, change.ResourceUID)
		assert.Equal(t, after.SyncToken, change.SyncToken)

		// GET and the list return the same task.
		code, body := f.get(t, f.ownerToken, f.taskList.UUID+"/tasks/"+got.ID)
		require.Equal(t, fiber.StatusOK, code, body)
		assert.JSONEq(t, raw, body, "GET answers exactly the created task")
		list := f.list(t, f.ownerToken, f.taskList, "")
		require.Len(t, list.Tasks, 1)
		assert.Equal(t, got.ID, list.Tasks[0].ID)
	})

	t.Run("read-write sharee in a mixed calendar", func(t *testing.T) {
		got, _ := f.create(t, f.shareeToken, f.sharedRW, `{"summary":"Bring cake"}`)
		assert.Equal(t, f.sharedRW.UUID, got.CalendarUUID)
		assert.Equal(t, f.sharedRW.ID, got.CalendarID)
		obj, err := f.repo.GetCalendarObjectByUUID(ctx, got.ID)
		require.NoError(t, err)
		assert.Equal(t, f.sharedRW.ID, obj.CalendarID)
	})

	t.Run("summary only", func(t *testing.T) {
		got, raw := f.create(t, f.ownerToken, f.taskList, `{"summary":"Call the dentist"}`)
		assert.Equal(t, "needs-action", got.Status)
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(raw), &m))
		for _, key := range []string{"completed_at", "percent_complete", "priority", "due", "due_date", "start", "start_date", "rrule"} {
			v, present := m[key]
			assert.True(t, present, key)
			assert.Nil(t, v, key)
		}
	})

	t.Run("explicit nulls are none", func(t *testing.T) {
		got, _ := f.create(t, f.ownerToken, f.taskList, `{"summary":"Nulls","description":null,"due":null,
			"start":null,"priority":null,"percent_complete":null,"status":null,"timezone":null}`)
		assert.Equal(t, "needs-action", got.Status)
		assert.Nil(t, got.Due)
		assert.Nil(t, got.Priority)
		assert.Nil(t, got.PercentComplete)
	})

	t.Run("all-day", func(t *testing.T) {
		got, raw := f.create(t, f.ownerToken, f.taskList, `{"summary":"Pay rent","due":"2026-12-01","start":"2026-11-28","due_all_day":true}`)
		assert.True(t, got.DueAllDay)
		assert.Nil(t, got.Due)
		require.NotNil(t, got.DueDate)
		assert.Equal(t, "2026-12-01", *got.DueDate)
		assert.Nil(t, got.Start)
		require.NotNil(t, got.StartDate)
		assert.Equal(t, "2026-11-28", *got.StartDate)
		assert.Contains(t, raw, `"due":null`)
		obj, err := f.repo.GetCalendarObjectByUUID(ctx, got.ID)
		require.NoError(t, err)
		assert.Contains(t, obj.ICalData, "DUE;VALUE=DATE:20261201\r\n")
		assert.Contains(t, obj.ICalData, "DTSTART;VALUE=DATE:20261128\r\n")
	})

	t.Run("timed without a timezone is UTC", func(t *testing.T) {
		got, _ := f.create(t, f.ownerToken, f.taskList, `{"summary":"Offset","due":"2026-11-05T17:00:00+05:00"}`)
		obj, err := f.repo.GetCalendarObjectByUUID(ctx, got.ID)
		require.NoError(t, err)
		assert.Contains(t, obj.ICalData, "DUE:20261105T120000Z\r\n")
		assert.NotContains(t, obj.ICalData, "VTIMEZONE")
	})

	t.Run("completed", func(t *testing.T) {
		start := time.Now().UTC().Truncate(time.Second)
		got, _ := f.create(t, f.ownerToken, f.taskList, `{"summary":"Done already","status":"completed","percent_complete":10}`)
		assert.Equal(t, "completed", got.Status)
		assert.True(t, got.Completed)
		require.NotNil(t, got.CompletedAt)
		assert.False(t, got.CompletedAt.Before(start))
		assert.Equal(t, time.UTC, got.CompletedAt.Location())
		assert.Equal(t, intPtr(100), got.PercentComplete)
		obj, err := f.repo.GetCalendarObjectByUUID(ctx, got.ID)
		require.NoError(t, err)
		assert.Contains(t, obj.ICalData, "STATUS:COMPLETED\r\n")
		assert.Contains(t, obj.ICalData, "PERCENT-COMPLETE:100\r\n")
		assert.Regexp(t, "\r\nCOMPLETED:\\d{8}T\\d{6}Z\r\n", obj.ICalData)
	})

	t.Run("a recurrence cannot be created", func(t *testing.T) {
		got, _ := f.create(t, f.ownerToken, f.taskList,
			`{"summary":"Water plants","recurrence":{"frequency":"DAILY"},"rrule":"FREQ=DAILY"}`)
		assert.False(t, got.IsRecurring)
		assert.Nil(t, got.RRule)
		obj, err := f.repo.GetCalendarObjectByUUID(ctx, got.ID)
		require.NoError(t, err)
		assert.NotContains(t, obj.ICalData, "RRULE")
	})
}

func intPtr(n int) *int { return &n }

// TestTaskHandlerCreateRefusals: 400 for every validation rule and a calendar
// that does not store tasks, 403 for a read-only share and a subscribed
// calendar, 404 for no access and an unknown calendar — all with the
// ErrorResponse envelope, and nothing is ever stored. The calendar is
// resolved and authorised before the body is read.
func TestTaskHandlerCreateRefusals(t *testing.T) {
	f := setupTaskHandlerTest(t)
	objectsBefore, changesBefore := f.counts(t)
	nothingStored := func(t *testing.T, name string) {
		t.Helper()
		objects, changes := f.counts(t)
		assert.Equal(t, objectsBefore, objects, "%s: no object stored", name)
		assert.Equal(t, changesBefore, changes, "%s: no change logged", name)
	}

	t.Run("400", func(t *testing.T) {
		for _, tc := range []struct{ name, body, message string }{
			{"empty summary", `{"summary":""}`, "summary is required"},
			{"missing summary", `{"due":"2026-11-05T17:00:00Z"}`, "summary is required"},
			{"blank summary", `{"summary":"   "}`, "summary is required"},
			{"501 runes", `{"summary":"` + strings.Repeat("ß", 501) + `"}`, "summary must be at most 500 characters"},
			{"due before start", `{"summary":"x","start":"2026-11-05T17:00:00+01:00","due":"2026-11-05T15:00:00Z"}`,
				"due must not be before start"},
			{"priority 10", `{"summary":"x","priority":10}`, "priority must be between 0 and 9"},
			{"priority -1", `{"summary":"x","priority":-1}`, "priority must be between 0 and 9"},
			{"percent 101", `{"summary":"x","percent_complete":101}`, "percent_complete must be between 0 and 100"},
			{"percent -1", `{"summary":"x","percent_complete":-1}`, "percent_complete must be between 0 and 100"},
			{"bad status", `{"summary":"x","status":"done"}`,
				`status must be "needs-action", "in-process", "completed" or "cancelled"`},
			{"upper-case status", `{"summary":"x","status":"COMPLETED"}`,
				`status must be "needs-action", "in-process", "completed" or "cancelled"`},
			{"all-day with timed due", `{"summary":"x","due_all_day":true,"due":"2026-11-05T17:00:00+01:00"}`,
				"due must be a date (YYYY-MM-DD) when due_all_day is true"},
			{"all-day with timed start", `{"summary":"x","due_all_day":true,"due":"2026-11-05","start":"2026-11-04T09:00:00Z"}`,
				"start must be a date (YYYY-MM-DD) when due_all_day is true"},
			{"timed with a date-only due", `{"summary":"x","due":"2026-11-05"}`,
				"due must be an RFC 3339 date-time unless due_all_day is true"},
			{"timed due with a date-only start", `{"summary":"x","due":"2026-11-05T17:00:00Z","start":"2026-11-04"}`,
				"start must be an RFC 3339 date-time unless due_all_day is true"},
			{"malformed due", `{"summary":"x","due":"tomorrow"}`,
				"due must be an RFC 3339 date-time unless due_all_day is true"},
			{"broken JSON", `{"summary":`, "Invalid request body"},
			{"priority as text", `{"summary":"x","priority":"high"}`, "Invalid request body"},
			{"fractional priority", `{"summary":"x","priority":2.5}`, "Invalid request body"},
			{"due_all_day as text", `{"summary":"x","due_all_day":"yes"}`, "Invalid request body"},
			{"due as a number", `{"summary":"x","due":20261105}`, "Invalid request body"},
		} {
			code, body := f.post(t, f.ownerToken, f.taskList.UUID, tc.body)
			assert.Equal(t, fiber.StatusBadRequest, code, tc.name)
			assert.JSONEq(t, errorBody(tc.message), body, tc.name)
			nothingStored(t, tc.name)
		}
	})

	t.Run("400 calendar does not store tasks", func(t *testing.T) {
		code, body := f.post(t, f.ownerToken, f.eventCal.UUID, `{"summary":"Renew passport"}`)
		assert.Equal(t, fiber.StatusBadRequest, code)
		assert.JSONEq(t, `{"status":"error","message":"This calendar does not store tasks"}`, body)
		// Checked before the fields: nothing else would make it succeed.
		code, body = f.post(t, f.ownerToken, f.eventCal.UUID, `{"summary":"","priority":12}`)
		assert.Equal(t, fiber.StatusBadRequest, code)
		assert.JSONEq(t, `{"status":"error","message":"This calendar does not store tasks"}`, body)
		nothingStored(t, "event calendar")
	})

	t.Run("403", func(t *testing.T) {
		for _, tc := range []struct {
			name, token string
			cal         *calendar.Calendar
			body        string
		}{
			{"read sharee", f.shareeToken, f.sharedRead, `{"summary":"Renew passport"}`},
			{"read sharee with a broken body", f.shareeToken, f.sharedRead, `{"summary":`},
			{"read sharee with an invalid field", f.shareeToken, f.sharedRead, `{"summary":""}`},
			{"subscribed calendar owner", f.ownerToken, f.subbed, `{"summary":"Renew passport"}`},
		} {
			code, body := f.post(t, tc.token, tc.cal.UUID, tc.body)
			assert.Equal(t, fiber.StatusForbidden, code, tc.name)
			assert.JSONEq(t, readOnlyBody, body, tc.name)
			nothingStored(t, tc.name)
		}
	})

	t.Run("404", func(t *testing.T) {
		for _, tc := range []struct{ name, token, calendar, body string }{
			{"outsider", f.outsiderToken, f.taskList.UUID, `{"summary":"Renew passport"}`},
			{"outsider with a broken body", f.outsiderToken, f.taskList.UUID, `{"summary":`},
			{"outsider on an event calendar", f.outsiderToken, f.eventCal.UUID, `{"summary":"Renew passport"}`},
			{"sharee on an unshared list", f.shareeToken, f.taskList.UUID, `{"summary":"Renew passport"}`},
			{"owner on the sharee's list", f.ownerToken, f.shareeOwnTaskList.UUID, `{"summary":"Renew passport"}`},
			{"unknown calendar", f.ownerToken, "00000000-0000-4000-8000-00000000dead", `{"summary":"Renew passport"}`},
			{"all-zero calendar", f.ownerToken, "00000000-0000-0000-0000-000000000000", `{"summary":"Renew passport"}`},
		} {
			code, body := f.post(t, tc.token, tc.calendar, tc.body)
			assert.Equal(t, fiber.StatusNotFound, code, tc.name)
			assert.JSONEq(t, calendarNotFoundBody, body, tc.name)
			nothingStored(t, tc.name)
		}
	})
}

// TestTaskHandlerCreateServerError (F28): a storage failure is a generic 500,
// the cause in the server log only.
func TestTaskHandlerCreateServerError(t *testing.T) {
	f := setupTaskHandlerTest(t)

	var logBuf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, nil)))
	defer slog.SetDefault(prev)

	require.NoError(t, f.db.DB().Exec("ALTER TABLE calendar_objects RENAME TO calendar_objects_t").Error)
	defer func() {
		require.NoError(t, f.db.DB().Exec("ALTER TABLE calendar_objects_t RENAME TO calendar_objects").Error)
	}()
	code, body := f.post(t, f.ownerToken, f.taskList.UUID, `{"summary":"Renew passport"}`)
	assert.Equal(t, fiber.StatusInternalServerError, code)
	assert.JSONEq(t, internalErrorBody, body)
	assert.NotContains(t, body, "calendar_objects")
	assert.Contains(t, logBuf.String(), "no such table: calendar_objects")
	assert.Contains(t, logBuf.String(), "task request failed")
	assert.Contains(t, logBuf.String(), "method=POST")
}
