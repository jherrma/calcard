//go:build integration

package integration_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEventMove creates an event in calendar A, moves it to calendar B via
// POST /calendars/:calendar_id/events/:event_id/move, and verifies that the
// event disappears from A's list and appears in B's list with the same UID.
// This covers the dedicated move route that's distinct from a delete+recreate
// sequence (and is more efficient on the server side).
func TestEventMove(t *testing.T) {
	email := "move-evt@example.test"
	password := "moveSecret!123"
	token := registerAndLogin(t, email, password, "Event Move User")

	calA, _ := createCalendar(t, token, "Cal A", "#aa0000")
	calB, _ := createCalendar(t, token, "Cal B", "#00aa00")

	// Seed one event in calendar A.
	start := time.Date(2031, 8, 1, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	var ev struct {
		ID  string `json:"id"`
		UID string `json:"uid"`
	}
	code := doJSONRaw(t, http.MethodPost, "/calendars/"+uintStr(calA)+"/events/", token, map[string]any{
		"summary":  "About to move",
		"start":    start.Format(time.RFC3339),
		"end":      end.Format(time.RFC3339),
		"timezone": "UTC",
		"all_day":  false,
	}, &ev)
	require.Equal(t, http.StatusCreated, code)
	require.NotEmpty(t, ev.ID)
	originalUID := ev.UID

	// --- Move: POST /calendars/:calendar_id/events/:event_id/move --------
	var moved struct {
		UID        string `json:"uid"`
		CalendarID uint   `json:"calendar_id"`
	}
	movePath := "/calendars/" + uintStr(calA) + "/events/" + ev.ID + "/move"
	code = doJSONRaw(t, http.MethodPost, movePath, token, map[string]string{
		"target_calendar_id": uintStr(calB),
	}, &moved)
	require.Equal(t, http.StatusOK, code, "move event")
	assert.Equal(t, originalUID, moved.UID, "UID must survive the move")
	assert.Equal(t, calB, moved.CalendarID, "event should now report calendar B as its parent")

	// --- Verify A no longer has it, B does -------------------------------
	rangeQS := "?start=2000-01-01T00:00:00Z&end=2099-12-31T23:59:59Z&expand=false"
	uidsInA := collectEventUIDs(t, token, calA, rangeQS)
	uidsInB := collectEventUIDs(t, token, calB, rangeQS)
	assert.NotContains(t, uidsInA, originalUID, "moved event must be gone from calendar A")
	assert.Contains(t, uidsInB, originalUID, "moved event must appear in calendar B")
}

// TestContactMove creates a contact in address book A, moves it to B, and
// verifies that the source no longer lists it while the target does, with
// the vCard UID preserved. The contact-move endpoint is interesting because
// it does NOT take the source addressbook id from the URL — the server looks
// the contact up by its UUID and only uses the target id from the body.
func TestContactMove(t *testing.T) {
	email := "move-ct@example.test"
	password := "moveSecret!123"
	token := registerAndLogin(t, email, password, "Contact Move User")

	abA := createAddressBook(t, token, "AB A")
	abB := createAddressBook(t, token, "AB B")

	// Seed a contact in A.
	var ct struct {
		ID  string `json:"id"`
		UID string `json:"uid"`
	}
	code := doJSONRaw(t, http.MethodPost, "/addressbooks/"+uintStr(abA)+"/contacts", token,
		map[string]any{
			"formatted_name": "Mover McMoveson",
			"given_name":     "Mover",
			"family_name":    "McMoveson",
		}, &ct)
	require.Equal(t, http.StatusCreated, code)
	require.NotEmpty(t, ct.ID)
	originalUID := ct.UID

	// --- Move ------------------------------------------------------------
	movePath := "/addressbooks/" + uintStr(abA) + "/contacts/" + ct.ID + "/move"
	var moved struct {
		UID           string `json:"uid"`
		AddressBookID string `json:"addressbook_id"`
	}
	code = doJSONRaw(t, http.MethodPost, movePath, token, map[string]string{
		"target_addressbook_id": uintStr(abB),
	}, &moved)
	require.Equal(t, http.StatusOK, code, "move contact")
	assert.Equal(t, originalUID, moved.UID, "contact UID must survive the move")
	assert.Equal(t, uintStr(abB), moved.AddressBookID, "addressbook_id should reflect the target")

	// --- Verify ---------------------------------------------------------
	uidsInA := collectContactUIDs(t, token, abA)
	uidsInB := collectContactUIDs(t, token, abB)
	assert.NotContains(t, uidsInA, originalUID, "moved contact must be gone from AB A")
	assert.Contains(t, uidsInB, originalUID, "moved contact must appear in AB B")
}

// collectEventUIDs returns just the UID set for a calendar, in a windowed list.
func collectEventUIDs(t *testing.T, token string, calID uint, rangeQS string) []string {
	t.Helper()
	var resp struct {
		Events []struct {
			UID string `json:"uid"`
		} `json:"events"`
	}
	code := doJSONRaw(t, http.MethodGet,
		"/calendars/"+uintStr(calID)+"/events/"+rangeQS, token, nil, &resp)
	require.Equal(t, http.StatusOK, code)
	out := make([]string, 0, len(resp.Events))
	for _, e := range resp.Events {
		out = append(out, e.UID)
	}
	return out
}

func collectContactUIDs(t *testing.T, token string, abID uint) []string {
	t.Helper()
	var resp struct {
		Contacts []struct {
			UID string `json:"uid"`
		} `json:"Contacts"`
	}
	code := doJSONRaw(t, http.MethodGet,
		"/addressbooks/"+uintStr(abID)+"/contacts?limit=100", token, nil, &resp)
	require.Equal(t, http.StatusOK, code)
	out := make([]string, 0, len(resp.Contacts))
	for _, c := range resp.Contacts {
		out = append(out, c.UID)
	}
	return out
}
