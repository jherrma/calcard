//go:build integration

package integration_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// propfindPrincipalCombinedBody asks for the current-user-principal plus BOTH
// the CalDAV calendar-home-set and the CardDAV addressbook-home-set in a single
// PROPFIND — exactly what RFC 6764 bootstrapping clients (DAVx5, Apple) send
// against the discovery root.
const propfindPrincipalCombinedBody = `<?xml version="1.0" encoding="utf-8"?>
<D:propfind xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav" xmlns:A="urn:ietf:params:xml:ns:carddav">
  <D:prop>
    <D:current-user-principal/>
    <C:calendar-home-set/>
    <A:addressbook-home-set/>
  </D:prop>
</D:propfind>`

// TestPrincipalDiscovery is the regression test for M2: a single PROPFIND on
// the discovery root ("/dav/") or the principal ("/dav/{user}/") must return
// BOTH the calendar and addressbook home sets. Previously each per-protocol
// handler only advertised its own home set, so a client discovering through
// one path never learned about the other collection type.
func TestPrincipalDiscovery(t *testing.T) {
	email := "principal-disco@example.test"
	token, username := registerAndLoginFull(t, email, "principalSecret!123", "Principal Disco")
	_, appPass := createAppPassword(t, token, "principal-disco")

	calendarHome := "/dav/" + username + "/calendars/"
	addressbookHome := "/dav/" + username + "/addressbooks/"

	for _, target := range []string{"/dav/", "/dav/" + username + "/"} {
		t.Run("PROPFIND "+target, func(t *testing.T) {
			status, _, body := davCall(t, "PROPFIND", target, email, appPass,
				propfindPrincipalCombinedBody, depthHeader("0"))
			require.Equalf(t, http.StatusMultiStatus, status, "PROPFIND %s: %s", target, string(body))

			assert.Containsf(t, string(body), "/dav/"+username+"/",
				"principal response should reference the current-user-principal: %s", string(body))
			assert.Containsf(t, string(body), calendarHome,
				"principal response must advertise the calendar home set: %s", string(body))
			assert.Containsf(t, string(body), addressbookHome,
				"principal response must advertise the addressbook home set: %s", string(body))
		})
	}

	// OPTIONS on the principal advertises both DAV capabilities in one response.
	t.Run("OPTIONS principal advertises both capabilities", func(t *testing.T) {
		status, hdrs, body := davCall(t, "OPTIONS", "/dav/"+username+"/", email, appPass, "", nil)
		require.Equalf(t, http.StatusNoContent, status, "OPTIONS principal: %s", string(body))
		dav := hdrs.Get("DAV")
		assert.Containsf(t, dav, "calendar-access", "DAV header should advertise CalDAV: %q", dav)
		assert.Containsf(t, dav, "addressbook", "DAV header should advertise CardDAV: %q", dav)
	})

	// The combined discovery above uses an app password with BOTH scopes, so the
	// per-protocol suppression is never exercised. A dedicated single-protocol
	// credential must see ONLY its own home set (dav_protocol drives the filter).
	t.Run("dedicated CalDAV credential sees only the calendar home set", func(t *testing.T) {
		const credUser, credPass = "disco-cal-cred", "DiscoCalPass_v3rY!S1x"
		var created struct {
			ID string `json:"id"`
		}
		code := doJSONRaw(t, http.MethodPost, "/caldav-credentials/", token, map[string]any{
			"name": "disco-caldav", "username": credUser, "password": credPass, "permission": "read",
		}, &created)
		require.Equalf(t, http.StatusCreated, code, "create caldav credential: %d", code)

		status, _, body := davCall(t, "PROPFIND", "/dav/"+username+"/", credUser, credPass,
			propfindPrincipalCombinedBody, depthHeader("0"))
		require.Equalf(t, http.StatusMultiStatus, status, "PROPFIND principal (caldav cred): %s", string(body))
		assert.Containsf(t, string(body), calendarHome, "caldav credential must see the calendar home set: %s", string(body))
		assert.NotContainsf(t, string(body), addressbookHome, "caldav credential must NOT see the addressbook home set: %s", string(body))
	})

	t.Run("dedicated CardDAV credential sees only the addressbook home set", func(t *testing.T) {
		const credUser, credPass = "disco-card-cred", "DiscoCardPass_v3rY!S1x"
		var created struct {
			ID string `json:"id"`
		}
		code := doJSONRaw(t, http.MethodPost, "/carddav-credentials/", token, map[string]any{
			"name": "disco-carddav", "username": credUser, "password": credPass, "permission": "read",
		}, &created)
		require.Equalf(t, http.StatusCreated, code, "create carddav credential: %d", code)

		status, _, body := davCall(t, "PROPFIND", "/dav/"+username+"/", credUser, credPass,
			propfindPrincipalCombinedBody, depthHeader("0"))
		require.Equalf(t, http.StatusMultiStatus, status, "PROPFIND principal (carddav cred): %s", string(body))
		assert.Containsf(t, string(body), addressbookHome, "carddav credential must see the addressbook home set: %s", string(body))
		assert.NotContainsf(t, string(body), calendarHome, "carddav credential must NOT see the calendar home set: %s", string(body))
	})
}
