package database

import (
	"os"
	"strings"
	"testing"

	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
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
