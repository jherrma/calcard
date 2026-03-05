package calendar

import (
	"context"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/sharing"
)

// CalendarOwner contains basic owner info for shared calendars
type CalendarOwner struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

// CalendarWithEventCount represents a calendar with its event count
type CalendarWithEventCount struct {
	*calendar.Calendar
	EventCount int64          `json:"event_count"`
	Shared     bool           `json:"shared"`
	Owner      *CalendarOwner `json:"owner,omitempty"`
}

// ListCalendarsUseCase handles listing calendars
type ListCalendarsUseCase struct {
	repo      calendar.CalendarRepository
	shareRepo sharing.CalendarShareRepository
}

// NewListCalendarsUseCase creates a new use case
func NewListCalendarsUseCase(repo calendar.CalendarRepository, shareRepo sharing.CalendarShareRepository) *ListCalendarsUseCase {
	return &ListCalendarsUseCase{repo: repo, shareRepo: shareRepo}
}

// Execute lists all calendars for a user (owned + shared)
func (uc *ListCalendarsUseCase) Execute(ctx context.Context, userID uint) ([]*CalendarWithEventCount, error) {
	// Owned calendars
	calendars, err := uc.repo.ListByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	result := make([]*CalendarWithEventCount, 0, len(calendars))
	for _, cal := range calendars {
		eventCount, err := uc.repo.GetEventCount(ctx, cal.ID)
		if err != nil {
			eventCount = 0
		}

		result = append(result, &CalendarWithEventCount{
			Calendar:   cal,
			EventCount: eventCount,
			Shared:     false,
		})
	}

	// Shared calendars
	if uc.shareRepo != nil {
		shares, err := uc.shareRepo.FindCalendarsSharedWithUser(ctx, userID)
		if err != nil {
			return result, nil // Return owned calendars even if shares fail
		}

		for _, share := range shares {
			cal := share.Calendar
			eventCount, err := uc.repo.GetEventCount(ctx, cal.ID)
			if err != nil {
				eventCount = 0
			}

			result = append(result, &CalendarWithEventCount{
				Calendar:   &cal,
				EventCount: eventCount,
				Shared:     true,
				Owner: &CalendarOwner{
					ID:          cal.Owner.UUID,
					DisplayName: cal.Owner.DisplayName,
				},
			})
		}
	}

	return result, nil
}
