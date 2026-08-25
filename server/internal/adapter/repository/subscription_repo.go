package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"gorm.io/gorm"
)

// CalendarSubscriptionRepository implements
// calendar.CalendarSubscriptionRepository using GORM (story 100).
type CalendarSubscriptionRepository struct {
	db *gorm.DB
}

// NewCalendarSubscriptionRepository creates a new subscription repository.
func NewCalendarSubscriptionRepository(db *gorm.DB) *CalendarSubscriptionRepository {
	return &CalendarSubscriptionRepository{db: db}
}

func (r *CalendarSubscriptionRepository) Create(ctx context.Context, sub *calendar.CalendarSubscription) error {
	return r.db.WithContext(ctx).Create(sub).Error
}

// GetByUUID returns (nil, nil) when no subscription has that id, so callers can
// answer 404 without unwrapping a driver error.
func (r *CalendarSubscriptionRepository) GetByUUID(ctx context.Context, uuid string) (*calendar.CalendarSubscription, error) {
	var sub calendar.CalendarSubscription
	err := r.db.WithContext(ctx).Preload("Calendar").Where("uuid = ?", uuid).First(&sub).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &sub, nil
}

// GetByCalendarID returns (nil, nil) for a calendar that is not a subscription.
func (r *CalendarSubscriptionRepository) GetByCalendarID(ctx context.Context, calendarID uint) (*calendar.CalendarSubscription, error) {
	var sub calendar.CalendarSubscription
	err := r.db.WithContext(ctx).Where("calendar_id = ?", calendarID).First(&sub).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &sub, nil
}

func (r *CalendarSubscriptionRepository) ListByUserID(ctx context.Context, userID uint) ([]*calendar.CalendarSubscription, error) {
	var subs []*calendar.CalendarSubscription
	err := r.db.WithContext(ctx).
		Preload("Calendar").
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Find(&subs).Error
	return subs, err
}

func (r *CalendarSubscriptionRepository) CountByUserID(ctx context.Context, userID uint) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&calendar.CalendarSubscription{}).Where("user_id = ?", userID).Count(&count).Error
	return count, err
}

// Update persists the mutable columns explicitly rather than Save()ing the
// struct: the sync path reloads a row, mutates its status fields and writes it
// back, and a blanket Save would also rewrite CalendarID/UserID/CreatedAt from
// a struct that may have been read minutes earlier.
func (r *CalendarSubscriptionRepository) Update(ctx context.Context, sub *calendar.CalendarSubscription) error {
	return r.db.WithContext(ctx).Model(&calendar.CalendarSubscription{}).
		Where("id = ?", sub.ID).
		Updates(map[string]interface{}{
			"url":              sub.URL,
			"refresh_interval": sub.RefreshInterval,
			"next_sync_at":     sub.NextSyncAt,
			"last_synced_at":   sub.LastSyncedAt,
			"last_error":       sub.LastError,
			"error_count":      sub.ErrorCount,
			"enabled":          sub.Enabled,
			"etag":             sub.ETag,
			"last_modified":    sub.LastModified,
			"updated_at":       time.Now(),
		}).Error
}

func (r *CalendarSubscriptionRepository) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&calendar.CalendarSubscription{}, id).Error
}

// FindDue returns enabled subscriptions whose next attempt is due, oldest first
// so a backlog after downtime drains in the order it accumulated rather than
// starving the subscriptions that have waited longest.
func (r *CalendarSubscriptionRepository) FindDue(ctx context.Context, now time.Time, limit int) ([]*calendar.CalendarSubscription, error) {
	if limit <= 0 {
		limit = 50
	}
	var subs []*calendar.CalendarSubscription
	err := r.db.WithContext(ctx).
		Where("enabled = ? AND next_sync_at <= ?", true, now).
		Order("next_sync_at ASC").
		Limit(limit).
		Find(&subs).Error
	return subs, err
}
