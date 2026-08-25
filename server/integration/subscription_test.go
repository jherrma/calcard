//go:build integration

package integration_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCalendarSubscriptions drives story 100 end to end against the real
// server: a local publisher, the REST lifecycle, the read-only guarantee on
// every write surface, and the delete cascade.
//
// The feed is served by an httptest server on 127.0.0.1, which the SSRF guard
// blocks by default — main_test.go sets AllowPrivateHosts/AllowInsecureURLs for
// exactly this reason. TestCalendarSubscriptionRejectsANonFeed covers the
// refusal path that guard exists for.
func TestCalendarSubscriptions(t *testing.T) {
	token, username := registerAndLoginFull(t, "subscriber@example.com", "SubsPass123!", "Subscriber")
	feed := newTestFeed(t)
	feed.set(feedV1)

	var created subscriptionPayload
	code := doJSONRaw(t, http.MethodPost, "/calendar-subscriptions/", token, map[string]any{
		"url":              feed.URL,
		"refresh_interval": "1h",
	}, &created)
	require.Equal(t, http.StatusCreated, code)

	t.Run("create mirrors the feed into a new calendar", func(t *testing.T) {
		assert.NotEmpty(t, created.ID)
		assert.NotEmpty(t, created.CalendarID)
		// The feed's X-WR-CALNAME is the default name — the user named nothing.
		assert.Equal(t, "Local test feed", created.Name)
		assert.Equal(t, "synced", created.Status)
		assert.Equal(t, int64(2), created.EventCount)
		assert.Equal(t, "1h", created.RefreshInterval)
		assert.NotNil(t, created.LastSyncedAt)
		assert.Empty(t, created.LastError)
		assert.Equal(t, 1, feed.hitCount(), "the feed is fetched once, at create time")
	})

	t.Run("the calendar is listed as subscribed and carries the feed's events", func(t *testing.T) {
		var cals struct {
			Calendars []struct {
				UUID       string `json:"uuid"`
				Name       string `json:"name"`
				Subscribed bool   `json:"subscribed"`
				Path       string `json:"path"`
			} `json:"calendars"`
		}
		require.Equal(t, http.StatusOK, doJSONRaw(t, http.MethodGet, "/calendars/", token, nil, &cals))

		var found bool
		for _, c := range cals.Calendars {
			if c.UUID == created.CalendarID {
				found = true
				assert.True(t, c.Subscribed, "the client needs this flag to hide every edit affordance")
				assert.Equal(t, "Local test feed", c.Name)
			}
		}
		require.True(t, found, "the subscribed calendar must appear in GET /calendars")

		summaries := eventSummaries(t, token, created.CalendarID)
		assert.ElementsMatch(t, []string{"Feed event one", "Feed event two"}, summaries)
	})

	t.Run("the mirrored calendar refuses every write", func(t *testing.T) {
		// A subscribed calendar is read-only to its OWNER, not just to
		// sharees: the next refresh replaces its contents wholesale, so a
		// write accepted here would vanish without a trace.
		status, body := restCall(t, http.MethodPost, "/calendars/"+created.CalendarID+"/events", token, map[string]any{
			"summary": "Mine", "start": "2026-09-01T10:00:00Z", "end": "2026-09-01T11:00:00Z",
		})
		assert.Equal(t, http.StatusForbidden, status, "REST create: %s", errorMessage(body))

		events := listFeedEvents(t, token, created.CalendarID)
		require.NotEmpty(t, events)
		victim := events[0].ID

		status, body = restCall(t, http.MethodPatch, "/calendars/"+created.CalendarID+"/events/"+victim, token, map[string]any{"summary": "Edited"})
		assert.Equal(t, http.StatusForbidden, status, "REST update: %s", errorMessage(body))

		status, body = restCall(t, http.MethodDelete, "/calendars/"+created.CalendarID+"/events/"+victim, token, nil)
		assert.Equal(t, http.StatusForbidden, status, "REST delete: %s", errorMessage(body))

		// Import would report "N events imported" and then lose them all at
		// the next refresh — a lie with a delay on it.
		status, body = restCall(t, http.MethodPost, "/calendars/"+created.CalendarID+"/import", token, map[string]any{
			"data": "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//T//EN\r\nBEGIN:VEVENT\r\nUID:i@x\r\nDTSTAMP:20260101T000000Z\r\nDTSTART:20260901T100000Z\r\nSUMMARY:Imported\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n",
		})
		assert.GreaterOrEqual(t, status, 400, "import must be refused")
		assert.Less(t, status, 500, "and refused as a client error: %s", errorMessage(body))

		// Nothing above may have landed.
		assert.ElementsMatch(t, []string{"Feed event one", "Feed event two"}, eventSummaries(t, token, created.CalendarID))
	})

	t.Run("CalDAV sees a read-only collection with a source", func(t *testing.T) {
		davUser, davPass := createAppPassword(t, token, "subscription-dav")
		calPath := calendarPath(t, token, created.CalendarID)
		collection := davURL(davUser, "calendars", calPath) + "/"

		status, _, body := davCall(t, "PROPFIND", collection, "subscriber@example.com", davPass,
			`<?xml version="1.0" encoding="utf-8"?><propfind xmlns="DAV:"><prop>`+
				`<current-user-privilege-set/><source xmlns="http://calendarserver.org/ns/"/><resourcetype/>`+
				`</prop></propfind>`, depthHeader("0"))
		require.Equal(t, http.StatusMultiStatus, status, "PROPFIND: %s", string(body))

		xml := string(body)
		assert.Contains(t, xml, "<read", "the collection must advertise read")
		// REVERT PROOF: without the privilege cap the collection advertises
		// write, so a client offers a "new event" button whose every use fails.
		assert.NotContains(t, xml, "<write", "a subscription must not advertise write")
		assert.Contains(t, xml, feed.URL, "CS:source names the feed this mirrors")
		// The resourcetype deliberately stays a calendar: a client that does
		// not understand CS:subscribed would hide the events entirely.
		assert.Contains(t, xml, "calendar")

		// And a DAV PUT into it is refused.
		status, _, body = davCall(t, http.MethodPut, collection+"intruder.ics", "subscriber@example.com", davPass,
			"BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//T//EN\r\nBEGIN:VEVENT\r\nUID:dav@x\r\nDTSTAMP:20260101T000000Z\r\nDTSTART:20260901T100000Z\r\nSUMMARY:DAV\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n",
			map[string]string{"Content-Type": "text/calendar"})
		assert.Equal(t, http.StatusForbidden, status, "DAV PUT: %s", string(body))
	})

	t.Run("refresh applies the feed's changes", func(t *testing.T) {
		// v2 renames one event, drops another and adds a third.
		feed.set(feedV2)

		var refreshed struct {
			subscriptionPayload
			Synced  bool `json:"synced"`
			Created int  `json:"created"`
			Updated int  `json:"updated"`
			Deleted int  `json:"deleted"`
		}
		code := doJSONRaw(t, http.MethodPost, "/calendar-subscriptions/"+created.ID+"/refresh", token, nil, &refreshed)
		require.Equal(t, http.StatusOK, code)

		assert.True(t, refreshed.Synced)
		assert.Equal(t, 1, refreshed.Created)
		assert.Equal(t, 1, refreshed.Updated)
		assert.Equal(t, 1, refreshed.Deleted)
		assert.Equal(t, "synced", refreshed.Status)
		assert.Equal(t, int64(2), refreshed.EventCount)

		assert.ElementsMatch(t,
			[]string{"Feed event one (renamed)", "Feed event three"},
			eventSummaries(t, token, created.CalendarID))
	})

	t.Run("settings can be changed without touching the feed", func(t *testing.T) {
		var updated subscriptionPayload
		code := doJSONRaw(t, http.MethodPatch, "/calendar-subscriptions/"+created.ID, token, map[string]any{
			"name":             "Renamed by me",
			"color":            "#123456",
			"refresh_interval": "6h",
		}, &updated)
		require.Equal(t, http.StatusOK, code)

		assert.Equal(t, "Renamed by me", updated.Name)
		assert.Equal(t, "#123456", updated.Color)
		assert.Equal(t, "6h", updated.RefreshInterval)

		// Renaming the collection is an ownership decision and stays allowed,
		// even though writing INTO it is not.
		var cal struct {
			Name       string `json:"name"`
			Subscribed bool   `json:"subscribed"`
		}
		require.Equal(t, http.StatusOK, doJSONRaw(t, http.MethodGet, "/calendars/"+created.CalendarID, token, nil, &cal))
		assert.Equal(t, "Renamed by me", cal.Name)
		assert.True(t, cal.Subscribed)
	})

	t.Run("an invalid refresh interval is refused", func(t *testing.T) {
		status, body := restCall(t, http.MethodPatch, "/calendar-subscriptions/"+created.ID, token, map[string]any{
			"refresh_interval": "1m",
		})
		assert.Equal(t, http.StatusBadRequest, status, "%s", errorMessage(body))
		assert.Contains(t, strings.ToLower(errorMessage(body)), "refresh interval")
	})

	t.Run("a failing feed is reported on the subscription, not as a server error", func(t *testing.T) {
		feed.fail(http.StatusServiceUnavailable)

		var failed subscriptionPayload
		code := doJSONRaw(t, http.MethodPost, "/calendar-subscriptions/"+created.ID+"/refresh", token, nil, &failed)
		// The request succeeded; the third party is what failed, and the client
		// needs the updated state to render it.
		require.Equal(t, http.StatusOK, code)
		assert.Equal(t, "error", failed.Status)
		assert.Equal(t, 1, failed.ErrorCount)
		assert.Contains(t, failed.LastError, "503")

		// The events already mirrored survive a failed refresh.
		assert.Len(t, eventSummaries(t, token, created.CalendarID), 2)

		var got subscriptionPayload
		require.Equal(t, http.StatusOK, doJSONRaw(t, http.MethodGet, "/calendar-subscriptions/"+created.ID, token, nil, &got))
		assert.Equal(t, "error", got.Status)
	})

	t.Run("another account cannot see or touch it", func(t *testing.T) {
		otherToken := registerAndLogin(t, "not-the-subscriber@example.com", "OtherPass123!", "Other")

		var list struct {
			Subscriptions []subscriptionPayload `json:"subscriptions"`
		}
		require.Equal(t, http.StatusOK, doJSONRaw(t, http.MethodGet, "/calendar-subscriptions/", otherToken, nil, &list))
		assert.Empty(t, list.Subscriptions)

		// 404 rather than 403 everywhere, so subscription ids cannot be probed.
		for _, call := range []struct{ method, path string }{
			{http.MethodGet, "/calendar-subscriptions/" + created.ID},
			{http.MethodPatch, "/calendar-subscriptions/" + created.ID},
			{http.MethodDelete, "/calendar-subscriptions/" + created.ID},
			{http.MethodPost, "/calendar-subscriptions/" + created.ID + "/refresh"},
		} {
			status, _ := restCall(t, call.method, call.path, otherToken, map[string]any{})
			assert.Equal(t, http.StatusNotFound, status, "%s %s", call.method, call.path)
		}
	})

	t.Run("delete removes the subscription and its calendar", func(t *testing.T) {
		status, body := restCall(t, http.MethodDelete, "/calendar-subscriptions/"+created.ID, token, nil)
		require.Equal(t, http.StatusNoContent, status, "%s", errorMessage(body))

		var cals struct {
			Calendars []struct {
				UUID string `json:"uuid"`
			} `json:"calendars"`
		}
		require.Equal(t, http.StatusOK, doJSONRaw(t, http.MethodGet, "/calendars/", token, nil, &cals))
		for _, c := range cals.Calendars {
			assert.NotEqual(t, created.CalendarID, c.UUID, "the mirrored calendar must go with the subscription")
		}

		status, _ = restCall(t, http.MethodGet, "/calendar-subscriptions/"+created.ID, token, nil)
		assert.Equal(t, http.StatusNotFound, status)
	})

	_ = username
}

