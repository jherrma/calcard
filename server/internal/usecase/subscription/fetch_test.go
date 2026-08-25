package subscription

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sampleICS = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Test//EN\r\n" +
	"BEGIN:VEVENT\r\nUID:a@example.com\r\nDTSTAMP:20260101T000000Z\r\nDTSTART:20260301T100000Z\r\nSUMMARY:Hi\r\nEND:VEVENT\r\n" +
	"END:VCALENDAR\r\n"

// localFetcher allows private hosts, because every httptest server lives on
// 127.0.0.1 — exactly the address the guard exists to refuse.
func localFetcher(t *testing.T) *Fetcher {
	t.Helper()
	return NewFetcher(FetchOptions{
		MaxSize:           1 << 20,
		Timeout:           5 * time.Second,
		AllowInsecureURLs: true,
		AllowPrivateHosts: true,
	})
}

func TestNormalizeURLRewritesWebcal(t *testing.T) {
	// The scheme publishers advertise on "subscribe" links. A user pastes it
	// verbatim, so refusing it would make the common case fail.
	got, err := NormalizeURL("webcal://example.com/feed.ics", false)
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/feed.ics", got)

	got, err = NormalizeURL("WEBCALS://example.com/feed.ics", false)
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/feed.ics", got)
}

func TestNormalizeURLRejects(t *testing.T) {
	cases := map[string]string{
		"empty":              "   ",
		"plain http":         "http://example.com/feed.ics",
		"file scheme":        "file:///etc/passwd",
		"gopher scheme":      "gopher://example.com/",
		"no host":            "https:///feed.ics",
		"embedded password":  "https://user:secret@example.com/feed.ics",
		"embedded user only": "https://user@example.com/feed.ics",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := NormalizeURL(raw, false)
			assert.ErrorIs(t, err, ErrInvalidURL)
		})
	}
}

func TestNormalizeURLAllowsHTTPOnlyWhenConfigured(t *testing.T) {
	_, err := NormalizeURL("http://example.com/feed.ics", false)
	require.ErrorIs(t, err, ErrInvalidURL)

	got, err := NormalizeURL("http://example.com/feed.ics", true)
	require.NoError(t, err)
	assert.Equal(t, "http://example.com/feed.ics", got)
}

func TestFetchRefusesNonPublicAddresses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(sampleICS))
	}))
	defer srv.Close()

	// Same server, same URL — the ONLY difference is the guard.
	blocked := NewFetcher(FetchOptions{Timeout: 5 * time.Second, AllowInsecureURLs: true})
	_, err := blocked.Fetch(context.Background(), srv.URL, "", "")

	// REVERT PROOF: without the dial-time address check this succeeds, and
	// "subscribe to a calendar" becomes a way to make the server fetch any URL
	// on its own network and hand the response back through the calendar.
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidURL)
	assert.NotContains(t, err.Error(), srv.URL, "the refusal must not echo the URL back")
	// It must still SAY why. Unwrapping all the way to the bare sentinel would
	// leave the user staring at "invalid feed URL" with no idea what to change.
	assert.Contains(t, err.Error(), "not a publicly routable address")

	allowed := localFetcher(t)
	res, err := allowed.Fetch(context.Background(), srv.URL, "", "")
	require.NoError(t, err)
	assert.Contains(t, string(res.Body), "BEGIN:VCALENDAR")
}

func TestIsPublicIPClassification(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "0.0.0.0", "10.0.0.1", "172.16.5.4", "192.168.1.1",
		"169.254.169.254", // cloud instance metadata — the canonical SSRF target
		"100.64.0.1",      // CGNAT
		"192.0.0.1", "198.18.0.1", "240.0.0.1", "255.255.255.255",
		"224.0.0.1",                 // multicast
		"::1", "fc00::1", "fe80::1", // IPv6 loopback / ULA / link-local
		"::ffff:127.0.0.1", // IPv4-mapped loopback
		"64:ff9b::7f00:1",  // NAT64-wrapped loopback
	}
	for _, s := range blocked {
		assert.False(t, isPublicIP(mustIP(t, s)), "%s must be blocked", s)
	}

	allowed := []string{"1.1.1.1", "8.8.8.8", "93.184.216.34", "2606:4700::1111"}
	for _, s := range allowed {
		assert.True(t, isPublicIP(mustIP(t, s)), "%s must be allowed", s)
	}
}

