package task

import (
	"context"
	"errors"
	"time"

	"github.com/emersion/go-ical"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

// CompleteTaskInput names the task to complete or un-complete: the calendar
// the URL addresses, already resolved and authorised for writing by the
// caller, and the task's UUID (the calendar object's UUID).
type CompleteTaskInput struct {
	Calendar *calendar.Calendar
	UUID     string
	// IfMatch is the ETag the caller's If-Match matched ("" when it sent
	// none): the write happens only on a task that still has it, else
	// ErrPreconditionFailed.
	IfMatch string
}

// CompleteTaskUseCase ticks a task off (POST …/tasks/:task_id/complete).
type CompleteTaskUseCase struct {
	calendarRepo calendar.CalendarRepository
	now          func() time.Time
}

// NewCompleteTaskUseCase builds the use case. now is the clock for the
// timestamps it writes (COMPLETED, LAST-MODIFIED, DTSTAMP); nil means
// time.Now.
func NewCompleteTaskUseCase(calendarRepo calendar.CalendarRepository, now func() time.Time) *CompleteTaskUseCase {
	if now == nil {
		now = time.Now
	}
	return &CompleteTaskUseCase{calendarRepo: calendarRepo, now: now}
}

// Execute completes the task: STATUS:COMPLETED, COMPLETED at now (UTC) and
// PERCENT-COMPLETE:100, written into the stored VTODO in place (editTask), so
// nothing else a client stored on it changes; SEQUENCE, LAST-MODIFIED, the
// ETag and the calendar's sync token advance (UpdateCalendarObjectIfETag).
//
// In this order: anything but a task of input.Calendar is ErrNotFound; a
// calendar that does not store tasks is ErrInvalidInput "This calendar does
// not store tasks"; a recurring task (RRULE or RDATE on the master) is
// ErrRecurringNotSupported (spec §1c), also when it is already completed. A
// task that is already completed comes back unchanged with nothing written,
// so its ETag stays the same: completing is idempotent.
func (uc *CompleteTaskUseCase) Execute(ctx context.Context, input CompleteTaskInput) (*Task, error) {
	return writeTask(input.IfMatch, func() (*Task, error) { return uc.attempt(ctx, input) })
}

func (uc *CompleteTaskUseCase) attempt(ctx context.Context, input CompleteTaskInput) (*Task, error) {
	obj, err := loadWritableTask(ctx, uc.calendarRepo, input.Calendar, input.UUID)
	if err != nil {
		return nil, err
	}
	if err := checkIfMatch(obj, input.IfMatch); err != nil {
		return nil, err
	}
	loadedETag := obj.ETag
	task := newTask(obj)
	if task.Details.Recurring {
		return nil, ErrRecurringNotSupported
	}
	if obj.Status == calendar.TaskStatusCompleted {
		return task, nil
	}

	now := uc.now().UTC()
	if err := editTask(obj, now, func(_ *ical.Calendar, todo *ical.Component) error {
		markCompleted(todo, now)
		return nil
	}); err != nil {
		return nil, err
	}
	if err := storeTask(ctx, uc.calendarRepo, obj, loadedETag); err != nil {
		return nil, err
	}
	return newTask(obj), nil
}

// UncompleteTaskUseCase reopens a completed task (POST
// …/tasks/:task_id/uncomplete).
type UncompleteTaskUseCase struct {
	calendarRepo calendar.CalendarRepository
	now          func() time.Time
}

// NewUncompleteTaskUseCase builds the use case. now is the clock for the
// timestamps it writes (LAST-MODIFIED, DTSTAMP); nil means time.Now.
func NewUncompleteTaskUseCase(calendarRepo calendar.CalendarRepository, now func() time.Time) *UncompleteTaskUseCase {
	if now == nil {
		now = time.Now
	}
	return &UncompleteTaskUseCase{calendarRepo: calendarRepo, now: now}
}

// Execute reopens a completed task: it removes COMPLETED, sets
// STATUS:NEEDS-ACTION and resets PERCENT-COMPLETE to 0 only when it was 100
// (markUncompleted), in place like complete, with the same bookkeeping.
//
// Uncompleting is the inverse of completing and as idempotent: a task that is
// not completed (needs-action, in-process or cancelled) comes back unchanged
// with nothing written and the same ETag, so an in-process task keeps its
// status and a cancelled one stays cancelled. A recurring task is not refused:
// reopening only undoes a completion some client wrote on the master. The
// not-found and component-set rules are the ones of complete.
func (uc *UncompleteTaskUseCase) Execute(ctx context.Context, input CompleteTaskInput) (*Task, error) {
	return writeTask(input.IfMatch, func() (*Task, error) { return uc.attempt(ctx, input) })
}

func (uc *UncompleteTaskUseCase) attempt(ctx context.Context, input CompleteTaskInput) (*Task, error) {
	obj, err := loadWritableTask(ctx, uc.calendarRepo, input.Calendar, input.UUID)
	if err != nil {
		return nil, err
	}
	if err := checkIfMatch(obj, input.IfMatch); err != nil {
		return nil, err
	}
	loadedETag := obj.ETag
	if obj.Status != calendar.TaskStatusCompleted {
		return newTask(obj), nil
	}

	if err := editTask(obj, uc.now().UTC(), func(_ *ical.Calendar, todo *ical.Component) error {
		markUncompleted(todo)
		return nil
	}); err != nil {
		return nil, err
	}
	if err := storeTask(ctx, uc.calendarRepo, obj, loadedETag); err != nil {
		return nil, err
	}
	return newTask(obj), nil
}

// loadWritableTask loads the task an edit (complete, uncomplete, update)
// addresses: anything but a task of cal is ErrNotFound (loadTask), then a
// calendar that does not store VTODO refuses the write with ErrInvalidInput
// "This calendar does not store tasks", like create (a VTODO left in a
// calendar narrowed to events can still be read and deleted, not edited).
func loadWritableTask(ctx context.Context, repo calendar.CalendarRepository, cal *calendar.Calendar, uuid string) (*calendar.CalendarObject, error) {
	if cal == nil {
		return nil, errors.New("task write: no calendar given")
	}
	obj, err := loadTask(ctx, repo, cal.ID, uuid)
	if err != nil {
		return nil, err
	}
	if err := calendar.RequireComponent(cal, calendar.ComponentToDo); err != nil {
		return nil, invalidInput(err)
	}
	return obj, nil
}
