//go:build integration

package integration_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// SCOPE: POST /auth/setup is first-run only. By the time this runs the shared
// server already has users (other tests register them), so setup must be
// refused with 409 — it can never be used to mint an admin on a live server.
// The happy path (empty server -> admin) is covered by the usecase unit tests
// and by the e2e suite against a fresh container.
func TestSetupRefusedOnceUsersExist(t *testing.T) {
	// Make sure at least one user exists regardless of test ordering.
	doJSON(t, http.MethodPost, "/auth/register", "", map[string]string{
		"email": "setup-guard@example.test", "password": "SecurePass123!", "display_name": "Guard",
	}, nil)

	code := doJSON(t, http.MethodPost, "/auth/setup", "", map[string]string{
		"username": "intruder", "email": "intruder@example.test",
		"password": "SecurePass123!", "display_name": "Intruder",
	}, nil)

	assert.Equal(t, http.StatusConflict, code)
}
