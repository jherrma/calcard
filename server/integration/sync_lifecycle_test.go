//go:build integration

package integration_test

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSyncTokenSurvivesRenameAndMove is the regression test for H8: tokens
// minted outside the change-log (fresh collection, rename, event move) used to
// trigger a 403 valid-sync-token on the next incremental sync, forcing endless
// full resyncs. After the fix, the incremental sync succeeds (207) every time.
func TestSyncTokenSurvivesRenameAndMove(t *testing.T) {
	email := "sync-lifecycle@example.test"
	token, username := registerAndLoginFull(t, email, "syncSecret!123", "Sync Lifecycle")
	_, appPass := createAppPassword(t, token, "sync-lifecycle")

	// Create a brand-new calendar (fresh collection — token came from Create).
	calID, calUUID := createCalendar(t, token, "Lifecycle Cal", "#111222")
	collection := "/dav/" + username + "/calendars/" + calUUID + ".ics/"

	// Initial sync on the fresh collection returns a token...
	status, _, body := davCall(t, "REPORT", collection, email, appPass, syncCollectionBody, depthHeader("1"))
	require.Equalf(t, http.StatusMultiStatus, status, "initial sync: %s", string(body))
	tok := extractSyncToken(string(body))
	require.NotEmpty(t, tok)

	// ...and an incremental sync with it must NOT 403 (was the bug).
	status = incrementalSync(t, collection, email, appPass, tok)
	require.Equalf(t, http.StatusMultiStatus, status,
		"incremental sync on a fresh collection must succeed, got %d", status)

	// Rename the calendar via REST, then sync again with the pre-rename token.
	status, raw := restCall(t, http.MethodPatch, "/calendars/"+calUUID, token,
		map[string]any{"name": "Renamed Cal"})
	require.Equalf(t, http.StatusOK, status, "rename: %s", errorMessage(raw))

	status = incrementalSync(t, collection, email, appPass, tok)
	require.Equalf(t, http.StatusMultiStatus, status,
		"incremental sync after rename must succeed, got %d", status)

	// Capture the POST-rename token and sync with THAT token. The pre-rename
	// token's changelog anchor always survives, so syncing with it can't prove
	// the rename minted an anchored token — only a fresh post-rename token would
	// regress to a 403 if RecordChange were swapped back for UpdateSyncTokens.
	status, _, body = davCall(t, "REPORT", collection, email, appPass, syncCollectionBody, depthHeader("1"))
	require.Equalf(t, http.StatusMultiStatus, status, "post-rename REPORT: %s", string(body))
	postRenameTok := extractSyncToken(string(body))
	require.NotEmpty(t, postRenameTok, "post-rename REPORT must return a sync token")
	status = incrementalSync(t, collection, email, appPass, postRenameTok)
	require.Equalf(t, http.StatusMultiStatus, status,
		"incremental sync with the POST-rename token must succeed (not 403), got %d", status)

	// Move an event out of this calendar; the source must report a deletion,
	// not 403.
	putEvent(t, collection, email, appPass, "move-src", "Moving", time.Date(2035, 5, 1, 9, 0, 0, 0, time.UTC))
	// Capture a token AFTER the put so the move shows as a delta.
	status, _, body = davCall(t, "REPORT", collection, email, appPass, syncCollectionBody, depthHeader("1"))
	require.Equal(t, http.StatusMultiStatus, status)
	preMoveToken := extractSyncToken(string(body))

	// Find the event's REST id and a target calendar.
	eventID := firstEventID(t, token, calID, "?start=2035-01-01T00:00:00Z&end=2035-12-31T00:00:00Z")
	require.NotEmpty(t, eventID)

	targetID, _ := createCalendar(t, token, "Move Target", "#333444")
	status, raw = restCall(t, http.MethodPost,
		fmt.Sprintf("/calendars/%d/events/%s/move", calID, eventID), token,
		map[string]any{"target_calendar_id": uintStr(targetID)})
	require.Equalf(t, http.StatusOK, status, "move: %s", errorMessage(raw))

	// Incremental sync of the SOURCE with the pre-move token must succeed and
	// report the moved resource as removed (404 inside the multistatus).
	status, _, body = davCall(t, "REPORT", collection, email, appPass, incrementalSyncBody(preMoveToken), depthHeader("1"))
	require.Equalf(t, http.StatusMultiStatus, status, "source sync after move: %s", string(body))

	// Scope the 404 to the moved resource's OWN <response> block rather than
	// matching "404" anywhere in the multistatus (which could be an unrelated
	// status, an ETag, or a timestamp fragment).
	var ms struct {
		Responses []struct {
			Href   string `xml:"href"`
			Status string `xml:"status"`
		} `xml:"response"`
	}
	require.NoErrorf(t, xml.Unmarshal(body, &ms), "parse multistatus: %s", string(body))
	var foundMoved bool
	for _, r := range ms.Responses {
		if strings.Contains(r.Href, "move-src.ics") {
			foundMoved = true
			assert.Containsf(t, r.Status, "404", "moved resource's response must report 404, got %q", r.Status)
		}
	}
	assert.True(t, foundMoved, "source sync must include a response for the moved resource (move-src.ics)")
}

func incrementalSyncBody(tok string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<D:sync-collection xmlns:D="DAV:">
  <D:sync-token>%s</D:sync-token>
  <D:sync-level>1</D:sync-level>
  <D:prop><D:getetag/></D:prop>
</D:sync-collection>`, tok)
}

func incrementalSync(t *testing.T, collection, email, pass, tok string) int {
	t.Helper()
	status, _, _ := davCall(t, "REPORT", collection, email, pass, incrementalSyncBody(tok), depthHeader("1"))
	return status
}

func firstEventID(t *testing.T, token string, calID uint, rangeQS string) string {
	t.Helper()
	var resp struct {
		Events []struct {
			ID string `json:"id"`
		} `json:"events"`
	}
	require.Equal(t, http.StatusOK, doJSONRaw(t, http.MethodGet,
		"/calendars/"+uintStr(calID)+"/events/"+rangeQS, token, nil, &resp))
	if len(resp.Events) == 0 {
		return ""
	}
	return resp.Events[0].ID
}
