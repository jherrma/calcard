package event

import (
	"context"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

type MoveEventInput struct {
	EventUUID        string
	TargetCalendarID uint
}

type MoveEventUseCase struct {
	calendarRepo calendar.CalendarRepository
}

func NewMoveEventUseCase(calendarRepo calendar.CalendarRepository) *MoveEventUseCase {
	return &MoveEventUseCase{calendarRepo: calendarRepo}
}

func (uc *MoveEventUseCase) Execute(ctx context.Context, input MoveEventInput) (*calendar.CalendarObject, error) {
	obj, err := uc.calendarRepo.GetCalendarObjectByUUID(ctx, input.EventUUID)
	if err != nil {
		return nil, err
	}

	obj.CalendarID = input.TargetCalendarID
	err = uc.calendarRepo.UpdateCalendarObject(ctx, obj)
	if err != nil {
		return nil, err
	}

	return obj, nil
}
