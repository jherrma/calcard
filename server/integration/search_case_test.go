//go:build integration

package integration_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSearchIgnoresCase is the end-to-end regression test for F26. Searches
// used a plain LIKE, whose case folding is the driver's: SQLite folds ASCII
// only, PostgreSQL nothing at all. So on PostgreSQL "mcpcase" missed
// "McpCase", and on both drivers "ärztin" missed "Ärztin" — the CardDAV
// text-match, which already lower-cased both sides, missed it on SQLite even
// for "Ärztin" itself, because SQLite's built-in lower() leaves "Ä" alone.
//
// Every surface is checked with differently cased ASCII and non-ASCII queries:
// REST /contacts/search, both groups of /search, and a CardDAV
// addressbook-query (RFC 6352 §10.5.4: i;unicode-casemap is the default
// collation). Run it against PostgreSQL too (see integration/CLAUDE.md).
func TestSearchIgnoresCase(t *testing.T) {
	email := "search-case@example.test"
	token, username := registerAndLoginFull(t, email, "searchCase!123", "Search Case User")
	_, calUUID := createCalendar(t, token, "Case Calendar", "#123456")
	_, abUUID := createAddressBook(t, token, "Case Contacts")

	createContactIn(t, token, abUUID, "Ärztin McpCase")
	start := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Hour)
	status, raw := restCall(t, http.MethodPost, "/calendars/"+calUUID+"/events/", token, map[string]any{
		"summary": "Übergabe QuarterlyCase", "location": "Raum Köln",
		"start": start.Format(time.RFC3339), "end": start.Add(time.Hour).Format(time.RFC3339),
		"timezone": "UTC",
	})
	require.Equalf(t, http.StatusCreated, status, "seed event: %s", errorMessage(raw))

	t.Run("contacts search", func(t *testing.T) {
		for _, q := range []string{"McpCase", "mcpcase", "MCPCASE", "Ärztin", "ärztin", "ÄRZTIN"} {
			var body struct {
				Contacts []struct {
					FormattedName string `json:"formatted_name"`
				} `json:"contacts"`
			}
			code := doJSONRaw(t, http.MethodGet, "/contacts/search?q="+url.QueryEscape(q), token, nil, &body)
			require.Equal(t, http.StatusOK, code)
			if assert.Lenf(t, body.Contacts, 1, "/contacts/search?q=%s", q) {
				assert.Equal(t, "Ärztin McpCase", body.Contacts[0].FormattedName)
			}

			code, resp := search(t, token, "types=contacts&q="+url.QueryEscape(q))
			require.Equal(t, http.StatusOK, code)
			assert.Lenf(t, resp.Contacts.Items, 1, "/search contacts q=%s", q)
		}
	})

	t.Run("event search", func(t *testing.T) {
		for _, q := range []string{"QuarterlyCase", "quarterlycase", "Übergabe", "übergabe", "ÜBERGABE", "raum köln"} {
			code, resp := search(t, token, "types=events&q="+url.QueryEscape(q))
			require.Equal(t, http.StatusOK, code)
			assert.Equalf(t, []string{"Übergabe QuarterlyCase"}, eventTitles(resp), "/search events q=%s", q)
		}
	})

	t.Run("wildcards still match literally", func(t *testing.T) {
		for _, q := range []string{"Mc_Case", "%%"} {
			code, resp := search(t, token, "types=events,contacts&q="+url.QueryEscape(q))
			require.Equal(t, http.StatusOK, code)
			assert.Emptyf(t, resp.Contacts.Items, "contacts q=%s", q)
			assert.Emptyf(t, resp.Events.Items, "events q=%s", q)
		}
	})

	t.Run("CardDAV text-match", func(t *testing.T) {
		_, appPass := createAppPassword(t, token, "search-case")
		var books struct {
			AddressBooks []struct {
				UUID string `json:"UUID"`
				Path string `json:"Path"`
			} `json:"addressbooks"`
		}
		_, raw := restCall(t, http.MethodGet, "/addressbooks/", token, nil)
		require.NoError(t, json.Unmarshal(raw, &books))
		var abPath string
		for _, ab := range books.AddressBooks {
			if ab.UUID == abUUID {
				abPath = ab.Path
			}
		}
		require.NotEmpty(t, abPath)
		collection := "/dav/" + username + "/addressbooks/" + abPath + "/"
		putVCard(t, collection, email, appPass, "case-dav", "Ärztin DavCase", "Ärztin", "DavCase")

		for _, q := range []string{"DavCase", "davcase", "Ärztin", "ärztin"} {
			query := `<?xml version="1.0" encoding="utf-8"?>
<C:addressbook-query xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:carddav">
  <D:prop><D:getetag/></D:prop>
  <C:filter><C:prop-filter name="FN">
    <C:text-match collation="i;unicode-casemap" match-type="contains">` + q + `</C:text-match>
  </C:prop-filter></C:filter>
</C:addressbook-query>`
			status, _, body := davCall(t, "REPORT", collection, email, appPass, query, depthHeader("1"))
			require.Equalf(t, http.StatusMultiStatus, status, "REPORT: %s", body)
			assert.Containsf(t, string(body), "case-dav.vcf", "text-match %q must find FN 'Ärztin DavCase'", q)
		}
	})
}
