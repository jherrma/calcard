//go:build integration

package integration_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCardDAVSyncTokenProgression is the CardDAV analogue of
// TestCalDAVSyncTokenProgression: a second sync-collection REPORT with the
// token from the first returns only the delta (the newly-added contact)
// rather than re-delivering everything. Real CardDAV clients (Apple
// Contacts, DAVx5) rely on this for incremental sync.
func TestCardDAVSyncTokenProgression(t *testing.T) {
	email := "carddav-sync@example.test"
	password := "syncSecret!123"
	token, username := registerAndLoginFull(t, email, password, "CardDAV Sync User")
	_, appPass := createAppPassword(t, token, "carddav-sync-test")

	// Find the default Contacts addressbook's URL-slug.
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
	collection := "/dav/" + username + "/addressbooks/" + abPath + "/"

	// Seed one contact, then do an initial sync-collection REPORT.
	putVCard(t, collection, email, appPass, "carddav-sync-1", "First Contact", "First", "Seed")

	status, _, body := davCall(t, "REPORT", collection, email, appPass, syncCollectionBody, depthHeader("1"))
	require.Equal(t, http.StatusMultiStatus, status)
	firstToken := extractSyncToken(string(body))
	require.NotEmpty(t, firstToken, "initial sync must return a sync-token")
	require.Contains(t, string(body), "carddav-sync-1", "initial sync must include the seeded contact")

	// Add a second contact.
	putVCard(t, collection, email, appPass, "carddav-sync-2", "Second Contact", "Second", "Delta")

	// Incremental sync with the previous token — must carry the new contact
	// and NOT re-deliver the previously-synced one.
	incremental := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<D:sync-collection xmlns:D="DAV:">
  <D:sync-token>%s</D:sync-token>
  <D:sync-level>1</D:sync-level>
  <D:prop>
    <D:getetag/>
  </D:prop>
</D:sync-collection>`, firstToken)

	status, _, body = davCall(t, "REPORT", collection, email, appPass, incremental, depthHeader("1"))
	require.Equalf(t, http.StatusMultiStatus, status, "delta sync: %s", string(body))
	s := string(body)
	assert.Contains(t, s, "carddav-sync-2", "delta sync must carry the new contact")
	assert.NotContainsf(t, s, "carddav-sync-1",
		"delta sync must NOT re-deliver the contact that was already reported; body was: %s", s)

	secondToken := extractSyncToken(s)
	assert.NotEmpty(t, secondToken)
	assert.NotEqual(t, firstToken, secondToken, "sync-token should advance after changes")
}

// TestCardDAVAddressBookQueryReport covers the addressbook-query REPORT.
// Clients use it for search-style filtering ("contacts with email matching
// 'company.com'"). The server must honor the filter and not just return
// every contact like it did before the calendar-query fix.
func TestCardDAVAddressBookQueryReport(t *testing.T) {
	email := "carddav-query@example.test"
	password := "querySecret!123"
	token, username := registerAndLoginFull(t, email, password, "CardDAV Query User")
	_, appPass := createAppPassword(t, token, "carddav-query-test")

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
	collection := "/dav/" + username + "/addressbooks/" + abPath + "/"

	// Seed three contacts: two with family name "Testerson", one with
	// "Unrelated". The query should return only the two Testersons.
	putVCard(t, collection, email, appPass, "query-ts-1", "Alice Testerson", "Alice", "Testerson")
	putVCard(t, collection, email, appPass, "query-ts-2", "Bob Testerson", "Bob", "Testerson")
	putVCard(t, collection, email, appPass, "query-un", "Charlie Unrelated", "Charlie", "Unrelated")

	// addressbook-query with a FN text-match filter. The emersion library
	// parses this into an AddressBookQuery with PropFilter entries which
	// our backend funnels through QueryObjects.
	query := `<?xml version="1.0" encoding="utf-8"?>
<C:addressbook-query xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:carddav">
  <D:prop>
    <D:getetag/>
    <C:address-data/>
  </D:prop>
  <C:filter>
    <C:prop-filter name="FN">
      <C:text-match collation="i;unicode-casemap" match-type="contains">Testerson</C:text-match>
    </C:prop-filter>
  </C:filter>
</C:addressbook-query>`

	status, _, body := davCall(t, "REPORT", collection, email, appPass, query, depthHeader("1"))
	require.Equalf(t, http.StatusMultiStatus, status, "addressbook-query REPORT: %s", string(body))
	s := string(body)
	assert.Contains(t, s, "query-ts-1", "Testerson 1 must match")
	assert.Contains(t, s, "query-ts-2", "Testerson 2 must match")
	assert.NotContainsf(t, s, "query-un",
		"Unrelated contact must NOT appear when the FN filter is 'Testerson'")
}

// putVCard is a small CardDAV convenience wrapper — writes a minimal vCard
// at the given collection path and fails the test on non-2xx.
func putVCard(t *testing.T, collection, basicUser, appPass, uid, fn, given, family string) {
	t.Helper()
	path := collection + uid + ".vcf"
	vc := buildMinimalVCard(uid, fn, given, family)
	status, _, body := davCall(t, "PUT", path, basicUser, appPass, vc,
		map[string]string{"Content-Type": "text/vcard; charset=utf-8"})
	require.Containsf(t, []int{http.StatusCreated, http.StatusNoContent, http.StatusOK}, status,
		"PUT %s: %s", path, string(body))
	_ = time.Now
}
