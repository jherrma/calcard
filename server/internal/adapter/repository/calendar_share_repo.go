package repository

import (
	"context"
	"errors"

	"github.com/jherrma/caldav-server/internal/domain/sharing"
	"gorm.io/gorm"
)

type gormCalendarShareRepo struct {
	db *gorm.DB
}

// NewCalendarShareRepository creates a new GORM-based CalendarShare repository
func NewCalendarShareRepository(db *gorm.DB) sharing.CalendarShareRepository {
	return &gormCalendarShareRepo{db: db}
}

func (r *gormCalendarShareRepo) Create(ctx context.Context, share *sharing.CalendarShare) error {
	return r.db.WithContext(ctx).Create(share).Error
}

func (r *gormCalendarShareRepo) GetByUUID(ctx context.Context, uuid string) (*sharing.CalendarShare, error) {
	var share sharing.CalendarShare
	if err := r.db.WithContext(ctx).Where("uuid = ?", uuid).Preload("SharedWith").First(&share).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &share, nil
}

func (r *gormCalendarShareRepo) ListByCalendarID(ctx context.Context, calendarID uint) ([]sharing.CalendarShare, error) {
	var shares []sharing.CalendarShare
	if err := r.db.WithContext(ctx).Where("calendar_id = ?", calendarID).Preload("SharedWith").Find(&shares).Error; err != nil {
		return nil, err
	}
	return shares, nil
}

func (r *gormCalendarShareRepo) FindCalendarsSharedWithUser(ctx context.Context, userID uint) ([]sharing.CalendarShare, error) {
	var shares []sharing.CalendarShare
	if err := r.db.WithContext(ctx).Where("shared_with_id = ?", userID).Preload("Calendar").Preload("Calendar.Owner").Find(&shares).Error; err != nil {
		return nil, err
	}
	return shares, nil
}

func (r *gormCalendarShareRepo) Update(ctx context.Context, share *sharing.CalendarShare) error {
	return r.db.WithContext(ctx).Save(share).Error
}

func (r *gormCalendarShareRepo) Revoke(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&sharing.CalendarShare{}, id).Error
}

func (r *gormCalendarShareRepo) GetByCalendarAndUser(ctx context.Context, calendarID, userID uint) (*sharing.CalendarShare, error) {
	var share sharing.CalendarShare
	if err := r.db.WithContext(ctx).Where("calendar_id = ? AND shared_with_id = ?", calendarID, userID).First(&share).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &share, nil
}
