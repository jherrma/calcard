package task

import (
	"context"
	"fmt"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

// TaskSummaryInput asks for the task counts of a user (GET
// /api/v1/tasks/summary, #170).
type TaskSummaryInput struct {
	UserID uint
	// AsOf is the user's "now", written with their UTC offset: that offset
	// decides which date is today (calendar.TaskDayOf).
	AsOf time.Time
}

// TaskSummary is the counts of every task list the user can read, in total
// and per list, keyed by the list's calendar UUID. Every readable task list
// has an entry, one without tasks too (zero counts), so a client can show a
// count for each list it knows. calendar.TaskCount says what is counted.
type TaskSummary struct {
	Total      calendar.TaskCount
	ByCalendar map[string]calendar.TaskCount
}

// TaskSummaryUseCase counts the tasks of every task list a user can read:
// their own (subscribed mirrors included) and those shared with them, the
// same lists GET /api/v1/tasks covers.
type TaskSummaryUseCase struct {
	calendarRepo calendar.CalendarRepository
}

func NewTaskSummaryUseCase(calendarRepo calendar.CalendarRepository) *TaskSummaryUseCase {
	return &TaskSummaryUseCase{calendarRepo: calendarRepo}
}

// Execute answers in two queries whatever the number of lists and tasks: the
// readable calendars (ListReadableCalendarIDs, never the calendar list use
// case, which counts the events of every calendar) and one CountTasks over
// every readable calendar that stores tasks (calendar.SupportsComponent). The
// counts are taken in the database, so they hold however many tasks there are
// (a list page stops at MaxLimit). A user without a task list gets zero
// counts and no count query.
func (uc *TaskSummaryUseCase) Execute(ctx context.Context, input TaskSummaryInput) (*TaskSummary, error) {
	readable, err := uc.calendarRepo.ListReadableCalendarIDs(ctx, input.UserID)
	if err != nil {
		return nil, err
	}

	ids, lists := taskListsByID(readable)
	out := &TaskSummary{ByCalendar: make(map[string]calendar.TaskCount, len(lists))}
	for _, list := range lists {
		out.ByCalendar[list.UUID] = calendar.TaskCount{}
	}
	if len(ids) == 0 {
		return out, nil
	}

	counts, err := uc.calendarRepo.CountTasks(ctx, ids, input.AsOf)
	if err != nil {
		return nil, err
	}
	for id, count := range counts.ByCalendar {
		list, ok := lists[id]
		if !ok {
			// The repository trusts the ids it is given; counts of another
			// calendar would be a bug that leaks another user's numbers.
			return nil, fmt.Errorf("task summary: counts of calendar %d, which is outside the readable calendars", id)
		}
		out.ByCalendar[list.UUID] = count
	}
	out.Total = counts.Total
	return out, nil
}
