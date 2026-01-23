package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/user"
)

type ForgotPasswordRequest struct {
	Email   string
	BaseURL string
}

type ForgotPasswordUseCase struct {
	userRepo     user.UserRepository
	resetRepo    user.PasswordResetRepository
	emailService EmailService
	resetExpiry  time.Duration
}

func NewForgotPasswordUseCase(
	userRepo user.UserRepository,
	resetRepo user.PasswordResetRepository,
	emailService EmailService,
	resetExpiry time.Duration,
) *ForgotPasswordUseCase {
	return &ForgotPasswordUseCase{
		userRepo:     userRepo,
		resetRepo:    resetRepo,
		emailService: emailService,
		resetExpiry:  resetExpiry,
	}
}

func (uc *ForgotPasswordUseCase) Execute(ctx context.Context, req ForgotPasswordRequest) error {
	u, err := uc.userRepo.GetByEmail(ctx, req.Email)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}

	// Always return nil to prevent email enumeration
	if u == nil {
		return nil
	}

	// Generate random token
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return fmt.Errorf("failed to generate random token: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)

	// Hash token for storage
	hash := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hash[:])

	// Invalidate old tokens
	if err := uc.resetRepo.DeleteByUserID(ctx, u.ID); err != nil {
		return fmt.Errorf("failed to delete old reset tokens: %w", err)
	}

	// Store new token
	reset := &user.PasswordReset{
		UserID:    u.ID,
		TokenHash: tokenHash,
		ExpiresAt: time.Now().Add(uc.resetExpiry),
	}
	if err := uc.resetRepo.Create(ctx, reset); err != nil {
		return fmt.Errorf("failed to store reset token: %w", err)
	}

	// Send email
	resetURL := fmt.Sprintf("%s/auth/reset-password?token=%s", req.BaseURL, token)
	subject := "Reset your CalDAV Server password"
	body := fmt.Sprintf(`Hi %s,

You requested to reset your password. Click the link below to set a new password:

%s

This link expires in %v.

If you didn't request this, you can safely ignore this email.

- CalDAV Server`, u.DisplayName, resetURL, uc.resetExpiry)

	if err := uc.emailService.SendEmail(ctx, u.Email, subject, body); err != nil {
		return fmt.Errorf("failed to send reset email: %w", err)
	}

	return nil
}
