package auth

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/jherrma/caldav-server/internal/infrastructure/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// The mocks (mockRefreshTokenRepo, mockTokenProvider) are the shared ones
// defined in oauth_callback_test.go. HashToken is stubbed per-input so a token
// string maps to a predictable "hash", which keeps the assertions readable.

const testRefreshExpiry = 7 * 24 * time.Hour

// newTestRefreshUC constructs a RefreshUseCase with a real (default-slog-backed)
// security logger so the reuse-detection logging path is exercised without any
// per-test wiring. Tests that want to assert on the emitted event construct the
// use case directly with a captured logger instead.
func newTestRefreshUC(repo user.RefreshTokenRepository, jwt user.TokenProvider) *RefreshUseCase {
	return NewRefreshUseCase(repo, jwt, testRefreshExpiry, logging.NewSecurityLogger(slog.Default()))
}

func activeUser() user.User {
	return user.User{ID: 1, UUID: "user-uuid", Email: "user@example.com", IsActive: true}
}

// TestRefresh_RotatesAndOldTokenStopsWorking proves the happy path rotates the
// token (new hash, access token) AND that the presented token is revoked so a
// replay lands on the revoked branch.
func TestRefresh_RotatesAndOldTokenStopsWorking(t *testing.T) {
	repo := new(mockRefreshTokenRepo)
	jwt := new(mockTokenProvider)
	uc := newTestRefreshUC(repo, jwt)
	ctx := context.Background()

	// Identity-style hashing: token string == its hash.
	jwt.On("HashToken", "old-refresh").Return("old-refresh")
	jwt.On("HashToken", "new-refresh").Return("new-refresh")
	jwt.On("GenerateRefreshToken").Return("new-refresh", nil)
	jwt.On("GenerateAccessToken", "user-uuid", "user@example.com").
		Return("access-token", time.Now().Add(time.Hour), nil)

	stored := &user.RefreshToken{
		ID:        10,
		UserID:    1,
		TokenHash: "old-refresh",
		ExpiresAt: time.Now().Add(testRefreshExpiry),
		FamilyID:  "fam-1",
		User:      activeUser(),
	}
	repo.On("GetByHash", ctx, "old-refresh").Return(stored, nil)

	var rotated *user.RefreshToken
	repo.On("Rotate", ctx, "old-refresh", mock.Anything).
		Run(func(args mock.Arguments) {
			rotated = args.Get(2).(*user.RefreshToken)
		}).Return(nil)

	res, err := uc.Execute(ctx, "old-refresh")

	assert.NoError(t, err)
	assert.NotNil(t, res)
	assert.Equal(t, "access-token", res.AccessToken)
	// A brand-new refresh token is returned, distinct from the presented one.
	assert.Equal(t, "new-refresh", res.RefreshToken)
	assert.NotEqual(t, "old-refresh", res.RefreshToken)

	// Rotate was called; the successor inherits the family and carries the new hash.
	repo.AssertCalled(t, "Rotate", ctx, "old-refresh", mock.Anything)
	assert.NotNil(t, rotated)
	assert.Equal(t, "new-refresh", rotated.TokenHash)
	assert.Equal(t, "fam-1", rotated.FamilyID)
	assert.Equal(t, uint(1), rotated.UserID)

	// --- Prove the OLD token stops working -------------------------------
	// Model what the DB now holds: the presented token is revoked and points at
	// its successor (as Rotate would have persisted). A replay must hit the
	// revoked branch. It's within the grace window here, so it's a benign race:
	// rejected, family untouched — the important part is it no longer rotates.
	now := time.Now()
	revoked := &user.RefreshToken{
		ID:             10,
		UserID:         1,
		TokenHash:      "old-refresh",
		ExpiresAt:      time.Now().Add(testRefreshExpiry),
		FamilyID:       "fam-1",
		RevokedAt:      &now,
		ReplacedByHash: "new-refresh",
		User:           activeUser(),
	}
	repo2 := new(mockRefreshTokenRepo)
	uc2 := newTestRefreshUC(repo2, jwt)
	repo2.On("GetByHash", ctx, "old-refresh").Return(revoked, nil)

	res2, err2 := uc2.Execute(ctx, "old-refresh")
	assert.Nil(t, res2)
	assert.ErrorIs(t, err2, ErrInvalidRefreshToken)
	repo2.AssertNotCalled(t, "Rotate", mock.Anything, mock.Anything, mock.Anything)
}

