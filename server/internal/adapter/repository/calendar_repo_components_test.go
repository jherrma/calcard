package repository_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func loadCalendarRow(t *testing.T, db *gorm.DB, id uint) calendar.Calendar {
	t.Helper()
	var cal calendar.Calendar
	require.NoError(t, db.Unscoped().First(&cal, id).Error)
	return cal
}

func changeLogRows(t *testing.T, db *gorm.DB, calendarID uint) []calendar.SyncChangeLog {
	t.Helper()
	var rows []calendar.SyncChangeLog
	require.NoError(t, db.Where("calendar_id = ?", calendarID).Order("id ASC").Find(&rows).Error)
	return rows
}

// TestUpdateSupportedComponents pins the repository write behind the
// widen-only PATCH and the narrowing migration (#170): it persists
// supported_components and, in the same transaction, advances the ctag and
// sync token and appends a "collection" change-log anchor (recordChange), so a
// DAV client polling the ctag or running sync-collection notices the change.
// It writes nothing else of the calendar.
func TestUpdateSupportedComponents(t *testing.T) {
	ctx := context.Background()

	t.Run("persists the set and advances ctag, sync token and change log", func(t *testing.T) {
		db := newFeedDB(t)
		repo := repository.NewCalendarRepository(db)
		cal := seedCalendar(t, repo, false) // stores "VEVENT"
		before := loadCalendarRow(t, db, cal.ID)
		logBefore := changeLogRows(t, db, cal.ID)

		require.NoError(t, repo.UpdateSupportedComponents(ctx, cal.ID, "VEVENT,VTODO"))

		after := loadCalendarRow(t, db, cal.ID)
		assert.Equal(t, "VEVENT,VTODO", after.SupportedComponents)
		assert.NotEqual(t, before.CTag, after.CTag, "the ctag advances")
		assert.NotEqual(t, before.SyncToken, after.SyncToken, "the sync token advances")
		assert.Equal(t, after.SyncToken, after.CTag)

		logAfter := changeLogRows(t, db, cal.ID)
		require.Len(t, logAfter, len(logBefore)+1, "exactly one change-log row is appended")
		anchor := logAfter[len(logAfter)-1]
		assert.Equal(t, "collection", anchor.ChangeType)
		assert.Equal(t, "", anchor.ResourcePath)
		assert.Equal(t, after.SyncToken, anchor.SyncToken, "the new token is a real change-log row")

		// Every other column is untouched.
		after.SupportedComponents, after.SyncToken, after.CTag = before.SupportedComponents, before.SyncToken, before.CTag
		after.UpdatedAt = before.UpdatedAt
		assert.Equal(t, before, after, "only supported_components, the tokens and updated_at change")

		// The domain view of the stored row reports the new kind.
		stored, err := repo.GetByID(ctx, cal.ID)
		require.NoError(t, err)
		assert.Equal(t, calendar.KindBoth, calendar.KindOf(stored))
	})

	t.Run("each call is its own change", func(t *testing.T) {
		db := newFeedDB(t)
		repo := repository.NewCalendarRepository(db)
		cal := seedCalendar(t, repo, false)

		require.NoError(t, repo.UpdateSupportedComponents(ctx, cal.ID, "VTODO"))
		first := loadCalendarRow(t, db, cal.ID)
		require.NoError(t, repo.UpdateSupportedComponents(ctx, cal.ID, "VTODO"))
		second := loadCalendarRow(t, db, cal.ID)
		assert.Equal(t, "VTODO", second.SupportedComponents)
		assert.NotEqual(t, first.CTag, second.CTag,
			"the repository does not decide what is a no-op; the use case does")
	})

	t.Run("an unknown or deleted calendar is not found and nothing is written", func(t *testing.T) {
		db := newFeedDB(t)
		repo := repository.NewCalendarRepository(db)
		deleted := seedCalendar(t, repo, false)
		require.NoError(t, db.Delete(&calendar.Calendar{}, deleted.ID).Error) // soft delete
		deletedBefore := loadCalendarRow(t, db, deleted.ID)

		for _, id := range []uint{deleted.ID, 99999} {
			var logCount int64
			require.NoError(t, db.Model(&calendar.SyncChangeLog{}).Count(&logCount).Error)

			err := repo.UpdateSupportedComponents(ctx, id, "VEVENT,VTODO")
			assert.True(t, errors.Is(err, calendar.ErrNotFound), "id %d: %v", id, err)

			var logAfter int64
			require.NoError(t, db.Model(&calendar.SyncChangeLog{}).Count(&logAfter).Error)
			assert.Equal(t, logCount, logAfter, "id %d: no change-log row for a calendar that is not there", id)
		}
		deletedAfter := loadCalendarRow(t, db, deleted.ID)
		assert.Equal(t, deletedBefore.SupportedComponents, deletedAfter.SupportedComponents)
		assert.Equal(t, deletedBefore.CTag, deletedAfter.CTag)
	})

	t.Run("the set and the change log commit together", func(t *testing.T) {
		db := newFeedDB(t)
		repo := repository.NewCalendarRepository(db)
		cal := seedCalendar(t, repo, false)
		before := loadCalendarRow(t, db, cal.ID)

		// Make the change-log insert fail: the set must roll back with it.
		require.NoError(t, db.Exec("CREATE TRIGGER block_changelog BEFORE INSERT ON sync_change_logs "+
			"BEGIN SELECT RAISE(ABORT, 'injected change-log failure'); END").Error)
		err := repo.UpdateSupportedComponents(ctx, cal.ID, "VEVENT,VTODO")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "injected change-log failure")
		require.NoError(t, db.Exec("DROP TRIGGER block_changelog").Error)

		after := loadCalendarRow(t, db, cal.ID)
		assert.Equal(t, "VEVENT", after.SupportedComponents, "rolled back with the change log")
		assert.Equal(t, before.CTag, after.CTag)
		assert.Equal(t, before.SyncToken, after.SyncToken)
	})
}

// TestUpdateMetadataLeavesSupportedComponents pins that UpdateMetadata still
// writes only name, description, color and timezone (#170): a component set
// (or a token) on the struct it is handed is never persisted, which is why the
// widening needs UpdateSupportedComponents.
func TestUpdateMetadataLeavesSupportedComponents(t *testing.T) {
	ctx := context.Background()
	db := newFeedDB(t)
	repo := repository.NewCalendarRepository(db)
	cal := seedCalendar(t, repo, false)
	before := loadCalendarRow(t, db, cal.ID)

	edit := before
	edit.Name = "Renamed"
	edit.Description = "New description"
	edit.Color = "#abcdef"
	edit.Timezone = "Europe/Berlin"
	edit.SupportedComponents = "VEVENT,VTODO"
	edit.Path = "elsewhere.ics"
	edit.Subscribed = true
	require.NoError(t, repo.UpdateMetadata(ctx, &edit))

	after := loadCalendarRow(t, db, cal.ID)
	assert.Equal(t, "Renamed", after.Name)
	assert.Equal(t, "New description", after.Description)
	assert.Equal(t, "#abcdef", after.Color)
	assert.Equal(t, "Europe/Berlin", after.Timezone)
	assert.Equal(t, "VEVENT", after.SupportedComponents, "UpdateMetadata does not write the component set")
	assert.Equal(t, before.Path, after.Path)
	assert.False(t, after.Subscribed)
	assert.NotEqual(t, before.CTag, after.CTag, "the rename still advances the ctag")
}
