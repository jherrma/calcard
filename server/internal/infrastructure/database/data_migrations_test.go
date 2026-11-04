package database

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/domain"
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/sharing"
	"github.com/jherrma/caldav-server/internal/domain/user"
	calendaruc "github.com/jherrma/caldav-server/internal/usecase/calendar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRunDataMigrations(t *testing.T) {
	dataDir, err := os.MkdirTemp("", "caldav-datamig-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dataDir)

	cfg := &config.Config{
		DataDir:  dataDir,
		Database: config.DatabaseConfig{Driver: "sqlite", AutoMigrate: true},
	}
	db, err := New(cfg)
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, db.Migrate(Models()...))
	g := db.DB()

	// The objects live in a live calendar: an object without one is an orphan
	// that purgeDeletedCollections removes.
	require.NoError(t, g.Create(&user.User{ID: 1, UUID: "u-1", Username: "u1", Email: "u1@x", PasswordHash: "x"}).Error)
	require.NoError(t, g.Create(&calendar.Calendar{ID: 1, UUID: "cal-1", UserID: 1, Path: "c1", Name: "C1",
		Color: "#ffffff", Timezone: "UTC", SupportedComponents: "VEVENT"}).Error)

	// Row with a quoted ETag and bare (unwrapped) VEVENT data.
	bare := &calendar.CalendarObject{
		UUID: "u-bare", CalendarID: 1, Path: "bare.ics", UID: "bare@x",
		ETag:     `"abc123"`,
		ICalData: "BEGIN:VEVENT\r\nUID:bare@x\r\nDTSTART:20260101T100000Z\r\nDTEND:20260101T110000Z\r\nEND:VEVENT\r\n",
	}
	require.NoError(t, g.Create(bare).Error)

	// Wrapped recurring row with NULL recurrence_end_time. It is ALREADY
	// well-formed (single VCALENDAR wrapper) and carries a legitimately UNQUOTED
	// ETag, so the migration must not double-wrap its body nor strip its ETag.
	recICal := wrap("BEGIN:VEVENT\r\nUID:rec@x\r\nDTSTART:20260101T100000Z\r\nDTEND:20260101T110000Z\r\nRRULE:FREQ=DAILY;COUNT=3\r\nEND:VEVENT")
	rec := &calendar.CalendarObject{
		UUID: "u-rec", CalendarID: 1, Path: "rec.ics", UID: "rec@x",
		ETag:     "plain",
		ICalData: recICal,
	}
	require.NoError(t, g.Create(rec).Error)

	run := func() {
		require.NoError(t, RunDataMigrations(g))
	}
	run()
	// Second run must be a no-op (idempotent).
	run()

	var got calendar.CalendarObject
	require.NoError(t, g.Where("uuid = ?", "u-bare").First(&got).Error)
	if strings.Contains(got.ETag, `"`) {
		t.Errorf("etag still quoted: %q", got.ETag)
	}
	if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(got.ICalData)), "BEGIN:VCALENDAR") {
		t.Errorf("ical_data not wrapped: %q", got.ICalData)
	}

	var gotRec calendar.CalendarObject
	require.NoError(t, g.Where("uuid = ?", "u-rec").First(&gotRec).Error)
	if gotRec.RecurrenceEndTime == nil {
		t.Error("recurrence_end_time was not backfilled")
	}
	// Already-wrapped body must not be re-wrapped (exactly one VCALENDAR) and
	// must be byte-for-byte unchanged.
	if n := strings.Count(strings.ToUpper(gotRec.ICalData), "BEGIN:VCALENDAR"); n != 1 {
		t.Errorf("already-wrapped row was re-wrapped: found %d BEGIN:VCALENDAR", n)
	}
	if gotRec.ICalData != recICal {
		t.Errorf("already-wrapped ICalData must be unchanged by migration:\n got %q\nwant %q", gotRec.ICalData, recICal)
	}
	// A legitimately-unquoted ETag must survive the blanket quote-strip untouched.
	if gotRec.ETag != "plain" {
		t.Errorf("unquoted ETag must be preserved, got %q", gotRec.ETag)
	}
}

func wrap(body string) string {
	return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//t//EN\r\n" + body + "\r\nEND:VCALENDAR\r\n"
}

// TestBackfillCollectionAnchors reproduces the pre-existing (legacy) collection
// that carries a sync_token with no matching "collection" changelog row — the
// state of any install upgraded from before the anchor row was minted at
// Create() time. The migration must insert exactly one anchor row per such
// collection, and be a no-op on re-run.
func TestBackfillCollectionAnchors(t *testing.T) {
	dataDir, err := os.MkdirTemp("", "caldav-anchor-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dataDir)

	cfg := &config.Config{
		DataDir:  dataDir,
		Database: config.DatabaseConfig{Driver: "sqlite", AutoMigrate: true},
	}
	db, err := New(cfg)
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, db.Migrate(Models()...))
	g := db.DB()

	// Owner for the collections (calendars/address_books have a user FK).
	require.NoError(t, g.Create(&user.User{
		ID: 1, UUID: "u-1", Email: "owner@x", PasswordHash: "x",
	}).Error)

	// Legacy calendar: stored sync_token, but no changelog row at all.
	cal := &calendar.Calendar{
		UUID: "cal-legacy", UserID: 1, Path: "legacy", Name: "Legacy",
		Color: "#ffffff", Timezone: "UTC", SupportedComponents: "VEVENT",
		SyncToken: "cal-token-legacy", CTag: "cal-token-legacy",
	}
	require.NoError(t, g.Create(cal).Error)

	// Legacy address book: same shape.
	ab := &addressbook.AddressBook{
		UUID: "ab-legacy", UserID: 1, Path: "legacy", Name: "Legacy",
		SyncToken: "ab-token-legacy", CTag: "ab-token-legacy",
	}
	require.NoError(t, g.Create(ab).Error)

	// A calendar with an empty sync_token must never get an anchor.
	calEmpty := &calendar.Calendar{
		UUID: "cal-empty", UserID: 1, Path: "empty", Name: "Empty",
		Color: "#ffffff", Timezone: "UTC", SupportedComponents: "VEVENT",
		SyncToken: "", CTag: "",
	}
	require.NoError(t, g.Create(calEmpty).Error)

	run := func() {
		require.NoError(t, RunDataMigrations(g))
	}
	run()
	// Second run must be a no-op (idempotent) — no duplicate anchors.
	run()

	var calAnchors []calendar.SyncChangeLog
	require.NoError(t, g.Where("calendar_id = ? AND sync_token = ?", cal.ID, cal.SyncToken).Find(&calAnchors).Error)
	require.Len(t, calAnchors, 1, "expected exactly one calendar anchor row")
	require.Equal(t, "collection", calAnchors[0].ChangeType)

	var abAnchors []addressbook.SyncChangeLog
	require.NoError(t, g.Where("address_book_id = ? AND sync_token = ?", ab.ID, ab.SyncToken).Find(&abAnchors).Error)
	require.Len(t, abAnchors, 1, "expected exactly one address book anchor row")
	require.Equal(t, "collection", abAnchors[0].ChangeType)

	var emptyCount int64
	require.NoError(t, g.Model(&calendar.SyncChangeLog{}).Where("calendar_id = ?", calEmpty.ID).Count(&emptyCount).Error)
	require.Zero(t, emptyCount, "empty-token calendar must not get an anchor")
}

