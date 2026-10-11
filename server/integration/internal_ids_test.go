//go:build integration

package integration_test

import (
	"encoding/json"
	"net/http"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// forbiddenIDKeys are JSON keys that would expose a user's internal
// auto-increment primary key, or soft-delete bookkeeping, in a REST response
// (security finding P03). Users are addressed by UUID everywhere in the API;
// a sharee in particular must never learn the owner's numeric id.
var forbiddenIDKeys = map[string]bool{
	"user_id":        true, // calendar.Calendar.UserID
	"UserID":         true, // addressbook.AddressBook.UserID
	"owner_id":       true,
	"shared_with_id": true, // sharing.CalendarShare.SharedWithID
	"DeletedAt":      true, // addressbook.AddressBook.DeletedAt
	"deleted_at":     true,
}

// findForbiddenIDKeys walks a decoded JSON value and collects the path of
// every forbidden key, so a failure names exactly where an id leaked.
func findForbiddenIDKeys(v any, path string, out *[]string) {
	switch node := v.(type) {
	case map[string]any:
		for k, child := range node {
			p := path + "." + k
			if forbiddenIDKeys[k] {
				*out = append(*out, p)
			}
			findForbiddenIDKeys(child, p, out)
		}
	case []any:
		for _, child := range node {
			findForbiddenIDKeys(child, path+"[]", out)
		}
	}
}

func assertNoInternalIDs(t *testing.T, name string, body []byte) {
	t.Helper()
	var decoded any
	require.NoErrorf(t, json.Unmarshal(body, &decoded), "%s: response is not JSON: %s", name, body)
	var hits []string
	findForbiddenIDKeys(decoded, "$", &hits)
	sort.Strings(hits)
	assert.Emptyf(t, hits, "%s exposes internal ids: %v", name, hits)
}

// TestNoInternalUserIDsInResponses guards P03: calendar and address-book
// responses used to carry the owner's numeric user id ("user_id" / "UserID")
// and the address book's soft-delete column ("DeletedAt"), and a SHAREE got
// the owner's numeric id with every shared collection. Every response that
// serialises a calendar or address book — create, get, update, list, unified
// search — is checked, for the owner and for a sharee.
//
// Deliberately NOT forbidden: a collection's own numeric "id"/"ID" and an
// event's "calendar_id". Both identify the collection, not a user; anyone who
// sees the event already sees that calendar; and the web UI keys events to
// their calendar by exactly that value (stores/calendars.ts).
func TestNoInternalUserIDsInResponses(t *testing.T) {
	password := "noIDs!12345"
	const ownerEmail, shareeEmail = "ids-owner@example.test", "ids-sharee@example.test"
	ownerToken := registerAndLogin(t, ownerEmail, password, "IDs Owner")
	shareeToken := registerAndLogin(t, shareeEmail, password, "IDs Sharee")

	// --- calendar: create / get / update as the owner ---
	status, body := restCall(t, http.MethodPost, "/calendars/", ownerToken,
		map[string]string{"name": "Idleak Calendar", "color": "#112233"})
	require.Equal(t, http.StatusCreated, status, "create calendar: %s", body)
	assertNoInternalIDs(t, "owner calendar create", body)
	var cal struct {
		ID   uint   `json:"id"`
		UUID string `json:"uuid"`
	}
	require.NoError(t, json.Unmarshal(body, &cal))
	require.NotZero(t, cal.ID, "a calendar's own numeric id stays exposed (the SPA's handle)")
	require.NotEmpty(t, cal.UUID)

	status, body = restCall(t, http.MethodGet, "/calendars/"+cal.UUID, ownerToken, nil)
	require.Equal(t, http.StatusOK, status)
	assertNoInternalIDs(t, "owner calendar get", body)

	status, body = restCall(t, http.MethodPatch, "/calendars/"+cal.UUID, ownerToken,
		map[string]string{"description": "changed"})
	require.Equal(t, http.StatusOK, status, "update calendar: %s", body)
	assertNoInternalIDs(t, "owner calendar update", body)

	// An event still names its calendar by the calendar's numeric id.
	var ev map[string]any
	code := doJSONRaw(t, http.MethodPost, "/calendars/"+cal.UUID+"/events", ownerToken, map[string]any{
		"summary": "Idleak Event", "start": "2030-01-01T10:00:00Z", "end": "2030-01-01T11:00:00Z",
	}, &ev)
	require.Equal(t, http.StatusCreated, code, "create event")
	assert.EqualValues(t, cal.ID, ev["calendar_id"], "event.calendar_id is kept and equals the calendar's id")

	// --- address book: create / get / update as the owner ---
	status, body = restCall(t, http.MethodPost, "/addressbooks/", ownerToken,
		map[string]string{"name": "Idleak Book"})
	require.Equal(t, http.StatusCreated, status, "create address book: %s", body)
	assertNoInternalIDs(t, "owner address book create", body)
	var ab struct {
		ID   uint   `json:"ID"`
		UUID string `json:"UUID"`
	}
	require.NoError(t, json.Unmarshal(body, &ab))
	require.NotZero(t, ab.ID, "an address book's own numeric ID stays exposed (the SPA's handle)")
	require.NotEmpty(t, ab.UUID)

	status, body = restCall(t, http.MethodGet, "/addressbooks/"+ab.UUID, ownerToken, nil)
	require.Equal(t, http.StatusOK, status)
	assertNoInternalIDs(t, "owner address book get", body)

	status, body = restCall(t, http.MethodPatch, "/addressbooks/"+ab.UUID, ownerToken,
		map[string]string{"description": "changed"})
	require.Equal(t, http.StatusOK, status, "update address book: %s", body)
	assertNoInternalIDs(t, "owner address book update", body)

	// --- share both with the sharee ---
	code = doJSONRaw(t, http.MethodPost, "/calendars/"+cal.UUID+"/shares", ownerToken,
		map[string]string{"user_identifier": shareeEmail, "permission": "read"}, nil)
	require.Equal(t, http.StatusCreated, code, "share calendar")
	shareAB(t, ownerToken, ab.UUID, shareeEmail, "read")

	// Positive controls: the sharee really sees both shared collections, so
	// the sharee checks below cannot pass on an owner-only list.
	require.True(t, calendarVisibleTo(t, shareeToken, cal.UUID), "sharee must see the shared calendar")
	require.True(t, addressBookVisibleTo(t, shareeToken, ab.ID), "sharee must see the shared address book")

	get := func(token, path string) []byte {
		t.Helper()
		status, body := restCall(t, http.MethodGet, path, token, nil)
		require.Equalf(t, http.StatusOK, status, "GET %s: %s", path, body)
		return body
	}

	shareeSearch := get(shareeToken, "/search?q=Idleak")
	assert.Contains(t, string(shareeSearch), "Idleak Calendar", "sharee search positive control")
	assert.Contains(t, string(shareeSearch), "Idleak Book", "sharee search positive control")

	for name, body := range map[string][]byte{
		"owner calendars list":      get(ownerToken, "/calendars/"),
		"owner address books list":  get(ownerToken, "/addressbooks/"),
		"owner calendar shares":     get(ownerToken, "/calendars/"+cal.UUID+"/shares"),
		"owner address book shares": get(ownerToken, "/addressbooks/"+ab.UUID+"/shares"),
		"owner unified search":      get(ownerToken, "/search?q=Idleak"),
		"sharee calendars list":     get(shareeToken, "/calendars/"),
		"sharee address books list": get(shareeToken, "/addressbooks/"),
		"sharee unified search":     shareeSearch,
	} {
		assertNoInternalIDs(t, name, body)
	}
}
