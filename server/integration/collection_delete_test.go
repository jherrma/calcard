//go:build integration

package integration_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/sharing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCollectionDeletePurgesRows deletes a calendar and an address book
// through every path a user has — REST, a DAV client's DELETE on the
// collection, MCP's delete_calendar / delete_address_book, and for a
// subscribed calendar the subscription endpoint — and then counts what is
// left in the database. Deleting used to soft-delete only the collection row:
// every event, contact, photo, change-log row and share stayed, unreachable
// but retained until the whole account was deleted. A 404 from the API cannot
// tell those apart, so these tests ask the database (testDB).
func TestCollectionDeletePurgesRows(t *testing.T) {
	const password = "PurgePass123!"
	ownerEmail, shareeEmail := "purge-owner@example.test", "purge-sharee@example.test"
	token, username := registerAndLoginFull(t, ownerEmail, password, "Purge Owner")
	shareeToken := registerAndLogin(t, shareeEmail, password, "Purge Sharee")
	_, appPass := createAppPassword(t, token, "purge-dav")
	mcpCredential := mcpToken(t, token, "purge mcp")

	// seedCalendar creates a calendar with two events, a share and a public
	// link — something in every table that hangs off a calendar.
	seedCalendar := func(t *testing.T, name string) (id uint, uuid, publicToken string) {
		t.Helper()
		id, uuid = createCalendar(t, token, name, "#3366aa")
		createSeededEvent(t, token, uuid, name, 1)
		createSeededEvent(t, token, uuid, name, 2)
		shareCollection(t, token, "/calendars/"+uuid+"/shares", shareeEmail)
		var public struct {
			Token *string `json:"token"`
		}
		require.Equal(t, http.StatusOK, doJSONRaw(t, http.MethodPost, "/calendars/"+uuid+"/public", token,
			map[string]bool{"enabled": true}, &public))
		require.NotNil(t, public.Token)
		requireRows(t, calendarDBRows(t, id))
		return id, uuid, *public.Token
	}
	assertCalendarGone := func(t *testing.T, id uint, uuid, publicToken string) {
		t.Helper()
		for table, n := range calendarDBRows(t, id) {
			assert.Zerof(t, n, "%s: the deleted calendar's rows must be gone from the database", table)
		}
		status, _, _ := rawGet(t, baseURL+"/public/calendar/"+publicToken)
		assert.Equal(t, http.StatusNotFound, status, "the public link must die with the calendar")
		status, _ = restCall(t, http.MethodGet, "/calendars/"+uuid, shareeToken, nil)
		assert.Equal(t, http.StatusNotFound, status, "the sharee must not reach the deleted calendar")
	}

	// seedAddressBook creates an address book with a contact carrying a photo
	// and a share.
	seedAddressBook := func(t *testing.T, name string) (id uint, uuid, path string, contacts []uint) {
		t.Helper()
		uuid, path = createAddressBookWithPath(t, token, name)
		var contact struct {
			ID string `json:"id"`
		}
		require.Equal(t, http.StatusCreated, doJSONRaw(t, http.MethodPost, "/addressbooks/"+uuid+"/contacts", token,
			map[string]any{"formatted_name": name + " contact", "given_name": "Pic", "family_name": "Ture"}, &contact))
		status, raw := rawCall(t, http.MethodPut, baseURL+"/api/v1/addressbooks/"+uuid+"/contacts/"+contact.ID+"/photo",
			token, readAsset(t, "user-icon.jpg"), map[string]string{"Content-Type": "image/jpeg"})
		require.Equalf(t, http.StatusNoContent, status, "upload photo: %s", errorMessage(raw))
		shareCollection(t, token, "/addressbooks/"+uuid+"/shares", shareeEmail)

		var book addressbook.AddressBook
		require.NoError(t, testDB.Where("uuid = ?", uuid).First(&book).Error)
		require.NoError(t, testDB.Unscoped().Model(&addressbook.AddressObject{}).
			Where("address_book_id = ?", book.ID).Pluck("id", &contacts).Error)
		requireRows(t, addressBookDBRows(t, book.ID, contacts))
		return book.ID, uuid, path, contacts
	}
	assertAddressBookGone := func(t *testing.T, id uint, uuid string, contacts []uint) {
		t.Helper()
		for table, n := range addressBookDBRows(t, id, contacts) {
			assert.Zerof(t, n, "%s: the deleted address book's rows must be gone from the database", table)
		}
		status, _ := restCall(t, http.MethodGet, "/addressbooks/"+uuid, shareeToken, nil)
		assert.Equal(t, http.StatusNotFound, status, "the sharee must not reach the deleted address book")
	}

	// A collection nobody deletes: none of its rows may go.
	keptCal, _, _ := seedCalendar(t, "Purge kept")
	keptBook, _, _, keptContacts := seedAddressBook(t, "Purge kept book")
	keptCalRows, keptBookRows := calendarDBRows(t, keptCal), addressBookDBRows(t, keptBook, keptContacts)

	t.Run("calendar over REST", func(t *testing.T) {
		id, uuid, pub := seedCalendar(t, "Purge REST")
		status, raw := restCall(t, http.MethodDelete, "/calendars/"+uuid, token, map[string]string{"confirmation": "DELETE"})
		require.Equalf(t, http.StatusNoContent, status, "delete: %s", errorMessage(raw))
		assertCalendarGone(t, id, uuid, pub)
	})

	t.Run("calendar over CalDAV", func(t *testing.T) {
		id, uuid, pub := seedCalendar(t, "Purge DAV")
		collection := "/dav/" + username + "/calendars/" + uuid + ".ics/"
		status, _, body := davCall(t, "DELETE", collection, ownerEmail, appPass, "", nil)
		require.Containsf(t, []int{http.StatusOK, http.StatusNoContent}, status, "DAV DELETE: %s", body)
		assertCalendarGone(t, id, uuid, pub)

		// Created again at the same URL, the collection starts out empty.
		status, _, body = davCall(t, "MKCALENDAR", collection, ownerEmail, appPass, "", nil)
		require.Equalf(t, http.StatusCreated, status, "MKCALENDAR again: %s", body)
		status, _, body = davCall(t, "PROPFIND", collection, ownerEmail, appPass, propfindCalendarBody, depthHeader("1"))
		require.Equalf(t, http.StatusMultiStatus, status, "PROPFIND: %s", body)
		assert.Equal(t, 1, strings.Count(string(body), "<response"), "the re-created collection must list itself and no member: %s", body)
		var again calendar.Calendar
		require.NoError(t, testDB.Where("user_id = (SELECT id FROM users WHERE email = ?) AND path = ?", ownerEmail, uuid+".ics").First(&again).Error)
		assert.NotEqual(t, id, again.ID, "a new row, not the old one revived")
		var objects int64
		require.NoError(t, testDB.Unscoped().Model(&calendar.CalendarObject{}).Where("calendar_id = ?", again.ID).Count(&objects).Error)
		assert.Zero(t, objects)
	})

	t.Run("calendar over MCP", func(t *testing.T) {
		id, uuid, pub := seedCalendar(t, "Purge MCP")
		deleted, isErr := mcpTool(t, mcpCredential, "delete_calendar", map[string]any{"calendar_id": uuid, "confirmation": "DELETE"})
		require.False(t, isErr, "delete_calendar failed: %v", deleted)
		assertCalendarGone(t, id, uuid, pub)
	})

	t.Run("subscribed calendar over the subscription endpoint", func(t *testing.T) {
		feed := newTestFeed(t)
		feed.set(feedV1)
		var sub subscriptionPayload
		require.Equal(t, http.StatusCreated, doJSONRaw(t, http.MethodPost, "/calendar-subscriptions/", token,
			map[string]any{"url": feed.URL, "refresh_interval": "1h"}, &sub))
		var cal calendar.Calendar
		require.NoError(t, testDB.Where("uuid = ?", sub.CalendarID).First(&cal).Error)
		shareCollection(t, token, "/calendars/"+cal.UUID+"/shares", shareeEmail)
		rows := calendarDBRows(t, cal.ID)
		require.EqualValues(t, 2, rows["calendar_objects"], "the feed's two events")
		require.EqualValues(t, 1, rows["calendar_subscriptions"])
		require.EqualValues(t, 1, rows["calendar_shares"])

		status, raw := restCall(t, http.MethodDelete, "/calendar-subscriptions/"+sub.ID, token, nil)
		require.Equalf(t, http.StatusNoContent, status, "delete subscription: %s", errorMessage(raw))
		for table, n := range calendarDBRows(t, cal.ID) {
			assert.Zerof(t, n, "%s: the subscribed calendar's rows must be gone from the database", table)
		}
	})

	t.Run("address book over REST", func(t *testing.T) {
		id, uuid, _, contacts := seedAddressBook(t, "Purge REST book")
		status, raw := restCall(t, http.MethodDelete, "/addressbooks/"+uuid, token, map[string]string{"confirmation": "DELETE"})
		require.Equalf(t, http.StatusNoContent, status, "delete: %s", errorMessage(raw))
		assertAddressBookGone(t, id, uuid, contacts)
	})

	t.Run("address book over CardDAV", func(t *testing.T) {
		id, uuid, path, contacts := seedAddressBook(t, "Purge DAV book")
		status, _, body := davCall(t, "DELETE", "/dav/"+username+"/addressbooks/"+path+"/", ownerEmail, appPass, "", nil)
		require.Containsf(t, []int{http.StatusOK, http.StatusNoContent}, status, "DAV DELETE: %s", body)
		assertAddressBookGone(t, id, uuid, contacts)
	})

	t.Run("address book over MCP", func(t *testing.T) {
		id, uuid, _, contacts := seedAddressBook(t, "Purge MCP book")
		deleted, isErr := mcpTool(t, mcpCredential, "delete_address_book", map[string]any{"address_book_id": uuid, "confirmation": "DELETE"})
		require.False(t, isErr, "delete_address_book failed: %v", deleted)
		assertAddressBookGone(t, id, uuid, contacts)
	})

	assert.Equal(t, keptCalRows, calendarDBRows(t, keptCal), "a calendar nobody deleted keeps every row")
	assert.Equal(t, keptBookRows, addressBookDBRows(t, keptBook, keptContacts), "an address book nobody deleted keeps every row")
}

