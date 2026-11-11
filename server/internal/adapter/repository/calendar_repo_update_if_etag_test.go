package repository_test

import (
	"context"
	"testing"

	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUpdateCalendarObjectIfETag (MJ1, #170): the task edits write only over
// the row they loaded. A row deleted meanwhile, hard or soft, is never brought
// back (Save's INSERT fall-back did), a row another writer changed is refused,
// and neither records a change.
func TestUpdateCalendarObjectIfETag(t *testing.T) {
	db := newFeedDB(t)
	repo := repository.NewCalendarRepository(db)
	ctx := context.Background()
	cal := seedCalendar(t, repo, false)
	todo := "BEGIN:VTODO\r\nUID:u\r\nDTSTAMP:20300101T000000Z\r\nSUMMARY:u\r\nEND:VTODO\r\n"

	changes := func() int64 {
		var n int64
		require.NoError(t, db.Model(&calendar.SyncChangeLog{}).Where("calendar_id = ?", cal.ID).Count(&n).Error)
		return n
	}
	rows := func(obj *calendar.CalendarObject, unscoped bool) int64 {
		var n int64
		q := db.Model(&calendar.CalendarObject{})
		if unscoped {
			q = q.Unscoped()
		}
		require.NoError(t, q.Where("id = ?", obj.ID).Count(&n).Error)
		return n
	}
	edit := func(obj *calendar.CalendarObject) (loaded string) {
		loaded = obj.ETag
		obj.Summary = "edited"
		obj.ETag = calendar.NewETag()
		return loaded
	}

	t.Run("writes the columns, advances the sync token and logs a modification", func(t *testing.T) {
		obj := componentObject(t, repo, cal, "ok", todo)
		before := changes()
		loaded := edit(obj)
		obj.Status = ""
		require.NoError(t, repo.UpdateCalendarObjectIfETag(ctx, obj, loaded))

		stored, err := repo.GetCalendarObjectByUUID(ctx, obj.UUID)
		require.NoError(t, err)
		assert.Equal(t, "edited", stored.Summary)
		assert.Equal(t, obj.ETag, stored.ETag)
		assert.Equal(t, cal.ID, stored.CalendarID)
		assert.Equal(t, before+1, changes())
	})

	t.Run("a hard-deleted row is not brought back", func(t *testing.T) {
		obj := componentObject(t, repo, cal, "hard", todo)
		loaded := edit(obj)
		require.NoError(t, repo.HardDeleteCalendarObject(ctx, obj))
		before := changes()

		err := repo.UpdateCalendarObjectIfETag(ctx, obj, loaded)
		assert.ErrorIs(t, err, calendar.ErrNotFound)
		assert.Zero(t, rows(obj, true))
		assert.Equal(t, before, changes())
	})

	t.Run("a soft-deleted row is not brought back", func(t *testing.T) {
		obj := componentObject(t, repo, cal, "soft", todo)
		loaded := edit(obj)
		require.NoError(t, repo.DeleteCalendarObject(ctx, obj))
		before := changes()

		err := repo.UpdateCalendarObjectIfETag(ctx, obj, loaded)
		assert.ErrorIs(t, err, calendar.ErrNotFound)
		assert.Zero(t, rows(obj, false), "still deleted")
		assert.Equal(t, before, changes())
	})

	t.Run("a row another writer changed is ErrETagMismatch", func(t *testing.T) {
		obj := componentObject(t, repo, cal, "stale", todo)
		stale := *obj
		loaded := edit(obj)
		require.NoError(t, repo.UpdateCalendarObjectIfETag(ctx, obj, loaded))
		before := changes()

		loaded = edit(&stale)
		err := repo.UpdateCalendarObjectIfETag(ctx, &stale, loaded)
		assert.ErrorIs(t, err, calendar.ErrETagMismatch)
		assert.Equal(t, before, changes())
		stored, err := repo.GetCalendarObjectByUUID(ctx, obj.UUID)
		require.NoError(t, err)
		assert.Equal(t, obj.ETag, stored.ETag, "the first writer's row stands")
	})
}
