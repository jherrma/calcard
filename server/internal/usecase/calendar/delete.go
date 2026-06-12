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
	// Validate confirmation
	if req.Confirmation != "DELETE" {
		return fmt.Errorf("please type DELETE to confirm calendar deletion")
	}

	// Get calendar
	cal, err := uc.repo.GetByUUID(ctx, calendarUUID)
	if err != nil {
		return fmt.Errorf("calendar not found")
	}

	// Verify ownership
	if cal.UserID != userID {
		return fmt.Errorf("access denied")
	}

	// Check if this is the last calendar
	count, err := uc.repo.CountByUserID(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to count calendars: %w", err)
	}

	if count <= 1 {
		return fmt.Errorf("cannot delete your last calendar")
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