func TestFetchSendsAndHonoursValidators(t *testing.T) {
	var gotETag, gotIMS string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotETag, gotIMS = r.Header.Get("If-None-Match"), r.Header.Get("If-Modified-Since")
		if gotETag == `W/"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `W/"v1"`)
		w.Header().Set("Last-Modified", "Mon, 24 Aug 2026 22:02:57 GMT")
		_, _ = w.Write([]byte(sampleICS))
	}))
	defer srv.Close()
	f := localFetcher(t)

	first, err := f.Fetch(context.Background(), srv.URL, "", "")
	require.NoError(t, err)
	assert.False(t, first.NotModified)
	assert.Equal(t, `W/"v1"`, first.ETag)
	assert.Equal(t, "Mon, 24 Aug 2026 22:02:57 GMT", first.LastModified)

	second, err := f.Fetch(context.Background(), srv.URL, first.ETag, first.LastModified)
	require.NoError(t, err)
	assert.True(t, second.NotModified)
	assert.Empty(t, second.Body)
	assert.Equal(t, `W/"v1"`, gotETag)
	assert.Equal(t, "Mon, 24 Aug 2026 22:02:57 GMT", gotIMS)
	// A 304 carries no validators of its own; keeping the ones we sent is what
	// stops the next poll from degrading into a full GET.
	assert.Equal(t, `W/"v1"`, second.ETag)
}

func TestFetchRejectsAnOversizedFeedRatherThanTruncatingIt(t *testing.T) {
	big := strings.Replace(sampleICS, "END:VCALENDAR", strings.Repeat("X-PAD:"+strings.Repeat("y", 100)+"\r\n", 200)+"END:VCALENDAR", 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(big))
	}))
	defer srv.Close()

	f := NewFetcher(FetchOptions{MaxSize: 1024, Timeout: 5 * time.Second, AllowInsecureURLs: true, AllowPrivateHosts: true})
	_, err := f.Fetch(context.Background(), srv.URL, "", "")

	// REVERT PROOF: a truncated .ics still parses. Accepting it would delete
	// every event past the cut-off from the mirrored calendar.
	assert.ErrorIs(t, err, ErrTooLarge)
}

func TestFetchRejectsANonCalendarBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/calendar")
		_, _ = w.Write([]byte("<!DOCTYPE html><html><body>Subscribe here!</body></html>"))
	}))
	defer srv.Close()

	_, err := localFetcher(t).Fetch(context.Background(), srv.URL, "", "")

	// The declared Content-Type says text/calendar and is a lie; the body is
	// what is checked. This is the shape of the real-world failure: a landing
	// page served where a feed was expected.
	assert.ErrorIs(t, err, ErrNotCalendar)
}

func TestFetchAcceptsAFeedWithNoContentType(t *testing.T) {
	// Verified against a real publisher (der-mond.de) that serves .ics with no
	// Content-Type header at all. A content-type check would reject it.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header()["Content-Type"] = nil
		_, _ = w.Write([]byte("\ufeff  \r\n" + sampleICS))
	}))
	defer srv.Close()

	res, err := localFetcher(t).Fetch(context.Background(), srv.URL, "", "")
	require.NoError(t, err)
	assert.Contains(t, string(res.Body), "SUMMARY:Hi")
}

func TestFetchReportsTheOriginStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	_, err := localFetcher(t).Fetch(context.Background(), srv.URL, "", "")
	require.Error(t, err)

	var feedErr *FeedError
	require.ErrorAs(t, err, &feedErr)
	assert.Equal(t, "HTTP 503: Service Unavailable", feedErr.Message)
}

func TestFetchDoesNotLeakTheURLInAFailureMessage(t *testing.T) {
	// A feed URL routinely carries a secret token. The message ends up in the
	// subscription list, so it must never repeat the URL.
	f := NewFetcher(FetchOptions{Timeout: 2 * time.Second, AllowPrivateHosts: true})
	_, err := f.Fetch(context.Background(), "https://feed.invalid.example/secret-token-abc123/cal.ics", "", "")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "secret-token-abc123")
}

func TestFetchRevalidatesRedirectTargets(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "file:///etc/passwd", http.StatusFound)
	}))
	defer origin.Close()

	_, err := localFetcher(t).Fetch(context.Background(), origin.URL, "", "")

	// REVERT PROOF: validating only the initial URL lets an https feed
	// redirect the fetch anywhere it likes.
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidURL)
}

func TestFetchFollowsAnOrdinaryRedirect(t *testing.T) {
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(sampleICS))
	}))
	defer feed.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, feed.URL, http.StatusMovedPermanently)
	}))
	defer origin.Close()

	res, err := localFetcher(t).Fetch(context.Background(), origin.URL, "", "")
	require.NoError(t, err)
	assert.Contains(t, string(res.Body), "BEGIN:VCALENDAR")
}

func TestFetchBoundsARedirectLoop(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srv.URL+"/next", http.StatusFound)
	}))
	defer srv.Close()

	_, err := localFetcher(t).Fetch(context.Background(), srv.URL, "", "")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidURL)
}

func mustIP(t *testing.T, s string) net.IP {
	t.Helper()
	ip := net.ParseIP(s)
	require.NotNil(t, ip, "unparseable test address %q", s)
	return ip
}
