package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jherrma/caldav-server/internal/adapter/http/dto"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	taskuc "github.com/jherrma/caldav-server/internal/usecase/task"
)

// The task tools (#170): tasks are VTODOs in task lists, calendars whose kind
// is "tasks" or "both". Every tool is a facade over the use case the matching
// REST task route calls (Deps.Task*): it decodes the arguments, checks access
// the way the REST handler does (taskPermission / loadCalendar), makes ONE use
// case call and renders the result with the REST DTOs, so a task reads
// byte-for-byte as REST answers it. Validation, the component-set check, the
// recurring refusal, idempotency, the in-place edit, the hard delete, paging
// caps and the counting all stay in the use cases.
//
// Argument names are the REST body and query names (query is REST's q), and
// times are passed to the use cases as strings, unparsed: their format rules
// are the use cases'. Only the list bounds and as_of are parsed here, as the
// REST handler parses its query parameters, with time.RFC3339 alone (an offset
// or Z is required; a bare date is refused, unlike parseTime).

// defaultTaskLimit is list_tasks' page size when no limit is passed: smaller
// than REST's 500 because every task lands in the model's context. The use
// case caps any limit at taskuc.MaxLimit.
const defaultTaskLimit = 50

// taskReadOnly is the refusal of a write on a task the caller can only read:
// a read share, or a subscribed calendar (read-only to its owner too).
const taskReadOnly = "You have read-only access to the task list holding that task."

