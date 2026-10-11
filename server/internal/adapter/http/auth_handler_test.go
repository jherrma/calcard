package http

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	authadapter "github.com/jherrma/caldav-server/internal/adapter/auth"
	"github.com/jherrma/caldav-server/internal/adapter/http/dto"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

func TestAuthHandler_Register(t *testing.T) {
	app, db, _ := setupTestApp(t)
	userRepo := repository.NewUserRepository(db.DB())

	t.Run("Success", func(t *testing.T) {
		payload := map[string]string{
			"email":        "newuser@example.com",
			"password":     "Pass123!",
			"display_name": "New User",
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)

		// Verify user created in DB
		u, err := userRepo.GetByEmail(context.Background(), "newuser@example.com")
		require.NoError(t, err)
		assert.Equal(t, "newuser@example.com", u.Email)
		assert.True(t, u.EmailVerified) // Default should be true when SMTP is disabled
	})

	t.Run("Duplicate Email", func(t *testing.T) {
		// Create a user first
		u := &user.User{
			Email:        "existing@example.com",
			Username:     "existing",
			PasswordHash: "hash",
			IsActive:     true,
			UUID:         "existing-uuid",
		}
		err := userRepo.Create(context.Background(), u)
		require.NoError(t, err)

		payload := map[string]string{
			"email":        "existing@example.com",
			"password":     "Pass123!",
			"display_name": "Existing User",
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusConflict, resp.StatusCode)
	})
}

// P01: with SMTP configured, POST /auth/register must answer a taken address
// exactly like a free one, and login must not tell "the account you just
// registered is unconfirmed" from "wrong password" — together those two
// responses were an account-existence oracle.
func TestAuthHandler_Register_NoEnumerationWithSMTP(t *testing.T) {
	app, db, cfg := setupTestApp(t)
	// The use case and handler share cfg, so this switches both to "SMTP on".
	// The email service was built from the empty SMTP config and stays a no-op.
	cfg.SMTP.Host = "smtp.invalid"
	userRepo := repository.NewUserRepository(db.DB())

	hash, err := bcrypt.GenerateFromPassword([]byte("Owner123!"), bcrypt.MinCost)
	require.NoError(t, err)
	require.NoError(t, userRepo.Create(context.Background(), &user.User{
		UUID: "owner-uuid", Email: "owner@example.com", Username: "owneruser",
		PasswordHash: string(hash), IsActive: true, EmailVerified: true,
	}))

	register := func(email, password string) (int, []byte) {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"email": email, "password": password, "display_name": "Probe"})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req, fiber.TestConfig{Timeout: 10 * time.Second})
		require.NoError(t, err)
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		return resp.StatusCode, b
	}
	login := func(email, password string) (int, []byte) {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"email": email, "password": password})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req, fiber.TestConfig{Timeout: 10 * time.Second})
		require.NoError(t, err)
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		return resp.StatusCode, b
	}

	// Same address casing on both sides so the bodies can be compared byte
	// for byte: the only request-derived fields are email and display_name.
	freshStatus, freshBody := register("fresh@example.com", "Probe123!")
	takenStatus, takenBody := register("owner@example.com", "Probe123!")

	assert.Equal(t, fiber.StatusOK, freshStatus)
	assert.Equal(t, freshStatus, takenStatus, "status must not reveal a taken address")
	assert.Equal(t,
		strings.Replace(string(freshBody), "fresh@example.com", "X", 1),
		strings.Replace(string(takenBody), "owner@example.com", "X", 1),
		"body must not reveal a taken address")
	for _, leak := range []string{`"id"`, `"username"`, `"created_at"`} {
		assert.NotContains(t, string(freshBody), leak, "pending registrations carry no stored-row fields")
	}

	// The taken address kept its account and password.
	owner, err := userRepo.GetByEmail(context.Background(), "owner@example.com")
	require.NoError(t, err)
	assert.Equal(t, "owneruser", owner.Username)
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(owner.PasswordHash), []byte("Owner123!")))

	// Second step of the oracle: log in with the password just submitted.
	// fresh -> account exists but is unconfirmed; owner -> wrong password.
	pendingStatus, pendingBody := login("fresh@example.com", "Probe123!")
	wrongStatus, wrongBody := login("owner@example.com", "Probe123!")
	assert.Equal(t, fiber.StatusUnauthorized, pendingStatus)
	assert.Equal(t, pendingStatus, wrongStatus)
	assert.Equal(t, string(pendingBody), string(wrongBody), "login must not tell unconfirmed from wrong password")
}

