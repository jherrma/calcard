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

// SCOPE: UserRepository.Count backs first-run setup. It must count soft-deleted
// accounts too, otherwise the last user deleting their account re-opens setup
// to anyone who can reach the server.
func TestUserRepoCount_IncludesSoftDeleted(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&user.User{}, &user.RefreshToken{}, &user.PasswordReset{}, &user.EmailVerification{}))
	repo := repository.NewUserRepository(db)
	ctx := context.Background()

	n, err := repo.Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), n)

	u := &user.User{UUID: "u1", Email: "a@example.com", Username: "a", PasswordHash: "x", IsActive: true}
	require.NoError(t, repo.Create(ctx, u))
	require.NoError(t, repo.Delete(ctx, u.ID))

	n, err = repo.Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), n, "a soft-deleted account must still count")
}
