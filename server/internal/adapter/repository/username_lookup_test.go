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

func newUsernameTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&user.User{}, &user.CardDAVCredential{}))
	return db
}

// F02: generated usernames are mixed-case (e.g. wTYypuRvIogjtyAt) and are what
// clients are told to log in with; the lookup used to lower-case its input
// before an exact match, so such a user could never be found.
func TestUserRepoGetByUsername_CaseInsensitive(t *testing.T) {
	db := newUsernameTestDB(t)
	repo := repository.NewUserRepository(db)
	ctx := context.Background()

	mixed := &user.User{UUID: "u1", Email: "mixed@example.com", Username: "wTYypuRvIogjtyAt", PasswordHash: "x", IsActive: true}
	lower := &user.User{UUID: "u2", Email: "admin@example.com", Username: "admin", PasswordHash: "x", IsActive: true}
	require.NoError(t, repo.Create(ctx, mixed))
	require.NoError(t, repo.Create(ctx, lower))

	for _, in := range []string{"wTYypuRvIogjtyAt", "wtyypurviogjtyat", "WTYYPURVIOGJTYAT", " wTYypuRvIogjtyAt "} {
		got, err := repo.GetByUsername(ctx, in)
		require.NoError(t, err)
		if assert.NotNil(t, got, "lookup %q", in) {
			assert.Equal(t, mixed.ID, got.ID)
		}
	}

	// A lower-case stored name (the set-up admin) still matches any spelling,
	// as it did when the input was lower-cased.
	got, err := repo.GetByUsername(ctx, "Admin")
	require.NoError(t, err)
	if assert.NotNil(t, got) {
		assert.Equal(t, lower.ID, got.ID)
	}

	got, err = repo.GetByUsername(ctx, "nobody")
	require.NoError(t, err)
	assert.Nil(t, got)
}

// The unique index is case-sensitive, so legacy rows may differ only in case.
// The exact spelling then wins; any other spelling is ambiguous and must not
// pick one of them.
func TestUserRepoGetByUsername_AmbiguousCase(t *testing.T) {
	db := newUsernameTestDB(t)
	repo := repository.NewUserRepository(db)
	ctx := context.Background()

	a := &user.User{UUID: "a", Email: "a@example.com", Username: "SameName", PasswordHash: "x", IsActive: true}
	b := &user.User{UUID: "b", Email: "b@example.com", Username: "samename", PasswordHash: "x", IsActive: true}
	require.NoError(t, repo.Create(ctx, a))
	require.NoError(t, repo.Create(ctx, b))

	got, err := repo.GetByUsername(ctx, "SameName")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, a.ID, got.ID)

	got, err = repo.GetByUsername(ctx, "samename")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, b.ID, got.ID)

	got, err = repo.GetByUsername(ctx, "SAMENAME")
	require.NoError(t, err)
	assert.Nil(t, got, "an ambiguous case-insensitive match must not resolve to either user")
}

// F02: the create use case stores a CardDAV credential username as given
// ([a-zA-Z0-9_-]); the lookup lower-cased it, so a mixed-case credential could
// never authenticate.
func TestCardDAVCredentialRepoGetByUsername_CaseInsensitive(t *testing.T) {
	db := newUsernameTestDB(t)
	repo := repository.NewCardDAVCredentialRepository(db)
	ctx := context.Background()

	cred := &user.CardDAVCredential{UUID: "c1", UserID: 1, Name: "n", Username: "Mixed-Card", PasswordHash: "x", Permission: "read", CreatedAt: time.Now()}
	require.NoError(t, repo.Create(ctx, cred))

	for _, in := range []string{"Mixed-Card", "mixed-card", "MIXED-CARD"} {
		got, err := repo.GetByUsername(ctx, in)
		require.NoError(t, err)
		if assert.NotNil(t, got, "lookup %q", in) {
			assert.Equal(t, cred.ID, got.ID)
		}
	}

	require.NoError(t, repo.Revoke(ctx, cred.ID))
	got, err := repo.GetByUsername(ctx, "Mixed-Card")
	require.NoError(t, err)
	assert.Nil(t, got, "a revoked credential must not be found")
}
