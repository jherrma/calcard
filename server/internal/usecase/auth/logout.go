package auth

import (
	"context"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/user"
)

// LogoutUseCase handles user logout
type LogoutUseCase struct {
	tokenRepo  user.RefreshTokenRepository
	userRepo   user.UserRepository
	jwtManager user.TokenProvider
}

// NewLogoutUseCase creates a new logout use case
func NewLogoutUseCase(tokenRepo user.RefreshTokenRepository, userRepo user.UserRepository, jwtManager user.TokenProvider) *LogoutUseCase {
	return &LogoutUseCase{tokenRepo: tokenRepo, userRepo: userRepo, jwtManager: jwtManager}
}

// Execute logs the caller out: it revokes the presented refresh token and
// invalidates every access token the account holds.
//
// Either credential identifies the account. The refresh token is the normal
// one (the SPA sends it); the bearer access token covers a client that only
// holds an access token. Both are optional; with neither, logout is a no-op.
//
// Access-token invalidation is deliberately account-wide, not per device: a
// stateless JWT carries nothing a per-token denylist could key on cheaply, and
// the cost to the user's OTHER sessions is a single transparent refresh — their
// refresh tokens are untouched, so the next 401 is answered by /auth/refresh
// and they carry on. Only the device that logged out loses its refresh token.
func (uc *LogoutUseCase) Execute(ctx context.Context, refreshToken, accessToken string) error {
	var userID uint

	if refreshToken != "" {
		hash := uc.jwtManager.HashToken(refreshToken)
		// Only a live refresh token identifies the account: an already
		// rotated or revoked one must not let its holder force everyone's
		// sessions through a refresh.
		if t, err := uc.tokenRepo.GetByHash(ctx, hash); err == nil && t != nil && t.RevokedAt == nil {
			userID = t.UserID
		}
		if err := uc.tokenRepo.DeleteByHash(ctx, hash); err != nil {
			return err
		}
	}

	if userID == 0 && accessToken != "" {
		if claims, err := uc.jwtManager.ValidateAccessToken(accessToken); err == nil {
			if u, err := uc.userRepo.GetByUUID(ctx, claims.UserUUID); err == nil && u != nil &&
				u.AcceptsAccessTokenIssuedAt(claims.IssuedAt) {
				userID = u.ID
			}
		}
	}

	if userID == 0 {
		return nil
	}
	return uc.userRepo.InvalidateAccessTokens(ctx, userID, time.Now())
}
