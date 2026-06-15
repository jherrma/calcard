package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/user"
)

var (
	ErrInvalidRefreshToken = errors.New("invalid refresh token")
)

// RefreshUseCase handles token refresh
type RefreshUseCase struct {
	tokenRepo  user.RefreshTokenRepository
	jwtManager user.TokenProvider
}

// RefreshResult contains the new access token
type RefreshResult struct {
	AccessToken string
	ExpiresAt   time.Time
}

// NewRefreshUseCase creates a new refresh use case
func NewRefreshUseCase(tokenRepo user.RefreshTokenRepository, jwtManager user.TokenProvider) *RefreshUseCase {
	return &RefreshUseCase{tokenRepo: tokenRepo, jwtManager: jwtManager}
}

// Execute performs the refresh logic
func (uc *RefreshUseCase) Execute(ctx context.Context, refreshToken string) (*RefreshResult, error) {
	hash := uc.jwtManager.HashToken(refreshToken)

	t, err := uc.tokenRepo.GetByHash(ctx, hash)
	if err != nil {
		return nil, ErrInvalidRefreshToken
	}
	if t == nil {
		return nil, ErrInvalidRefreshToken
	}

	if t.RevokedAt != nil || t.ExpiresAt.Before(time.Now()) {
		return nil, ErrInvalidRefreshToken
	}

	// A deactivated account must not be able to mint fresh access tokens from a
	// still-valid refresh token. GetByHash preloads the associated User.
	if !t.User.IsActive {
		return nil, ErrInactiveAccount
	}

	accessToken, expiresAt, err := uc.jwtManager.GenerateAccessToken(t.User.UUID, t.User.Email)
	if err != nil {
		return nil, fmt.Errorf("failed to generate access token: %w", err)
	}

	return &RefreshResult{
		AccessToken: accessToken,
		ExpiresAt:   expiresAt,
	}, nil
}
