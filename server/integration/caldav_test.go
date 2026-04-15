//go:build integration

package integration_test

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCalDAV exercises the CalDAV protocol end-to-end over a real TCP socket:
// PROPFIND discovery (principal → home → collection), PUT/GET/DELETE of an
// event, cross-verification through the REST API, and one sync-collection
// REPORT. We authenticate with an app password created via the REST API so
// the Basic-Auth path through webdav/handler.go is exercised too.
func TestCalDAV(t *testing.T) {
	email := "caldav@example.test"
	password := "caldavSecret!123"
	token, username := registerAndLoginFull(t, email, password, "CalDAV User")

	// The DAV Basic-Auth handler (adapter/webdav/handler.go) looks users up
	// by email only — not by username — so the Basic-Auth principal is the
	// email, while the URL path segment is the opaque username.
	_, appPassword := createAppPassword(t, token, "caldav-test")
	basicUser := email

	// Use the default "Personal" calendar that register creates for every
	// user. GET /calendars lists both id and path.
	idx := listCalendarsIndex(t, token)
	personal, ok := idx["Personal"]
	require.True(t, ok, "default Personal calendar should exist")
	// calPath is the URL slug component — this is what the DAV backend uses
	// to look up the calendar, and it is `{uuid}.ics` for calendars created
	// by the server.
	calPath := personal.UUID + ".ics"

	// --- PROPFIND: principal resource -------------------------------------

	status, _, body := davCall(t, "PROPFIND", "/dav/", basicUser, appPassword, propfindPrincipalBody, depthHeader("0"))
	require.Equal(t, http.StatusMultiStatus, status, "PROPFIND /dav/: %s", string(body))
	require.Contains(t, string(body), username, "principal response should reference user")

	// --- PROPFIND: calendar home set --------------------------------------

	homePath := "/dav/" + username + "/calendars/"
	status, _, body = davCall(t, "PROPFIND", homePath, basicUser, appPassword, propfindCalendarHomeBody, depthHeader("1"))
	require.Equal(t, http.StatusMultiStatus, status, "PROPFIND home: %s", string(body))
	assert.Contains(t, string(body), calPath, "calendar home listing should include the Personal calendar path")

	// --- PROPFIND: calendar collection ------------------------------------

	collectionPath := homePath + calPath + "/"
	status, _, body = davCall(t, "PROPFIND", collectionPath, basicUser, appPassword, propfindCalendarBody, depthHeader("0"))
	require.Equal(t, http.StatusMultiStatus, status, "PROPFIND collection: %s", string(body))
	// The emersion/go-webdav library serializes elements with explicit xmlns
	// attributes, so match on the value between the tags rather than on the
	// exact bracketed tag form.
	assert.Contains(t, string(body), ">Personal</displayname>", "collection displayname should be in the response")

	// --- PUT: create an event via CalDAV ----------------------------------

	eventUID := "caldav-integration-" + time.Now().Format("20060102T150405") + "@calcard.test"
	eventPath := collectionPath + eventUID + ".ics"
	icalBody := buildMinimalVEvent(eventUID, "CalDAV Put Event", time.Date(2030, 7, 1, 9, 0, 0, 0, time.UTC))

	status, hdrs, body := davCall(t, "PUT", eventPath, basicUser, appPassword, icalBody, map[string]string{
		"Content-Type": "text/calendar; charset=utf-8",
	})
	require.Contains(t, []int{http.StatusCreated, http.StatusNoContent}, status, "PUT event: %s", string(body))
	assert.NotEmpty(t, hdrs.Get("ETag"), "PUT should return an ETag")

	// --- GET: fetch the just-created event via CalDAV ---------------------

	status, _, body = davCall(t, "GET", eventPath, basicUser, appPassword, "", nil)
	require.Equal(t, http.StatusOK, status, "GET event: %s", string(body))
	assert.Contains(t, string(body), "UID:"+eventUID)
	assert.Contains(t, string(body), "SUMMARY:CalDAV Put Event")

	// --- Cross-check: event appears in REST event list --------------------

	var listResp struct {
		Events []struct {
			UID string `json:"uid"`
		} `json:"events"`
	}
	rangeQS := "?start=2000-01-01T00:00:00Z&end=2099-12-31T23:59:59Z&expand=false"
	code := doJSONRaw(t, http.MethodGet, "/calendars/"+uintStr(personal.ID)+"/events/"+rangeQS, token, nil, &listResp)
	require.Equal(t, http.StatusOK, code)
	found := false
	for _, ev := range listResp.Events {
		if ev.UID == eventUID {
			found = true
			break
		}
	}
	assert.True(t, found, "event PUT via CalDAV should show up in REST /events list")

	// --- REPORT sync-collection ------------------------------------------

	status, _, body = davCall(t, "REPORT", collectionPath, basicUser, appPassword, syncCollectionBody, depthHeader("1"))
	require.Equal(t, http.StatusMultiStatus, status, "sync-collection REPORT: %s", string(body))
	assert.Contains(t, string(body), eventUID, "sync report should list the newly put event")
	assert.Contains(t, string(body), "<sync-token>", "sync report should include a sync-token")

	// --- PUT (update) -----------------------------------------------------

	updated := buildMinimalVEvent(eventUID, "CalDAV Put Event (updated)", time.Date(2030, 7, 1, 10, 0, 0, 0, time.UTC))
	status, _, body = davCall(t, "PUT", eventPath, basicUser, appPassword, updated, map[string]string{
		"Content-Type": "text/calendar; charset=utf-8",
	})
	require.Contains(t, []int{http.StatusCreated, http.StatusNoContent, http.StatusOK}, status, "PUT update: %s", string(body))

	status, _, body = davCall(t, "GET", eventPath, basicUser, appPassword, "", nil)
	require.Equal(t, http.StatusOK, status)
	assert.Contains(t, string(body), "CalDAV Put Event (updated)")

	// --- DELETE -----------------------------------------------------------

	status, _, body = davCall(t, "DELETE", eventPath, basicUser, appPassword, "", nil)
	require.Contains(t, []int{http.StatusOK, http.StatusNoContent}, status, "DELETE event: %s", string(body))

	status, _, _ = davCall(t, "GET", eventPath, basicUser, appPassword, "", nil)
	assert.Equal(t, http.StatusNotFound, status, "deleted event must 404")
}