func TestAuthHandler_Login(t *testing.T) {
	app, db, _ := setupTestApp(t)
	userRepo := repository.NewUserRepository(db.DB())

	// Create a user manually
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte("Pass123!"), bcrypt.DefaultCost)
	require.NoError(t, err)

	u := &user.User{
		Email:         "loginuser@example.com",
		Username:      "loginuser",
		PasswordHash:  string(hashedPassword),
		IsActive:      true,
		EmailVerified: true,
		UUID:          "login-uuid",
	}
	err = userRepo.Create(context.Background(), u)
	require.NoError(t, err)

	t.Run("Success", func(t *testing.T) {
		payload := map[string]string{
			"email":    "loginuser@example.com",
			"password": "Pass123!",
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)

		var respData struct {
			Data dto.LoginResponse `json:"data"`
		}
		err = json.NewDecoder(resp.Body).Decode(&respData)
		require.NoError(t, err)
		res := respData.Data
		assert.NotEmpty(t, res.AccessToken)
		assert.NotEmpty(t, res.RefreshToken)
		assert.Equal(t, u.Email, res.User.Email)
	})

	t.Run("Invalid Password", func(t *testing.T) {
		payload := map[string]string{
			"email":    "loginuser@example.com",
			"password": "wrongpassword",
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode)
	})
}

func TestAuthHandler_ForgotPassword(t *testing.T) {
	app, _, _ := setupTestApp(t)

	t.Run("Success", func(t *testing.T) {
		payload := map[string]string{
			"email": "anyemail@example.com", // Doesn't matter if exists, should verify always 200
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/forgot-password", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)
	})
}

func TestAuthHandler_Verify(t *testing.T) {
	app, db, _ := setupTestApp(t)
	userRepo := repository.NewUserRepository(db.DB())

	// Create inactive user
	u := &user.User{
		Email:        "verify@example.com",
		Username:     "verify",
		IsActive:     false,
		UUID:         "verify-uuid",
		PasswordHash: "hash",
	}
	err := userRepo.Create(context.Background(), u)
	require.NoError(t, err)

	// Create verification token. Tokens are stored hashed (SHA-256 hex); the
	// raw token "valid-token" is what the client presents in the URL.
	rawToken := "valid-token"
	tokenSum := sha256.Sum256([]byte(rawToken))
	v := &user.EmailVerification{
		UserID:    u.ID,
		Token:     hex.EncodeToString(tokenSum[:]),
		ExpiresAt: time.Now().Add(time.Hour),
	}
	err = userRepo.CreateVerification(context.Background(), v)
	require.NoError(t, err)

	t.Run("Success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/verify?token=valid-token", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)

		// Verify active
		u, err := userRepo.GetByID(context.Background(), u.ID)
		require.NoError(t, err)
		assert.True(t, u.IsActive)
	})

	t.Run("Invalid Token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/verify?token=invalid", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	})
}

func TestAuthHandler_Refresh(t *testing.T) {
	app, db, cfg := setupTestApp(t)
	tokenRepo := repository.NewRefreshTokenRepository(db.DB())
	userRepo := repository.NewUserRepository(db.DB())
	jwtManager := authadapter.NewJWTManager(&cfg.JWT)

	// Create user
	u := &user.User{
		Email:        "refresh@example.com",
		Username:     "refresh",
		PasswordHash: "hash",
		IsActive:     true,
		UUID:         "refresh-uuid",
	}
	err := userRepo.Create(context.Background(), u)
	require.NoError(t, err)

	// Generate refresh token
	token, err := jwtManager.GenerateRefreshToken()
	require.NoError(t, err)
	hash := jwtManager.HashToken(token)

	err = tokenRepo.Create(context.Background(), &user.RefreshToken{
		TokenHash: hash,
		UserID:    u.ID,
		ExpiresAt: time.Now().Add(time.Hour),
	})
	require.NoError(t, err)

	t.Run("Success", func(t *testing.T) {
		payload := map[string]string{
			"refresh_token": token,
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)

		var respData struct {
			Data map[string]interface{} `json:"data"`
		}
		json.NewDecoder(resp.Body).Decode(&respData)
		res := respData.Data
		assert.NotEmpty(t, res["access_token"])
	})
}

