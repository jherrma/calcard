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
	"golang.org/x/crypto/bcrypt"
)

func TestUserHandler_GetProfile(t *testing.T) {
	app, db, cfg := setupTestApp(t)
	userRepo := repository.NewUserRepository(db.DB())
	jwtManager := authadapter.NewJWTManager(&cfg.JWT)

	// Create user
	u := &user.User{
		Email:         "profile@example.com",
		Username:      "profile",
		PasswordHash:  "hash",
		IsActive:      true,
		EmailVerified: true,
		DisplayName:   "Profile User",
		UUID:          "profile-uuid",
	}
	err := userRepo.Create(context.Background(), u)
	require.NoError(t, err)

	// Generate token
	token, _, err := jwtManager.GenerateAccessToken(u.UUID, u.Email)
	require.NoError(t, err)

	t.Run("Success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)

		var respData struct {
			Data dto.UserProfileResponse `json:"data"`
		}
		err = json.NewDecoder(resp.Body).Decode(&respData)
		require.NoError(t, err)
		assert.Equal(t, u.Email, respData.Data.Email)
		assert.Equal(t, u.DisplayName, respData.Data.DisplayName)
	})

	t.Run("Unauthorized", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode)
	})
}

func TestUserHandler_UpdateProfile(t *testing.T) {
	app, db, cfg := setupTestApp(t)
	userRepo := repository.NewUserRepository(db.DB())
	jwtManager := authadapter.NewJWTManager(&cfg.JWT)

	u := &user.User{
		Email:         "update@example.com",
		Username:      "update",
		PasswordHash:  "hash",
		IsActive:      true,
		EmailVerified: true,
		DisplayName:   "Old Name",
		UUID:          "update-uuid",
	}
	err := userRepo.Create(context.Background(), u)
	require.NoError(t, err)

	token, _, err := jwtManager.GenerateAccessToken(u.UUID, u.Email)
	require.NoError(t, err)

	t.Run("Success", func(t *testing.T) {
		payload := map[string]string{
			"display_name": "New Name",
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPatch, "/api/v1/users/me", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)

		// Verify DB
		updatedUser, err := userRepo.GetByID(context.Background(), u.ID)
		require.NoError(t, err)
		assert.Equal(t, "New Name", updatedUser.DisplayName)
	})
}

func TestUserHandler_ChangePassword(t *testing.T) {
	app, db, cfg := setupTestApp(t)
	userRepo := repository.NewUserRepository(db.DB())
	jwtManager := authadapter.NewJWTManager(&cfg.JWT)

	hash, _ := bcrypt.GenerateFromPassword([]byte("OldPass123!"), bcrypt.DefaultCost)
	u := &user.User{
		Email:         "changepw@example.com",
		Username:      "changepw",
		PasswordHash:  string(hash),
		IsActive:      true,
		EmailVerified: true,
		UUID:          "changepw-uuid",
	}
	err := userRepo.Create(context.Background(), u)
	require.NoError(t, err)

	token, _, err := jwtManager.GenerateAccessToken(u.UUID, u.Email)
	require.NoError(t, err)

	t.Run("Success", func(t *testing.T) {
		payload := map[string]string{
			"current_password": "OldPass123!",
			"new_password":     "NewPass123!",
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPut, "/api/v1/users/me/password", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)

		// Verify new password works
		updatedUser, err := userRepo.GetByID(context.Background(), u.ID)
		require.NoError(t, err)
		err = bcrypt.CompareHashAndPassword([]byte(updatedUser.PasswordHash), []byte("NewPass123!"))
		assert.NoError(t, err)
	})

	t.Run("Wrong Current Password", func(t *testing.T) {
		payload := map[string]string{
			"current_password": "WrongPass123!",
			"new_password":     "NewPass123!",
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPut, "/api/v1/users/me/password", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode)
	})
}

func TestUserHandler_DeleteAccount(t *testing.T) {
	app, db, cfg := setupTestApp(t)
	userRepo := repository.NewUserRepository(db.DB())
	jwtManager := authadapter.NewJWTManager(&cfg.JWT)

	hash, _ := bcrypt.GenerateFromPassword([]byte("Pass123!"), bcrypt.DefaultCost)
	u := &user.User{
		Email:         "delete@example.com",
		Username:      "delete",
		PasswordHash:  string(hash),
		IsActive:      true,
		EmailVerified: true,
		UUID:          "delete-uuid",
	}
	err := userRepo.Create(context.Background(), u)
	require.NoError(t, err)

	token, _, err := jwtManager.GenerateAccessToken(u.UUID, u.Email)
	require.NoError(t, err)

	t.Run("Success", func(t *testing.T) {
		payload := map[string]interface{}{
			"password":     "Pass123!",
			"confirmation": "DELETE", // Assuming confirmation string required? Check handler.
		}
		// Checking handler: DeleteAccountRequest has `Confirmation string`.
		// And `deleteAccountUC.Execute` checks `req.Confirmation`.
		// Usually confirmation string is "DELETE".

		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/users/me", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusNoContent, resp.StatusCode)

		// Verify soft deleted
		_, err = userRepo.GetByID(context.Background(), u.ID)
		// GetByID usually filters by deleted_at is null. So it should return nil or error?
		// Repo implementation: `Where("email = ? AND deleted_at IS NULL")` or similar.
		// `GetByID`: `First(&u, id)`. GORM default scope handles soft delete if `DeletedAt` field exists.
		// `User` struct has `gorm.Model`? No, it has `DeletedAt gorm.DeletedAt`.
		// So `First` should fail.
		// `userRepo.GetByID` returns `(*User, error)`.
		// If not found, it might return `(nil, nil)` or error?
		// In `auth_handler_test.go` logs we saw "record not found".
		// Let's assume it returns nil or error.
		// Wait, `GetByEmail` returned nil.
		// `GetByID`:
		// `if errors.Is(err, gorm.ErrRecordNotFound) { return nil, nil }`

		found, err := userRepo.GetByID(context.Background(), u.ID)
		require.NoError(t, err)
		assert.Nil(t, found)
	})
}
