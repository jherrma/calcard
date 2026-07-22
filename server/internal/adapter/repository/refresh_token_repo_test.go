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

func newRefreshTokenTestRepo(t *testing.T) (user.RefreshTokenRepository, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&user.User{}, &user.RefreshToken{}))
	return repository.NewRefreshTokenRepository(db), db
}

// TestRotate_AtomicallySupersedes verifies Rotate creates the successor and, in
// the same transaction, revokes the old row with ReplacedByHash set.
func TestRotate_AtomicallySupersedes(t *testing.T) {
	repo, db := newRefreshTokenTestRepo(t)
	ctx := context.Background()

	old := &user.RefreshToken{
		UserID:    1,
		TokenHash: "old-hash",
		ExpiresAt: time.Now().Add(time.Hour),
		FamilyID:  "fam-1",
	}
	require.NoError(t, repo.Create(ctx, old))

	successor := &user.RefreshToken{
		UserID:    1,
		TokenHash: "new-hash",
		ExpiresAt: time.Now().Add(time.Hour),
		FamilyID:  "fam-1",
	}
	require.NoError(t, repo.Rotate(ctx, "old-hash", successor))

	// Old row is revoked and points at its successor.
	var reloadedOld user.RefreshToken
	require.NoError(t, db.Where("token_hash = ?", "old-hash").First(&reloadedOld).Error)
	assert.NotNil(t, reloadedOld.RevokedAt, "old token must be revoked after rotation")
	assert.Equal(t, "new-hash", reloadedOld.ReplacedByHash)

	// Successor exists and is live.
	var reloadedNew user.RefreshToken
	require.NoError(t, db.Where("token_hash = ?", "new-hash").First(&reloadedNew).Error)
	assert.Nil(t, reloadedNew.RevokedAt)
	assert.Equal(t, "fam-1", reloadedNew.FamilyID)
}

// TestRevokeFamily_RevokesEntireLineage verifies every live token in a family is
// revoked while tokens in other families are untouched.
func TestRevokeFamily_RevokesEntireLineage(t *testing.T) {
	repo, db := newRefreshTokenTestRepo(t)
	ctx := context.Background()

	require.NoError(t, repo.Create(ctx, &user.RefreshToken{UserID: 1, TokenHash: "a", ExpiresAt: time.Now().Add(time.Hour), FamilyID: "fam-1"}))
	require.NoError(t, repo.Create(ctx, &user.RefreshToken{UserID: 1, TokenHash: "b", ExpiresAt: time.Now().Add(time.Hour), FamilyID: "fam-1"}))
	// A token in a DIFFERENT family belonging to a DIFFERENT user must survive.
	require.NoError(t, repo.Create(ctx, &user.RefreshToken{UserID: 2, TokenHash: "c", ExpiresAt: time.Now().Add(time.Hour), FamilyID: "fam-2"}))

	require.NoError(t, repo.RevokeFamily(ctx, "fam-1"))

	var a, b, c user.RefreshToken
	require.NoError(t, db.Where("token_hash = ?", "a").First(&a).Error)
	require.NoError(t, db.Where("token_hash = ?", "b").First(&b).Error)
	require.NoError(t, db.Where("token_hash = ?", "c").First(&c).Error)

	assert.NotNil(t, a.RevokedAt, "family member a must be revoked")
	assert.NotNil(t, b.RevokedAt, "family member b must be revoked")
	assert.Nil(t, c.RevokedAt, "unrelated family/user must NOT be revoked")
}

// TestRevokeFamily_RefusesEmptyFamily is the critical cross-user safety guard:
// legacy rows carry family_id='' and a blanket revoke of that value would log
// out unrelated users. RevokeFamily("") must error and touch nothing.
func TestRevokeFamily_RefusesEmptyFamily(t *testing.T) {
	repo, db := newRefreshTokenTestRepo(t)
	ctx := context.Background()

	// Two legacy tokens (empty family) belonging to different users.
	require.NoError(t, repo.Create(ctx, &user.RefreshToken{UserID: 1, TokenHash: "legacy-1", ExpiresAt: time.Now().Add(time.Hour), FamilyID: ""}))
	require.NoError(t, repo.Create(ctx, &user.RefreshToken{UserID: 2, TokenHash: "legacy-2", ExpiresAt: time.Now().Add(time.Hour), FamilyID: ""}))

	err := repo.RevokeFamily(ctx, "")
	assert.Error(t, err, "RevokeFamily must refuse an empty family id")

	// Neither legacy token was revoked.
	var l1, l2 user.RefreshToken
	require.NoError(t, db.Where("token_hash = ?", "legacy-1").First(&l1).Error)
	require.NoError(t, db.Where("token_hash = ?", "legacy-2").First(&l2).Error)
	assert.Nil(t, l1.RevokedAt)
	assert.Nil(t, l2.RevokedAt)
}
