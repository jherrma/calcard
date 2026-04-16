//go:build integration

package integration_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWellKnownRedirects verifies that /.well-known/caldav and
// /.well-known/carddav return 301 redirects to /dav/. Every DAV client
// (Apple Calendar, DAVx5, Thunderbird) hits these during account setup —
// a broken redirect means no native client can discover the server.
func TestWellKnownRedirects(t *testing.T) {
	// httpClient follows redirects by default; use a raw client that
	// stops at the first response so we can inspect the 301.
	noRedirect := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	for _, path := range []string{"/.well-known/caldav", "/.well-known/carddav"} {
		t.Run(path, func(t *testing.T) {
			resp, err := noRedirect.Get(baseURL + path)
			require.NoError(t, err)
			_ = resp.Body.Close()
			assert.Equal(t, http.StatusMovedPermanently, resp.StatusCode,
				"%s should 301", path)
			assert.Equal(t, "/dav/", resp.Header.Get("Location"),
				"%s should redirect to /dav/", path)
		})
	}
}

// TestAuthMethods verifies GET /auth/methods returns a JSON list that at
// least contains the "local" (email+password) method. The frontend login
// page calls this endpoint to decide which buttons to render — a broken
// response means a blank login screen.
func TestAuthMethods(t *testing.T) {
	var resp struct {
		Methods []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
			Name string `json:"name"`
		} `json:"methods"`
	}
	code := doJSON(t, http.MethodGet, "/auth/methods", "", nil, &resp)
	require.Equal(t, http.StatusOK, code)
	require.NotEmpty(t, resp.Methods, "auth/methods must return at least one method")

	foundLocal := false
	for _, m := range resp.Methods {
		if m.ID == "local" && m.Type == "local" {
			foundLocal = true
			break
		}
	}
	assert.True(t, foundLocal, "auth/methods must include the local (email+password) method")
}

// TestReadinessProbe verifies GET /ready returns 200. Unlike /health
// (which only proves the process is alive), /ready pings the database.
// Kubernetes and load-balancer health checks poll this endpoint — a
// failure here stalls rolling updates.
func TestReadinessProbe(t *testing.T) {
	resp, err := http.Get(baseURL + "/ready")
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode, "/ready must return 200")
}

// TestUpdateProfile verifies PATCH /users/me can change display_name and
// that a subsequent GET /users/me reflects the change. This is the only
// way users edit their profile in the web UI.
func TestUpdateProfile(t *testing.T) {
	email := "profile-patch@example.test"
	password := "patchSecret!123"
	token := registerAndLogin(t, email, password, "Original Name")

	// Verify initial state.
	var before struct {
		DisplayName string `json:"display_name"`
		Email       string `json:"email"`
	}
	code := doJSON(t, http.MethodGet, "/users/me", token, nil, &before)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "Original Name", before.DisplayName)

	// PATCH display_name.
	var after struct {
		DisplayName string `json:"display_name"`
	}
	code = doJSON(t, http.MethodPatch, "/users/me", token, map[string]string{
		"display_name": "Updated Name",
	}, &after)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "Updated Name", after.DisplayName)

	// GET confirms the change persisted.
	code = doJSON(t, http.MethodGet, "/users/me", token, nil, &after)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "Updated Name", after.DisplayName)
}

// TestAddressBookExport verifies GET /addressbooks/:id/export returns a
// parseable text/vcard body containing the seeded contact's FN. The
// calendar-export sibling is already tested; this covers the address-book
// analogue.
func TestAddressBookExport(t *testing.T) {
	email := "abexport@example.test"
	password := "abexportSecret!123"
	token := registerAndLogin(t, email, password, "AB Export User")

	abID := createAddressBook(t, token, "Exportable")
	_, fn := createSeededContact(t, token, abID, "Exportable", 0)

	status, raw := restCall(t, http.MethodGet,
		"/addressbooks/"+uintStr(abID)+"/export", token, nil)
	require.Equal(t, http.StatusOK, status)

	body := string(raw)
	assert.Contains(t, body, "BEGIN:VCARD", "export must be valid vCard")
	assert.Contains(t, body, fn, "export must include the seeded contact's FN")
}
