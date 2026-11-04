package task

import (
	"context"
	"errors"
	"strings"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

// DefaultLimit and MaxLimit bound one page of a task list: no limit (or 0)
// means DefaultLimit, anything above MaxLimit is capped to it. The web client
// groups and sorts on its side, so a page is large.
const (
	DefaultLimit = 500
	MaxLimit     = 1000
)

// ListTasksInput selects one page of a task list.
type ListTasksInput struct {
	// Calendar is the task list, already resolved and authorised by the
	// caller.
	Calendar *calendar.Calendar
	// Status is "open" (also when empty), "completed" or "all"; anything else
	// is ErrInvalidInput. Done means COMPLETED or CANCELLED.
	Status string
	// Query, trimmed, must occur in the summary or description
	// (case-insensitive, literally: LIKE wildcards are escaped). Empty matches
	// every task.
	Query string
	// Limit and Offset page the list; see DefaultLimit/MaxLimit. A negative
	// Offset is 0.
	Limit, Offset int
}

// ListTasksOutput is one page of tasks in the server order (due date, then
// priority, then creation; undated and unprioritised last) and the number of
// matching tasks across all pages.
type ListTasksOutput struct {
	Tasks []*Task
	Total int64
}

type ListTasksUseCase struct {
	calendarRepo calendar.CalendarRepository
}

func NewListTasksUseCase(calendarRepo calendar.CalendarRepository) *ListTasksUseCase {
	return &ListTasksUseCase{calendarRepo: calendarRepo}
}

// Execute lists the tasks of input.Calendar. A calendar that does not store
// tasks (calendar.SupportsComponent, e.g. a VEVENT-only calendar) answers an
// empty page without a query: it is not a task list.
func (uc *ListTasksUseCase) Execute(ctx context.Context, input ListTasksInput) (*ListTasksOutput, error) {
	status, err := statusFilter(input.Status)
	if err != nil {
		return nil, err
	}
	if input.Calendar == nil {
		return nil, errors.New("list tasks: no calendar given")
	}
	out := &ListTasksOutput{Tasks: []*Task{}}
	if !calendar.SupportsComponent(input.Calendar, calendar.ComponentToDo) {
		return out, nil
	}

	objects, total, err := uc.calendarRepo.ListTasks(ctx, calendar.TaskQuery{
		CalendarIDs: []uint{input.Calendar.ID},
		Status:      status,
		Text:        strings.TrimSpace(input.Query),
		Limit:       pageLimit(input.Limit),
		Offset:      max(input.Offset, 0),
	})
	if err != nil {
		return nil, err
	}
	for _, obj := range objects {
		out.Tasks = append(out.Tasks, newTask(obj))
	}
	out.Total = total
	return out, nil
}

// statusFilter validates the status query value: "" is open, the default.
// The values are a closed set compared exactly, like a calendar's kind.
func statusFilter(status string) (string, error) {
	switch status {
	case "":
		return calendar.TaskFilterOpen, nil
	case calendar.TaskFilterOpen, calendar.TaskFilterCompleted, calendar.TaskFilterAll:
		return status, nil
	}
	return "", invalidInputf(`status must be "open", "completed" or "all"`)
}

// pageLimit applies DefaultLimit and MaxLimit.
func pageLimit(limit int) int {
	if limit <= 0 {
		return DefaultLimit
	}
	return min(limit, MaxLimit)
}
