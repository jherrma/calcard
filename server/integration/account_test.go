//go:build integration

package integration_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAccountDeletion exercises DELETE /users/me: wrong password → 401,
// wrong confirmation text → 4xx, correct credentials → 204 and the user
// is actually removed (login no longer works, old access token rejected).
func TestAccountDeletion(t *testing.T) {
	email := "delete-me@example.test"
	password := "deleteSecret!123"
	token := registerAndLogin(t, email, password, "Delete Me")

	// Seed something — calendar + contact — just to prove the account owns
	// state before deletion. We don't assert cascade cleanup here (that
	// behavior lives at the DB layer and depends on gorm soft-delete /
	// foreign-key constraints), but it's a useful smoke.
	_, _ = createCalendar(t, token, "To Be Deleted", "#123456")

	// --- Wrong password must 401 ---------------------------------------
	status, _ := restCall(t, http.MethodDelete, "/users/me", token, map[string]string{
		"password":     "wrong-password",
		"confirmation": "DELETE",
	})
	assert.Equal(t, http.StatusUnauthorized, status,
		"delete with wrong password must 401, not silently succeed")

	// --- Wrong confirmation text must 4xx ------------------------------
	status, _ = restCall(t, http.MethodDelete, "/users/me", token, map[string]string{
		"password":     password,
		"confirmation": "yes please",
	})
	assert.True(t, status == http.StatusBadRequest || status == http.StatusUnauthorized,
		"missing/incorrect confirmation text must 4xx, got %d", status)

	// --- Missing confirmation key must 4xx -----------------------------
	status, _ = restCall(t, http.MethodDelete, "/users/me", token, map[string]string{
		"password": password,
	})
	assert.GreaterOrEqual(t, status, 400)
	assert.Less(t, status, 500, "missing confirmation must 4xx, not 5xx")

	// --- Account is still alive after the rejected attempts ------------
	status, _ = restCall(t, http.MethodGet, "/users/me", token, nil)
	require.Equalf(t, http.StatusOK, status,
		"account must still exist after failed deletion attempts")

	// --- Correct password + confirmation deletes the account ----------
	status, raw := restCall(t, http.MethodDelete, "/users/me", token, map[string]string{
		"password":     password,
		"confirmation": "DELETE",
	})
	require.Equalf(t, http.StatusNoContent, status, "account deletion: %s", errorMessage(raw))

	// --- After deletion the old token must be rejected -----------------
	status, _ = restCall(t, http.MethodGet, "/users/me", token, nil)
	assert.Equalf(t, http.StatusUnauthorized, status,
		"old access token must be invalidated once the user row is gone")

	// --- And login with the old credentials must fail ------------------
	status, _ = restCall(t, http.MethodPost, "/auth/login", "", map[string]string{
		"email":    email,
		"password": password,
	})
	assert.Equalf(t, http.StatusUnauthorized, status,
		"login with a deleted account must 401")
}
