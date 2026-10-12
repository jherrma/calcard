//go:build integration

package integration_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEventURLMustNameItsCalendar is the F09 regression guard over the real
// routes: GET/PATCH/DELETE /calendars/:calendar_id/events/:event_id and the
// move route used to look the event up by its UUID alone, so it answered under
// another of the caller's calendars, an all-zero UUID or a UUID that does not
// exist. Each must now be the same 404 an unknown event gets, and change nothing.
func TestEventURLMustNameItsCalendar(t *testing.T) {
	token := registerAndLogin(t, "evt-path@example.test", "pathSecret!123", "Event Path User")
	_, homeUUID := createCalendar(t, token, "Home", "#aa0000")
	_, otherUUID := createCalendar(t, token, "Other", "#00aa00")

	start := time.Date(2031, 3, 1, 9, 0, 0, 0, time.UTC)
	var ev struct {
		ID string `json:"id"`
	}
	code := doJSONRaw(t, http.MethodPost, "/calendars/"+homeUUID+"/events/", token, map[string]any{
		"summary": "Stays home",
		"start":   start.Format(time.RFC3339),
		"end":     start.Add(time.Hour).Format(time.RFC3339),
	}, &ev)
	require.Equal(t, http.StatusCreated, code)

	unknownCode, unknownBody := restCall(t, http.MethodGet, "/calendars/"+homeUUID+"/events/00000000-0000-0000-0000-000000000001", token, nil)
	require.Equal(t, http.StatusNotFound, unknownCode)

	for _, wrong := range []string{otherUUID, "00000000-0000-0000-0000-000000000000", "5b0e7c1a-0000-4000-8000-000000000000"} {
		base := "/calendars/" + wrong + "/events/" + ev.ID
		for _, tc := range []struct {
			method, path string
			body         any
		}{
			{http.MethodGet, base, nil},
			{http.MethodPatch, base, map[string]string{"summary": "hijacked"}},
			{http.MethodDelete, base, nil},
			{http.MethodPost, base + "/move", map[string]string{"target_calendar_id": otherUUID}},
		} {
			code, body := restCall(t, tc.method, tc.path, token, tc.body)
			assert.Equal(t, http.StatusNotFound, code, "%s %s", tc.method, tc.path)
			assert.JSONEq(t, string(unknownBody), string(body), "%s %s must look exactly like an unknown event", tc.method, tc.path)
		}
	}

	var got struct {
		Summary string `json:"summary"`
	}
	code = doJSONRaw(t, http.MethodGet, "/calendars/"+homeUUID+"/events/"+ev.ID, token, nil, &got)
	require.Equal(t, http.StatusOK, code, "the event is still served under its own calendar")
	assert.Equal(t, "Stays home", got.Summary, "no wrong-calendar request may have changed it")
	uids := collectEventUIDs(t, token, otherUUID, "?start=2000-01-01T00:00:00Z&end=2099-12-31T23:59:59Z&expand=false")
	assert.Empty(t, uids, "no wrong-calendar move may have filed it elsewhere")
}

// TestContactMoveMustNameItsAddressBook applies the same rule to the one
// contact route that ignored its :addressbook_id: the move. Every other contact
// route already checks the book in the use case.
func TestContactMoveMustNameItsAddressBook(t *testing.T) {
	token := registerAndLogin(t, "ct-path@example.test", "pathSecret!123", "Contact Path User")
	_, homeUUID := createAddressBook(t, token, "Home book")
	_, otherUUID := createAddressBook(t, token, "Other book")

	var ct struct {
		ID  string `json:"id"`
		UID string `json:"uid"`
	}
	code := doJSONRaw(t, http.MethodPost, "/addressbooks/"+homeUUID+"/contacts", token,
		map[string]any{"formatted_name": "Homebody", "given_name": "Home"}, &ct)
	require.Equal(t, http.StatusCreated, code)

	for _, wrong := range []string{otherUUID, "00000000-0000-0000-0000-000000000000"} {
		code, _ := restCall(t, http.MethodPost, "/addressbooks/"+wrong+"/contacts/"+ct.ID+"/move", token,
			map[string]string{"target_addressbook_id": otherUUID})
		assert.Equal(t, http.StatusNotFound, code, "move via book %s", wrong)
	}
	assert.Contains(t, collectContactUIDs(t, token, homeUUID), ct.UID, "the contact stays in its book")
	assert.NotContains(t, collectContactUIDs(t, token, otherUUID), ct.UID)
}
