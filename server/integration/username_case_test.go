//go:build integration

package integration_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUsernameLookupIsCaseInsensitive covers F02. The server assigned random
// mixed-case usernames, hands them to DAV clients as credentials.username and
// accepts them as a share's user_identifier — but the lookups lower-cased the
// input before an exact match, so the issued username was never found: Basic
// auth answered 401 and sharing 400 "user not found". A mixed-case CardDAV
// credential was created (201) and then never authenticated. New account and
// credential usernames are lowercase now, so the case-insensitive match is
// checked by sending them in upper case — for a CalDAV credential too, whose
// lookup used to be exact.
func TestUsernameLookupIsCaseInsensitive(t *testing.T) {
	ownerToken, ownerUsername := registerAndLoginFull(t, "f02-owner@example.test", "f02Secret!123", "F02 Owner")
	shareeToken, shareeUsername := registerAndLoginFull(t, "f02-sharee@example.test", "f02Secret!123", "F02 Sharee")

	// --- Basic auth with credentials.username from POST /app-passwords -----
	apUser, apPass := createAppPassword(t, shareeToken, "f02 sync")
	require.Equal(t, shareeUsername, apUser)
	for _, name := range []string{apUser, strings.ToUpper(apUser), strings.ToLower(apUser)} {
		status, _, body := davCall(t, "PROPFIND", "/dav/", name, apPass, propfindPrincipalBody, depthHeader("0"))
		assert.Equalf(t, http.StatusMultiStatus, status, "Basic auth as %q: %s", name, string(body))
	}

	// --- Sharing by username ---------------------------------------------
	_, calUUID := createCalendar(t, ownerToken, "F02 shared by username", "#336699")
	var calShare struct {
		SharedWith struct {
			Username string `json:"username"`
		} `json:"shared_with"`
	}
	code := doJSONRaw(t, http.MethodPost, "/calendars/"+calUUID+"/shares", ownerToken, map[string]string{
		"user_identifier": shareeUsername,
		"permission":      "read",
	}, &calShare)
	require.Equal(t, http.StatusCreated, code, "share a calendar with user_identifier=<username>")
	assert.Equal(t, shareeUsername, calShare.SharedWith.Username)

	_, abUUID := createAddressBook(t, ownerToken, "F02 book shared by username")
	code = doJSONRaw(t, http.MethodPost, "/addressbooks/"+abUUID+"/shares", ownerToken, map[string]string{
		"user_identifier": strings.ToUpper(shareeUsername),
		"permission":      "read",
	}, nil)
	require.Equal(t, http.StatusCreated, code, "share an address book with the username in another case")

	// --- CardDAV credential in another case -------------------------------
	// A credential's username is lowercase letters only, but a client may send
	// it in another case. (The CardDAV lookup also matches a mixed-case stored
	// name: webdav TestBasicAuthAcceptsMixedCaseUsernames.)
	credUser := credUsername("ftwocard")
	credPass := "MixedCase_Pass_v3rY!"
	code = doJSONRaw(t, http.MethodPost, "/carddav-credentials/", ownerToken, map[string]any{
		"name": "f02 card", "username": credUser, "password": credPass, "permission": "read",
	}, nil)
	require.Equal(t, http.StatusCreated, code, "create a CardDAV credential")
	for _, name := range []string{credUser, strings.ToUpper(credUser)} {
		status, _, body := davCall(t, "PROPFIND", "/dav/"+ownerUsername+"/addressbooks/", name, credPass,
			propfindAddressBookBody, depthHeader("1"))
		assert.Equalf(t, http.StatusMultiStatus, status, "CardDAV credential as %q: %s", name, string(body))
	}

	// --- CalDAV credential in another case --------------------------------
	// The CalDAV lookup used to match exactly: the same credential sent in
	// upper case was a 401 while CardDAV accepted it.
	calUser := credUsername("ftwocal")
	calPass := "CalCase_Pass_v3rY!"
	code = doJSONRaw(t, http.MethodPost, "/caldav-credentials/", ownerToken, map[string]any{
		"name": "f02 cal", "username": calUser, "password": calPass, "permission": "read",
	}, nil)
	require.Equal(t, http.StatusCreated, code, "create a CalDAV credential")
	for _, name := range []string{calUser, strings.ToUpper(calUser)} {
		status, _, body := davCall(t, "PROPFIND", "/dav/"+ownerUsername+"/calendars/", name, calPass,
			propfindCalendarBody, depthHeader("1"))
		assert.Equalf(t, http.StatusMultiStatus, status, "CalDAV credential as %q: %s", name, string(body))
	}
	status, _, _ := davCall(t, "PROPFIND", "/dav/"+ownerUsername+"/calendars/", strings.ToUpper(calUser), "wrong-Pass_123!",
		propfindCalendarBody, depthHeader("1"))
	assert.Equal(t, http.StatusUnauthorized, status, "CalDAV credential in upper case with a wrong password")
}
