package http

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	authadapter "github.com/jherrma/caldav-server/internal/adapter/auth"
	"github.com/jherrma/caldav-server/internal/adapter/http/dto"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/user"
	authusecase "github.com/jherrma/caldav-server/internal/usecase/auth"
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

// With SMTP configured a new account is usable before its address is
// confirmed: register answers the same full body as without SMTP (only
// email_verified is false), the account can log in, call an authenticated
// endpoint and refresh, and opening the activation link still sets
// email_verified. A taken address answers 409, as without SMTP.
func TestAuthHandler_UnverifiedAccountUsableWithSMTP(t *testing.T) {
	app, db, cfg := setupTestApp(t)
	// The use case and handler share cfg, so this switches both to "SMTP on".
	// The email service was built from the empty SMTP config and stays a no-op.
	cfg.SMTP.Host = "smtp.invalid"
	userRepo := repository.NewUserRepository(db.DB())

	call := func(method, path, bearer string, payload any) (int, map[string]any) {
		t.Helper()
		var body io.Reader
		if payload != nil {
			b, _ := json.Marshal(payload)
			body = bytes.NewReader(b)
		}
		req := httptest.NewRequest(method, path, body)
		req.Header.Set("Content-Type", "application/json")
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		resp, err := app.Test(req, fiber.TestConfig{Timeout: 10 * time.Second})
		require.NoError(t, err)
		defer resp.Body.Close()
		var out struct {
			Data map[string]any `json:"data"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out.Data
	}

	status, reg := call(http.MethodPost, "/api/v1/auth/register", "",
		map[string]string{"email": "Fresh@Example.com", "password": "Probe123!", "display_name": "Probe"})
	require.Equal(t, fiber.StatusOK, status)
	assert.NotEmpty(t, reg["id"], "the register body carries the id, SMTP or not")
	assert.NotEmpty(t, reg["username"])
	assert.NotEmpty(t, reg["created_at"])
	assert.Equal(t, "fresh@example.com", reg["email"])
	assert.Equal(t, true, reg["is_active"])
	assert.Equal(t, false, reg["email_verified"])

	status, login := call(http.MethodPost, "/api/v1/auth/login", "",
		map[string]string{"email": "fresh@example.com", "password": "Probe123!"})
	require.Equal(t, fiber.StatusOK, status, "an unconfirmed account can log in")
	access, _ := login["access_token"].(string)
	refresh, _ := login["refresh_token"].(string)
	require.NotEmpty(t, access)
	require.NotEmpty(t, refresh)

	status, me := call(http.MethodGet, "/api/v1/users/me", access, nil)
	require.Equal(t, fiber.StatusOK, status, "and use the authenticated API")
	assert.Equal(t, reg["id"], me["id"])
	assert.Equal(t, false, me["email_verified"])

	status, _ = call(http.MethodPost, "/api/v1/auth/refresh", "", map[string]string{"refresh_token": refresh})
	assert.Equal(t, fiber.StatusOK, status, "and refresh its session")

	// A taken address is a 409 here too; the account keeps its password.
	status, _ = call(http.MethodPost, "/api/v1/auth/register", "",
		map[string]string{"email": "fresh@example.com", "password": "Other123!", "display_name": "Dup"})
	assert.Equal(t, fiber.StatusConflict, status)

	// Opening the activation link confirms the address. Only the token's
	// hash is stored, so plant a known one for this account.
	u, err := userRepo.GetByEmail(context.Background(), "fresh@example.com")
	require.NoError(t, err)
	require.NotNil(t, u)
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte("Probe123!")))
	sum := sha256.Sum256([]byte("activation-token"))
	require.NoError(t, userRepo.CreateVerification(context.Background(), &user.EmailVerification{
		UserID: u.ID, Token: hex.EncodeToString(sum[:]), ExpiresAt: time.Now().Add(time.Hour),
	}))
	status, _ = call(http.MethodGet, "/api/v1/auth/verify?token=activation-token", "", nil)
	require.Equal(t, fiber.StatusOK, status)

	status, me = call(http.MethodGet, "/api/v1/users/me", access, nil)
	require.Equal(t, fiber.StatusOK, status, "verifying does not end the session")
	assert.Equal(t, true, me["email_verified"])
	assert.Equal(t, true, me["is_active"])
}

// is_active=false means a deactivated account, whether or not its address is
// confirmed. A verified but deactivated account gets 401 from login, refresh
// and the authenticated API alike. The same session is used before and after
// the deactivation, so each 401 can only come from the IsActive check: with it
// removed, every call below would answer 200.
func TestAuthHandler_DeactivatedVerifiedAccountIsRefused(t *testing.T) {
	app, db, _ := setupTestApp(t)
	userRepo := repository.NewUserRepository(db.DB())

	hash, err := bcrypt.GenerateFromPassword([]byte("Pass123!"), bcrypt.MinCost)
	require.NoError(t, err)
	u := &user.User{
		Email:         "deactivated@example.com",
		Username:      "deactivated",
		PasswordHash:  string(hash),
		IsActive:      true,
		EmailVerified: true,
		UUID:          "deactivated-uuid",
	}
	require.NoError(t, userRepo.Create(context.Background(), u))

	call := func(method, path, bearer string, payload any) (int, map[string]any) {
		t.Helper()
		var body io.Reader
		if payload != nil {
			b, _ := json.Marshal(payload)
			body = bytes.NewReader(b)
		}
		req := httptest.NewRequest(method, path, body)
		req.Header.Set("Content-Type", "application/json")
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		resp, err := app.Test(req, fiber.TestConfig{Timeout: 10 * time.Second})
		require.NoError(t, err)
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	login := map[string]string{"email": "deactivated@example.com", "password": "Pass123!"}

	// While active: a working session.
	status, out := call(http.MethodPost, "/api/v1/auth/login", "", login)
	require.Equal(t, fiber.StatusOK, status)
	data, _ := out["data"].(map[string]any)
	access, _ := data["access_token"].(string)
	refresh, _ := data["refresh_token"].(string)
	require.NotEmpty(t, access)
	require.NotEmpty(t, refresh)
	status, _ = call(http.MethodGet, "/api/v1/users/me", access, nil)
	require.Equal(t, fiber.StatusOK, status)
	status, out = call(http.MethodPost, "/api/v1/auth/refresh", "", map[string]string{"refresh_token": refresh})
	require.Equal(t, fiber.StatusOK, status)
	data, _ = out["data"].(map[string]any)
	refresh, _ = data["refresh_token"].(string) // rotated: the successor is live
	require.NotEmpty(t, refresh)

	// Deactivate it. The address stays confirmed.
	require.NoError(t, db.DB().Model(&user.User{}).Where("id = ?", u.ID).
		UpdateColumn("is_active", false).Error)

	status, out = call(http.MethodPost, "/api/v1/auth/login", "", login)
	assert.Equal(t, fiber.StatusUnauthorized, status, "login")
	assert.Equal(t, "account is not active", out["message"], "login")

	status, out = call(http.MethodPost, "/api/v1/auth/refresh", "", map[string]string{"refresh_token": refresh})
	assert.Equal(t, fiber.StatusUnauthorized, status, "refresh")
	assert.Equal(t, "Invalid or expired refresh token", out["message"], "refresh")

	status, out = call(http.MethodGet, "/api/v1/users/me", access, nil)
	assert.Equal(t, fiber.StatusUnauthorized, status, "GET /users/me")
	assert.Equal(t, "account is not active", out["message"], "GET /users/me")
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
		// The SPA keeps this user object until the next page load, and builds
		// the DAV URLs it shows (/dav/{username}/...) from its username.
		assert.Equal(t, u.Username, res.User.Username)
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

// failingEmailService stands in for a mail server that cannot be reached.
type failingEmailService struct{}

func (failingEmailService) SendActivationEmail(context.Context, string, string) error {
	return errors.New("dial tcp: lookup smtp.example.test: server misbehaving")
}

func (failingEmailService) SendEmail(context.Context, string, string, string) error {
	return errors.New("dial tcp: lookup smtp.example.test: server misbehaving")
}

// A failed send is a 500 with a generic message, but only for an address that
// has an account: an unknown address stays the same green 200 (no account
// enumeration), and the driver text never reaches the client.
func TestAuthHandler_ForgotPasswordSendFailure(t *testing.T) {
	_, db, cfg := setupTestApp(t)
	userRepo := repository.NewUserRepository(db.DB())
	resetRepo := repository.NewGORMPasswordResetRepository(db.DB())

	u := &user.User{
		Email:        "known@example.com",
		Username:     "known",
		IsActive:     true,
		UUID:         "known-uuid",
		PasswordHash: "hash",
	}
	require.NoError(t, userRepo.Create(context.Background(), u))

	forgotUC := authusecase.NewForgotPasswordUseCase(userRepo, resetRepo, failingEmailService{}, cfg.JWT.ResetExpiry)
	h := NewAuthHandler(nil, nil, nil, nil, nil, forgotUC, nil, cfg)
	app := fiber.New()
	app.Post("/forgot-password", h.ForgotPassword)

	post := func(email string) (int, string) {
		body, _ := json.Marshal(map[string]string{"email": email})
		req := httptest.NewRequest(http.MethodPost, "/forgot-password", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		raw, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(raw)
	}

	t.Run("known address, mail server down", func(t *testing.T) {
		status, body := post("known@example.com")
		assert.Equal(t, fiber.StatusInternalServerError, status)
		assert.Contains(t, body, "A server error occurred while sending the reset link")
		assert.NotContains(t, body, "misbehaving")
		assert.NotContains(t, body, "smtp.example.test")
	})

	t.Run("unknown address stays a success", func(t *testing.T) {
		status, _ := post("nobody@example.com")
		assert.Equal(t, fiber.StatusOK, status)
	})
}

func TestAuthHandler_Verify(t *testing.T) {
	app, db, _ := setupTestApp(t)
	userRepo := repository.NewUserRepository(db.DB())

	// An account whose address is not confirmed yet (active, as registration
	// creates every account).
	u := &user.User{
		Email:        "verify@example.com",
		Username:     "verify",
		IsActive:     true,
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

		// The address is confirmed; nothing else changed.
		u, err := userRepo.GetByID(context.Background(), u.ID)
		require.NoError(t, err)
		assert.True(t, u.EmailVerified)
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
