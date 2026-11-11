package repository_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

// alarmIndexPostgresEnv names the DSN of a throwaway PostgreSQL database for the
// partial-index test; the PostgreSQL half is skipped when it is unset.
const alarmIndexPostgresEnv = "CALCARD_TEST_POSTGRES_DSN"

const alarmBoundsQuery = "SELECT id FROM calendar_objects WHERE calendar_id IN (1, 2, 3) " +
	"AND alarm_last_at >= ? AND alarm_first_at <= ? AND deleted_at IS NULL"

// seedAlarmRows fills calendar_objects with a few thousand rows of which one in
// twenty carries bounds, mostly in the past, as in production.
func seedAlarmRows(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.AutoMigrate(&calendar.CalendarObject{}))
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rows := make([]calendar.CalendarObject, 0, 3000)
	for i := 0; i < 3000; i++ {
		o := calendar.CalendarObject{UUID: fmt.Sprintf("u-%05d", i), CalendarID: uint(i%40 + 1), Path: fmt.Sprintf("%d.ics", i),
			UID: fmt.Sprintf("uid-%d", i), ETag: "e", ComponentType: "VEVENT", ICalData: "x"}
		if i%20 == 0 {
			first := base.Add(time.Duration(i) * time.Hour)
			last := first.Add(time.Hour)
			o.AlarmFirstAt, o.AlarmLastAt = &first, &last
		}
		rows = append(rows, o)
	}
	require.NoError(t, db.CreateInBatches(rows, 200).Error)
}

func TestCalendarObjectAlarmIndexSQLite(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "idx.db")), &gorm.Config{})
	require.NoError(t, err)
	seedAlarmRows(t, db)

	var def string
	require.NoError(t, db.Raw("SELECT sql FROM sqlite_master WHERE type = 'index' AND name = 'idx_calobj_cal_alarm'").Scan(&def).Error)
	require.NotEmpty(t, def, "idx_calobj_cal_alarm exists")
	require.Contains(t, def, "WHERE alarm_last_at IS NOT NULL")
	var comp string
	require.NoError(t, db.Raw("SELECT sql FROM sqlite_master WHERE type = 'index' AND name = 'idx_calobj_cal_comp'").Scan(&comp).Error)
	require.Contains(t, comp, "`calendar_id`,`component_type`")

	require.NoError(t, db.Exec("ANALYZE").Error)
	from := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	var plan []struct {
		Detail string `gorm:"column:detail"`
	}
	require.NoError(t, db.Raw("EXPLAIN QUERY PLAN "+alarmBoundsQuery, from, from.Add(48*time.Hour)).Scan(&plan).Error)
	var all []string
	for _, p := range plan {
		all = append(all, p.Detail)
	}
	t.Logf("SQLite plan:\n%s", strings.Join(all, "\n"))
	require.Contains(t, strings.Join(all, "\n"), "idx_calobj_cal_alarm")
}

func TestCalendarObjectAlarmIndexPostgres(t *testing.T) {
	dsn := os.Getenv(alarmIndexPostgresEnv)
	if dsn == "" {
		t.Skip(alarmIndexPostgresEnv + " not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec("DROP TABLE IF EXISTS calendar_objects").Error)
	t.Cleanup(func() { _ = db.Exec("DROP TABLE IF EXISTS calendar_objects") })
	seedAlarmRows(t, db)

	var def string
	require.NoError(t, db.Raw("SELECT indexdef FROM pg_indexes WHERE tablename = 'calendar_objects' AND indexname = 'idx_calobj_cal_alarm'").Scan(&def).Error)
	require.NotEmpty(t, def, "idx_calobj_cal_alarm exists")
	require.Contains(t, def, "WHERE (alarm_last_at IS NOT NULL)")

	require.NoError(t, db.Exec("ANALYZE calendar_objects").Error)
	from := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	var plan []string
	require.NoError(t, db.Raw("EXPLAIN "+alarmBoundsQuery, from, from.Add(48*time.Hour)).Scan(&plan).Error)
	t.Logf("PostgreSQL plan:\n%s", strings.Join(plan, "\n"))
	require.Contains(t, strings.Join(plan, "\n"), "idx_calobj_cal_alarm")
}
