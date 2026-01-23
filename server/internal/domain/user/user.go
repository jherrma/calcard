package user

import (
	"time"

	"gorm.io/gorm"
)

// User represents a user in the system
type User struct {
	ID            uint   `gorm:"primaryKey"`
	UUID          string `gorm:"uniqueIndex;size:36;not null"`
	Email         string `gorm:"uniqueIndex;size:255;not null"`
	PasswordHash  string `gorm:"size:255;not null"`
	DisplayName   string `gorm:"size:255"`
	IsActive      bool   `gorm:"not null"`
	EmailVerified bool   `gorm:"not null"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
	DeletedAt     gorm.DeletedAt `gorm:"index"`
}

// PasswordReset represents a password reset token
type PasswordReset struct {
	ID        uint      `gorm:"primaryKey"`
	UserID    uint      `gorm:"index;not null"`
	TokenHash string    `gorm:"uniqueIndex;size:64;not null"`
	ExpiresAt time.Time `gorm:"index;not null"`
	CreatedAt time.Time
	UsedAt    *time.Time
	User      User `gorm:"foreignKey:UserID"`
}

// TableName returns the table name for the PasswordReset model
func (PasswordReset) TableName() string {
	return "password_resets"
}

// TableName returns the table name for the User model
func (User) TableName() string {
	return "users"
}
