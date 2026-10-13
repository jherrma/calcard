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

	// Check if this is the last calendar
	count, err := uc.repo.CountByUserID(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to count calendars: %w", err)
	}

	if count <= 1 {
		return calendar.Invalidf("cannot delete your last calendar")
	}

	// One transaction removes the calendar and everything in it: events,
	// change log, shares (so the calendar can't linger as a ghost entry in the
	// sharees' lists), subscription and public link.
	if err := uc.repo.Delete(ctx, cal.ID); err != nil {
		return fmt.Errorf("failed to delete calendar: %w", err)
	}
	return nil
}