// shareCollection shares a calendar or address book (path is its /shares
// route) read-only with the account behind email.
func shareCollection(t *testing.T, token, path, email string) {
	t.Helper()
	status, raw := restCall(t, http.MethodPost, path, token, map[string]string{"user_identifier": email, "permission": "read"})
	require.Equalf(t, http.StatusCreated, status, "share %s: %s", path, errorMessage(raw))
}

// calendarDBRows counts, per table, the rows of calendar id — soft-deleted
// ones included.
func calendarDBRows(t *testing.T, id uint) map[string]int64 {
	t.Helper()
	return map[string]int64{
		"calendars":              countDBRows(t, &calendar.Calendar{}, "id = ?", id),
		"calendar_objects":       countDBRows(t, &calendar.CalendarObject{}, "calendar_id = ?", id),
		"sync_change_logs":       countDBRows(t, &calendar.SyncChangeLog{}, "calendar_id = ?", id),
		"calendar_shares":        countDBRows(t, &sharing.CalendarShare{}, "calendar_id = ?", id),
		"calendar_subscriptions": countDBRows(t, &calendar.CalendarSubscription{}, "calendar_id = ?", id),
	}
}

// addressBookDBRows counts, per table, the rows of address book id and of its
// contacts — soft-deleted ones included.
func addressBookDBRows(t *testing.T, id uint, contacts []uint) map[string]int64 {
	t.Helper()
	return map[string]int64{
		"address_books":                countDBRows(t, &addressbook.AddressBook{}, "id = ?", id),
		"address_objects":              countDBRows(t, &addressbook.AddressObject{}, "address_book_id = ?", id),
		"contact_photos":               countDBRows(t, &addressbook.ContactPhoto{}, "address_object_id IN ?", contacts),
		"addressbook_sync_change_logs": countDBRows(t, &addressbook.SyncChangeLog{}, "address_book_id = ?", id),
		"addressbook_shares":           countDBRows(t, &sharing.AddressBookShare{}, "address_book_id = ?", id),
	}
}

func countDBRows(t *testing.T, model any, where string, args ...any) int64 {
	t.Helper()
	var n int64
	require.NoError(t, testDB.Unscoped().Model(model).Where(where, args...).Count(&n).Error)
	return n
}

// requireRows fails the test unless every table holds rows: a delete test
// whose seed left a table empty would pass without proving anything.
func requireRows(t *testing.T, rows map[string]int64) {
	t.Helper()
	for table, n := range rows {
		if table == "calendar_subscriptions" {
			continue // only a subscribed calendar has one
		}
		require.NotZerof(t, n, "seed: %s must hold rows of the collection", table)
	}
}
