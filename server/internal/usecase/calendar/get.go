package calendar

import (
	"context"
	"fmt"

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
		return nil, fmt.Errorf("calendar not found")
	}

	// Verify ownership
	if cal.UserID != userID {
		return nil, fmt.Errorf("access denied")
	}

	return cal, nil
}
