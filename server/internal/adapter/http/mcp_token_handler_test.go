package http

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	authadapter "github.com/jherrma/caldav-server/internal/adapter/auth"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/jherrma/caldav-server/internal/usecase/mcptoken"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPTokenHandler_CreateReturnsSecretOnceThenNeverAgain(t *testing.T) {
	app, db, cfg := setupTestApp(t)
	userRepo := repository.NewUserRepository(db.DB())
	jwtManager := authadapter.NewJWTManager(&cfg.JWT)

	u := &user.User{Email: "mcp@example.com", Username: "mcpuser", PasswordHash: "h",
		IsActive: true, EmailVerified: true, UUID: "mcp-uuid-1"}
	require.NoError(t, userRepo.Create(context.Background(), u))
	jwt, _, err := jwtManager.GenerateAccessToken(u.UUID, u.Email)
	require.NoError(t, err)

	// Create
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp-tokens",
		strings.NewReader(`{"name":"Claude on my laptop"}`))
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusCreated, resp.StatusCode)

	var created struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Token       string `json:"token"`
		TokenPrefix string `json:"token_prefix"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&created))
	assert.Equal(t, "Claude on my laptop", created.Name)
	require.True(t, strings.HasPrefix(created.Token, mcptoken.TokenPrefix),
		"the secret must carry the routing prefix the transport looks for")
	assert.True(t, strings.HasPrefix(created.TokenPrefix, mcptoken.TokenPrefix))

	// List — the secret must be gone for good.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/mcp-tokens", nil)
	req.Header.Set("Authorization", "Bearer "+jwt)
	resp, err = app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.NotContains(t, string(body), created.Token,
		"a token is shown exactly once; the list must never be able to reproduce it")

	var listed struct {
		Tokens []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			TokenPrefix string `json:"token_prefix"`
		} `json:"tokens"`
	}
	require.NoError(t, json.Unmarshal(body, &listed))
	require.Len(t, listed.Tokens, 1)
	assert.Equal(t, created.ID, listed.Tokens[0].ID)
	assert.Equal(t, created.TokenPrefix, listed.Tokens[0].TokenPrefix)
}

func TestMCPTokenHandler_CreateValidatesInput(t *testing.T) {
	app, db, cfg := setupTestApp(t)
	userRepo := repository.NewUserRepository(db.DB())
	jwtManager := authadapter.NewJWTManager(&cfg.JWT)

	u := &user.User{Email: "mcp2@example.com", Username: "mcpuser2", PasswordHash: "h",
		IsActive: true, EmailVerified: true, UUID: "mcp-uuid-2"}
	require.NoError(t, userRepo.Create(context.Background(), u))
	jwt, _, err := jwtManager.GenerateAccessToken(u.UUID, u.Email)
	require.NoError(t, err)

	for name, body := range map[string]string{
		"missing name":    `{}`,
		"blank name":      `{"name":"   "}`,
		"bad expires_at":  `{"name":"ok","expires_at":"next tuesday"}`,
		"expires in past": `{"name":"ok","expires_at":"2020-01-01T00:00:00Z"}`,
		"malformed json":  `{`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp-tokens", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+jwt)
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode, "%s must be a 400", name)
	}
}

func TestMCPTokenHandler_RevokeIsScopedToTheOwner(t *testing.T) {
	app, db, cfg := setupTestApp(t)
	userRepo := repository.NewUserRepository(db.DB())
	jwtManager := authadapter.NewJWTManager(&cfg.JWT)

	alice := &user.User{Email: "alice-mcp@example.com", Username: "alicemcp", PasswordHash: "h",
		IsActive: true, EmailVerified: true, UUID: "mcp-alice"}
	bob := &user.User{Email: "bob-mcp@example.com", Username: "bobmcp", PasswordHash: "h",
		IsActive: true, EmailVerified: true, UUID: "mcp-bob"}
	require.NoError(t, userRepo.Create(context.Background(), alice))
	require.NoError(t, userRepo.Create(context.Background(), bob))

	aliceJWT, _, err := jwtManager.GenerateAccessToken(alice.UUID, alice.Email)
	require.NoError(t, err)
	bobJWT, _, err := jwtManager.GenerateAccessToken(bob.UUID, bob.Email)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp-tokens", strings.NewReader(`{"name":"alice laptop"}`))
	req.Header.Set("Authorization", "Bearer "+aliceJWT)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusCreated, resp.StatusCode)

	var created struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&created))

	// Bob must not be able to revoke Alice's token, and must not learn it exists.
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/mcp-tokens/"+created.ID, nil)
	req.Header.Set("Authorization", "Bearer "+bobJWT)
	resp, err = app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusNotFound, resp.StatusCode)

	// Alice can.
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/mcp-tokens/"+created.ID, nil)
	req.Header.Set("Authorization", "Bearer "+aliceJWT)
	resp, err = app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusNoContent, resp.StatusCode)

	// And it is gone from her list.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/mcp-tokens", nil)
	req.Header.Set("Authorization", "Bearer "+aliceJWT)
	resp, err = app.Test(req)
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.NotContains(t, string(body), created.ID)
}

func TestMCPTokenHandler_RequiresAuthentication(t *testing.T) {
	app, _, _ := setupTestApp(t)

	for _, tc := range []struct {
		method, path string
	}{
		{http.MethodGet, "/api/v1/mcp-tokens"},
		{http.MethodPost, "/api/v1/mcp-tokens"},
		{http.MethodDelete, "/api/v1/mcp-tokens/whatever"},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{"name":"x"}`))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode, "%s %s", tc.method, tc.path)
	}
}

func TestMCPTokenHandler_EmptyListIsAnArrayNotNull(t *testing.T) {
	app, db, cfg := setupTestApp(t)
	userRepo := repository.NewUserRepository(db.DB())
	jwtManager := authadapter.NewJWTManager(&cfg.JWT)

	u := &user.User{Email: "empty-mcp@example.com", Username: "emptymcp", PasswordHash: "h",
		IsActive: true, EmailVerified: true, UUID: "mcp-empty"}
	require.NoError(t, userRepo.Create(context.Background(), u))
	jwt, _, err := jwtManager.GenerateAccessToken(u.UUID, u.Email)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/mcp-tokens", nil)
	req.Header.Set("Authorization", "Bearer "+jwt)
	resp, err := app.Test(req)
	require.NoError(t, err)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	// The client iterates this unconditionally; null would make it throw.
	assert.Contains(t, string(body), `"tokens":[]`)
}
