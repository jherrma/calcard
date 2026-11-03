package database

import (
	"os"
	"testing"
	"time"

	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMigrations(t *testing.T) {
	dataDir, err := os.MkdirTemp("", "caldav-migrate-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dataDir)

	cfg := &config.Config{
		DataDir: dataDir,
		Database: config.DatabaseConfig{
			Driver:      "sqlite",
			AutoMigrate: true,
		},
	}

	db, err := New(cfg)
	require.NoError(t, err)
	defer db.Close()

	// Run migrations
	err = db.Migrate(Models()...)
	require.NoError(t, err)

	// Verify table exists by attempting to perform an operation
	gormDB := db.DB()
	hasUserTable := gormDB.Migrator().HasTable(&user.User{})
	assert.True(t, hasUserTable)

	// Verify columns
	assert.True(t, gormDB.Migrator().HasColumn(&user.User{}, "uuid"))
	assert.True(t, gormDB.Migrator().HasColumn(&user.User{}, "email"))
}

// legacyCalendarObject is calendar_objects as it was before the task columns
// (issue #170): the schema an existing installation boots the new version on.
type legacyCalendarObject struct {
	ID                uint       `gorm:"primaryKey"`
	UUID              string     `gorm:"uniqueIndex;size:36;not null"`
	CalendarID        uint       `gorm:"index;not null"`
	Path              string     `gorm:"size:255;not null"`
	UID               string     `gorm:"index;size:255;not null"`
	ETag              string     `gorm:"size:64;not null"`
	ComponentType     string     `gorm:"size:20;not null"`
	ICalData          string     `gorm:"type:text;not null"`
	ContentLength     int        `gorm:"not null"`
	Summary           string     `gorm:"size:500"`
	Description       string     `gorm:"type:text"`
	Location          string     `gorm:"size:500"`
	StartTime         *time.Time `gorm:"index"`
	EndTime           *time.Time
	RecurrenceEndTime *time.Time
	IsAllDay          bool
	CreatedAt         time.Time
	UpdatedAt         time.Time
	DeletedAt         gorm.DeletedAt `gorm:"index"`
}

func (legacyCalendarObject) TableName() string { return "calendar_objects" }

// TestMigrations_CalendarObjectTaskColumns checks that AutoMigrate alone (no
// change to Models()) creates the task columns and indexes of issue #170, both
// on a fresh database and on one that already holds calendar objects.
func TestMigrations_CalendarObjectTaskColumns(t *testing.T) {
	open := func(t *testing.T) *gorm.DB {
		t.Helper()
		db, err := New(&config.Config{
			DataDir:  t.TempDir(),
			Database: config.DatabaseConfig{Driver: "sqlite", AutoMigrate: true},
		})
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		return db.DB()
	}

	assertTaskSchema := func(t *testing.T, gormDB *gorm.DB) {
		t.Helper()
		m := gormDB.Migrator()
		for _, col := range []string{"status", "due_at", "completed_at", "percent_complete", "priority"} {
			assert.True(t, m.HasColumn(&calendar.CalendarObject{}, col), "column %s", col)
		}
		assert.True(t, m.HasIndex(&calendar.CalendarObject{}, "idx_calendar_objects_due_at"))
		assert.True(t, m.HasIndex(&calendar.CalendarObject{}, "idx_calobj_cal_comp"))
		// The single-column calendar_id index stays alongside the composite one.
		assert.True(t, m.HasIndex(&calendar.CalendarObject{}, "idx_calendar_objects_calendar_id"))

		indexes, err := m.GetIndexes(&calendar.CalendarObject{})
		require.NoError(t, err)
		var composite []string
		for _, idx := range indexes {
			if idx.Name() == "idx_calobj_cal_comp" {
				composite = idx.Columns()
			}
		}
		assert.Equal(t, []string{"calendar_id", "component_type"}, composite)
	}

	t.Run("fresh database", func(t *testing.T) {
		gormDB := open(t)
		require.NoError(t, gormDB.AutoMigrate(Models()...))
		assertTaskSchema(t, gormDB)
	})

	t.Run("existing calendar_objects table", func(t *testing.T) {
		gormDB := open(t)
		require.NoError(t, gormDB.AutoMigrate(&legacyCalendarObject{}))
		require.False(t, gormDB.Migrator().HasColumn(&calendar.CalendarObject{}, "status"))
		legacy := legacyCalendarObject{
			UUID: "11111111-1111-1111-1111-111111111111", CalendarID: 7, Path: "t.ics", UID: "t@x",
			ETag: "e", ComponentType: calendar.ComponentToDo,
			ICalData: "BEGIN:VCALENDAR\r\nBEGIN:VTODO\r\nUID:t@x\r\nPRIORITY:1\r\nEND:VTODO\r\nEND:VCALENDAR\r\n",
		}
		require.NoError(t, gormDB.Create(&legacy).Error)

		require.NoError(t, gormDB.AutoMigrate(Models()...))
		assertTaskSchema(t, gormDB)

		// The existing row survives with empty task columns (filled by the
		// backfill, not by the schema migration).
		var got calendar.CalendarObject
		require.NoError(t, gormDB.First(&got, legacy.ID).Error)
		assert.Equal(t, legacy.UUID, got.UUID)
		assert.Empty(t, got.Status)
		assert.Nil(t, got.DueAt)
		assert.Nil(t, got.CompletedAt)
		assert.Nil(t, got.PercentComplete)
		assert.Nil(t, got.Priority)

		// A second boot migrates again without error.
		require.NoError(t, gormDB.AutoMigrate(Models()...))
	})

	t.Run("task columns round-trip", func(t *testing.T) {
		gormDB := open(t)
		require.NoError(t, gormDB.AutoMigrate(Models()...))
		obj := &calendar.CalendarObject{
			UUID: "22222222-2222-2222-2222-222222222222", CalendarID: 1, Path: "t.ics", ETag: "e",
			ICalData: "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//t//EN\r\nBEGIN:VTODO\r\nUID:t@x\r\n" +
				"STATUS:in-process\r\nDUE;TZID=Europe/Berlin:20260115T100000\r\nPERCENT-COMPLETE:40\r\n" +
				"PRIORITY:5\r\nEND:VTODO\r\nEND:VCALENDAR\r\n",
		}
		require.NoError(t, obj.PopulateDenormFieldsFromICal())
		require.NoError(t, gormDB.Create(obj).Error)

		var got calendar.CalendarObject
		require.NoError(t, gormDB.First(&got, obj.ID).Error)
		assert.Equal(t, calendar.TaskStatusInProcess, got.Status)
		require.NotNil(t, got.DueAt)
		assert.True(t, got.DueAt.Equal(time.Date(2026, 1, 15, 9, 0, 0, 0, time.UTC)), "due_at = %v", got.DueAt)
		assert.Nil(t, got.CompletedAt)
		require.NotNil(t, got.PercentComplete)
		assert.Equal(t, 40, *got.PercentComplete)
		require.NotNil(t, got.Priority)
		assert.Equal(t, 5, *got.Priority)
	})
}
