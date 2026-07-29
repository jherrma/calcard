//go:build integration

package integration_test

import (
	"net/http"
	"strings"
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
	_, calUUID := createCalendar(t, ownerToken, "Shared Work", "#445566")

	// --- Before sharing: target cannot modify the owner's calendar --------
	// PATCH /calendars/:uuid goes through UpdateCalendarUseCase which verifies
	// ownership — a stranger must be rejected, not silently allowed.
	rename := "Hijacked"
	status, _ := restCall(t, http.MethodPatch, "/calendars/"+calUUID, targetToken,
		map[string]*string{"name": &rename})
	// Assert the exact denial status, not just "!= 200": NotEqual(200) also
	// passes on a 500, which would hide a broken authz path. UpdateCalendarUseCase
	// rejects a non-owner with "access denied", which the handler maps to 400
	// (the calendar-mutation endpoints answer 400 here, unlike the event
	// endpoints' existence-hiding 404).
	assert.Equal(t, http.StatusBadRequest, status, "target user must NOT be able to rename owner's calendar before sharing")

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
	code := doJSONRaw(t, http.MethodPost, "/calendars/"+calUUID+"/shares", ownerToken, map[string]string{
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
	code = doJSONRaw(t, http.MethodGet, "/calendars/"+calUUID+"/shares", ownerToken, nil, &listResp)
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
	code = doJSONRaw(t, http.MethodPatch, "/calendars/"+calUUID+"/shares/"+shareUUID, ownerToken,
		map[string]string{"permission": "read"}, &updated)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "read", updated.Permission)

	// --- Owner revokes the share ------------------------------------------
	status, raw := restCall(t, http.MethodDelete, "/calendars/"+calUUID+"/shares/"+shareUUID, ownerToken, nil)
	require.Equalf(t, http.StatusNoContent, status, "revoke share: %s", errorMessage(raw))

	// --- Target no longer sees the calendar -------------------------------
	code = doJSONRaw(t, http.MethodGet, "/calendars/", targetToken, nil, &targetList)
	require.Equal(t, http.StatusOK, code)
	for _, c := range targetList.Calendars {
		assert.NotEqualf(t, calUUID, c.UUID, "revoked calendar must disappear from target's list")
	}

	// --- Re-sharing the same (calendar, user) pair must work (H6) ---------
	// Revoke soft-deleted the row before the fix, and the composite unique
	// index (calendar_id, shared_with_id) then permanently blocked re-sharing.
	code = doJSONRaw(t, http.MethodPost, "/calendars/"+calUUID+"/shares", ownerToken, map[string]string{
		"user_identifier": targetEmail,
		"permission":      "read",
	}, &createResp)
	require.Equalf(t, http.StatusCreated, code, "must be able to re-share the same calendar with the same user after revoke")
}

// TestAddressBookSharing walks the same lifecycle for address books, checking
// visibility on the target side through the share-list endpoint and via CardDAV
// PROPFIND on the home set. (The REST list endpoint enumerates shared books too
// — see TestSharedAddressBookRESTVisible — but the DAV assertion here is worth
// keeping: it's the path real clients take.)
func TestAddressBookSharing(t *testing.T) {
	ownerEmail := "absharing-owner@example.test"
	targetEmail := "absharing-target@example.test"
	password := "sharingSecret!123"

	ownerToken := registerAndLogin(t, ownerEmail, password, "AB Owner")
	targetToken, targetUsername := registerAndLoginFull(t, targetEmail, password, "AB Target")

	// Owner creates an address book to share.
	_, abUUID := createAddressBook(t, ownerToken, "Shared Colleagues")

	// Owner creates a share with the target at read-write.
	var createResp struct {
		ID         string `json:"id"`
		Permission string `json:"permission"`
		SharedWith struct {
			Email string `json:"email"`
		} `json:"shared_with"`
	}
	code := doJSONRaw(t, http.MethodPost, "/addressbooks/"+abUUID+"/shares", ownerToken, map[string]string{
		"user_identifier": targetEmail,
		"permission":      "read-write",
	}, &createResp)
	require.Equal(t, http.StatusCreated, code)
	require.NotEmpty(t, createResp.ID)
	assert.Equal(t, targetEmail, createResp.SharedWith.Email)
	shareUUID := createResp.ID

	// Owner listing the shares sees the target.
	status, raw := restCall(t, http.MethodGet, "/addressbooks/"+abUUID+"/shares", ownerToken, nil)
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
	code = doJSONRaw(t, http.MethodPatch, "/addressbooks/"+abUUID+"/shares/"+shareUUID, ownerToken,
		map[string]string{"permission": "read"}, &updated)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "read", updated.Permission)

	// Owner revokes.
	status, raw = restCall(t, http.MethodDelete, "/addressbooks/"+abUUID+"/shares/"+shareUUID, ownerToken, nil)
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

	abID, abUUID := createAddressBook(t, ownerToken, "Team Directory")

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
		"/addressbooks/"+abUUID+"/shares", ownerToken, map[string]string{
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
	_, calUUID := createCalendar(t, ownerToken, "TeamSchedule", "#336699")
	seedUID, _ := createSeededEvent(t, ownerToken, calUUID, "TeamSchedule", 0)
	// The owner addresses this calendar as {uuid}.ics; since #47 the sharee
	// sees it advertised under the bare UUID so a share can never collide
	// with one of their own collection paths.
	sharedSeg := calUUID

	// Owner shares with target at read-write.
	var shareResp struct {
		ID string `json:"id"`
	}
	code := doJSONRaw(t, http.MethodPost,
		"/calendars/"+calUUID+"/shares", ownerToken, map[string]string{
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
	assert.Containsf(t, string(body), "/calendars/"+sharedSeg+"/",
		"shared calendar must show up in target's CalDAV home-set under its UUID %q", sharedSeg)

	// 2. PROPFIND on the collection itself — addressed under the TARGET's
	//    /dav path, since ResolvePath matches parts[1] against the
	//    authenticated user. The shared-calendar fallback in ResolvePath
	//    matches the segment against the calendar's UUID (or legacy path).
	collection := home + sharedSeg + "/"
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
		"/calendars/"+calUUID+"/events/"+rangeQS, ownerToken, nil, &events)
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

// TestSharedAddressBookCardDAVVisible is the regression test for the DAV half
// of H7: a shared address book was listed in the home set but resolveAddressBook
// only searched owned books, so every PROPFIND/GET/REPORT on it 404'd. After the
// fix, a sharee can browse and (read-write) write to the shared book, and a
// read-only sharee is forbidden from writing.
func TestSharedAddressBookCardDAVVisible(t *testing.T) {
	ownerEmail := "abdav-owner@example.test"
	targetEmail := "abdav-target@example.test"
	roEmail := "abdav-ro@example.test"
	password := "abdavSecret!123"

	ownerToken, ownerUsername := registerAndLoginFull(t, ownerEmail, password, "AB DAV Owner")
	targetToken, targetUsername := registerAndLoginFull(t, targetEmail, password, "AB DAV Target")
	roToken, roUsername := registerAndLoginFull(t, roEmail, password, "AB DAV ReadOnly")

	_, abUUID := createAddressBook(t, ownerToken, "Shared DAV Book")

	// Owner's address book path slug (a UUID) — needed to build the DAV URL.
	abPath := addressBookPath(t, ownerToken, "Shared DAV Book")
	require.NotEmpty(t, abPath)

	// Seed a contact via the owner's CardDAV so there's something to read.
	_, ownerAppPass := createAppPassword(t, ownerToken, "abdav-owner-cred")
	ownerCollection := "/dav/" + ownerUsername + "/addressbooks/" + abPath + "/"
	seedUID := "abdav-seed@x"
	status, _, body := davCall(t, "PUT", ownerCollection+seedUID+".vcf", ownerEmail, ownerAppPass,
		buildMinimalVCard(seedUID, "Seed Contact", "Seed", "Contact"),
		map[string]string{"Content-Type": "text/vcard; charset=utf-8"})
	require.Containsf(t, []int{http.StatusCreated, http.StatusNoContent, http.StatusOK}, status, "owner seed PUT: %s", string(body))

	// Share read-write with target, read-only with the RO user.
	shareAB(t, ownerToken, abUUID, targetEmail, "read-write")
	shareAB(t, ownerToken, abUUID, roEmail, "read")

	// Read-write sharee: PROPFIND the shared collection under their own DAV path
	// (was 404 before the fix).
	_, targetAppPass := createAppPassword(t, targetToken, "abdav-target-cred")
	targetCollection := "/dav/" + targetUsername + "/addressbooks/" + abPath + "/"
	status, _, body = davCall(t, "PROPFIND", targetCollection, targetEmail, targetAppPass,
		propfindAddressBookBody, depthHeader("0"))
	require.Equalf(t, http.StatusMultiStatus, status, "sharee PROPFIND shared book: %s", string(body))

	// Read-write sharee can GET the seeded contact.
	status, _, body = davCall(t, "GET", targetCollection+seedUID+".vcf", targetEmail, targetAppPass, "", nil)
	require.Equalf(t, http.StatusOK, status, "sharee GET seeded contact: %s", string(body))
	assert.Contains(t, string(body), "Seed Contact")

	// Read-write sharee can PUT a new contact.
	newUID := "abdav-by-sharee@x"
	status, _, body = davCall(t, "PUT", targetCollection+newUID+".vcf", targetEmail, targetAppPass,
		buildMinimalVCard(newUID, "By Sharee", "By", "Sharee"),
		map[string]string{"Content-Type": "text/vcard; charset=utf-8"})
	require.Containsf(t, []int{http.StatusCreated, http.StatusNoContent, http.StatusOK}, status,
		"read-write sharee PUT: %s", string(body))

	// Read-only sharee may read but not delete.
	_, roAppPass := createAppPassword(t, roToken, "abdav-ro-cred")
	roCollection := "/dav/" + roUsername + "/addressbooks/" + abPath + "/"
	status, _, _ = davCall(t, "PROPFIND", roCollection, roEmail, roAppPass, propfindAddressBookBody, depthHeader("0"))
	require.Equal(t, http.StatusMultiStatus, status, "read-only sharee PROPFIND must work")
	status, _, _ = davCall(t, "DELETE", roCollection+seedUID+".vcf", roEmail, roAppPass, "", nil)
	assert.Equalf(t, http.StatusForbidden, status, "read-only sharee DELETE must be forbidden, got %d", status)
}

// addressBookPath returns the URL-path slug (UUID) of a named address book.
func addressBookPath(t *testing.T, token, name string) string {
	t.Helper()
	var wrap struct {
		AddressBooks []struct {
			Name string `json:"Name"`
			Path string `json:"Path"`
		} `json:"addressbooks"`
	}
	require.Equal(t, http.StatusOK, doJSONRaw(t, http.MethodGet, "/addressbooks/", token, nil, &wrap))
	for _, ab := range wrap.AddressBooks {
		if ab.Name == name {
			return ab.Path
		}
	}
	return ""
}

// addressBookUUID returns the book's UUID — the segment a SHAREE's home set
// advertises it under (#47). The owner keeps addressing it by Path.
func addressBookUUID(t *testing.T, token, name string) string {
	t.Helper()
	var wrap struct {
		AddressBooks []struct {
			Name string `json:"Name"`
			UUID string `json:"UUID"`
		} `json:"addressbooks"`
	}
	require.Equal(t, http.StatusOK, doJSONRaw(t, http.MethodGet, "/addressbooks/", token, nil, &wrap))
	for _, ab := range wrap.AddressBooks {
		if ab.Name == name {
			return ab.UUID
		}
	}
	return ""
}

func shareAB(t *testing.T, ownerToken string, abUUID string, targetEmail, permission string) {
	t.Helper()
	var resp struct {
		ID string `json:"id"`
	}
	code := doJSONRaw(t, http.MethodPost, "/addressbooks/"+abUUID+"/shares", ownerToken,
		map[string]string{"user_identifier": targetEmail, "permission": permission}, &resp)
	require.Equalf(t, http.StatusCreated, code, "share addressbook (%s)", permission)
}

// calendarVisibleTo reports whether the given calendar UUID appears in the
// user's REST calendar list (owned or shared).
func calendarVisibleTo(t *testing.T, token, calUUID string) bool {
	t.Helper()
	var list struct {
		Calendars []struct {
			UUID string `json:"uuid"`
		} `json:"calendars"`
	}
	code := doJSONRaw(t, http.MethodGet, "/calendars/", token, nil, &list)
	require.Equal(t, http.StatusOK, code)
	for _, c := range list.Calendars {
		if c.UUID == calUUID {
			return true
		}
	}
	return false
}

// addressBookVisibleTo reports whether the given address book id appears in
// the user's REST address book list (owned or shared).
func addressBookVisibleTo(t *testing.T, token string, abID uint) bool {
	t.Helper()
	var list struct {
		AddressBooks []struct {
			ID uint `json:"ID"`
		} `json:"addressbooks"`
	}
	code := doJSONRaw(t, http.MethodGet, "/addressbooks/", token, nil, &list)
	require.Equal(t, http.StatusOK, code)
	for _, ab := range list.AddressBooks {
		if ab.ID == abID {
			return true
		}
	}
	return false
}

// TestCalendarDeleteRevokesShares is the regression test for the ghost-share
// fix (TODO 4.3): deleting a shared calendar must revoke its shares so the
// sharee's list no longer shows a blank ghost entry, and a fresh share to the
// same user still works afterwards.
func TestCalendarDeleteRevokesShares(t *testing.T) {
	ownerEmail := "ghost-cal-owner@example.test"
	shareeEmail := "ghost-cal-sharee@example.test"
	password := "sharingSecret!123"

	ownerToken := registerAndLogin(t, ownerEmail, password, "Ghost Cal Owner")
	shareeToken, _ := registerAndLoginFull(t, shareeEmail, password, "Ghost Cal Sharee")

	// Owner needs more than one calendar (the last one can't be deleted).
	_, calUUID := createCalendar(t, ownerToken, "Doomed Calendar", "#abcdef")
	createCalendar(t, ownerToken, "Keeper Calendar", "#fedcba")

	var shareResp struct {
		ID string `json:"id"`
	}
	code := doJSONRaw(t, http.MethodPost, "/calendars/"+calUUID+"/shares", ownerToken,
		map[string]string{"user_identifier": shareeEmail, "permission": "read"}, &shareResp)
	require.Equal(t, http.StatusCreated, code)
	require.True(t, calendarVisibleTo(t, shareeToken, calUUID), "sharee should see the shared calendar")

	// Owner deletes the shared calendar.
	status, raw := restCall(t, http.MethodDelete, "/calendars/"+calUUID, ownerToken,
		map[string]string{"confirmation": "DELETE"})
	require.Equalf(t, http.StatusNoContent, status, "delete calendar: %s", errorMessage(raw))

	// The sharee's list must not carry a ghost entry for the deleted calendar.
	assert.False(t, calendarVisibleTo(t, shareeToken, calUUID),
		"deleted calendar must not linger as a ghost in the sharee's list")

	// And a new calendar can still be shared with the same user.
	_, newUUID := createCalendar(t, ownerToken, "Fresh Calendar", "#0a0a0a")
	code = doJSONRaw(t, http.MethodPost, "/calendars/"+newUUID+"/shares", ownerToken,
		map[string]string{"user_identifier": shareeEmail, "permission": "read"}, &shareResp)
	require.Equal(t, http.StatusCreated, code, "must be able to share a new calendar with the same user")
	assert.True(t, calendarVisibleTo(t, shareeToken, newUUID), "sharee should see the freshly shared calendar")
}

// TestAddressBookDeleteRevokesShares is the address-book analogue of the
// ghost-share fix.
func TestAddressBookDeleteRevokesShares(t *testing.T) {
	ownerEmail := "ghost-ab-owner@example.test"
	shareeEmail := "ghost-ab-sharee@example.test"
	password := "sharingSecret!123"

	ownerToken := registerAndLogin(t, ownerEmail, password, "Ghost AB Owner")
	shareeToken, _ := registerAndLoginFull(t, shareeEmail, password, "Ghost AB Sharee")

	abID, abUUID := createAddressBook(t, ownerToken, "Doomed Directory")
	shareAB(t, ownerToken, abUUID, shareeEmail, "read")
	require.True(t, addressBookVisibleTo(t, shareeToken, abID), "sharee should see the shared address book")

	// Owner deletes the shared address book.
	status, raw := restCall(t, http.MethodDelete, "/addressbooks/"+abUUID, ownerToken,
		map[string]string{"confirmation": "DELETE"})
	require.Equalf(t, http.StatusNoContent, status, "delete address book: %s", errorMessage(raw))

	assert.False(t, addressBookVisibleTo(t, shareeToken, abID),
		"deleted address book must not linger as a ghost in the sharee's list")

	// A new address book can still be shared with the same user.
	newABID, newABUUID := createAddressBook(t, ownerToken, "Fresh Directory")
	shareAB(t, ownerToken, newABUUID, shareeEmail, "read")
	assert.True(t, addressBookVisibleTo(t, shareeToken, newABID), "sharee should see the freshly shared address book")
}

// TestAddressBookDAVDeleteRevokesShares is the DAV-path analogue of
// TestAddressBookDeleteRevokesShares. That test deletes via REST, which always
// revoked shares; the CardDAV collection-DELETE path is the one that previously
// left the share dangling (and ListAddressBooks lacked the ghost guard). Delete
// over DAV, then confirm the sharee's CardDAV home set no longer lists the book.
func TestAddressBookDAVDeleteRevokesShares(t *testing.T) {
	ownerEmail := "ghost-ab-dav-owner@example.test"
	shareeEmail := "ghost-ab-dav-sharee@example.test"
	password := "sharingSecret!123"

	ownerToken, ownerUsername := registerAndLoginFull(t, ownerEmail, password, "Ghost AB DAV Owner")
	shareeToken, shareeUsername := registerAndLoginFull(t, shareeEmail, password, "Ghost AB DAV Sharee")
	_, ownerAppPass := createAppPassword(t, ownerToken, "ghost-ab-dav-owner")
	_, shareeAppPass := createAppPassword(t, shareeToken, "ghost-ab-dav-sharee")

	abID, abUUID := createAddressBook(t, ownerToken, "Doomed DAV Directory")
	abPath := addressBookPath(t, ownerToken, "Doomed DAV Directory")
	require.NotEmpty(t, abPath)

	shareAB(t, ownerToken, abUUID, shareeEmail, "read")
	require.True(t, addressBookVisibleTo(t, shareeToken, abID), "sharee should see the shared book pre-delete")

	// Sanity: the shared book appears in the sharee's CardDAV home set first, so
	// its later absence is meaningful.
	shareeHome := "/dav/" + shareeUsername + "/addressbooks/"
	propfind := `<?xml version="1.0" encoding="utf-8"?><D:propfind xmlns:D="DAV:"><D:prop><D:resourcetype/><D:displayname/></D:prop></D:propfind>`
	pfHeaders := map[string]string{"Depth": "1", "Content-Type": "application/xml"}
	status, _, body := davCall(t, "PROPFIND", shareeHome, shareeEmail, shareeAppPass, propfind, pfHeaders)
	require.Equalf(t, http.StatusMultiStatus, status, "sharee PROPFIND pre-delete: %s", string(body))
	require.Truef(t, strings.Contains(string(body), abUUID), "shared book must appear in sharee's home set (under its UUID) before delete")

	// Delete the book over DAV (CardDAVBackend.DeleteAddressBook).
	status, _, body = davCall(t, "DELETE", "/dav/"+ownerUsername+"/addressbooks/"+abPath+"/",
		ownerEmail, ownerAppPass, "", nil)
	require.Containsf(t, []int{http.StatusNoContent, http.StatusOK}, status, "DAV DELETE addressbook: %s", string(body))

	// The share must be revoked: no ghost in the sharee's CardDAV home set or
	// their REST list.
	status, _, body = davCall(t, "PROPFIND", shareeHome, shareeEmail, shareeAppPass, propfind, pfHeaders)
	require.Equalf(t, http.StatusMultiStatus, status, "sharee PROPFIND post-delete: %s", string(body))
	assert.NotContainsf(t, string(body), abUUID, "DAV-deleted book must not linger as a ghost in the sharee's home set")
	assert.False(t, addressBookVisibleTo(t, shareeToken, abID),
		"DAV-deleted book must not linger in the sharee's REST list")

	// Re-sharing a fresh book with the same user still works.
	newABID, newABUUID := createAddressBook(t, ownerToken, "Fresh DAV Directory")
	shareAB(t, ownerToken, newABUUID, shareeEmail, "read")
	assert.True(t, addressBookVisibleTo(t, shareeToken, newABID))
}

// TestSharedCalendarEventPermissions is the regression test for H7 (REST part):
// the event endpoints must honor share permissions, not just ownership. A
// stranger gets 404 (no existence leak); a read-write sharee can list/create
// events on the shared calendar (previously every event call 404'd because the
// gate only checked ownership); a read-only sharee can list but every write is
// rejected with 403.
func TestSharedCalendarEventPermissions(t *testing.T) {
	ownerEmail := "evt-share-owner@example.test"
	shareeEmail := "evt-share-sharee@example.test"
	password := "sharingSecret!123"

	ownerToken := registerAndLogin(t, ownerEmail, password, "Evt Owner")
	shareeToken, _ := registerAndLoginFull(t, shareeEmail, password, "Evt Sharee")

	_, calUUID := createCalendar(t, ownerToken, "Team Calendar", "#123456")
	eventsPath := "/calendars/" + calUUID + "/events/"
	rangeQS := "?start=2000-01-01T00:00:00Z&end=2099-12-31T23:59:59Z&expand=false"
	start := time.Date(2032, 3, 1, 9, 0, 0, 0, time.UTC)

	// Owner seeds an event.
	var seed struct {
		ID string `json:"id"`
	}
	code := doJSONRaw(t, http.MethodPost, eventsPath, ownerToken, map[string]any{
		"summary": "Owner Event", "start": start.Format(time.RFC3339),
		"end": start.Add(time.Hour).Format(time.RFC3339), "timezone": "UTC", "all_day": false,
	}, &seed)
	require.Equal(t, http.StatusCreated, code)

	// --- Before any share: the sharee can't see the calendar's events -----
	status, _ := restCall(t, http.MethodGet, eventsPath+rangeQS, shareeToken, nil)
	require.Equal(t, http.StatusNotFound, status, "stranger must get 404 listing a calendar they can't see")

	// --- Owner shares read-write ------------------------------------------
	var shareResp struct {
		ID string `json:"id"`
	}
	code = doJSONRaw(t, http.MethodPost, "/calendars/"+calUUID+"/shares", ownerToken,
		map[string]string{"user_identifier": shareeEmail, "permission": "read-write"}, &shareResp)
	require.Equal(t, http.StatusCreated, code)
	shareUUID := shareResp.ID

	// Read-write sharee can list events (404 before the fix).
	var shareeList struct {
		Events []struct {
			ID string `json:"id"`
		} `json:"events"`
	}
	code = doJSONRaw(t, http.MethodGet, eventsPath+rangeQS, shareeToken, nil, &shareeList)
	require.Equal(t, http.StatusOK, code, "read-write sharee must be able to list events")
	require.NotEmpty(t, shareeList.Events)

	// ...and create one the owner then sees.
	var created struct {
		ID string `json:"id"`
	}
	code = doJSONRaw(t, http.MethodPost, eventsPath, shareeToken, map[string]any{
		"summary": "Sharee Event", "start": start.Add(48 * time.Hour).Format(time.RFC3339),
		"end": start.Add(49 * time.Hour).Format(time.RFC3339), "timezone": "UTC", "all_day": false,
	}, &created)
	require.Equal(t, http.StatusCreated, code, "read-write sharee must be able to create events")
	require.GreaterOrEqual(t, len(collectEventUIDs(t, ownerToken, calUUID, rangeQS)), 2,
		"owner must see the event the sharee created")

	// --- Owner downgrades the share to read-only --------------------------
	var updated struct {
		Permission string `json:"permission"`
	}
	code = doJSONRaw(t, http.MethodPatch, "/calendars/"+calUUID+"/shares/"+shareUUID, ownerToken,
		map[string]string{"permission": "read"}, &updated)
	require.Equal(t, http.StatusOK, code)

	// Read-only sharee can still list...
	code = doJSONRaw(t, http.MethodGet, eventsPath+rangeQS, shareeToken, nil, &shareeList)
	require.Equal(t, http.StatusOK, code, "read-only sharee can still list events")
	require.NotEmpty(t, shareeList.Events)
	evID := shareeList.Events[0].ID

	// ...but every write is now 403, not a silent success and not a 404.
	status, _ = restCall(t, http.MethodPost, eventsPath, shareeToken, map[string]any{
		"summary": "Nope", "start": start.Format(time.RFC3339),
		"end": start.Add(time.Hour).Format(time.RFC3339), "timezone": "UTC", "all_day": false,
	})
	assert.Equal(t, http.StatusForbidden, status, "read-only sharee create must be 403")

	status, _ = restCall(t, http.MethodPatch, eventsPath+evID, shareeToken, map[string]any{"summary": "Hijack"})
	assert.Equal(t, http.StatusForbidden, status, "read-only sharee update must be 403")

	status, _ = restCall(t, http.MethodDelete, eventsPath+evID, shareeToken, nil)
	assert.Equal(t, http.StatusForbidden, status, "read-only sharee delete must be 403")
}

// TestCalendarShareAndPublicRequireUUID locks the #52 contract for the calendar
// family: the sharing and public-access endpoints now take the calendar UUID
// (matching the sibling CRUD routes), and the old numeric id is rejected with a
// 404 (not leaked as 400/403), so the identifier form can't silently drift back.
func TestCalendarShareAndPublicRequireUUID(t *testing.T) {
	password := "uuidContract!123"
	ownerToken := registerAndLogin(t, "uuidcontract-owner@example.test", password, "UUID Contract Owner")
	shareeEmail := "uuidcontract-sharee@example.test"
	registerAndLogin(t, shareeEmail, password, "UUID Contract Sharee")

	calID, calUUID := createCalendar(t, ownerToken, "UUID Contract Cal", "#101010")

	// shares: the UUID works; the numeric id must 404.
	var shareResp struct {
		ID string `json:"id"`
	}
	code := doJSONRaw(t, http.MethodPost, "/calendars/"+calUUID+"/shares", ownerToken,
		map[string]string{"user_identifier": shareeEmail, "permission": "read"}, &shareResp)
	require.Equal(t, http.StatusCreated, code, "share via UUID must succeed")

	status, _ := restCall(t, http.MethodGet, "/calendars/"+uintStr(calID)+"/shares", ownerToken, nil)
	assert.Equal(t, http.StatusNotFound, status, "numeric calendar id must 404 on /shares (UUID-only now)")

	// public: the UUID works; the numeric id must 404.
	var enable struct {
		Enabled bool `json:"enabled"`
	}
	code = doJSONRaw(t, http.MethodPost, "/calendars/"+calUUID+"/public", ownerToken,
		map[string]bool{"enabled": true}, &enable)
	require.Equal(t, http.StatusOK, code, "enable public via UUID must succeed")
	assert.True(t, enable.Enabled)

	status, _ = restCall(t, http.MethodGet, "/calendars/"+uintStr(calID)+"/public", ownerToken, nil)
	assert.Equal(t, http.StatusNotFound, status, "numeric calendar id must 404 on /public (UUID-only now)")
}
