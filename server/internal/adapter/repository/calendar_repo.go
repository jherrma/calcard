package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/sharing"
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
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(obj).Error; err != nil {
			return err
		}
		return r.recordChange(tx, obj.CalendarID, obj.Path, obj.UID, "created")
	})
}

// UpdateCalendarObject updates an existing calendar object
func (r *CalendarRepository) UpdateCalendarObject(ctx context.Context, obj *calendar.CalendarObject) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(obj).Error; err != nil {
			return err
		}
		return r.recordChange(tx, obj.CalendarID, obj.Path, obj.UID, "modified")
	})
}

// DeleteCalendarObject deletes a calendar object
func (r *CalendarRepository) DeleteCalendarObject(ctx context.Context, obj *calendar.CalendarObject) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&calendar.CalendarObject{}, obj.ID).Error; err != nil {
			return err
		}
		return r.recordChange(tx, obj.CalendarID, obj.Path, obj.UID, "deleted")
	})
}

// GetChangesSinceToken retrieves all changes to a calendar since a given sync token
func (r *CalendarRepository) GetChangesSinceToken(ctx context.Context, calendarID uint, token string) ([]*calendar.SyncChangeLog, error) {
	var changes []*calendar.SyncChangeLog
	query := r.db.WithContext(ctx).Where("calendar_id = ?", calendarID)
	if token != "" {
		// Find the ID of the sync change log entry with the given token
		var lastChange calendar.SyncChangeLog
		err := r.db.WithContext(ctx).Where("calendar_id = ? AND sync_token = ?", calendarID, token).First(&lastChange).Error
		if err == nil {
			query = query.Where("id > ?", lastChange.ID)
		} else if err != gorm.ErrRecordNotFound {
			return nil, err
		}
		// If token not found, RFC 6578 says we SHOULD return 403 Forbidden with valid-sync-token error.
		// For now, if we can't find the token, we'll return an error that the caller can handle.
		if err == gorm.ErrRecordNotFound {
			return nil, gorm.ErrRecordNotFound
		}
	}

	err := query.Order("id ASC").Find(&changes).Error
	return changes, err
}

// GetCalendarObjectByUUID retrieves a calendar object by UUID
func (r *CalendarRepository) GetCalendarObjectByUUID(ctx context.Context, uuid string) (*calendar.CalendarObject, error) {
	var obj calendar.CalendarObject
	err := r.db.WithContext(ctx).Where("uuid = ?", uuid).First(&obj).Error
	if err != nil {
		return nil, err
	}
	return &obj, nil
}

// ListEvents retrieves calendar objects within a time range
func (r *CalendarRepository) ListEvents(ctx context.Context, calendarID uint, start, end time.Time) ([]*calendar.CalendarObject, error) {
	var objects []*calendar.CalendarObject
	err := r.db.WithContext(ctx).
		Where("calendar_id = ?", calendarID).
		Where("start_time < ? AND end_time > ?", end, start).
		Order("start_time ASC, created_at ASC").
		Find(&objects).Error
	return objects, err
}

func (r *CalendarRepository) recordChange(tx *gorm.DB, calendarID uint, path, uid, changeType string) error {
	newToken := calendar.GenerateSyncToken()

	// Update calendar sync token and ctag
	if err := tx.Model(&calendar.Calendar{}).Where("id = ?", calendarID).Updates(map[string]interface{}{
		"sync_token": newToken,
		"ctag":       newToken,
	}).Error; err != nil {
		return err
	}

	// Record change
	return tx.Create(&calendar.SyncChangeLog{
		CalendarID:   calendarID,
		ResourcePath: path,
		ResourceUID:  uid,
		ChangeType:   changeType,
		SyncToken:    newToken,
	}).Error
}

// GetUserPermission determines a user's permission for a calendar
func (r *CalendarRepository) GetUserPermission(ctx context.Context, calendarID, userID uint) (calendar.CalendarPermission, error) {
	// Check ownership
	var cal calendar.Calendar
	if err := r.db.WithContext(ctx).First(&cal, calendarID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return calendar.PermissionNone, nil
		}
		return calendar.PermissionNone, err
	}

	if cal.UserID == userID {
		return calendar.PermissionOwner, nil
	}

	// Check share
	var share sharing.CalendarShare
	err := r.db.WithContext(ctx).Where("calendar_id = ? AND shared_with_id = ?", calendarID, userID).First(&share).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return calendar.PermissionNone, nil
		}
		return calendar.PermissionNone, err
	}

	if share.Permission == "read-write" {
		return calendar.PermissionReadWrite, nil
	}
	return calendar.PermissionRead, nil
}
