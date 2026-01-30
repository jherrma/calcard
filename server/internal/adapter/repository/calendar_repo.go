package repository

import (
	"context"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"gorm.io/gorm"
)

// CalendarRepository implements calendar.CalendarRepository using GORM
type CalendarRepository struct {
	db *gorm.DB
}

// NewCalendarRepository creates a new calendar repository
func NewCalendarRepository(db *gorm.DB) *CalendarRepository {
	return &CalendarRepository{db: db}
}

// Create creates a new calendar
func (r *CalendarRepository) Create(ctx context.Context, cal *calendar.Calendar) error {
	return r.db.WithContext(ctx).Create(cal).Error
}

// GetByID retrieves a calendar by its ID
func (r *CalendarRepository) GetByID(ctx context.Context, id uint) (*calendar.Calendar, error) {
	var cal calendar.Calendar
	err := r.db.WithContext(ctx).First(&cal, id).Error
	if err != nil {
		return nil, err
	}
	return &cal, nil
}

// GetByUUID retrieves a calendar by its UUID
func (r *CalendarRepository) GetByUUID(ctx context.Context, uuid string) (*calendar.Calendar, error) {
	var cal calendar.Calendar
	err := r.db.WithContext(ctx).Where("uuid = ?", uuid).First(&cal).Error
	if err != nil {
		return nil, err
	}
	return &cal, nil
}

// ListByUserID retrieves all calendars for a user
func (r *CalendarRepository) ListByUserID(ctx context.Context, userID uint) ([]*calendar.Calendar, error) {
	var calendars []*calendar.Calendar
	err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at ASC").
		Find(&calendars).Error
	return calendars, err
}

// Update updates an existing calendar
func (r *CalendarRepository) Update(ctx context.Context, cal *calendar.Calendar) error {
	return r.db.WithContext(ctx).Save(cal).Error
}

// Delete deletes a calendar by ID
func (r *CalendarRepository) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&calendar.Calendar{}, id).Error
}

// CountByUserID counts calendars for a user
func (r *CalendarRepository) CountByUserID(ctx context.Context, userID uint) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&calendar.Calendar{}).
		Where("user_id = ?", userID).
		Count(&count).Error
	return count, err
}

// GetEventCount returns the number of events in a calendar
func (r *CalendarRepository) GetEventCount(ctx context.Context, calendarID uint) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&calendar.CalendarObject{}).
		Where("calendar_id = ?", calendarID).
		Count(&count).Error
	return count, err
}

// GetByPath retrieves a calendar by user ID and path
func (r *CalendarRepository) GetByPath(ctx context.Context, userID uint, path string) (*calendar.Calendar, error) {
	var cal calendar.Calendar
	err := r.db.WithContext(ctx).Where("user_id = ? AND path = ?", userID, path).First(&cal).Error
	if err != nil {
		return nil, err
	}
	return &cal, nil
}

// GetCalendarObjects retrieves all calendar objects for a calendar
func (r *CalendarRepository) GetCalendarObjects(ctx context.Context, calendarID uint) ([]*calendar.CalendarObject, error) {
	var objects []*calendar.CalendarObject
	err := r.db.WithContext(ctx).
		Where("calendar_id = ?", calendarID).
		Order("start_time ASC, created_at ASC").
		Find(&objects).Error
	return objects, err
}

// GetCalendarObjectByPath retrieves a calendar object by calendar ID and path
func (r *CalendarRepository) GetCalendarObjectByPath(ctx context.Context, calendarID uint, path string) (*calendar.CalendarObject, error) {
	var obj calendar.CalendarObject
	err := r.db.WithContext(ctx).Where("calendar_id = ? AND path = ?", calendarID, path).First(&obj).Error
	if err != nil {
		return nil, err
	}
	return &obj, nil
}

// CreateCalendarObject creates a new calendar object
func (r *CalendarRepository) CreateCalendarObject(ctx context.Context, obj *calendar.CalendarObject) error {
	return r.db.WithContext(ctx).Create(obj).Error
}

// UpdateCalendarObject updates an existing calendar object
func (r *CalendarRepository) UpdateCalendarObject(ctx context.Context, obj *calendar.CalendarObject) error {
	return r.db.WithContext(ctx).Save(obj).Error
}

// DeleteCalendarObject deletes a calendar object by ID
func (r *CalendarRepository) DeleteCalendarObject(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&calendar.CalendarObject{}, id).Error
}
