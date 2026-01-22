package user

import (
	"context"
	"time"
)

// UserRepository defines the interface for user persistence
type UserRepository interface {
	Create(ctx context.Context, user *User) error
	GetByEmail(ctx context.Context, email string) (*User, error)
	GetByUUID(ctx context.Context, uuid string) (*User, error)
	Update(ctx context.Context, user *User) error

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

// TokenProvider defines the interface for token operations
type TokenProvider interface {
	GenerateAccessToken(userID string, email string) (string, time.Time, error)
	GenerateRefreshToken() (string, error)
	HashToken(token string) string
	ValidateAccessToken(tokenStr string) (string, string, error) // Returns UserUUID, Email, error
}
