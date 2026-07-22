//go:build integration

package integration_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRefreshTokenFlow exercises /auth/refresh end-to-end with ROTATION +
// REUSE DETECTION (#75):
//
//   - a fresh refresh token produces a new *access* token that authenticates
//     protected endpoints AND a new *refresh* token (rotation);
//   - invalid / garbage refresh tokens are rejected with 401;
//   - the OLD refresh token stops working the instant it is rotated;
//   - two sequential refreshes chain (token1 → token2 → token3);
//   - logout revokes the current refresh token so subsequent attempts fail.
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
	token1 := login.RefreshToken

	// --- Garbage refresh token must 401 ---------------------------------
	status, _ := restCall(t, http.MethodPost, "/auth/refresh", "",
		map[string]string{"refresh_token": "clearly-not-a-real-token"})
	assert.Equal(t, http.StatusUnauthorized, status, "garbage refresh token must 401")

	// --- Empty body must 4xx --------------------------------------------
	status, _ = restCall(t, http.MethodPost, "/auth/refresh", "", map[string]string{})
	assert.True(t, status == http.StatusBadRequest || status == http.StatusUnauthorized,
		"missing refresh_token must yield 4xx, got %d", status)

	// --- Valid refresh: returns an access token that works + a NEW refresh
	// token (rotation). ------------------------------------------------------
	type refreshResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresAt    int64  `json:"expires_at"`
		TokenType    string `json:"token_type"`
	}
	var refresh1 refreshResp
	code = doJSON(t, http.MethodPost, "/auth/refresh", "",
		map[string]string{"refresh_token": token1}, &refresh1)
	require.Equal(t, http.StatusOK, code)
	require.NotEmpty(t, refresh1.AccessToken)
	assert.Equal(t, "Bearer", refresh1.TokenType)
	require.NotEmpty(t, refresh1.RefreshToken, "refresh must rotate: a new refresh token is returned")
	assert.NotEqual(t, token1, refresh1.RefreshToken, "the rotated refresh token must differ from the presented one")
	token2 := refresh1.RefreshToken

	// The refreshed access token authenticates a protected endpoint.
	status, raw := restCall(t, http.MethodGet, "/users/me", refresh1.AccessToken, nil)
	require.Equalf(t, http.StatusOK, status, "refreshed access token: %s", string(raw))

	// --- The OLD refresh token (token1) is now dead ---------------------
	// This is the rotation guarantee: reusing token1 after it was rotated must
	// be rejected. (Before rotation existed, token1 would keep working.)
	status, _ = restCall(t, http.MethodPost, "/auth/refresh", "",
		map[string]string{"refresh_token": token1})
	assert.Equal(t, http.StatusUnauthorized, status,
		"the rotated (old) refresh token must be rejected")

	// --- Second sequential refresh chains: token2 → token3 --------------
	var refresh2 refreshResp
	code = doJSON(t, http.MethodPost, "/auth/refresh", "",
		map[string]string{"refresh_token": token2}, &refresh2)
	require.Equal(t, http.StatusOK, code, "the current refresh token (token2) must work")
	require.NotEmpty(t, refresh2.RefreshToken)
	assert.NotEqual(t, token2, refresh2.RefreshToken)
	assert.NotEqual(t, token1, refresh2.RefreshToken)
	token3 := refresh2.RefreshToken

	// token3 authenticates a protected endpoint.
	status, raw = restCall(t, http.MethodGet, "/users/me", refresh2.AccessToken, nil)
	require.Equalf(t, http.StatusOK, status, "second refreshed access token: %s", string(raw))

	// --- Logout revokes the current refresh token (token3) --------------
	code = doJSON(t, http.MethodPost, "/auth/logout", refresh2.AccessToken,
		map[string]string{"refresh_token": token3}, nil)
	require.Equal(t, http.StatusOK, code)

	status, _ = restCall(t, http.MethodPost, "/auth/refresh", "",
		map[string]string{"refresh_token": token3})
	assert.Equal(t, http.StatusUnauthorized, status,
		"refresh after logout must be rejected")
}
