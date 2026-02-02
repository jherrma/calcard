package repository

import (
	"context"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/user"
	"gorm.io/gorm"
)

// CalDAVCredentialRepository implements user.CalDAVCredentialRepository
type CalDAVCredentialRepository struct {
	db *gorm.DB
}

// NewCalDAVCredentialRepository creates a new CalDAVCredentialRepository
func NewCalDAVCredentialRepository(db *gorm.DB) *CalDAVCredentialRepository {
	return &CalDAVCredentialRepository{db: db}
}

// Create creates a new CalDAV credential
func (r *CalDAVCredentialRepository) Create(ctx context.Context, cred *user.CalDAVCredential) error {
	return r.db.WithContext(ctx).Create(cred).Error
}

// GetByUUID retrieves a CalDAV credential by UUID
func (r *CalDAVCredentialRepository) GetByUUID(ctx context.Context, uuid string) (*user.CalDAVCredential, error) {
	var cred user.CalDAVCredential
	if err := r.db.WithContext(ctx).Where("uuid = ? AND revoked_at IS NULL", uuid).First(&cred).Error; err != nil {
		return nil, err
	}
	return &cred, nil
}

// GetByUsername retrieves a CalDAV credential by username
func (r *CalDAVCredentialRepository) GetByUsername(ctx context.Context, username string) (*user.CalDAVCredential, error) {
	var cred user.CalDAVCredential
	if err := r.db.WithContext(ctx).Where("username = ? AND revoked_at IS NULL", username).First(&cred).Error; err != nil {
		return nil, err
	}
	return &cred, nil
}

// ListByUserID lists all CalDAV credentials for a user
func (r *CalDAVCredentialRepository) ListByUserID(ctx context.Context, userID uint) ([]user.CalDAVCredential, error) {
	var creds []user.CalDAVCredential
	if err := r.db.WithContext(ctx).Where("user_id = ? AND revoked_at IS NULL", userID).Order("created_at DESC").Find(&creds).Error; err != nil {
		return nil, err
	}
	return creds, nil
}

// Revoke soft-deletes a CalDAV credential by setting revoked_at
func (r *CalDAVCredentialRepository) Revoke(ctx context.Context, id uint) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&user.CalDAVCredential{}).Where("id = ?", id).Update("revoked_at", now).Error
}

// UpdateLastUsed updates the last used timestamp and IP
func (r *CalDAVCredentialRepository) UpdateLastUsed(ctx context.Context, id uint, ip string) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&user.CalDAVCredential{}).Where("id = ?", id).Updates(map[string]interface{}{
		"last_used_at": now,
		"last_used_ip": ip,
	}).Error
}