// TestPurgeSoftDeletedShares reproduces a tombstone left behind by the old
// soft-delete revoke path: a share row with a non-NULL deleted_at that still
// occupies its (collection_id, shared_with_id) unique-index slot. The migration
// must hard-delete such rows so the pair can be re-shared, while leaving live
// (non-deleted) shares untouched. Idempotent on re-run.
func TestPurgeSoftDeletedShares(t *testing.T) {
	dataDir, err := os.MkdirTemp("", "caldav-share-purge-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dataDir)

	cfg := &config.Config{
		DataDir:  dataDir,
		Database: config.DatabaseConfig{Driver: "sqlite", AutoMigrate: true},
	}
	db, err := New(cfg)
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, db.Migrate(Models()...))
	g := db.DB()

	// Referenced rows so the share foreign keys (calendar_id, addressbook_id,
	// shared_with_id) resolve under SQLite FK enforcement.
	require.NoError(t, g.Create(&user.User{ID: 1, UUID: "u-owner", Username: "owner", Email: "owner@x", PasswordHash: "x"}).Error)
	require.NoError(t, g.Create(&user.User{ID: 2, UUID: "u-2", Username: "u2", Email: "u2@x", PasswordHash: "x"}).Error)
	require.NoError(t, g.Create(&user.User{ID: 3, UUID: "u-3", Username: "u3", Email: "u3@x", PasswordHash: "x"}).Error)
	require.NoError(t, g.Create(&calendar.Calendar{
		ID: 1, UUID: "cal-1", UserID: 1, Path: "c1", Name: "C1",
		Color: "#ffffff", Timezone: "UTC", SupportedComponents: "VEVENT",
	}).Error)
	require.NoError(t, g.Create(&addressbook.AddressBook{
		ID: 1, UUID: "ab-1", UserID: 1, Path: "ab1", Name: "AB1",
	}).Error)

	// Soft-deleted calendar share (tombstone): GORM's Delete on a model with a
	// gorm.DeletedAt field sets deleted_at rather than removing the row.
	calTomb := &sharing.CalendarShare{
		UUID: "cs-tomb", CalendarID: 1, SharedWithID: 2, Permission: "read",
	}
	require.NoError(t, g.Create(calTomb).Error)
	require.NoError(t, g.Delete(calTomb).Error)

	// Live calendar share for a different pair — must survive.
	calLive := &sharing.CalendarShare{
		UUID: "cs-live", CalendarID: 1, SharedWithID: 3, Permission: "read-write",
	}
	require.NoError(t, g.Create(calLive).Error)

	// Soft-deleted address book share (tombstone).
	abTomb := &sharing.AddressBookShare{
		UUID: "abs-tomb", AddressBookID: 1, SharedWithID: 2, Permission: "read",
	}
	require.NoError(t, g.Create(abTomb).Error)
	require.NoError(t, g.Delete(abTomb).Error)

	// Sanity: the tombstone is present pre-migration (Unscoped sees it).
	var preCount int64
	require.NoError(t, g.Unscoped().Model(&sharing.CalendarShare{}).
		Where("uuid = ?", "cs-tomb").Count(&preCount).Error)
	require.EqualValues(t, 1, preCount, "tombstone must exist before migration")

	run := func() {
		require.NoError(t, RunDataMigrations(g))
	}
	run()
	// Second run must be a no-op (idempotent).
	run()

	// Tombstones are hard-deleted — invisible even to an Unscoped query.
	var calTombCount int64
	require.NoError(t, g.Unscoped().Model(&sharing.CalendarShare{}).
		Where("uuid = ?", "cs-tomb").Count(&calTombCount).Error)
	require.Zero(t, calTombCount, "soft-deleted calendar share must be purged")

	var abTombCount int64
	require.NoError(t, g.Unscoped().Model(&sharing.AddressBookShare{}).
		Where("uuid = ?", "abs-tomb").Count(&abTombCount).Error)
	require.Zero(t, abTombCount, "soft-deleted address book share must be purged")

	// Live share is untouched.
	var live sharing.CalendarShare
	require.NoError(t, g.Where("uuid = ?", "cs-live").First(&live).Error)
	require.Equal(t, "read-write", live.Permission)

	// The freed unique-index slot can be re-shared without a UNIQUE violation.
	reshare := &sharing.CalendarShare{
		UUID: "cs-reshare", CalendarID: 1, SharedWithID: 2, Permission: "read-write",
	}
	require.NoError(t, g.Create(reshare).Error, "re-sharing the purged pair must succeed")
}

// TestPurgeDeletedAccounts covers accounts deleted before account deletion
// cascaded to their data (F18): the users row is soft-deleted but the calendar,
// its public link, its events and its shares are still there. The migration
// must remove them, leave live accounts alone, and be a no-op on re-run.
func TestPurgeDeletedAccounts(t *testing.T) {
	dataDir, err := os.MkdirTemp("", "caldav-account-purge-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dataDir)

	cfg := &config.Config{
		DataDir:  dataDir,
		Database: config.DatabaseConfig{Driver: "sqlite", AutoMigrate: true},
	}
	db, err := New(cfg)
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, db.Migrate(Models()...))
	g := db.DB()

	gone := &user.User{ID: 1, UUID: "u-gone", Username: "gone", Email: "gone@x", PasswordHash: "x"}
	live := &user.User{ID: 2, UUID: "u-live", Username: "live", Email: "live@x", PasswordHash: "x"}
	require.NoError(t, g.Create(gone).Error)
	require.NoError(t, g.Create(live).Error)
	token := "pub-gone"
	for _, c := range []*calendar.Calendar{
		{ID: 1, UUID: "cal-gone", UserID: 1, Path: "c1", Name: "Gone", Color: "#ffffff", Timezone: "UTC",
			SupportedComponents: "VEVENT", PublicToken: &token, PublicEnabled: true},
		{ID: 2, UUID: "cal-live", UserID: 2, Path: "c2", Name: "Live", Color: "#ffffff", Timezone: "UTC",
			SupportedComponents: "VEVENT"},
	} {
		require.NoError(t, g.Create(c).Error)
	}
	require.NoError(t, g.Create(&calendar.CalendarObject{
		UUID: "obj-gone", CalendarID: 1, Path: "e.ics", UID: "e@gone", ETag: "1",
		ComponentType: "VEVENT", ICalData: wrap("BEGIN:VEVENT\r\nUID:e@gone\r\nEND:VEVENT\r\n"),
	}).Error)
	require.NoError(t, g.Create(&sharing.CalendarShare{UUID: "cs-out", CalendarID: 1, SharedWithID: 2, Permission: "read"}).Error)
	require.NoError(t, g.Create(&sharing.CalendarShare{UUID: "cs-in", CalendarID: 2, SharedWithID: 1, Permission: "read"}).Error)
	require.NoError(t, g.Create(&addressbook.AddressBook{ID: 1, UUID: "ab-gone", UserID: 1, Path: "ab", Name: "AB"}).Error)
	require.NoError(t, g.Create(&addressbook.AddressBook{ID: 2, UUID: "ab-live", UserID: 2, Path: "ab", Name: "AB"}).Error)

	// What DELETE /users/me did before the fix: soft-delete the user row only.
	require.NoError(t, g.Delete(gone).Error)

	run := func() { require.NoError(t, RunDataMigrations(g)) }
	run()
	run() // idempotent

	count := func(model any, where string, args ...any) int64 {
		var n int64
		require.NoError(t, g.Unscoped().Model(model).Where(where, args...).Count(&n).Error)
		return n
	}
	require.Zero(t, count(&calendar.Calendar{}, "user_id = ?", 1), "the deleted account's calendar must be purged")
	require.Zero(t, count(&calendar.Calendar{}, "public_token = ?", token), "its public link must stop resolving")
	require.Zero(t, count(&calendar.CalendarObject{}, "calendar_id = ?", 1), "its events must be purged")
	require.Zero(t, count(&sharing.CalendarShare{}, "1 = 1"), "shares from and to the deleted account must be purged")
	require.Zero(t, count(&addressbook.AddressBook{}, "user_id = ?", 1), "its address book must be purged")

	require.EqualValues(t, 1, count(&calendar.Calendar{}, "user_id = ?", 2), "a live account keeps its calendar")
	require.EqualValues(t, 1, count(&addressbook.AddressBook{}, "user_id = ?", 2), "a live account keeps its address book")
	require.EqualValues(t, 2, count(&user.User{}, "1 = 1"), "user rows themselves are not touched")
}

