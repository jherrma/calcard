package webdav

import (
	"bytes"
	"context"
	"encoding/base64"
	"net/http"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	authadapter "github.com/jherrma/caldav-server/internal/adapter/auth"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// TestBasicAuthAcceptsUsername is the regression test for #48: DAV Basic auth
// must accept the account username (the segment shown in the DAV URL), not only
// the email. The identity lookup is credential-agnostic, so the primary
// password exercises the GetByUsername fallback directly.
func TestBasicAuthAcceptsUsername(t *testing.T) {
	app, db, _ := setupTestApp(t)
	defer db.Close()

	userRepo := repository.NewUserRepository(db.DB())
	passwordHash, _ := bcrypt.GenerateFromPassword([]byte("primary-pass"), bcrypt.DefaultCost)
	u := &user.User{
		UUID:         "user-uuid",
		Email:        "user@example.com",
		Username:     "testuser",
		PasswordHash: string(passwordHash),
		IsActive:     true,
	}
	require.NoError(t, userRepo.Create(context.Background(), u))

	propfind := func(identifier, password string) int {
		body := `<?xml version="1.0" encoding="utf-8" ?>
<D:propfind xmlns:D="DAV:"><D:prop><D:current-user-principal/></D:prop></D:propfind>`
		req, _ := http.NewRequest("PROPFIND", "/dav/testuser/", bytes.NewReader([]byte(body)))
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(identifier+":"+password)))
		req.Header.Set("Content-Type", "application/xml")
		resp, err := app.Test(req, davTestTimeout)
		require.NoError(t, err)
		return resp.StatusCode
	}

	t.Run("username + correct password authenticates", func(t *testing.T) {
		assert.Equal(t, fiber.StatusMultiStatus, propfind("testuser", "primary-pass"))
	})

	t.Run("username + wrong password is rejected", func(t *testing.T) {
		assert.Equal(t, fiber.StatusUnauthorized, propfind("testuser", "wrong-pass"))
	})

	t.Run("email still authenticates", func(t *testing.T) {
		assert.Equal(t, fiber.StatusMultiStatus, propfind("user@example.com", "primary-pass"))
	})

	t.Run("unknown identifier is rejected", func(t *testing.T) {
		assert.Equal(t, fiber.StatusUnauthorized, propfind("nobody", "primary-pass"))
	})
}

// P02: DAV also accepts a bearer JWT; a token issued before the account's last
// logout / password change must be refused there as well.
func TestBearerAuthRejectsRevokedJWT(t *testing.T) {
	app, db, cfg := setupTestApp(t)
	defer db.Close()

	userRepo := repository.NewUserRepository(db.DB())
	// The shared harness leaves AccessExpiry at zero (tokens born expired);
	// same secret, usable lifetime.
	jwtCfg := cfg.JWT
	jwtCfg.AccessExpiry = time.Hour
	jwtManager := authadapter.NewJWTManager(&jwtCfg)
	u := &user.User{UUID: "jwt-uuid", Email: "jwt@example.com", Username: "jwtuser", PasswordHash: "x", IsActive: true}
	require.NoError(t, userRepo.Create(context.Background(), u))

	propfind := func(token string) int {
		body := `<?xml version="1.0" encoding="utf-8" ?>
<D:propfind xmlns:D="DAV:"><D:prop><D:current-user-principal/></D:prop></D:propfind>`
		req, _ := http.NewRequest("PROPFIND", "/dav/jwtuser/", bytes.NewReader([]byte(body)))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/xml")
		resp, err := app.Test(req, davTestTimeout)
		require.NoError(t, err)
		return resp.StatusCode
	}

	old, _, err := jwtManager.GenerateAccessToken(u.UUID, u.Email)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusMultiStatus, propfind(old), "precondition: bearer auth works")

	require.NoError(t, userRepo.InvalidateAccessTokens(context.Background(), u.ID, time.Now()))
	assert.Equal(t, fiber.StatusUnauthorized, propfind(old))

	fresh, _, err := jwtManager.GenerateAccessToken(u.UUID, u.Email)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusMultiStatus, propfind(fresh))
}
