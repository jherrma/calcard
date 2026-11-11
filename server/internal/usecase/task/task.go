// Package task holds the task (VTODO) use cases behind the REST task routes
// (#170). It mirrors usecase/event: the use cases depend on the domain's
// CalendarRepository only, and permission checks stay in the handlers.
package task

import (
	"context"
	"errors"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"gorm.io/gorm"
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
// different calendar, and so is a task deleted since the caller's permission
// check (the repository's record-not-found). Any other failed lookup is
// returned as is.
func loadTask(ctx context.Context, repo calendar.CalendarRepository, calendarID uint, uuid string) (*calendar.CalendarObject, error) {
	obj, err := repo.GetCalendarObjectByUUID(ctx, uuid)
	if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, calendar.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if !obj.IsTask() || obj.CalendarID != calendarID {
		return nil, ErrNotFound
	}
	return obj, nil
}

// maxWriteAttempts bounds how often an edit without an If-Match is redone on
// the task as another writer changed it meanwhile.
const maxWriteAttempts = 3

// writeTask runs one load-edit-store attempt of a task write. A conditional
// store (storeTask) that finds the task changed under it is
// calendar.ErrETagMismatch. With an If-Match (ifMatch is the ETag it named)
// that is ErrPreconditionFailed; without one the attempt is redone on the
// fresh task, a few times, so unconditional writes keep working under load.
func writeTask(ifMatch string, attempt func() (*Task, error)) (*Task, error) {
	for i := 0; i < maxWriteAttempts; i++ {
		task, err := attempt()
		if !errors.Is(err, calendar.ErrETagMismatch) {
			return task, err
		}
		if ifMatch != "" {
			break
		}
	}
	return nil, ErrPreconditionFailed
}

// checkIfMatch is the atomic half of the handler's If-Match check: the task
// as just loaded must still carry the ETag the precondition matched.
func checkIfMatch(obj *calendar.CalendarObject, ifMatch string) error {
	if ifMatch != "" && obj.ETag != ifMatch {
		return ErrPreconditionFailed
	}
	return nil
}

// storeTask writes the edited obj over the row it was loaded as (loadedETag),
// never resurrecting a task deleted meanwhile: that is ErrNotFound.
func storeTask(ctx context.Context, repo calendar.CalendarRepository, obj *calendar.CalendarObject, loadedETag string) error {
	err := repo.UpdateCalendarObjectIfETag(ctx, obj, loadedETag)
	if errors.Is(err, calendar.ErrNotFound) {
		return ErrNotFound
	}
	return err
}