// TestPurgeDeletedCollections covers calendars and address books deleted
// before a collection delete removed its contents: only the collection row was
// soft-deleted, the events, contacts, photos, change log, shares and
// subscription stayed. The migration must purge them — and contents whose
// collection row is missing altogether — log what it removed, leave live
// collections (and their own soft-deleted objects) alone, and be a no-op, also
// in the log, on re-run.
func TestPurgeDeletedCollections(t *testing.T) {
	cfg := &config.Config{
		DataDir:  t.TempDir(),
		Database: config.DatabaseConfig{Driver: "sqlite", AutoMigrate: true},
	}
	db, err := New(cfg)
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, db.Migrate(Models()...))
	g := db.DB()

	owner := &user.User{ID: 1, UUID: "u-owner", Username: "owner", Email: "owner@x", PasswordHash: "x"}
	sharee := &user.User{ID: 2, UUID: "u-sharee", Username: "sharee", Email: "sharee@x", PasswordHash: "x"}
	require.NoError(t, g.Create(owner).Error)
	require.NoError(t, g.Create(sharee).Error)

	ical := wrap("BEGIN:VEVENT\r\nUID:e\r\nEND:VEVENT")
	token := "pub-old"
	seedCalendar := func(id uint, name string, publicToken *string) {
		require.NoError(t, g.Create(&calendar.Calendar{ID: id, UUID: "cal-" + name, UserID: 1, Path: name, Name: name,
			Color: "#ffffff", Timezone: "UTC", SupportedComponents: "VEVENT", PublicToken: publicToken, PublicEnabled: publicToken != nil}).Error)
		obj := &calendar.CalendarObject{UUID: "obj-" + name, CalendarID: id, Path: "e.ics", UID: "e@" + name, ETag: "1",
			ComponentType: "VEVENT", ICalData: ical}
		require.NoError(t, g.Create(obj).Error)
		gone := &calendar.CalendarObject{UUID: "obj-gone-" + name, CalendarID: id, Path: "g.ics", UID: "g@" + name, ETag: "1",
			ComponentType: "VEVENT", ICalData: ical}
		require.NoError(t, g.Create(gone).Error)
		require.NoError(t, g.Delete(gone).Error) // an event DELETE soft-deletes the object
		require.NoError(t, g.Create(&calendar.SyncChangeLog{CalendarID: id, ResourcePath: "e.ics", ChangeType: "created", SyncToken: "t-" + name}).Error)
		require.NoError(t, g.Create(&sharing.CalendarShare{UUID: "cs-" + name, CalendarID: id, SharedWithID: 2, Permission: "read"}).Error)
		require.NoError(t, g.Create(&calendar.CalendarSubscription{UUID: "sub-" + name, CalendarID: id, UserID: 1,
			URL: "https://feed.example/" + name, RefreshInterval: time.Hour, NextSyncAt: time.Now(), Enabled: true}).Error)
	}
	seedBook := func(id uint, name string) {
		// The token matches the change-log row below, so backfillCollectionAnchors
		// has nothing to add.
		require.NoError(t, g.Create(&addressbook.AddressBook{ID: id, UUID: "ab-" + name, UserID: 1, Path: name, Name: name,
			SyncToken: "t-" + name, CTag: "t-" + name}).Error)
		contact := &addressbook.AddressObject{ID: id * 100, UUID: "con-" + name, AddressBookID: id, Path: "c.vcf", UID: "c@" + name,
			VCardData: "BEGIN:VCARD\r\nFN:X\r\nEND:VCARD\r\n", FormattedName: "X"}
		require.NoError(t, g.Create(contact).Error)
		require.NoError(t, g.Create(&addressbook.ContactPhoto{AddressObjectID: contact.ID, PhotoData: "AA==", PhotoType: "JPEG"}).Error)
		require.NoError(t, g.Create(&addressbook.SyncChangeLog{AddressBookID: id, ResourcePath: "c.vcf", ChangeType: "created", SyncToken: "t-" + name}).Error)
		require.NoError(t, g.Create(&sharing.AddressBookShare{UUID: "as-" + name, AddressBookID: id, SharedWithID: 2, Permission: "read"}).Error)
	}

	// Collection 1 of each kind: deleted the old way, only its row soft-deleted.
	seedCalendar(1, "old", &token)
	seedBook(1, "old")
	require.NoError(t, g.Delete(&calendar.Calendar{}, 1).Error)
	require.NoError(t, g.Delete(&addressbook.AddressBook{}, 1).Error)
	// Collection 2 of each kind: live.
	seedCalendar(2, "live", nil)
	seedBook(2, "live")
	// Contents of collection 7, which does not exist at all.
	require.NoError(t, g.Create(&calendar.CalendarObject{UUID: "obj-orphan", CalendarID: 7, Path: "o.ics", UID: "o", ETag: "1",
		ComponentType: "VEVENT", ICalData: ical}).Error)
	require.NoError(t, g.Create(&calendar.SyncChangeLog{CalendarID: 7, ResourcePath: "o.ics", ChangeType: "created", SyncToken: "t-orphan"}).Error)
	require.NoError(t, g.Create(&addressbook.SyncChangeLog{AddressBookID: 7, ResourcePath: "o.vcf", ChangeType: "created", SyncToken: "t-orphan"}).Error)
	require.NoError(t, g.Create(&addressbook.ContactPhoto{AddressObjectID: 700, PhotoData: "AA==", PhotoType: "JPEG"}).Error)

	count := func(model any, where string, args ...any) int64 {
		var n int64
		require.NoError(t, g.Unscoped().Model(model).Where(where, args...).Count(&n).Error)
		return n
	}
	collectionRows := func(calID, bookID uint, contactID uint) map[string]int64 {
		return map[string]int64{
			"calendars":                    count(&calendar.Calendar{}, "id = ?", calID),
			"calendar_objects":             count(&calendar.CalendarObject{}, "calendar_id = ?", calID),
			"sync_change_logs":             count(&calendar.SyncChangeLog{}, "calendar_id = ?", calID),
			"calendar_shares":              count(&sharing.CalendarShare{}, "calendar_id = ?", calID),
			"calendar_subscriptions":       count(&calendar.CalendarSubscription{}, "calendar_id = ?", calID),
			"address_books":                count(&addressbook.AddressBook{}, "id = ?", bookID),
			"address_objects":              count(&addressbook.AddressObject{}, "address_book_id = ?", bookID),
			"contact_photos":               count(&addressbook.ContactPhoto{}, "address_object_id = ?", contactID),
			"addressbook_sync_change_logs": count(&addressbook.SyncChangeLog{}, "address_book_id = ?", bookID),
			"addressbook_shares":           count(&sharing.AddressBookShare{}, "address_book_id = ?", bookID),
		}
	}
	liveBefore := collectionRows(2, 2, 200)
	for table, n := range liveBefore {
		require.NotZerof(t, n, "seed: the live collections must hold %s rows", table)
	}

	var logBuf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, nil)))
	defer slog.SetDefault(prev)

	require.NoError(t, RunDataMigrations(g))
	firstLog := logBuf.String()

	for table, n := range collectionRows(1, 1, 100) {
		assert.Zerof(t, n, "%s: the old soft-deleted collections must be purged with everything in them", table)
	}
	for table, n := range collectionRows(7, 7, 700) {
		assert.Zerof(t, n, "%s: contents of a missing collection must be purged", table)
	}
	assert.Zero(t, count(&calendar.Calendar{}, "public_token = ?", token), "the old calendar's public link must be gone")
	assert.Equal(t, liveBefore, collectionRows(2, 2, 200), "live collections keep everything, their soft-deleted objects included")
	assert.EqualValues(t, 2, count(&user.User{}, "1 = 1"), "users are not touched")

	assert.Contains(t, firstLog, "permanently deleted", "the purge must be logged")
	assert.Contains(t, firstLog, "calendar_objects=3", "two objects of the old calendar and the orphan")
	assert.Contains(t, firstLog, "address_books=1")

	logBuf.Reset()
	require.NoError(t, RunDataMigrations(g))
	assert.Equal(t, liveBefore, collectionRows(2, 2, 200), "a second run changes nothing")
	assert.NotContains(t, logBuf.String(), "permanently deleted", "a second run finds nothing to purge and logs nothing")
}

// TestReleaseDeletedUserIdentities covers accounts deleted before F19 was
// fixed: the soft-deleted row still holds its e-mail and username (and its
// OAuth connection the provider identity). The migration tombstones them so the
// address can register again, leaves live accounts alone, and is idempotent.
func TestReleaseDeletedUserIdentities(t *testing.T) {
	dataDir, err := os.MkdirTemp("", "caldav-user-release-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dataDir)

	cfg := &config.Config{
		DataDir:  dataDir,
		Database: config.DatabaseConfig{Driver: "sqlite", AutoMigrate: true},
	}
	db, err := New(cfg)
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, db.Migrate(Models()...))
	g := db.DB()

	// Deleted the old way: soft delete only, identity kept.
	gone := &user.User{ID: 1, UUID: "u-gone", Username: "gone", Email: "gone@x", PasswordHash: "x"}
	require.NoError(t, g.Create(gone).Error)
	require.NoError(t, g.Create(&user.OAuthConnection{UserID: 1, Provider: "google", ProviderID: "sub-gone"}).Error)
	require.NoError(t, g.Delete(gone).Error)
	live := &user.User{ID: 2, UUID: "u-live", Username: "live", Email: "live@x", PasswordHash: "x"}
	require.NoError(t, g.Create(live).Error)
	require.NoError(t, g.Create(&user.OAuthConnection{UserID: 2, Provider: "google", ProviderID: "sub-live"}).Error)

	require.NoError(t, RunDataMigrations(g))
	require.NoError(t, RunDataMigrations(g)) // idempotent

	var got user.User
	require.NoError(t, g.Unscoped().First(&got, 1).Error)
	require.True(t, got.DeletedAt.Valid, "the row stays soft-deleted (setup keeps counting it)")
	require.Equal(t, "deleted:u-gone", got.Email)
	require.Equal(t, "deleted:u-gone", got.Username)
	got = user.User{}
	require.NoError(t, g.First(&got, 2).Error)
	require.Equal(t, "live@x", got.Email)
	require.Equal(t, "live", got.Username)

	var conns []user.OAuthConnection
	require.NoError(t, g.Find(&conns).Error)
	require.Len(t, conns, 1, "only the deleted account's provider link is removed")
	require.Equal(t, "sub-live", conns[0].ProviderID)

	// The address and username are free again.
	require.NoError(t, g.Create(&user.User{UUID: "u-back", Username: "gone", Email: "gone@x", PasswordHash: "x"}).Error)
}

