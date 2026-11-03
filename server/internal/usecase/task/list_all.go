package task

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

// ListAllTasksInput selects one page of the tasks of every task list a user
// can read (GET /api/v1/tasks, #170).
type ListAllTasksInput struct {
	UserID uint
	// CalendarUUID, when not empty, narrows the query to the calendar with
	// that UUID. One the user cannot read (or that does not exist) is
	// ErrCalendarNotFound; a readable one that does not store tasks gives an
	// empty page, as its own task list does.
	CalendarUUID string
	// Status, Query, Limit and Offset as in ListTasksInput.
	Status string
	Query  string
	// DueAfter and DueBefore, when set, bound the due date to the half-open
	// range [DueAfter, DueBefore); a task without a due date never matches a
	// bound. Compared as instants (normalised to UTC).
	DueBefore, DueAfter *time.Time
	Limit, Offset       int
}

// ListedTask is a task of a cross-list page together with the calendar that
// holds it, so a row can be labelled and linked without another request.
type ListedTask struct {
	*Task
	Calendar calendar.ReadableCalendar
}

// ListAllTasksOutput is one page of tasks across lists in the server order of
// a single list (due date, then priority, then creation; undated and
// unprioritised last), and the number of matching tasks across all pages.
type ListAllTasksOutput struct {
	Tasks []*ListedTask
	Total int64
}

// ListAllTasksUseCase lists the tasks of every task list the user can read:
// their own (subscribed mirrors included) and those shared with them.
type ListAllTasksUseCase struct {
	calendarRepo calendar.CalendarRepository
}

func NewListAllTasksUseCase(calendarRepo calendar.CalendarRepository) *ListAllTasksUseCase {
	return &ListAllTasksUseCase{calendarRepo: calendarRepo}
}

// Execute answers one page in two queries whatever the number of lists: the
// readable calendars (ListReadableCalendarIDs — not the calendar list use
// case, which counts the events of every calendar) and one ListTasks over
// every readable calendar that stores tasks (calendar.SupportsComponent). A
// user without such a calendar gets an empty page and no task query. An
// invalid status is ErrInvalidInput before anything is read.
func (uc *ListAllTasksUseCase) Execute(ctx context.Context, input ListAllTasksInput) (*ListAllTasksOutput, error) {
	status, err := statusFilter(input.Status)
	if err != nil {
		return nil, err
	}
	readable, err := uc.calendarRepo.ListReadableCalendarIDs(ctx, input.UserID)
	if err != nil {
		return nil, err
	}

	ids := make([]uint, 0, len(readable))
	byID := make(map[uint]calendar.ReadableCalendar, len(readable))
	named := false
	for _, cal := range readable {
		if input.CalendarUUID != "" {
			if cal.UUID != input.CalendarUUID {
				continue
			}
			named = true
		}
		if !cal.SupportsComponent(calendar.ComponentToDo) {
			continue
		}
		ids = append(ids, cal.ID)
		byID[cal.ID] = cal
	}
	// The filter narrows the readable set and never widens it, so a UUID
	// outside it is unknown to this user. Answering an empty page instead
	// would read as "that list has no tasks".
	if input.CalendarUUID != "" && !named {
		return nil, ErrCalendarNotFound
	}

	out := &ListAllTasksOutput{Tasks: []*ListedTask{}}
	if len(ids) == 0 {
		return out, nil
	}
	objects, total, err := uc.calendarRepo.ListTasks(ctx, calendar.TaskQuery{
		CalendarIDs: ids,
		Status:      status,
		Text:        strings.TrimSpace(input.Query),
		DueBefore:   utcTime(input.DueBefore),
		DueAfter:    utcTime(input.DueAfter),
		Limit:       pageLimit(input.Limit),
		Offset:      max(input.Offset, 0),
	})
	if err != nil {
		return nil, err
	}
	for _, obj := range objects {
		cal, ok := byID[obj.CalendarID]
		if !ok {
			// The repository trusts CalendarIDs; a row from elsewhere would
			// be a bug that leaks another user's task, so fail loudly.
			return nil, fmt.Errorf("list all tasks: task %s of calendar %d is outside the readable calendars", obj.UUID, obj.CalendarID)
		}
		out.Tasks = append(out.Tasks, &ListedTask{Task: newTask(obj), Calendar: cal})
	}
	out.Total = total
	return out, nil
}

// utcTime returns a copy of t in UTC, nil for nil.
func utcTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	utc := t.UTC()
	return &utc
}
