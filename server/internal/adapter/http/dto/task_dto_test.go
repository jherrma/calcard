package dto

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// taskJSON renders a TaskResponse and decodes it into a generic map, so the
// tests see the wire form: which keys exist, null versus a value, time strings.
func taskJSON(t *testing.T, resp TaskResponse) map[string]any {
	t.Helper()
	raw, err := json.Marshal(resp)
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

// TestNewTaskResponse pins the TaskResponse rules of #170 story 08: UTC "Z"
// times, an all-day due date as due_date only, start/start_date by DTSTART's
// own value type, lower-case status, completed, priority/percent null when
// absent, the recurrence, and both calendar ids.
func TestNewTaskResponse(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	require.NoError(t, err)
	created := time.Date(2026, 11, 3, 9, 30, 0, 0, berlin)
	timedDue := time.Date(2026, 11, 5, 17, 0, 0, 0, berlin) // 16:00Z, as a non-UTC value
	completedAt := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)

	t.Run("timed task", func(t *testing.T) {
		obj := &calendar.CalendarObject{
			UUID: "6f1c-uuid", CalendarID: 12, UID: "6f1c@calcard.io", Summary: "Renew passport",
			Description: "Bring two photos", Status: calendar.TaskStatusInProcess, DueAt: &timedDue,
			PercentComplete: ptr(40), Priority: ptr(3), ETag: "1735-ab12", CreatedAt: created, UpdatedAt: created,
		}
		start := time.Date(2026, 11, 4, 8, 0, 0, 0, berlin)
		got := taskJSON(t, NewTaskResponse(obj, calendar.TaskDetails{Start: &start}, "9a0e-uuid"))
		assert.Equal(t, map[string]any{
			"id": "6f1c-uuid", "calendar_id": float64(12), "calendar_uuid": "9a0e-uuid", "uid": "6f1c@calcard.io",
			"summary": "Renew passport", "description": "Bring two photos",
			"status": "in-process", "completed": false, "completed_at": nil,
			"percent_complete": float64(40), "priority": float64(3),
			"due": "2026-11-05T16:00:00Z", "due_date": nil, "due_all_day": false,
			"start": "2026-11-04T07:00:00Z", "start_date": nil,
			"is_recurring": false, "rrule": nil, "etag": "1735-ab12",
			"created_at": "2026-11-03T08:30:00Z", "updated_at": "2026-11-03T08:30:00Z",
		}, got)
	})

	t.Run("all-day, completed, recurring task", func(t *testing.T) {
		dueDate := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC) // DUE;VALUE=DATE is midnight UTC
		startDate := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
		obj := &calendar.CalendarObject{
			UUID: "t2", CalendarID: 3, Status: calendar.TaskStatusCompleted, DueAt: &dueDate, IsAllDay: true,
			CompletedAt: &completedAt, PercentComplete: ptr(100),
		}
		got := taskJSON(t, NewTaskResponse(obj, calendar.TaskDetails{
			Start: &startDate, StartAllDay: true, RRule: "FREQ=WEEKLY;BYDAY=MO", Recurring: true,
		}, "list"))
		assert.Nil(t, got["due"], "an all-day due date has no instant")
		assert.Equal(t, "2026-11-05", got["due_date"])
		assert.Equal(t, true, got["due_all_day"])
		assert.Nil(t, got["start"])
		assert.Equal(t, "2026-11-01", got["start_date"])
		assert.Equal(t, "completed", got["status"])
		assert.Equal(t, true, got["completed"])
		assert.Equal(t, "2026-11-02T10:00:00Z", got["completed_at"])
		assert.Equal(t, float64(100), got["percent_complete"])
		assert.Nil(t, got["priority"])
		assert.Equal(t, true, got["is_recurring"])
		assert.Equal(t, "FREQ=WEEKLY;BYDAY=MO", got["rrule"])
	})

	t.Run("start follows its own value type, not the due date's", func(t *testing.T) {
		// RFC 5545 wants DTSTART and DUE of one type; clients do not always
		// comply, and each field reports what is stored.
		dueDate := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
		start := time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)
		got := taskJSON(t, NewTaskResponse(&calendar.CalendarObject{DueAt: &dueDate, IsAllDay: true},
			calendar.TaskDetails{Start: &start}, "list"))
		assert.Equal(t, "2026-11-05", got["due_date"])
		assert.Equal(t, "2026-11-04T08:00:00Z", got["start"])
		assert.Nil(t, got["start_date"])

		got = taskJSON(t, NewTaskResponse(&calendar.CalendarObject{DueAt: &timedDue},
			calendar.TaskDetails{Start: &dueDate, StartAllDay: true}, "list"))
		assert.Equal(t, "2026-11-05T16:00:00Z", got["due"])
		assert.Nil(t, got["start"])
		assert.Equal(t, "2026-11-05", got["start_date"])
	})

	t.Run("no dates, recurring by RDATE only", func(t *testing.T) {
		got := taskJSON(t, NewTaskResponse(&calendar.CalendarObject{UUID: "t3", Status: calendar.TaskStatusNeedsAction},
			calendar.TaskDetails{Recurring: true}, "list"))
		for _, key := range []string{"due", "due_date", "start", "start_date", "completed_at", "percent_complete", "priority", "rrule"} {
			v, present := got[key]
			assert.True(t, present, "%s must always be present", key)
			assert.Nil(t, v, key)
		}
		assert.Equal(t, true, got["is_recurring"])
		assert.Equal(t, false, got["due_all_day"])
	})
}

