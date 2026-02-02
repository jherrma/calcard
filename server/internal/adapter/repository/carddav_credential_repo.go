package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/user"
	"gorm.io/gorm"
)

type gormCardDAVCredentialRepo struct {
	db *gorm.DB
}

// NewCardDAVCredentialRepository creates a new GORM-based CardDAV credential repository
func NewCardDAVCredentialRepository(db *gorm.DB) user.CardDAVCredentialRepository {
	return &gormCardDAVCredentialRepo{db: db}
}

func (r *gormCardDAVCredentialRepo) Create(ctx context.Context, cred *user.CardDAVCredential) error {
	return r.db.WithContext(ctx).Create(cred).Error
}

func (r *gormCardDAVCredentialRepo) GetByUUID(ctx context.Context, uuid string) (*user.CardDAVCredential, error) {
	var cred user.CardDAVCredential
	if err := r.db.WithContext(ctx).Where("uuid = ?", uuid).First(&cred).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &cred, nil
}

func (r *gormCardDAVCredentialRepo) GetByUsername(ctx context.Context, username string) (*user.CardDAVCredential, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	var cred user.CardDAVCredential
	// Find credential that is NOT revoked. Expired ones are returned but validity checked by caller.
	if err := r.db.WithContext(ctx).Where("username = ? AND revoked_at IS NULL", username).First(&cred).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &cred, nil
}

func (r *gormCardDAVCredentialRepo) ListByUserID(ctx context.Context, userID uint) ([]user.CardDAVCredential, error) {
	var creds []user.CardDAVCredential
	if err := r.db.WithContext(ctx).Where("user_id = ? AND revoked_at IS NULL", userID).Find(&creds).Error; err != nil {
		return nil, err
	}
	return creds, nil
}

func (r *gormCardDAVCredentialRepo) Update(ctx context.Context, cred *user.CardDAVCredential) error {
	return r.db.WithContext(ctx).Save(cred).Error
}

func (r *gormCardDAVCredentialRepo) Revoke(ctx context.Context, id uint) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&user.CardDAVCredential{}).Where("id = ?", id).Update("revoked_at", now).Error
}

func (r *gormCardDAVCredentialRepo) UpdateLastUsed(ctx context.Context, id uint, ip string) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&user.CardDAVCredential{}).Where("id = ?", id).Updates(map[string]interface{}{
		"last_used_at": now,
		"last_used_ip": ip,
	}).Error
}
