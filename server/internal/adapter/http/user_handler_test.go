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

	// Create user. Mixed case like a server-generated username: the profile
	// must echo it exactly as stored, because it is the /dav/{username}/
	// segment, which the DAV handler compares case-sensitively.
	u := &user.User{
		Email:         "profile@example.com",
		Username:      "ProfileUserXyZ",
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
		assert.Equal(t, "ProfileUserXyZ", respData.Data.Username)
	})

	t.Run("UsernameIsInTheJSON", func(t *testing.T) {
		// Decode into a map, not the DTO: a struct field without its json tag
		// (or with omitempty on an empty value) would still pass the typed
		// check above while the SPA reads data.username and gets nothing.
		req := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := app.Test(req)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusOK, resp.StatusCode)

		var raw struct {
			Data map[string]any `json:"data"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&raw))
		assert.Equal(t, "ProfileUserXyZ", raw.Data["username"])
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

		// PATCH answers with the same profile shape as GET, username included.
		var respData struct {
			Data dto.UserProfileResponse `json:"data"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&respData))
		assert.Equal(t, "New Name", respData.Data.DisplayName)
		assert.Equal(t, u.Username, respData.Data.Username)

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

	t.Run("WeakPassword", func(t *testing.T) {
		// Use a dedicated user so this case does not depend on whether the
		// "Success" subtest above has already rotated the shared user's password.
		weakHash, _ := bcrypt.GenerateFromPassword([]byte("OldPass123!"), bcrypt.DefaultCost)
		weakUser := &user.User{
			Email:         "changepw-weak@example.com",
			Username:      "changepw-weak",
			PasswordHash:  string(weakHash),
			IsActive:      true,
			EmailVerified: true,
			UUID:          "changepw-weak-uuid",
		}
		require.NoError(t, userRepo.Create(context.Background(), weakUser))
		weakToken, _, err := jwtManager.GenerateAccessToken(weakUser.UUID, weakUser.Email)
		require.NoError(t, err)

		payload := map[string]string{
			"current_password": "OldPass123!",
			"new_password":     "a",
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPut, "/api/v1/users/me/password", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+weakToken)
		req.Header.Set("Content-Type", "application/json")

		resp, err := app.Test(req)
		require.NoError(t, err)
		// The policy sentinel must surface as a 400 with its actionable message,
		// not be swallowed into a generic 500.
		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)

		var respData struct {
			Message string `json:"message"`
		}
		err = json.NewDecoder(resp.Body).Decode(&respData)
		require.NoError(t, err)
		assert.Contains(t, respData.Message, "at least 8 characters")

		// The account must not have been touched on the rejected path.
		unchanged, err := userRepo.GetByID(context.Background(), weakUser.ID)
		require.NoError(t, err)
		assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(unchanged.PasswordHash), []byte("OldPass123!")))
	})

	t.Run("SamePassword", func(t *testing.T) {
		// Dedicated user with a live token, so neither the 400 nor the 401
		// below can come from a session "Success" has already revoked.
		sameHash, _ := bcrypt.GenerateFromPassword([]byte("OldPass123!"), bcrypt.DefaultCost)
		sameUser := &user.User{
			Email:         "changepw-same@example.com",
			Username:      "changepw-same",
			PasswordHash:  string(sameHash),
			IsActive:      true,
			EmailVerified: true,
			UUID:          "changepw-same-uuid",
		}
		require.NoError(t, userRepo.Create(context.Background(), sameUser))
		sameToken, _, err := jwtManager.GenerateAccessToken(sameUser.UUID, sameUser.Email)
		require.NoError(t, err)

		changePassword := func(current, next string) (int, string) {
			body, _ := json.Marshal(map[string]string{"current_password": current, "new_password": next})
			req := httptest.NewRequest(http.MethodPut, "/api/v1/users/me/password", bytes.NewReader(body))
			req.Header.Set("Authorization", "Bearer "+sameToken)
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			require.NoError(t, err)
			var respData struct {
				Message string `json:"message"`
			}
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&respData))
			return resp.StatusCode, respData.Message
		}

		// new == current is a validation error, not an authentication failure
		// (F10): a 401 makes the SPA refresh its session and replay the request.
		status, msg := changePassword("OldPass123!", "OldPass123!")
		assert.Equal(t, fiber.StatusBadRequest, status)
		assert.Equal(t, "new password cannot be the same as the current password", msg)

		// A wrong current password stays a 401, also when new == current.
		status, msg = changePassword("WrongPass123!", "WrongPass123!")
		assert.Equal(t, fiber.StatusUnauthorized, status)
		assert.Equal(t, "current password is incorrect", msg)

		// Neither refusal touched the password or revoked the session.
		unchanged, err := userRepo.GetByID(context.Background(), sameUser.ID)
		require.NoError(t, err)
		assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(unchanged.PasswordHash), []byte("OldPass123!")))
		req := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
		req.Header.Set("Authorization", "Bearer "+sameToken)
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)
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
