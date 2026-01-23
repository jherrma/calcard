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
	userRepo    user.UserRepository
	resetRepo   user.PasswordResetRepository
	refreshRepo user.RefreshTokenRepository
}

func NewResetPasswordUseCase(
	userRepo user.UserRepository,
	resetRepo user.PasswordResetRepository,
	refreshRepo user.RefreshTokenRepository,
) *ResetPasswordUseCase {
	return &ResetPasswordUseCase{
		userRepo:    userRepo,
		resetRepo:   resetRepo,
		refreshRepo: refreshRepo,
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

	// Hash new password
	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash new password: %w", err)
	}

	// Update user password
	u.PasswordHash = string(newHash)
	if err := uc.userRepo.Update(ctx, u); err != nil {
		return fmt.Errorf("failed to update user: %w", err)
	}

	// Mark token as used
	now := time.Now()
	reset.UsedAt = &now
	if err := uc.resetRepo.DeleteByUserID(ctx, u.ID); err != nil {
		return fmt.Errorf("failed to clear reset tokens: %w", err)
	}

	// Revoke all refresh tokens
	if err := uc.refreshRepo.DeleteByUserID(ctx, u.ID); err != nil {
		return fmt.Errorf("failed to revoke sessions: %w", err)
	}

	return nil
}
