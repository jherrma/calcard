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
	Username      string `gorm:"uniqueIndex;size:100;not null;default:''"` // Default empty for migration
	PasswordHash  string `gorm:"size:255;not null"`
	DisplayName   string `gorm:"size:255"`
	IsActive      bool   `gorm:"not null"`
	EmailVerified bool   `gorm:"not null"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
	DeletedAt     gorm.DeletedAt `gorm:"index"`

	OAuthConnections []OAuthConnection `gorm:"foreignKey:UserID"`
}

// OAuthConnection represents a linked OAuth/OIDC provider
type OAuthConnection struct {
	ID            uint   `gorm:"primaryKey"`
	UserID        uint   `gorm:"index;not null"`
	Provider      string `gorm:"size:50;not null"`  // google, microsoft, custom
	ProviderID    string `gorm:"size:255;not null"` // sub claim from OIDC
	ProviderEmail string `gorm:"size:255"`
	AccessToken   string `gorm:"size:2000"` // encrypted
	RefreshToken  string `gorm:"size:2000"` // encrypted
	TokenExpiry   *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
	User          User `gorm:"foreignKey:UserID"`
}

// TableName returns the table name for the OAuthConnection model
func (OAuthConnection) TableName() string {
	return "oauth_connections"
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

// SAMLSession represents an active SAML session
type SAMLSession struct {
	ID        uint      `gorm:"primaryKey"`
	UserID    uint      `gorm:"index;not null"`
	SessionID string    `gorm:"uniqueIndex;size:64;not null"` // SAML SessionIndex
	NameID    string    `gorm:"size:255;not null"`
	ExpiresAt time.Time `gorm:"index;not null"`
	CreatedAt time.Time
	User      User `gorm:"foreignKey:UserID"`
}

// TableName returns the table name for the SAMLSession model
func (SAMLSession) TableName() string {
	return "saml_sessions"
}

// TableName returns the table name for the User model
func (User) TableName() string {
	return "users"
}
