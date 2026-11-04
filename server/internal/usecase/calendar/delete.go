package calendar

import (
	"context"
	"fmt"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/sharing"
)

// DeleteCalendarRequest represents the request to delete a calendar
type DeleteCalendarRequest struct {
	Confirmation string `json:"confirmation"`
}

// DeleteCalendarUseCase handles calendar deletion. REST and MCP's
// delete_calendar both run it.
type DeleteCalendarUseCase struct {
	repo calendar.CalendarRepository
}

// NewDeleteCalendarUseCase creates a new use case.
//
// The share repository is no longer used: CalendarRepository.Delete removes
// the calendar's shares in the same transaction as everything else in it. The
// parameter stays only so existing wiring keeps compiling; drop it together
// with its callers.
func NewDeleteCalendarUseCase(repo calendar.CalendarRepository, _ sharing.CalendarShareRepository) *DeleteCalendarUseCase {
	return &DeleteCalendarUseCase{repo: repo}
}

// Execute permanently deletes a calendar with confirmation.
func (uc *DeleteCalendarUseCase) Execute(ctx context.Context, userID uint, calendarUUID string, req DeleteCalendarRequest) error {
	// Resolve the calendar before looking at the request: an unknown or foreign
	// calendar is the same 404 whatever the body says (F28).
	cal, err := LoadOwnedCalendar(ctx, uc.repo, userID, calendarUUID)
	if err != nil {
		return err
	}

	// Validate confirmation
	if req.Confirmation != "DELETE" {
		return calendar.Invalidf("please type DELETE to confirm calendar deletion")
	}

	if err := uc.guardLastEventCalendar(ctx, userID, cal); err != nil {
		return err
	}

	// One transaction removes the calendar and everything in it: events,
	// change log, shares (so the calendar can't linger as a ghost entry in the
	// sharees' lists), subscription and public link.
	if err := uc.repo.Delete(ctx, cal.ID); err != nil {
		return fmt.Errorf("failed to delete calendar: %w", err)
	}
	return nil
}

// guardLastEventCalendar is the last-calendar guard, per kind (#170): it
// refuses to delete the user's last OWNED calendar that can hold events
// (calendar.SupportsComponent with VEVENT, so a mixed VEVENT,VTODO calendar and
// a legacy "" one count), so the user always keeps somewhere to put events. A
// calendar that cannot hold events, i.e. a VTODO-only task list, is never
// protected, the user's last one included (the Tasks page has an empty state
// for a user without one), and does not count as a calendar that remains.
//
// Only calendars the user owns count (ListByUserID returns owner rows only, and
// the UserID test keeps it so); one shared with them can be taken away by its
// owner at any time. A subscribed calendar is owned and stores VEVENT, so it
// counts, as it did when the guard counted every calendar.
func (uc *DeleteCalendarUseCase) guardLastEventCalendar(ctx context.Context, userID uint, target *calendar.Calendar) error {
	if !calendar.SupportsComponent(target, calendar.ComponentEvent) {
		return nil
	}

	owned, err := uc.repo.ListByUserID(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to list calendars: %w", err)
	}
	for _, other := range owned {
		if other != nil && other.UserID == userID && other.ID != target.ID &&
			calendar.SupportsComponent(other, calendar.ComponentEvent) {
			return nil
		}
	}
	return calendar.Invalidf("cannot delete your last calendar")
}
