package http

import (
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/adapter/http/dto"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	taskuc "github.com/jherrma/caldav-server/internal/usecase/task"
)

// Task routes (#170). A task is a VTODO calendar object; the routes mirror the
// event routes and share their access rules (calendar_access.go): a calendar
// the caller cannot read, or that does not exist, is 404 "Calendar not found"
// on the list route; on a task route every miss — unknown or unreadable
// calendar, unknown task, a task filed under another calendar, an event's
// UUID — is the same 404 "Task not found". Responses are raw JSON
// (dto.TaskResponse); a server failure is a generic 500 with the cause logged.
// The cross-list GET /api/v1/tasks (ListAll) hangs off /api/v1 itself and
// lists the tasks of every task list the caller can read; GET
// /api/v1/tasks/summary (Summary) counts them.

const (
	taskNotFoundMessage     = "Task not found"
	calendarNotFoundMessage = "Calendar not found"
)

// TaskHandler serves the task routes under /api/v1/calendars/:calendar_id/tasks
// and the cross-list /api/v1/tasks and /api/v1/tasks/summary.
type TaskHandler struct {
	listUC       *taskuc.ListTasksUseCase
	getUC        *taskuc.GetTaskUseCase
	createUC     *taskuc.CreateTaskUseCase
	updateUC     *taskuc.UpdateTaskUseCase
	deleteUC     *taskuc.DeleteTaskUseCase
	completeUC   *taskuc.CompleteTaskUseCase
	uncompleteUC *taskuc.UncompleteTaskUseCase
	listAllUC    *taskuc.ListAllTasksUseCase
	summaryUC    *taskuc.TaskSummaryUseCase
	calendarRepo calendar.CalendarRepository
}

func NewTaskHandler(
	listUC *taskuc.ListTasksUseCase,
	getUC *taskuc.GetTaskUseCase,
	createUC *taskuc.CreateTaskUseCase,
	updateUC *taskuc.UpdateTaskUseCase,
	deleteUC *taskuc.DeleteTaskUseCase,
	completeUC *taskuc.CompleteTaskUseCase,
	uncompleteUC *taskuc.UncompleteTaskUseCase,
	listAllUC *taskuc.ListAllTasksUseCase,
	summaryUC *taskuc.TaskSummaryUseCase,
	calendarRepo calendar.CalendarRepository,
) *TaskHandler {
	return &TaskHandler{
		listUC:       listUC,
		getUC:        getUC,
		createUC:     createUC,
		updateUC:     updateUC,
		deleteUC:     deleteUC,
		completeUC:   completeUC,
		uncompleteUC: uncompleteUC,
		listAllUC:    listAllUC,
		summaryUC:    summaryUC,
		calendarRepo: calendarRepo,
	}
}

// taskPermission returns the user's permission on the task's calendar and
// that calendar: the shared objectPermission with IsTask as the kind test, so
// an event's UUID is an unknown task.
func (h *TaskHandler) taskPermission(c fiber.Ctx, taskUUID string) (calendar.CalendarPermission, *calendar.Calendar, error) {
	return objectPermission(c, h.calendarRepo, taskUUID, (*calendar.CalendarObject).IsTask)
}

// calendarAccess resolves the URL's :calendar_id and the caller's permission
// on it, for the routes that address a task list: PermissionNone (and a nil
// calendar) for an unknown or unreadable one, which the caller answers with
// 404 "Calendar not found". The error is a storage failure, to be answered
// with accessError.
func (h *TaskHandler) calendarAccess(c fiber.Ctx) (*calendar.Calendar, calendar.CalendarPermission, error) {
	cal, err := resolveCalendar(c, h.calendarRepo)
	if err != nil || cal == nil {
		return nil, calendar.PermissionNone, err
	}
	perm, err := calendarPermission(c, h.calendarRepo, cal.ID)
	return cal, perm, err
}

// taskResponse renders a task of cal.
func taskResponse(task *taskuc.Task, cal *calendar.Calendar) dto.TaskResponse {
	return dto.NewTaskResponse(task.Object, task.Details, cal.UUID)
}

