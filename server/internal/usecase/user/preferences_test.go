package user

import (
	"context"
	"errors"
	"testing"

	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakePrefRepo is an in-memory UserPreferenceRepository. The upsert semantics
// (one row per user_id+key, later writes win) are the part under test, so a real
// map beats a call-recording mock here.
type fakePrefRepo struct {
	rows    []user.UserPreference
	getErr  error
	saveErr error
	upserts int
}

func (f *fakePrefRepo) GetByUserID(_ context.Context, userID uint) ([]user.UserPreference, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	var out []user.UserPreference
	for _, r := range f.rows {
		if r.UserID == userID {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakePrefRepo) GetByKey(_ context.Context, userID uint, key string) (*user.UserPreference, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	for i := range f.rows {
		if f.rows[i].UserID == userID && f.rows[i].Key == key {
			return &f.rows[i], nil
		}
	}
	return nil, nil
}

func (f *fakePrefRepo) Upsert(_ context.Context, pref *user.UserPreference) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.upserts++
	for i := range f.rows {
		if f.rows[i].UserID == pref.UserID && f.rows[i].Key == pref.Key {
			f.rows[i].Value = pref.Value
			return nil
		}
	}
	f.rows = append(f.rows, *pref)
	return nil
}

func (f *fakePrefRepo) Delete(_ context.Context, userID uint, key string) error {
	kept := f.rows[:0]
	for _, r := range f.rows {
		if r.UserID == userID && r.Key == key {
			continue
		}
		kept = append(kept, r)
	}
	f.rows = kept
	return nil
}

const prefUserUUID = "550e8400-e29b-41d4-a716-446655440000"

func prefTestUser() *user.User {
	return &user.User{ID: 7, UUID: prefUserUUID, Email: "prefs@example.com"}
}

func TestGetPreferencesUseCase_Execute(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name   string
		stored []user.UserPreference
		// want holds only the keys that should DIFFER from the defaults; the
		// assertion merges it onto user.DefaultPreferences(). Spelling out the
		// whole map made every one of these cases fail whenever a new preference
		// key was added, for a reason unrelated to what they test.
		want map[string]string
	}{
		{
			name:   "unset keys fall back to defaults",
			stored: nil,
			want:   nil,
		},
		{
			name: "stored values override defaults, untouched keys keep theirs",
			stored: []user.UserPreference{
				{UserID: 7, Key: user.PrefDefaultEventDuration, Value: "30"},
				{UserID: 7, Key: user.PrefTimeFormat, Value: "12h"},
			},
			want: map[string]string{
				user.PrefDefaultEventDuration: "30",
				user.PrefTimeFormat:           "12h",
			},
		},
		{
			name: "rows with unknown keys or stale values are ignored, not echoed",
			stored: []user.UserPreference{
				{UserID: 7, Key: "legacy_key_from_an_older_build", Value: "whatever"},
				{UserID: 7, Key: user.PrefDefaultEventDuration, Value: "37"}, // not in the allowed set
				{UserID: 7, Key: user.PrefDefaultAllDay, Value: "true"},
			},
			want: map[string]string{
				user.PrefDefaultAllDay: "true",
			},
		},
		{
			name: "a pattern-validated accent colour round-trips (story 046)",
			stored: []user.UserPreference{
				{UserID: 7, Key: user.PrefAccentColor, Value: "#8b5cf6"},
			},
			want: map[string]string{
				user.PrefAccentColor: "#8b5cf6",
			},
		},
		{
			name: "an accent colour stored uppercase by an older build still reads back",
			stored: []user.UserPreference{
				{UserID: 7, Key: user.PrefAccentColor, Value: "#8B5CF6"},
			},
			// Normalized on read as well as on write, so the user's choice
			// survives rather than silently reverting to the default blue.
			want: map[string]string{
				user.PrefAccentColor: "#8b5cf6",
			},
		},
		{
			name: "a malformed accent colour falls back to the default",
			stored: []user.UserPreference{
				{UserID: 7, Key: user.PrefAccentColor, Value: "rebeccapurple"},
			},
			want: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := new(mockUserRepo)
			repo.On("GetByUUID", ctx, prefUserUUID).Return(prefTestUser(), nil)
			prefRepo := &fakePrefRepo{rows: tc.stored}

			prefs, err := NewGetPreferencesUseCase(repo, prefRepo).Execute(ctx, prefUserUUID)

			require.NoError(t, err)
			want := user.DefaultPreferences()
			for k, v := range tc.want {
				want[k] = v
			}
			assert.Equal(t, want, prefs)
			repo.AssertExpectations(t)
		})
	}

	t.Run("missing user", func(t *testing.T) {
		repo := new(mockUserRepo)
		repo.On("GetByUUID", ctx, prefUserUUID).Return(nil, nil)

		prefs, err := NewGetPreferencesUseCase(repo, &fakePrefRepo{}).Execute(ctx, prefUserUUID)

		assert.Nil(t, prefs)
		assert.ErrorIs(t, err, ErrUserNotFound)
	})

	t.Run("repository error propagates", func(t *testing.T) {
		repo := new(mockUserRepo)
		repo.On("GetByUUID", ctx, prefUserUUID).Return(prefTestUser(), nil)
		boom := errors.New("db down")

		prefs, err := NewGetPreferencesUseCase(repo, &fakePrefRepo{getErr: boom}).Execute(ctx, prefUserUUID)

		assert.Nil(t, prefs)
		assert.ErrorIs(t, err, boom)
	})
}

