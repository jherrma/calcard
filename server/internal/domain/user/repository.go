package user

import (
	"context"
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
