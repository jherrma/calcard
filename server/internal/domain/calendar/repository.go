package calendar

import "context"

// CalendarRepository defines the interface for calendar persistence
type CalendarRepository interface {
	// Create creates a new calendar
	Create(ctx context.Context, calendar *Calendar) error

	// GetByID retrieves a calendar by its ID
	GetByID(ctx context.Context, id uint) (*Calendar, error)

	// GetByUUID retrieves a calendar by its UUID
	GetByUUID(ctx context.Context, uuid string) (*Calendar, error)

	// ListByUserID retrieves all calendars for a user
	ListByUserID(ctx context.Context, userID uint) ([]*Calendar, error)

	// Update updates an existing calendar
	Update(ctx context.Context, calendar *Calendar) error

	// Delete deletes a calendar by ID
	Delete(ctx context.Context, id uint) error

	// CountByUserID counts calendars for a user
	CountByUserID(ctx context.Context, userID uint) (int64, error)

	// GetEventCount returns the number of events in a calendar
	GetEventCount(ctx context.Context, calendarID uint) (int64, error)

	// GetCalendarObjects retrieves all calendar objects (events/todos) for a calendar
	GetCalendarObjects(ctx context.Context, calendarID uint) ([]*CalendarObject, error)
}