// TestCalendarSubscriptionRejectsBadURLs covers the create-time validation the
// story asks for: the feed is fetched and checked BEFORE anything is stored, so
// a mistake is reported at the moment it is made.
func TestCalendarSubscriptionRejectsBadURLs(t *testing.T) {
	token := registerAndLogin(t, "bad-feeds@example.com", "BadFeeds123!", "Bad Feeds")

	landing := newTestFeed(t)
	landing.set("<!DOCTYPE html><html><body>Subscribe to our calendar!</body></html>")

	cases := []struct {
		name     string
		url      string
		contains string
	}{
		{"a landing page rather than a feed", landing.URL, "iCalendar"},
		{"an unsupported scheme", "ftp://example.com/feed.ics", "scheme"},
		{"credentials embedded in the URL", "https://user:secret@example.com/feed.ics", "credentials"},
		{"no host", "https:///feed.ics", "host"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := restCall(t, http.MethodPost, "/calendar-subscriptions/", token, map[string]any{"url": tc.url})
			require.Equal(t, http.StatusBadRequest, status, "%s", string(body))
			assert.Contains(t, errorMessage(body), tc.contains)
			assert.NotContains(t, errorMessage(body), "secret", "a URL's secrets must never be echoed back")
		})
	}

	// Nothing may have been created by any of the failures above.
	var list struct {
		Subscriptions []subscriptionPayload `json:"subscriptions"`
	}
	require.Equal(t, http.StatusOK, doJSONRaw(t, http.MethodGet, "/calendar-subscriptions/", token, nil, &list))
	assert.Empty(t, list.Subscriptions)
}

