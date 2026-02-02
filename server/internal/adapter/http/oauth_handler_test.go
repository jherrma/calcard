package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	authadapter "github.com/jherrma/caldav-server/internal/adapter/auth"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOAuthHandler_Lifecycle(t *testing.T) {
	app, db, cfg := setupTestApp(t)
	userRepo := repository.NewUserRepository(db.DB())
	jwtManager := authadapter.NewJWTManager(&cfg.JWT)
	connRepo := repository.NewOAuthConnectionRepository(db.DB())

	u := &user.User{
		Email:         "oauth_test@example.com",
		Username:      "oauthtest",
		PasswordHash:  "hash",
		IsActive:      true,
		EmailVerified: true,
		UUID:          "oauth-test-uuid",
	}
	require.NoError(t, userRepo.Create(context.Background(), u))

	token, _, err := jwtManager.GenerateAccessToken(u.UUID, u.Email)
	require.NoError(t, err)

	t.Run("Initiate", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oauth/google/initiate", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		// Fiber might return 302 or 303
		assert.True(t, resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusSeeOther, "Expected 302 or 303 status")
		assert.Contains(t, resp.Header.Get("Location"), "https://example.com/auth")
		assert.NotEmpty(t, resp.Cookies())
	})

	t.Run("List Providers", func(t *testing.T) {
		// Mock a connection first so it's not empty
		conn := &user.OAuthConnection{
			UserID:     u.ID,
			Provider:   "google",
			ProviderID: "fake-sub",
		}
		require.NoError(t, connRepo.Create(context.Background(), conn))

		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oauth/providers", nil)
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)

		var respData struct {
			Providers []struct {
				Provider string `json:"provider"`
				Email    string `json:"email"`
			} `json:"providers"`
			HasPassword bool `json:"has_password"`
		}
		err = json.NewDecoder(resp.Body).Decode(&respData)
		require.NoError(t, err)
		assert.Equal(t, "google", respData.Providers[0].Provider)
		assert.True(t, respData.HasPassword)
	})

	t.Run("Unlink", func(t *testing.T) {
		// Connection already exists from "List Providers" test
		// But let's create a second authentication method (password) so we can unlink.
		// Unlink UC checks if the user has other providers or a password.
		// In setupTestApp, u has a password "hash".

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/auth/oauth/google", nil)
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusNoContent, resp.StatusCode)

		// Verify deleted
		c, err := connRepo.GetByProvider(context.Background(), u.ID, "google")
		assert.NoError(t, err)
		assert.Nil(t, c)
	})
}
