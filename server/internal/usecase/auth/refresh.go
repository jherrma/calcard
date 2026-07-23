package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/jherrma/caldav-server/internal/infrastructure/logging"
)

var (
	ErrInvalidRefreshToken = errors.New("invalid refresh token")
)

// rotationGrace is how long after a token is rotated a second presentation of
// that same (now-revoked) token is treated as a benign multi-tab race rather
// than a stolen-token replay. The frontend serializes refreshes across tabs, so
// a legitimate double-refresh only happens in a very narrow window; anything
// past the grace is treated as reuse and revokes the whole family.
const rotationGrace = 15 * time.Second

// RefreshUseCase handles token refresh with rotation and reuse detection.
type RefreshUseCase struct {
	tokenRepo      user.RefreshTokenRepository
	jwtManager     user.TokenProvider
	cfg            refreshConfig
	securityLogger *logging.SecurityLogger
}

// refreshConfig is the minimal config surface the use case needs (kept as a
// small interface-free struct so the use case layer stays free of adapter/http
// and framework types).
type refreshConfig struct {
	RefreshExpiry time.Duration
}

// RefreshResult contains the freshly minted access token plus the rotated
// refresh token the client must persist in place of the one it presented.
type RefreshResult struct {
	AccessToken  string
	ExpiresAt    time.Time
	RefreshToken string
}

// NewRefreshUseCase creates a new refresh use case. refreshExpiry is the
// lifetime stamped onto rotated refresh tokens. securityLogger records detected
// refresh-token reuse (theft) as a structured security event.
func NewRefreshUseCase(tokenRepo user.RefreshTokenRepository, jwtManager user.TokenProvider, refreshExpiry time.Duration, securityLogger *logging.SecurityLogger) *RefreshUseCase {
	return &RefreshUseCase{
		tokenRepo:      tokenRepo,
		jwtManager:     jwtManager,
		cfg:            refreshConfig{RefreshExpiry: refreshExpiry},
		securityLogger: securityLogger,
	}
}

// Execute validates the presented refresh token, rotates it, and returns a new
// access + refresh token pair. See the inline comments for the reuse-detection
// state machine.
func (uc *RefreshUseCase) Execute(ctx context.Context, presented string) (*RefreshResult, error) {
	hash := uc.jwtManager.HashToken(presented)

	t, err := uc.tokenRepo.GetByHash(ctx, hash)
	if err != nil {
		return nil, ErrInvalidRefreshToken
	}
	if t == nil {
		return nil, ErrInvalidRefreshToken
	}

	now := time.Now()

	if t.ExpiresAt.Before(now) {
		return nil, ErrInvalidRefreshToken
	}

	// The token was already used/rotated. This is either a benign multi-tab race
	// or a replay of a stolen token.
	if t.RevokedAt != nil {
		// Benign race: the token was legitimately superseded (ReplacedByHash set)
		// only moments ago. Two tabs raced through refresh before the frontend
		// lock serialized them. Reject this stale presentation but do NOT punish
		// the user by revoking the family. We store only hashes, so we cannot
		// re-hand the successor's plaintext here — the client's other tab already
		// holds it.
		if t.ReplacedByHash != "" && now.Sub(*t.RevokedAt) <= rotationGrace {
			return nil, ErrInvalidRefreshToken
		}

		// Reuse/theft: a revoked token presented outside the grace window (or one
		// that was never rotated). Kill the whole lineage — the classic
		// stolen-token signal. Legacy rows have no family (family_id=''); never
		// pass "" to RevokeFamily (it would nuke unrelated users), just reject.
		var revokeErr error
		if t.FamilyID != "" {
			// Best-effort: even if the revoke errors we still reject the token —
			// but the failure is logged below, not swallowed.
			revokeErr = uc.tokenRepo.RevokeFamily(ctx, t.FamilyID)
		}
		if uc.securityLogger != nil {
			// t.IP / t.UserAgent are the LOGIN-time values stored on the token
			// row, not the replaying request's — still useful for correlation.
			uc.securityLogger.LogRefreshTokenReuse(ctx, t.UserID, t.FamilyID, t.IP, t.UserAgent, revokeErr)
		}
		return nil, ErrInvalidRefreshToken
	}

	// A deactivated account must not be able to mint fresh access tokens from a
	// still-valid refresh token. GetByHash preloads the associated User.
	if !t.User.IsActive {
		return nil, ErrInactiveAccount
	}

	// Normal path: mint a successor refresh token and rotate atomically.
	newRefresh, err := uc.jwtManager.GenerateRefreshToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate refresh token: %w", err)
	}

	// Inherit the family so the lineage stays linked; legacy tokens with no
	// family get a fresh one assigned on first rotation.
	familyID := t.FamilyID
	if familyID == "" {
		familyID, err = uc.jwtManager.GenerateRefreshToken()
		if err != nil {
			return nil, fmt.Errorf("failed to generate token family id: %w", err)
		}
	}

	successor := &user.RefreshToken{
		UserID:    t.UserID,
		TokenHash: uc.jwtManager.HashToken(newRefresh),
		ExpiresAt: now.Add(uc.cfg.RefreshExpiry),
		UserAgent: t.UserAgent,
		IP:        t.IP,
		FamilyID:  familyID,
	}

	if err := uc.tokenRepo.Rotate(ctx, hash, successor); err != nil {
		if errors.Is(err, user.ErrRefreshTokenRotated) {
			// Lost a concurrent-rotation race on the same token. Same semantics
			// as the benign multi-tab race: reject this presentation, do NOT
			// revoke the family — the winner's successor is the live token.
			return nil, ErrInvalidRefreshToken
		}
		return nil, fmt.Errorf("failed to rotate refresh token: %w", err)
	}

	accessToken, expiresAt, err := uc.jwtManager.GenerateAccessToken(t.User.UUID, t.User.Email)
	if err != nil {
		return nil, fmt.Errorf("failed to generate access token: %w", err)
	}

	return &RefreshResult{
		AccessToken:  accessToken,
		ExpiresAt:    expiresAt,
		RefreshToken: newRefresh,
	}, nil
}