// TestReleaseRevokedCredentialUsernames covers DAV credentials revoked by the
// old Revoke, which only set revoked_at: the row kept its username in the
// unique index, so the name could never be used again (F24).
func TestReleaseRevokedCredentialUsernames(t *testing.T) {
	dataDir, err := os.MkdirTemp("", "caldav-cred-release-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dataDir)

	cfg := &config.Config{
		DataDir:  dataDir,
		Database: config.DatabaseConfig{Driver: "sqlite", AutoMigrate: true},
	}
	db, err := New(cfg)
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, db.Migrate(Models()...))
	g := db.DB()

	require.NoError(t, g.Create(&user.User{ID: 1, UUID: "u-1", Username: "u1", Email: "u1@x", PasswordHash: "x"}).Error)
	revokedAt := time.Now().Add(-time.Hour)
	require.NoError(t, g.Create(&user.CalDAVCredential{UUID: "cal-revoked", UserID: 1, Name: "old", Username: "calname",
		PasswordHash: "x", Permission: "read", RevokedAt: &revokedAt}).Error)
	require.NoError(t, g.Create(&user.CalDAVCredential{UUID: "cal-live", UserID: 1, Name: "live", Username: "calive",
		PasswordHash: "x", Permission: "read"}).Error)
	require.NoError(t, g.Create(&user.CardDAVCredential{UUID: "card-revoked", UserID: 1, Name: "old", Username: "cardname",
		PasswordHash: "x", Permission: "read", RevokedAt: &revokedAt}).Error)

	require.NoError(t, RunDataMigrations(g))
	require.NoError(t, RunDataMigrations(g)) // idempotent

	var cal user.CalDAVCredential
	require.NoError(t, g.Where("uuid = ?", "cal-revoked").First(&cal).Error)
	require.Equal(t, "revoked:cal-revoked", cal.Username)
	require.NotNil(t, cal.RevokedAt)
	var live user.CalDAVCredential
	require.NoError(t, g.Where("uuid = ?", "cal-live").First(&live).Error)
	require.Equal(t, "calive", live.Username, "a live credential keeps its username")
	var card user.CardDAVCredential
	require.NoError(t, g.Where("uuid = ?", "card-revoked").First(&card).Error)
	require.Equal(t, "revoked:card-revoked", card.Username)

	// The released names can be taken by new credentials.
	require.NoError(t, g.Create(&user.CalDAVCredential{UUID: "cal-new", UserID: 1, Name: "new", Username: "calname",
		PasswordHash: "x", Permission: "read"}).Error)
	require.NoError(t, g.Create(&user.CardDAVCredential{UUID: "card-new", UserID: 1, Name: "new", Username: "cardname",
		PasswordHash: "x", Permission: "read"}).Error)
}

// newDataMigrationTestDB opens a migrated SQLite database in a temp dir, with
// user 1 owning the mixed calendar 1. Objects need a live calendar: anything
// else is an orphan that purgeDeletedCollections removes.
func newDataMigrationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	g := openDataMigrationTestDB(t)
	require.NoError(t, g.Create(&calendar.Calendar{ID: 1, UUID: "cal-1", UserID: 1, Path: "c1", Name: "C1",
		Color: "#ffffff", Timezone: "UTC", SupportedComponents: "VEVENT,VTODO"}).Error)
	return g
}

// openDataMigrationTestDB opens a migrated SQLite database in a temp dir that
// holds user 1 and nothing else.
func openDataMigrationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := New(&config.Config{
		DataDir:  t.TempDir(),
		Database: config.DatabaseConfig{Driver: "sqlite", AutoMigrate: true},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.Migrate(Models()...))
	g := db.DB()
	require.NoError(t, g.Create(&user.User{ID: 1, UUID: "u-1", Username: "u1", Email: "u1@x", PasswordHash: "x"}).Error)
	return g
}

// todo wraps VTODO property lines in a VCALENDAR.
func todo(lines ...string) string {
	return wrap("BEGIN:VTODO\r\n" + strings.Join(lines, "\r\n") + "\r\nEND:VTODO")
}

// sameTime reports whether two optional instants are both nil or equal.
func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

// TestBackfillTaskFields covers VTODOs stored before #170 derived the task
// columns: the boot backfill fills them (soft-deleted rows too) from the stored
// iCalendar without touching e_tag or updated_at — a backfill must not make DAV
// clients re-download — and a second run is a no-op. It runs through
// RunDataMigrations, as on boot (the integration TestMain does not run data
// migrations).
func TestBackfillTaskFields(t *testing.T) {
	g := newDataMigrationTestDB(t)

	// Every row is seeded the way pre-#170 code left it: empty task columns,
	// and is_all_day / start_time from the old VEVENT-only logic or not at all.
	old := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	seeded := map[string]string{}
	seed := func(uuid, componentType, ical string) {
		t.Helper()
		seeded[uuid] = ical
		require.NoError(t, g.Create(&calendar.CalendarObject{UUID: uuid, CalendarID: 1, Path: uuid + ".ics", UID: uuid,
			ETag: "etag-" + uuid, ComponentType: componentType, ICalData: ical, ContentLength: len(ical),
			CreatedAt: old, UpdatedAt: old}).Error)
	}
	get := func(uuid string) calendar.CalendarObject {
		t.Helper()
		var o calendar.CalendarObject
		require.NoError(t, g.Unscoped().Where("uuid = ?", uuid).First(&o).Error)
		return o
	}

	// A due date (all-day) with a timed DTSTART: IsAllDay follows DUE for a task.
	seed("task-open", calendar.ComponentToDo, todo("UID:task-open", "DTSTART:20261101T090000Z",
		"DUE;VALUE=DATE:20261105", "STATUS:in-process", "PRIORITY:1", "PERCENT-COMPLETE:40"))
	// No STATUS but COMPLETED: completed. PRIORITY:0 is "undefined", stored nil.
	seed("task-done", calendar.ComponentToDo, todo("UID:task-done", "DUE;TZID=Europe/Berlin:20261103T120000",
		"COMPLETED:20261102T100000Z", "PRIORITY:0"))
	// A row upgraded in place holds NULL, not '': AutoMigrate adds the column
	// without a default.
	require.NoError(t, g.Exec("UPDATE calendar_objects SET status = NULL WHERE uuid = ?", "task-done").Error)
	// A soft-deleted task (a DELETE soft-deletes the object) is backfilled too.
	seed("task-deleted", calendar.ComponentToDo, todo("UID:task-deleted", "STATUS:NEEDS-ACTION", "PRIORITY:9"))
	require.NoError(t, g.Where("uuid = ?", "task-deleted").Delete(&calendar.CalendarObject{}).Error)
	// Undecodable (the VTODO never ends): skipped, the boot does not fail.
	seed("task-bad", calendar.ComponentToDo, "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VTODO\r\nUID:task-bad\r\nSTATUS:COMPLETED\r\n")
	// A row whose status is already set is not selected again, even though its
	// iCalendar says otherwise.
	seed("task-filled", calendar.ComponentToDo, todo("UID:task-filled", "STATUS:NEEDS-ACTION", "PRIORITY:5"))
	require.NoError(t, g.Model(&calendar.CalendarObject{}).Where("uuid = ?", "task-filled").
		UpdateColumn("status", calendar.TaskStatusCompleted).Error)
	// Events are not touched, even with an empty status.
	seed("event", calendar.ComponentEvent, wrap("BEGIN:VEVENT\r\nUID:event\r\nDTSTART:20261101T090000Z\r\nDTEND:20261101T100000Z\r\nEND:VEVENT"))
	// Stored as a VTODO but holding an event: not a task, skipped.
	seed("task-mismatch", calendar.ComponentToDo, wrap("BEGIN:VEVENT\r\nUID:task-mismatch\r\nDTSTART:20261101T090000Z\r\nEND:VEVENT"))
	// Enough tasks to cross the 200-row batch boundary while rows drop out of
	// the query as they are filled.
	const bulk = 250
	for i := 0; i < bulk; i++ {
		uuid := fmt.Sprintf("bulk-%03d", i)
		seed(uuid, calendar.ComponentToDo, todo("UID:"+uuid, fmt.Sprintf("PRIORITY:%d", i%9+1)))
	}

	require.NoError(t, RunDataMigrations(g))

	t.Run("fills the task columns", func(t *testing.T) {
		o := get("task-open")
		assert.Equal(t, calendar.TaskStatusInProcess, o.Status)
		assert.True(t, sameTime(o.DueAt, ptrTime(time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC))), "due_at = %v", o.DueAt)
		assert.True(t, o.IsAllDay, "is_all_day follows the date-only DUE")
		assert.True(t, sameTime(o.StartTime, ptrTime(time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC))), "start_time = %v", o.StartTime)
		assert.Equal(t, ptrInt(1), o.Priority)
		assert.Equal(t, ptrInt(40), o.PercentComplete)
		assert.Nil(t, o.CompletedAt)

		o = get("task-done")
		assert.Equal(t, calendar.TaskStatusCompleted, o.Status)
		assert.True(t, sameTime(o.CompletedAt, ptrTime(time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC))), "completed_at = %v", o.CompletedAt)
		assert.True(t, sameTime(o.DueAt, ptrTime(time.Date(2026, 11, 3, 11, 0, 0, 0, time.UTC))), "due_at = %v (12:00 CET)", o.DueAt)
		assert.False(t, o.IsAllDay)
		assert.Nil(t, o.Priority, "PRIORITY:0 is stored as nil")
		assert.Nil(t, o.PercentComplete)

		o = get("task-deleted")
		assert.True(t, o.DeletedAt.Valid, "the row stays soft-deleted")
		assert.Equal(t, calendar.TaskStatusNeedsAction, o.Status, "soft-deleted rows are backfilled too")
		assert.Equal(t, ptrInt(9), o.Priority)

		// The written columns are exactly what every write path derives.
		for _, uuid := range []string{"task-open", "task-done", "task-deleted"} {
			o := get(uuid)
			want := calendar.CalendarObject{ICalData: o.ICalData}
			require.NoError(t, want.PopulateDenormFieldsFromICal())
			assert.Equal(t, want.Status, o.Status, uuid)
			assert.True(t, sameTime(want.DueAt, o.DueAt), "%s due_at", uuid)
			assert.True(t, sameTime(want.CompletedAt, o.CompletedAt), "%s completed_at", uuid)
			assert.Equal(t, want.PercentComplete, o.PercentComplete, uuid)
			assert.Equal(t, want.Priority, o.Priority, uuid)
			assert.Equal(t, want.IsAllDay, o.IsAllDay, uuid)
			assert.True(t, sameTime(want.StartTime, o.StartTime), "%s start_time", uuid)
			assert.True(t, sameTime(want.EndTime, o.EndTime), "%s end_time", uuid)
		}

		var filled int64
		require.NoError(t, g.Unscoped().Model(&calendar.CalendarObject{}).
			Where("uuid LIKE 'bulk-%' AND status = ? AND priority IS NOT NULL", calendar.TaskStatusNeedsAction).
			Count(&filled).Error)
		assert.EqualValues(t, bulk, filled, "every task is filled, across batches")
		assert.Equal(t, ptrInt(211%9+1), get("bulk-211").Priority)

		bad := get("task-bad")
		assert.Empty(t, bad.Status, "an undecodable row is skipped")
		assert.Nil(t, bad.Priority)

		filledBefore := get("task-filled")
		assert.Equal(t, calendar.TaskStatusCompleted, filledBefore.Status, "a row with a status is not re-derived")
		assert.Nil(t, filledBefore.Priority)

		for _, uuid := range []string{"event", "task-mismatch"} {
			o := get(uuid)
			assert.Empty(t, o.Status, uuid)
			assert.Nil(t, o.StartTime, "%s is not rewritten", uuid)
		}
	})

	t.Run("leaves e_tag and updated_at untouched", func(t *testing.T) {
		var rows []calendar.CalendarObject
		require.NoError(t, g.Unscoped().Order("id").Find(&rows).Error)
		require.Len(t, rows, len(seeded))
		for _, o := range rows {
			assert.Equal(t, "etag-"+o.UUID, o.ETag, "%s: e_tag must not change", o.UUID)
			assert.Truef(t, o.UpdatedAt.Equal(old), "%s: updated_at must not change, got %v", o.UUID, o.UpdatedAt)
			assert.Equal(t, seeded[o.UUID], o.ICalData, "%s: i_cal_data must not change", o.UUID)
		}
	})

	t.Run("second run is a no-op", func(t *testing.T) {
		// A filled row is not selected again: a value changed behind the
		// backfill's back survives the next boot.
		require.NoError(t, g.Model(&calendar.CalendarObject{}).Where("uuid = ?", "task-open").
			UpdateColumn("priority", 7).Error)
		snapshot := func() []calendar.CalendarObject {
			var rows []calendar.CalendarObject
			require.NoError(t, g.Unscoped().Order("id").Find(&rows).Error)
			return rows
		}
		before := snapshot()

		require.NoError(t, RunDataMigrations(g))

		assert.Equal(t, before, snapshot(), "the second run changes no calendar object")
		assert.Equal(t, ptrInt(7), get("task-open").Priority)
	})
}

