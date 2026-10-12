package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newTokenMarkerRepo(t *testing.T) (user.UserRepository, *gorm.DB, *user.User) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(accountModels...)) // Delete purges every per-account table (F18)
	repo := repository.NewUserRepository(db)
	u := &user.User{UUID: "u1", Email: "a@example.com", Username: "a", PasswordHash: "x", IsActive: true}
	require.NoError(t, repo.Create(context.Background(), u))
	return repo, db, u
}

// SCOPE: P02 — the revocation marker must survive the database round trip at
// sub-second precision; a store that truncated it to whole seconds would
// quietly reopen the same-second window the iat_ns claim exists to close.
func TestInvalidateAccessTokens_RoundTripsSubSecond(t *testing.T) {
	repo, _, u := newTokenMarkerRepo(t)
	ctx := context.Background()

	fresh, err := repo.GetByUUID(ctx, u.UUID)
	require.NoError(t, err)
	assert.Nil(t, fresh.TokensValidAfter, "a new account has never revoked anything")

	at := time.Date(2026, 10, 11, 12, 0, 0, 123_456_789, time.FixedZone("CEST", 2*3600))
	require.NoError(t, repo.InvalidateAccessTokens(ctx, u.ID, at))

	got, err := repo.GetByUUID(ctx, u.UUID)
	require.NoError(t, err)
	require.NotNil(t, got.TokensValidAfter)
	assert.True(t, got.TokensValidAfter.Equal(at), "stored %v, want %v", got.TokensValidAfter, at)
	assert.False(t, got.AcceptsAccessTokenIssuedAt(at.Add(-time.Nanosecond)))
	assert.True(t, got.AcceptsAccessTokenIssuedAt(at.Add(time.Nanosecond)))
}

// A User loaded before an invalidation and saved after it (the auth middleware
// loads one per request, a profile update saves it) must not roll the marker
// back and resurrect the revoked tokens.
func TestUpdate_DoesNotRollBackTokenMarker(t *testing.T) {
	repo, _, u := newTokenMarkerRepo(t)
	ctx := context.Background()

	stale, err := repo.GetByUUID(ctx, u.UUID)
	require.NoError(t, err)

	at := time.Now()
	require.NoError(t, repo.InvalidateAccessTokens(ctx, u.ID, at))

	stale.DisplayName = "renamed"
	require.NoError(t, repo.Update(ctx, stale))

	got, err := repo.GetByUUID(ctx, u.UUID)
	require.NoError(t, err)
	assert.Equal(t, "renamed", got.DisplayName, "the rest of the row is still saved")
	require.NotNil(t, got.TokensValidAfter, "a stale Save must not clear the marker")
	assert.True(t, got.TokensValidAfter.Equal(at))
}

func TestDelete_InvalidatesAccessTokens(t *testing.T) {
	repo, db, u := newTokenMarkerRepo(t)
	before := time.Now()
	require.NoError(t, repo.Delete(context.Background(), u.ID))

	var deleted user.User
	require.NoError(t, db.Unscoped().First(&deleted, u.ID).Error)
	require.NotNil(t, deleted.TokensValidAfter)
	assert.False(t, deleted.TokensValidAfter.Before(before))
}
