package repository

import (
	"context"
	"errors"

	"github.com/jherrma/caldav-server/internal/domain/user"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type gormAppPasswordRepo struct {
	db *gorm.DB
}

// NewAppPasswordRepository creates a new GORM-based app password repository
func NewAppPasswordRepository(db *gorm.DB) user.AppPasswordRepository {
	return &gormAppPasswordRepo{db: db}
}

func (r *gormAppPasswordRepo) Create(ctx context.Context, ap *user.AppPassword) error {
	return r.db.WithContext(ctx).Create(ap).Error
}

func (r *gormAppPasswordRepo) GetByUUID(ctx context.Context, uuid string) (*user.AppPassword, error) {
	var ap user.AppPassword
	if err := r.db.WithContext(ctx).Where("uuid = ? AND revoked_at IS NULL", uuid).First(&ap).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &ap, nil
}

func (r *gormAppPasswordRepo) ListByUserID(ctx context.Context, userID uint) ([]user.AppPassword, error) {
	var aps []user.AppPassword
	if err := r.db.WithContext(ctx).Where("user_id = ? AND revoked_at IS NULL", userID).Find(&aps).Error; err != nil {
		return nil, err
	}
	return aps, nil
}

func (r *gormAppPasswordRepo) Update(ctx context.Context, ap *user.AppPassword) error {
	return r.db.WithContext(ctx).Save(ap).Error
}

func (r *gormAppPasswordRepo) CountByUserID(ctx context.Context, userID uint) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&user.AppPassword{}).
		Where("user_id = ? AND revoked_at IS NULL", userID).
		Count(&count).Error
	return count, err
}

func (r *gormAppPasswordRepo) FindValidForUser(ctx context.Context, userID uint, password string) (*user.AppPassword, error) {
	var aps []user.AppPassword
	// We might have multiple app passwords, we need to check each one
	if err := r.db.WithContext(ctx).Where("user_id = ? AND revoked_at IS NULL", userID).Find(&aps).Error; err != nil {
		return nil, err
	}

	for _, ap := range aps {
		if err := bcrypt.CompareHashAndPassword([]byte(ap.PasswordHash), []byte(password)); err == nil {
			return &ap, nil
		}
	}

	return nil, nil
}
