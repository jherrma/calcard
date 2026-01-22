package repository

import (
	"context"
	"errors"

	"github.com/jherrma/caldav-server/internal/domain"
	"gorm.io/gorm"
)

type gormSystemSettingRepo struct {
	db *gorm.DB
}

// NewSystemSettingRepository creates a new GORM-based system setting repository
func NewSystemSettingRepository(db *gorm.DB) domain.SystemSettingRepository {
	return &gormSystemSettingRepo{db: db}
}

func (r *gormSystemSettingRepo) Get(ctx context.Context, key string) (string, error) {
	var s domain.SystemSetting
	if err := r.db.WithContext(ctx).Where("key = ?", key).First(&s).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}
	return s.Value, nil
}

func (r *gormSystemSettingRepo) Set(ctx context.Context, key, value string) error {
	s := domain.SystemSetting{
		Key:   key,
		Value: value,
	}
	return r.db.WithContext(ctx).Save(&s).Error
}