// --- helpers ----------------------------------------------------------------

// createAppPassword creates an app password via the REST API and returns the
// DAV-usable (username, password) pair. The username returned is the same
// opaque slug that appears in DAV URL paths. Note: the DAV auth handler
// looks users up by *email*, so Basic Auth should use (email, password) even
// though this helper also returns the URL-path username.
func createAppPassword(t *testing.T, token, name string) (username, password string) {
	t.Helper()
	var resp struct {
		Password    string `json:"password"`
		Credentials struct {
			Username string `json:"username"`
			Password string `json:"password"`
		} `json:"credentials"`
	}
	code := doJSON(t, http.MethodPost, "/app-passwords/", token, map[string]any{
		"name":   name,
		"scopes": []string{"caldav", "carddav"},
	}, &resp)
	require.Equal(t, http.StatusOK, code, "create app password")
	require.NotEmpty(t, resp.Credentials.Username)
	require.NotEmpty(t, resp.Credentials.Password)
	return resp.Credentials.Username, resp.Credentials.Password
}

func depthHeader(d string) map[string]string {
	return map[string]string{"Depth": d}
}

const propfindPrincipalBody = `<?xml version="1.0" encoding="utf-8"?>
<D:propfind xmlns:D="DAV:">
  <D:prop>
    <D:current-user-principal/>
    <D:resourcetype/>
  </D:prop>
</D:propfind>`

const propfindCalendarHomeBody = `<?xml version="1.0" encoding="utf-8"?>
<D:propfind xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">
  <D:prop>
    <D:resourcetype/>
    <D:displayname/>
    <C:calendar-description/>
  </D:prop>
</D:propfind>`

const propfindCalendarBody = `<?xml version="1.0" encoding="utf-8"?>
<D:propfind xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">
  <D:prop>
    <D:resourcetype/>
    <D:displayname/>
    <D:getetag/>
    <C:supported-calendar-component-set/>
  </D:prop>
</D:propfind>`

const syncCollectionBody = `<?xml version="1.0" encoding="utf-8"?>
<D:sync-collection xmlns:D="DAV:">
  <D:sync-token/>
  <D:sync-level>1</D:sync-level>
  <D:prop>
    <D:getetag/>
    <D:getcontenttype/>
  </D:prop>
</D:sync-collection>`

// buildMinimalVEvent returns an iCalendar document with a single VEVENT.
// Suitable for CalDAV PUT — the server accepts it as-is.
func buildMinimalVEvent(uid, summary string, start time.Time) string {
	end := start.Add(time.Hour)
	stamp := time.Now().UTC()
	b := &strings.Builder{}
	fmt.Fprint(b, "BEGIN:VCALENDAR\r\n")
	fmt.Fprint(b, "VERSION:2.0\r\n")
	fmt.Fprint(b, "PRODID:-//CalCard Integration Tests//EN\r\n")
	fmt.Fprint(b, "BEGIN:VEVENT\r\n")
	fmt.Fprintf(b, "UID:%s\r\n", uid)
	fmt.Fprintf(b, "DTSTAMP:%s\r\n", stamp.Format("20060102T150405Z"))
	fmt.Fprintf(b, "DTSTART:%s\r\n", start.Format("20060102T150405Z"))
	fmt.Fprintf(b, "DTEND:%s\r\n", end.Format("20060102T150405Z"))
	fmt.Fprintf(b, "SUMMARY:%s\r\n", summary)
	fmt.Fprint(b, "END:VEVENT\r\n")
	fmt.Fprint(b, "END:VCALENDAR\r\n")
	return b.String()
}

// davXMLContains is a cheap lookup that unmarshals the response into a map-ish
// structure. Kept for potential future assertions — right now we just use
// string-level Contains checks, which are reliable enough for smoke-level
// discovery assertions and keep the test readable.
var _ = xml.Unmarshal
