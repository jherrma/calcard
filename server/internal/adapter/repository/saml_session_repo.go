package repository

import (
	"context"
	"errors"

	"github.com/jherrma/caldav-server/internal/domain/user"
	"gorm.io/gorm"
)

type gormSAMLSessionRepo struct {
	db *gorm.DB
}

// NewSAMLSessionRepository creates a new GORM-based SAML session repository
func NewSAMLSessionRepository(db *gorm.DB) user.SAMLSessionRepository {
	return &gormSAMLSessionRepo{db: db}
}

func (r *gormSAMLSessionRepo) Create(ctx context.Context, session *user.SAMLSession) error {
	return r.db.WithContext(ctx).Create(session).Error
}

func (r *gormSAMLSessionRepo) GetBySessionID(ctx context.Context, sessionID string) (*user.SAMLSession, error) {
	var session user.SAMLSession
	if err := r.db.WithContext(ctx).
		Where("session_id = ?", sessionID).
		First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &session, nil
}

func (r *gormSAMLSessionRepo) DeleteBySessionID(ctx context.Context, sessionID string) error {
	return r.db.WithContext(ctx).
		Where("session_id = ?", sessionID).
		Delete(&user.SAMLSession{}).Error
}

func (r *gormSAMLSessionRepo) DeleteByUserID(ctx context.Context, userID uint) error {
	return r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Delete(&user.SAMLSession{}).Error
}
