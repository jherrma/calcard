package task

import (
	"context"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

// GetTaskInput names one task: the calendar the URL addresses and the task's
// UUID (the calendar object's UUID).
type GetTaskInput struct {
	CalendarID uint
	UUID       string
}

type GetTaskUseCase struct {
	calendarRepo calendar.CalendarRepository
}

func NewGetTaskUseCase(calendarRepo calendar.CalendarRepository) *GetTaskUseCase {
	return &GetTaskUseCase{calendarRepo: calendarRepo}
}

// Execute returns the task. An event under that UUID, or a task in another
// calendar, is ErrNotFound; the caller has already checked that it may read
// the calendar.
func (uc *GetTaskUseCase) Execute(ctx context.Context, input GetTaskInput) (*Task, error) {
	obj, err := loadTask(ctx, uc.calendarRepo, input.CalendarID, input.UUID)
	if err != nil {
		return nil, err
	}
	return newTask(obj), nil
}
