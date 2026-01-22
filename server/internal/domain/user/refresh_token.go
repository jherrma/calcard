package user

import (
	"time"
)

// RefreshToken represents a long-lived token used to obtain new access tokens
type RefreshToken struct {
	ID        uint      `gorm:"primaryKey"`
	UserID    uint      `gorm:"index;not null"`
	TokenHash string    `gorm:"uniqueIndex;size:64;not null"`
	ExpiresAt time.Time `gorm:"index;not null"`
	UserAgent string    `gorm:"size:500"`
	IP        string    `gorm:"size:45"`
	CreatedAt time.Time
	RevokedAt *time.Time `gorm:"index"`
	User      User       `gorm:"foreignKey:UserID"`
}

// TableName returns the table name for the RefreshToken model
func (RefreshToken) TableName() string {
	return "refresh_tokens"
}
