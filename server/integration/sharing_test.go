//go:build integration

package integration_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCalendarSharing exercises the full calendar-share lifecycle:
// owner creates → lists → target user sees the shared calendar in their list
// → owner updates the permission → owner revokes → target no longer sees it.
// Also spot-checks that an unprivileged user can't mutate someone else's
// calendar before the share is in place.
func TestCalendarSharing(t *testing.T) {
	ownerEmail := "share-owner@example.test"
	targetEmail := "share-target@example.test"
	password := "sharingSecret!123"

	ownerToken := registerAndLogin(t, ownerEmail, password, "Share Owner")
	targetToken, _ := registerAndLoginFull(t, targetEmail, password, "Share Target")

	// --- Owner creates a calendar the test will share ---------------------
	calID, calUUID := createCalendar(t, ownerToken, "Shared Work", "#445566")

	// --- Before sharing: target cannot modify the owner's calendar --------
	// PATCH /calendars/:uuid goes through UpdateCalendarUseCase which verifies
	// ownership — a stranger must be rejected, not silently allowed.
	rename := "Hijacked"
	status, _ := restCall(t, http.MethodPatch, "/calendars/"+calUUID, targetToken,
		map[string]*string{"name": &rename})
	assert.NotEqual(t, http.StatusOK, status, "target user must NOT be able to rename owner's calendar before sharing")

	// Target should not see the calendar in their listing either.
	targetIdx := listCalendarsIndex(t, targetToken)
	_, seen := targetIdx["Shared Work"]
	assert.False(t, seen, "target must not see unshared calendar in their list")

	// --- Owner creates a share for the target user (read-write) -----------
	var createResp struct {
		ID         string `json:"id"`
		Permission string `json:"permission"`
		SharedWith struct {
			Email string `json:"email"`
		} `json:"shared_with"`
	}
	code := doJSONRaw(t, http.MethodPost, "/calendars/"+uintStr(calID)+"/shares", ownerToken, map[string]string{
		"user_identifier": targetEmail,
		"permission":      "read-write",
	}, &createResp)
	require.Equal(t, http.StatusCreated, code, "owner creates share")
	require.NotEmpty(t, createResp.ID)
	assert.Equal(t, "read-write", createResp.Permission)
	assert.Equal(t, targetEmail, createResp.SharedWith.Email)
	shareUUID := createResp.ID

	// --- Owner lists shares: sees the target ------------------------------
	var listResp struct {
		Shares []struct {
			ID         string `json:"id"`
			Permission string `json:"permission"`
			SharedWith struct {
				Email string `json:"email"`
			} `json:"shared_with"`
		} `json:"shares"`
	}
	code = doJSONRaw(t, http.MethodGet, "/calendars/"+uintStr(calID)+"/shares", ownerToken, nil, &listResp)
	require.Equal(t, http.StatusOK, code)
	require.Len(t, listResp.Shares, 1)
	assert.Equal(t, targetEmail, listResp.Shares[0].SharedWith.Email)

	// --- Target lists calendars: sees the shared one with Shared/Owner ---
	var targetList struct {
		Calendars []struct {
			ID     uint   `json:"id"`
			UUID   string `json:"uuid"`
			Name   string `json:"name"`
			Shared bool   `json:"shared"`
			Owner  *struct {
				DisplayName string `json:"display_name"`
			} `json:"owner,omitempty"`
		} `json:"calendars"`
	}
	code = doJSONRaw(t, http.MethodGet, "/calendars/", targetToken, nil, &targetList)
	require.Equal(t, http.StatusOK, code)
	var sharedEntry *struct {
		ID     uint   `json:"id"`
		UUID   string `json:"uuid"`
		Name   string `json:"name"`
		Shared bool   `json:"shared"`
		Owner  *struct {
			DisplayName string `json:"display_name"`
		} `json:"owner,omitempty"`
	}
	for i := range targetList.Calendars {
		if targetList.Calendars[i].UUID == calUUID {
			sharedEntry = &targetList.Calendars[i]
			break
		}
	}
	require.NotNil(t, sharedEntry, "shared calendar must appear in target's list")
	assert.True(t, sharedEntry.Shared, "entry should be flagged as shared")
	require.NotNil(t, sharedEntry.Owner, "shared entry should carry owner info")
	assert.Equal(t, "Share Owner", sharedEntry.Owner.DisplayName)

	// --- Owner downgrades permission to read-only -------------------------
	var updated struct {
		Permission string `json:"permission"`
	}
	code = doJSONRaw(t, http.MethodPatch, "/calendars/"+uintStr(calID)+"/shares/"+shareUUID, ownerToken,
		map[string]string{"permission": "read"}, &updated)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "read", updated.Permission)

	// --- Owner revokes the share ------------------------------------------
	status, raw := restCall(t, http.MethodDelete, "/calendars/"+uintStr(calID)+"/shares/"+shareUUID, ownerToken, nil)
	require.Equalf(t, http.StatusNoContent, status, "revoke share: %s", errorMessage(raw))

	// --- Target no longer sees the calendar -------------------------------
	code = doJSONRaw(t, http.MethodGet, "/calendars/", targetToken, nil, &targetList)
	require.Equal(t, http.StatusOK, code)
	for _, c := range targetList.Calendars {
		assert.NotEqualf(t, calUUID, c.UUID, "revoked calendar must disappear from target's list")
	}
}

