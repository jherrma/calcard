//go:build integration

package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAccountDeletion exercises DELETE /users/me: wrong password → 401,
// wrong confirmation text → 4xx, correct credentials → 204 and the user
// is actually removed (login no longer works, old access token rejected).
func TestAccountDeletion(t *testing.T) {
	email := "delete-me@example.test"
	password := "deleteSecret!123"
	token := registerAndLogin(t, email, password, "Delete Me")

	// Seed something so the account owns state before deletion. The cascade
	// itself is asserted by TestAccountDeletionRemovesSharedData.
	_, _ = createCalendar(t, token, "To Be Deleted", "#123456")

	// --- Wrong password must 401 ---------------------------------------
	status, _ := restCall(t, http.MethodDelete, "/users/me", token, map[string]string{
		"password":     "wrong-password",
		"confirmation": "DELETE",
	})
	assert.Equal(t, http.StatusUnauthorized, status,
		"delete with wrong password must 401, not silently succeed")

	// --- Wrong confirmation text must 4xx ------------------------------
	status, _ = restCall(t, http.MethodDelete, "/users/me", token, map[string]string{
		"password":     password,
		"confirmation": "yes please",
	})
	assert.True(t, status == http.StatusBadRequest || status == http.StatusUnauthorized,
		"missing/incorrect confirmation text must 4xx, got %d", status)

	// --- Missing confirmation key must 4xx -----------------------------
	status, _ = restCall(t, http.MethodDelete, "/users/me", token, map[string]string{
		"password": password,
	})
	assert.GreaterOrEqual(t, status, 400)
	assert.Less(t, status, 500, "missing confirmation must 4xx, not 5xx")

	// --- Account is still alive after the rejected attempts ------------
	status, _ = restCall(t, http.MethodGet, "/users/me", token, nil)
	require.Equalf(t, http.StatusOK, status,
		"account must still exist after failed deletion attempts")

	// --- Correct password + confirmation deletes the account ----------
	status, raw := restCall(t, http.MethodDelete, "/users/me", token, map[string]string{
		"password":     password,
		"confirmation": "DELETE",
	})
	require.Equalf(t, http.StatusNoContent, status, "account deletion: %s", errorMessage(raw))

	// --- After deletion the old token must be rejected -----------------
	status, _ = restCall(t, http.MethodGet, "/users/me", token, nil)
	assert.Equalf(t, http.StatusUnauthorized, status,
		"old access token must be invalidated once the user row is gone")

	// --- And login with the old credentials must fail ------------------
	status, _ = restCall(t, http.MethodPost, "/auth/login", "", map[string]string{
		"email":    email,
		"password": password,
	})
	assert.Equalf(t, http.StatusUnauthorized, status,
		"login with a deleted account must 401")
}