// taskServerError answers a failure the caller cannot fix: a generic 500, the
// cause in the server log only (F28).
func taskServerError(c fiber.Ctx, err error) error {
	slog.Error("task request failed",
		slog.String("method", c.Method()),
		slog.String("path", c.Path()),
		slog.Any("error", err),
	)
	return ErrorResponse(c, fiber.StatusInternalServerError, "Internal server error")
}

// writableTask runs the checks every write on an existing task (PATCH,
// DELETE, complete, uncomplete) shares, in
// this order, before any body or use case: the task must be a task of the
// URL's calendar that the caller can read (else 404 "Task not found", also
// for an event's UUID), the caller must be allowed to write that calendar
// (else 403; a read share or a subscribed calendar), and an If-Match header,
// when sent, must match the task's ETag (else 412 "ETag precondition
// failed"). It returns the calendar and true to go on, or false and the
// response the handler must return.
//
// The returned ifMatch is the ETag a given If-Match matched ("" without one);
// the use case re-checks it atomically with its write.
func (h *TaskHandler) writableTask(c fiber.Ctx, taskID string) (cal *calendar.Calendar, ifMatch string, ok bool, resp error) {
	perm, cal, err := h.taskPermission(c, taskID)
	if err != nil {
		return nil, "", false, accessError(c, err)
	}
	if perm == calendar.PermissionNone {
		return nil, "", false, ErrorResponse(c, fiber.StatusNotFound, taskNotFoundMessage)
	}
	if !canWrite(perm) {
		return nil, "", false, ErrorResponse(c, fiber.StatusForbidden, "You have read-only access to this calendar")
	}
	ifMatch, ok, resp = ifMatchETag(c, h.calendarRepo, taskID)
	if !ok {
		return nil, "", false, resp
	}
	return cal, ifMatch, true, nil
}

// taskWriteError answers a task write the use case refused: an object that is
// no longer a task of this calendar (a CalDAV PUT may have changed it since
// the check) is the 404 of an unknown task, a recurring task the 409 of
// ErrRecurringNotSupported, invalid input a 400, each with the error's own
// message; anything else is a server failure.
func taskWriteError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, taskuc.ErrNotFound):
		return ErrorResponse(c, fiber.StatusNotFound, taskNotFoundMessage)
	case errors.Is(err, taskuc.ErrPreconditionFailed):
		return ErrorResponse(c, fiber.StatusPreconditionFailed, "ETag precondition failed")
	case errors.Is(err, taskuc.ErrRecurringNotSupported):
		return ConflictResponse(c, err.Error())
	case errors.Is(err, taskuc.ErrInvalidInput):
		return BadRequestResponse(c, err.Error())
	}
	return taskServerError(c, err)
}

// GET /api/v1/calendars/:calendar_id/tasks
//
// Lists the tasks of one task list, readable by its owner and by every sharee
// (a subscribed calendar too). Query parameters:
//   - status: open (default: NEEDS-ACTION, IN-PROCESS), completed (done:
//     COMPLETED or CANCELLED) or all; anything else is 400.
//   - q: a case-insensitive substring of the summary or description, matched
//     literally (% and _ are not wildcards).
//   - limit (default 500, capped at 1000) and offset: not a non-negative
//     integer is 400.
//
// Answers 200 {"tasks":[TaskResponse…],"count":n,"total":n} in the server
// order: due date ascending (undated last), then priority (unset last), then
// creation. A recurring task is one row. A calendar that does not store tasks
// (e.g. a VEVENT-only calendar) answers 200 with no tasks.
func (h *TaskHandler) List(c fiber.Ctx) error {
	cal, perm, err := h.calendarAccess(c)
	if err != nil {
		return accessError(c, err)
	}
	if perm == calendar.PermissionNone {
		return ErrorResponse(c, fiber.StatusNotFound, calendarNotFoundMessage)
	}
	limit, offset, ok, resp := pageQuery(c)
	if !ok {
		return resp
	}

	out, err := h.listUC.Execute(c.Context(), taskuc.ListTasksInput{
		Calendar: cal,
		Status:   c.Query("status"),
		Query:    c.Query("q"),
		Limit:    limit,
		Offset:   offset,
	})
	if err != nil {
		if errors.Is(err, taskuc.ErrInvalidInput) {
			return BadRequestResponse(c, err.Error())
		}
		return taskServerError(c, err)
	}

	tasks := make([]dto.TaskResponse, len(out.Tasks))
	for i, task := range out.Tasks {
		tasks[i] = taskResponse(task, cal)
	}
	return c.JSON(dto.TaskListResponse{Tasks: tasks, Count: len(tasks), Total: out.Total})
}

