//go:build integration

package integration_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLogoutRevokesRefreshToken is the regression guard for #7: logging out
// must revoke the refresh token server-side so a stolen or replayed token can
// no longer mint fresh access tokens once the user has logged out.
//
// It also pins the logout endpoint's hardened no-body behaviour — a logout
// request that carries no refresh_token is a 200 no-op (there is nothing to
// revoke) rather than the old 400 "Invalid request body".
func TestLogoutRevokesRefreshToken(t *testing.T) {
	email := "logout-revoke@example.test"
	password := "logoutSecret!123"

	// Register + login manually so we hold the real refresh token.
	code := doJSON(t, http.MethodPost, "/auth/register", "", map[string]string{
		"email": email, "password": password, "display_name": "Logout Revoke User",
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

	// Sanity check: the refresh token works before logout.
	code = doJSON(t, http.MethodPost, "/auth/refresh", "",
		map[string]string{"refresh_token": login.RefreshToken}, nil)
	require.Equal(t, http.StatusOK, code, "refresh should work before logout")

	// Hardening: a logout with no body is a 200 no-op — nothing to revoke.
	status, raw := restCall(t, http.MethodPost, "/auth/logout", login.AccessToken, nil)
	assert.Equalf(t, http.StatusOK, status,
		"empty-body logout must be a no-op success, got %d: %s", status, string(raw))

	// The no-op logout must NOT have revoked the still-valid token.
	code = doJSON(t, http.MethodPost, "/auth/refresh", "",
		map[string]string{"refresh_token": login.RefreshToken}, nil)
	require.Equal(t, http.StatusOK, code, "empty-body logout must not revoke the token")

	// Real logout: send the refresh token in the JSON body.
	code = doJSON(t, http.MethodPost, "/auth/logout", login.AccessToken,
		map[string]string{"refresh_token": login.RefreshToken}, nil)
	require.Equal(t, http.StatusOK, code)

	// Replaying the same refresh token after logout must be rejected.
	status, _ = restCall(t, http.MethodPost, "/auth/refresh", "",
		map[string]string{"refresh_token": login.RefreshToken})
	assert.Equal(t, http.StatusUnauthorized, status,
		"refresh with a logged-out token must 401")
}