func TestUpdatePreferencesUseCase_Rejections(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		updates map[string]string
		wantErr error
	}{
		{
			name:    "empty payload",
			updates: map[string]string{},
			wantErr: ErrNoPreferencesGiven,
		},
		{
			name:    "unknown key",
			updates: map[string]string{"is_admin": "true"},
			wantErr: ErrUnknownPreferenceKey,
		},
		{
			name:    "duration outside the allowed set",
			updates: map[string]string{user.PrefDefaultEventDuration: "37"},
			wantErr: ErrInvalidPreferenceValue,
		},
		{
			name:    "duration not even a number",
			updates: map[string]string{user.PrefDefaultEventDuration: "60; DROP TABLE users"},
			wantErr: ErrInvalidPreferenceValue,
		},
		{
			name:    "all-day is not a boolean string",
			updates: map[string]string{user.PrefDefaultAllDay: "yes"},
			wantErr: ErrInvalidPreferenceValue,
		},
		{
			name:    "time format is not 12h/24h",
			updates: map[string]string{user.PrefTimeFormat: "military"},
			wantErr: ErrInvalidPreferenceValue,
		},
		{
			name: "one bad value rejects the whole batch",
			updates: map[string]string{
				user.PrefTimeFormat:           "12h",
				user.PrefDefaultEventDuration: "999",
			},
			wantErr: ErrInvalidPreferenceValue,
		},
		{
			name:    "accent colour with no hash",
			updates: map[string]string{user.PrefAccentColor: "3b82f6"},
			wantErr: ErrInvalidPreferenceValue,
		},
		{
			name:    "accent colour as a CSS name",
			updates: map[string]string{user.PrefAccentColor: "rebeccapurple"},
			wantErr: ErrInvalidPreferenceValue,
		},
		{
			name: "accent colour carrying a CSS injection attempt",
			// The value ends up interpolated into a style property in the browser,
			// so the pattern is the boundary that keeps it a colour.
			updates: map[string]string{user.PrefAccentColor: "#3b82f6; background: url(evil)"},
			wantErr: ErrInvalidPreferenceValue,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// The user is never looked up and nothing is written: validation runs
			// before any repository call, so a rejected batch cannot half-apply.
			repo := new(mockUserRepo)
			prefRepo := &fakePrefRepo{}

			prefs, err := NewUpdatePreferencesUseCase(repo, prefRepo).Execute(ctx, prefUserUUID, tc.updates)

			assert.Nil(t, prefs)
			assert.ErrorIs(t, err, tc.wantErr)
			assert.Zero(t, prefRepo.upserts)
			assert.Empty(t, prefRepo.rows)
			repo.AssertExpectations(t)
		})
	}
}

