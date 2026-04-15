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

// TestCalDAVTimeRangeReport issues a CalDAV `calendar-query` REPORT that
// filters VEVENTs by a time range. This is the report real clients (Apple
// Calendar, Thunderbird/Lightning, DAVx5) lean on when fetching the visible
// window of a calendar — not `sync-collection` — so it matters that it
// actually honours the filter.
func TestCalDAVTimeRangeReport(t *testing.T) {
	email := "dav-query@example.test"
	password := "querySecret!123"
	token, username := registerAndLoginFull(t, email, password, "Query User")
	_, appPass := createAppPassword(t, token, "query-test")

	// Find the default "Personal" calendar's URL path.
	idx := listCalendarsIndex(t, token)
	personal, ok := idx["Personal"]
	require.True(t, ok)
	calPath := personal.UUID + ".ics"
	collection := "/dav/" + username + "/calendars/" + calPath + "/"

	// Seed three events: two inside the query window, one outside.
	inEarly := time.Date(2031, 5, 10, 9, 0, 0, 0, time.UTC)
	inLate := time.Date(2031, 5, 15, 14, 0, 0, 0, time.UTC)
	outOfRange := time.Date(2032, 1, 1, 9, 0, 0, 0, time.UTC)
	putEvent(t, collection, email, appPass, "query-inside-1", "Inside A", inEarly)
	putEvent(t, collection, email, appPass, "query-inside-2", "Inside B", inLate)
	putEvent(t, collection, email, appPass, "query-outside", "Outside", outOfRange)

	// Time-range REPORT: only May 2031.
	reportBody := `<?xml version="1.0" encoding="utf-8"?>
<C:calendar-query xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">
  <D:prop>
    <D:getetag/>
    <C:calendar-data/>
  </D:prop>
  <C:filter>
    <C:comp-filter name="VCALENDAR">
      <C:comp-filter name="VEVENT">
        <C:time-range start="20310501T000000Z" end="20310601T000000Z"/>
      </C:comp-filter>
    </C:comp-filter>
  </C:filter>
</C:calendar-query>`

	status, _, body := davCall(t, "REPORT", collection, email, appPass, reportBody, depthHeader("1"))
	require.Equalf(t, http.StatusMultiStatus, status, "calendar-query REPORT: %s", string(body))
	s := string(body)
	assert.Contains(t, s, "query-inside-1", "in-range events must appear")
	assert.Contains(t, s, "query-inside-2")
	assert.NotContains(t, s, "query-outside",
		"event outside the time-range filter must NOT appear in the REPORT response")
}

