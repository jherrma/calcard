//go:build integration

package integration_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDAVMkcolCalendar creates a new calendar via MKCOL over CalDAV and
// asserts that it appears in the REST list afterwards. Real clients
// (Apple Calendar "New Calendar", DAVx5 "+ calendar") go through this
// endpoint — breaking it means "create a calendar" silently stops working
// for every native client while the web UI keeps functioning.
func TestDAVMkcolCalendar(t *testing.T) {
	email := "mkcol-cal@example.test"
	password := "mkcolSecret!123"
	token, username := registerAndLoginFull(t, email, password, "Mkcol Cal User")
	_, appPass := createAppPassword(t, token, "mkcol-cal-test")

	slug := "dav-created-" + time.Now().Format("20060102t150405")
	path := "/dav/" + username + "/calendars/" + slug + "/"

	mkcol := `<?xml version="1.0" encoding="utf-8"?>
<D:mkcol xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">
  <D:set>
    <D:prop>
      <D:resourcetype>
        <D:collection/>
        <C:calendar/>
      </D:resourcetype>
      <D:displayname>DAV-Created Calendar</D:displayname>
    </D:prop>
  </D:set>
</D:mkcol>`

	status, _, body := davCall(t, "MKCOL", path, email, appPass, mkcol,
		map[string]string{"Content-Type": "application/xml; charset=utf-8"})
	require.Containsf(t, []int{http.StatusCreated, http.StatusOK, http.StatusNoContent}, status,
		"MKCOL calendar should succeed: %d %s", status, string(body))

	// PROPFIND the new collection — must exist and be a calendar.
	status, _, body = davCall(t, "PROPFIND", path, email, appPass,
		propfindCalendarBody, depthHeader("0"))
	require.Equal(t, http.StatusMultiStatus, status)
	assert.Contains(t, string(body), "calendar xmlns=\"urn:ietf:params:xml:ns:caldav\"",
		"new collection must advertise the calendar resourcetype")

	// REST list should also see it. The slug is stored as the calendar.Path
	// field, and the list exposes that via the calendar JSON — there's no
	// matching `displayname` roundtrip here (the MKCOL sets displayname on
	// the DAV side; whether the server persists it to calendar.Name depends
	// on the backend). We assert visibility by path presence.
	var wrap struct {
		Calendars []struct {
			Path string `json:"path"`
			Name string `json:"name"`
		} `json:"calendars"`
	}
	code := doJSONRaw(t, http.MethodGet, "/calendars/", token, nil, &wrap)
	require.Equal(t, http.StatusOK, code)
	found := false
	for _, c := range wrap.Calendars {
		if c.Path == slug {
			found = true
			break
		}
	}
	assert.True(t, found, "DAV-created calendar must be visible in REST /calendars list (slug %q)", slug)
}

// TestDAVMkcolAddressBook creates an addressbook via MKCOL over CardDAV.
func TestDAVMkcolAddressBook(t *testing.T) {
	email := "mkcol-ab@example.test"
	password := "mkcolSecret!123"
	token, username := registerAndLoginFull(t, email, password, "Mkcol AB User")
	_, appPass := createAppPassword(t, token, "mkcol-ab-test")

	slug := "davbook-" + time.Now().Format("20060102t150405")
	path := "/dav/" + username + "/addressbooks/" + slug + "/"

	mkcol := `<?xml version="1.0" encoding="utf-8"?>
<D:mkcol xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:carddav">
  <D:set>
    <D:prop>
      <D:resourcetype>
        <D:collection/>
        <C:addressbook/>
      </D:resourcetype>
      <D:displayname>DAV-Created Addressbook</D:displayname>
    </D:prop>
  </D:set>
</D:mkcol>`

	status, _, body := davCall(t, "MKCOL", path, email, appPass, mkcol,
		map[string]string{"Content-Type": "application/xml; charset=utf-8"})
	require.Containsf(t, []int{http.StatusCreated, http.StatusOK, http.StatusNoContent}, status,
		"MKCOL addressbook should succeed: %d %s", status, string(body))

	// REST list must include the new addressbook by Path.
	var wrap struct {
		AddressBooks []struct {
			Name string `json:"Name"`
			Path string `json:"Path"`
		} `json:"addressbooks"`
	}
	code := doJSONRaw(t, http.MethodGet, "/addressbooks/", token, nil, &wrap)
	require.Equal(t, http.StatusOK, code)
	found := false
	for _, ab := range wrap.AddressBooks {
		if ab.Path == slug {
			found = true
			break
		}
	}
	assert.True(t, found, "DAV-created addressbook must be visible in REST /addressbooks list")
}

// TestDAVDeleteAddressBook deletes an addressbook collection via DAV DELETE.
// The CalDAV side does not expose a DeleteCalendar hook in the emersion
// library, so this test is CardDAV-only.
func TestDAVDeleteAddressBook(t *testing.T) {
	email := "dav-del-ab@example.test"
	password := "delSecret!123"
	token, username := registerAndLoginFull(t, email, password, "DAV Del AB")
	_, appPass := createAppPassword(t, token, "dav-del-ab-test")

	// Create a second addressbook so we're not trying to delete the user's
	// only one (the domain forbids that and it's tested elsewhere).
	abID := createAddressBook(t, token, "ToBeDeleted")
	idx := listAddressBooksIndex(t, token)
	var abPath string
	// Need to grab the URL slug — the list endpoint returns it as `Path`.
	var wrap struct {
		AddressBooks []struct {
			ID   uint   `json:"ID"`
			Path string `json:"Path"`
		} `json:"addressbooks"`
	}
	code := doJSONRaw(t, http.MethodGet, "/addressbooks/", token, nil, &wrap)
	require.Equal(t, http.StatusOK, code)
	for _, ab := range wrap.AddressBooks {
		if ab.ID == abID {
			abPath = ab.Path
			break
		}
	}
	require.NotEmpty(t, abPath)
	_ = idx

	collection := "/dav/" + username + "/addressbooks/" + abPath + "/"
	status, _, body := davCall(t, "DELETE", collection, email, appPass, "", nil)
	require.Containsf(t, []int{http.StatusOK, http.StatusNoContent}, status,
		"DELETE addressbook collection: %d %s", status, string(body))

	// REST list must no longer show it.
	code = doJSONRaw(t, http.MethodGet, "/addressbooks/", token, nil, &wrap)
	require.Equal(t, http.StatusOK, code)
	for _, ab := range wrap.AddressBooks {
		assert.NotEqualf(t, abID, ab.ID,
			"deleted addressbook (id=%d) must not remain in the REST list", abID)
	}
}
