//go:build integration

package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SCOPE: first-run setup (POST /auth/setup). The shared test server already has
// users by the time these run, so the happy path boots its own fresh server via
// bootServerWithConfig.

// freshCall is rawCall against a specific server, returning the decoded body.
func freshCall(t *testing.T, base, method, path, token string, body any) (int, map[string]any) {
	t.Helper()
	code, raw := rawCall(t, method, base+"/api/v1"+path, token, body, nil)
	out := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out) // 204 / non-JSON bodies leave it empty
	}
	return code, out
}

func dataOf(m map[string]any) map[string]any {
	d, _ := m["data"].(map[string]any)
	return d
}

func setupBody(username, email string) map[string]string {
	return map[string]string{
		"username": username, "email": email,
		"password": "SecurePass123!", "display_name": "Admin",
	}
}

// Setup is refused once users exist, so it can never mint an admin on a live server.
func TestSetupRefusedOnceUsersExist(t *testing.T) {
	doJSON(t, http.MethodPost, "/auth/register", "", map[string]string{
		"email": "setup-guard@example.test", "password": "SecurePass123!", "display_name": "Guard",
	}, nil)

	code := doJSON(t, http.MethodPost, "/auth/setup", "", setupBody("intruder", "intruder@example.test"), nil)

	assert.Equal(t, http.StatusConflict, code)
}

// Whole first-run journey on a pristine server: setup -> admin login ->
// is_admin everywhere -> setup closed -> later registrations are not admins.
func TestFirstRunSetupCreatesAdmin(t *testing.T) {
	base, shutdown := bootServerWithConfig(t, nil)
	t.Cleanup(shutdown)

	_, settings := freshCall(t, base, http.MethodGet, "/system/settings", "", nil)
	assert.Equal(t, false, dataOf(settings)["admin_configured"], "pristine server")

	// Invalid input is rejected without consuming the one-shot.
	code, _ := freshCall(t, base, http.MethodPost, "/auth/setup", "", setupBody("Bad Name", "admin@example.test"))
	assert.Equal(t, http.StatusBadRequest, code)

	code, created := freshCall(t, base, http.MethodPost, "/auth/setup", "", setupBody("Admin", "Admin@Example.test"))
	require.Equal(t, http.StatusOK, code, "setup must succeed on an empty server")
	assert.Equal(t, "admin", dataOf(created)["username"], "username is lower-cased")
	assert.Equal(t, "admin@example.test", dataOf(created)["email"])
	assert.Equal(t, true, dataOf(created)["is_active"], "admin is active at once")
	assert.Equal(t, true, dataOf(created)["email_verified"])

	// The admin can sign in and the flag shows on login and /users/me.
	code, login := freshCall(t, base, http.MethodPost, "/auth/login", "", map[string]string{
		"email": "admin@example.test", "password": "SecurePass123!",
	})
	require.Equal(t, http.StatusOK, code)
	user, _ := dataOf(login)["user"].(map[string]any)
	assert.Equal(t, true, user["is_admin"], "login response carries is_admin")
	token, _ := dataOf(login)["access_token"].(string)
	require.NotEmpty(t, token)

	_, me := freshCall(t, base, http.MethodGet, "/users/me", token, nil)
	assert.Equal(t, true, dataOf(me)["is_admin"], "GET /users/me carries is_admin")

	// Setup also provisioned the default calendar and address book.
	_, cals := freshCall(t, base, http.MethodGet, "/calendars", token, nil)
	assert.NotEmpty(t, cals["calendars"], "default calendar created")

	// Now closed for good.
	_, settings = freshCall(t, base, http.MethodGet, "/system/settings", "", nil)
	assert.Equal(t, true, dataOf(settings)["admin_configured"])
	code, _ = freshCall(t, base, http.MethodPost, "/auth/setup", "", setupBody("second", "second@example.test"))
	assert.Equal(t, http.StatusConflict, code, "second setup refused")

	// Ordinary registration still works and never yields an admin.
	code, _ = freshCall(t, base, http.MethodPost, "/auth/register", "", map[string]string{
		"email": "user@example.test", "password": "SecurePass123!", "display_name": "User",
	})
	require.Equal(t, http.StatusOK, code)
	_, ulogin := freshCall(t, base, http.MethodPost, "/auth/login", "", map[string]string{
		"email": "user@example.test", "password": "SecurePass123!",
	})
	uuser, _ := dataOf(ulogin)["user"].(map[string]any)
	assert.Equal(t, false, uuser["is_admin"], "registered users are not admins")
}

// If the only account deletes itself, setup must NOT re-open: Count includes
// soft-deleted rows. Otherwise anyone reaching the server could claim admin.
func TestSetupStaysClosedAfterLastAccountDeleted(t *testing.T) {
	base, shutdown := bootServerWithConfig(t, nil)
	t.Cleanup(shutdown)

	code, _ := freshCall(t, base, http.MethodPost, "/auth/setup", "", setupBody("admin", "admin@example.test"))
	require.Equal(t, http.StatusOK, code)
	_, login := freshCall(t, base, http.MethodPost, "/auth/login", "", map[string]string{
		"email": "admin@example.test", "password": "SecurePass123!",
	})
	token, _ := dataOf(login)["access_token"].(string)
	require.NotEmpty(t, token)

	code, _ = freshCall(t, base, http.MethodDelete, "/users/me", token, map[string]string{
		"password": "SecurePass123!", "confirmation": "DELETE",
	})
	require.Equal(t, http.StatusNoContent, code, "account deleted")

	_, settings := freshCall(t, base, http.MethodGet, "/system/settings", "", nil)
	assert.Equal(t, true, dataOf(settings)["admin_configured"],
		"a deleted account still counts, so the SPA does not bounce back to /auth/setup")
	code, _ = freshCall(t, base, http.MethodPost, "/auth/setup", "", setupBody("intruder", "intruder@example.test"))
	assert.Equal(t, http.StatusConflict, code, "setup must stay closed")
}
