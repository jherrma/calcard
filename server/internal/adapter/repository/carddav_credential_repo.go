package repository

import (
	"context"
	"errors"
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

// Create stores a new credential. A username the unique index already holds
// yields user.ErrCredentialUsernameTaken, never the raw driver error.
func (r *gormCardDAVCredentialRepo) Create(ctx context.Context, cred *user.CardDAVCredential) error {
	err := r.db.WithContext(ctx).Create(cred).Error
	if err != nil && isUniqueViolation(r.db, err) {
		return user.ErrCredentialUsernameTaken
	}
	return err
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

// GetByUsername matches case-insensitively (see findByUsername): the username
// is stored exactly as the user chose it, upper-case letters included.
func (r *gormCardDAVCredentialRepo) GetByUsername(ctx context.Context, username string) (*user.CardDAVCredential, error) {
	// Find credential that is NOT revoked. Expired ones are returned but validity checked by caller.
	notRevoked := func(db *gorm.DB) *gorm.DB { return db.Where("revoked_at IS NULL") }
	return findByUsername[user.CardDAVCredential](r.db.WithContext(ctx), username, notRevoked)
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

// Revoke sets revoked_at and releases the username (rewritten to
// "revoked:<uuid>") so it can be used again (F24).
func (r *gormCardDAVCredentialRepo) Revoke(ctx context.Context, id uint) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&user.CardDAVCredential{}).Where("id = ?", id).Updates(map[string]interface{}{
		"revoked_at": now,
		"username":   revokedCredentialUsername,
	}).Error
}

func (r *gormCardDAVCredentialRepo) UpdateLastUsed(ctx context.Context, id uint, ip string) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&user.CardDAVCredential{}).Where("id = ?", id).Updates(map[string]interface{}{
		"last_used_at": now,
		"last_used_ip": ip,
	}).Error
}
