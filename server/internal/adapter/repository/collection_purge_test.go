package repository_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/sharing"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Deleting a calendar or an address book used to soft-delete only the
// collection row; its events/contacts, photos, change log, shares and
// subscription stayed in the database with deleted_at NULL until the whole
// account was deleted. These tests count rows — soft-deleted ones included —
// so "gone from the API" is not mistaken for "gone".

// countRows counts every row (Unscoped) of model matching where.
func countRows(t *testing.T, db *gorm.DB, model any, where string, args ...any) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.Unscoped().Model(model).Where(where, args...).Count(&n).Error)
	return n
}

// calendarFixture is one calendar with something in every table that hangs
// off it.
type calendarFixture struct {
	cal   *calendar.Calendar
	token string
}

// seedCalendar gives owner a calendar holding two events (one already
// soft-deleted, as an event DELETE leaves it), change-log rows, a public link,
// a subscription and a share with every user in sharees.
func seedFullCalendar(t *testing.T, db *gorm.DB, owner *user.User, name string, subscribed bool, sharees ...*user.User) calendarFixture {
	t.Helper()
	token := "pub-" + name
	cal := &calendar.Calendar{
		UUID: "cal-" + name, UserID: owner.ID, Path: "cal-" + name, Name: name,
		Color: "#123456", Timezone: "UTC", SupportedComponents: "VEVENT",
		PublicToken: &token, PublicEnabled: true, Subscribed: subscribed,
	}
	require.NoError(t, db.Create(cal).Error)
	for i, deleted := range []bool{false, true} {
		obj := &calendar.CalendarObject{
			UUID: fmt.Sprintf("obj-%s-%d", name, i), CalendarID: cal.ID, Path: fmt.Sprintf("e%d.ics", i),
			UID: fmt.Sprintf("e%d@%s", i, name), ETag: "1", ComponentType: "VEVENT",
			ICalData: "BEGIN:VCALENDAR\r\nEND:VCALENDAR\r\n",
		}
		require.NoError(t, db.Create(obj).Error)
		if deleted {
			require.NoError(t, db.Delete(obj).Error)
		}
	}
	for _, change := range []string{"collection", "created", "deleted"} {
		require.NoError(t, db.Create(&calendar.SyncChangeLog{CalendarID: cal.ID, ResourcePath: "e.ics", ChangeType: change,
			SyncToken: "t-" + name + "-" + change}).Error)
	}
	require.NoError(t, db.Create(&calendar.CalendarSubscription{
		UUID: "sub-" + name, CalendarID: cal.ID, UserID: owner.ID, URL: "https://feed.example/" + name,
		RefreshInterval: time.Hour, NextSyncAt: time.Now(), Enabled: true,
	}).Error)
	for _, sharee := range sharees {
		require.NoError(t, db.Create(&sharing.CalendarShare{UUID: "cs-" + name + "-" + sharee.Username, CalendarID: cal.ID,
			SharedWithID: sharee.ID, Permission: "read-write"}).Error)
	}
	return calendarFixture{cal: cal, token: token}
}

// calendarRows counts, per table, the rows that belong to calendar id.
func calendarRows(t *testing.T, db *gorm.DB, id uint) map[string]int64 {
	t.Helper()
	return map[string]int64{
		"calendars":              countRows(t, db, &calendar.Calendar{}, "id = ?", id),
		"calendar_objects":       countRows(t, db, &calendar.CalendarObject{}, "calendar_id = ?", id),
		"sync_change_logs":       countRows(t, db, &calendar.SyncChangeLog{}, "calendar_id = ?", id),
		"calendar_subscriptions": countRows(t, db, &calendar.CalendarSubscription{}, "calendar_id = ?", id),
		"calendar_shares":        countRows(t, db, &sharing.CalendarShare{}, "calendar_id = ?", id),
	}
}

// bookFixture is one address book with something in every table that hangs
// off it, plus the ids of its contacts (photos are keyed on those, so they
// must be counted by id once the contacts are gone).
type bookFixture struct {
	book     *addressbook.AddressBook
	contacts []uint
}

