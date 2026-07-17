package database

import (
	"os"
	"strings"
	"testing"

	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/sharing"
	"github.com/jherrma/caldav-server/internal/domain/user"
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

	// Row with a quoted ETag and bare (unwrapped) VEVENT data.
	bare := &calendar.CalendarObject{
		UUID: "u-bare", CalendarID: 1, Path: "bare.ics", UID: "bare@x",
		ETag:     `"abc123"`,
		ICalData: "BEGIN:VEVENT\r\nUID:bare@x\r\nDTSTART:20260101T100000Z\r\nDTEND:20260101T110000Z\r\nEND:VEVENT\r\n",
	}
	require.NoError(t, g.Create(bare).Error)

	// Wrapped recurring row with NULL recurrence_end_time.
	rec := &calendar.CalendarObject{
		UUID: "u-rec", CalendarID: 1, Path: "rec.ics", UID: "rec@x",
		ETag:     "plain",
		ICalData: wrap("BEGIN:VEVENT\r\nUID:rec@x\r\nDTSTART:20260101T100000Z\r\nDTEND:20260101T110000Z\r\nRRULE:FREQ=DAILY;COUNT=3\r\nEND:VEVENT"),
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
