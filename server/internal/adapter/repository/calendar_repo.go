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

// Create creates a new calendar. It mints the collection's initial sync token
// and writes a matching "collection" anchor row to the change log in the same
// transaction, so the token handed out by the very first sync-collection REPORT
// corresponds to a real change-log row. Without this anchor, the next
// incremental sync would be rejected with 403 valid-sync-token, forcing an
// endless full-resync loop on fresh calendars.
func (r *CalendarRepository) Create(ctx context.Context, cal *calendar.Calendar) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		token := calendar.GenerateSyncToken()
		cal.SyncToken = token
		cal.CTag = token
		if err := tx.Create(cal).Error; err != nil {
			return err
		}
		return tx.Create(&calendar.SyncChangeLog{
			CalendarID:   cal.ID,
			ResourcePath: "",
			ChangeType:   "collection",
			SyncToken:    token,
		}).Error
	})
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

// UpdateMetadata persists a rename and mints a new sync token atomically. It
// updates only the metadata columns via Select (so a Save of the whole struct
// can't write back the stale sync_token/ctag the caller loaded at request
// start, clobbering a token a concurrent object PUT just committed) and records
// the collection change in the same transaction (so a mid-rename failure can't
// leave the CTag un-bumped, hiding the new displayname from clients).
func (r *CalendarRepository) UpdateMetadata(ctx context.Context, cal *calendar.Calendar) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&calendar.Calendar{}).
			Where("id = ?", cal.ID).
			Select("name", "description", "color", "timezone").
			Updates(cal).Error; err != nil {
			return err
		}
		return r.recordChange(tx, cal.ID, "", "", "collection")
	})
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

// MoveCalendarObject reassigns an object to a new calendar and records both the
// target "modified" change and the source "deleted" change in a single
// transaction. obj.CalendarID must already point at the target calendar.
// Doing both sync-log writes atomically with the reassign prevents the
// permanent sync ghost that occurs if the source "deleted" change is lost
// after the reassign has already committed.
func (r *CalendarRepository) MoveCalendarObject(ctx context.Context, obj *calendar.CalendarObject, sourceCalendarID uint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(obj).Error; err != nil {
			return err
		}
		// Target collection sees the object arrive.
		if err := r.recordChange(tx, obj.CalendarID, obj.Path, obj.UID, "modified"); err != nil {
			return err
		}
		// Source collection sees the object leave.
		return r.recordChange(tx, sourceCalendarID, obj.Path, obj.UID, "deleted")
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

	if token == "" {
		// Initial sync (RFC 6578 §3.3): report the current members once each,
		// synthesized from live objects — NOT a replay of the raw change log
		// (which would duplicate hrefs and emit 404s for long-gone resources).
		var objs []*calendar.CalendarObject
		if err := r.db.WithContext(ctx).Where("calendar_id = ?", calendarID).Find(&objs).Error; err != nil {
			return nil, err
		}
		var cal calendar.Calendar
		if err := r.db.WithContext(ctx).First(&cal, calendarID).Error; err != nil {
			return nil, err
		}
		for _, obj := range objs {
			changes = append(changes, &calendar.SyncChangeLog{
				CalendarID:   calendarID,
				ResourcePath: obj.Path,
				ResourceUID:  obj.UID,
				ChangeType:   "created",
				SyncToken:    cal.SyncToken,
			})
		}
		return changes, nil
	}

	// "collection" anchor rows exist only to give freshly minted tokens a valid
	// change-log entry; they never represent a resource change, so exclude them
	// from the delta itself.
	query := r.db.WithContext(ctx).Where("calendar_id = ?", calendarID).Where("change_type <> ?", "collection")
	{
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

// GetCalendarObjectByUID looks up a calendar object by its iCalendar UID within
// a specific calendar (used for RFC 4791 no-uid-conflict detection on PUT).
// Returns (nil, nil) when not found.
func (r *CalendarRepository) GetCalendarObjectByUID(ctx context.Context, calendarID uint, uid string) (*calendar.CalendarObject, error) {
	var obj calendar.CalendarObject
	if err := r.db.WithContext(ctx).Where("calendar_id = ? AND uid = ?", calendarID, uid).First(&obj).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &obj, nil
}

// ListEvents retrieves calendar objects within a time range
func (r *CalendarRepository) ListEvents(ctx context.Context, calendarID uint, start, end time.Time) ([]*calendar.CalendarObject, error) {
	var objects []*calendar.CalendarObject
	// For recurring masters, end_time holds only the FIRST occurrence's end;
	// recurrence_end_time holds the last occurrence's end (far-future for
	// unbounded series). COALESCE keeps recurring events visible in windows
	// past their first occurrence while leaving non-recurring rows unchanged.
	err := r.db.WithContext(ctx).
		Where("calendar_id = ?", calendarID).
		Where("start_time < ? AND COALESCE(recurrence_end_time, end_time) > ?", end, start).
		Order("start_time ASC, created_at ASC").
		Find(&objects).Error
	return objects, err
}

// RecordChange advances the calendar's sync token and writes a matching
// change-log row, atomically. Used by mutations that don't go through
// Create/Update/DeleteCalendarObject — collection rename (change type
// "collection") and the source side of a cross-calendar move (change type
// "deleted").
func (r *CalendarRepository) RecordChange(ctx context.Context, calendarID uint, path, uid, changeType string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return r.recordChange(tx, calendarID, path, uid, changeType)
	})
}

// recordChange advances the collection's sync token and appends a changelog row
// inside tx.
//
// Known limitation (M20): under PostgreSQL, concurrent transactions can commit
// changelog rows out of monotonic ID order. A client running a sync REPORT in
// the window between two such commits can permanently miss the lower-ID change,
// because its next sync starts from the higher token it already observed. This
// is not mitigated here. A full fix would serialize changelog appends per
// collection (e.g. a Postgres advisory lock keyed on calendarID, or
// SELECT ... FOR UPDATE on the calendar row before inserting). SQLite is
// unaffected because writes are serialized.
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

// FindByPublicToken retrieves a calendar by its public token
func (r *CalendarRepository) FindByPublicToken(ctx context.Context, token string) (*calendar.Calendar, error) {
	var cal calendar.Calendar
	err := r.db.WithContext(ctx).Where("public_token = ? AND public_enabled = ?", token, true).First(&cal).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &cal, nil
}