// TestAccountDeletionRemovesSharedData is the F18 regression test: deleting an
// account must take its data with it. Before the fix only the users row was
// soft-deleted, so a sharee kept listing and reading the calendar (with a blank
// owner), the address book stayed readable, and the public link kept serving.
// Shares granted TO the deleted account must go as well.
func TestAccountDeletionRemovesSharedData(t *testing.T) {
	ownerEmail, ownerPassword := "f18-owner@example.test", "f18OwnerSecret!123"
	ownerToken := registerAndLogin(t, ownerEmail, ownerPassword, "F18 Owner")
	shareeEmail := "f18-sharee@example.test"
	shareeToken := registerAndLogin(t, shareeEmail, "f18ShareeSecret!123", "F18 Sharee")

	_, calUUID := createCalendar(t, ownerToken, "F18 Leaving", "#123456")
	createSeededEvent(t, ownerToken, calUUID, "F18 Leaving", 0)
	_, abUUID := createAddressBook(t, ownerToken, "F18 Leaving Book")
	contactID := createContactIn(t, ownerToken, abUUID, "F18 Private Contact")

	code := doJSONRaw(t, http.MethodPost, "/calendars/"+calUUID+"/shares", ownerToken,
		map[string]string{"user_identifier": shareeEmail, "permission": "read"}, nil)
	require.Equal(t, http.StatusCreated, code, "share calendar")
	shareAddressBook(t, ownerToken, abUUID, shareeEmail, "read")

	var pub struct {
		Token *string `json:"token"`
	}
	code = doJSONRaw(t, http.MethodPost, "/calendars/"+calUUID+"/public", ownerToken,
		map[string]bool{"enabled": true}, &pub)
	require.Equal(t, http.StatusOK, code)
	require.NotNil(t, pub.Token)
	publicURL := baseURL + "/public/calendar/" + *pub.Token

	// The sharee also shares one of their own calendars WITH the leaving owner.
	_, shareeCalUUID := createCalendar(t, shareeToken, "F18 Sharee Own", "#654321")
	code = doJSONRaw(t, http.MethodPost, "/calendars/"+shareeCalUUID+"/shares", shareeToken,
		map[string]string{"user_identifier": ownerEmail, "permission": "read"}, nil)
	require.Equal(t, http.StatusCreated, code, "share the sharee's calendar with the owner")

	eventsPath := "/calendars/" + calUUID + "/events?start=2030-01-01T00:00:00Z&end=2031-01-01T00:00:00Z"
	contactPath := "/addressbooks/" + abUUID + "/contacts/" + contactID

	// Preconditions: everything is reachable while the owner exists.
	status, _ := restCall(t, http.MethodGet, eventsPath, shareeToken, nil)
	require.Equal(t, http.StatusOK, status, "sharee reads the calendar")
	status, _ = restCall(t, http.MethodGet, contactPath, shareeToken, nil)
	require.Equal(t, http.StatusOK, status, "sharee reads the contact")
	status, _, _ = rawGet(t, publicURL)
	require.Equal(t, http.StatusOK, status, "public link serves")

	status, raw := restCall(t, http.MethodDelete, "/users/me", ownerToken, map[string]string{
		"password":     ownerPassword,
		"confirmation": "DELETE",
	})
	require.Equalf(t, http.StatusNoContent, status, "account deletion: %s", errorMessage(raw))

	status, body := restCall(t, http.MethodGet, "/calendars", shareeToken, nil)
	require.Equal(t, http.StatusOK, status)
	assert.NotContains(t, string(body), calUUID, "the deleted owner's calendar must leave the sharee's list")
	status, body = restCall(t, http.MethodGet, "/addressbooks", shareeToken, nil)
	require.Equal(t, http.StatusOK, status)
	assert.NotContains(t, string(body), abUUID, "the deleted owner's address book must leave the sharee's list")

	status, _ = restCall(t, http.MethodGet, eventsPath, shareeToken, nil)
	assert.Equal(t, http.StatusNotFound, status, "the sharee must no longer read the deleted owner's calendar")
	status, _ = restCall(t, http.MethodGet, contactPath, shareeToken, nil)
	assert.Equal(t, http.StatusNotFound, status, "the sharee must no longer read the deleted owner's contact")
	status, _, _ = rawGet(t, publicURL)
	assert.Equal(t, http.StatusNotFound, status, "the public link of a deleted account must stop serving")

	var shares struct {
		Shares []json.RawMessage `json:"shares"`
	}
	code = doJSONRaw(t, http.MethodGet, "/calendars/"+shareeCalUUID+"/shares", shareeToken, nil, &shares)
	require.Equal(t, http.StatusOK, code)
	assert.Empty(t, shares.Shares, "a share granted to the deleted account must be removed")
}

// TestReregisterAfterAccountDeletion (F19): the e-mail of a deleted account can
// register again. Deletion is a soft delete and the users.email unique index
// covers the deleted row, so this used to fail with 400 "registration failed".
// A live duplicate keeps answering 409.
func TestReregisterAfterAccountDeletion(t *testing.T) {
	email := "come-back@example.test"
	password := "comeBack!123"
	token := registerAndLogin(t, email, password, "Leaving")

	status, raw := restCall(t, http.MethodDelete, "/users/me", token, map[string]string{
		"password": password, "confirmation": "DELETE",
	})
	require.Equalf(t, http.StatusNoContent, status, "account deletion: %s", errorMessage(raw))

	// registerAndLogin asserts the 200 and that the new account can log in.
	token = registerAndLogin(t, email, password, "Returning")
	status, _ = restCall(t, http.MethodGet, "/users/me", token, nil)
	assert.Equal(t, http.StatusOK, status, "the returning account works")

	status, _ = restCall(t, http.MethodPost, "/auth/register", "", map[string]string{
		"email": email, "password": password, "display_name": "Twice",
	})
	assert.Equal(t, http.StatusConflict, status, "the address is taken again by the live account")
}
