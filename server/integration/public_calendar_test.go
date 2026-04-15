//go:build integration

package integration_test

import (
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPublicCalendar walks the public-token flow end-to-end:
// enable → anonymous GET returns the iCal feed → GET with a wrong token 404s
// → regenerate invalidates the old token → disable takes the feed offline.
//
// The feed must contain the event we seeded, so we also prove the token
// actually yields the right calendar's data (the classic "wrong feed" bug).
func TestPublicCalendar(t *testing.T) {
	email := "pub-cal@example.test"
	password := "publicSecret!123"
	token := registerAndLogin(t, email, password, "Public Cal User")

	calID, calUUID := createCalendar(t, token, "Team Holidays", "#998877")

	// Seed one event so the feed has recognisable content.
	eventUID, eventSummary := createSeededEvent(t, token, calID, "Team Holidays", 0)

	// --- Enable public access ---------------------------------------------
	var enable struct {
		Enabled   bool    `json:"enabled"`
		PublicURL *string `json:"public_url"`
		Token     *string `json:"token"`
	}
	code := doJSONRaw(t, http.MethodPost, "/calendars/"+uintStr(calID)+"/public", token,
		map[string]bool{"enabled": true}, &enable)
	require.Equal(t, http.StatusOK, code)
	assert.True(t, enable.Enabled)
	require.NotNil(t, enable.Token, "enable should return a token")
	require.NotNil(t, enable.PublicURL)
	require.NotEmpty(t, *enable.Token)
	firstToken := *enable.Token

	// --- Anonymous GET of the public feed ---------------------------------
	status, _, body := rawGet(t, baseURL+"/public/calendar/"+firstToken)
	require.Equalf(t, http.StatusOK, status, "public feed GET: %s", string(body))
	assert.Contains(t, string(body), "BEGIN:VCALENDAR")
	assert.Contains(t, string(body), "END:VCALENDAR")
	assert.Contains(t, string(body), "UID:"+eventUID, "feed must contain the seeded event's UID")
	assert.Contains(t, string(body), "SUMMARY:"+eventSummary, "feed must contain the seeded event's SUMMARY")
	// The `.ics` suffix on the URL is supported for subscription-client friendliness.
	status2, _, _ := rawGet(t, baseURL+"/public/calendar/"+firstToken+".ics")
	assert.Equal(t, http.StatusOK, status2, "token.ics URL should also work")

	// --- Unknown token must 404 -------------------------------------------
	status, _, _ = rawGet(t, baseURL+"/public/calendar/not-a-real-token")
	assert.Equal(t, http.StatusNotFound, status, "unknown token must 404")

	// --- Status endpoint mirrors what we just set -------------------------
	var statusResp struct {
		Enabled   bool    `json:"enabled"`
		PublicURL *string `json:"public_url"`
		Token     *string `json:"token"`
	}
	code = doJSONRaw(t, http.MethodGet, "/calendars/"+uintStr(calID)+"/public", token, nil, &statusResp)
	require.Equal(t, http.StatusOK, code)
	assert.True(t, statusResp.Enabled)
	require.NotNil(t, statusResp.Token)
	assert.Equal(t, firstToken, *statusResp.Token)

	// --- Regenerate: old token must stop working, new one must work -------
	var regen struct {
		Token *string `json:"token"`
	}
	code = doJSONRaw(t, http.MethodPost, "/calendars/"+uintStr(calID)+"/public/regenerate", token, nil, &regen)
	require.Equal(t, http.StatusOK, code)
	require.NotNil(t, regen.Token)
	require.NotEmpty(t, *regen.Token)
	assert.NotEqual(t, firstToken, *regen.Token, "regenerate should issue a fresh token")

	status, _, _ = rawGet(t, baseURL+"/public/calendar/"+firstToken)
	assert.Equal(t, http.StatusNotFound, status, "old token must stop working after regenerate")

	status, _, body = rawGet(t, baseURL+"/public/calendar/"+*regen.Token)
	require.Equal(t, http.StatusOK, status)
	assert.Contains(t, string(body), "UID:"+eventUID, "new token must serve the same calendar content")

	// --- Disable takes the feed offline -----------------------------------
	code = doJSONRaw(t, http.MethodPost, "/calendars/"+uintStr(calID)+"/public", token,
		map[string]bool{"enabled": false}, &enable)
	require.Equal(t, http.StatusOK, code)
	assert.False(t, enable.Enabled)

	status, _, _ = rawGet(t, baseURL+"/public/calendar/"+*regen.Token)
	assert.Equal(t, http.StatusNotFound, status, "after disable the most recent token must 404 too")

	_ = calUUID // not used here; kept so the call site reads naturally
}

// rawGet issues a plain unauthenticated GET against the given URL and returns
// (status, headers, body). Used for the /public/* endpoints which must work
// without any Authorization header.
func rawGet(t *testing.T, url string) (int, http.Header, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	require.NoError(t, err)
	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, resp.Header, body
}
