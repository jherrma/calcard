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

// OAuthConnection represents a linked OAuth/OIDC provider.
//
// Two unique indexes are enforced at the DB level:
//   - (user_id, provider): a user may link at most one account per provider.
//   - (provider, provider_id): a given provider identity links to one user.
//
// GORM composes each index from the fields sharing its name, in struct-field
// order, so the field order below matters. The (user_id, provider) index also
// serves ListByUserID via its user_id prefix, so no separate index is needed.
type OAuthConnection struct {
	ID            uint       `gorm:"primaryKey"`
	UserID        uint       `gorm:"not null;uniqueIndex:idx_oauth_user_provider"`
	Provider      string     `gorm:"size:50;not null;uniqueIndex:idx_oauth_user_provider;uniqueIndex:idx_oauth_provider_subject"` // google, microsoft, custom
	ProviderID    string     `gorm:"size:255;not null;uniqueIndex:idx_oauth_provider_subject"`                                    // sub claim from OIDC
	ProviderEmail string     `gorm:"size:255"`
	AccessToken   string     `gorm:"size:2000"` // deprecated: no longer written; kept to avoid a destructive migration
	RefreshToken  string     `gorm:"size:2000"` // deprecated: no longer written; kept to avoid a destructive migration
	TokenExpiry   *time.Time // deprecated: no longer written; kept to avoid a destructive migration
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

// TableName returns the table name for the User model
func (User) TableName() string {
	return "users"
}
