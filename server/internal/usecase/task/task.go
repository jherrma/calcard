// Package task holds the task (VTODO) use cases behind the REST task routes
// (#170). It mirrors usecase/event: the use cases depend on the domain's
// CalendarRepository only, and permission checks stay in the handlers.
package task

import (
	"context"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

// Task is one stored task as the task use cases return it: the object, whose
// denormalized columns hold status, due date, completion, percent and
// priority, plus the TaskDetails that have no column (DTSTART's value type,
// the recurrence), decoded once here.
type Task struct {
	Object  *calendar.CalendarObject
	Details calendar.TaskDetails
}

func newTask(obj *calendar.CalendarObject) *Task {
	return &Task{Object: obj, Details: obj.TaskDetails()}
}

// loadTask loads the task with the given UUID in the given calendar. Anything
// else under that UUID is ErrNotFound: an event (a CalDAV PUT may also have
// turned the task into one since the handler looked), or a task filed under a
// different calendar. A failed lookup is returned as is.
func loadTask(ctx context.Context, repo calendar.CalendarRepository, calendarID uint, uuid string) (*calendar.CalendarObject, error) {
	obj, err := repo.GetCalendarObjectByUUID(ctx, uuid)
	if err != nil {
		return nil, err
	}
	if !obj.IsTask() || obj.CalendarID != calendarID {
		return nil, ErrNotFound
	}
	return obj, nil
}
