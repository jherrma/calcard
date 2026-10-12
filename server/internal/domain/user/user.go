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
	PasswordHash  string `gorm:"size:255;not null" json:"-"`               // never serialised: a User can be embedded in API responses
	DisplayName   string `gorm:"size:255"`
	IsActive      bool   `gorm:"not null"`
	EmailVerified bool   `gorm:"not null"`
	// IsAdmin marks the account created by first-run setup (POST /auth/setup).
	// Nothing grants it afterwards.
	IsAdmin bool `gorm:"not null;default:false"`
	// TokensValidAfter invalidates every access token (JWT) issued before it.
	// Access tokens are stateless, so this per-user marker is the only way to
	// kill one before it expires: logout, password change/reset and account
	// deletion move it to "now". Nil means no token has ever been revoked.
	// Written ONLY through UserRepository.InvalidateAccessTokens — Update
	// deliberately leaves it alone so a stale User struct cannot roll it back.
	TokensValidAfter *time.Time `json:"-"`
	CreatedAt        time.Time
	UpdatedAt        time.Time
	DeletedAt        gorm.DeletedAt `gorm:"index"`

	OAuthConnections []OAuthConnection `gorm:"foreignKey:UserID"`
}

// DeletedIdentityPrefix starts the tombstone that replaces both the e-mail and
// the username of a deleted account: "deleted:<uuid>". Deletion is a soft
// delete and the email/username unique indexes cover deleted rows, so without
// it the address could never register again (F19). The value is unique per
// account (the UUID is) and has no '@' and a ':', so neither ValidateEmail nor
// the setup username pattern accepts it: no live account can pre-claim one.
const DeletedIdentityPrefix = "deleted:"

// AcceptsAccessTokenIssuedAt reports whether an access token issued at
// issuedAt is still honoured for this user, i.e. was not issued before the
// last invalidation (see TokensValidAfter).
//
// A token issued at exactly the marker instant is accepted: the request that
// invalidates sessions (change password) moves the marker first and then
// issues its own replacement token, which on a coarse clock can carry the very
// same timestamp. Token issue times carry nanosecond precision (the iat_ns
// claim), so "the same instant" is a sub-microsecond window, not the
// one-second window a plain RFC 7519 iat would leave open.
func (u *User) AcceptsAccessTokenIssuedAt(issuedAt time.Time) bool {
	return u.TokensValidAfter == nil || !issuedAt.Before(*u.TokensValidAfter)
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
	AccessToken   string     `gorm:"size:2000" json:"-"` // deprecated: no longer written; kept to avoid a destructive migration
	RefreshToken  string     `gorm:"size:2000" json:"-"` // deprecated: no longer written; kept to avoid a destructive migration
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
	TokenHash string    `gorm:"uniqueIndex;size:64;not null" json:"-"`
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
