//go:build integration

package integration_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// importHrefPattern pulls every <href> value out of a multistatus body.
var importHrefPattern = regexp.MustCompile(`<(?:[A-Za-z]+:)?href[^>]*>([^<]+)</`)

// TestImportObjectPaths is the regression test for F23: the REST import used
// "<uid>.ics" / "<uid>.vcf" as the DAV resource name, so a UID with '/' or ".."
// produced an object no DAV client could fetch (or an href outside the
// collection), and an event without DTSTAMP failed to import at all.
func TestImportObjectPaths(t *testing.T) {
	email := "import-paths@example.test"
	token, username := registerAndLoginFull(t, email, "importPaths!123", "Import Paths")
	_, appPassword := createAppPassword(t, token, "import-paths")

	t.Run("calendar", func(t *testing.T) {
		_, calUUID := createCalendar(t, token, "Import Paths", "#123456")
		collection := "/dav/" + username + "/calendars/" + calUUID + ".ics/"

		event := func(uid, stamp string) string {
			return "BEGIN:VEVENT\r\nUID:" + uid + "\r\n" + stamp +
				"DTSTART:20300301T090000Z\r\nDTEND:20300301T100000Z\r\nSUMMARY:F23\r\nEND:VEVENT\r\n"
		}
		wrap := func(events string) []byte {
			return []byte("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Test//EN\r\n" + events + "END:VCALENDAR\r\n")
		}
		uids := []string{"a/b@f23", "../up@f23", "with space?@f23", "plain-1@f23"}
		events := ""
		for _, uid := range uids {
			events += event(uid, "DTSTAMP:20260101T000000Z\r\n")
		}
		importCalendar(t, token, calUUID, wrap(events)) // fails on any per-event error

		// No DTSTAMP: many real exports omit it; the server must supply one.
		res := importCalendarCounts(t, token, calUUID, "skip", wrap(event("nostamp@f23", "")))
		assert.Equal(t, 1, res.Imported, "an event without DTSTAMP must import: %+v", res)
		uids = append(uids, "nostamp@f23")

		got := fetchImportedUIDs(t, collection, email, appPassword)
		assert.ElementsMatch(t, uids, got, "every imported event must be reachable over CalDAV inside its collection")

		res = importCalendarCounts(t, token, calUUID, "skip", wrap(events))
		assert.Equal(t, 0, res.Imported, "re-import must match by UID: %+v", res)
		assert.Equal(t, 4, res.Skipped, "re-import must match by UID: %+v", res)
	})

	t.Run("addressbook", func(t *testing.T) {
		abID, abUUID := createAddressBook(t, token, "Import Paths")
		var list struct {
			AddressBooks []struct {
				ID   uint   `json:"ID"`
				Path string `json:"Path"`
			} `json:"addressbooks"`
		}
		require.Equal(t, http.StatusOK, doJSONRaw(t, http.MethodGet, "/addressbooks/", token, nil, &list))
		abPath := ""
		for _, ab := range list.AddressBooks {
			if ab.ID == abID {
				abPath = ab.Path
			}
		}
		require.NotEmpty(t, abPath)
		collection := "/dav/" + username + "/addressbooks/" + abPath + "/"

		uids := []string{"c/1@f23", "../c2", "plain-c3@f23"}
		vcf := ""
		for i, uid := range uids {
			vcf += "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:" + uid + "\r\nFN:F23 Contact " + string(rune('A'+i)) +
				"\r\nN:Contact;F23;;;\r\nEND:VCARD\r\n"
		}
		importAddressBook(t, token, abUUID, []byte(vcf))

		got := fetchImportedUIDs(t, collection, email, appPassword)
		assert.ElementsMatch(t, uids, got, "every imported contact must be reachable over CardDAV inside its collection")
	})
}

type importCounts struct {
	Total, Imported, Skipped, Failed int
}

func importCalendarCounts(t *testing.T, token, calUUID, duplicateHandling string, ics []byte) importCounts {
	t.Helper()
	status, raw := rawCall(t, http.MethodPost,
		baseURL+"/api/v1/calendars/"+calUUID+"/import?duplicate_handling="+duplicateHandling,
		token, ics, map[string]string{"Content-Type": "text/calendar"})
	require.Equal(t, http.StatusOK, status, string(raw))
	var res importCounts
	require.NoError(t, json.Unmarshal(raw, &res))
	return res
}

// fetchImportedUIDs lists a DAV collection (Depth 1), GETs every member href,
// and returns the UID each one carries. A member href outside the collection
// or a failing GET fails the test.
func fetchImportedUIDs(t *testing.T, collection, user, pass string) []string {
	t.Helper()
	status, _, body := davCall(t, "PROPFIND", collection, user, pass,
		`<?xml version="1.0"?><D:propfind xmlns:D="DAV:"><D:prop><D:getetag/></D:prop></D:propfind>`, depthHeader("1"))
	require.Equal(t, http.StatusMultiStatus, status, string(body))

	var uids []string
	for _, m := range importHrefPattern.FindAllStringSubmatch(string(body), -1) {
		href, err := url.PathUnescape(m[1])
		require.NoError(t, err)
		if strings.TrimSuffix(href, "/") == strings.TrimSuffix(collection, "/") {
			continue
		}
		if !assert.Truef(t, strings.HasPrefix(href, collection), "href %q is outside %q", href, collection) {
			continue
		}
		status, _, obj := davCall(t, "GET", m[1], user, pass, "", nil)
		if !assert.Equalf(t, http.StatusOK, status, "GET %s", m[1]) {
			continue
		}
		for _, line := range strings.Split(string(obj), "\r\n") {
			if strings.HasPrefix(line, "UID:") {
				uids = append(uids, strings.TrimPrefix(line, "UID:"))
			}
		}
	}
	return uids
}