func (s *Server) registerTaskTools() {
	s.register(Tool{
		Name: "list_tasks",
		Description: "List tasks (to-dos) across every task list the user can read — own, shared " +
			"and subscribed — or, with calendar_id, in one list. Each task carries its id (a UUID " +
			"for update_task, complete_task, uncomplete_task and delete_task), the calendar_uuid, " +
			"calendar_name and calendar_color of its list, status, due date, priority and " +
			"is_recurring. status open (the default) lists needs-action and in-process tasks, " +
			"completed lists completed AND cancelled ones, all lists both. Sorted by due date " +
			"(undated last), then priority. Returns at most limit tasks (default 50, max 1000); " +
			"total is the number of matching tasks, so a total above count means more exist: " +
			"fetch them with offset. A task without a due date never matches due_after or " +
			"due_before. For counts (overdue, due today) use get_task_summary.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "calendar_id": {"type": "string", "description": "Optional UUID of one task list (kind tasks or both) from list_calendars. Without it, every task list the user can read"},
    "status": {"type": "string", "enum": ["open", "completed", "all"], "description": "open (default: needs-action and in-process), completed (completed and cancelled) or all"},
    "query": {"type": "string", "description": "Case-insensitive text to find in the title or the notes"},
    "due_after": {"type": "string", "description": "Only tasks due at or after this instant, RFC 3339 with an offset or Z"},
    "due_before": {"type": "string", "description": "Only tasks due before this instant, RFC 3339 with an offset or Z"},
    "limit": {"type": "integer", "description": "Maximum tasks returned, default 50, max 1000"},
    "offset": {"type": "integer", "description": "Number of matching tasks to skip, for the next page"}
  },
  "additionalProperties": false
}`),
	}, s.toolListTasks)

	s.register(Tool{
		Name: "create_task",
		Description: "Create a task (to-do) in a task list. Only a calendar of kind tasks or both " +
			"(list_calendars) takes tasks; a calendar of kind events refuses them. A task created " +
			"here cannot repeat: there is no repeat rule. due and start are RFC 3339 date-times " +
			"with an offset or Z (e.g. 2026-11-05T17:00:00+01:00), or dates (YYYY-MM-DD) when " +
			"due_all_day is true; due must not be before start. With timezone (IANA) a timed " +
			"due/start is stored in that zone, otherwise in UTC. Returns the task with its id.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "calendar_id": {"type": "string", "description": "UUID of a task list (kind tasks or both) from list_calendars"},
    "summary": {"type": "string", "description": "Title, required, at most 500 characters"},
    "description": {"type": "string", "description": "Notes"},
    "due": {"type": "string", "description": "Due date-time, RFC 3339 with an offset or Z; a date YYYY-MM-DD when due_all_day is true"},
    "due_all_day": {"type": "boolean", "description": "true: due and start are dates (YYYY-MM-DD). Defaults to false"},
    "start": {"type": "string", "description": "Start, in the same format as due"},
    "priority": {"type": "integer", "minimum": 0, "maximum": 9, "description": "1 (highest) to 9 (lowest): 1 high, 5 medium, 9 low; 0: none"},
    "percent_complete": {"type": "integer", "minimum": 0, "maximum": 100},
    "status": {"type": "string", "enum": ["needs-action", "in-process", "completed", "cancelled"], "description": "Defaults to needs-action"},
    "timezone": {"type": "string", "description": "IANA timezone for a timed due/start, e.g. Europe/Berlin"}
  },
  "required": ["calendar_id", "summary"],
  "additionalProperties": false
}`),
	}, s.toolCreateTask)

	s.register(Tool{
		Name: "update_task",
		Description: "Change a task. Only the fields you pass change; omitted fields keep their " +
			"stored value. Pass null to remove description, due, start, priority or " +
			"percent_complete; summary, status and due_all_day cannot be removed. The stored task " +
			"keeps everything else a task app wrote on it, including its alarms, subtask links and " +
			"repeat rule. status completed works like complete_task, another status on a " +
			"completed task like uncomplete_task; a repeating task (is_recurring) refuses status " +
			"completed or cancelled and percent_complete 100. due/start formats as in " +
			"create_task. A call that changes nothing returns the task unchanged (same etag).",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "task_id": {"type": "string", "description": "Task UUID from list_tasks"},
    "summary": {"type": "string", "description": "New title, at most 500 characters"},
    "description": {"type": ["string", "null"], "description": "New notes; null removes them"},
    "due": {"type": ["string", "null"], "description": "New due date-time (RFC 3339 with an offset or Z) or date (YYYY-MM-DD when the task is all-day); null removes it"},
    "start": {"type": ["string", "null"], "description": "New start, in the same format as due; null removes it"},
    "due_all_day": {"type": "boolean", "description": "Whether due and start are dates; send it with due/start when switching"},
    "priority": {"type": ["integer", "null"], "minimum": 0, "maximum": 9, "description": "1 (highest) to 9 (lowest); null removes it"},
    "percent_complete": {"type": ["integer", "null"], "minimum": 0, "maximum": 100, "description": "null removes it"},
    "status": {"type": "string", "enum": ["needs-action", "in-process", "completed", "cancelled"]},
    "timezone": {"type": "string", "description": "IANA timezone for a timed due/start passed in this call; without it they are stored in UTC"}
  },
  "required": ["task_id"],
  "additionalProperties": false
}`),
	}, s.toolUpdateTask)

	s.register(Tool{
		Name: "complete_task",
		Description: "Mark a task completed: status completed, completed_at now, percent_complete " +
			"100. Idempotent: completing a completed task changes nothing (same etag). A repeating " +
			"task (is_recurring true) is refused: it cannot be completed here, the user has to " +
			"complete it in their task app.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "task_id": {"type": "string", "description": "Task UUID from list_tasks"}
  },
  "required": ["task_id"],
  "additionalProperties": false
}`),
	}, s.toolCompleteTask)

	s.register(Tool{
		Name: "uncomplete_task",
		Description: "Reopen a completed task: status needs-action, completed_at removed, and " +
			"percent_complete back to 0 only if it was 100 (a percentage from before completing is " +
			"not restored). Allowed for repeating tasks too. A task that is not completed " +
			"(needs-action, in-process or cancelled) is returned unchanged.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "task_id": {"type": "string", "description": "Task UUID from list_tasks"}
  },
  "required": ["task_id"],
  "additionalProperties": false
}`),
	}, s.toolUncompleteTask)

	s.register(Tool{
		Name: "delete_task",
		Description: "PERMANENTLY delete a task. It cannot be undone or restored and disappears " +
			"from every device that syncs the task list; a repeating task goes with all its " +
			"occurrences. Always name the task to the user and call this only after they agreed. " +
			"To tick a task off, use complete_task instead.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "task_id": {"type": "string", "description": "Task UUID from list_tasks"}
  },
  "required": ["task_id"],
  "additionalProperties": false
}`),
	}, s.toolDeleteTask)

	s.register(Tool{
		Name: "get_task_summary",
		Description: "Count the user's tasks across every task list they can read: open " +
			"(needs-action and in-process), overdue, due_today and completed (completed AND " +
			"cancelled), plus open and overdue per list in by_calendar, keyed by the list's UUID " +
			"(every list, zeros included). as_of is the user's current time with their UTC offset; " +
			"that offset decides which date is \"today\". A timed task is overdue once its due time " +
			"has passed, an all-day task only from the day after its date. overdue and due_today " +
			"overlap: a timed task due earlier today counts in both. Answers \"what is overdue / " +
			"due today?\" without listing every task.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "as_of": {"type": "string", "description": "The user's current time, RFC 3339 with their UTC offset, e.g. 2026-11-04T09:00:00+01:00"}
  },
  "required": ["as_of"],
  "additionalProperties": false
}`),
	}, s.toolGetTaskSummary)
}

// taskNotReadable is the one answer for a task id the caller cannot reach:
// unknown, an event's UUID, another user's task, or a task in a calendar not
// shared with the caller — like REST's single 404 "Task not found".
func taskNotReadable(id string) *toolCallResult {
	return errorResult("No task with id " + id + " is readable by you.")
}

// calendarNotReadable is get_events' answer for a calendar id the caller
// cannot read, which list_tasks and create_task give too.
func calendarNotReadable(id string) *toolCallResult {
	return errorResult("No calendar with id " + id + " is readable by you.")
}

// taskToolError maps a task use-case error the way the REST task handler
// does: not a task of that calendar any more (a CalDAV PUT may have turned it
// into an event since taskPermission looked) → the unknown-task answer; the
// recurring refusal (REST's 409) and invalid input (REST's 400) → their own
// sentence; a calendar_id the caller cannot read (list_tasks) → the
// unknown-calendar answer; anything else → a generic internal error with the
// cause in the log. id is the identifier the call names: the task_id, or the
// calendar_id for list_tasks and create_task (which cannot get ErrNotFound).
func taskToolError(tool, id string, err error) *toolCallResult {
	switch {
	case errors.Is(err, taskuc.ErrNotFound):
		return taskNotReadable(id)
	case errors.Is(err, taskuc.ErrRecurringNotSupported):
		return errorResult(err.Error())
	case errors.Is(err, taskuc.ErrInvalidInput):
		return errorResult(err.Error())
	case errors.Is(err, taskuc.ErrCalendarNotFound):
		return calendarNotReadable(id)
	}
	return toolInternalError(tool, err)
}

// writableTask runs what every write on an existing task checks before its
// use case, in REST's order: a task_id was given; it names a task in a
// calendar the caller can read (else the unknown-task answer, also for an
// event's UUID); the caller may write that calendar (else read-only). It
// returns the task's calendar, or the refusal to answer with.
func (s *Server) writableTask(cc *callContext, taskID string) (*calendar.Calendar, *toolCallResult) {
	if taskID == "" {
		return nil, errorResult("task_id is required.")
	}
	_, cal, perm := s.taskPermission(cc, taskID)
	if perm == calendar.PermissionNone {
		return nil, taskNotReadable(taskID)
	}
	if !canWriteCalendar(perm) {
		return nil, errorResult(taskReadOnly)
	}
	return cal, nil
}

// taskView renders a task of cal exactly as the REST task routes do.
func taskView(t *taskuc.Task, cal *calendar.Calendar) dto.TaskResponse {
	return dto.NewTaskResponse(t.Object, t.Details, cal.UUID)
}

// rfc3339Argument parses an optional instant argument the way the REST task
// routes parse due_after, due_before and as_of: RFC 3339 with an offset or Z,
// nothing else. "" is nil.
func rfc3339Argument(field, value string) (*time.Time, *toolCallResult) {
	if value == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, errorResult(fmt.Sprintf("%s must be an RFC 3339 timestamp with a UTC offset, got %q.", field, value))
	}
	return &t, nil
}

func (s *Server) toolListTasks(cc *callContext, args json.RawMessage) (*toolCallResult, *RPCError) {
	var in struct {
		CalendarID string `json:"calendar_id"`
		Status     string `json:"status"`
		Query      string `json:"query"`
		DueAfter   string `json:"due_after"`
		DueBefore  string `json:"due_before"`
		Limit      int    `json:"limit"`
		Offset     int    `json:"offset"`
	}
	if bad := decodeArgs(args, &in); bad != nil {
		return bad, nil
	}

	// The argument checks come before the use case's status check and its
	// calendar check, as REST checks its query parameters first.
	if in.Limit < 0 {
		return errorResult("limit must not be negative."), nil
	}
	if in.Offset < 0 {
		return errorResult("offset must not be negative."), nil
	}
	limit := in.Limit
	if limit == 0 {
		limit = defaultTaskLimit
	}
	dueAfter, bad := rfc3339Argument("due_after", in.DueAfter)
	if bad != nil {
		return bad, nil
	}
	dueBefore, bad := rfc3339Argument("due_before", in.DueBefore)
	if bad != nil {
		return bad, nil
	}

	// calendar_id narrows like REST's calendar_uuid: the use case answers a
	// calendar the caller cannot read with ErrCalendarNotFound, never an
	// empty page.
	out, err := s.deps.TaskListAll.Execute(cc.ctx, taskuc.ListAllTasksInput{
		UserID:       cc.userID,
		CalendarUUID: in.CalendarID,
		Status:       in.Status,
		Query:        in.Query,
		DueAfter:     dueAfter,
		DueBefore:    dueBefore,
		Limit:        limit,
		Offset:       in.Offset,
	})
	if err != nil {
		return taskToolError("list_tasks", in.CalendarID, err), nil
	}

	tasks := make([]dto.TaskWithCalendarResponse, len(out.Tasks))
	for i, t := range out.Tasks {
		tasks[i] = dto.TaskWithCalendarResponse{
			TaskResponse:  dto.NewTaskResponse(t.Object, t.Details, t.Calendar.UUID),
			CalendarName:  t.Calendar.Name,
			CalendarColor: t.Calendar.Color,
		}
	}
	return jsonText(dto.TaskWithCalendarListResponse{Tasks: tasks, Count: len(tasks), Total: out.Total})
}

func (s *Server) toolCreateTask(cc *callContext, args json.RawMessage) (*toolCallResult, *RPCError) {
	var in struct {
		CalendarID      string `json:"calendar_id"`
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
	if bad := decodeArgs(args, &in); bad != nil {
		return bad, nil
	}
	if in.CalendarID == "" {
		return errorResult("calendar_id is required."), nil
	}

	cal, perm := s.loadCalendar(cc, in.CalendarID)
	if perm == calendar.PermissionNone {
		return calendarNotReadable(in.CalendarID), nil
	}
	if !canWriteCalendar(perm) {
		return errorResult("You have read-only access to that calendar, so tasks cannot be created in it."), nil
	}

	t, err := s.deps.TaskCreate.Execute(cc.ctx, taskuc.CreateTaskInput{
		Calendar:        cal,
		Summary:         in.Summary,
		Description:     in.Description,
		Due:             in.Due,
		DueAllDay:       in.DueAllDay,
		Start:           in.Start,
		Priority:        in.Priority,
		PercentComplete: in.PercentComplete,
		Status:          in.Status,
		Timezone:        in.Timezone,
	})
	if err != nil {
		return taskToolError("create_task", in.CalendarID, err), nil
	}
	return jsonText(map[string]interface{}{
		"created": true,
		"task":    taskView(t, cal),
	})
}

func (s *Server) toolUpdateTask(cc *callContext, args json.RawMessage) (*toolCallResult, *RPCError) {
	// The five removable fields tell an absent key from null exactly as the
	// REST PATCH body does (dto.OptionalField), and are converted the same way.
	var in struct {
		TaskID          string                    `json:"task_id"`
		Summary         *string                   `json:"summary"`
		Description     dto.OptionalField[string] `json:"description"`
		Due             dto.OptionalField[string] `json:"due"`
		Start           dto.OptionalField[string] `json:"start"`
		DueAllDay       *bool                     `json:"due_all_day"`
		Priority        dto.OptionalField[int]    `json:"priority"`
		PercentComplete dto.OptionalField[int]    `json:"percent_complete"`
		Status          *string                   `json:"status"`
		Timezone        string                    `json:"timezone"`
	}
	if bad := decodeArgs(args, &in); bad != nil {
		return bad, nil
	}
	cal, refused := s.writableTask(cc, in.TaskID)
	if refused != nil {
		return refused, nil
	}

	t, err := s.deps.TaskUpdate.Execute(cc.ctx, taskuc.UpdateTaskInput{
		Calendar:        cal,
		UUID:            in.TaskID,
		Summary:         in.Summary,
		Description:     taskuc.Change[string](in.Description),
		Due:             taskuc.Change[string](in.Due),
		DueAllDay:       in.DueAllDay,
		Start:           taskuc.Change[string](in.Start),
		Priority:        taskuc.Change[int](in.Priority),
		PercentComplete: taskuc.Change[int](in.PercentComplete),
		Status:          in.Status,
		Timezone:        in.Timezone,
	})
	if err != nil {
		return taskToolError("update_task", in.TaskID, err), nil
	}
	return jsonText(map[string]interface{}{
		"updated": true,
		"task":    taskView(t, cal),
	})
}

func (s *Server) toolCompleteTask(cc *callContext, args json.RawMessage) (*toolCallResult, *RPCError) {
	var in struct {
		TaskID string `json:"task_id"`
	}
	if bad := decodeArgs(args, &in); bad != nil {
		return bad, nil
	}
	cal, refused := s.writableTask(cc, in.TaskID)
	if refused != nil {
		return refused, nil
	}

	t, err := s.deps.TaskComplete.Execute(cc.ctx, taskuc.CompleteTaskInput{Calendar: cal, UUID: in.TaskID})
	if err != nil {
		return taskToolError("complete_task", in.TaskID, err), nil
	}
	return jsonText(map[string]interface{}{"task": taskView(t, cal)})
}

func (s *Server) toolUncompleteTask(cc *callContext, args json.RawMessage) (*toolCallResult, *RPCError) {
	var in struct {
		TaskID string `json:"task_id"`
	}
	if bad := decodeArgs(args, &in); bad != nil {
		return bad, nil
	}
	cal, refused := s.writableTask(cc, in.TaskID)
	if refused != nil {
		return refused, nil
	}

	t, err := s.deps.TaskUncomplete.Execute(cc.ctx, taskuc.CompleteTaskInput{Calendar: cal, UUID: in.TaskID})
	if err != nil {
		return taskToolError("uncomplete_task", in.TaskID, err), nil
	}
	return jsonText(map[string]interface{}{"task": taskView(t, cal)})
}

func (s *Server) toolDeleteTask(cc *callContext, args json.RawMessage) (*toolCallResult, *RPCError) {
	var in struct {
		TaskID string `json:"task_id"`
	}
	if bad := decodeArgs(args, &in); bad != nil {
		return bad, nil
	}
	cal, refused := s.writableTask(cc, in.TaskID)
	if refused != nil {
		return refused, nil
	}

	// A hard delete, like the REST route: the use case removes the row for
	// good and records the deletion for DAV sync.
	if err := s.deps.TaskDelete.Execute(cc.ctx, taskuc.DeleteTaskInput{CalendarID: cal.ID, UUID: in.TaskID}); err != nil {
		return taskToolError("delete_task", in.TaskID, err), nil
	}
	return jsonText(map[string]interface{}{
		"deleted": true,
		"task_id": in.TaskID,
	})
}

func (s *Server) toolGetTaskSummary(cc *callContext, args json.RawMessage) (*toolCallResult, *RPCError) {
	var in struct {
		AsOf string `json:"as_of"`
	}
	if bad := decodeArgs(args, &in); bad != nil {
		return bad, nil
	}
	if in.AsOf == "" {
		return errorResult("as_of is required: pass the user's current time in RFC 3339 with their UTC offset, e.g. 2026-11-04T09:00:00+01:00."), nil
	}
	asOf, bad := rfc3339Argument("as_of", in.AsOf)
	if bad != nil {
		return bad, nil
	}

	out, err := s.deps.TaskSummary.Execute(cc.ctx, taskuc.TaskSummaryInput{UserID: cc.userID, AsOf: *asOf})
	if err != nil {
		// The summary use case has no error the caller could act on (REST
		// answers every one with a 500).
		return toolInternalError("get_task_summary", err), nil
	}
	return jsonText(dto.NewTaskSummaryResponse(out.Total, out.ByCalendar))
}
