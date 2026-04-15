//go:build integration

package integration_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRefreshTokenFlow exercises /auth/refresh end-to-end:
//
//   - a fresh refresh token produces a new *access* token that actually
//     authenticates protected endpoints;
//   - invalid / garbage refresh tokens are rejected with 401;
//   - logout revokes the refresh token so subsequent refresh attempts fail.
//
// The current implementation does NOT rotate the refresh token itself —
// successive refreshes with the same refresh token are accepted until the
// user explicitly logs out or the token expires. That's a deliberate choice
// in the server code (see auth.RefreshUseCase.Execute); if you ever add
// rotation, the "second refresh with the same token succeeds" assertion
// below will start failing and should be updated to assert rotation
// semantics instead (old-token-after-rotation → 401).
func TestRefreshTokenFlow(t *testing.T) {
	email := "refresh@example.test"
	password := "refreshSecret!123"

	// Register + login manually so we capture the refresh token.
	code := doJSON(t, http.MethodPost, "/auth/register", "", map[string]string{
		"email": email, "password": password, "display_name": "Refresh User",
	}, nil)
	require.Equal(t, http.StatusOK, code)

	var login struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	code = doJSON(t, http.MethodPost, "/auth/login", "",
		map[string]string{"email": email, "password": password}, &login)
	require.Equal(t, http.StatusOK, code)
	require.NotEmpty(t, login.RefreshToken)
	initialAccess := login.AccessToken

	// --- Garbage refresh token must 401 ---------------------------------
	status, _ := restCall(t, http.MethodPost, "/auth/refresh", "",
		map[string]string{"refresh_token": "clearly-not-a-real-token"})
	assert.Equal(t, http.StatusUnauthorized, status, "garbage refresh token must 401")

	// --- Empty body must 4xx --------------------------------------------
	status, _ = restCall(t, http.MethodPost, "/auth/refresh", "", map[string]string{})
	assert.True(t, status == http.StatusBadRequest || status == http.StatusUnauthorized,
		"missing refresh_token must yield 4xx, got %d", status)

	// --- Valid refresh: returns an access token that works -------------
	// We don't assert token-inequality: the JWT's `iat`/`exp` are in whole
	// seconds, so two refreshes issued in the same second produce byte-
	// identical tokens. The meaningful property is that the returned token
	// authenticates a protected endpoint.
	var refresh1 struct {
		AccessToken string `json:"access_token"`
		ExpiresAt   int64  `json:"expires_at"`
		TokenType   string `json:"token_type"`
	}
	code = doJSON(t, http.MethodPost, "/auth/refresh", "",
		map[string]string{"refresh_token": login.RefreshToken}, &refresh1)
	require.Equal(t, http.StatusOK, code)
	require.NotEmpty(t, refresh1.AccessToken)
	assert.Equal(t, "Bearer", refresh1.TokenType)
	_ = initialAccess // same JWT claims within a 1-second window would match

	status, raw := restCall(t, http.MethodGet, "/users/me", refresh1.AccessToken, nil)
	require.Equalf(t, http.StatusOK, status, "refreshed access token: %s", string(raw))

	// --- Second refresh with the same refresh token is still accepted ---
	// (Current server does not rotate refresh tokens. If rotation is added
	// later this sub-assertion should flip to asserting 401 on the second
	// use of the same refresh token.)
	code = doJSON(t, http.MethodPost, "/auth/refresh", "",
		map[string]string{"refresh_token": login.RefreshToken}, nil)
	require.Equal(t, http.StatusOK, code,
		"without rotation the same refresh token should keep working until logout")

	// --- Logout revokes the refresh token -------------------------------
	code = doJSON(t, http.MethodPost, "/auth/logout", refresh1.AccessToken,
		map[string]string{"refresh_token": login.RefreshToken}, nil)
	require.Equal(t, http.StatusOK, code)

	status, _ = restCall(t, http.MethodPost, "/auth/refresh", "",
		map[string]string{"refresh_token": login.RefreshToken})
	assert.Equal(t, http.StatusUnauthorized, status,
		"refresh after logout must be rejected")
}