// seedFullAddressBook gives owner an address book holding two contacts with a
// photo each (one contact already soft-deleted), change-log rows and a share
// with every user in sharees.
func seedFullAddressBook(t *testing.T, db *gorm.DB, owner *user.User, name string, sharees ...*user.User) bookFixture {
	t.Helper()
	book := &addressbook.AddressBook{UUID: "ab-" + name, UserID: owner.ID, Path: "ab-" + name, Name: name, SyncToken: "s", CTag: "c"}
	require.NoError(t, db.Create(book).Error)
	f := bookFixture{book: book}
	for i, deleted := range []bool{false, true} {
		contact := &addressbook.AddressObject{
			UUID: fmt.Sprintf("con-%s-%d", name, i), AddressBookID: book.ID, Path: fmt.Sprintf("c%d.vcf", i),
			UID: fmt.Sprintf("c%d@%s", i, name), VCardData: "BEGIN:VCARD\r\nFN:X\r\nEND:VCARD\r\n", FormattedName: "X",
		}
		require.NoError(t, db.Create(contact).Error)
		require.NoError(t, db.Create(&addressbook.ContactPhoto{AddressObjectID: contact.ID, PhotoData: "AA==", PhotoType: "JPEG"}).Error)
		if deleted {
			require.NoError(t, db.Delete(contact).Error)
		}
		f.contacts = append(f.contacts, contact.ID)
	}
	for _, change := range []string{"collection", "created"} {
		require.NoError(t, db.Create(&addressbook.SyncChangeLog{AddressBookID: book.ID, ResourcePath: "c.vcf", ChangeType: change,
			SyncToken: "t-" + name + "-" + change}).Error)
	}
	for _, sharee := range sharees {
		require.NoError(t, db.Create(&sharing.AddressBookShare{UUID: "as-" + name + "-" + sharee.Username, AddressBookID: book.ID,
			SharedWithID: sharee.ID, Permission: "read"}).Error)
	}
	return f
}

// bookRows counts, per table, the rows that belong to the fixture's book.
func bookRows(t *testing.T, db *gorm.DB, f bookFixture) map[string]int64 {
	t.Helper()
	id := f.book.ID
	return map[string]int64{
		"address_books":                countRows(t, db, &addressbook.AddressBook{}, "id = ?", id),
		"address_objects":              countRows(t, db, &addressbook.AddressObject{}, "address_book_id = ?", id),
		"contact_photos":               countRows(t, db, &addressbook.ContactPhoto{}, "address_object_id IN ?", f.contacts),
		"addressbook_sync_change_logs": countRows(t, db, &addressbook.SyncChangeLog{}, "address_book_id = ?", id),
		"addressbook_shares":           countRows(t, db, &sharing.AddressBookShare{}, "address_book_id = ?", id),
	}
}

func requireAllSeeded(t *testing.T, rows map[string]int64) {
	t.Helper()
	for table, n := range rows {
		require.NotZerof(t, n, "seed: %s must hold rows of the collection", table)
	}
}

func mkUser(t *testing.T, db *gorm.DB, name string) *user.User {
	t.Helper()
	u := &user.User{UUID: "u-" + name, Email: name + "@example.test", Username: name, PasswordHash: "x", IsActive: true}
	require.NoError(t, db.Create(u).Error)
	return u
}

// Deleting a calendar — including a subscribed one shared with two other
// users — removes every row that belongs to it, and nothing else: not the
// owner's other calendar, not another user's calendar, not the shares the
// owner was granted on someone else's calendar.
func TestCalendarRepoDelete_PurgesContents(t *testing.T) {
	db := openAccountDB(t)
	ctx := context.Background()
	owner, bob, carol := mkUser(t, db, "owner"), mkUser(t, db, "bob"), mkUser(t, db, "carol")

	doomed := seedFullCalendar(t, db, owner, "doomed", true, bob, carol)
	sibling := seedFullCalendar(t, db, owner, "sibling", false, bob)
	foreign := seedFullCalendar(t, db, bob, "foreign", false, owner)

	requireAllSeeded(t, calendarRows(t, db, doomed.cal.ID))
	siblingBefore := calendarRows(t, db, sibling.cal.ID)
	foreignBefore := calendarRows(t, db, foreign.cal.ID)

	require.NoError(t, repository.NewCalendarRepository(db).Delete(ctx, doomed.cal.ID))

	for table, n := range calendarRows(t, db, doomed.cal.ID) {
		assert.Zerof(t, n, "%s: every row of the deleted calendar must be hard-deleted", table)
	}
	assert.Zero(t, countRows(t, db, &calendar.Calendar{}, "public_token = ?", doomed.token), "the public link dies with the calendar")
	assert.Equal(t, siblingBefore, calendarRows(t, db, sibling.cal.ID), "the owner's other calendar is untouched")
	assert.Equal(t, foreignBefore, calendarRows(t, db, foreign.cal.ID), "another user's calendar (shared with the owner) is untouched")

	// The id is not handed out again (SQLite AUTOINCREMENT): a new calendar
	// must not inherit anything a stale reference still points at.
	next := &calendar.Calendar{UUID: "cal-next", UserID: owner.ID, Path: "cal-doomed", Name: "Next", Color: "#123456",
		Timezone: "UTC", SupportedComponents: "VEVENT"}
	require.NoError(t, db.Create(next).Error)
	assert.Greater(t, next.ID, foreign.cal.ID, "a deleted calendar's id must not be reused")
}