// TestUnescapeTextColumnsIsOneShot: the repair claims its marker in the same
// transaction as its writes. A row that needs repairing after the marker exists
// is not read, a failure leaves no marker, and an install that ran the earlier
// unmarked version runs it once more without touching e_tag or updated_at.
func TestUnescapeTextColumnsIsOneShot(t *testing.T) {
	g := newDataMigrationTestDB(t)
	old := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	ical := wrap("BEGIN:VEVENT\r\nUID:one\r\nDTSTART:20261101T090000Z\r\nSUMMARY:Buy milk\\, eggs\r\nEND:VEVENT")
	require.NoError(t, g.Create(&calendar.CalendarObject{UUID: "one", CalendarID: 1, Path: "one.ics", UID: "one",
		ETag: "etag-one", ComponentType: calendar.ComponentEvent, ICalData: ical, ContentLength: len(ical),
		Summary: `Buy milk\, eggs`, CreatedAt: old, UpdatedAt: old}).Error)
	column := func() string {
		var o calendar.CalendarObject
		require.NoError(t, g.Where("uuid = ?", "one").First(&o).Error)
		return o.Summary
	}
	markers := func() int64 {
		var n int64
		require.NoError(t, g.Model(&domain.SystemSetting{}).Where("key = ?", unescapeTextColumnsMarkerKey).Count(&n).Error)
		return n
	}

	require.NoError(t, unescapeTextColumns(g, time.Now().UTC()))
	assert.Equal(t, "Buy milk, eggs", column())
	assert.EqualValues(t, 1, markers(), "the marker is written with the repair")

	// Marked: a row that still has the old shape is left alone.
	require.NoError(t, g.Model(&calendar.CalendarObject{}).Where("uuid = ?", "one").UpdateColumn("summary", `Buy milk\, eggs`).Error)
	require.NoError(t, unescapeTextColumns(g, time.Now().UTC()))
	assert.Equal(t, `Buy milk\, eggs`, column(), "once marked, the step reads nothing")
	assert.EqualValues(t, 1, markers())

	var o calendar.CalendarObject
	require.NoError(t, g.Where("uuid = ?", "one").First(&o).Error)
	assert.Equal(t, "etag-one", o.ETag)
	assert.True(t, o.UpdatedAt.Equal(old), "updated_at must not change")
}

