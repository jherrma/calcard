package event

import (
	"context"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

// loadEvent loads the object the get, update, delete and move use cases act on.
// They serve events only (#170): a task or any other non-event object is
// ErrNotFound, the answer an unknown event gets over REST and MCP, so it is
// never returned, rewritten, deleted or moved as an event. A failed lookup is
// returned as is.
func loadEvent(ctx context.Context, repo calendar.CalendarRepository, uuid string) (*calendar.CalendarObject, error) {
	obj, err := repo.GetCalendarObjectByUUID(ctx, uuid)
	if err != nil {
		return nil, err
	}
	if !obj.IsEvent() {
		return nil, ErrNotFound
	}
	return obj, nil
}

// requireEventCalendar refuses a write of an event into the calendar with the
// given id unless that calendar stores events (calendar.RequireComponent, the
// one component-set rule of #170). The refusal is an ErrInvalidInput whose
// message is exactly "This calendar does not store events", so REST answers 400
// with it and the MCP event tools return it; a failed lookup is returned as is
// (a server failure).
func requireEventCalendar(ctx context.Context, repo calendar.CalendarRepository, calendarID uint) error {
	cal, err := repo.GetByID(ctx, calendarID)
	if err != nil {
		return err
	}
	if err := calendar.RequireComponent(cal, calendar.ComponentEvent); err != nil {
		return invalidInput(err)
	}
	return nil
}
