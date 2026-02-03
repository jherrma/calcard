package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/jherrma/caldav-server/internal/infrastructure/logging"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrIncorrectPassword = errors.New("current password is incorrect")
	ErrSamePassword      = errors.New("new password cannot be the same as the current password")
)

type ChangePasswordRequest struct {
	UserUUID        string
	CurrentPassword string
	NewPassword     string
	IP              string
	UserAgent       string
}

type ChangePasswordResult struct {
	AccessToken string
}

type ChangePasswordUseCase struct {
	userRepo    user.UserRepository
	refreshRepo user.RefreshTokenRepository
	jwtManager  user.TokenProvider
	logger      *logging.SecurityLogger
}

func NewChangePasswordUseCase(
	userRepo user.UserRepository,
	refreshRepo user.RefreshTokenRepository,
	jwtManager user.TokenProvider,
	logger *logging.SecurityLogger,
) *ChangePasswordUseCase {
	return &ChangePasswordUseCase{
		userRepo:    userRepo,
		refreshRepo: refreshRepo,
		jwtManager:  jwtManager,
		logger:      logger,
	}
}

func (uc *ChangePasswordUseCase) Execute(ctx context.Context, req ChangePasswordRequest) (*ChangePasswordResult, error) {
	u, err := uc.userRepo.GetByUUID(ctx, req.UserUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	if u == nil {
		return nil, errors.New("user not found")
	}

	// Verify current password
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.CurrentPassword)); err != nil {
		return nil, ErrIncorrectPassword
	}

	// Check if new password is same as current
	if req.CurrentPassword == req.NewPassword {
		return nil, ErrSamePassword
	}

	// Hash new password
	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash new password: %w", err)
	}

	// Revoke all refresh tokens
	if err := uc.refreshRepo.DeleteByUserID(ctx, u.ID); err != nil {
		return nil, fmt.Errorf("failed to revoke sessions: %w", err)
	}

	// Update user password
	u.PasswordHash = string(hash)
	if err := uc.userRepo.Update(ctx, u); err != nil {
		return nil, fmt.Errorf("failed to update user: %w", err)
	}

	uc.logger.LogPasswordChange(ctx, u.ID, req.IP, req.UserAgent)

	// Generate new access token
	token, _, err := uc.jwtManager.GenerateAccessToken(u.UUID, u.Email)
	if err != nil {
		return nil, fmt.Errorf("failed to generate new token: %w", err)
	}

	return &ChangePasswordResult{AccessToken: token}, nil
}
