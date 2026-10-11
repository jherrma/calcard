//go:build integration

package integration_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// P02: access tokens are stateless JWTs, so before the per-user revocation
// marker (users.tokens_valid_after) a token kept working for its full lifetime
// after logout or a password change. These tests drive the real server.

type revocationTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

func revocationLogin(t *testing.T, email, password string) revocationTokens {
	t.Helper()
	var out revocationTokens
	code := doJSON(t, http.MethodPost, "/auth/login", "",
		map[string]string{"email": email, "password": password}, &out)
	require.Equal(t, http.StatusOK, code)
	require.NotEmpty(t, out.AccessToken)
	return out
}

func revocationRegister(t *testing.T, email, password string) {
	t.Helper()
	code := doJSON(t, http.MethodPost, "/auth/register", "", map[string]string{
		"email": email, "password": password, "display_name": "Revocation User",
	}, nil)
	require.Equal(t, http.StatusOK, code)
}

func meStatus(t *testing.T, token string) int {
	t.Helper()
	status, _ := restCall(t, http.MethodGet, "/users/me", token, nil)
	return status
}

func TestLogoutRevokesAccessToken(t *testing.T) {
	const email, password = "revoke-logout@example.test", "revokeSecret!123"
	revocationRegister(t, email, password)

	device := revocationLogin(t, email, password)
	other := revocationLogin(t, email, password)
	require.Equal(t, http.StatusOK, meStatus(t, device.AccessToken))

	// An MCP client authenticating with a JWT is covered by the same marker.
	initialize := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"integration","version":"1"}}}`
	status, _, _ := mcpCall(t, device.AccessToken, "", initialize)
	require.Equal(t, http.StatusOK, status, "precondition: MCP accepts the JWT before logout")

	code := doJSON(t, http.MethodPost, "/auth/logout", device.AccessToken,
		map[string]string{"refresh_token": device.RefreshToken}, nil)
	require.Equal(t, http.StatusOK, code)

	assert.Equal(t, http.StatusUnauthorized, meStatus(t, device.AccessToken),
		"the access token must stop working at logout, not when it expires")
	status, _, _ = mcpCall(t, device.AccessToken, "", initialize)
	assert.Equal(t, http.StatusUnauthorized, status, "MCP must refuse the logged-out JWT too")

	// Invalidation is account-wide; the other session recovers with one
	// refresh because its refresh token is untouched.
	assert.Equal(t, http.StatusUnauthorized, meStatus(t, other.AccessToken))
	var refreshed revocationTokens
	code = doJSON(t, http.MethodPost, "/auth/refresh", "",
		map[string]string{"refresh_token": other.RefreshToken}, &refreshed)
	require.Equal(t, http.StatusOK, code, "the other session's refresh token must survive")
	assert.Equal(t, http.StatusOK, meStatus(t, refreshed.AccessToken), "a token refreshed after logout works")

	// A fresh login right after logout — typically within the same second —
	// must not be caught by the marker.
	fresh := revocationLogin(t, email, password)
	assert.Equal(t, http.StatusOK, meStatus(t, fresh.AccessToken))
	status, _, _ = mcpCall(t, fresh.AccessToken, "", initialize)
	assert.Equal(t, http.StatusOK, status)
}

func TestChangePasswordRevokesAccessTokens(t *testing.T) {
	const email, password, newPassword = "revoke-change@example.test", "revokeSecret!123", "revokeSecret!456"
	revocationRegister(t, email, password)

	session := revocationLogin(t, email, password)
	other := revocationLogin(t, email, password)

	var changed struct {
		AccessToken string `json:"access_token"`
	}
	code := doJSON(t, http.MethodPut, "/users/me/password", session.AccessToken, map[string]string{
		"current_password": password, "new_password": newPassword,
	}, &changed)
	require.Equal(t, http.StatusOK, code)
	require.NotEmpty(t, changed.AccessToken)

	assert.Equal(t, http.StatusUnauthorized, meStatus(t, session.AccessToken), "old token of the changing session")
	assert.Equal(t, http.StatusUnauthorized, meStatus(t, other.AccessToken), "old token of another session")
	assert.Equal(t, http.StatusOK, meStatus(t, changed.AccessToken),
		"the replacement token change-password returns must work")

	fresh := revocationLogin(t, email, newPassword)
	assert.Equal(t, http.StatusOK, meStatus(t, fresh.AccessToken))
}
