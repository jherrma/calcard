package http

import (
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
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/jherrma/caldav-server/internal/infrastructure/email"
	authusecase "github.com/jherrma/caldav-server/internal/usecase/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// bcrypt refuses passwords over 72 bytes. Before the policy had a maximum, a
// long password (a password manager's default, say) passed validation and
// then failed at the hash: a 500 "Failed to reset password" / "Failed to
// change password", or a generic 400 "registration failed" / "setup failed".
// Every endpoint that sets a password must now name the rule.

const tooLongMessage = "password must be at most 72 bytes"

// 73 bytes that meet every other rule.
var password73Bytes = "Aa1!" + strings.Repeat("x", 69)

// 39 characters, 74 bytes: the limit counts bytes, like bcrypt.
var password39Chars74Bytes = "Aa1!" + strings.Repeat("ä", 35)

func sendJSON(t *testing.T, app *fiber.App, method, path, bearer string, body any) (int, string) {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(method, path, strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 10 * time.Second})
	require.NoError(t, err)
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(b)
}

func errorMessage(t *testing.T, body string) string {
	t.Helper()
	var r struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &r), body)
	return r.Message
}

func TestPasswordOverBcryptLimit_Register(t *testing.T) {
	app, db, _ := setupTestApp(t)
	userRepo := repository.NewUserRepository(db.DB())

	for _, pw := range []string{password73Bytes, password39Chars74Bytes} {
		status, body := sendJSON(t, app, http.MethodPost, "/api/v1/auth/register", "",
			map[string]string{"email": "long@example.com", "password": pw, "display_name": "Long"})
		assert.Equal(t, fiber.StatusBadRequest, status, body)
		assert.Equal(t, tooLongMessage, errorMessage(t, body))
	}

	u, err := userRepo.GetByEmail(context.Background(), "long@example.com")
	require.NoError(t, err)
	assert.Nil(t, u, "no account may be created")
}

func TestPasswordOverBcryptLimit_Setup(t *testing.T) {
	_, db, cfg := setupTestApp(t)
	userRepo := repository.NewUserRepository(db.DB())
	registerUC := authusecase.NewRegisterUseCase(userRepo, repository.NewCalendarRepository(db.DB()),
		repository.NewAddressBookRepository(db.DB()), email.NewEmailService(cfg.SMTP), cfg)
	app := fiber.New()
	app.Post("/api/v1/auth/setup", NewSetupHandler(authusecase.NewSetupUseCase(userRepo, registerUC, "")).Setup)

	status, body := sendJSON(t, app, http.MethodPost, "/api/v1/auth/setup", "",
		map[string]string{"username": "admin", "email": "admin@example.com", "password": password73Bytes})
	assert.Equal(t, fiber.StatusBadRequest, status, body)
	assert.Equal(t, tooLongMessage, errorMessage(t, body))

	n, err := userRepo.Count(context.Background())
	require.NoError(t, err)
	assert.Zero(t, n, "setup must stay open")
}

func TestPasswordOverBcryptLimit_ResetPassword(t *testing.T) {
	app, db, _ := setupTestApp(t)
	userRepo := repository.NewUserRepository(db.DB())
	resetRepo := repository.NewGORMPasswordResetRepository(db.DB())

	u := &user.User{Email: "reset-long@example.com", Username: "resetlong", PasswordHash: "oldhash", IsActive: true, UUID: "reset-long-uuid"}
	require.NoError(t, userRepo.Create(context.Background(), u))
	sum := sha256.Sum256([]byte("reset-long-token"))
	require.NoError(t, resetRepo.Create(context.Background(), &user.PasswordReset{
		UserID: u.ID, TokenHash: hex.EncodeToString(sum[:]), ExpiresAt: time.Now().Add(time.Hour),
	}))

	status, body := sendJSON(t, app, http.MethodPost, "/api/v1/auth/reset-password", "",
		map[string]string{"token": "reset-long-token", "new_password": password73Bytes})
	assert.Equal(t, fiber.StatusBadRequest, status, body)
	assert.Equal(t, tooLongMessage, errorMessage(t, body))

	unchanged, err := userRepo.GetByID(context.Background(), u.ID)
	require.NoError(t, err)
	assert.Equal(t, "oldhash", unchanged.PasswordHash)
}

func TestPasswordOverBcryptLimit_ChangePassword(t *testing.T) {
	app, db, cfg := setupTestApp(t)
	userRepo := repository.NewUserRepository(db.DB())
	jwtManager := authadapter.NewJWTManager(&cfg.JWT)

	hash, err := bcrypt.GenerateFromPassword([]byte("OldPass123!"), bcrypt.MinCost)
	require.NoError(t, err)
	u := &user.User{Email: "change-long@example.com", Username: "changelong", PasswordHash: string(hash),
		IsActive: true, EmailVerified: true, UUID: "change-long-uuid"}
	require.NoError(t, userRepo.Create(context.Background(), u))
	token, _, err := jwtManager.GenerateAccessToken(u.UUID, u.Email)
	require.NoError(t, err)

	status, body := sendJSON(t, app, http.MethodPut, "/api/v1/users/me/password", token,
		map[string]string{"current_password": "OldPass123!", "new_password": password73Bytes})
	assert.Equal(t, fiber.StatusBadRequest, status, body)
	assert.Equal(t, tooLongMessage, errorMessage(t, body))

	unchanged, err := userRepo.GetByID(context.Background(), u.ID)
	require.NoError(t, err)
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(unchanged.PasswordHash), []byte("OldPass123!")))
}

// DAV credentials keep their own, looser rule (8 bytes, no classes), but hash
// with bcrypt all the same.
func TestPasswordOverBcryptLimit_DAVCredentials(t *testing.T) {
	app, token, _ := credentialTestUser(t)
	for _, kind := range []string{"caldav", "carddav"} {
		t.Run(kind, func(t *testing.T) {
			path := "/api/v1/" + kind + "-credentials"
			status, body := sendJSON(t, app, http.MethodPost, path, token,
				map[string]string{"name": "x", "username": kind + "long", "password": strings.Repeat("p", 73)})
			assert.Equal(t, fiber.StatusBadRequest, status, body)
			assert.Contains(t, body, tooLongMessage)
			assert.NotContains(t, body, "bcrypt")

			status, body = sendJSON(t, app, http.MethodPost, path, token,
				map[string]string{"name": "x", "username": kind + "max", "password": strings.Repeat("p", 72)})
			assert.Equal(t, fiber.StatusCreated, status, "72 bytes still fit: "+body)
		})
	}
}

// Login never applies the maximum. A hash made by an older bcrypt that
// silently truncated a long password still matches that full password, and
// such an account must keep signing in.
func TestPasswordOverBcryptLimit_LoginUnaffected(t *testing.T) {
	app, db, _ := setupTestApp(t)
	userRepo := repository.NewUserRepository(db.DB())

	long := "Aa1!" + strings.Repeat("y", 96) // 100 bytes
	legacyHash, err := bcrypt.GenerateFromPassword([]byte(long[:user.MaxPasswordBytes]), bcrypt.MinCost)
	require.NoError(t, err)
	require.NoError(t, userRepo.Create(context.Background(), &user.User{
		Email: "legacy@example.com", Username: "legacylong", PasswordHash: string(legacyHash),
		IsActive: true, EmailVerified: true, UUID: "legacy-long-uuid",
	}))

	status, body := sendJSON(t, app, http.MethodPost, "/api/v1/auth/login", "",
		map[string]string{"email": "legacy@example.com", "password": long})
	assert.Equal(t, fiber.StatusOK, status, body)
}
