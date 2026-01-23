package repository

import (
	"context"

	"github.com/jherrma/caldav-server/internal/domain/user"
	"gorm.io/gorm"
)

type GORMPasswordResetRepository struct {
	db *gorm.DB
}

func NewGORMPasswordResetRepository(db *gorm.DB) *GORMPasswordResetRepository {
	return &GORMPasswordResetRepository{db: db}
}

func (r *GORMPasswordResetRepository) Create(ctx context.Context, reset *user.PasswordReset) error {
	return r.db.WithContext(ctx).Create(reset).Error
}

func (r *GORMPasswordResetRepository) GetByHash(ctx context.Context, hash string) (*user.PasswordReset, error) {
	var reset user.PasswordReset
	err := r.db.WithContext(ctx).Preload("User").Where("token_hash = ?", hash).First(&reset).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &reset, nil
}

func (r *GORMPasswordResetRepository) DeleteByUserID(ctx context.Context, userID uint) error {
	return r.db.WithContext(ctx).Where("user_id = ?", userID).Delete(&user.PasswordReset{}).Error
}
