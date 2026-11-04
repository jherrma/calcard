package user

import (
	"context"
	"errors"
	"testing"

	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTZUC(rows []user.UserPreference) (*SetDefaultTimezoneUseCase, *fakePrefRepo) {
	repo := new(mockUserRepo)
	repo.On("GetByUUID", context.Background(), prefUserUUID).Return(prefTestUser(), nil)
	prefs := &fakePrefRepo{rows: rows}
	return NewSetDefaultTimezoneUseCase(repo, prefs), prefs
}

func TestSetDefaultTimezoneUseCase(t *testing.T) {
	ctx := context.Background()

	t.Run("first call stores, second returns the first", func(t *testing.T) {
		uc, prefs := newTZUC(nil)
		got, err := uc.Execute(ctx, prefUserUUID, " Europe/Berlin ")
		require.NoError(t, err)
		assert.Equal(t, "Europe/Berlin", got)
		got, err = uc.Execute(ctx, prefUserUUID, "America/New_York")
		require.NoError(t, err)
		assert.Equal(t, "Europe/Berlin", got)
		assert.Len(t, prefs.rows, 1)
	})

	t.Run("an explicit value, even UTC, is kept", func(t *testing.T) {
		uc, _ := newTZUC([]user.UserPreference{{UserID: 7, Key: user.PrefTimezone, Value: "UTC"}})
		got, err := uc.Execute(ctx, prefUserUUID, "Asia/Tokyo")
		require.NoError(t, err)
		assert.Equal(t, "UTC", got)
	})

	t.Run("an invalid stored row is replaced", func(t *testing.T) {
		uc, prefs := newTZUC([]user.UserPreference{{UserID: 7, Key: user.PrefTimezone, Value: "Mars/Base"}})
		got, err := uc.Execute(ctx, prefUserUUID, "Asia/Tokyo")
		require.NoError(t, err)
		assert.Equal(t, "Asia/Tokyo", got)
		assert.Equal(t, "Asia/Tokyo", prefs.rows[0].Value)
	})

	t.Run("invalid input never reaches the repository", func(t *testing.T) {
		for _, bad := range []string{"", "Local", "Europe/Nowhere", "../etc/passwd"} {
			uc, prefs := newTZUC(nil)
			_, err := uc.Execute(ctx, prefUserUUID, bad)
			require.ErrorIs(t, err, ErrInvalidPreferenceValue, "%q", bad)
			assert.Equal(t, `invalid preference value: "timezone" must be an IANA time zone name such as Europe/Berlin`, err.Error())
			assert.Zero(t, prefs.inserts)
			assert.Empty(t, prefs.rows)
		}
	})

	t.Run("unknown user", func(t *testing.T) {
		repo := new(mockUserRepo)
		repo.On("GetByUUID", ctx, "gone").Return(nil, nil)
		uc := NewSetDefaultTimezoneUseCase(repo, &fakePrefRepo{})
		_, err := uc.Execute(ctx, "gone", "Europe/Berlin")
		assert.True(t, errors.Is(err, ErrUserNotFound))
	})
}
