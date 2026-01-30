package calendar

import (
	"context"
	"time"
)

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

	// GetByPath retrieves a calendar by user ID and path
	GetByPath(ctx context.Context, userID uint, path string) (*Calendar, error)

	// GetCalendarObjectByPath retrieves a calendar object by calendar ID and path
	GetCalendarObjectByPath(ctx context.Context, calendarID uint, path string) (*CalendarObject, error)

	// CreateCalendarObject creates a new calendar object
	CreateCalendarObject(ctx context.Context, obj *CalendarObject) error

	// UpdateCalendarObject updates an existing calendar object
	UpdateCalendarObject(ctx context.Context, obj *CalendarObject) error

	// DeleteCalendarObject deletes a calendar object
	DeleteCalendarObject(ctx context.Context, obj *CalendarObject) error

	// GetChangesSinceToken retrieves all changes to a calendar since a given sync token
	GetChangesSinceToken(ctx context.Context, calendarID uint, token string) ([]*SyncChangeLog, error)

	// ListEvents retrieves calendar objects within a time range
	ListEvents(ctx context.Context, calendarID uint, start, end time.Time) ([]*CalendarObject, error)

	// GetCalendarObjectByUUID retrieves a calendar object by UUID
	GetCalendarObjectByUUID(ctx context.Context, uuid string) (*CalendarObject, error)
}