// --- helpers ---------------------------------------------------------------

type subscriptionPayload struct {
	ID              string  `json:"id"`
	CalendarID      string  `json:"calendar_id"`
	Name            string  `json:"name"`
	Color           string  `json:"color"`
	URL             string  `json:"url"`
	RefreshInterval string  `json:"refresh_interval"`
	Status          string  `json:"status"`
	Enabled         bool    `json:"enabled"`
	LastSyncedAt    *string `json:"last_synced_at"`
	NextSyncAt      *string `json:"next_sync_at"`
	LastError       string  `json:"last_error"`
	ErrorCount      int     `json:"error_count"`
	EventCount      int64   `json:"event_count"`
}

type eventPayload struct {
	ID      string `json:"id"`
	Summary string `json:"summary"`
}

// testFeed is a publisher under the test's control.
type testFeed struct {
	*httptest.Server
	mu   sync.Mutex
	body string
	code int
	hits int
}

func newTestFeed(t *testing.T) *testFeed {
	t.Helper()
	f := &testFeed{code: http.StatusOK}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.hits++
		if f.code != http.StatusOK {
			w.WriteHeader(f.code)
			return
		}
		// Deliberately no Content-Type: a real publisher was observed serving
		// .ics with none at all, and the server sniffs the body instead.
		_, _ = w.Write([]byte(f.body))
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *testFeed) set(body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.body, f.code = body, http.StatusOK
}

