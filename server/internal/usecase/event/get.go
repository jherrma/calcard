package event

import (
	"context"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

type GetEventUseCase struct {
	calendarRepo calendar.CalendarRepository
}

func NewGetEventUseCase(calendarRepo calendar.CalendarRepository) *GetEventUseCase {
	return &GetEventUseCase{calendarRepo: calendarRepo}
}

// Execute returns the event with the given UUID; a task under that UUID is
// ErrNotFound (#170).
func (uc *GetEventUseCase) Execute(ctx context.Context, uuid string) (*calendar.CalendarObject, error) {
	return loadEvent(ctx, uc.calendarRepo, uuid)
}
