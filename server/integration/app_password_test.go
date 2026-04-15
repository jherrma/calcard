//go:build integration

package integration_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAppPasswordRevocation proves the security-critical property of app
// passwords: once revoked, they can no longer authenticate DAV requests.
// A bug here means a user who rotates a lost / stolen DAV credential still
// can't stop the attacker — the revoked password keeps working.
func TestAppPasswordRevocation(t *testing.T) {
	email := "apprev@example.test"
	password := "apprevSecret!123"
	token, _ := registerAndLoginFull(t, email, password, "AppRev User")

	// --- Create two app passwords so we can verify that revoking one does
	// not affect the other. Name them distinctly so the list endpoint's
	// response is unambiguous.
	_, pass1 := createAppPassword(t, token, "keep-me")
	_, pass2 := createAppPassword(t, token, "revoke-me")

	// Both credentials must work before any revocation — this also serves
	// as a baseline for the post-revoke assertions.
	status, _, _ := davCall(t, "PROPFIND", "/dav/", email, pass1, propfindPrincipalBody, depthHeader("0"))
	require.Equal(t, http.StatusMultiStatus, status, "keep-me: PROPFIND must work pre-revoke")
	status, _, _ = davCall(t, "PROPFIND", "/dav/", email, pass2, propfindPrincipalBody, depthHeader("0"))
	require.Equal(t, http.StatusMultiStatus, status, "revoke-me: PROPFIND must work pre-revoke")

	// --- Find the `revoke-me` password's UUID via GET /app-passwords -----
	var list struct {
		AppPasswords []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"app_passwords"`
	}
	code := doJSON(t, http.MethodGet, "/app-passwords/", token, nil, &list)
	require.Equal(t, http.StatusOK, code)
	require.Len(t, list.AppPasswords, 2, "list endpoint should reflect both app passwords we just created")

	var revokeUUID string
	for _, ap := range list.AppPasswords {
		if ap.Name == "revoke-me" {
			revokeUUID = ap.ID
			break
		}
	}
	require.NotEmpty(t, revokeUUID, "list must surface the named app password's UUID")

	// --- Revoke it ------------------------------------------------------
	status, raw := restCall(t, http.MethodDelete, "/app-passwords/"+revokeUUID, token, nil)
	require.Equalf(t, http.StatusNoContent, status, "revoke app password: %s", errorMessage(raw))

	// The list endpoint should no longer return the revoked entry.
	code = doJSON(t, http.MethodGet, "/app-passwords/", token, nil, &list)
	require.Equal(t, http.StatusOK, code)
	require.Len(t, list.AppPasswords, 1, "list must drop the revoked app password")
	assert.Equal(t, "keep-me", list.AppPasswords[0].Name)

	// --- The revoked credential must now fail DAV auth ------------------
	// This is the whole point of the test: if the password still authenticates
	// here, revocation is cosmetic and the attacker keeps their access.
	status, _, body := davCall(t, "PROPFIND", "/dav/", email, pass2, propfindPrincipalBody, depthHeader("0"))
	assert.Equalf(t, http.StatusUnauthorized, status,
		"revoked app password must no longer authenticate DAV (got %d, body: %s)", status, string(body))

	// The other app password must still work — revocation is scoped to one
	// credential, not all of them.
	status, _, _ = davCall(t, "PROPFIND", "/dav/", email, pass1, propfindPrincipalBody, depthHeader("0"))
	assert.Equal(t, http.StatusMultiStatus, status, "keep-me: must still work after revoking the other credential")
}
