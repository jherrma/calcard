//go:build integration

package integration_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMalformedInputs feeds the server the kind of junk a confused client
// or a hostile probe would send: invalid JSON, missing required fields,
// unknown resource UUIDs, and a malformed iCalendar PUT. The point of this
// test is *not* to validate specific error messages — it's to prove the
// server responds with 4xx instead of 5xx / panics / stack traces, which
// would indicate an unhandled path.
func TestMalformedInputs(t *testing.T) {
	email := "malformed@example.test"
	password := "malformedSecret!123"
	token, username := registerAndLoginFull(t, email, password, "Malformed User")
	_, appPass := createAppPassword(t, token, "malformed-test")

	cases := []struct {
		name    string
		method  string
		path    string
		token   string
		body    any
		headers map[string]string
		wantMin int
		wantMax int // inclusive
	}{
		// ------ Malformed request bodies --------------------------------
		{
			name:    "register with non-JSON body",
			method:  http.MethodPost,
			path:    "/auth/register",
			body:    []byte("not-even-json"),
			headers: map[string]string{"Content-Type": "application/json"},
			wantMin: 400, wantMax: 499,
		},
		{
			name:    "register with truncated JSON",
			method:  http.MethodPost,
			path:    "/auth/register",
			body:    []byte(`{"email": "foo@bar.baz",`), // dangling
			headers: map[string]string{"Content-Type": "application/json"},
			wantMin: 400, wantMax: 499,
		},
		{
			name:    "login without credentials",
			method:  http.MethodPost,
			path:    "/auth/login",
			body:    map[string]string{},
			wantMin: 400, wantMax: 499,
		},
		{
			name:    "register missing required fields",
			method:  http.MethodPost,
			path:    "/auth/register",
			body:    map[string]string{"email": "incomplete@x.test"},
			wantMin: 400, wantMax: 499,
		},
		{
			name:    "create calendar with empty name",
			method:  http.MethodPost,
			path:    "/calendars/",
			token:   token,
			body:    map[string]string{"name": ""},
			wantMin: 400, wantMax: 499,
		},
		{
			name:    "create calendar with invalid color",
			method:  http.MethodPost,
			path:    "/calendars/",
			token:   token,
			body:    map[string]string{"name": "Bad Color", "color": "not-a-hex"},
			wantMin: 400, wantMax: 499,
		},
		{
			name:    "create addressbook with empty name",
			method:  http.MethodPost,
			path:    "/addressbooks/",
			token:   token,
			body:    map[string]string{"name": ""},
			wantMin: 400, wantMax: 499,
		},

		// ------ Unknown resource identifiers ----------------------------
		{
			name:    "get unknown calendar UUID",
			method:  http.MethodGet,
			path:    "/calendars/00000000-0000-0000-0000-000000000000",
			token:   token,
			wantMin: 400, wantMax: 499,
		},
		{
			name:    "get unknown addressbook ID",
			method:  http.MethodGet,
			path:    "/addressbooks/99999999",
			token:   token,
			wantMin: 400, wantMax: 499,
		},
		{
			name:    "delete unknown calendar UUID",
			method:  http.MethodDelete,
			path:    "/calendars/00000000-0000-0000-0000-000000000000",
			token:   token,
			body:    map[string]string{"confirmation": "DELETE"},
			wantMin: 400, wantMax: 499,
		},
		{
			name:    "event in unknown calendar",
			method:  http.MethodGet,
			path:    "/calendars/99999999/events/?start=2000-01-01T00:00:00Z&end=2099-12-31T23:59:59Z",
			token:   token,
			wantMin: 200, wantMax: 499, // empty list is 200, error is 4xx — either is fine, must not be 5xx
		},
		{
			name:    "contact search without query",
			method:  http.MethodGet,
			path:    "/contacts/search",
			token:   token,
			wantMin: 400, wantMax: 499,
		},

		// ------ Authentication failures ---------------------------------
		{
			name:    "protected endpoint with no token",
			method:  http.MethodGet,
			path:    "/users/me",
			wantMin: 401, wantMax: 401,
		},
		{
			name:    "protected endpoint with garbage token",
			method:  http.MethodGet,
			path:    "/users/me",
			token:   "not.a.real.jwt",
			wantMin: 401, wantMax: 401,
		},
		{
			name:    "wrong auth scheme",
			method:  http.MethodGet,
			path:    "/users/me",
			headers: map[string]string{"Authorization": "Basic Zm9vOmJhcg=="},
			wantMin: 401, wantMax: 401,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, raw := rawCallWithHeaders(t, tc.method, baseURL+"/api/v1"+tc.path, tc.token, tc.body, tc.headers)
			assert.GreaterOrEqualf(t, status, tc.wantMin,
				"status %d below expected minimum %d (body: %s)", status, tc.wantMin, string(raw))
			assert.LessOrEqualf(t, status, tc.wantMax,
				"status %d above expected maximum %d (body: %s)", status, tc.wantMax, string(raw))
			assert.Lessf(t, status, 500,
				"must not return 5xx for malformed client input (got %d, body: %s)", status, string(raw))
		})
	}

	// --- Malformed iCalendar PUT over CalDAV -----------------------------
	// A client sending junk bytes at the DAV PUT route must fail cleanly,
	// not 500 and not silently store the bad bytes. Use the default Personal
	// calendar's path.
	idx := listCalendarsIndex(t, token)
	personal, ok := idx["Personal"]
	require.True(t, ok)
	badIcal := "THIS IS NOT ICAL\r\nBEGIN:NOT\r\nEND:NOT\r\n"
	eventPath := "/dav/" + username + "/calendars/" + personal.UUID + ".ics/malformed.ics"
	status, _, body := davCall(t, "PUT", eventPath, email, appPass, badIcal, map[string]string{
		"Content-Type": "text/calendar; charset=utf-8",
	})
	assert.GreaterOrEqualf(t, status, 400, "malformed iCal PUT: %s", string(body))
	assert.Lessf(t, status, 500, "malformed iCal PUT must 4xx (got %d, body: %s)", status, string(body))

	// And a GET of a never-created DAV event must 404.
	ghost := "/dav/" + username + "/calendars/" + personal.UUID + ".ics/this-never-existed.ics"
	status, _, _ = davCall(t, "GET", ghost, email, appPass, "", nil)
	assert.Equal(t, http.StatusNotFound, status, "GET of a nonexistent DAV event must 404")
}

// rawCallWithHeaders is a convenience on top of `rawCall` that lets a test
// pick the Content-Type and other headers explicitly.
func rawCallWithHeaders(t *testing.T, method, fullURL, token string, body any, headers map[string]string) (int, []byte) {
	t.Helper()
	return rawCall(t, method, fullURL, token, body, headers)
}
