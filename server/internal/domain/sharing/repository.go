package sharing

import (
	"context"
)

// CalendarShareRepository defines the interface for calendar share persistence
type CalendarShareRepository interface {
	Create(ctx context.Context, share *CalendarShare) error
	GetByUUID(ctx context.Context, uuid string) (*CalendarShare, error)
	ListByCalendarID(ctx context.Context, calendarID uint) ([]CalendarShare, error)
	FindCalendarsSharedWithUser(ctx context.Context, userID uint) ([]CalendarShare, error)
	Update(ctx context.Context, share *CalendarShare) error
	Revoke(ctx context.Context, id uint) error
	GetByCalendarAndUser(ctx context.Context, calendarID, userID uint) (*CalendarShare, error)
}
