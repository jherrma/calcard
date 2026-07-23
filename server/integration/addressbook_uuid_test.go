//go:build integration

package integration_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAddressBookRoutesRequireUUID locks the #52 contract for the address-book
// family: the CRUD, contacts and share routes take the address-book UUID
// (matching the sibling calendar routes), and the old numeric id is rejected
// with 404 (not leaked as 400/403), so the identifier form can't silently drift
// back to numeric.
func TestAddressBookRoutesRequireUUID(t *testing.T) {
	token := registerAndLogin(t, "ab-uuid@example.test", "abUUID!123", "AB UUID User")
	shareeEmail := "ab-uuid-sharee@example.test"
	registerAndLogin(t, shareeEmail, "abUUID!123", "AB UUID Sharee")

	abID, abUUID := createAddressBook(t, token, "AB UUID Book")

	// GET the address book: the UUID works; the numeric id must 404.
	status, _ := restCall(t, http.MethodGet, "/addressbooks/"+abUUID, token, nil)
	assert.Equal(t, http.StatusOK, status, "GET address book via UUID must succeed")
	status, _ = restCall(t, http.MethodGet, "/addressbooks/"+uintStr(abID), token, nil)
	assert.Equal(t, http.StatusNotFound, status, "numeric addressbook id must 404 on GET")

	// Contacts list: the UUID works; the numeric id must 404.
	status, _ = restCall(t, http.MethodGet, "/addressbooks/"+abUUID+"/contacts", token, nil)
	assert.Equal(t, http.StatusOK, status, "contacts list via UUID must succeed")
	status, _ = restCall(t, http.MethodGet, "/addressbooks/"+uintStr(abID)+"/contacts", token, nil)
	assert.Equal(t, http.StatusNotFound, status, "numeric addressbook id must 404 on contacts list")

	// Share create: the UUID works (201); the numeric id must 404.
	var shareResp struct {
		ID string `json:"id"`
	}
	code := doJSONRaw(t, http.MethodPost, "/addressbooks/"+abUUID+"/shares", token,
		map[string]string{"user_identifier": shareeEmail, "permission": "read"}, &shareResp)
	require.Equal(t, http.StatusCreated, code, "share create via UUID must succeed")

	status, _ = restCall(t, http.MethodPost, "/addressbooks/"+uintStr(abID)+"/shares", token, nil)
	assert.Equal(t, http.StatusNotFound, status, "numeric addressbook id must 404 on share create")
}