// TestUnescapeTextColumns covers objects stored while the summary, description
// and location columns kept the iCalendar's TEXT escaping (`Buy milk\, eggs`):
// the boot step re-derives them as plain text from the stored iCalendar
// (soft-deleted rows too, across batches) without touching e_tag, updated_at
// or i_cal_data; a column whose plain text holds a backslash is never
// unescaped twice, the UID stays raw, and a second run is a no-op.
func TestUnescapeTextColumns(t *testing.T) {
	g := newDataMigrationTestDB(t)
	old := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)

	type row struct{ summary, description, location string }
	seeded := map[string]string{}
	// seed stores an object with the given iCalendar and column values, the
	// way the code before the fix left it (columns = raw property values).
	seed := func(uuid, componentType, ical string, cols row) {
		t.Helper()
		seeded[uuid] = ical
		obj := &calendar.CalendarObject{UUID: uuid, CalendarID: 1, Path: uuid + ".ics", UID: uuid,
			ETag: "etag-" + uuid, ComponentType: componentType, ICalData: ical, ContentLength: len(ical),
			Summary: cols.summary, Description: cols.description, Location: cols.location,
			CreatedAt: old, UpdatedAt: old}
		if componentType == calendar.ComponentToDo {
			obj.Status = calendar.TaskStatusNeedsAction // keep backfillTaskFields out of the picture
		}
		require.NoError(t, g.Create(obj).Error)
	}
	get := func(uuid string) calendar.CalendarObject {
		t.Helper()
		var o calendar.CalendarObject
		require.NoError(t, g.Unscoped().Where("uuid = ?", uuid).First(&o).Error)
		return o
	}
	event := func(uid string, lines ...string) string {
		return wrap("BEGIN:VEVENT\r\nUID:" + uid + "\r\nDTSTART:20261101T090000Z\r\n" + strings.Join(lines, "\r\n") + "\r\nEND:VEVENT")
	}

	seed("event-escaped", calendar.ComponentEvent,
		event("event-escaped", `SUMMARY:Buy milk\, eggs\; bread`, `DESCRIPTION:Line one\nLine two`, `LOCATION:Hall\, 2`),
		row{`Buy milk\, eggs\; bread`, `Line one\nLine two`, `Hall\, 2`})
	seed("task-escaped", calendar.ComponentToDo,
		todo("UID:task-escaped", `SUMMARY:Call Bob\, Alice`, `DESCRIPTION:C:\\temp\\new`),
		row{summary: `Call Bob\, Alice`, description: `C:\\temp\\new`})
	// Only the description was escaped: the other columns are rewritten with
	// the values they already hold.
	seed("description-only", calendar.ComponentEvent,
		event("description-only", "SUMMARY:Standup", `DESCRIPTION:Agenda\N1. budget`),
		row{summary: "Standup", description: `Agenda\N1. budget`})
	seed("deleted", calendar.ComponentEvent, event("deleted", `SUMMARY:Gone\, but fixed`), row{summary: `Gone\, but fixed`})
	require.NoError(t, g.Where("uuid = ?", "deleted").Delete(&calendar.CalendarObject{}).Error)
	// Already plain text with a real backslash (written after the fix): the
	// row is read but must not be unescaped a second time ("C:\new" is not
	// "C:" + newline + "ew").
	seed("real-backslash", calendar.ComponentToDo, todo("UID:real-backslash", `SUMMARY:C:\\new`), row{summary: `C:\new`})
	// No backslash anywhere: not selected.
	seed("plain", calendar.ComponentEvent, event("plain", "SUMMARY:Standup", "LOCATION:Room 1"), row{summary: "Standup", location: "Room 1"})
	// A client's unescaped comma was never escaped: nothing to do.
	seed("sloppy", calendar.ComponentToDo, todo("UID:sloppy", "SUMMARY:Buy milk, eggs"), row{summary: "Buy milk, eggs"})
	// Undecodable: skipped, the boot does not fail, the column stays.
	seed("bad", calendar.ComponentToDo, "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VTODO\r\nUID:bad\r\nSUMMARY:x\\, y\r\n", row{summary: `x\, y`})
	// The UID is an identifier and is never rewritten by this step.
	seed("uid", calendar.ComponentToDo, todo(`UID:odd\,uid`, `SUMMARY:With\, uid`), row{summary: `With\, uid`})
	require.NoError(t, g.Model(&calendar.CalendarObject{}).Where("uuid = ?", "uid").UpdateColumn("uid", `odd\,uid`).Error)
	// Enough escaped rows to cross the 200-row batch boundary.
	const bulk = 210
	for i := range bulk {
		uuid := fmt.Sprintf("bulk-%03d", i)
		seed(uuid, calendar.ComponentEvent, event(uuid, fmt.Sprintf(`SUMMARY:Item\, %d`, i)), row{summary: fmt.Sprintf(`Item\, %d`, i)})
	}

	require.NoError(t, RunDataMigrations(g))

	t.Run("re-derives the text columns", func(t *testing.T) {
		for uuid, want := range map[string]row{
			"event-escaped":    {"Buy milk, eggs; bread", "Line one\nLine two", "Hall, 2"},
			"task-escaped":     {"Call Bob, Alice", `C:\temp\new`, ""},
			"description-only": {"Standup", "Agenda\n1. budget", ""},
			"deleted":          {"Gone, but fixed", "", ""},
			"real-backslash":   {`C:\new`, "", ""},
			"plain":            {"Standup", "", "Room 1"},
			"sloppy":           {"Buy milk, eggs", "", ""},
			"bad":              {`x\, y`, "", ""},
			"uid":              {"With, uid", "", ""},
		} {
			o := get(uuid)
			assert.Equal(t, want, row{o.Summary, o.Description, o.Location}, uuid)
		}
		assert.True(t, get("deleted").DeletedAt.Valid, "the row stays soft-deleted")
		assert.Equal(t, `odd\,uid`, get("uid").UID, "the UID column is not touched")

		var fixed int64
		require.NoError(t, g.Model(&calendar.CalendarObject{}).
			Where("uuid LIKE 'bulk-%' AND summary LIKE 'Item, %'").Count(&fixed).Error)
		assert.EqualValues(t, bulk, fixed, "every row, across batches")
		assert.Equal(t, "Item, 205", get("bulk-205").Summary)

		// The columns are exactly what every write path now derives.
		for uuid := range seeded {
			if uuid == "bad" {
				continue
			}
			o := get(uuid)
			want := calendar.CalendarObject{ICalData: o.ICalData}
			require.NoError(t, want.PopulateDenormFieldsFromICal())
			assert.Equal(t, row{want.Summary, want.Description, want.Location}, row{o.Summary, o.Description, o.Location}, uuid)
		}
	})

	t.Run("leaves e_tag, updated_at and i_cal_data untouched", func(t *testing.T) {
		var rows []calendar.CalendarObject
		require.NoError(t, g.Unscoped().Order("id").Find(&rows).Error)
		require.Len(t, rows, len(seeded))
		for _, o := range rows {
			assert.Equal(t, "etag-"+o.UUID, o.ETag, "%s: e_tag must not change", o.UUID)
			assert.Truef(t, o.UpdatedAt.Equal(old), "%s: updated_at must not change, got %v", o.UUID, o.UpdatedAt)
			assert.Equal(t, seeded[o.UUID], o.ICalData, "%s: i_cal_data must not change", o.UUID)
		}
	})

	t.Run("second run is a no-op", func(t *testing.T) {
		snapshot := func() []calendar.CalendarObject {
			var rows []calendar.CalendarObject
			require.NoError(t, g.Unscoped().Order("id").Find(&rows).Error)
			return rows
		}
		before := snapshot()
		require.NoError(t, RunDataMigrations(g))
		assert.Equal(t, before, snapshot())
	})
}

// TestRecordTaskKindCutoff covers tasks.kind_introduced_at: the first boot with
// #170 records when the calendar `kind` arrived, and no later boot moves it — the
// narrowing migration tells legacy calendars from deliberate ones by it.
func TestRecordTaskKindCutoff(t *testing.T) {
	cutoff := func(t *testing.T, g *gorm.DB) string {
		t.Helper()
		var rows []domain.SystemSetting
		require.NoError(t, g.Where("key = ?", taskKindIntroducedAtKey).Find(&rows).Error)
		require.Len(t, rows, 1, "exactly one cutoff row")
		return rows[0].Value
	}

	t.Run("written on the first boot and never overwritten", func(t *testing.T) {
		g := newDataMigrationTestDB(t)

		before := time.Now().UTC()
		require.NoError(t, RunDataMigrations(g))
		after := time.Now().UTC()

		first := cutoff(t, g)
		at, err := time.Parse(time.RFC3339Nano, first)
		require.NoError(t, err, "the value is RFC 3339")
		assert.True(t, strings.HasSuffix(first, "Z"), "the value is UTC: %q", first)
		assert.False(t, at.Before(before) || at.After(after), "the value is the boot time: %v not in [%v, %v]", at, before, after)

		require.NoError(t, RunDataMigrations(g))
		require.NoError(t, recordTaskKindCutoff(g, after.Add(time.Hour)))
		assert.Equal(t, first, cutoff(t, g), "later boots keep the first value")
	})

	t.Run("an existing value is kept", func(t *testing.T) {
		g := newDataMigrationTestDB(t)
		const stored = "2026-01-01T00:00:00Z"
		require.NoError(t, g.Create(&domain.SystemSetting{Key: taskKindIntroducedAtKey, Value: stored}).Error)

		require.NoError(t, RunDataMigrations(g))

		assert.Equal(t, stored, cutoff(t, g))
	})
}

func ptrTime(t time.Time) *time.Time { return &t }
func ptrInt(i int) *int              { return &i }

// narrowCutoff is the tasks.kind_introduced_at the narrowing tests store before
// the first boot. recordTaskKindCutoff keeps a stored value, so the narrowing
// reads this one; calendars created during the test (real clock) are after it.
var narrowCutoff = time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)

// narrowFixture seeds calendars and objects for the tests of the one-shot
// component-set narrowing (#170).
type narrowFixture struct {
	t    *testing.T
	g    *gorm.DB
	objs int
}

// newNarrowFixture opens a migrated database holding users 1 and 2 and, unless
// cutoff is empty, tasks.kind_introduced_at = cutoff.
func newNarrowFixture(t *testing.T, cutoff string) *narrowFixture {
	t.Helper()
	g := openDataMigrationTestDB(t)
	require.NoError(t, g.Create(&user.User{ID: 2, UUID: "u-2", Username: "u2", Email: "u2@x", PasswordHash: "x"}).Error)
	if cutoff != "" {
		require.NoError(t, g.Create(&domain.SystemSetting{Key: taskKindIntroducedAtKey, Value: cutoff}).Error)
	}
	// The narrowing tests are about the narrowing: the text repair has run.
	require.NoError(t, g.Create(&domain.SystemSetting{Key: unescapeTextColumnsMarkerKey, Value: "2025-01-01T00:00:00Z"}).Error)
	return &narrowFixture{t: t, g: g}
}

// narrowCalendar describes a calendar to seed.
type narrowCalendar struct {
	name       string
	set        string
	createdAt  time.Time // zero: an hour before narrowCutoff
	user       uint      // zero: user 1
	subscribed bool
	live       []string // component types of its live objects
	deleted    []string // component types of its soft-deleted objects
}

// seed stores the calendar and its objects. Its sync token has the
// "collection" anchor row Create writes, so backfillCollectionAnchors adds
// nothing and every change-log row after it is the narrowing's.
func (f *narrowFixture) seed(c narrowCalendar) *calendar.Calendar {
	f.t.Helper()
	if c.createdAt.IsZero() {
		c.createdAt = narrowCutoff.Add(-time.Hour)
	}
	if c.user == 0 {
		c.user = 1
	}
	token := "tok-" + c.name
	cal := &calendar.Calendar{UUID: "cal-" + c.name, UserID: c.user, Path: c.name, Name: c.name,
		Color: "#ffffff", Timezone: "UTC", SupportedComponents: c.set, SyncToken: token, CTag: token,
		Subscribed: c.subscribed, CreatedAt: c.createdAt, UpdatedAt: c.createdAt}
	require.NoError(f.t, f.g.Create(cal).Error)
	require.NoError(f.t, f.g.Create(&calendar.SyncChangeLog{CalendarID: cal.ID, ChangeType: "collection", SyncToken: token}).Error)
	for _, comp := range c.live {
		f.object(cal, comp)
	}
	for _, comp := range c.deleted {
		require.NoError(f.t, f.g.Delete(f.object(cal, comp)).Error) // a DELETE soft-deletes the object
	}
	return cal
}