// TestRefresh_ReuseDetectionRevokesFamily proves that a revoked token presented
// outside the grace window revokes the entire family.
func TestRefresh_ReuseDetectionRevokesFamily(t *testing.T) {
	repo := new(mockRefreshTokenRepo)
	jwt := new(mockTokenProvider)
	// Capture the security log so we can assert the reuse event is emitted.
	var buf bytes.Buffer
	logger := logging.NewSecurityLogger(slog.New(slog.NewTextHandler(&buf, nil)))
	uc := NewRefreshUseCase(repo, jwt, testRefreshExpiry, logger)
	ctx := context.Background()

	jwt.On("HashToken", "stolen").Return("stolen")

	// Revoked well past the grace window → treated as theft/replay.
	revokedAt := time.Now().Add(-time.Hour)
	stored := &user.RefreshToken{
		ID:             10,
		UserID:         1,
		TokenHash:      "stolen",
		ExpiresAt:      time.Now().Add(testRefreshExpiry),
		FamilyID:       "fam-42",
		RevokedAt:      &revokedAt,
		ReplacedByHash: "successor",
		User:           activeUser(),
	}
	repo.On("GetByHash", ctx, "stolen").Return(stored, nil)
	repo.On("RevokeFamily", ctx, "fam-42").Return(nil)

	res, err := uc.Execute(ctx, "stolen")

	assert.Nil(t, res)
	assert.ErrorIs(t, err, ErrInvalidRefreshToken)
	// The whole lineage is revoked.
	repo.AssertCalled(t, "RevokeFamily", ctx, "fam-42")
	repo.AssertNotCalled(t, "Rotate", mock.Anything, mock.Anything, mock.Anything)
	// The detected reuse is logged as a security event (not swallowed).
	assert.Contains(t, buf.String(), "refresh_token_reuse")
}

// TestRefresh_ReuseDetection_SiblingRevoked models the family-store behavior: a
// real RevokeFamily flips every live token in the family to revoked. We back
// the mock with an in-memory family so we can assert a *sibling* token is dead
// afterwards (mirrors the integration/DB behavior without a DB).
func TestRefresh_ReuseDetection_SiblingRevoked(t *testing.T) {
	jwt := new(mockTokenProvider)
	jwt.On("HashToken", "reused").Return("reused")
	ctx := context.Background()

	// In-memory family: two live tokens sharing fam-42, one already rotated.
	revokedAt := time.Now().Add(-time.Hour)
	reused := &user.RefreshToken{
		TokenHash: "reused", FamilyID: "fam-42",
		ExpiresAt: time.Now().Add(testRefreshExpiry), RevokedAt: &revokedAt,
		ReplacedByHash: "successor", User: activeUser(),
	}
	sibling := &user.RefreshToken{
		TokenHash: "sibling", FamilyID: "fam-42",
		ExpiresAt: time.Now().Add(testRefreshExpiry), User: activeUser(),
	}
	family := map[string]*user.RefreshToken{"reused": reused, "sibling": sibling}

	repo := new(mockRefreshTokenRepo)
	repo.On("GetByHash", ctx, "reused").Return(reused, nil)
	repo.On("RevokeFamily", ctx, "fam-42").
		Run(func(args mock.Arguments) {
			now := time.Now()
			for _, tok := range family {
				if tok.FamilyID == "fam-42" && tok.RevokedAt == nil {
					tok.RevokedAt = &now
				}
			}
		}).Return(nil)

	uc := newTestRefreshUC(repo, jwt)
	_, err := uc.Execute(ctx, "reused")
	assert.ErrorIs(t, err, ErrInvalidRefreshToken)

	// The sibling token that was never presented is now revoked too.
	assert.NotNil(t, sibling.RevokedAt, "sibling in the family must be revoked after reuse detection")
}

// TestRefresh_BenignRaceDoesNotRevokeFamily proves a revoked token still inside
// the grace window is a tolerated multi-tab race: rejected, but the family is
// left intact.
func TestRefresh_BenignRaceDoesNotRevokeFamily(t *testing.T) {
	repo := new(mockRefreshTokenRepo)
	jwt := new(mockTokenProvider)
	uc := newTestRefreshUC(repo, jwt)
	ctx := context.Background()

	jwt.On("HashToken", "raced").Return("raced")

	justNow := time.Now().Add(-2 * time.Second) // within rotationGrace
	stored := &user.RefreshToken{
		ID:             10,
		UserID:         1,
		TokenHash:      "raced",
		ExpiresAt:      time.Now().Add(testRefreshExpiry),
		FamilyID:       "fam-7",
		RevokedAt:      &justNow,
		ReplacedByHash: "successor",
		User:           activeUser(),
	}
	repo.On("GetByHash", ctx, "raced").Return(stored, nil)

	res, err := uc.Execute(ctx, "raced")

	assert.Nil(t, res)
	assert.ErrorIs(t, err, ErrInvalidRefreshToken)
	// Crucially: the family is NOT revoked for a benign race.
	repo.AssertNotCalled(t, "RevokeFamily", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "Rotate", mock.Anything, mock.Anything, mock.Anything)
}

