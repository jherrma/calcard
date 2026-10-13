package database

import (
	"bytes"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/sharing"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
