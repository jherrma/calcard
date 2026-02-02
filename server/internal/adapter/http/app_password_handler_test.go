package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	authadapter "github.com/jherrma/caldav-server/internal/adapter/auth"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAppPasswordHandler_List(t *testing.T) {
	app, db, cfg := setupTestApp(t)
	userRepo := repository.NewUserRepository(db.DB())
	appPwdRepo := repository.NewAppPasswordRepository(db.DB())
	jwtManager := authadapter.NewJWTManager(&cfg.JWT)

	u := &user.User{
		Email:         "apppwd@example.com",
		Username:      "apppwduser",
		PasswordHash:  "hash",
		IsActive:      true,
		EmailVerified: true,
		UUID:          "apppwd-uuid",
	}
	err := userRepo.Create(context.Background(), u)
	require.NoError(t, err)

	token, _, err := jwtManager.GenerateAccessToken(u.UUID, u.Email)
	require.NoError(t, err)

	// Create an app password
	pwd := &user.AppPassword{
		UserID:       u.ID,
		UUID:         "pwd-uuid-1",
		Name:         "Test App Password",
		PasswordHash: "hash",
		Scopes:       `["caldav"]`,
		CreatedAt:    time.Now(),
	}
	err = appPwdRepo.Create(context.Background(), pwd)
	require.NoError(t, err)

	t.Run("Success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/app-passwords", nil)
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)

		var respData struct {
			Data struct {
				AppPasswords []struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"app_passwords"`
			} `json:"data"`
		}
		err = json.NewDecoder(resp.Body).Decode(&respData)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(respData.Data.AppPasswords), 1)
		assert.Equal(t, "Test App Password", respData.Data.AppPasswords[0].Name)
		assert.Equal(t, "pwd-uuid-1", respData.Data.AppPasswords[0].ID)
	})
}

func TestAppPasswordHandler_Revoke(t *testing.T) {
	app, db, cfg := setupTestApp(t)
	userRepo := repository.NewUserRepository(db.DB())
	appPwdRepo := repository.NewAppPasswordRepository(db.DB())
	jwtManager := authadapter.NewJWTManager(&cfg.JWT)

	u := &user.User{
		Email:         "revoke@example.com",
		Username:      "revokeuser",
		PasswordHash:  "hash",
		IsActive:      true,
		EmailVerified: true,
		UUID:          "revoke-uuid",
	}
	err := userRepo.Create(context.Background(), u)
	require.NoError(t, err)

	token, _, err := jwtManager.GenerateAccessToken(u.UUID, u.Email)
	require.NoError(t, err)

	// Create an app password
	pwd := &user.AppPassword{
		UserID:       u.ID,
		UUID:         "pwd-uuid-revoke",
		Name:         "To Be Revoked",
		PasswordHash: "hash",
		Scopes:       `["caldav"]`,
		CreatedAt:    time.Now(),
	}
	err = appPwdRepo.Create(context.Background(), pwd)
	require.NoError(t, err)

	t.Run("Success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/app-passwords/"+pwd.UUID, nil)
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusNoContent, resp.StatusCode)

		// Verify revoked (deleted)
		p, err := appPwdRepo.GetByUUID(context.Background(), pwd.UUID)
		if err != nil {
			assert.Contains(t, err.Error(), "record not found")
		} else {
			assert.Nil(t, p)
		}
	})
}