func TestAuthHandler_Logout(t *testing.T) {
	app, db, cfg := setupTestApp(t)
	tokenRepo := repository.NewRefreshTokenRepository(db.DB())
	userRepo := repository.NewUserRepository(db.DB())
	jwtManager := authadapter.NewJWTManager(&cfg.JWT)

	// Create user
	u := &user.User{
		Email:        "logout@example.com",
		Username:     "logout",
		PasswordHash: "hash",
		IsActive:     true,
		UUID:         "logout-uuid",
	}
	err := userRepo.Create(context.Background(), u)
	require.NoError(t, err)

	token, err := jwtManager.GenerateRefreshToken()
	require.NoError(t, err)
	hash := jwtManager.HashToken(token)

	err = tokenRepo.Create(context.Background(), &user.RefreshToken{
		TokenHash: hash,
		UserID:    u.ID,
		ExpiresAt: time.Now().Add(time.Hour),
	})
	require.NoError(t, err)

	t.Run("Success", func(t *testing.T) {
		payload := map[string]string{
			"refresh_token": token,
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)

		// Verify deleted
		revokedToken, err := tokenRepo.GetByHash(context.Background(), hash)
		require.NoError(t, err)
		assert.Nil(t, revokedToken)
	})
}

func TestAuthHandler_ResetPassword(t *testing.T) {
	app, db, _ := setupTestApp(t)
	userRepo := repository.NewUserRepository(db.DB())
	resetRepo := repository.NewGORMPasswordResetRepository(db.DB())

	u := &user.User{
		Email:        "reset@example.com",
		Username:     "reset",
		PasswordHash: "oldhash",
		IsActive:     true,
		UUID:         "reset-uuid",
	}
	err := userRepo.Create(context.Background(), u)
	require.NoError(t, err)

	token := "reset-token"
	// Hash token
	hash := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hash[:])

	err = resetRepo.Create(context.Background(), &user.PasswordReset{
		UserID:    u.ID,
		TokenHash: tokenHash,
		ExpiresAt: time.Now().Add(time.Hour),
	})
	require.NoError(t, err)

	t.Run("WeakPassword", func(t *testing.T) {
		payload := map[string]string{
			"token":        token,
			"new_password": "a",
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/reset-password", bytes.NewReader(body))
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

		// The account must not have been downgraded on the rejected path.
		unchanged, err := userRepo.GetByID(context.Background(), u.ID)
		require.NoError(t, err)
		assert.Equal(t, "oldhash", unchanged.PasswordHash)
	})

	t.Run("InvalidToken", func(t *testing.T) {
		payload := map[string]string{
			"token":        "does-not-exist",
			"new_password": "NewPass123!",
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/reset-password", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		resp, err := app.Test(req)
		require.NoError(t, err)
		// An invalid/expired reset token surfaces ErrInvalidResetToken, which
		// must map to a 400 with its actionable message, not a generic 500.
		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)

		var respData struct {
			Message string `json:"message"`
		}
		err = json.NewDecoder(resp.Body).Decode(&respData)
		require.NoError(t, err)
		assert.Contains(t, respData.Message, "invalid or has expired")
	})

	t.Run("Success", func(t *testing.T) {
		payload := map[string]string{
			"token":        token,
			"new_password": "NewPass123!",
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/reset-password", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)

		// Verify password changed
		u, err := userRepo.GetByID(context.Background(), u.ID)
		require.NoError(t, err)
		assert.NotEqual(t, "oldhash", u.PasswordHash)
		// Check if we can login (implies hash is valid bcrypt)
		err = bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte("NewPass123!"))
		assert.NoError(t, err)
	})
}