// TestAddressBookSharing walks the same lifecycle for address books. The
// REST list endpoint currently does not include shared address books (there's
// a known TODO in `addressbook.ListUseCase`), so visibility on the target
// side is checked through the share-list endpoint and via CardDAV PROPFIND
// on the home set, which *does* enumerate shared address books.
func TestAddressBookSharing(t *testing.T) {
	ownerEmail := "absharing-owner@example.test"
	targetEmail := "absharing-target@example.test"
	password := "sharingSecret!123"

	ownerToken := registerAndLogin(t, ownerEmail, password, "AB Owner")
	targetToken, targetUsername := registerAndLoginFull(t, targetEmail, password, "AB Target")

	// Owner creates an address book to share.
	abID := createAddressBook(t, ownerToken, "Shared Colleagues")

	// Owner creates a share with the target at read-write.
	var createResp struct {
		ID         string `json:"id"`
		Permission string `json:"permission"`
		SharedWith struct {
			Email string `json:"email"`
		} `json:"shared_with"`
	}
	code := doJSONRaw(t, http.MethodPost, "/addressbooks/"+uintStr(abID)+"/shares", ownerToken, map[string]string{
		"user_identifier": targetEmail,
		"permission":      "read-write",
	}, &createResp)
	require.Equal(t, http.StatusCreated, code)
	require.NotEmpty(t, createResp.ID)
	assert.Equal(t, targetEmail, createResp.SharedWith.Email)
	shareUUID := createResp.ID

	// Owner listing the shares sees the target.
	status, raw := restCall(t, http.MethodGet, "/addressbooks/"+uintStr(abID)+"/shares", ownerToken, nil)
	require.Equal(t, http.StatusOK, status, "list shares: %s", errorMessage(raw))
	require.Contains(t, string(raw), targetEmail, "owner's share list should mention the target")

	// Target CardDAV home-set PROPFIND should include the shared book. We
	// need DAV creds for the target; create an app password on their behalf.
	_, targetAppPass := createAppPassword(t, targetToken, "absharing-test")
	davStatus, _, davBody := davCall(t, "PROPFIND",
		"/dav/"+targetUsername+"/addressbooks/",
		targetEmail, targetAppPass,
		propfindAddressBookHomeBody, depthHeader("1"))
	require.Equal(t, http.StatusMultiStatus, davStatus, "target home PROPFIND: %s", string(davBody))
	assert.Contains(t, string(davBody), "Shared Colleagues",
		"shared address book must be visible in target's CardDAV home")

	// Owner downgrades permission to read.
	var updated struct {
		Permission string `json:"permission"`
	}
	code = doJSONRaw(t, http.MethodPatch, "/addressbooks/"+uintStr(abID)+"/shares/"+shareUUID, ownerToken,
		map[string]string{"permission": "read"}, &updated)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "read", updated.Permission)

	// Owner revokes.
	status, raw = restCall(t, http.MethodDelete, "/addressbooks/"+uintStr(abID)+"/shares/"+shareUUID, ownerToken, nil)
	require.Equalf(t, http.StatusNoContent, status, "revoke: %s", errorMessage(raw))

	// After revoke the target's home no longer lists the shared book.
	davStatus, _, davBody = davCall(t, "PROPFIND",
		"/dav/"+targetUsername+"/addressbooks/",
		targetEmail, targetAppPass,
		propfindAddressBookHomeBody, depthHeader("1"))
	require.Equal(t, http.StatusMultiStatus, davStatus)
	assert.NotContains(t, string(davBody), "Shared Colleagues",
		"after revoke, the shared book must disappear from target's home")
}