// object stores a live object of the given component type in cal.
func (f *narrowFixture) object(cal *calendar.Calendar, componentType string) *calendar.CalendarObject {
	f.t.Helper()
	f.objs++
	uid := fmt.Sprintf("%s-%d", cal.Name, f.objs)
	ical := wrap(fmt.Sprintf("BEGIN:%[1]s\r\nUID:%[2]s\r\nDTSTART:20250101T100000Z\r\nEND:%[1]s", componentType, uid))
	obj := &calendar.CalendarObject{UUID: uid, CalendarID: cal.ID, Path: uid + ".ics", UID: uid, ETag: "1",
		ComponentType: componentType, ICalData: ical, ContentLength: len(ical)}
	require.NoError(f.t, f.g.Create(obj).Error)
	return obj
}

// get loads a seeded calendar, soft-deleted or not.
func (f *narrowFixture) get(name string) calendar.Calendar {
	f.t.Helper()
	var c calendar.Calendar
	require.NoError(f.t, f.g.Unscoped().Where("uuid = ?", "cal-"+name).First(&c).Error)
	return c
}

// changes is the change log of one calendar, oldest first.
func (f *narrowFixture) changes(calendarID uint) []calendar.SyncChangeLog {
	f.t.Helper()
	var rows []calendar.SyncChangeLog
	require.NoError(f.t, f.g.Where("calendar_id = ?", calendarID).Order("id").Find(&rows).Error)
	return rows
}

// markers are the narrowing's system_settings rows (at most one).
func (f *narrowFixture) markers() []domain.SystemSetting {
	f.t.Helper()
	var rows []domain.SystemSetting
	require.NoError(f.t, f.g.Where("key = ?", narrowComponentSetsMarkerKey).Find(&rows).Error)
	return rows
}

// narrowState is everything a narrowing writes: the calendar rows (soft-deleted
// ones too), the change log and the system settings.
type narrowState struct {
	Calendars []calendar.Calendar
	Changes   []calendar.SyncChangeLog
	Settings  []domain.SystemSetting
}

func (f *narrowFixture) state() narrowState {
	f.t.Helper()
	var s narrowState
	require.NoError(f.t, f.g.Unscoped().Order("id").Find(&s.Calendars).Error)
	require.NoError(f.t, f.g.Order("id").Find(&s.Changes).Error)
	require.NoError(f.t, f.g.Order("key").Find(&s.Settings).Error)
	return s
}

// assertNarrowed checks that the calendar stored as before (with
// changesBefore change-log rows) now has the component set set, a new ctag
// equal to its new sync token and exactly one new "collection" anchor row for
// that token, and that nothing else in its row changed but updated_at.
func (f *narrowFixture) assertNarrowed(t *testing.T, before calendar.Calendar, changesBefore int, set string) {
	t.Helper()
	after := f.get(before.Name)
	assert.Equal(t, set, after.SupportedComponents, "%s: component set", before.Name)
	assert.NotEqual(t, before.CTag, after.CTag, "%s: the ctag advances", before.Name)
	assert.Equal(t, after.SyncToken, after.CTag, "%s: ctag = sync token", before.Name)
	want := before
	want.SupportedComponents, want.SyncToken, want.CTag, want.UpdatedAt = set, after.SyncToken, after.CTag, after.UpdatedAt
	assert.Equal(t, want, after, "%s: nothing else changes", before.Name)

	changes := f.changes(before.ID)
	if assert.Len(t, changes, changesBefore+1, "%s: one change-log row", before.Name) {
		last := changes[len(changes)-1]
		assert.Equal(t, "collection", last.ChangeType, before.Name)
		assert.Empty(t, last.ResourcePath, before.Name)
		assert.Equal(t, after.SyncToken, last.SyncToken, "%s: the anchor carries the new token", before.Name)
	}
}

// TestNarrowCalendarComponentSets covers the one-shot narrowing (#170) through
// RunDataMigrations, as on boot: each legacy "VEVENT,VTODO" calendar created
// before tasks.kind_introduced_at takes the set its live objects show (only
// events -> VEVENT, only tasks -> VTODO, both or none -> left mixed), through
// UpdateSupportedComponents (ctag + change-log anchor); subscribed, "" and
// post-cutoff rows are untouched; and the second boot changes nothing, not even
// a narrowed calendar its owner widened again.
func TestNarrowCalendarComponentSets(t *testing.T) {
	f := newNarrowFixture(t, narrowCutoff.Format(time.RFC3339Nano))
	const mixed = legacyComponentSet
	ev, td := calendar.ComponentEvent, calendar.ComponentToDo
	west := time.FixedZone("UTC-10", -10*3600)
	east := time.FixedZone("UTC+10", 10*3600)

	cases := []struct {
		cal  narrowCalendar
		want string
	}{
		// The user's first and oldest calendar is not protected: holding only
		// synced reminders, it becomes a task list.
		{narrowCalendar{name: "personal", set: mixed, createdAt: narrowCutoff.AddDate(-1, 0, 0), live: []string{td, td}}, td},
		{narrowCalendar{name: "events-only", set: mixed, live: []string{ev, ev}}, ev},
		{narrowCalendar{name: "both", set: mixed, live: []string{ev, td}}, mixed},
		{narrowCalendar{name: "empty", set: mixed}, mixed},
		// Soft-deleted objects do not count.
		{narrowCalendar{name: "events-gone-task", set: mixed, live: []string{ev}, deleted: []string{td}}, ev},
		{narrowCalendar{name: "tasks-gone-event", set: mixed, live: []string{td}, deleted: []string{ev, ev}}, td},
		{narrowCalendar{name: "only-gone", set: mixed, deleted: []string{ev, td}}, mixed},
		// An object of another component type: never narrowed around it.
		{narrowCalendar{name: "event-journal", set: mixed, live: []string{ev, "VJOURNAL"}}, mixed},
		{narrowCalendar{name: "other-user", set: mixed, user: 2, live: []string{ev}}, ev},
		{narrowCalendar{name: "subscribed", set: mixed, subscribed: true, live: []string{ev}}, mixed},
		{narrowCalendar{name: "subscribed-tasks", set: mixed, subscribed: true, live: []string{td}}, mixed},
		{narrowCalendar{name: "legacy-empty", set: "", live: []string{ev}}, ""},
		{narrowCalendar{name: "legacy-empty-tasks", set: "", live: []string{td}}, ""},
		{narrowCalendar{name: "after-events", set: mixed, createdAt: narrowCutoff.Add(time.Hour), live: []string{ev}}, mixed},
		{narrowCalendar{name: "after-tasks", set: mixed, createdAt: narrowCutoff.Add(time.Hour), live: []string{td}}, mixed},
		{narrowCalendar{name: "at-cutoff", set: mixed, createdAt: narrowCutoff, live: []string{ev}}, mixed},
		{narrowCalendar{name: "just-before", set: mixed, createdAt: narrowCutoff.Add(-time.Microsecond), live: []string{ev}}, ev},
		// created_at is compared as an instant. SQLite stores it as TEXT in
		// the offset it was written in: compared as text with the cutoff, the
		// first would count as before it and the second as after it.
		{narrowCalendar{name: "after-west", set: mixed, createdAt: narrowCutoff.Add(time.Hour).In(west), live: []string{ev}}, mixed},
		{narrowCalendar{name: "before-east", set: mixed, createdAt: narrowCutoff.Add(-time.Hour).In(east), live: []string{td}}, td},
		// Already narrow: not candidates.
		{narrowCalendar{name: "events-cal", set: ev, live: []string{ev}}, ev},
		{narrowCalendar{name: "task-list", set: td, live: []string{td}}, td},
	}
	wantEvents, wantTasks := 0, 0
	for _, c := range cases {
		f.seed(c.cal)
		if c.cal.set == mixed && c.want == ev {
			wantEvents++
		}
		if c.cal.set == mixed && c.want == td {
			wantTasks++
		}
	}
	// The trap the two offset cases are built on: their stored text sorts on
	// the wrong side of the cutoff.
	createdText := func(name string) string {
		var s string
		require.NoError(t, f.g.Raw("SELECT CAST(created_at AS TEXT) FROM calendars WHERE uuid = ?", "cal-"+name).Scan(&s).Error)
		return s
	}
	cutoffText := narrowCutoff.Format("2006-01-02 15:04:05")
	require.True(t, strings.HasSuffix(createdText("after-west"), "-10:00") && createdText("after-west") < cutoffText,
		"seed: %q must sort before %q", createdText("after-west"), cutoffText)
	require.True(t, strings.HasSuffix(createdText("before-east"), "+10:00") && createdText("before-east") > cutoffText,
		"seed: %q must sort after %q", createdText("before-east"), cutoffText)

	before := map[string]calendar.Calendar{}
	changesBefore := map[string]int{}
	for _, c := range cases {
		before[c.cal.name] = f.get(c.cal.name)
		changesBefore[c.cal.name] = len(f.changes(before[c.cal.name].ID))
	}

	var logBuf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, nil)))
	defer slog.SetDefault(prev)

	bootStart := time.Now().UTC()
	require.NoError(t, RunDataMigrations(f.g))
	bootEnd := time.Now().UTC()

	t.Run("narrowing table", func(t *testing.T) {
		for _, c := range cases {
			b := before[c.cal.name]
			if c.want != c.cal.set {
				f.assertNarrowed(t, b, changesBefore[c.cal.name], c.want)
				continue
			}
			assert.Equal(t, b, f.get(c.cal.name), "%s: untouched", c.cal.name)
			assert.Len(t, f.changes(b.ID), changesBefore[c.cal.name], "%s: no change-log row", c.cal.name)
		}
		assert.Equal(t, calendar.KindTasks, calendar.KindOf(ptrCalendar(f.get("personal"))))
		assert.Equal(t, calendar.KindEvents, calendar.KindOf(ptrCalendar(f.get("events-only"))))
		assert.Equal(t, calendar.KindBoth, calendar.KindOf(ptrCalendar(f.get("both"))))
	})

	t.Run("marker written once", func(t *testing.T) {
		markers := f.markers()
		require.Len(t, markers, 1)
		at, err := time.Parse(time.RFC3339Nano, markers[0].Value)
		require.NoError(t, err, "the value is RFC 3339")
		assert.True(t, strings.HasSuffix(markers[0].Value, "Z"), "UTC: %q", markers[0].Value)
		assert.False(t, at.Before(bootStart) || at.After(bootEnd), "the boot time: %v not in [%v, %v]", at, bootStart, bootEnd)
	})

	t.Run("logged", func(t *testing.T) {
		assert.Contains(t, logBuf.String(), "narrowed the component set of legacy calendars")
		assert.Contains(t, logBuf.String(), fmt.Sprintf("event_calendars=%d task_lists=%d", wantEvents, wantTasks))
	})

	t.Run("second run changes nothing", func(t *testing.T) {
		ctx := context.Background()
		repo := repository.NewCalendarRepository(f.g)
		// Widened again by their owners (PATCH kind "both"): legacy calendars
		// holding one component type, which the step would narrow again.
		for _, name := range []string{"events-only", "personal"} {
			cal, err := calendaruc.NewUpdateCalendarUseCase(repo).Execute(ctx, 1, "cal-"+name,
				calendaruc.UpdateCalendarRequest{Kind: calendar.KindBoth})
			require.NoError(t, err, name)
			require.Equal(t, mixed, cal.SupportedComponents, name)
		}
		// Created "both" after the cutoff, holding only events.
		created, err := calendaruc.NewCreateCalendarUseCase(repo).Execute(ctx, 1,
			calendaruc.CreateCalendarRequest{Name: "Created both", Kind: calendar.KindBoth})
		require.NoError(t, err)
		f.object(created, ev)
		// A mixed legacy calendar whose tasks were deleted since.
		require.NoError(t, f.g.Where("calendar_id = ? AND component_type = ?", before["both"].ID, td).
			Delete(&calendar.CalendarObject{}).Error)
		snapshot := f.state()
		logBuf.Reset()

		require.NoError(t, RunDataMigrations(f.g))

		assert.Equal(t, snapshot, f.state(), "no calendar, change-log row or setting changes")
		assert.Equal(t, mixed, f.get("events-only").SupportedComponents, "a widened calendar survives")
		assert.NotContains(t, logBuf.String(), "narrowed")
	})
}

