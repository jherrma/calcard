package calendar

import (
	"context"
	"errors"
	"fmt"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

// LoadOwnedCalendar resolves a calendar UUID for userID. A missing calendar and
// one owned by someone else are both calendar.ErrNotFound (no existence leak,
// F12); any other repository failure comes back wrapped, as an internal error,
// so it is never reported as "not found" (F28).
func LoadOwnedCalendar(ctx context.Context, repo calendar.CalendarRepository, userID uint, calendarUUID string) (*calendar.Calendar, error) {
	cal, err := repo.GetByUUID(ctx, calendarUUID)
	if errors.Is(err, calendar.ErrNotFound) {
		return nil, calendar.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load calendar: %w", err)
	}
	if cal == nil || cal.UserID != userID {
		return nil, calendar.ErrNotFound
	}
	return cal, nil
}

// LoadOwnedCalendarByID is LoadOwnedCalendar for the numeric id the share and
// public-link handlers have already resolved.
func LoadOwnedCalendarByID(ctx context.Context, repo calendar.CalendarRepository, userID, calendarID uint) (*calendar.Calendar, error) {
	cal, err := repo.GetByID(ctx, calendarID)
	if errors.Is(err, calendar.ErrNotFound) {
		return nil, calendar.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load calendar: %w", err)
	}
	if cal == nil || cal.UserID != userID {
		return nil, calendar.ErrNotFound
	}
	return cal, nil
}
