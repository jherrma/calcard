package webdav

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/sharing"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// TestDAVCollectionDeletePurgesContents: a DAV client's DELETE on a calendar
// or address book collection removes the collection's contents from the
// database — events, change log and shares; contacts, their photos, change log
// and shares — not just the collection row. A client that had synced the
// collection gets a 404 for its old sync token afterwards, and a collection
// created again at the same URL starts out empty.
func TestDAVCollectionDeletePurgesContents(t *testing.T) {
	app, db, _ := setupTestApp(t)
	defer db.Close()
	ctx := context.Background()
	g := db.DB()

	userRepo := repository.NewUserRepository(g)
	calRepo := repository.NewCalendarRepository(g)
	abRepo := repository.NewAddressBookRepository(g)
	calShares := repository.NewCalendarShareRepository(g)
	abShares := repository.NewAddressBookShareRepository(g)

	hash, _ := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	owner := &user.User{UUID: "owner", Email: "owner@example.com", Username: "owner", PasswordHash: string(hash), IsActive: true}
	sharee := &user.User{UUID: "sharee", Email: "sharee@example.com", Username: "sharee", PasswordHash: string(hash), IsActive: true}
	require.NoError(t, userRepo.Create(ctx, owner))
	require.NoError(t, userRepo.Create(ctx, sharee))
	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte("owner@example.com:password"))

	do := func(method, path, depth, ctype, body string) (int, string) {
		t.Helper()
		req, _ := http.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", auth)
		if depth != "" {
			req.Header.Set("Depth", depth)
		}
		if ctype != "" {
			req.Header.Set("Content-Type", ctype)
		}
		resp := davDo(t, app, req)
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	const syncBody = `<?xml version="1.0" encoding="utf-8" ?>
<D:sync-collection xmlns:D="DAV:"><D:sync-token>%s</D:sync-token><D:sync-level>1</D:sync-level><D:prop><D:getetag/></D:prop></D:sync-collection>`
	syncToken := regexp.MustCompile(`<[^>]*sync-token>([^<]+)<`)
	sync := func(path, token string) (int, string) {
		t.Helper()
		return do("REPORT", path, "1", "application/xml", strings.Replace(syncBody, "%s", token, 1))
	}
	count := func(model any, where string, args ...any) int64 {
		t.Helper()
		var n int64
		require.NoError(t, g.Unscoped().Model(model).Where(where, args...).Count(&n).Error)
		return n
	}

	t.Run("calendar", func(t *testing.T) {
		mkCal := func(path string) *calendar.Calendar {
			cal := &calendar.Calendar{UserID: owner.ID, UUID: "cal-" + path, Path: path, Name: path, Timezone: "UTC",
				Color: "#336699", SupportedComponents: "VEVENT,VTODO"}
			require.NoError(t, calRepo.Create(ctx, cal))
			ical := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//T//EN\r\nBEGIN:VEVENT\r\nUID:ev@" + path +
				"\r\nDTSTAMP:20260101T000000Z\r\nDTSTART:20300301T100000Z\r\nDTEND:20300301T110000Z\r\nSUMMARY:Ev\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
			status, body := do("PUT", "/dav/owner/calendars/"+path+"/ev.ics", "", "text/calendar", ical)
			require.Equal(t, http.StatusCreated, status, body)
			require.NoError(t, calShares.Create(ctx, &sharing.CalendarShare{UUID: "cs-" + path, CalendarID: cal.ID,
				SharedWithID: sharee.ID, Permission: "read"}))
			return cal
		}
		doomed, kept := mkCal("doomed"), mkCal("kept")
		rows := func(id uint) map[string]int64 {
			return map[string]int64{
				"calendars":        count(&calendar.Calendar{}, "id = ?", id),
				"calendar_objects": count(&calendar.CalendarObject{}, "calendar_id = ?", id),
				"sync_change_logs": count(&calendar.SyncChangeLog{}, "calendar_id = ?", id),
				"calendar_shares":  count(&sharing.CalendarShare{}, "calendar_id = ?", id),
			}
		}
		keptBefore := rows(kept.ID)

		// A client syncs the collection and keeps the token.
		status, body := sync("/dav/owner/calendars/doomed/", "")
		require.Equal(t, http.StatusMultiStatus, status, body)
		m := syncToken.FindStringSubmatch(body)
		require.Len(t, m, 2, "the initial sync must hand out a token: %s", body)
		oldToken := m[1]

		status, body = do("DELETE", "/dav/owner/calendars/doomed/", "", "", "")
		require.Contains(t, []int{http.StatusOK, http.StatusNoContent}, status, body)

		for table, n := range rows(doomed.ID) {
			assert.Zerof(t, n, "%s: the DAV-deleted calendar's rows must be hard-deleted", table)
		}
		assert.Equal(t, keptBefore, rows(kept.ID), "the other calendar is untouched")

		status, _ = sync("/dav/owner/calendars/doomed/", oldToken)
		assert.Equal(t, http.StatusNotFound, status, "a client syncing the deleted collection gets a 404")

		// Created again at the same URL, the collection is empty.
		status, body = do("MKCALENDAR", "/dav/owner/calendars/doomed/", "", "", "")
		require.Equal(t, http.StatusCreated, status, body)
		status, body = do("PROPFIND", "/dav/owner/calendars/doomed/", "1", "application/xml",
			`<?xml version="1.0"?><D:propfind xmlns:D="DAV:"><D:prop><D:getetag/></D:prop></D:propfind>`)
		require.Equal(t, http.StatusMultiStatus, status, body)
		assert.NotContains(t, body, "ev.ics", "the old event must not reappear in a new collection at the same URL")
		status, _ = sync("/dav/owner/calendars/doomed/", oldToken)
		assert.Equal(t, http.StatusForbidden, status, "the old token means nothing to the new collection (valid-sync-token)")
	})

	t.Run("address book", func(t *testing.T) {
		mkBook := func(path string) (*addressbook.AddressBook, uint) {
			ab := &addressbook.AddressBook{UUID: "ab-" + path, UserID: owner.ID, Path: path, Name: path}
			require.NoError(t, abRepo.Create(ctx, ab))
			vcard := "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:c@" + path + "\r\nFN:Pic " + path + "\r\nN:Pic;" + path +
				";;;\r\nPHOTO;ENCODING=b;TYPE=JPEG:AAAA\r\nEND:VCARD\r\n"
			status, body := do("PUT", "/dav/owner/addressbooks/"+path+"/c.vcf", "", "text/vcard", vcard)
			require.Equal(t, http.StatusCreated, status, body)
			require.NoError(t, abShares.Create(ctx, &sharing.AddressBookShare{UUID: "as-" + path, AddressBookID: ab.ID,
				SharedWithID: sharee.ID, Permission: "read"}))
			var contact addressbook.AddressObject
			require.NoError(t, g.Where("address_book_id = ?", ab.ID).First(&contact).Error)
			return ab, contact.ID
		}
		doomed, doomedContact := mkBook("doomed")
		kept, keptContact := mkBook("kept")
		rows := func(id, contact uint) map[string]int64 {
			return map[string]int64{
				"address_books":                count(&addressbook.AddressBook{}, "id = ?", id),
				"address_objects":              count(&addressbook.AddressObject{}, "address_book_id = ?", id),
				"contact_photos":               count(&addressbook.ContactPhoto{}, "address_object_id = ?", contact),
				"addressbook_sync_change_logs": count(&addressbook.SyncChangeLog{}, "address_book_id = ?", id),
				"addressbook_shares":           count(&sharing.AddressBookShare{}, "address_book_id = ?", id),
			}
		}
		for table, n := range rows(doomed.ID, doomedContact) {
			require.NotZerof(t, n, "seed: %s", table)
		}
		keptBefore := rows(kept.ID, keptContact)

		status, body := sync("/dav/owner/addressbooks/doomed/", "")
		require.Equal(t, http.StatusMultiStatus, status, body)
		m := syncToken.FindStringSubmatch(body)
		require.Len(t, m, 2, "the initial sync must hand out a token: %s", body)
		oldToken := m[1]

		status, body = do("DELETE", "/dav/owner/addressbooks/doomed/", "", "", "")
		require.Contains(t, []int{http.StatusOK, http.StatusNoContent}, status, body)

		for table, n := range rows(doomed.ID, doomedContact) {
			assert.Zerof(t, n, "%s: the DAV-deleted address book's rows must be hard-deleted", table)
		}
		assert.Equal(t, keptBefore, rows(kept.ID, keptContact), "the other address book is untouched")

		status, _ = sync("/dav/owner/addressbooks/doomed/", oldToken)
		assert.Equal(t, http.StatusNotFound, status, "a client syncing the deleted collection gets a 404")
	})

	// The sharee keeps exactly the grants on the collections that still exist.
	assert.EqualValues(t, 1, count(&sharing.CalendarShare{}, "shared_with_id = ?", sharee.ID))
	assert.EqualValues(t, 1, count(&sharing.AddressBookShare{}, "shared_with_id = ?", sharee.ID))
}