// TestTaskStatus: the wire form of every Status column value.
func TestTaskStatus(t *testing.T) {
	for column, wire := range map[string]string{
		calendar.TaskStatusNeedsAction: "needs-action",
		calendar.TaskStatusInProcess:   "in-process",
		calendar.TaskStatusCompleted:   "completed",
		calendar.TaskStatusCancelled:   "cancelled",
		"":                             "needs-action",
	} {
		assert.Equal(t, wire, TaskStatus(column), "column %q", column)
	}
	cancelled := NewTaskResponse(&calendar.CalendarObject{Status: calendar.TaskStatusCancelled}, calendar.TaskDetails{}, "")
	assert.False(t, cancelled.Completed, "completed means status completed, not done")
}

// TestUpdateTaskRequestAbsentVsNull: OptionalField tells an absent key (Set
// false) from an explicit null (Set, nil Value) and from a value; a plain
// pointer field reads null as absent. A value of the wrong type fails the
// decode.
func TestUpdateTaskRequestAbsentVsNull(t *testing.T) {
	decode := func(t *testing.T, body string) UpdateTaskRequest {
		t.Helper()
		var req UpdateTaskRequest
		require.NoError(t, json.Unmarshal([]byte(body), &req), body)
		return req
	}

	absent := decode(t, `{}`)
	for name, f := range map[string]OptionalField[string]{"description": absent.Description, "due": absent.Due, "start": absent.Start} {
		assert.Equal(t, OptionalField[string]{}, f, "%s absent", name)
	}
	assert.Equal(t, OptionalField[int]{}, absent.Priority)
	assert.Equal(t, OptionalField[int]{}, absent.PercentComplete)
	assert.Nil(t, absent.Summary)
	assert.Nil(t, absent.Status)
	assert.Nil(t, absent.DueAllDay)

	null := decode(t, `{"description":null,"due":null,"start": null ,"priority":null,"percent_complete":null,"summary":null,"status":null,"due_all_day":null}`)
	for name, f := range map[string]OptionalField[string]{"description": null.Description, "due": null.Due, "start": null.Start} {
		assert.Equal(t, OptionalField[string]{Set: true}, f, "%s null", name)
	}
	assert.Equal(t, OptionalField[int]{Set: true}, null.Priority)
	assert.Equal(t, OptionalField[int]{Set: true}, null.PercentComplete)
	assert.Nil(t, null.Summary, "summary cannot be removed: null reads as absent")
	assert.Nil(t, null.Status)
	assert.Nil(t, null.DueAllDay)

	values := decode(t, `{"description":"","due":"2026-11-05","start":"2026-11-04T08:00:00Z","priority":0,"percent_complete":100,`+
		`"summary":"S","status":"completed","due_all_day":true,"timezone":"Europe/Berlin"}`)
	assert.Equal(t, OptionalField[string]{Set: true, Value: ptr("")}, values.Description)
	assert.Equal(t, OptionalField[string]{Set: true, Value: ptr("2026-11-05")}, values.Due)
	assert.Equal(t, OptionalField[string]{Set: true, Value: ptr("2026-11-04T08:00:00Z")}, values.Start)
	assert.Equal(t, OptionalField[int]{Set: true, Value: ptr(0)}, values.Priority, "0 is a value, not absent")
	assert.Equal(t, OptionalField[int]{Set: true, Value: ptr(100)}, values.PercentComplete)
	assert.Equal(t, "S", *values.Summary)
	assert.Equal(t, "completed", *values.Status)
	assert.True(t, *values.DueAllDay)
	assert.Equal(t, "Europe/Berlin", values.Timezone)

	// A second decode into the same struct starts over (as a reused value
	// would): a null after a value clears it.
	var reused OptionalField[int]
	require.NoError(t, json.Unmarshal([]byte(`5`), &reused))
	require.NoError(t, json.Unmarshal([]byte(`null`), &reused))
	assert.Equal(t, OptionalField[int]{Set: true}, reused)

	for _, body := range []string{`{"priority":"high"}`, `{"priority":1.5}`, `{"due":5}`, `{"description":false}`, `{"percent_complete":[1]}`} {
		var req UpdateTaskRequest
		assert.Error(t, json.Unmarshal([]byte(body), &req), body)
	}
}

// TestNewTaskSummaryResponse pins the wire shape of GET /api/v1/tasks/summary
// (#170 story 13): the four totals and by_calendar keyed by calendar UUID
// with open and overdue only; by_calendar is {} (never null) without lists.
func TestNewTaskSummaryResponse(t *testing.T) {
	render := func(resp TaskSummaryResponse) string {
		raw, err := json.Marshal(resp)
		require.NoError(t, err)
		return string(raw)
	}

	resp := NewTaskSummaryResponse(
		calendar.TaskCount{Open: 14, Overdue: 3, DueToday: 2, Completed: 41},
		map[string]calendar.TaskCount{
			"9a0e0000-0000-4000-8000-000000000001": {Open: 5, Overdue: 1, DueToday: 2, Completed: 40},
			"9a0e0000-0000-4000-8000-000000000002": {Open: 9, Overdue: 2, Completed: 1},
			"9a0e0000-0000-4000-8000-000000000003": {},
		})
	assert.JSONEq(t, `{"open":14,"overdue":3,"due_today":2,"completed":41,"by_calendar":{
		"9a0e0000-0000-4000-8000-000000000001":{"open":5,"overdue":1},
		"9a0e0000-0000-4000-8000-000000000002":{"open":9,"overdue":2},
		"9a0e0000-0000-4000-8000-000000000003":{"open":0,"overdue":0}}}`, render(resp))

	for _, byCalendar := range []map[string]calendar.TaskCount{nil, {}} {
		assert.JSONEq(t, `{"open":0,"overdue":0,"due_today":0,"completed":0,"by_calendar":{}}`,
			render(NewTaskSummaryResponse(calendar.TaskCount{}, byCalendar)))
	}
}
