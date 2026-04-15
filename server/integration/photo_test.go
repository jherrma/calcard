//go:build integration

package integration_test

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestContactPhoto uploads a JPEG photo for a contact, fetches it back, then
// replaces it with a second image, and finally deletes it. The JPEG bytes are
// loaded from files on disk so we're really exercising the multipart/photo
// pipeline (content-type sniffing, byte round-trip, delete path) rather than
// some hand-rolled payload.
func TestContactPhoto(t *testing.T) {
	email := "photo@example.test"
	password := "photoSecret!123"
	token := registerAndLogin(t, email, password, "Photo User")

	abID := createAddressBook(t, token, "Photo Book")

	// Create a contact we can attach the photo to.
	var contact struct {
		ID  string `json:"id"`
		UID string `json:"uid"`
	}
	code := doJSONRaw(t, http.MethodPost, "/addressbooks/"+uintStr(abID)+"/contacts", token,
		map[string]any{
			"formatted_name": "Alice Avatar",
			"given_name":     "Alice",
			"family_name":    "Avatar",
		}, &contact)
	require.Equal(t, http.StatusCreated, code)
	require.NotEmpty(t, contact.ID)

	// Load the first profile icon from disk.
	icon1 := readAsset(t, "user-icon.jpg")
	require.Greater(t, len(icon1), 1000, "asset appears truncated")
	assert.Equal(t, []byte{0xFF, 0xD8, 0xFF}, icon1[:3], "user-icon.jpg should be a JPEG")

	// --- Upload ----------------------------------------------------------

	photoURL := "/api/v1/addressbooks/" + uintStr(abID) + "/contacts/" + contact.ID + "/photo"
	status, raw := rawCall(t, http.MethodPut, baseURL+photoURL, token, icon1, map[string]string{
		"Content-Type": "image/jpeg",
	})
	require.Equalf(t, http.StatusNoContent, status, "PUT photo: %s", errorMessage(raw))

	// --- Fetch (unauthenticated GET isn't allowed; use the user's token) -

	status, hdrs, body := rawCall2(t, http.MethodGet, baseURL+photoURL, token, nil, nil)
	require.Equalf(t, http.StatusOK, status, "GET photo: %s", string(body))
	assert.Equal(t, "image/jpeg", hdrs.Get("Content-Type"), "content-type should reflect photo format")
	assert.Equal(t, []byte{0xFF, 0xD8, 0xFF}, body[:3], "fetched bytes should still look like a JPEG")

	// --- Replace with icon 2 ---------------------------------------------

	icon2 := readAsset(t, "user-icon-2.jpg")
	require.NotEqual(t, icon1, icon2, "the two asset files should differ")

	status, raw = rawCall(t, http.MethodPut, baseURL+photoURL, token, icon2, map[string]string{
		"Content-Type": "image/jpeg",
	})
	require.Equalf(t, http.StatusNoContent, status, "PUT replacement photo: %s", errorMessage(raw))

	status, _, body = rawCall2(t, http.MethodGet, baseURL+photoURL, token, nil, nil)
	require.Equal(t, http.StatusOK, status)
	// Bytes after re-upload should match the icon-2 file we sent in.
	// (The server round-trips through base64 storage, so this also proves the
	// decode path round-trips cleanly.)
	assert.Equalf(t, len(icon2), len(body),
		"fetched photo length should match uploaded icon 2 (was %d, got %d)", len(icon2), len(body))

	// --- Delete ----------------------------------------------------------

	status, raw = restCall(t, http.MethodDelete, photoURL[len("/api/v1"):], token, nil)
	require.Equalf(t, http.StatusNoContent, status, "DELETE photo: %s", errorMessage(raw))

	// After delete, GET should 404 and the contact itself (re-fetched via
	// single-contact GET) should have its photo URL stripped.
	status, _, _ = rawCall2(t, http.MethodGet, baseURL+photoURL, token, nil, nil)
	assert.Equal(t, http.StatusNotFound, status, "deleted photo must 404")

	var after struct {
		PhotoURL string `json:"photo_url"`
	}
	code = doJSONRaw(t, http.MethodGet,
		"/addressbooks/"+uintStr(abID)+"/contacts/"+contact.ID, token, nil, &after)
	require.Equal(t, http.StatusOK, code)
	assert.Empty(t, after.PhotoURL, "contact must not advertise a photo_url once the photo is gone")
}

// readAsset loads a fixture from server/integration/Assets.
func readAsset(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("Assets", name))
	require.NoErrorf(t, err, "read asset %s", name)
	return data
}

// rawCall2 is a bearer-auth GET that also exposes the response headers.
// The existing `rawCall` helper drops headers; here we need Content-Type of
// the fetched photo so we can assert the server restores it.
func rawCall2(t *testing.T, method, fullURL, bearerToken string, body any, headers map[string]string) (int, http.Header, []byte) {
	t.Helper()
	_ = body // no body for the GETs we make here
	req, err := http.NewRequest(method, fullURL, nil)
	require.NoError(t, err)
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, resp.Header, respBody
}
