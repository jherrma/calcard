package http

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	authadapter "github.com/jherrma/caldav-server/internal/adapter/auth"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/jherrma/caldav-server/internal/infrastructure/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// SCOPE: P02 — an access token (stateless JWT) must stop working the moment
// its account logs out, changes or resets its password, not ~10 minutes later
// when it expires.

const revocationPassword = "OldPass123!"

type revocationEnv struct {
	t        *testing.T
	app      *fiber.App
	userRepo user.UserRepository
	jwt      *authadapter.JWTManager
	user     *user.User
	db       database.Database
}

func newRevocationEnv(t *testing.T) *revocationEnv {
	t.Helper()
	app, db, cfg := setupTestApp(t)
	userRepo := repository.NewUserRepository(db.DB())
	hash, err := bcrypt.GenerateFromPassword([]byte(revocationPassword), bcrypt.MinCost)
	require.NoError(t, err)
	u := &user.User{
		UUID:          "revoke-uuid",
		Email:         "revoke@example.com",
		Username:      "revoke",
		PasswordHash:  string(hash),
		IsActive:      true,
		EmailVerified: true,
	}
	require.NoError(t, userRepo.Create(context.Background(), u))
	return &revocationEnv{t: t, app: app, userRepo: userRepo, jwt: authadapter.NewJWTManager(&cfg.JWT), user: u, db: db}
}

func (e *revocationEnv) do(method, path, bearer string, body any) *http.Response {
	e.t.Helper()
	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(e.t, err)
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := e.app.Test(req)
	require.NoError(e.t, err)
	return resp
}

// login goes through the real /auth/login so the tokens are minted exactly as
// in production.
func (e *revocationEnv) login(password string) (access, refresh string) {
	e.t.Helper()
	resp := e.do(http.MethodPost, "/api/v1/auth/login", "", map[string]string{"email": e.user.Email, "password": password})
	require.Equal(e.t, fiber.StatusOK, resp.StatusCode)
	var out struct {
		Data struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
		} `json:"data"`
	}
	require.NoError(e.t, json.NewDecoder(resp.Body).Decode(&out))
	return out.Data.AccessToken, out.Data.RefreshToken
}

func (e *revocationEnv) status(bearer string) int {
	e.t.Helper()
	return e.do(http.MethodGet, "/api/v1/users/me", bearer, nil).StatusCode
}

