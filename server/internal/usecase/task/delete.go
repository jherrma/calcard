package task

import (
	"context"
	"errors"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

// DeleteTaskInput names the task to delete: the calendar the URL addresses
// (the caller has checked it may write it) and the task's UUID.
type DeleteTaskInput struct {
	CalendarID uint
	UUID       string
}

// DeleteTaskUseCase deletes a task (DELETE …/tasks/:task_id).
type DeleteTaskUseCase struct {
	calendarRepo calendar.CalendarRepository
}

func NewDeleteTaskUseCase(calendarRepo calendar.CalendarRepository) *DeleteTaskUseCase {
	return &DeleteTaskUseCase{calendarRepo: calendarRepo}
}

// Execute deletes the whole task object, a recurring task with its overrides
// included, for good: HardDeleteCalendarObject removes the row (no soft-delete
// tombstone, unlike an event DELETE) and records the "deleted" change with the
// calendar's new sync token, which is how DAV clients learn of the removal.
// Anything but a task of the given calendar is ErrNotFound, and so is a task
// that is already gone when the delete runs (a concurrent DELETE won). A task
// in a calendar that no longer stores tasks can still be deleted.
func (uc *DeleteTaskUseCase) Execute(ctx context.Context, input DeleteTaskInput) error {
	obj, err := loadTask(ctx, uc.calendarRepo, input.CalendarID, input.UUID)
	if err != nil {
		return err
	}
	if err := uc.calendarRepo.HardDeleteCalendarObject(ctx, obj); err != nil {
		if errors.Is(err, calendar.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	return nil
}