// TestSharedAddressBookRESTVisible verifies that shared address books appear
// in the sharee's GET /addressbooks list with Shared=true and an owner block.
// Prior to the fix this endpoint only returned owned books (TODO in
// addressbook.ListUseCase) — TestAddressBookSharing worked around it by
// checking visibility via CardDAV PROPFIND. This test locks the REST path in
// place.
func TestSharedAddressBookRESTVisible(t *testing.T) {
	ownerEmail := "abrest-owner@example.test"
	targetEmail := "abrest-target@example.test"
	password := "abrestSecret!123"

	ownerToken := registerAndLogin(t, ownerEmail, password, "AB REST Owner")
	targetToken := registerAndLogin(t, targetEmail, password, "AB REST Target")

	abID := createAddressBook(t, ownerToken, "Team Directory")

	// Before the share, the target's list must NOT include the book.
	var targetList struct {
		AddressBooks []struct {
			ID     uint   `json:"ID"`
			Name   string `json:"Name"`
			Shared bool   `json:"shared"`
			Owner  *struct {
				DisplayName string `json:"display_name"`
			} `json:"owner,omitempty"`
		} `json:"addressbooks"`
	}
	code := doJSONRaw(t, http.MethodGet, "/addressbooks/", targetToken, nil, &targetList)
	require.Equal(t, http.StatusOK, code)
	for _, ab := range targetList.AddressBooks {
		assert.NotEqualf(t, abID, ab.ID,
			"target must not see unshared addressbook id=%d", abID)
	}

	// Owner shares the book with the target at read-write.
	var createResp struct {
		ID string `json:"id"`
	}
	code = doJSONRaw(t, http.MethodPost,
		"/addressbooks/"+uintStr(abID)+"/shares", ownerToken, map[string]string{
			"user_identifier": targetEmail,
			"permission":      "read-write",
		}, &createResp)
	require.Equal(t, http.StatusCreated, code, "create share")

	// Now the target's REST list must include the book with Shared=true and
	// an Owner pointing at the owner's display name. The list preserves
	// ordering (owned before shared), which we don't rely on here — we
	// search the slice for the known id.
	code = doJSONRaw(t, http.MethodGet, "/addressbooks/", targetToken, nil, &targetList)
	require.Equal(t, http.StatusOK, code)
	var sharedEntry *struct {
		ID     uint   `json:"ID"`
		Name   string `json:"Name"`
		Shared bool   `json:"shared"`
		Owner  *struct {
			DisplayName string `json:"display_name"`
		} `json:"owner,omitempty"`
	}
	for i := range targetList.AddressBooks {
		if targetList.AddressBooks[i].ID == abID {
			sharedEntry = &targetList.AddressBooks[i]
			break
		}
	}
	require.NotNilf(t, sharedEntry,
		"shared addressbook id=%d must appear in target's REST /addressbooks list", abID)
	assert.True(t, sharedEntry.Shared, "entry should carry shared=true")
	require.NotNil(t, sharedEntry.Owner, "shared entry should include owner info")
	assert.Equal(t, "AB REST Owner", sharedEntry.Owner.DisplayName,
		"owner.display_name should match the sharer")
}