func (f *testFeed) fail(code int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.code = code
}

func (f *testFeed) hitCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hits
}

func icsEvent(uid, summary, start string) string {
	return fmt.Sprintf("BEGIN:VEVENT\r\nUID:%s\r\nDTSTAMP:20260101T000000Z\r\nDTSTART:%s\r\nDTEND:%s\r\nSUMMARY:%s\r\nEND:VEVENT\r\n",
		uid, start, start, summary)
}

var feedV1 = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//CalCard Test//EN\r\nX-WR-CALNAME:Local test feed\r\n" +
	icsEvent("one@feed.test", "Feed event one", "20260301T100000Z") +
	icsEvent("two@feed.test", "Feed event two", "20260302T100000Z") +
	"END:VCALENDAR\r\n"

var feedV2 = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//CalCard Test//EN\r\nX-WR-CALNAME:Local test feed\r\n" +
	icsEvent("one@feed.test", "Feed event one (renamed)", "20260301T100000Z") +
	icsEvent("three@feed.test", "Feed event three", "20260303T100000Z") +
	"END:VCALENDAR\r\n"

func listFeedEvents(t *testing.T, token, calUUID string) []eventPayload {
	t.Helper()
	var resp struct {
		Events []eventPayload `json:"events"`
	}
	code := doJSONRaw(t, http.MethodGet,
		"/calendars/"+calUUID+"/events?start=2020-01-01T00:00:00Z&end=2030-01-01T00:00:00Z", token, nil, &resp)
	require.Equal(t, http.StatusOK, code, "list events")
	return resp.Events
}

func eventSummaries(t *testing.T, token, calUUID string) []string {
	t.Helper()
	var out []string
	for _, e := range listFeedEvents(t, token, calUUID) {
		out = append(out, e.Summary)
	}
	return out
}

// calendarPath returns a calendar's DAV path segment.
func calendarPath(t *testing.T, token, calUUID string) string {
	t.Helper()
	var cal struct {
		Path string `json:"path"`
	}
	require.Equal(t, http.StatusOK, doJSONRaw(t, http.MethodGet, "/calendars/"+calUUID, token, nil, &cal))
	require.NotEmpty(t, cal.Path)
	return cal.Path
}
