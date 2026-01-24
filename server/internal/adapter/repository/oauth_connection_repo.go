package repository

import (
	"context"
	"errors"

	"github.com/jherrma/caldav-server/internal/domain/user"
	"gorm.io/gorm"
)

type gormOAuthConnectionRepo struct {
	db *gorm.DB
}

// NewOAuthConnectionRepository creates a new GORM-based OAuth connection repository
func NewOAuthConnectionRepository(db *gorm.DB) user.OAuthConnectionRepository {
	return &gormOAuthConnectionRepo{db: db}
}

func (r *gormOAuthConnectionRepo) Create(ctx context.Context, conn *user.OAuthConnection) error {
	return r.db.WithContext(ctx).Create(conn).Error
}

func (r *gormOAuthConnectionRepo) GetByProvider(ctx context.Context, userID uint, provider string) (*user.OAuthConnection, error) {
	var conn user.OAuthConnection
	if err := r.db.WithContext(ctx).
		Where("user_id = ? AND provider = ?", userID, provider).
		First(&conn).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &conn, nil
}

func (r *gormOAuthConnectionRepo) ListByUserID(ctx context.Context, userID uint) ([]user.OAuthConnection, error) {
	var conns []user.OAuthConnection
	if err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Find(&conns).Error; err != nil {
		return nil, err
	}
	return conns, nil
}

func (r *gormOAuthConnectionRepo) Update(ctx context.Context, conn *user.OAuthConnection) error {
	return r.db.WithContext(ctx).Save(conn).Error
}

func (r *gormOAuthConnectionRepo) Delete(ctx context.Context, userID uint, provider string) error {
	return r.db.WithContext(ctx).
		Where("user_id = ? AND provider = ?", userID, provider).
		Delete(&user.OAuthConnection{}).Error
}
