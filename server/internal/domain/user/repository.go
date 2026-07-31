package user

import (
	"context"
	"errors"
	"time"
)

// ErrRefreshTokenRotated is returned by Rotate when the old token was already
// revoked/rotated by a concurrent request. The caller must NOT get a successor
// in that case — treat it like presenting an already-rotated token.
var ErrRefreshTokenRotated = errors.New("refresh token: already rotated by a concurrent request")

// UserRepository defines the interface for user persistence
type UserRepository interface {
	Create(ctx context.Context, user *User) error
	GetByEmail(ctx context.Context, email string) (*User, error)
	GetByUsername(ctx context.Context, username string) (*User, error)
	GetByUUID(ctx context.Context, uuid string) (*User, error)
	GetByID(ctx context.Context, id uint) (*User, error)
	Update(ctx context.Context, user *User) error
	Delete(ctx context.Context, userID uint) error
	GetByOAuth(ctx context.Context, provider, providerID string) (*User, error)
	Count(ctx context.Context) (int64, error)

	CreateVerification(ctx context.Context, v *EmailVerification) error
	GetVerificationByToken(ctx context.Context, token string) (*EmailVerification, error)
	DeleteVerification(ctx context.Context, token string) error
}

// RefreshTokenRepository defines the interface for refresh token persistence
type RefreshTokenRepository interface {
	Create(ctx context.Context, token *RefreshToken) error
	GetByHash(ctx context.Context, hash string) (*RefreshToken, error)
	DeleteByHash(ctx context.Context, hash string) error
	DeleteByUserID(ctx context.Context, userID uint) error
	// Rotate atomically supersedes oldHash with newToken: within a single
	// transaction it creates newToken and marks the old row revoked with its
	// ReplacedByHash set to newToken.TokenHash. Atomicity guarantees a crash can
	// never leave both tokens live or both dead.
	//
	// Returns ErrRefreshTokenRotated (and creates nothing) if the old token is
	// already revoked — i.e. a concurrent request rotated it first.
	Rotate(ctx context.Context, oldHash string, newToken *RefreshToken) error
	// RevokeFamily revokes every not-yet-revoked token in the given family.
	// Implementations MUST refuse an empty familyID: legacy rows carry
	// family_id='' and revoking that "family" would log out unrelated users.
	RevokeFamily(ctx context.Context, familyID string) error
}

// AppPasswordRepository defines the interface for app password persistence
type AppPasswordRepository interface {
	Create(ctx context.Context, ap *AppPassword) error
	GetByUUID(ctx context.Context, uuid string) (*AppPassword, error)
	ListByUserID(ctx context.Context, userID uint) ([]AppPassword, error)
	Update(ctx context.Context, ap *AppPassword) error
	FindValidForUser(ctx context.Context, userID uint, password string) (*AppPassword, error)
	CountByUserID(ctx context.Context, userID uint) (int64, error)
}

// UserPreferenceRepository defines the interface for user preference persistence
type UserPreferenceRepository interface {
	GetByUserID(ctx context.Context, userID uint) ([]UserPreference, error)
	GetByKey(ctx context.Context, userID uint, key string) (*UserPreference, error)
	// Upsert inserts the preference or, when (user_id, key) already exists,
	// updates its value in a single statement. Implementations MUST be
	// insert-or-update in one round trip: a read-then-write would let two
	// concurrent PATCHes race into a unique-index violation.
	Upsert(ctx context.Context, pref *UserPreference) error
	Delete(ctx context.Context, userID uint, key string) error
}

type CardDAVCredentialRepository interface {
	Create(ctx context.Context, cred *CardDAVCredential) error
	GetByUUID(ctx context.Context, uuid string) (*CardDAVCredential, error)
	GetByUsername(ctx context.Context, username string) (*CardDAVCredential, error)
	ListByUserID(ctx context.Context, userID uint) ([]CardDAVCredential, error)
	Update(ctx context.Context, cred *CardDAVCredential) error
	Revoke(ctx context.Context, id uint) error
	UpdateLastUsed(ctx context.Context, id uint, ip string) error
}

// PasswordResetRepository defines the interface for password reset persistence
type PasswordResetRepository interface {
	Create(ctx context.Context, reset *PasswordReset) error
	GetByHash(ctx context.Context, hash string) (*PasswordReset, error)
	DeleteByUserID(ctx context.Context, userID uint) error
}

// TokenProvider defines the interface for token operations
type TokenProvider interface {
	GenerateAccessToken(userID string, email string) (string, time.Time, error)
	GenerateRefreshToken() (string, error)
	HashToken(token string) string
	ValidateAccessToken(tokenStr string) (string, string, error) // Returns UserUUID, Email, error
}

// OAuthConnectionRepository defines the interface for OAuth connection persistence
type OAuthConnectionRepository interface {
	Create(ctx context.Context, conn *OAuthConnection) error
	GetByProvider(ctx context.Context, userID uint, provider string) (*OAuthConnection, error)
	ListByUserID(ctx context.Context, userID uint) ([]OAuthConnection, error)
	Update(ctx context.Context, conn *OAuthConnection) error
	Delete(ctx context.Context, userID uint, provider string) error
}

// CalDAVCredentialRepository defines the interface for CalDAV credential persistence
type CalDAVCredentialRepository interface {
	Create(ctx context.Context, cred *CalDAVCredential) error
	GetByUUID(ctx context.Context, uuid string) (*CalDAVCredential, error)
	GetByUsername(ctx context.Context, username string) (*CalDAVCredential, error)
	ListByUserID(ctx context.Context, userID uint) ([]CalDAVCredential, error)
	Revoke(ctx context.Context, id uint) error
	UpdateLastUsed(ctx context.Context, id uint, ip string) error
}
