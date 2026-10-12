//go:build integration

package integration_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDAVPathForms is the regression test for F29 over the real wiring: a home
// set named without its trailing slash (or with a doubled slash) lists the same
// calendars as the canonical URL instead of an empty 207, and a CardDAV-only
// app password cannot reach a calendar by percent-encoding the "calendars"
// segment, which the scope check used to see raw while go-webdav decoded it.
func TestDAVPathForms(t *testing.T) {
	email := "path-forms@example.test"
	token, username := registerAndLoginFull(t, email, "pathFormsSecret!123", "Path Forms")
	_, appPass := createAppPassword(t, token, "path-forms")
	personal, ok := listCalendarsIndex(t, token)["Personal"]
	require.True(t, ok, "default Personal calendar should exist")
	collection := "/dav/" + username + "/calendars/" + personal.UUID + ".ics/"

	for _, home := range []string{"/dav/" + username + "/calendars", "/dav/" + username + "//calendars/"} {
		t.Run("PROPFIND "+home, func(t *testing.T) {
			status, hdr, body := davCall(t, "PROPFIND", home, email, appPass, propfindCalendarHomeBody, depthHeader("1"))
			require.Equal(t, http.StatusMultiStatus, status, "body: %s", body)
			assert.Contains(t, string(body), collection, "the home must list the Personal calendar")
			if home[len(home)-1] != '/' {
				assert.Equal(t, "/dav/"+username+"/calendars/", hdr.Get("Content-Location"))
			}
		})
	}

	var cardOnly struct {
		Password string `json:"password"`
	}
	require.Equal(t, http.StatusOK, doJSON(t, http.MethodPost, "/app-passwords/", token,
		map[string]any{"name": "path-forms-card", "scopes": []string{"carddav"}}, &cardOnly))
	encoded := "/dav/" + username + "/%63alendars/" + personal.UUID + ".ics/"
	status, _, body := davCall(t, "PROPFIND", encoded, email, cardOnly.Password, propfindCalendarHomeBody, depthHeader("1"))
	assert.Equal(t, http.StatusForbidden, status, "a carddav-only app password must not read calendars: %s", body)
}