// GET /api/v1/calendars/:calendar_id/tasks/:task_id
//
// Answers 200 TaskResponse, or 404 "Task not found" for an unknown or
// unreadable calendar, an unknown task, a task under another calendar and an
// event's UUID alike.
func (h *TaskHandler) Get(c fiber.Ctx) error {
	taskID := c.Params("task_id")
	perm, cal, err := h.taskPermission(c, taskID)
	if err != nil {
		return accessError(c, err)
	}
	if perm == calendar.PermissionNone {
		return ErrorResponse(c, fiber.StatusNotFound, taskNotFoundMessage)
	}
	task, err := h.getUC.Execute(c.Context(), taskuc.GetTaskInput{CalendarID: cal.ID, UUID: taskID})
	if err != nil {
		// It stopped being a task of this calendar after the check above (a
		// CalDAV PUT keeps the UUID): still an unknown task.
		if errors.Is(err, taskuc.ErrNotFound) {
			return ErrorResponse(c, fiber.StatusNotFound, taskNotFoundMessage)
		}
		return taskServerError(c, err)
	}
	return c.JSON(taskResponse(task, cal))
}

// POST /api/v1/calendars/:calendar_id/tasks
//
// Creates a task (a VTODO) in a task list from a dto.CreateTaskRequest and
// answers 201 with its TaskResponse. The calendar is resolved and authorised
// before the body is read: an unknown or unreadable calendar is 404 "Calendar
// not found", a read-only share or a subscribed calendar 403 "You have
// read-only access to this calendar". Then 400 for a body that does not bind
// ("Invalid request body"), for a calendar that does not store tasks ("This
// calendar does not store tasks") and for every field the use case refuses,
// with its message. There is no recurrence field.
func (h *TaskHandler) Create(c fiber.Ctx) error {
	cal, perm, err := h.calendarAccess(c)
	if err != nil {
		return accessError(c, err)
	}
	if perm == calendar.PermissionNone {
		return ErrorResponse(c, fiber.StatusNotFound, calendarNotFoundMessage)
	}
	if !canWrite(perm) {
		return ErrorResponse(c, fiber.StatusForbidden, "You have read-only access to this calendar")
	}
	var req dto.CreateTaskRequest
	if err := c.Bind().Body(&req); err != nil {
		return BadRequestResponse(c, "Invalid request body")
	}

	var reminders []calendar.ReminderInput
	if len(req.Reminders) > 0 {
		if reminders, err = dto.ToDomainReminders(req.Reminders); err != nil {
			return BadRequestResponse(c, err.Error())
		}
	}

	task, err := h.createUC.Execute(c.Context(), taskuc.CreateTaskInput{
		Reminders:       reminders,
		Calendar:        cal,
		Summary:         req.Summary,
		Description:     req.Description,
		Due:             req.Due,
		DueAllDay:       req.DueAllDay,
		Start:           req.Start,
		Priority:        req.Priority.Value,
		PercentComplete: req.PercentComplete.Value,
		Status:          req.Status,
		Timezone:        req.Timezone,
	})
	if err != nil {
		if errors.Is(err, taskuc.ErrInvalidInput) {
			return BadRequestResponse(c, err.Error())
		}
		return taskServerError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(taskResponse(task, cal))
}

// PATCH /api/v1/calendars/:calendar_id/tasks/:task_id
//
// Edits the task from a dto.UpdateTaskRequest and answers 200 with its
// TaskResponse. Only the fields sent change (null removes description, due,
// start, priority and percent_complete; an absent key keeps them), edited
// into the stored VTODO in place, so alarms, subtask links, repeat rules and
// other clients' properties survive; SEQUENCE, LAST-MODIFIED, the ETag and
// the calendar's sync token advance. A request that changes nothing answers
// the task unchanged, same ETag, nothing written. status completed and
// another status behave like complete and uncomplete; on a recurring task
// status completed or cancelled and percent_complete 100 are 409 "Repeating
// tasks cannot be completed here yet. Complete it in your task app.". The
// 404, 403 and 412 rules of DELETE come first, then 400 for a body that does
// not bind ("Invalid request body"), for a calendar that does not store tasks
// and for every field the use case refuses, with its message. There is no
// recurrence field and no list change.
func (h *TaskHandler) Update(c fiber.Ctx) error {
	taskID := c.Params("task_id")
	cal, ifMatch, ok, resp := h.writableTask(c, taskID)
	if !ok {
		return resp
	}
	var req dto.UpdateTaskRequest
	if err := c.Bind().Body(&req); err != nil {
		return BadRequestResponse(c, "Invalid request body")
	}
	var reminders *[]calendar.ReminderInput
	if req.Reminders.Set {
		list := []calendar.ReminderInput{}
		if req.Reminders.Value != nil {
			var err error
			if list, err = dto.ToDomainReminders(*req.Reminders.Value); err != nil {
				return BadRequestResponse(c, err.Error())
			}
		}
		reminders = &list
	}
	task, err := h.updateUC.Execute(c.Context(), taskuc.UpdateTaskInput{
		Reminders:       reminders,
		Calendar:        cal,
		UUID:            taskID,
		Summary:         req.Summary,
		Description:     taskuc.Change[string](req.Description),
		Due:             taskuc.Change[string](req.Due),
		DueAllDay:       req.DueAllDay,
		Start:           taskuc.Change[string](req.Start),
		Priority:        taskuc.Change[int](req.Priority),
		PercentComplete: taskuc.Change[int](req.PercentComplete),
		Status:          req.Status,
		Timezone:        req.Timezone,
		IfMatch:         ifMatch,
	})
	if err != nil {
		return taskWriteError(c, err)
	}
	return c.JSON(taskResponse(task, cal))
}

// DELETE /api/v1/calendars/:calendar_id/tasks/:task_id
//
// Deletes the task (a recurring one with all its occurrences) and answers 204
// No Content; the calendar's sync token advances and the deletion is in its
// change log, so DAV clients drop the task on their next sync. 404 "Task not
// found" for every miss (as GET), 403 "You have read-only access to this
// calendar" for a read share or a subscribed calendar, 412 "ETag precondition
// failed" when an If-Match header does not match the task's ETag.
func (h *TaskHandler) Delete(c fiber.Ctx) error {
	taskID := c.Params("task_id")
	cal, _, ok, resp := h.writableTask(c, taskID)
	if !ok {
		return resp
	}
	if err := h.deleteUC.Execute(c.Context(), taskuc.DeleteTaskInput{CalendarID: cal.ID, UUID: taskID}); err != nil {
		return taskWriteError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// POST /api/v1/calendars/:calendar_id/tasks/:task_id/complete
//
// Completes the task (STATUS:COMPLETED, COMPLETED now, PERCENT-COMPLETE:100,
// edited into the stored VTODO in place) and answers 200 with its
// TaskResponse, carrying the new ETag. Idempotent: an already completed task
// answers 200 unchanged, same ETag, nothing written. A request body is
// optional and ignored. 409 "Repeating tasks cannot be completed here yet.
// Complete it in your task app." for a recurring task; 400 "This calendar
// does not store tasks" for a task left in an events-only calendar; the 404,
// 403 and 412 rules of DELETE.
func (h *TaskHandler) Complete(c fiber.Ctx) error {
	taskID := c.Params("task_id")
	cal, ifMatch, ok, resp := h.writableTask(c, taskID)
	if !ok {
		return resp
	}
	task, err := h.completeUC.Execute(c.Context(), taskuc.CompleteTaskInput{Calendar: cal, UUID: taskID, IfMatch: ifMatch})
	if err != nil {
		return taskWriteError(c, err)
	}
	return c.JSON(taskResponse(task, cal))
}

// POST /api/v1/calendars/:calendar_id/tasks/:task_id/uncomplete
//
// Reopens a completed task (removes COMPLETED, STATUS:NEEDS-ACTION,
// PERCENT-COMPLETE 0 only when it was 100) and answers 200 with its
// TaskResponse. Idempotent: a task that is not completed answers 200
// unchanged, same ETag, nothing written. A request body is optional and
// ignored. A recurring task is not refused. 400, 404, 403 and 412 as for
// complete.
func (h *TaskHandler) Uncomplete(c fiber.Ctx) error {
	taskID := c.Params("task_id")
	cal, ifMatch, ok, resp := h.writableTask(c, taskID)
	if !ok {
		return resp
	}
	task, err := h.uncompleteUC.Execute(c.Context(), taskuc.CompleteTaskInput{Calendar: cal, UUID: taskID, IfMatch: ifMatch})
	if err != nil {
		return taskWriteError(c, err)
	}
	return c.JSON(taskResponse(task, cal))
}

// GET /api/v1/tasks
//
// Lists, in one page, the tasks of every task list the caller can read: their
// own (subscribed calendars included) and those shared with them, read-only
// or read-write. A calendar that does not store tasks (VEVENT-only) is not a
// task list and is skipped. Query parameters:
//   - status, q, limit (default 500, capped at 1000) and offset exactly as on
//     GET /api/v1/calendars/:calendar_id/tasks (bad status, limit, offset:
//     400).
//   - calendar_uuid: narrows to that one calendar. A calendar the caller
//     cannot read, or that does not exist, is 404 "Calendar not found" (never
//     an empty 200); a readable one that does not store tasks answers no
//     tasks, like its own list.
//   - due_after, due_before (RFC 3339, with Z or an offset; a "+" must be
//     sent as %2B, as in any query string): only tasks whose due date lies in
//     [due_after, due_before), compared as instants. A task without a due
//     date never matches a bound. Anything else is 400.
//
// Answers 200 {"tasks":[TaskResponse + calendar_name, calendar_color …],
// "count":n,"total":n} in the server order of a single list (due date, then
// priority, then creation; undated and unprioritised last), across lists.
// However many lists there are, it is one task query, not one per list.
func (h *TaskHandler) ListAll(c fiber.Ctx) error {
	userID, err := GetUserIDFromContext(c)
	if err != nil {
		// Only reachable if the route lost its Authenticate middleware.
		return UnauthorizedResponse(c, "missing authentication token")
	}
	limit, offset, ok, resp := pageQuery(c)
	if !ok {
		return resp
	}
	dueAfter, err := parseOptionalTime(c.Query("due_after"))
	if err != nil {
		return BadRequestResponse(c, "Invalid 'due_after' value")
	}
	dueBefore, err := parseOptionalTime(c.Query("due_before"))
	if err != nil {
		return BadRequestResponse(c, "Invalid 'due_before' value")
	}

	out, err := h.listAllUC.Execute(c.Context(), taskuc.ListAllTasksInput{
		UserID:       userID,
		CalendarUUID: c.Query("calendar_uuid"),
		Status:       c.Query("status"),
		Query:        c.Query("q"),
		DueAfter:     dueAfter,
		DueBefore:    dueBefore,
		Limit:        limit,
		Offset:       offset,
	})
	if err != nil {
		switch {
		case errors.Is(err, taskuc.ErrInvalidInput):
			return BadRequestResponse(c, err.Error())
		case errors.Is(err, taskuc.ErrCalendarNotFound):
			return ErrorResponse(c, fiber.StatusNotFound, calendarNotFoundMessage)
		}
		return taskServerError(c, err)
	}

	tasks := make([]dto.TaskWithCalendarResponse, len(out.Tasks))
	for i, listed := range out.Tasks {
		tasks[i] = dto.NewTaskWithCalendarResponse(listed.Object, listed.Details, listed.Calendar)
	}
	return c.JSON(dto.TaskWithCalendarListResponse{Tasks: tasks, Count: len(tasks), Total: out.Total})
}

// GET /api/v1/tasks/summary
//
// Counts the tasks of every task list the caller can read (the lists GET
// /api/v1/tasks covers) in the database, so the numbers hold however many
// tasks there are. Query parameter as_of (required): the user's current time
// in RFC 3339 WITH their UTC offset (e.g. 2026-11-03T15:00:00+01:00; send the
// "+" as %2B, as in any query string). That offset decides which date is
// today. Missing: 400 "Query parameter 'as_of' is required"; anything else
// that is not RFC 3339 (no offset, a date alone, an unencoded "+"): 400
// "Invalid 'as_of' value".
//
// Answers 200 {"open":n,"overdue":n,"due_today":n,"completed":n,
// "by_calendar":{"<calendar uuid>":{"open":n,"overdue":n,"completed":n}}}:
//   - open: needs-action and in-process tasks.
//   - overdue: open tasks due before as_of (timed; instants compared), or,
//     for an all-day task, due on a date before as_of's date in its offset
//     (an all-day task due today is not overdue).
//   - due_today: open tasks due on as_of's date in its offset; a timed task
//     due earlier today is overdue AND due today.
//   - completed: done tasks, completed and cancelled, so it equals the total
//     of GET /api/v1/tasks?status=completed. A cancelled task never counts as
//     open, overdue or due today.
//   - by_calendar: open, overdue and completed (done, as above) per task
//     list, keyed by calendar UUID, with an entry (zeros included) for every
//     readable task list.
//
// A floating due time (no Z, no TZID) is compared as UTC wall time.
func (h *TaskHandler) Summary(c fiber.Ctx) error {
	userID, err := GetUserIDFromContext(c)
	if err != nil {
		// Only reachable if the route lost its Authenticate middleware.
		return UnauthorizedResponse(c, "missing authentication token")
	}
	raw := c.Query("as_of")
	if raw == "" {
		return BadRequestResponse(c, "Query parameter 'as_of' is required")
	}
	asOf, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return BadRequestResponse(c, "Invalid 'as_of' value")
	}

	out, err := h.summaryUC.Execute(c.Context(), taskuc.TaskSummaryInput{UserID: userID, AsOf: asOf})
	if err != nil {
		return taskServerError(c, err)
	}
	return c.JSON(dto.NewTaskSummaryResponse(out.Total, out.ByCalendar))
}

// pageQuery reads the paging parameters of the task list routes, limit first:
// each is optional (0 when absent) and must be a non-negative integer, else
// the 400 "Invalid 'limit' value" / "Invalid 'offset' value". It returns them
// and true to go on, or false and the response the handler must return.
func pageQuery(c fiber.Ctx) (limit, offset int, ok bool, resp error) {
	if limit, ok = nonNegativeQueryInt(c, "limit"); !ok {
		return 0, 0, false, BadRequestResponse(c, "Invalid 'limit' value")
	}
	if offset, ok = nonNegativeQueryInt(c, "offset"); !ok {
		return 0, 0, false, BadRequestResponse(c, "Invalid 'offset' value")
	}
	return limit, offset, true, nil
}

// nonNegativeQueryInt reads an optional integer query parameter: 0 when
// absent, ok false when it is not a non-negative integer. A mistyped value is
// refused rather than defaulted: answering page 1 for a broken offset would
// look like a list that simply ran out.
func nonNegativeQueryInt(c fiber.Ctx, name string) (int, bool) {
	raw := c.Query(name)
	if raw == "" {
		return 0, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}