// Deleting an address book shared with two users removes its contacts, their
// photos (also of an already soft-deleted contact), its change log and its
// shares — and nothing of any other book.
func TestAddressBookRepoDelete_PurgesContents(t *testing.T) {
	db := openAccountDB(t)
	ctx := context.Background()
	owner, bob, carol := mkUser(t, db, "owner"), mkUser(t, db, "bob"), mkUser(t, db, "carol")

	doomed := seedFullAddressBook(t, db, owner, "doomed", bob, carol)
	sibling := seedFullAddressBook(t, db, owner, "sibling", bob)
	foreign := seedFullAddressBook(t, db, bob, "foreign", owner)

	requireAllSeeded(t, bookRows(t, db, doomed))
	siblingBefore := bookRows(t, db, sibling)
	foreignBefore := bookRows(t, db, foreign)

	require.NoError(t, repository.NewAddressBookRepository(db).Delete(ctx, doomed.book.ID))

	for table, n := range bookRows(t, db, doomed) {
		assert.Zerof(t, n, "%s: every row of the deleted address book must be hard-deleted", table)
	}
	assert.Equal(t, siblingBefore, bookRows(t, db, sibling), "the owner's other address book is untouched")
	assert.Equal(t, foreignBefore, bookRows(t, db, foreign), "another user's address book (shared with the owner) is untouched")
}

// The cascade is one transaction: when its last statement fails, everything
// it already deleted comes back and the collection is not even soft-deleted.
func TestCollectionDeleteRollsBackOnFailure(t *testing.T) {
	db := openAccountDB(t)
	ctx := context.Background()
	owner, bob := mkUser(t, db, "owner"), mkUser(t, db, "bob")
	cal := seedFullCalendar(t, db, owner, "kept", true, bob)
	book := seedFullAddressBook(t, db, owner, "kept", bob)
	calBefore, bookBefore := calendarRows(t, db, cal.cal.ID), bookRows(t, db, book)

	// The collection row goes last, so failing it fails the cascade after all
	// of its contents were already deleted.
	for _, table := range []string{"calendars", "address_books"} {
		require.NoError(t, db.Exec("CREATE TRIGGER block_"+table+" BEFORE DELETE ON "+table+
			" BEGIN SELECT RAISE(ABORT, 'injected failure'); END").Error)
	}

	require.ErrorContains(t, repository.NewCalendarRepository(db).Delete(ctx, cal.cal.ID), "injected failure")
	require.ErrorContains(t, repository.NewAddressBookRepository(db).Delete(ctx, book.book.ID), "injected failure")

	assert.Equal(t, calBefore, calendarRows(t, db, cal.cal.ID), "a failed calendar delete must leave every row in place")
	assert.Equal(t, bookBefore, bookRows(t, db, book), "a failed address book delete must leave every row in place")
	assert.EqualValues(t, 1, countRows(t, db, &calendar.Calendar{}, "id = ? AND deleted_at IS NULL", cal.cal.ID),
		"the calendar must still be live, not soft-deleted")
	assert.EqualValues(t, 1, countRows(t, db, &addressbook.AddressBook{}, "id = ? AND deleted_at IS NULL", book.book.ID),
		"the address book must still be live, not soft-deleted")
}

