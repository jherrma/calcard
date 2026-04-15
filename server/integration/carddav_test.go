//go:build integration

package integration_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCardDAV walks a minimal discovery + CRUD flow against the CardDAV
// endpoint, analogous to TestCalDAV but for address books / vCards. It uses
// the default "Contacts" address book that every user gets at registration.
func TestCardDAV(t *testing.T) {
	email := "carddav@example.test"
	password := "carddavSecret!123"
	token, username := registerAndLoginFull(t, email, password, "CardDAV User")

	_, appPassword := createAppPassword(t, token, "carddav-test")
	basicUser := email

	// Find the default "Contacts" address book and its URL-path slug.
	// AddressBook fields have no JSON tags → PascalCase field names.
	var wrap struct {
		AddressBooks []struct {
			ID   uint   `json:"ID"`
			UUID string `json:"UUID"`
			Name string `json:"Name"`
			Path string `json:"Path"`
		} `json:"addressbooks"`
	}
	code := doJSONRaw(t, http.MethodGet, "/addressbooks/", token, nil, &wrap)
	require.Equal(t, http.StatusOK, code)
	var abID uint
	var abPath string
	for _, ab := range wrap.AddressBooks {
		if ab.Name == "Contacts" {
			abID = ab.ID
			abPath = ab.Path
			break
		}
	}
	require.NotZero(t, abID, "default Contacts address book should exist")
	require.NotEmpty(t, abPath, "address book should have a URL-path slug")

	// --- PROPFIND: addressbook home set -----------------------------------

	homePath := "/dav/" + username + "/addressbooks/"
	status, _, body := davCall(t, "PROPFIND", homePath, basicUser, appPassword, propfindAddressBookHomeBody, depthHeader("1"))
	require.Equal(t, http.StatusMultiStatus, status, "PROPFIND home: %s", string(body))
	assert.Contains(t, string(body), abPath, "addressbook home listing should reference the Contacts addressbook")

	// --- PROPFIND: addressbook collection ---------------------------------

	collectionPath := homePath + abPath + "/"
	status, _, body = davCall(t, "PROPFIND", collectionPath, basicUser, appPassword, propfindAddressBookBody, depthHeader("0"))
	require.Equal(t, http.StatusMultiStatus, status, "PROPFIND addressbook: %s", string(body))
	assert.Contains(t, string(body), ">Contacts</displayname>")

	// --- PUT: create a contact via CardDAV --------------------------------

	// Use a UID without `@` — the emersion CardDAV library matches objects by
	// their URL path and an `@` in the slug seems to confuse the lookup.
	contactUID := "carddav-integration-" + time.Now().Format("20060102T150405")
	contactPath := collectionPath + contactUID + ".vcf"
	vcardBody := buildMinimalVCard(contactUID, "Charlie DavTest", "Charlie", "DavTest")

	status, hdrs, body := davCall(t, "PUT", contactPath, basicUser, appPassword, vcardBody, map[string]string{
		"Content-Type": "text/vcard; charset=utf-8",
	})
	require.Contains(t, []int{http.StatusCreated, http.StatusNoContent}, status, "PUT contact: %s", string(body))
	assert.NotEmpty(t, hdrs.Get("ETag"), "PUT should return an ETag")

	// --- GET: fetch contact via CardDAV -----------------------------------

	status, _, body = davCall(t, "GET", contactPath, basicUser, appPassword, "", nil)
	require.Equal(t, http.StatusOK, status, "GET contact: %s", string(body))
	assert.Contains(t, string(body), "UID:"+contactUID)
	assert.Contains(t, string(body), "FN:Charlie DavTest")

	// --- REST cross-check: contact shows up in the list -------------------

	var listResp struct {
		Contacts []struct {
			UID           string `json:"uid"`
			FormattedName string `json:"formatted_name"`
		} `json:"Contacts"`
	}
	code = doJSONRaw(t, http.MethodGet, "/addressbooks/"+uintStr(abID)+"/contacts?limit=100", token, nil, &listResp)
	require.Equal(t, http.StatusOK, code)
	found := false
	for _, c := range listResp.Contacts {
		if c.UID == contactUID {
			found = true
			assert.Equal(t, "Charlie DavTest", c.FormattedName)
			break
		}
	}
	assert.True(t, found, "contact PUT via CardDAV should show in the REST list")

	// --- sync-collection REPORT -------------------------------------------

	status, _, body = davCall(t, "REPORT", collectionPath, basicUser, appPassword, syncCollectionBody, depthHeader("1"))
	require.Equal(t, http.StatusMultiStatus, status, "sync REPORT: %s", string(body))
	assert.Contains(t, string(body), contactUID)
	assert.Contains(t, string(body), "<sync-token>")

	// --- PUT (update) -----------------------------------------------------

	updated := buildMinimalVCard(contactUID, "Charlie DavTest (updated)", "Charlie", "DavTest")
	status, _, body = davCall(t, "PUT", contactPath, basicUser, appPassword, updated, map[string]string{
		"Content-Type": "text/vcard; charset=utf-8",
	})
	require.Contains(t, []int{http.StatusCreated, http.StatusNoContent, http.StatusOK}, status, "PUT update: %s", string(body))

	status, _, body = davCall(t, "GET", contactPath, basicUser, appPassword, "", nil)
	require.Equal(t, http.StatusOK, status)
	assert.Contains(t, string(body), "Charlie DavTest (updated)")

	// --- DELETE -----------------------------------------------------------

	status, _, body = davCall(t, "DELETE", contactPath, basicUser, appPassword, "", nil)
	require.Contains(t, []int{http.StatusOK, http.StatusNoContent}, status, "DELETE contact: %s", string(body))

	status, _, _ = davCall(t, "GET", contactPath, basicUser, appPassword, "", nil)
	assert.Equal(t, http.StatusNotFound, status, "deleted contact must 404")
}

const propfindAddressBookHomeBody = `<?xml version="1.0" encoding="utf-8"?>
<D:propfind xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:carddav">
  <D:prop>
    <D:resourcetype/>
    <D:displayname/>
    <C:addressbook-description/>
  </D:prop>
</D:propfind>`

const propfindAddressBookBody = `<?xml version="1.0" encoding="utf-8"?>
<D:propfind xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:carddav">
  <D:prop>
    <D:resourcetype/>
    <D:displayname/>
    <D:getetag/>
    <C:supported-address-data/>
  </D:prop>
</D:propfind>`

func buildMinimalVCard(uid, fn, given, family string) string {
	b := &strings.Builder{}
	fmt.Fprint(b, "BEGIN:VCARD\r\n")
	fmt.Fprint(b, "VERSION:3.0\r\n")
	fmt.Fprintf(b, "UID:%s\r\n", uid)
	fmt.Fprintf(b, "FN:%s\r\n", fn)
	fmt.Fprintf(b, "N:%s;%s;;;\r\n", family, given)
	fmt.Fprint(b, "END:VCARD\r\n")
	return b.String()
}
