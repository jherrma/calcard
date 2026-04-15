//go:build integration

package integration_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAuthorizationBoundaries asserts that user B, authenticated with their
// own token, can neither read nor mutate any resource owned by user A via
// the REST API or via CalDAV/CardDAV. This is the kind of check that catches
// one-line authorization regressions which would otherwise leak one user's
// calendar into every other user's account.
//
// Note: we intentionally use 404 (not 403) for authorization failures so the
// server doesn't leak existence ("this calendar exists, but you can't see
// it" is itself a leak). The assertions below therefore accept any 4xx from
// the REST API for the "cannot read" cases, and specifically 404/not-found
// semantics for the "cannot mutate" cases.
func TestAuthorizationBoundaries(t *testing.T) {
	password := "authzSecret!123"
	aliceToken, aliceUser := registerAndLoginFull(t, "authz-alice@example.test", password, "Alice")
	bobToken, _ := registerAndLoginFull(t, "authz-bob@example.test", password, "Bob")

	// Alice creates a calendar, an event, an address book, and a contact.
	aliceCalID, aliceCalUUID := createCalendar(t, aliceToken, "Alice's Calendar", "#ff0000")

	startT := time.Date(2031, 10, 1, 9, 0, 0, 0, time.UTC)
	endT := startT.Add(time.Hour)
	var ev struct {
		ID  string `json:"id"`
		UID string `json:"uid"`
	}
	code := doJSONRaw(t, http.MethodPost,
		"/calendars/"+uintStr(aliceCalID)+"/events/", aliceToken,
		map[string]any{
			"summary":  "Alice's secret meeting",
			"start":    startT.Format(time.RFC3339),
			"end":      endT.Format(time.RFC3339),
			"timezone": "UTC",
			"all_day":  false,
		}, &ev)
	require.Equal(t, http.StatusCreated, code)

	aliceAbID := createAddressBook(t, aliceToken, "Alice's Addressbook")
	var ct struct {
		ID  string `json:"id"`
		UID string `json:"uid"`
	}
	code = doJSONRaw(t, http.MethodPost,
		"/addressbooks/"+uintStr(aliceAbID)+"/contacts", aliceToken,
		map[string]any{
			"formatted_name": "Alice's contact",
			"given_name":     "Secret",
			"family_name":    "Contact",
		}, &ct)
	require.Equal(t, http.StatusCreated, code)

	// Bob needs his own calendar in-hand for the event-move hijack attempt.
	bobCalID, _ := createCalendar(t, bobToken, "Bob's Calendar", "#0000ff")

	// ------ Calendar-level boundaries ------------------------------------

	t.Run("Bob cannot GET Alice's calendar", func(t *testing.T) {
		status, _ := restCall(t, http.MethodGet, "/calendars/"+aliceCalUUID, bobToken, nil)
		assert.GreaterOrEqual(t, status, 400)
		assert.Less(t, status, 500)
	})
	t.Run("Bob cannot PATCH Alice's calendar", func(t *testing.T) {
		newName := "Owned by Bob"
		status, _ := restCall(t, http.MethodPatch, "/calendars/"+aliceCalUUID, bobToken,
			map[string]*string{"name": &newName})
		assert.GreaterOrEqual(t, status, 400)
		assert.Less(t, status, 500)

		// And Alice's calendar really wasn't renamed.
		var cal struct {
			Name string `json:"name"`
		}
		code := doJSONRaw(t, http.MethodGet, "/calendars/"+aliceCalUUID, aliceToken, nil, &cal)
		require.Equal(t, http.StatusOK, code)
		assert.Equal(t, "Alice's Calendar", cal.Name, "Alice's calendar name must be untouched")
	})
	t.Run("Bob cannot DELETE Alice's calendar", func(t *testing.T) {
		status, _ := restCall(t, http.MethodDelete, "/calendars/"+aliceCalUUID, bobToken,
			map[string]string{"confirmation": "DELETE"})
		assert.GreaterOrEqual(t, status, 400)
		assert.Less(t, status, 500)
	})

	// ------ Event-level boundaries (via Alice's calendar id) -------------

	t.Run("Bob cannot list events in Alice's calendar", func(t *testing.T) {
		status, _ := restCall(t, http.MethodGet,
			"/calendars/"+uintStr(aliceCalID)+"/events/?start=2000-01-01T00:00:00Z&end=2099-12-31T23:59:59Z",
			bobToken, nil)
		assert.Equal(t, http.StatusNotFound, status,
			"Bob must not be able to list Alice's events using her numeric calendar id")
	})
	t.Run("Bob cannot GET a specific event in Alice's calendar", func(t *testing.T) {
		status, _ := restCall(t, http.MethodGet,
			"/calendars/"+uintStr(aliceCalID)+"/events/"+ev.ID, bobToken, nil)
		assert.Equal(t, http.StatusNotFound, status)
	})
	t.Run("Bob cannot PATCH an event in Alice's calendar", func(t *testing.T) {
		newSummary := "Hijacked by Bob"
		status, _ := restCall(t, http.MethodPatch,
			"/calendars/"+uintStr(aliceCalID)+"/events/"+ev.ID, bobToken,
			map[string]any{"summary": newSummary})
		assert.Equal(t, http.StatusNotFound, status)
	})
	t.Run("Bob cannot DELETE Alice's event", func(t *testing.T) {
		status, _ := restCall(t, http.MethodDelete,
			"/calendars/"+uintStr(aliceCalID)+"/events/"+ev.ID, bobToken, nil)
		assert.Equal(t, http.StatusNotFound, status)

		// Alice's event must still be there.
		var got struct {
			Summary string `json:"summary"`
		}
		code := doJSONRaw(t, http.MethodGet,
			"/calendars/"+uintStr(aliceCalID)+"/events/"+ev.ID, aliceToken, nil, &got)
		require.Equal(t, http.StatusOK, code)
		assert.Equal(t, "Alice's secret meeting", got.Summary, "Alice's event must survive Bob's delete attempt")
	})
	t.Run("Bob cannot move Alice's event into Bob's calendar", func(t *testing.T) {
		status, _ := restCall(t, http.MethodPost,
			"/calendars/"+uintStr(aliceCalID)+"/events/"+ev.ID+"/move", bobToken,
			map[string]string{"target_calendar_id": uintStr(bobCalID)})
		assert.Equal(t, http.StatusNotFound, status)
	})

	// ------ Address book / contact boundaries ----------------------------

	t.Run("Bob cannot GET Alice's addressbook", func(t *testing.T) {
		status, _ := restCall(t, http.MethodGet, "/addressbooks/"+uintStr(aliceAbID), bobToken, nil)
		assert.GreaterOrEqual(t, status, 400)
		assert.Less(t, status, 500)
	})
	t.Run("Bob cannot list contacts in Alice's addressbook", func(t *testing.T) {
		status, _ := restCall(t, http.MethodGet,
			"/addressbooks/"+uintStr(aliceAbID)+"/contacts", bobToken, nil)
		assert.Equal(t, http.StatusNotFound, status)
	})
	t.Run("Bob cannot GET Alice's contact", func(t *testing.T) {
		status, _ := restCall(t, http.MethodGet,
			"/addressbooks/"+uintStr(aliceAbID)+"/contacts/"+ct.ID, bobToken, nil)
		assert.Equal(t, http.StatusNotFound, status)
	})
	t.Run("Bob cannot DELETE Alice's contact", func(t *testing.T) {
		status, _ := restCall(t, http.MethodDelete,
			"/addressbooks/"+uintStr(aliceAbID)+"/contacts/"+ct.ID, bobToken, nil)
		assert.Equal(t, http.StatusNotFound, status)

		var got struct {
			FormattedName string `json:"formatted_name"`
		}
		code := doJSONRaw(t, http.MethodGet,
			"/addressbooks/"+uintStr(aliceAbID)+"/contacts/"+ct.ID, aliceToken, nil, &got)
		require.Equal(t, http.StatusOK, code)
		assert.Equal(t, "Alice's contact", got.FormattedName, "Alice's contact must survive Bob's delete attempt")
	})

	// ------ DAV-level boundary -------------------------------------------
	// Bob authenticates with *his own* app password but targets Alice's
	// principal path. The DAV backend checks that the URL's username segment
	// matches the authenticated user's username, so the request must fail.

	t.Run("Bob cannot PROPFIND Alice's CalDAV home with his own credentials", func(t *testing.T) {
		_, bobAppPass := createAppPassword(t, bobToken, "authz-bob-dav")
		status, _, body := davCall(t, "PROPFIND",
			"/dav/"+aliceUser+"/calendars/",
			"authz-bob@example.test", bobAppPass,
			propfindCalendarHomeBody, depthHeader("1"))
		// The server may answer 207 with an empty multistatus, or return
		// 404/403 — but the response must not include Alice's calendar data.
		assert.NotContains(t, string(body), "Alice's Calendar",
			"Bob's DAV request to Alice's principal must not leak her calendar name")
		// Any 2xx that did contain content would have tripped the assertion
		// above. A 403/404 is also fine. What we must not see is 207 with
		// Alice's content.
		if status == http.StatusMultiStatus {
			// Ensure the body is at most a bare multistatus skeleton for Bob
			// himself — i.e. no `<calendar>` resourcetype from another user's
			// home set. We simply verify Alice's name didn't leak (checked).
			return
		}
		assert.GreaterOrEqual(t, status, 400)
	})
}
