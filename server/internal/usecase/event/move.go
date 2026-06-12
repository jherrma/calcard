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

	// Capture the source calendar identity before reassigning, so we can tell
	// the source calendar's sync clients the event left.
	sourceID, srcPath, srcUID := obj.CalendarID, obj.Path, obj.UID

	obj.CalendarID = input.TargetCalendarID
	// UpdateCalendarObject records a "modified" change on the TARGET calendar.
	err = uc.calendarRepo.UpdateCalendarObject(ctx, obj)
	if err != nil {
		return nil, err
	}

	// Record a "deleted" change on the source calendar so clients syncing it
	// drop the stale copy instead of keeping it forever.
	if sourceID != input.TargetCalendarID {
		if err := uc.calendarRepo.RecordChange(ctx, sourceID, srcPath, srcUID, "deleted"); err != nil {
			return nil, err
		}
	}

	return obj, nil
}
