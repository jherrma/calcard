package repository_test

import (
	"context"
	"testing"

	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHardDeleteCalendarObject (#170, the REST task DELETE): the row is gone
// for good — not even an Unscoped read finds it — and the removal is recorded
// exactly like a soft delete's: a "deleted" change-log row with the path, the
// UID and the calendar's new sync token (= ctag), which is what a
// sync-collection REPORT reports. A second delete of the same row is
// calendar.ErrNotFound and records nothing.
func TestHardDeleteCalendarObject(t *testing.T) {
	db := newFeedDB(t)
	repo := repository.NewCalendarRepository(db)
	ctx := context.Background()
	cal := seedCalendar(t, repo, false)

	todo := func(uid string) string {
		return "BEGIN:VTODO\r\nUID:" + uid + "\r\nDTSTAMP:20300101T000000Z\r\nSUMMARY:" + uid + "\r\nEND:VTODO\r\n"
	}
	gone := componentObject(t, repo, cal, "gone", todo("gone"))
	keep := componentObject(t, repo, cal, "keep", todo("keep"))
	soft := componentObject(t, repo, cal, "soft", todo("soft"))

	before, err := repo.GetByID(ctx, cal.ID)
	require.NoError(t, err)
	countChanges := func() int64 {
		var n int64
		require.NoError(t, db.Model(&calendar.SyncChangeLog{}).Where("calendar_id = ?", cal.ID).Count(&n).Error)
		return n
	}
	countRows := func(obj *calendar.CalendarObject) int64 {
		var n int64
		require.NoError(t, db.Unscoped().Model(&calendar.CalendarObject{}).Where("id = ?", obj.ID).Count(&n).Error)
		return n
	}
	changesBefore := countChanges()

	require.NoError(t, repo.HardDeleteCalendarObject(ctx, gone))

	assert.Zero(t, countRows(gone), "no row left, not even a soft-deleted one")
	assert.EqualValues(t, 1, countRows(keep), "the other object is untouched")
	_, err = repo.GetCalendarObjectByUUID(ctx, gone.UUID)
	assert.Error(t, err)

	after, err := repo.GetByID(ctx, cal.ID)
	require.NoError(t, err)
	assert.NotEqual(t, before.SyncToken, after.SyncToken, "the sync token advances")
	assert.Equal(t, after.SyncToken, after.CTag)
	assert.Equal(t, changesBefore+1, countChanges())

	changes, err := repo.GetChangesSinceToken(ctx, cal.ID, before.SyncToken)
	require.NoError(t, err)
	require.Len(t, changes, 1, "the delta since the old token is the removal")
	assert.Equal(t, "deleted", changes[0].ChangeType)
	assert.Equal(t, "gone.ics", changes[0].ResourcePath)
	assert.Equal(t, "gone", changes[0].ResourceUID)
	assert.Equal(t, after.SyncToken, changes[0].SyncToken)

	initial, err := repo.GetChangesSinceToken(ctx, cal.ID, "")
	require.NoError(t, err)
	var members []string
	for _, c := range initial {
		members = append(members, c.ResourcePath)
	}
	assert.ElementsMatch(t, []string{"keep.ics", "soft.ics"}, members, "an initial sync lists what is left")

	t.Run("a second delete is ErrNotFound and records nothing", func(t *testing.T) {
		err := repo.HardDeleteCalendarObject(ctx, gone)
		assert.ErrorIs(t, err, calendar.ErrNotFound)
		again, err := repo.GetByID(ctx, cal.ID)
		require.NoError(t, err)
		assert.Equal(t, after.SyncToken, again.SyncToken, "no new token")
		assert.Equal(t, changesBefore+1, countChanges(), "no second change-log row")
	})

	t.Run("unlike DeleteCalendarObject, which keeps a tombstone", func(t *testing.T) {
		require.NoError(t, repo.DeleteCalendarObject(ctx, soft))
		assert.EqualValues(t, 1, countRows(soft), "the soft delete leaves the row with deleted_at")

		// MN2: a tombstone is "already gone". Removing it would answer success
		// for a task nobody can see and log a second removal.
		changesAfterSoft := countChanges()
		tokenAfterSoft, err := repo.GetByID(ctx, cal.ID)
		require.NoError(t, err)
		err = repo.HardDeleteCalendarObject(ctx, soft)
		assert.ErrorIs(t, err, calendar.ErrNotFound, "a soft-deleted tombstone is not removed by a hard delete")
		assert.EqualValues(t, 1, countRows(soft), "the tombstone stays")
		assert.Equal(t, changesAfterSoft, countChanges(), "no second change-log row")
		unchanged, err := repo.GetByID(ctx, cal.ID)
		require.NoError(t, err)
		assert.Equal(t, tokenAfterSoft.SyncToken, unchanged.SyncToken, "no new token")
	})
}
