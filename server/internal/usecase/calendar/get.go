package calendar

import (
	"context"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

// GetCalendarUseCase handles retrieving a single calendar
type GetCalendarUseCase struct {
	repo calendar.CalendarRepository
}

// NewGetCalendarUseCase creates a new use case
func NewGetCalendarUseCase(repo calendar.CalendarRepository) *GetCalendarUseCase {
	return &GetCalendarUseCase{repo: repo}
}

// Execute retrieves a calendar by UUID and verifies ownership
func (uc *GetCalendarUseCase) Execute(ctx context.Context, userID uint, calendarUUID string) (*calendar.Calendar, error) {
	cal, err := uc.repo.GetByUUID(ctx, calendarUUID)
	if err != nil {
		return nil, calendar.ErrNotFound
	}

	// Verify ownership. Not owned is the same error as missing (no existence leak).
	if cal.UserID != userID {
		return nil, calendar.ErrNotFound
	}

	return cal, nil
}
