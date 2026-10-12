//go:build integration

package integration_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCalendarNotFoundStatus guards F12: every calendar endpoint that names a
// calendar (or a share) in its path answers 404 when that resource does not
// exist, and answers a calendar the caller does not own with the SAME status
// and body, so a stranger cannot probe for existence. PATCH, DELETE and import
// used to answer 400 ("calendar not found" vs "access denied", which even told
// the two apart). Genuine request errors on an owned calendar stay 400.
func TestCalendarNotFoundStatus(t *testing.T) {
	password := "notFoundSecret!123"
	ownerToken := registerAndLogin(t, "f12-owner@example.test", password, "F12 Owner")
	strangerToken := registerAndLogin(t, "f12-stranger@example.test", password, "F12 Stranger")

	// A second calendar besides the seeded Personal one, so the last-calendar
	// guard can never be what rejects a DELETE below.
	_, calUUID := createCalendar(t, ownerToken, "F12 Calendar", "#123456")
	const unknownUUID = "00000000-0000-0000-0000-000000000000"

	ics := []byte(buildMinimalVEvent("f12-import@example.test", "F12 import", time.Date(2031, 1, 1, 10, 0, 0, 0, time.UTC)))
	icsHeaders := map[string]string{"Content-Type": "text/calendar"}

	cases := []struct {
		name string
		call func(uuid, token string) (int, []byte)
	}{
		{"GET", func(uuid, token string) (int, []byte) {
			return restCall(t, http.MethodGet, "/calendars/"+uuid, token, nil)
		}},
		{"PATCH", func(uuid, token string) (int, []byte) {
			return restCall(t, http.MethodPatch, "/calendars/"+uuid, token, map[string]string{"name": "Hijacked"})
		}},
		{"DELETE", func(uuid, token string) (int, []byte) {
			return restCall(t, http.MethodDelete, "/calendars/"+uuid, token, map[string]string{"confirmation": "DELETE"})
		}},
		{"import", func(uuid, token string) (int, []byte) {
			return rawCall(t, http.MethodPost, baseURL+"/api/v1/calendars/"+uuid+"/import", token, ics, icsHeaders)
		}},
		{"export", func(uuid, token string) (int, []byte) {
			return restCall(t, http.MethodGet, "/calendars/"+uuid+"/export", token, nil)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			unknownStatus, unknownBody := tc.call(unknownUUID, ownerToken)
			foreignStatus, foreignBody := tc.call(calUUID, strangerToken)
			assert.Equalf(t, http.StatusNotFound, unknownStatus, "unknown calendar: %s", string(unknownBody))
			assert.Equalf(t, http.StatusNotFound, foreignStatus, "another user's calendar: %s", string(foreignBody))
			assert.Equal(t, string(unknownBody), string(foreignBody),
				"an unknown and a foreign calendar must be indistinguishable")
		})
	}

	// None of the stranger's attempts touched the owner's calendar.
	var cal struct {
		Name string `json:"name"`
	}
	require.Equal(t, http.StatusOK, doJSONRaw(t, http.MethodGet, "/calendars/"+calUUID, ownerToken, nil, &cal))
	assert.Equal(t, "F12 Calendar", cal.Name)
	var events struct {
		Events []any `json:"events"`
	}
	require.Equal(t, http.StatusOK, doJSONRaw(t, http.MethodGet,
		"/calendars/"+calUUID+"/events/?start=2000-01-01T00:00:00Z&end=2099-12-31T23:59:59Z", ownerToken, nil, &events))
	assert.Empty(t, events.Events, "the stranger's import must not have landed")

	t.Run("unknown share id", func(t *testing.T) {
		status, body := restCall(t, http.MethodPatch, "/calendars/"+calUUID+"/shares/"+unknownUUID, ownerToken,
			map[string]string{"permission": "read"})
		assert.Equalf(t, http.StatusNotFound, status, "PATCH unknown share: %s", string(body))
		status, body = restCall(t, http.MethodDelete, "/calendars/"+calUUID+"/shares/"+unknownUUID, ownerToken, nil)
		assert.Equalf(t, http.StatusNotFound, status, "DELETE unknown share: %s", string(body))
	})

	t.Run("request errors on an owned calendar stay 400", func(t *testing.T) {
		status, body := restCall(t, http.MethodPatch, "/calendars/"+calUUID, ownerToken, map[string]string{"color": "blue"})
		assert.Equalf(t, http.StatusBadRequest, status, "invalid colour: %s", string(body))
		status, body = restCall(t, http.MethodDelete, "/calendars/"+calUUID, ownerToken, map[string]string{"confirmation": "yes"})
		assert.Equalf(t, http.StatusBadRequest, status, "wrong confirmation: %s", string(body))
		status, body = rawCall(t, http.MethodPost, baseURL+"/api/v1/calendars/"+calUUID+"/import", ownerToken,
			[]byte("not an icalendar file"), icsHeaders)
		assert.Equalf(t, http.StatusBadRequest, status, "unparseable import: %s", string(body))

		// A real share with an invalid permission is a 400, not a 404.
		var share struct {
			ID string `json:"id"`
		}
		require.Equal(t, http.StatusCreated, doJSONRaw(t, http.MethodPost, "/calendars/"+calUUID+"/shares", ownerToken,
			map[string]string{"user_identifier": "f12-stranger@example.test", "permission": "read"}, &share))
		status, body = restCall(t, http.MethodPatch, "/calendars/"+calUUID+"/shares/"+share.ID, ownerToken,
			map[string]string{"permission": "admin"})
		assert.Equalf(t, http.StatusBadRequest, status, "invalid share permission: %s", string(body))
	})
}
