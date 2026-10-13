package repository

import (
	"context"
	"strings"
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

// Create creates a new CalDAV credential. A username the unique index already
// holds yields user.ErrCredentialUsernameTaken, never the raw driver error.
func (r *CalDAVCredentialRepository) Create(ctx context.Context, cred *user.CalDAVCredential) error {
	err := r.db.WithContext(ctx).Create(cred).Error
	if err != nil && isUniqueViolation(r.db, err) {
		return user.ErrCredentialUsernameTaken
	}
	return err
}

// GetByUUID retrieves a CalDAV credential by UUID
func (r *CalDAVCredentialRepository) GetByUUID(ctx context.Context, uuid string) (*user.CalDAVCredential, error) {
	var cred user.CalDAVCredential
	if err := r.db.WithContext(ctx).Where("uuid = ? AND revoked_at IS NULL", uuid).First(&cred).Error; err != nil {
		return nil, err
	}
	return &cred, nil
}

// GetByUsername retrieves a live CalDAV credential by username, in any case.
// The case is folded in Go, never in SQL: a new credential's username can
// only be lowercase letters (user.ValidateDAVCredentialUsername), so the
// lower-cased input is its stored spelling whatever case a client sends.
// The spelling as sent is tried first, for credentials created before that
// rule (up to v1.0.0-rc3 upper case was accepted and stored as given): they
// keep matching exactly as stored. A miss is gorm.ErrRecordNotFound.
func (r *CalDAVCredentialRepository) GetByUsername(ctx context.Context, username string) (*user.CalDAVCredential, error) {
	username = strings.TrimSpace(username)
	spellings := []string{username}
	if lower := strings.ToLower(username); lower != username {
		spellings = append(spellings, lower)
	}
	for _, name := range spellings {
		// Find, not First: a miss is expected here and must not log "record not found".
		var creds []user.CalDAVCredential
		if err := r.db.WithContext(ctx).Where("username = ? AND revoked_at IS NULL", name).
			Limit(1).Find(&creds).Error; err != nil {
			return nil, err
		}
		if len(creds) == 1 {
			return &creds[0], nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

// ListByUserID lists all CalDAV credentials for a user
func (r *CalDAVCredentialRepository) ListByUserID(ctx context.Context, userID uint) ([]user.CalDAVCredential, error) {
	var creds []user.CalDAVCredential
	if err := r.db.WithContext(ctx).Where("user_id = ? AND revoked_at IS NULL", userID).Order("created_at DESC").Find(&creds).Error; err != nil {
		return nil, err
	}
	return creds, nil
}

// Revoke soft-deletes a CalDAV credential by setting revoked_at and releases
// its username (rewritten to "revoked:<uuid>") so it can be used again (F24).
func (r *CalDAVCredentialRepository) Revoke(ctx context.Context, id uint) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&user.CalDAVCredential{}).Where("id = ?", id).Updates(map[string]interface{}{
		"revoked_at": now,
		"username":   revokedCredentialUsername,
	}).Error
}

// UpdateLastUsed updates the last used timestamp and IP
func (r *CalDAVCredentialRepository) UpdateLastUsed(ctx context.Context, id uint, ip string) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&user.CalDAVCredential{}).Where("id = ?", id).Updates(map[string]interface{}{
		"last_used_at": now,
		"last_used_ip": ip,
	}).Error
}