func TestLogoutRevokesAccessTokens(t *testing.T) {
	t.Run("logout with the refresh token kills the access token", func(t *testing.T) {
		e := newRevocationEnv(t)
		access, refresh := e.login(revocationPassword)
		otherDevice, otherRefresh := e.login(revocationPassword)
		require.Equal(t, fiber.StatusOK, e.status(access))

		resp := e.do(http.MethodPost, "/api/v1/auth/logout", "", map[string]string{"refresh_token": refresh})
		require.Equal(t, fiber.StatusOK, resp.StatusCode)

		assert.Equal(t, fiber.StatusUnauthorized, e.status(access), "access token must die with the logout")
		// Invalidation is account-wide: the other device's access token is
		// dead too, but its refresh token survives, so it recovers with one
		// refresh — which is exactly what the SPA does on a 401.
		assert.Equal(t, fiber.StatusUnauthorized, e.status(otherDevice))
		resp = e.do(http.MethodPost, "/api/v1/auth/refresh", "", map[string]string{"refresh_token": otherRefresh})
		require.Equal(t, fiber.StatusOK, resp.StatusCode)
		var refreshed struct {
			Data struct {
				AccessToken string `json:"access_token"`
			} `json:"data"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&refreshed))
		assert.Equal(t, fiber.StatusOK, e.status(refreshed.Data.AccessToken), "a refreshed token must work")

		fresh, _ := e.login(revocationPassword)
		assert.Equal(t, fiber.StatusOK, e.status(fresh), "logging in again must work immediately")
	})

	t.Run("logout with only a bearer token kills it", func(t *testing.T) {
		e := newRevocationEnv(t)
		access, _ := e.login(revocationPassword)
		resp := e.do(http.MethodPost, "/api/v1/auth/logout", access, nil)
		require.Equal(t, fiber.StatusOK, resp.StatusCode)
		assert.Equal(t, fiber.StatusUnauthorized, e.status(access))
	})

	t.Run("an unknown refresh token revokes nothing", func(t *testing.T) {
		e := newRevocationEnv(t)
		access, _ := e.login(revocationPassword)
		resp := e.do(http.MethodPost, "/api/v1/auth/logout", "", map[string]string{"refresh_token": "not-a-token"})
		require.Equal(t, fiber.StatusOK, resp.StatusCode)
		assert.Equal(t, fiber.StatusOK, e.status(access))
	})

	t.Run("a rotated refresh token cannot force everyone out", func(t *testing.T) {
		e := newRevocationEnv(t)
		access, refresh := e.login(revocationPassword)
		resp := e.do(http.MethodPost, "/api/v1/auth/refresh", "", map[string]string{"refresh_token": refresh})
		require.Equal(t, fiber.StatusOK, resp.StatusCode)

		resp = e.do(http.MethodPost, "/api/v1/auth/logout", "", map[string]string{"refresh_token": refresh})
		require.Equal(t, fiber.StatusOK, resp.StatusCode)
		assert.Equal(t, fiber.StatusOK, e.status(access))
	})
}

func TestChangePasswordRevokesOldAccessTokens(t *testing.T) {
	e := newRevocationEnv(t)
	old, _ := e.login(revocationPassword)
	otherDevice, _ := e.login(revocationPassword)

	resp := e.do(http.MethodPut, "/api/v1/users/me/password", old, map[string]string{
		"current_password": revocationPassword,
		"new_password":     "NewPass456!",
	})
	require.Equal(t, fiber.StatusOK, resp.StatusCode)
	var out struct {
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.NotEmpty(t, out.Data.AccessToken)

	assert.Equal(t, fiber.StatusUnauthorized, e.status(old), "the token the change was made with must die")
	assert.Equal(t, fiber.StatusUnauthorized, e.status(otherDevice), "every other session's token must die")
	// Minted in the same request, almost certainly within the same second as
	// the invalidation: it must live, or the SPA would be logged out by its own
	// password change.
	assert.Equal(t, fiber.StatusOK, e.status(out.Data.AccessToken), "the replacement token must work")

	fresh, _ := e.login("NewPass456!")
	assert.Equal(t, fiber.StatusOK, e.status(fresh))
}

func TestResetPasswordRevokesAccessTokens(t *testing.T) {
	e := newRevocationEnv(t)
	access, _ := e.login(revocationPassword)

	token := "reset-token-p02"
	sum := sha256.Sum256([]byte(token))
	resetRepo := repository.NewGORMPasswordResetRepository(e.db.DB())
	require.NoError(t, resetRepo.Create(context.Background(), &user.PasswordReset{
		UserID:    e.user.ID,
		TokenHash: hex.EncodeToString(sum[:]),
		ExpiresAt: time.Now().Add(time.Hour),
	}))

	resp := e.do(http.MethodPost, "/api/v1/auth/reset-password", "", map[string]string{"token": token, "new_password": "NewPass456!"})
	require.Equal(t, fiber.StatusOK, resp.StatusCode)
	assert.Equal(t, fiber.StatusUnauthorized, e.status(access))

	fresh, _ := e.login("NewPass456!")
	assert.Equal(t, fiber.StatusOK, e.status(fresh))
}

// The second-precision edge, at the middleware: a token minted just BEFORE an
// invalidation in the same wall-clock second dies, one minted just AFTER it
// lives. With a whole-second iat one of the two would necessarily be wrong.
func TestAuthenticate_SameSecondInvalidation(t *testing.T) {
	e := newRevocationEnv(t)
	ctx := context.Background()

	var before, after string
	for attempt := 0; attempt < 10; attempt++ {
		start := time.Now()
		var err error
		before, _, err = e.jwt.GenerateAccessToken(e.user.UUID, e.user.Email)
		require.NoError(t, err)
		require.NoError(t, e.userRepo.InvalidateAccessTokens(ctx, e.user.ID, time.Now()))
		after, _, err = e.jwt.GenerateAccessToken(e.user.UUID, e.user.Email)
		require.NoError(t, err)
		if time.Now().Unix() == start.Unix() {
			break // all three happened inside one second: the case under test
		}
	}

	assert.Equal(t, fiber.StatusUnauthorized, e.status(before), "issued before the invalidation, same second")
	assert.Equal(t, fiber.StatusOK, e.status(after), "issued after the invalidation, same second")
}