// TestCalDAVSyncTokenProgression proves that a second `sync-collection`
// REPORT, using the token returned by the first, returns only the changes
// made since then — which is the whole point of WebDAV-Sync. Real clients
// rely on this to avoid re-fetching the full collection on every refresh.
func TestCalDAVSyncTokenProgression(t *testing.T) {
	email := "dav-sync@example.test"
	password := "syncSecret!123"
	token, username := registerAndLoginFull(t, email, password, "Sync User")
	_, appPass := createAppPassword(t, token, "sync-test")

	idx := listCalendarsIndex(t, token)
	personal, ok := idx["Personal"]
	require.True(t, ok)
	collection := "/dav/" + username + "/calendars/" + personal.UUID + ".ics/"

	// Seed one event, do an initial sync, capture the sync-token.
	putEvent(t, collection, email, appPass, "sync-seed-1", "First", time.Date(2031, 7, 1, 9, 0, 0, 0, time.UTC))

	status, _, body := davCall(t, "REPORT", collection, email, appPass, syncCollectionBody, depthHeader("1"))
	require.Equal(t, http.StatusMultiStatus, status)
	firstToken := extractSyncToken(string(body))
	require.NotEmpty(t, firstToken, "initial sync must return a sync-token")
	require.Contains(t, string(body), "sync-seed-1")

	// Add a second event.
	putEvent(t, collection, email, appPass, "sync-new-1", "Second", time.Date(2031, 7, 2, 9, 0, 0, 0, time.UTC))

	// Second sync with the previous token: must include the new event but
	// not the old one (that was already delivered in the first response).
	incrementalBody := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<D:sync-collection xmlns:D="DAV:">
  <D:sync-token>%s</D:sync-token>
  <D:sync-level>1</D:sync-level>
  <D:prop>
    <D:getetag/>
  </D:prop>
</D:sync-collection>`, firstToken)

	status, _, body = davCall(t, "REPORT", collection, email, appPass, incrementalBody, depthHeader("1"))
	require.Equalf(t, http.StatusMultiStatus, status, "delta sync: %s", string(body))
	s := string(body)
	assert.Contains(t, s, "sync-new-1", "delta response must carry the new event")
	assert.NotContainsf(t, s, "sync-seed-1",
		"delta response must NOT re-deliver events that were already in the prior sync window; body was: %s", s)

	secondToken := extractSyncToken(s)
	require.NotEmpty(t, secondToken)
	assert.NotEqual(t, firstToken, secondToken, "sync-token should advance after changes")
}

// TestCalDAVEtagPreconditions covers If-Match. A client doing a safe update
// reads the current ETag, then PUTs with `If-Match: <etag>` — if the server
// mutated the object in between, the stale If-Match should produce a 412 so
// the client can re-read and merge instead of silently clobbering.
func TestCalDAVEtagPreconditions(t *testing.T) {
	email := "dav-etag@example.test"
	password := "etagSecret!123"
	token, username := registerAndLoginFull(t, email, password, "Etag User")
	_, appPass := createAppPassword(t, token, "etag-test")

	idx := listCalendarsIndex(t, token)
	personal, ok := idx["Personal"]
	require.True(t, ok)
	collection := "/dav/" + username + "/calendars/" + personal.UUID + ".ics/"

	// Initial PUT — capture the first ETag.
	eventPath := collection + "etag-event.ics"
	ical := buildMinimalVEvent("etag-event", "ETag v1", time.Date(2031, 9, 1, 9, 0, 0, 0, time.UTC))
	status, hdrs, body := davCall(t, "PUT", eventPath, email, appPass, ical, map[string]string{
		"Content-Type": "text/calendar; charset=utf-8",
	})
	require.Contains(t, []int{http.StatusCreated, http.StatusNoContent}, status, "initial PUT: %s", string(body))
	initialETag := hdrs.Get("ETag")
	require.NotEmpty(t, initialETag, "initial PUT must return an ETag")

	// Valid If-Match (current ETag) should succeed.
	updated := buildMinimalVEvent("etag-event", "ETag v2", time.Date(2031, 9, 1, 9, 0, 0, 0, time.UTC))
	status, hdrs, body = davCall(t, "PUT", eventPath, email, appPass, updated, map[string]string{
		"Content-Type": "text/calendar; charset=utf-8",
		"If-Match":     initialETag,
	})
	require.Contains(t, []int{http.StatusOK, http.StatusCreated, http.StatusNoContent}, status,
		"PUT with matching If-Match must succeed: %s", string(body))
	newETag := hdrs.Get("ETag")
	if newETag != "" {
		assert.NotEqual(t, initialETag, newETag, "successful update should bump the ETag")
	}

	// Stale If-Match (the original ETag) must now be rejected with 412.
	stale := buildMinimalVEvent("etag-event", "ETag v3 — stale write", time.Date(2031, 9, 1, 9, 0, 0, 0, time.UTC))
	status, _, body = davCall(t, "PUT", eventPath, email, appPass, stale, map[string]string{
		"Content-Type": "text/calendar; charset=utf-8",
		"If-Match":     initialETag,
	})
	assert.Equalf(t, http.StatusPreconditionFailed, status,
		"stale If-Match must yield 412 Precondition Failed, got: %s", string(body))

	// The event body must still be v2, not silently overwritten with v3.
	status, _, body = davCall(t, "GET", eventPath, email, appPass, "", nil)
	require.Equal(t, http.StatusOK, status)
	assert.Contains(t, string(body), "ETag v2")
	assert.NotContains(t, string(body), "stale write")
}

// --- local helpers ---------------------------------------------------------

// putEvent is a small wrapper around buildMinimalVEvent + davCall PUT that
// makes the test flow read cleanly.
func putEvent(t *testing.T, collection, basicUser, appPass, uid, summary string, start time.Time) {
	t.Helper()
	path := collection + uid + ".ics"
	ical := buildMinimalVEvent(uid, summary, start)
	status, _, body := davCall(t, "PUT", path, basicUser, appPass, ical, map[string]string{
		"Content-Type": "text/calendar; charset=utf-8",
	})
	require.Containsf(t, []int{http.StatusCreated, http.StatusNoContent, http.StatusOK}, status,
		"PUT %s: %s", path, string(body))
}

// extractSyncToken pulls the DAV:sync-token value out of a multistatus response
// body. The XML shape we get back from emersion/go-webdav looks like
// `<sync-token xmlns="DAV:">...</sync-token>` — we use simple string slicing
// rather than a full XML decoder to keep the test dependency-free.
func extractSyncToken(body string) string {
	const openTag = "<sync-token"
	i := strings.Index(body, openTag)
	if i == -1 {
		return ""
	}
	// find the first '>' that closes the opening tag
	gt := strings.Index(body[i:], ">")
	if gt == -1 {
		return ""
	}
	rest := body[i+gt+1:]
	endIdx := strings.Index(rest, "</sync-token>")
	if endIdx == -1 {
		return ""
	}
	return strings.TrimSpace(rest[:endIdx])
}
