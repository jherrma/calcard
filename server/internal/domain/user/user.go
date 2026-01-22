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
	Username      string `gorm:"uniqueIndex;size:100;not null"`
	PasswordHash  string `gorm:"size:255;not null"`
	DisplayName   string `gorm:"size:255"`
	IsActive      bool   `gorm:"default:true"`
	EmailVerified bool   `gorm:"default:false"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
	DeletedAt     gorm.DeletedAt `gorm:"index"`
}

// TableName returns the table name for the User model
func (User) TableName() string {
	return "users"
}