// PurgeDeletedCollections (the data migration's cascade) removes what older
// deletes left behind — soft-deleted collections with their contents, and
// contents whose collection row is missing altogether — reports how much, and
// never touches a live collection, even one sitting next to a deleted one.
func TestPurgeDeletedCollections(t *testing.T) {
	db := openAccountDB(t)
	owner, bob := mkUser(t, db, "owner"), mkUser(t, db, "bob")

	// Deleted the old way: only the collection row was soft-deleted.
	oldCal := seedFullCalendar(t, db, owner, "old", true, bob)
	require.NoError(t, db.Delete(oldCal.cal).Error)
	oldBook := seedFullAddressBook(t, db, owner, "old", bob)
	require.NoError(t, db.Delete(oldBook.book).Error)
	// Contents whose collection row is gone entirely (calendar_objects and
	// both change logs have no foreign key that would have stopped this).
	const missing = 9999
	require.NoError(t, db.Create(&calendar.CalendarObject{UUID: "obj-orphan", CalendarID: missing, Path: "o.ics", UID: "o",
		ETag: "1", ComponentType: "VEVENT", ICalData: "BEGIN:VCALENDAR\r\nEND:VCALENDAR\r\n"}).Error)
	require.NoError(t, db.Create(&calendar.SyncChangeLog{CalendarID: missing, ResourcePath: "o.ics", ChangeType: "created", SyncToken: "t-orphan"}).Error)
	require.NoError(t, db.Create(&addressbook.SyncChangeLog{AddressBookID: missing, ResourcePath: "o.vcf", ChangeType: "created", SyncToken: "t-orphan"}).Error)
	require.NoError(t, db.Create(&addressbook.ContactPhoto{AddressObjectID: missing, PhotoData: "AA==", PhotoType: "JPEG"}).Error)

	// Live collections, including soft-deleted objects inside them (an event
	// or contact DELETE leaves those): none of it may be touched.
	liveCal := seedFullCalendar(t, db, owner, "live", false, bob)
	liveBook := seedFullAddressBook(t, db, owner, "live", bob)
	liveCalBefore, liveBookBefore := calendarRows(t, db, liveCal.cal.ID), bookRows(t, db, liveBook)
	requireAllSeeded(t, liveCalBefore)
	requireAllSeeded(t, liveBookBefore)

	var counts map[string]int64
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		var err error
		counts, err = repository.PurgeDeletedCollections(tx)
		return err
	}))

	for table, n := range calendarRows(t, db, oldCal.cal.ID) {
		assert.Zerof(t, n, "%s: the soft-deleted calendar and its contents must be purged", table)
	}
	for table, n := range bookRows(t, db, oldBook) {
		assert.Zerof(t, n, "%s: the soft-deleted address book and its contents must be purged", table)
	}
	assert.Zero(t, countRows(t, db, &calendar.CalendarObject{}, "calendar_id = ?", missing))
	assert.Zero(t, countRows(t, db, &calendar.SyncChangeLog{}, "calendar_id = ?", missing))
	assert.Zero(t, countRows(t, db, &addressbook.SyncChangeLog{}, "address_book_id = ?", missing))
	assert.Zero(t, countRows(t, db, &addressbook.ContactPhoto{}, "address_object_id = ?", missing))

	assert.Equal(t, liveCalBefore, calendarRows(t, db, liveCal.cal.ID), "a live calendar keeps everything")
	assert.Equal(t, liveBookBefore, bookRows(t, db, liveBook), "a live address book keeps everything")

	assert.Equal(t, map[string]int64{
		"calendar_shares":              1,
		"calendar_subscriptions":       1,
		"calendar_objects":             3, // two of the old calendar, one orphan
		"sync_change_logs":             4, // three of the old calendar, one orphan
		"calendars":                    1,
		"addressbook_shares":           1,
		"contact_photos":               3, // two of the old book, one orphan
		"address_objects":              2,
		"addressbook_sync_change_logs": 3, // two of the old book, one orphan
		"address_books":                1,
	}, counts, "the counts the migration logs")

	// Idempotent: a second run finds nothing.
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		var err error
		counts, err = repository.PurgeDeletedCollections(tx)
		return err
	}))
	assert.Empty(t, counts, "a second run must be a no-op")
	assert.Equal(t, liveCalBefore, calendarRows(t, db, liveCal.cal.ID))
	assert.Equal(t, liveBookBefore, bookRows(t, db, liveBook))
}
