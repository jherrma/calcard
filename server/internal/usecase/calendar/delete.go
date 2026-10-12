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

// DeleteCalendarUseCase handles calendar deletion
type DeleteCalendarUseCase struct {
	repo      calendar.CalendarRepository
	shareRepo sharing.CalendarShareRepository
}

// NewDeleteCalendarUseCase creates a new use case. shareRepo may be nil in
// unit tests that don't exercise sharing.
func NewDeleteCalendarUseCase(repo calendar.CalendarRepository, shareRepo sharing.CalendarShareRepository) *DeleteCalendarUseCase {
	return &DeleteCalendarUseCase{repo: repo, shareRepo: shareRepo}
}

// Execute deletes a calendar with confirmation
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

	// Delete the calendar and its objects.
	if err := uc.repo.Delete(ctx, cal.ID); err != nil {
		return fmt.Errorf("failed to delete calendar: %w", err)
	}

	// Revoke every share of this calendar so it doesn't linger as a ghost
	// entry in the sharees' calendar lists (and so a future calendar can't
	// inherit a stale share).
	if uc.shareRepo != nil {
		if err := uc.shareRepo.DeleteByCalendarID(ctx, cal.ID); err != nil {
			return fmt.Errorf("failed to revoke calendar shares: %w", err)
		}
	}

	return nil
}
