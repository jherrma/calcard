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
	// A task is ErrNotFound (#170), checked before the target: the event route
	// never moves one, whatever the target stores.
	obj, err := loadEvent(ctx, uc.calendarRepo, input.EventUUID)
	if err != nil {
		return nil, err
	}
	// The target must store events (#170): a task list never gains one through
	// a move. Checked before the same-calendar shortcut, so the answer depends
	// on the target alone.
	if _, err := requireEventCalendar(ctx, uc.calendarRepo, input.TargetCalendarID); err != nil {
		return nil, err
	}

	// Capture the source calendar identity before reassigning, so we can tell
	// the source calendar's sync clients the event left.
	sourceID := obj.CalendarID
	if sourceID == input.TargetCalendarID {
		// Already in the target calendar; nothing to move.
		return obj, nil
	}

	obj.CalendarID = input.TargetCalendarID
	// MoveCalendarObject atomically records a "modified" change on the TARGET
	// calendar and a "deleted" change on the SOURCE calendar in one
	// transaction, so a partial failure can't leave a permanent sync ghost.
	if err := uc.calendarRepo.MoveCalendarObject(ctx, obj, sourceID); err != nil {
		return nil, err
	}

	return obj, nil
}
