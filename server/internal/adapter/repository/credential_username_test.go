package repository_test

import (
	"context"
	"testing"

	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func credTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&user.User{}, &user.CalDAVCredential{}, &user.CardDAVCredential{}))
	require.NoError(t, db.Create(&user.User{ID: 1, UUID: "u1", Email: "a@example.com", Username: "a", PasswordHash: "x"}).Error)
	return db
}

// SCOPE (F24): the username column's unique index also covers revoked rows,
// while the create-time duplicate check skips them. Revoke must therefore
// release the name, and an insert the index still rejects must come back as
// ErrCredentialUsernameTaken rather than the raw driver error.
func TestCalDAVCredentialRepo_RevokeReleasesUsername(t *testing.T) {
	repo := repository.NewCalDAVCredentialRepository(credTestDB(t))
	ctx := context.Background()

	first := &user.CalDAVCredential{UUID: "c1", UserID: 1, Name: "one", Username: "shared-name", PasswordHash: "x", Permission: "read"}
	require.NoError(t, repo.Create(ctx, first))

	dup := &user.CalDAVCredential{UUID: "c2", UserID: 1, Name: "dup", Username: "shared-name", PasswordHash: "x", Permission: "read"}
	assert.ErrorIs(t, repo.Create(ctx, dup), user.ErrCredentialUsernameTaken)

	require.NoError(t, repo.Revoke(ctx, first.ID))
	got, err := repo.GetByUsername(ctx, "shared-name")
	assert.Error(t, err, "a revoked credential must not be found by its old name")
	assert.Nil(t, got)

	again := &user.CalDAVCredential{UUID: "c3", UserID: 1, Name: "again", Username: "shared-name", PasswordHash: "x", Permission: "read"}
	require.NoError(t, repo.Create(ctx, again), "the revoked credential's username must be reusable")
	got, err = repo.GetByUsername(ctx, "shared-name")
	require.NoError(t, err)
	assert.Equal(t, "c3", got.UUID)
}

func TestCardDAVCredentialRepo_RevokeReleasesUsername(t *testing.T) {
	db := credTestDB(t)
	repo := repository.NewCardDAVCredentialRepository(db)
	ctx := context.Background()

	first := &user.CardDAVCredential{UUID: "d1", UserID: 1, Name: "one", Username: "shared-name", PasswordHash: "x", Permission: "read"}
	require.NoError(t, repo.Create(ctx, first))

	dup := &user.CardDAVCredential{UUID: "d2", UserID: 1, Name: "dup", Username: "shared-name", PasswordHash: "x", Permission: "read"}
	assert.ErrorIs(t, repo.Create(ctx, dup), user.ErrCredentialUsernameTaken)

	require.NoError(t, repo.Revoke(ctx, first.ID))
	var stored user.CardDAVCredential
	require.NoError(t, db.Where("uuid = ?", "d1").First(&stored).Error)
	assert.Equal(t, user.RevokedCredentialUsernamePrefix+"d1", stored.Username)
	assert.NotNil(t, stored.RevokedAt)

	again := &user.CardDAVCredential{UUID: "d3", UserID: 1, Name: "again", Username: "shared-name", PasswordHash: "x", Permission: "read"}
	require.NoError(t, repo.Create(ctx, again), "the revoked credential's username must be reusable")
	got, err := repo.GetByUsername(ctx, "shared-name")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "d3", got.UUID)
}
