//go:build integration

package integration_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDedicatedCalDAVCredential covers the standalone DAV credential flow
// — separate from user email + app password — which is the way to hand out
// scoped, read-only access to a third party without giving them your real
// account. A bug in this flow typically means *write* credentials silently
// get read-only semantics, or worse, read-only credentials accept writes.
func TestDedicatedCalDAVCredential(t *testing.T) {
	email := "caldav-cred@example.test"
	password := "credSecret!123"
	token, username := registerAndLoginFull(t, email, password, "CalDAV Cred User")

	// The user always has a default Personal calendar we can PROPFIND.
	idx := listCalendarsIndex(t, token)
	personal, ok := idx["Personal"]
	require.True(t, ok)
	calPath := personal.UUID + ".ics"

	// --- Create a read-only CalDAV credential ---------------------------
	rawPass := "ReadOnlyPass_v3rY!S1x"
	// The credential repo lowercases the username on lookup (see
	// GetByUsername) but Create stores whatever the caller supplied, so
	// a mixed-case username would silently fail to authenticate. We ensure
	// the test uses lowercase to match the lookup path.
	credUser := "readonly-" + time.Now().Format("20060102t150405")
	var created struct {
		ID         string `json:"id"` // UUID of the credential row
		Username   string `json:"username"`
		Permission string `json:"permission"`
	}
	code := doJSONRaw(t, http.MethodPost, "/caldav-credentials/", token, map[string]any{
		"name":       "Read-only sync",
		"username":   credUser,
		"password":   rawPass,
		"permission": "read",
	}, &created)
	require.Equalf(t, http.StatusCreated, code, "create caldav credential: code %d", code)
	require.NotEmpty(t, created.ID)
	assert.Equal(t, credUser, created.Username)
	assert.Equal(t, "read", created.Permission)

	// --- List surfaces the credential -----------------------------------
	var list struct {
		Credentials []struct {
			ID         string `json:"id"`
			Username   string `json:"username"`
			Permission string `json:"permission"`
		} `json:"credentials"`
	}
	code = doJSONRaw(t, http.MethodGet, "/caldav-credentials/", token, nil, &list)
	require.Equal(t, http.StatusOK, code)
	var matching bool
	for _, c := range list.Credentials {
		if c.Username == credUser {
			matching = true
			assert.Equal(t, "read", c.Permission)
			break
		}
	}
	assert.True(t, matching, "list must surface the credential we just created")

	// --- Authenticate a DAV request with it (read) ----------------------
	propfind := propfindCalendarBody
	status, _, body := davCall(t, "PROPFIND",
		"/dav/"+username+"/calendars/"+calPath+"/",
		credUser, rawPass, propfind, depthHeader("0"))
	require.Equalf(t, http.StatusMultiStatus, status,
		"read-only credential must authorize PROPFIND: %s", string(body))

	// --- Writes must be rejected (read-only scope) ----------------------
	eventPath := "/dav/" + username + "/calendars/" + calPath + "/readonly-write.ics"
	ical := buildMinimalVEvent("readonly-write", "Should never land",
		time.Date(2032, 3, 1, 9, 0, 0, 0, time.UTC))
	status, _, body = davCall(t, "PUT", eventPath, credUser, rawPass, ical,
		map[string]string{"Content-Type": "text/calendar; charset=utf-8"})
	assert.Equalf(t, http.StatusForbidden, status,
		"read-only CalDAV credential must NOT accept PUT (got %d, body: %s)", status, string(body))

	// --- Revoke ---------------------------------------------------------
	status, raw := restCall(t, http.MethodDelete, "/caldav-credentials/"+created.ID, token, nil)
	require.Equalf(t, http.StatusNoContent, status, "revoke: %s", errorMessage(raw))

	// --- After revoke, auth must fail -----------------------------------
	status, _, _ = davCall(t, "PROPFIND",
		"/dav/"+username+"/calendars/"+calPath+"/",
		credUser, rawPass, propfind, depthHeader("0"))
	assert.Equal(t, http.StatusUnauthorized, status,
		"revoked DAV credential must no longer authenticate")
}

// TestDedicatedCardDAVCredential is the analogue for CardDAV. A read-write
// credential exercises the happy path and proves writes are allowed on an
// addressbook the credential's owner has access to.
func TestDedicatedCardDAVCredential(t *testing.T) {
	email := "carddav-cred@example.test"
	password := "credSecret!123"
	token, username := registerAndLoginFull(t, email, password, "CardDAV Cred User")

	// Grab the default Contacts addressbook URL-slug.
	var wrap struct {
		AddressBooks []struct {
			Name string `json:"Name"`
			Path string `json:"Path"`
		} `json:"addressbooks"`
	}
	code := doJSONRaw(t, http.MethodGet, "/addressbooks/", token, nil, &wrap)
	require.Equal(t, http.StatusOK, code)
	var abPath string
	for _, ab := range wrap.AddressBooks {
		if ab.Name == "Contacts" {
			abPath = ab.Path
			break
		}
	}
	require.NotEmpty(t, abPath)

	rawPass := "ReadWritePass_v3rY!S1x"
	credUser := "rw-" + time.Now().Format("20060102t150405")
	var created struct {
		ID         string `json:"id"` // UUID of the credential row
		Username   string `json:"username"`
		Permission string `json:"permission"`
	}
	code = doJSONRaw(t, http.MethodPost, "/carddav-credentials/", token, map[string]any{
		"name":       "Read-write sync",
		"username":   credUser,
		"password":   rawPass,
		"permission": "read-write",
	}, &created)
	require.Equalf(t, http.StatusCreated, code, "create carddav credential: code %d", code)
	assert.Equal(t, "read-write", created.Permission)

	// --- PROPFIND the addressbook collection ----------------------------
	collection := "/dav/" + username + "/addressbooks/" + abPath + "/"
	status, _, body := davCall(t, "PROPFIND", collection, credUser, rawPass,
		propfindAddressBookBody, depthHeader("0"))
	require.Equalf(t, http.StatusMultiStatus, status,
		"read-write credential must authorize PROPFIND: %s", string(body))

	// --- PUT a contact via the dedicated CardDAV credential -------------
	contactUID := "carddav-cred-" + time.Now().Format("20060102T150405")
	contactPath := collection + contactUID + ".vcf"
	vcard := buildMinimalVCard(contactUID, "CarddavCred Test", "CardDav", "Cred")
	status, _, body = davCall(t, "PUT", contactPath, credUser, rawPass, vcard,
		map[string]string{"Content-Type": "text/vcard; charset=utf-8"})
	require.Contains(t, []int{http.StatusCreated, http.StatusNoContent}, status,
		"read-write carddav credential must accept PUT: %s", string(body))

	// Cross-check via REST that the contact landed in the right addressbook.
	// We can't use the credUser token for REST (it's a DAV credential, not
	// a JWT), so we re-use the user's bearer token.
	status, raw := restCall(t, http.MethodGet, "/contacts/search?q=CarddavCred", token, nil)
	require.Equal(t, http.StatusOK, status, errorMessage(raw))
	assert.Contains(t, string(raw), contactUID,
		"contact PUT via CardDAV credential should be visible to the owner")

	// --- Revoke, then auth fails ----------------------------------------
	status, raw = restCall(t, http.MethodDelete, "/carddav-credentials/"+created.ID, token, nil)
	require.Equalf(t, http.StatusNoContent, status, "revoke: %s", errorMessage(raw))

	status, _, _ = davCall(t, "PROPFIND", collection, credUser, rawPass,
		propfindAddressBookBody, depthHeader("0"))
	assert.Equal(t, http.StatusUnauthorized, status)
}