func TestUpdatePreferencesUseCase_Execute(t *testing.T) {
	ctx := context.Background()

	t.Run("persists the given keys and returns the merged map", func(t *testing.T) {
		repo := new(mockUserRepo)
		repo.On("GetByUUID", ctx, prefUserUUID).Return(prefTestUser(), nil)
		prefRepo := &fakePrefRepo{}
		uc := NewUpdatePreferencesUseCase(repo, prefRepo)

		prefs, err := uc.Execute(ctx, prefUserUUID, map[string]string{
			user.PrefDefaultEventDuration: "30",
			user.PrefDefaultAllDay:        "true",
		})

		require.NoError(t, err)
		// Built from the defaults rather than spelled out, so adding a preference
		// key does not fail this test for a reason that has nothing to do with it.
		// The point being asserted is that the two written keys took, and every
		// untouched key kept its default.
		want := user.DefaultPreferences()
		want[user.PrefDefaultEventDuration] = "30"
		want[user.PrefDefaultAllDay] = "true"
		assert.Equal(t, want, prefs)
		assert.Equal(t, "24h", prefs[user.PrefTimeFormat], "an untouched key keeps its default")
		assert.Equal(t, 2, prefRepo.upserts)
		for _, r := range prefRepo.rows {
			assert.Equal(t, uint(7), r.UserID, "rows must be scoped to the caller")
		}
	})

	t.Run("stores an accent colour in canonical lowercase (story 046)", func(t *testing.T) {
		repo := new(mockUserRepo)
		repo.On("GetByUUID", ctx, prefUserUUID).Return(prefTestUser(), nil)
		prefRepo := &fakePrefRepo{}
		uc := NewUpdatePreferencesUseCase(repo, prefRepo)

		prefs, err := uc.Execute(ctx, prefUserUUID, map[string]string{
			user.PrefAccentColor: "  #8B5CF6 ",
		})

		require.NoError(t, err)
		// Normalized before validation AND before the write, so the row and the
		// response agree and the UI's "is this one of my presets?" comparison
		// cannot be defeated by casing.
		assert.Equal(t, "#8b5cf6", prefs[user.PrefAccentColor])
		require.Len(t, prefRepo.rows, 1)
		assert.Equal(t, "#8b5cf6", prefRepo.rows[0].Value, "the stored row must be canonical too")
	})

	t.Run("a second update overwrites instead of duplicating the row", func(t *testing.T) {
		repo := new(mockUserRepo)
		repo.On("GetByUUID", ctx, prefUserUUID).Return(prefTestUser(), nil)
		prefRepo := &fakePrefRepo{}
		uc := NewUpdatePreferencesUseCase(repo, prefRepo)

		_, err := uc.Execute(ctx, prefUserUUID, map[string]string{user.PrefTimeFormat: "12h"})
		require.NoError(t, err)
		prefs, err := uc.Execute(ctx, prefUserUUID, map[string]string{user.PrefTimeFormat: "24h"})
		require.NoError(t, err)

		assert.Equal(t, "24h", prefs[user.PrefTimeFormat])
		assert.Len(t, prefRepo.rows, 1)
	})

	t.Run("missing user", func(t *testing.T) {
		repo := new(mockUserRepo)
		repo.On("GetByUUID", ctx, prefUserUUID).Return(nil, nil)
		prefRepo := &fakePrefRepo{}

		prefs, err := NewUpdatePreferencesUseCase(repo, prefRepo).Execute(ctx, prefUserUUID,
			map[string]string{user.PrefTimeFormat: "12h"})

		assert.Nil(t, prefs)
		assert.ErrorIs(t, err, ErrUserNotFound)
		assert.Zero(t, prefRepo.upserts)
	})

	t.Run("write error propagates", func(t *testing.T) {
		repo := new(mockUserRepo)
		repo.On("GetByUUID", ctx, prefUserUUID).Return(prefTestUser(), nil)
		boom := errors.New("db down")

		prefs, err := NewUpdatePreferencesUseCase(repo, &fakePrefRepo{saveErr: boom}).
			Execute(ctx, prefUserUUID, map[string]string{user.PrefTimeFormat: "12h"})

		assert.Nil(t, prefs)
		assert.ErrorIs(t, err, boom)
	})
}
