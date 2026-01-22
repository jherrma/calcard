package user

import (
	"time"
)

// EmailVerification represents an email verification token
type EmailVerification struct {
	ID        uint      `gorm:"primaryKey"`
	UserID    uint      `gorm:"index;not null"`
	Token     string    `gorm:"uniqueIndex;size:64;not null"`
	ExpiresAt time.Time `gorm:"index;not null"`
	CreatedAt time.Time
	User      User `gorm:"foreignKey:UserID"`
}

// TableName returns the table name for the EmailVerification model
func (EmailVerification) TableName() string {
	return "email_verifications"
}