func ptrCalendar(c calendar.Calendar) *calendar.Calendar { return &c }

// TestNarrowCalendarComponentSetsOnFirstBoot is the upgrade as it happens: no
// cutoff yet. RunDataMigrations records it first, so the calendars stored
// before this boot are legacy and narrowed in the same boot.
func TestNarrowCalendarComponentSetsOnFirstBoot(t *testing.T) {
	f := newNarrowFixture(t, "")
	f.seed(narrowCalendar{name: "events", set: legacyComponentSet, createdAt: time.Now().Add(-time.Minute),
		live: []string{calendar.ComponentEvent}})
	f.seed(narrowCalendar{name: "tasks", set: legacyComponentSet, createdAt: time.Now().Add(-time.Minute),
		live: []string{calendar.ComponentToDo}})
	eventsBefore, tasksBefore := f.get("events"), f.get("tasks")

	require.NoError(t, RunDataMigrations(f.g))

	f.assertNarrowed(t, eventsBefore, 1, calendar.ComponentEvent)
	f.assertNarrowed(t, tasksBefore, 1, calendar.ComponentToDo)
	assert.Len(t, f.markers(), 1)
}

// TestNarrowCalendarComponentSetsSkippedWhenMarked: once the marker exists the
// step does nothing at all, whatever the calendars hold.
func TestNarrowCalendarComponentSetsSkippedWhenMarked(t *testing.T) {
	f := newNarrowFixture(t, narrowCutoff.Format(time.RFC3339Nano))
	require.NoError(t, f.g.Create(&domain.SystemSetting{Key: narrowComponentSetsMarkerKey, Value: "2025-07-01T00:00:00Z"}).Error)
	f.seed(narrowCalendar{name: "events", set: legacyComponentSet, live: []string{calendar.ComponentEvent}})
	f.seed(narrowCalendar{name: "tasks", set: legacyComponentSet, live: []string{calendar.ComponentToDo}})
	before := f.state()

	require.NoError(t, RunDataMigrations(f.g))

	assert.Equal(t, before, f.state(), "nothing narrowed, the marker kept as it was")
}

// TestNarrowCalendarComponentSetsIsOneTransaction: the marker and every
// narrowing commit together. A failing narrowing fails the boot, rolls back the
// narrowings before it and leaves no marker, so the next boot does it all.
func TestNarrowCalendarComponentSetsIsOneTransaction(t *testing.T) {
	f := newNarrowFixture(t, narrowCutoff.Format(time.RFC3339Nano))
	first := f.seed(narrowCalendar{name: "first", set: legacyComponentSet, live: []string{calendar.ComponentEvent}})
	second := f.seed(narrowCalendar{name: "second", set: legacyComponentSet, live: []string{calendar.ComponentToDo}})
	require.Less(t, first.ID, second.ID, "the first calendar is narrowed before the second fails")
	require.NoError(t, f.g.Exec(fmt.Sprintf(`CREATE TRIGGER fail_second BEFORE INSERT ON sync_change_logs
		WHEN NEW.calendar_id = %d BEGIN SELECT RAISE(ABORT, 'injected failure'); END`, second.ID)).Error)
	firstBefore, secondBefore := f.get("first"), f.get("second")
	before := f.state()

	err := RunDataMigrations(f.g)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "narrow calendar component sets")
	assert.Contains(t, err.Error(), "injected failure")
	assert.Equal(t, before, f.state(), "the first narrowing is rolled back and no marker is written")

	require.NoError(t, f.g.Exec("DROP TRIGGER fail_second").Error)
	require.NoError(t, RunDataMigrations(f.g))
	f.assertNarrowed(t, firstBefore, 1, calendar.ComponentEvent)
	f.assertNarrowed(t, secondBefore, 1, calendar.ComponentToDo)
	assert.Len(t, f.markers(), 1)
}

// TestNarrowCalendarComponentSetsNeedsTheCutoff: without a readable
// tasks.kind_introduced_at the step fails instead of treating every calendar as
// legacy, and writes nothing (RunDataMigrations records the cutoff first).
func TestNarrowCalendarComponentSetsNeedsTheCutoff(t *testing.T) {
	for _, tc := range []struct{ name, cutoff, err string }{
		{"missing", "", "tasks.kind_introduced_at is not recorded"},
		{"unreadable", "last spring", `tasks.kind_introduced_at: parsing time "last spring"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newNarrowFixture(t, tc.cutoff)
			f.seed(narrowCalendar{name: "events", set: legacyComponentSet, live: []string{calendar.ComponentEvent}})
			before := f.state()

			err := narrowCalendarComponentSets(f.g, time.Now().UTC())

			require.ErrorContains(t, err, tc.err)
			assert.Equal(t, before, f.state(), "nothing narrowed, no marker")
		})
	}
}

// TestNarrowCalendarComponentSetsSkipsDeletedCalendars calls the step directly:
// purgeDeletedCollections, later in RunDataMigrations, removes soft-deleted
// calendars, which would hide whether the narrowing touched one.
func TestNarrowCalendarComponentSetsSkipsDeletedCalendars(t *testing.T) {
	f := newNarrowFixture(t, narrowCutoff.Format(time.RFC3339Nano))
	gone := f.seed(narrowCalendar{name: "gone", set: legacyComponentSet, live: []string{calendar.ComponentEvent}})
	require.NoError(t, f.g.Delete(gone).Error)
	f.seed(narrowCalendar{name: "live", set: legacyComponentSet, live: []string{calendar.ComponentEvent}})
	goneBefore, liveBefore := f.get("gone"), f.get("live")
	require.True(t, goneBefore.DeletedAt.Valid)
	goneChanges := f.changes(gone.ID)

	require.NoError(t, narrowCalendarComponentSets(f.g, time.Now().UTC()))

	assert.Equal(t, goneBefore, f.get("gone"), "a soft-deleted calendar is untouched")
	assert.Equal(t, goneChanges, f.changes(gone.ID))
	f.assertNarrowed(t, liveBefore, 1, calendar.ComponentEvent)
}
