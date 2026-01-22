package auth

import (
	"context"
	"errors"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/user"
)

var (
	ErrInvalidToken = errors.New("invalid or expired token")
)

type VerifyUseCase struct {
	repo user.UserRepository
}

func NewVerifyUseCase(repo user.UserRepository) *VerifyUseCase {
	return &VerifyUseCase{repo: repo}
}

func (uc *VerifyUseCase) Execute(ctx context.Context, token string) error {
	// 1. Find token
	v, err := uc.repo.GetVerificationByToken(ctx, token)
	if err != nil {
		return err
	}
	if v == nil {
		return ErrInvalidToken
	}

	// 2. Check expiration
	if time.Now().After(v.ExpiresAt) {
		_ = uc.repo.DeleteVerification(ctx, token)
		return ErrInvalidToken
	}

	// 3. Activate user
	u := &v.User
	u.IsActive = true
	u.EmailVerified = true

	if err := uc.repo.Update(ctx, u); err != nil {
		return err
	}

	// 4. Delete token
	return uc.repo.DeleteVerification(ctx, token)
}
