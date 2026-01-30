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

func (uc *GetEventUseCase) Execute(ctx context.Context, uuid string) (*calendar.CalendarObject, error) {
	return uc.calendarRepo.GetCalendarObjectByUUID(ctx, uuid)
}
