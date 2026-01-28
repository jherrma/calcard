package user

import (
	"context"
	"time"
)

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
}

// AppPasswordRepository defines the interface for app password persistence
type AppPasswordRepository interface {
	Create(ctx context.Context, ap *AppPassword) error
	GetByUUID(ctx context.Context, uuid string) (*AppPassword, error)
	ListByUserID(ctx context.Context, userID uint) ([]AppPassword, error)
	Update(ctx context.Context, ap *AppPassword) error
	FindValidForUser(ctx context.Context, userID uint, password string) (*AppPassword, error)
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

// SAMLSessionRepository defines the interface for SAML session persistence
type SAMLSessionRepository interface {
	Create(ctx context.Context, session *SAMLSession) error
	GetBySessionID(ctx context.Context, sessionID string) (*SAMLSession, error)
	DeleteBySessionID(ctx context.Context, sessionID string) error
	DeleteByUserID(ctx context.Context, userID uint) error
}
