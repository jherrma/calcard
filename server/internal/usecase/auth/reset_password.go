package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/user"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidResetToken = errors.New("password reset link is invalid or has expired")
)

type ResetPasswordRequest struct {
	Token       string
	NewPassword string
}

type ResetPasswordUseCase struct {
	userRepo  user.UserRepository
	resetRepo user.PasswordResetRepository
}

func NewResetPasswordUseCase(
	userRepo user.UserRepository,
	resetRepo user.PasswordResetRepository,
) *ResetPasswordUseCase {
	return &ResetPasswordUseCase{
		userRepo:  userRepo,
		resetRepo: resetRepo,
	}
}

func (uc *ResetPasswordUseCase) Execute(ctx context.Context, req ResetPasswordRequest) error {
	// Hash the token to find it in the DB
	hash := sha256.Sum256([]byte(req.Token))
	tokenHash := hex.EncodeToString(hash[:])

	reset, err := uc.resetRepo.GetByHash(ctx, tokenHash)
	if err != nil {
		return fmt.Errorf("failed to get reset token: %w", err)
	}

	// Validate token
	if reset == nil || reset.UsedAt != nil || reset.ExpiresAt.Before(time.Now()) {
		return ErrInvalidResetToken
	}

	u, err := uc.userRepo.GetByID(ctx, reset.UserID)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}
	if u == nil {
		return errors.New("user not found")
	}

	// Enforce the password complexity policy before hashing so a reset link
	// cannot be used to downgrade the account to a weak password.
	if err := user.ValidatePassword(req.NewPassword); err != nil {
		return err
	}

	// Hash new password
	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), user.BcryptCost)
	if err != nil {
		return fmt.Errorf("failed to hash new password: %w", err)
	}

	// Completing a reset proves control of the mailbox, so it also confirms
	// the address.
	u.PasswordHash = string(newHash)
	u.EmailVerified = true

	// One transaction: the new password, and the end of every way in that
	// existed before it — access and refresh tokens, reset links, app
	// passwords, DAV credentials, MCP tokens and OAuth links. A reset is the
	// recovery path after a compromise, and an account is usable before its
	// address is confirmed, so whoever registered it with this address must
	// not keep a credential past the owner's reset. Outgoing shares and public
	// links are not credentials and stay.
	if err := uc.userRepo.CompletePasswordReset(ctx, u, time.Now()); err != nil {
		return fmt.Errorf("failed to reset password: %w", err)
	}

	return nil
}
