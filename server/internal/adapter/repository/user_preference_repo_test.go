package repository_test

import (
	"context"
	"sync"
	"testing"

	"github.com/jherrma/caldav-server/internal/adapter/repository"

	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserPreferenceRepo_InsertIfAbsentAndReplaceValueIf(t *testing.T) {
	db := openAccountDB(t)
	repo := repository.NewUserPreferenceRepository(db)
	ctx := context.Background()
	u := &user.User{UUID: "tz-u", Email: "tz@example.test", Username: "tz", PasswordHash: "x", IsActive: true}
	require.NoError(t, db.Create(u).Error)

	var wg sync.WaitGroup
	for _, z := range []string{"Europe/Berlin", "Asia/Tokyo"} {
		wg.Add(1)
		go func(z string) {
			defer wg.Done()
			// SQLite may answer "database is locked" to the loser; the row
			// count below is what matters.
			_ = repo.InsertIfAbsent(ctx, &user.UserPreference{UserID: u.ID, Key: user.PrefTimezone, Value: z})
		}(z)
	}
	wg.Wait()
	rows, err := repo.GetByUserID(ctx, u.ID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	first := rows[0].Value

	require.NoError(t, repo.InsertIfAbsent(ctx, &user.UserPreference{UserID: u.ID, Key: user.PrefTimezone, Value: "UTC"}))
	got, _ := repo.GetByKey(ctx, u.ID, user.PrefTimezone)
	assert.Equal(t, first, got.Value)

	ok, err := repo.ReplaceValueIf(ctx, u.ID, user.PrefTimezone, "something-else", "UTC")
	require.NoError(t, err)
	assert.False(t, ok)
	ok, err = repo.ReplaceValueIf(ctx, u.ID, user.PrefTimezone, first, "UTC")
	require.NoError(t, err)
	assert.True(t, ok)
	got, _ = repo.GetByKey(ctx, u.ID, user.PrefTimezone)
	assert.Equal(t, "UTC", got.Value)
}