// TestSharedCalendarCalDAVVisible verifies that a sharee can see and act on
// an owner's calendar through the CalDAV backend using their own Basic Auth
// credentials. Covers three things the REST-level TestCalendarSharing doesn't:
//  1. The shared calendar appears in the target's PROPFIND home-set listing.
//  2. PROPFIND on the owner's collection path — addressed under the TARGET's
//     /dav/{username}/calendars/... — resolves via shareRepo lookup.
//  3. A read-write sharee can PUT a new event into the shared calendar.
func TestSharedCalendarCalDAVVisible(t *testing.T) {
	ownerEmail := "caldavshare-owner@example.test"
	targetEmail := "caldavshare-target@example.test"
	password := "caldavShareSecret!123"

	ownerToken := registerAndLogin(t, ownerEmail, password, "CalDAV Share Owner")
	targetToken, targetUsername := registerAndLoginFull(t, targetEmail, password, "CalDAV Share Target")

	// Owner creates a calendar + seeds one event so the sharee has something
	// to discover via PROPFIND / GET.
	calID, calUUID := createCalendar(t, ownerToken, "TeamSchedule", "#336699")
	seedUID, _ := createSeededEvent(t, ownerToken, calID, "TeamSchedule", 0)
	calPath := calUUID + ".ics"

	// Owner shares with target at read-write.
	var shareResp struct {
		ID string `json:"id"`
	}
	code := doJSONRaw(t, http.MethodPost,
		"/calendars/"+uintStr(calID)+"/shares", ownerToken, map[string]string{
			"user_identifier": targetEmail,
			"permission":      "read-write",
		}, &shareResp)
	require.Equal(t, http.StatusCreated, code, "create calendar share")

	// Target needs DAV credentials.
	_, targetAppPass := createAppPassword(t, targetToken, "caldavshare-test")

	// 1. Target PROPFIND home-set — must list the shared calendar by path.
	home := "/dav/" + targetUsername + "/calendars/"
	status, _, body := davCall(t, "PROPFIND", home,
		targetEmail, targetAppPass,
		propfindCalendarHomeBody, depthHeader("1"))
	require.Equalf(t, http.StatusMultiStatus, status,
		"target home PROPFIND: %s", string(body))
	assert.Containsf(t, string(body), calPath,
		"shared calendar path %q must show up in target's CalDAV home-set", calPath)

	// 2. PROPFIND on the collection itself — addressed under the TARGET's
	//    /dav path, since ResolvePath matches parts[1] against the
	//    authenticated user. The shared-calendar fallback in ResolvePath
	//    handles the mapping by comparing calendar.Path.
	collection := home + calPath + "/"
	status, _, body = davCall(t, "PROPFIND", collection,
		targetEmail, targetAppPass,
		propfindCalendarBody, depthHeader("0"))
	require.Equalf(t, http.StatusMultiStatus, status,
		"target collection PROPFIND: %s", string(body))

	// 3. Target lists the seeded event via a sync-collection REPORT.
	//    The REPORT body asks only for etag/contenttype (not calendar-data),
	//    so the response carries <href>…/<object>.ics</href> entries but
	//    not the iCalendar UID. We assert on href shape — it must live
	//    under the shared collection path and end in .ics — which is enough
	//    to prove the sharee got the list back.
	status, _, body = davCall(t, "REPORT", collection,
		targetEmail, targetAppPass,
		syncCollectionBody, depthHeader("1"))
	require.Equalf(t, http.StatusMultiStatus, status,
		"target sync REPORT: %s", string(body))
	assert.Containsf(t, string(body), collection,
		"target sync REPORT must list hrefs under the shared collection")
	assert.Containsf(t, string(body), ".ics</href>",
		"target sync REPORT must advertise at least one object")
	_ = seedUID // kept for readability — it's the UID the server emitted at seed time

	// 4. Read-write sharee can PUT a brand new event into the shared
	//    calendar. Sanity-check that the write actually lands by looking it
	//    up through the REST events list (which the owner can always see).
	newUID := "shared-put-" + time.Now().Format("20060102T150405") + "@calcard.test"
	newPath := collection + newUID + ".ics"
	ical := buildMinimalVEvent(newUID, "Put by sharee",
		time.Date(2030, 9, 1, 10, 0, 0, 0, time.UTC))
	status, _, body = davCall(t, "PUT", newPath,
		targetEmail, targetAppPass,
		ical, map[string]string{"Content-Type": "text/calendar; charset=utf-8"})
	require.Containsf(t, []int{http.StatusCreated, http.StatusNoContent, http.StatusOK}, status,
		"read-write sharee PUT must succeed: %d %s", status, string(body))

	// Owner's REST /events lists the PUT.
	var events struct {
		Events []struct {
			UID string `json:"uid"`
		} `json:"events"`
	}
	rangeQS := "?start=2030-01-01T00:00:00Z&end=2031-12-31T23:59:59Z&expand=false"
	code = doJSONRaw(t, http.MethodGet,
		"/calendars/"+uintStr(calID)+"/events/"+rangeQS, ownerToken, nil, &events)
	require.Equal(t, http.StatusOK, code)
	seen := false
	for _, ev := range events.Events {
		if ev.UID == newUID {
			seen = true
			break
		}
	}
	assert.Truef(t, seen,
		"event PUT by sharee must appear in owner's REST /events list (uid=%s)", newUID)
}
