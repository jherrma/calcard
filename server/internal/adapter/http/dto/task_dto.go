package dto

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

// dateLayout is the wire form of a date without a time ("YYYY-MM-DD").
const dateLayout = "2006-01-02"

// TaskResponse is one task (a VTODO) as the task routes answer it (#170), raw
// JSON like events. Every nullable field is always present, as null.
//
//   - Status is lower-case with hyphens: needs-action, in-process, completed
//     or cancelled. Completed is status == completed (the checkbox state).
//   - Priority is the RFC 5545 integer 1..9 (1 highest), or null when the task
//     has none (PRIORITY 0 or absent). The web UI maps 1-4 to high, 5 to
//     medium and 6-9 to low.
//   - Due is an RFC 3339 instant in UTC ("Z"). For an all-day task
//     (DueAllDay, a date-only DUE) Due is null and the date is in DueDate
//     ("YYYY-MM-DD", no time, so a browser west of UTC cannot shift it a day
//     through new Date()); a timed task has DueDate null. DueAllDay is the
//     task's denormalized IsAllDay, which is read from DUE, else DTSTART.
//   - Start/StartDate follow the same rule for DTSTART, by its own value type.
//   - IsRecurring marks a task with RRULE or RDATE. It is listed once (its
//     master) and never expanded; RRule is its rule, read-only, or null.
//   - CalendarID is the calendar's numeric id (parity with EventResponse; a
//     later API revision should move both to UUIDs); CalendarUUID is the id
//     every task URL uses.
type TaskResponse struct {
	ID              string     `json:"id"`
	CalendarID      uint       `json:"calendar_id"`
	CalendarUUID    string     `json:"calendar_uuid"`
	UID             string     `json:"uid"`
	Summary         string     `json:"summary"`
	Description     string     `json:"description"`
	Status          string     `json:"status"`
	Completed       bool       `json:"completed"`
	CompletedAt     *time.Time `json:"completed_at"`
	PercentComplete *int       `json:"percent_complete"`
	Priority        *int       `json:"priority"`
	Due             *time.Time `json:"due"`
	DueDate         *string    `json:"due_date"`
	DueAllDay       bool       `json:"due_all_day"`
	Start           *time.Time `json:"start"`
	StartDate       *string    `json:"start_date"`
	IsRecurring     bool       `json:"is_recurring"`
	RRule           *string    `json:"rrule"`
	ETag            string     `json:"etag"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// CreateTaskRequest is the body of POST /calendars/:calendar_id/tasks (#170).
// It is bound without a struct validator; taskuc.CreateTaskUseCase validates
// every field. Due and Start are strings because an all-day task sends a date
// ("YYYY-MM-DD"); a timed one sends RFC 3339. null, absent and "" all mean
// none, for them and for Priority and PercentComplete. There is no recurrence
// field: a repeating task cannot be created over REST, and an unknown field
// (recurrence, rrule) is ignored like any other.
type CreateTaskRequest struct {
	Summary         string `json:"summary"`
	Description     string `json:"description"`
	Due             string `json:"due"`
	DueAllDay       bool   `json:"due_all_day"`
	Start           string `json:"start"`
	Priority        *int   `json:"priority"`
	PercentComplete *int   `json:"percent_complete"`
	Status          string `json:"status"`
	Timezone        string `json:"timezone"`
}

// UpdateTaskRequest is the body of PATCH /calendars/:calendar_id/tasks/:task_id
// (#170): every field is optional and only the fields sent are changed.
// Description, Due, Start, Priority and PercentComplete are OptionalFields,
// so an explicit null removes the property while an absent key leaves it as
// stored; for Description, Due and Start "" removes it too. Summary, Status
// and DueAllDay cannot be removed: null is the same as absent. Due and Start
// are strings for the reason given at CreateTaskRequest; Timezone applies to
// the due/start sent in the same body. There is no recurrence and no list
// field; unknown fields are ignored. taskuc.UpdateTaskUseCase validates.
type UpdateTaskRequest struct {
	Summary         *string               `json:"summary"`
	Description     OptionalField[string] `json:"description"`
	Due             OptionalField[string] `json:"due"`
	DueAllDay       *bool                 `json:"due_all_day"`
	Start           OptionalField[string] `json:"start"`
	Priority        OptionalField[int]    `json:"priority"`
	PercentComplete OptionalField[int]    `json:"percent_complete"`
	Status          *string               `json:"status"`
	Timezone        string                `json:"timezone"`
}

// OptionalField is a JSON field of a partial update that tells an absent key
// from an explicit null, which a plain pointer cannot (encoding/json leaves
// it nil for both): Set is true when the key was in the body, Value is nil
// for null and points to the decoded value otherwise. A value of the wrong
// JSON type fails the whole decode.
type OptionalField[T any] struct {
	Set   bool
	Value *T
}

// UnmarshalJSON is only called when the key is present (encoding/json calls
// it for null too), so it always marks the field Set.
func (f *OptionalField[T]) UnmarshalJSON(data []byte) error {
	f.Set = true
	f.Value = nil
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return nil
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	f.Value = &v
	return nil
}

// TaskListResponse is one page of a task list: Count tasks on this page,
// Total across all pages.
type TaskListResponse struct {
	Tasks []TaskResponse `json:"tasks"`
	Count int            `json:"count"`
	Total int64          `json:"total"`
}

// TaskWithCalendarResponse is one task of the cross-list GET /api/v1/tasks:
// the TaskResponse fields plus the name and colour of the task list that
// holds it (the list whose UUID is CalendarUUID), so a row can be labelled
// without a second request.
type TaskWithCalendarResponse struct {
	TaskResponse
	CalendarName  string `json:"calendar_name"`
	CalendarColor string `json:"calendar_color"`
}

// NewTaskWithCalendarResponse renders a task of the cross-list page with the
// name and colour of list, the task list that holds it. REST GET /api/v1/tasks
// and the MCP list_tasks tool both render through it, so the two answer a task
// alike.
func NewTaskWithCalendarResponse(obj *calendar.CalendarObject, details calendar.TaskDetails, list calendar.ReadableCalendar) TaskWithCalendarResponse {
	return TaskWithCalendarResponse{
		TaskResponse:  NewTaskResponse(obj, details, list.UUID),
		CalendarName:  list.Name,
		CalendarColor: list.Color,
	}
}

// TaskWithCalendarListResponse is one page of GET /api/v1/tasks: Count tasks
// on this page, Total across all pages.
type TaskWithCalendarListResponse struct {
	Tasks []TaskWithCalendarResponse `json:"tasks"`
	Count int                        `json:"count"`
	Total int64                      `json:"total"`
}

// TaskSummaryResponse is the body of GET /api/v1/tasks/summary (#170): the
// task counts of every task list the caller can read, taken at the request's
// as_of. Open, Overdue and DueToday count open tasks (needs-action,
// in-process); Completed counts the done ones, completed AND cancelled, so it
// equals the total of GET /api/v1/tasks?status=completed. A cancelled task
// never counts as open, overdue or due today. Overdue and DueToday overlap: a
// timed task due earlier today is both. ByCalendar has an entry, keyed by
// calendar UUID, for every readable task list, zeros included.
type TaskSummaryResponse struct {
	Open       int64                             `json:"open"`
	Overdue    int64                             `json:"overdue"`
	DueToday   int64                             `json:"due_today"`
	Completed  int64                             `json:"completed"`
	ByCalendar map[string]TaskListCountsResponse `json:"by_calendar"`
}

// TaskListCountsResponse is one task list's entry in
// TaskSummaryResponse.ByCalendar: its open and overdue tasks.
type TaskListCountsResponse struct {
	Open    int64 `json:"open"`
	Overdue int64 `json:"overdue"`
}

// NewTaskSummaryResponse renders the counts: total across every list, and
// byCalendar keyed by calendar UUID. ByCalendar is never null on the wire
// (an empty object for a caller without task lists).
func NewTaskSummaryResponse(total calendar.TaskCount, byCalendar map[string]calendar.TaskCount) TaskSummaryResponse {
	resp := TaskSummaryResponse{
		Open:       total.Open,
		Overdue:    total.Overdue,
		DueToday:   total.DueToday,
		Completed:  total.Completed,
		ByCalendar: make(map[string]TaskListCountsResponse, len(byCalendar)),
	}
	for uuid, c := range byCalendar {
		resp.ByCalendar[uuid] = TaskListCountsResponse{Open: c.Open, Overdue: c.Overdue}
	}
	return resp
}

// NewTaskResponse renders a stored task. obj carries the denormalized task
// columns; details the fields that have no column (calendar.TaskDetails);
// calendarUUID is the UUID of obj's calendar.
func NewTaskResponse(obj *calendar.CalendarObject, details calendar.TaskDetails, calendarUUID string) TaskResponse {
	resp := TaskResponse{
		ID:              obj.UUID,
		CalendarID:      obj.CalendarID,
		CalendarUUID:    calendarUUID,
		UID:             obj.UID,
		Summary:         obj.Summary,
		Description:     obj.Description,
		Status:          TaskStatus(obj.Status),
		Completed:       obj.Status == calendar.TaskStatusCompleted,
		CompletedAt:     utcPtr(obj.CompletedAt),
		PercentComplete: copyInt(obj.PercentComplete),
		Priority:        copyInt(obj.Priority),
		DueAllDay:       obj.IsAllDay,
		IsRecurring:     details.Recurring,
		ETag:            obj.ETag,
		CreatedAt:       obj.CreatedAt.UTC(),
		UpdatedAt:       obj.UpdatedAt.UTC(),
	}
	resp.Due, resp.DueDate = instantOrDate(obj.DueAt, obj.IsAllDay)
	resp.Start, resp.StartDate = instantOrDate(details.Start, details.StartAllDay)
	if details.RRule != "" {
		rule := details.RRule
		resp.RRule = &rule
	}
	return resp
}

// TaskStatus is the wire form of a task Status column value: lower-case with
// hyphens. An empty column (a row the backfill could not decode) reads
// needs-action, like an unknown STATUS does.
func TaskStatus(status string) string {
	if status == "" {
		status = calendar.TaskStatusNeedsAction
	}
	return strings.ToLower(status)
}

// instantOrDate splits a stored time into the wire pair: a date-only value is
// (nil, "YYYY-MM-DD"), a timed one (UTC instant, nil), none (nil, nil).
func instantOrDate(t *time.Time, dateOnly bool) (*time.Time, *string) {
	if t == nil {
		return nil, nil
	}
	utc := t.UTC()
	if dateOnly {
		date := utc.Format(dateLayout)
		return nil, &date
	}
	return &utc, nil
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	utc := t.UTC()
	return &utc
}

func copyInt(v *int) *int {
	if v == nil {
		return nil
	}
	n := *v
	return &n
}
