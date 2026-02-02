package user

import (
	"time"

	"gorm.io/gorm"
)

// CardDAVCredential represents a dedicated credential for CardDAV access
type CardDAVCredential struct {
	ID           uint           `gorm:"primaryKey" json:"id"`
	UUID         string         `gorm:"uniqueIndex;size:36;not null" json:"uuid"`
	UserID       uint           `gorm:"index;not null" json:"user_id"`
	Name         string         `gorm:"size:100;not null" json:"name"`
	Username     string         `gorm:"uniqueIndex;size:50;not null" json:"username"`
	PasswordHash string         `gorm:"size:255;not null" json:"-"`
	Permission   string         `gorm:"size:20;not null" json:"permission"` // "read" or "read-write"
	ExpiresAt    *time.Time     `gorm:"index" json:"expires_at"`
	LastUsedAt   *time.Time     `json:"last_used_at"`
	LastUsedIP   string         `gorm:"size:45" json:"last_used_ip"`
	CreatedAt    time.Time      `json:"created_at"`
	RevokedAt    *time.Time     `gorm:"index" json:"revoked_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
	User         User           `gorm:"foreignKey:UserID" json:"-"`
}

// IsValid checks if the credential is valid (not revoked and not expired)
func (c *CardDAVCredential) IsValid() bool {
	if c.RevokedAt != nil {
		return false
	}
	if c.ExpiresAt != nil && time.Now().After(*c.ExpiresAt) {
		return false
	}
	return true
}

// CanWrite checks if the credential has write permission
func (c *CardDAVCredential) CanWrite() bool {
	return c.Permission == "read-write"
}

// IsRevoked checks if the credential has been revoked
func (c *CardDAVCredential) IsRevoked() bool {
	return c.RevokedAt != nil
}

// IsExpired checks if the credential has expired
func (c *CardDAVCredential) IsExpired() bool {
	return c.ExpiresAt != nil && time.Now().After(*c.ExpiresAt)
}
