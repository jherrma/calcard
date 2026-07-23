//go:build integration

package integration_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestContactEditPreservesUnknownProps is the regression test for M6: editing a
// contact through the REST/web-UI path must not drop vCard properties the UI
// doesn't manage. A contact PUT over CardDAV with CATEGORIES, X-CUSTOM-FIELD
// and IMPP, then renamed via REST, must still carry those properties when read
// back over CardDAV.
func TestContactEditPreservesUnknownProps(t *testing.T) {
	email := "contact-preserve@example.test"
	token, username := registerAndLoginFull(t, email, "preserveSecret!123", "Preserve User")
	_, appPass := createAppPassword(t, token, "preserve")

	abPath := addressBookPath(t, token, "Contacts")
	require.NotEmpty(t, abPath)
	abUUID := addressBookUUID(t, token, "Contacts")
	require.NotEmpty(t, abUUID)

	uid := "preserve-uid"
	collection := "/dav/" + username + "/addressbooks/" + abPath + "/"
	path := collection + uid + ".vcf"
	vcard := "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:" + uid + "\r\n" +
		"FN:Original Name\r\nN:Name;Original;;;\r\n" +
		"CATEGORIES:Work,Important\r\n" +
		"X-CUSTOM-FIELD:custom-value\r\n" +
		"IMPP:xmpp:user@chat.example\r\n" +
		"item1.URL:https://x.example\r\n" +
		"item1.X-ABLabel:MyLabel\r\n" +
		"END:VCARD\r\n"

	status, _, body := davCall(t, "PUT", path, email, appPass, vcard,
		map[string]string{"Content-Type": "text/vcard; charset=utf-8"})
	require.Containsf(t, []int{http.StatusCreated, http.StatusNoContent, http.StatusOK}, status, "PUT: %s", string(body))

	// Look up the contact's internal UUID via the REST list.
	var listResp struct {
		Contacts []struct {
			ID string `json:"id"`
		} `json:"Contacts"`
	}
	require.Equal(t, http.StatusOK, doJSONRaw(t, http.MethodGet, "/addressbooks/"+abUUID+"/contacts", token, nil, &listResp))
	require.NotEmpty(t, listResp.Contacts)
	contactID := listResp.Contacts[0].ID

	// Rename the contact via the REST (web-UI) path.
	newName := "Renamed Person"
	status, raw := restCall(t, http.MethodPatch, "/addressbooks/"+abUUID+"/contacts/"+contactID, token,
		map[string]any{"formatted_name": newName, "given_name": "Renamed", "family_name": "Person"})
	require.Equalf(t, http.StatusOK, status, "rename contact: %s", string(raw))

	// Read it back over CardDAV — the unmanaged properties must still be there.
	status, _, getBody := davCall(t, "GET", path, email, appPass, "", nil)
	require.Equalf(t, http.StatusOK, status, "GET: %s", string(getBody))
	served := string(getBody)
	assert.Contains(t, served, "FN:Renamed Person", "managed field must reflect the edit")
	assert.NotContains(t, served, "Original Name", "old FN must be gone")
	// CATEGORIES must survive as two distinct categories. Since #44 the server
	// re-serializes the comma list as repeated CATEGORIES instances (the RFC
	// 6350 §6.7.1-equivalent form) so go-vcard's encoder can never escape the
	// separator into a single mangled "Work\,Important" value.
	assert.Contains(t, served, "CATEGORIES:Work", "first category must survive")
	assert.Contains(t, served, "CATEGORIES:Important", "second category must survive as its own instance")
	assert.NotContains(t, served, `Work\,Important`, "commas in a category list must not be escaped into one value")
	assert.Contains(t, served, "X-CUSTOM-FIELD:custom-value", "X-CUSTOM-FIELD must survive a web-UI edit")
	assert.Contains(t, served, "chat.example", "IMPP must survive a web-UI edit")
	// The grouped item1.URL must keep its group so the paired item1.X-ABLabel is
	// not orphaned (managed-field re-adds previously dropped the group).
	upper := strings.ToUpper(served)
	assert.Contains(t, upper, "ITEM1.URL:HTTPS://X.EXAMPLE", "grouped URL must keep its item1. group")
	assert.Contains(t, upper, "ITEM1.X-ABLABEL", "grouped X-ABLabel must survive with its group")
}