// TestRefresh_RotateConflictRejectedWithoutFamilyRevocation: losing the
// concurrent-rotation race is rejected like a benign race — 401 semantics, no
// family revocation.
func TestRefresh_RotateConflictRejectedWithoutFamilyRevocation(t *testing.T) {
	repo := new(mockRefreshTokenRepo)
	jwt := new(mockTokenProvider)
	uc := newTestRefreshUC(repo, jwt)
	ctx := context.Background()

	jwt.On("HashToken", "raced").Return("raced")
	jwt.On("GenerateRefreshToken").Return("new-refresh", nil)
	jwt.On("HashToken", "new-refresh").Return("new-refresh")

	stored := &user.RefreshToken{
		ID: 10, UserID: 1, TokenHash: "raced",
		ExpiresAt: time.Now().Add(testRefreshExpiry),
		FamilyID:  "fam-1", User: activeUser(),
	}
	repo.On("GetByHash", ctx, "raced").Return(stored, nil)
	repo.On("Rotate", ctx, "raced", mock.Anything).Return(user.ErrRefreshTokenRotated)

	res, err := uc.Execute(ctx, "raced")

	assert.Nil(t, res)
	assert.ErrorIs(t, err, ErrInvalidRefreshToken)
	repo.AssertNotCalled(t, "RevokeFamily", mock.Anything, mock.Anything)
}

// TestRefresh_LegacyRevokedTokenNeverNukesEmptyFamily proves a legacy revoked
// token (FamilyID == "") presented as reuse is rejected WITHOUT calling
// RevokeFamily("") — that would log out unrelated users.
func TestRefresh_LegacyRevokedTokenNeverNukesEmptyFamily(t *testing.T) {
	repo := new(mockRefreshTokenRepo)
	jwt := new(mockTokenProvider)
	uc := newTestRefreshUC(repo, jwt)
	ctx := context.Background()

	jwt.On("HashToken", "legacy").Return("legacy")

	// Revoked long ago (outside grace), no ReplacedByHash and no family: a legacy
	// row. Must be rejected as a single token, no family nuke.
	revokedAt := time.Now().Add(-time.Hour)
	legacy := &user.RefreshToken{
		ID:        10,
		UserID:    1,
		TokenHash: "legacy",
		ExpiresAt: time.Now().Add(testRefreshExpiry),
		FamilyID:  "", // legacy pre-migration row
		RevokedAt: &revokedAt,
		User:      activeUser(),
	}
	repo.On("GetByHash", ctx, "legacy").Return(legacy, nil)

	res, err := uc.Execute(ctx, "legacy")

	assert.Nil(t, res)
	assert.ErrorIs(t, err, ErrInvalidRefreshToken)
	// The empty-family guard: RevokeFamily must never be called at all here, and
	// certainly never with "".
	repo.AssertNotCalled(t, "RevokeFamily", ctx, "")
	repo.AssertNotCalled(t, "RevokeFamily", mock.Anything, mock.Anything)
}

// TestRefresh_UnknownTokenRejected covers the missing-token path.
func TestRefresh_UnknownTokenRejected(t *testing.T) {
	repo := new(mockRefreshTokenRepo)
	jwt := new(mockTokenProvider)
	uc := newTestRefreshUC(repo, jwt)
	ctx := context.Background()

	jwt.On("HashToken", "ghost").Return("ghost")
	repo.On("GetByHash", ctx, "ghost").Return(nil, nil)

	res, err := uc.Execute(ctx, "ghost")
	assert.Nil(t, res)
	assert.ErrorIs(t, err, ErrInvalidRefreshToken)
}

// TestRefresh_ExpiredTokenRejected covers the expiry path.
func TestRefresh_ExpiredTokenRejected(t *testing.T) {
	repo := new(mockRefreshTokenRepo)
	jwt := new(mockTokenProvider)
	uc := newTestRefreshUC(repo, jwt)
	ctx := context.Background()

	jwt.On("HashToken", "old").Return("old")
	expired := &user.RefreshToken{
		TokenHash: "old",
		ExpiresAt: time.Now().Add(-time.Minute),
		FamilyID:  "fam-1",
		User:      activeUser(),
	}
	repo.On("GetByHash", ctx, "old").Return(expired, nil)

	res, err := uc.Execute(ctx, "old")
	assert.Nil(t, res)
	assert.ErrorIs(t, err, ErrInvalidRefreshToken)
	repo.AssertNotCalled(t, "Rotate", mock.Anything, mock.Anything, mock.Anything)
}

// TestRefresh_InactiveAccountRejected preserves the deactivated-account guard.
func TestRefresh_InactiveAccountRejected(t *testing.T) {
	repo := new(mockRefreshTokenRepo)
	jwt := new(mockTokenProvider)
	uc := newTestRefreshUC(repo, jwt)
	ctx := context.Background()

	jwt.On("HashToken", "valid").Return("valid")
	inactive := activeUser()
	inactive.IsActive = false
	stored := &user.RefreshToken{
		TokenHash: "valid",
		ExpiresAt: time.Now().Add(testRefreshExpiry),
		FamilyID:  "fam-1",
		User:      inactive,
	}
	repo.On("GetByHash", ctx, "valid").Return(stored, nil)

	res, err := uc.Execute(ctx, "valid")
	assert.Nil(t, res)
	assert.ErrorIs(t, err, ErrInactiveAccount)
	repo.AssertNotCalled(t, "Rotate", mock.Anything, mock.Anything, mock.Anything)
}
