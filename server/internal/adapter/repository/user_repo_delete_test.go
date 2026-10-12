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

// SCOPE: F19 — deleting an account must release its e-mail, username and OAuth
// identity. The row is only soft-deleted and the unique indexes cover it, so
// before the tombstone a re-registration failed with "UNIQUE constraint
// failed: users.email" (400 "registration failed" to the client).
func TestUserRepoDelete_ReleasesIdentity(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(accountModels...)) // Delete purges every per-account table (F18)
	users := repository.NewUserRepository(db)
	oauth := repository.NewOAuthConnectionRepository(db)
	ctx := context.Background()

	old := &user.User{UUID: "uuid-old", Email: "back@example.com", Username: "backagain", PasswordHash: "x", IsActive: true}
	require.NoError(t, users.Create(ctx, old))
	require.NoError(t, oauth.Create(ctx, &user.OAuthConnection{UserID: old.ID, Provider: "google", ProviderID: "sub-1"}))
	require.NoError(t, users.Delete(ctx, old.ID))

	var row user.User
	require.NoError(t, db.Unscoped().First(&row, old.ID).Error)
	assert.True(t, row.DeletedAt.Valid, "still a soft delete")
	assert.Equal(t, "deleted:uuid-old", row.Email)
	assert.Equal(t, "deleted:uuid-old", row.Username)

	n, err := users.Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), n, "a deleted account must keep counting, or first-run setup re-opens")

	got, err := users.GetByOAuth(ctx, "google", "sub-1")
	require.NoError(t, err)
	assert.Nil(t, got, "the provider identity must not resolve to the deleted account")

	again := &user.User{UUID: "uuid-new", Email: "back@example.com", Username: "backagain", PasswordHash: "x", IsActive: true}
	require.NoError(t, users.Create(ctx, again), "the e-mail and username of a deleted account must be reusable")
	require.NoError(t, oauth.Create(ctx, &user.OAuthConnection{UserID: again.ID, Provider: "google", ProviderID: "sub-1"}),
		"the provider identity of a deleted account must be linkable again")

	got, err = users.GetByEmail(ctx, "back@example.com")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "uuid-new", got.UUID)
}
