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
	return LoadOwnedCalendar(ctx, uc.repo, userID, calendarUUID)
}
