//go:build integration

package integration_test

import (
	"net/http"
	"testing"

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
