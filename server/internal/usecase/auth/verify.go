package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/user"
)

var (
	ErrInvalidToken = errors.New("invalid or expired token")
)

// hashVerificationToken hashes an email-verification token for storage/lookup,
// matching the SHA-256 hex scheme used for password-reset tokens. The raw token
// is only ever sent in the activation email; only its hash is persisted.
func hashVerificationToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

type VerifyUseCase struct {
	repo user.UserRepository
}

func NewVerifyUseCase(repo user.UserRepository) *VerifyUseCase {
	return &VerifyUseCase{repo: repo}
}

func (uc *VerifyUseCase) Execute(ctx context.Context, token string) error {
	// Tokens are stored hashed; look up by hash.
	tokenHash := hashVerificationToken(token)

	// 1. Find token
	v, err := uc.repo.GetVerificationByToken(ctx, tokenHash)
	if err != nil {
		return err
	}
	if v == nil {
		return ErrInvalidToken
	}

	// 2. Check expiration
	if time.Now().After(v.ExpiresAt) {
		_ = uc.repo.DeleteVerification(ctx, tokenHash)
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
	return uc.repo.DeleteVerification(ctx, tokenHash)
}
