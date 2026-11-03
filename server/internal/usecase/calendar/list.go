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

// CalendarWithEventCount represents a calendar with its event count. It is the
// shape of GET /api/v1/calendars, of the /search calendars group and of MCP
// list_calendars.
//
// There is deliberately no task count next to EventCount (#170): this list
// already runs one GetEventCount per calendar for every caller, so another
// COUNT would add a second N+1. Per-list open-task counts come from the task
// summary instead.
type CalendarWithEventCount struct {
	*calendar.Calendar
	// EventCount counts the calendar's events (VEVENT objects); tasks are
	// not counted.
	EventCount int64 `json:"event_count"`
	// Kind is "events", "tasks" or "both", derived from SupportedComponents
	// (calendar.KindOf) and read-only (#170).
	Kind       string         `json:"kind"`
	Shared     bool           `json:"shared"`
	Permission string         `json:"permission"` // "owner" for owned; "read" / "read-write" for shared
	Owner      *CalendarOwner `json:"owner,omitempty"`
}

// CalendarWithKind is a single calendar as the REST create, get and update
// responses render it: the stored row plus its derived, read-only kind
// (calendar.KindOf, #170), next to the unchanged supported_components.
type CalendarWithKind struct {
	*calendar.Calendar
	Kind string `json:"kind"`
}

// NewCalendarWithKind wraps cal with its kind.
func NewCalendarWithKind(cal *calendar.Calendar) *CalendarWithKind {
	return &CalendarWithKind{Calendar: cal, Kind: calendar.KindOf(cal)}
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
			Kind:       calendar.KindOf(cal),
			Shared:     false,
			Permission: "owner",
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
			// Defense in depth: skip a share whose calendar no longer exists
			// (zero-valued preload). CalendarRepository.Delete removes a
			// calendar's shares with it, but a stale row must never surface as
			// a blank ghost entry.
			if cal.ID == 0 {
				continue
			}
			eventCount, err := uc.repo.GetEventCount(ctx, cal.ID)
			if err != nil {
				eventCount = 0
			}

			result = append(result, &CalendarWithEventCount{
				Calendar:   &cal,
				EventCount: eventCount,
				Kind:       calendar.KindOf(&cal),
				Shared:     true,
				Permission: share.Permission,
				Owner: &CalendarOwner{
					ID:          cal.Owner.UUID,
					DisplayName: cal.Owner.DisplayName,
				},
			})
		}
	}

	return result, nil
}
