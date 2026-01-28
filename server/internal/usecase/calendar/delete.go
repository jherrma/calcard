package calendar

import (
	"context"
	"fmt"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

// DeleteCalendarRequest represents the request to delete a calendar
type DeleteCalendarRequest struct {
	Confirmation string `json:"confirmation"`
}

// DeleteCalendarUseCase handles calendar deletion
type DeleteCalendarUseCase struct {
	repo calendar.CalendarRepository
}

// NewDeleteCalendarUseCase creates a new use case
func NewDeleteCalendarUseCase(repo calendar.CalendarRepository) *DeleteCalendarUseCase {
	return &DeleteCalendarUseCase{repo: repo}
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

	// Delete calendar (cascade delete will handle events)
	if err := uc.repo.Delete(ctx, cal.ID); err != nil {
		return fmt.Errorf("failed to delete calendar: %w", err)
	}

	return nil
}
