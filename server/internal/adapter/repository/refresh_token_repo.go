package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/user"
	"gorm.io/gorm"
)

type gormRefreshTokenRepo struct {
	db *gorm.DB
}

// NewRefreshTokenRepository creates a new GORM-based refresh token repository
func NewRefreshTokenRepository(db *gorm.DB) user.RefreshTokenRepository {
	return &gormRefreshTokenRepo{db: db}
}

func (r *gormRefreshTokenRepo) Create(ctx context.Context, t *user.RefreshToken) error {
	return r.db.WithContext(ctx).Create(t).Error
}

func (r *gormRefreshTokenRepo) GetByHash(ctx context.Context, hash string) (*user.RefreshToken, error) {
	// Automatically delete expired tokens
	if err := r.db.WithContext(ctx).Where("expires_at < ?", time.Now()).Delete(&user.RefreshToken{}).Error; err != nil {
		// Log error but don't fail the request
		fmt.Printf("failed to cleanup expired refresh tokens: %v\n", err)
	}

	var t user.RefreshToken
	if err := r.db.WithContext(ctx).Preload("User").Where("token_hash = ?", hash).First(&t).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &t, nil
}

func (r *gormRefreshTokenRepo) DeleteByHash(ctx context.Context, hash string) error {
	return r.db.WithContext(ctx).Where("token_hash = ?", hash).Delete(&user.RefreshToken{}).Error
}

func (r *gormRefreshTokenRepo) DeleteByUserID(ctx context.Context, userID uint) error {
	return r.db.WithContext(ctx).Where("user_id = ?", userID).Delete(&user.RefreshToken{}).Error
}
