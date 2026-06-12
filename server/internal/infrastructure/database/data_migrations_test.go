package database

import (
	"os"
	"strings"
	"testing"

	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
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
