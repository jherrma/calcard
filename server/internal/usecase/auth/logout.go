package auth

import (
	"context"

	"github.com/jherrma/caldav-server/internal/domain/user"
)

// LogoutUseCase handles user logout
type LogoutUseCase struct {
	tokenRepo  user.RefreshTokenRepository
	jwtManager user.TokenProvider
}

// NewLogoutUseCase creates a new logout use case
func NewLogoutUseCase(tokenRepo user.RefreshTokenRepository, jwtManager user.TokenProvider) *LogoutUseCase {
	return &LogoutUseCase{tokenRepo: tokenRepo, jwtManager: jwtManager}
}

// Execute performs the logout logic by revoking the refresh token
func (uc *LogoutUseCase) Execute(ctx context.Context, refreshToken string) error {
	hash := uc.jwtManager.HashToken(refreshToken)
	return uc.tokenRepo.DeleteByHash(ctx, hash)
}
