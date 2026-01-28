package calendar

import (
	"context"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

// CalendarWithEventCount represents a calendar with its event count
type CalendarWithEventCount struct {
	*calendar.Calendar
	EventCount int64 `json:"event_count"`
}

// ListCalendarsUseCase handles listing calendars
type ListCalendarsUseCase struct {
	repo calendar.CalendarRepository
}

// NewListCalendarsUseCase creates a new use case
func NewListCalendarsUseCase(repo calendar.CalendarRepository) *ListCalendarsUseCase {
	return &ListCalendarsUseCase{repo: repo}
}

// Execute lists all calendars for a user
func (uc *ListCalendarsUseCase) Execute(ctx context.Context, userID uint) ([]*CalendarWithEventCount, error) {
	calendars, err := uc.repo.ListByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	result := make([]*CalendarWithEventCount, len(calendars))
	for i, cal := range calendars {
		eventCount, err := uc.repo.GetEventCount(ctx, cal.ID)
		if err != nil {
			eventCount = 0 // Gracefully handle error
		}

		result[i] = &CalendarWithEventCount{
			Calendar:   cal,
			EventCount: eventCount,
		}
	}

	return result, nil
}
