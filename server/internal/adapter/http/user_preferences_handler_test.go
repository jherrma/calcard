package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	authadapter "github.com/jherrma/caldav-server/internal/adapter/auth"
	"github.com/jherrma/caldav-server/internal/adapter/http/dto"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserHandler_Preferences(t *testing.T) {
	app, db, cfg := setupTestApp(t)
	userRepo := repository.NewUserRepository(db.DB())
	prefRepo := repository.NewUserPreferenceRepository(db.DB())
	jwtManager := authadapter.NewJWTManager(&cfg.JWT)

	u := &user.User{
		Email:         "prefs@example.com",
		Username:      "prefs",
		PasswordHash:  "hash",
		IsActive:      true,
		EmailVerified: true,
		UUID:          "prefs-uuid",
	}
	require.NoError(t, userRepo.Create(context.Background(), u))

	token, _, err := jwtManager.GenerateAccessToken(u.UUID, u.Email)
	require.NoError(t, err)

	// A second user, to prove preferences never leak across accounts.
	other := &user.User{
		Email:         "other-prefs@example.com",
		Username:      "otherprefs",
		PasswordHash:  "hash",
		IsActive:      true,
		EmailVerified: true,
		UUID:          "other-prefs-uuid",
	}
	require.NoError(t, userRepo.Create(context.Background(), other))
	otherToken, _, err := jwtManager.GenerateAccessToken(other.UUID, other.Email)
	require.NoError(t, err)

	get := func(t *testing.T, bearer string) (*http.Response, map[string]string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/users/me/preferences", nil)
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		resp, err := app.Test(req)
		require.NoError(t, err)
		if resp.StatusCode != fiber.StatusOK {
			return resp, nil
		}
		var body struct {
			Data dto.PreferencesResponse `json:"data"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
		return resp, body.Data.Preferences
	}

	patch := func(t *testing.T, bearer string, payload any) (*http.Response, map[string]string, string) {
		t.Helper()
		raw, err := json.Marshal(payload)
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/users/me/preferences", bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+bearer)
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		var body struct {
			Data    dto.PreferencesResponse `json:"data"`
			Message string                  `json:"message"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
		return resp, body.Data.Preferences, body.Message
	}

	t.Run("GET returns defaults when nothing is set", func(t *testing.T) {
		resp, prefs := get(t, token)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)
		// Compared against the domain's own default map rather than a literal, so
		// adding a preference key does not fail this test for an unrelated reason.
		assert.Equal(t, user.DefaultPreferences(), prefs)
	})

	t.Run("GET unauthorized", func(t *testing.T) {
		resp, _ := get(t, "")
		assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("PATCH persists and returns the full map", func(t *testing.T) {
		resp, prefs, _ := patch(t, token, dto.UpdatePreferencesRequest{Preferences: map[string]string{
			user.PrefDefaultEventDuration: "30",
			user.PrefTimeFormat:           "12h",
		}})
		require.Equal(t, fiber.StatusOK, resp.StatusCode)
		want := user.DefaultPreferences()
		want[user.PrefDefaultEventDuration] = "30"
		want[user.PrefTimeFormat] = "12h"
		assert.Equal(t, want, prefs)

		// Verify the DB, then that a follow-up GET reads the same values back.
		stored, err := prefRepo.GetByKey(context.Background(), u.ID, user.PrefTimeFormat)
		require.NoError(t, err)
		require.NotNil(t, stored)
		assert.Equal(t, "12h", stored.Value)

		_, reread := get(t, token)
		assert.Equal(t, prefs, reread)
	})

	t.Run("PATCH twice upserts the same row", func(t *testing.T) {
		_, prefs, _ := patch(t, token, dto.UpdatePreferencesRequest{Preferences: map[string]string{
			user.PrefTimeFormat: "24h",
		}})
		assert.Equal(t, "24h", prefs[user.PrefTimeFormat])

		rows, err := prefRepo.GetByUserID(context.Background(), u.ID)
		require.NoError(t, err)
		count := 0
		for _, r := range rows {
			if r.Key == user.PrefTimeFormat {
				count++
			}
		}
		assert.Equal(t, 1, count, "upsert must not create a duplicate row")
	})

	t.Run("PATCH normalizes an accent colour end to end (story 046)", func(t *testing.T) {
		_, prefs, _ := patch(t, token, dto.UpdatePreferencesRequest{Preferences: map[string]string{
			user.PrefAccentColor: "#8B5CF6",
		}})
		assert.Equal(t, "#8b5cf6", prefs[user.PrefAccentColor])

		stored, err := prefRepo.GetByKey(context.Background(), u.ID, user.PrefAccentColor)
		require.NoError(t, err)
		require.NotNil(t, stored)
		assert.Equal(t, "#8b5cf6", stored.Value, "the row must hold the canonical form")

		_, reread := get(t, token)
		assert.Equal(t, "#8b5cf6", reread[user.PrefAccentColor])
	})

	t.Run("PATCH rejections", func(t *testing.T) {
		tests := []struct {
			name    string
			payload any
		}{
			{"unknown key", map[string]any{"preferences": map[string]string{"is_admin": "true"}}},
			{"bad duration", map[string]any{"preferences": map[string]string{user.PrefDefaultEventDuration: "37"}}},
			{"bad all-day", map[string]any{"preferences": map[string]string{user.PrefDefaultAllDay: "yes"}}},
			{"bad time format", map[string]any{"preferences": map[string]string{user.PrefTimeFormat: "military"}}},
			{"accent colour without a hash", map[string]any{"preferences": map[string]string{user.PrefAccentColor: "3b82f6"}}},
			{"accent colour as a CSS name", map[string]any{"preferences": map[string]string{user.PrefAccentColor: "rebeccapurple"}}},
			{"empty map", map[string]any{"preferences": map[string]string{}}},
			{"missing preferences object", map[string]any{}},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				resp, _, message := patch(t, token, tc.payload)
				assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
				assert.NotEmpty(t, message, "a 400 must explain what was wrong")
			})
		}
	})

	t.Run("PATCH is scoped to the caller", func(t *testing.T) {
		// The other user must still see defaults after the writes above.
		_, prefs := get(t, otherToken)
		assert.Equal(t, "60", prefs[user.PrefDefaultEventDuration])
		assert.Equal(t, "24h", prefs[user.PrefTimeFormat])

		_, otherPrefs, _ := patch(t, otherToken, dto.UpdatePreferencesRequest{Preferences: map[string]string{
			user.PrefDefaultAllDay: "true",
		}})
		assert.Equal(t, "true", otherPrefs[user.PrefDefaultAllDay])

		// ...and writing theirs must not have touched the first user's.
		_, mine := get(t, token)
		assert.Equal(t, "false", mine[user.PrefDefaultAllDay])
	})

	t.Run("PATCH unauthorized", func(t *testing.T) {
		raw, _ := json.Marshal(dto.UpdatePreferencesRequest{Preferences: map[string]string{
			user.PrefTimeFormat: "12h",
		}})
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/users/me/preferences", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode)
	})
}
