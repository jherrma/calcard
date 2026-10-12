package sharing

import (
	"context"
	"errors"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
	calendaruc "github.com/jherrma/caldav-server/internal/usecase/calendar"
)

// ErrCalendarShareNotFound is returned when the share addressed by the URL does
// not exist or belongs to a different calendar. The handler answers it with a
// 404, like any other unknown resource in the path.
var ErrCalendarShareNotFound = errors.New("share not found")

// ownedCalendar resolves the calendar a share request names: missing or not
// owned is calendar.ErrNotFound (404), a storage failure an internal error
// (500). Request problems are calendar.Invalidf errors (400).
func ownedCalendar(ctx context.Context, repo calendar.CalendarRepository, userID, calendarID uint) (*calendar.Calendar, error) {
	return calendaruc.LoadOwnedCalendarByID(ctx, repo, userID, calendarID)
}
