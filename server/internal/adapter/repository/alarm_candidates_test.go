package repository_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListAlarmCandidates (#169): the bounds overlap test is inclusive on both
// edges, the calendar filter never widens, a soft-deleted object is excluded and
// an empty id list runs no query.
func TestListAlarmCandidates(t *testing.T) {
	db := newFeedDB(t)
	repo := repository.NewCalendarRepository(db)
	ctx := context.Background()
	from := time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)
	until := from.Add(24 * time.Hour)

	n := 0
	mk := func(cal uint, first, last *time.Time) *calendar.CalendarObject {
		n++
		o := &calendar.CalendarObject{UUID: fmt.Sprintf("obj-%d", n), CalendarID: cal, Path: fmt.Sprintf("%d.ics", n),
			UID: fmt.Sprintf("uid-%d", n), ETag: "e", ComponentType: "VEVENT", ICalData: "x", AlarmFirstAt: first, AlarmLastAt: last}
		require.NoError(t, db.Create(o).Error)
		return o
	}
	at := func(d time.Duration) *time.Time { t := from.Add(d); return &t }

	inside := mk(1, at(time.Hour), at(time.Hour))
	loEdge := mk(1, at(-time.Hour), at(0))                           // last == from
	hiEdge := mk(1, at(24*time.Hour), at(30*time.Hour))              // first == until
	tooEarly := mk(1, at(-3*time.Hour), at(-time.Second))            // last < from
	tooLate := mk(1, at(24*time.Hour+time.Second), at(48*time.Hour)) // first > until
	mk(1, nil, nil)                                                  // no alarm
	otherCal := mk(2, at(time.Hour), at(time.Hour))
	gone := mk(1, at(time.Hour), at(time.Hour))
	require.NoError(t, db.Delete(gone).Error)

	uuids := func(objs []*calendar.CalendarObject) []string {
		out := make([]string, 0, len(objs))
		for _, o := range objs {
			out = append(out, o.UUID)
		}
		return out
	}

	got, err := repo.ListAlarmCandidates(ctx, []uint{1}, from, until)
	require.NoError(t, err)
	assert.Equal(t, []string{inside.UUID, loEdge.UUID, hiEdge.UUID}, uuids(got), "inclusive edges, live rows of calendar 1 only")
	assert.NotContains(t, uuids(got), tooEarly.UUID)
	assert.NotContains(t, uuids(got), tooLate.UUID)
	assert.NotContains(t, uuids(got), otherCal.UUID)
	assert.NotContains(t, uuids(got), gone.UUID)

	got, err = repo.ListAlarmCandidates(ctx, []uint{1, 2}, from, until)
	require.NoError(t, err)
	assert.Len(t, got, 4)

	// A bound in another zone is the same instant.
	berlin := time.FixedZone("CET", 3600)
	got, err = repo.ListAlarmCandidates(ctx, []uint{1}, from.In(berlin), until.In(berlin))
	require.NoError(t, err)
	assert.Len(t, got, 3)

	t.Run("an empty id list matches nothing without a query", func(t *testing.T) {
		got, err := repo.ListAlarmCandidates(ctx, nil, from, until)
		require.NoError(t, err)
		assert.Empty(t, got)
	})
}

func TestUserPreferenceRepo_ListByKey(t *testing.T) {
	db := openAccountDB(t)
	repo := repository.NewUserPreferenceRepository(db)
	ctx := context.Background()
	var ids []uint
	for i, name := range []string{"a", "b", "c"} {
		u := &user.User{UUID: "lbk-" + name, Email: name + "@lbk.test", Username: "lbk" + name, PasswordHash: "x", IsActive: true}
		require.NoError(t, db.Create(u).Error)
		ids = append(ids, u.ID)
		require.NoError(t, repo.Upsert(ctx, &user.UserPreference{UserID: u.ID, Key: user.PrefTimezone, Value: fmt.Sprintf("zone-%d", i)}))
		require.NoError(t, repo.Upsert(ctx, &user.UserPreference{UserID: u.ID, Key: user.PrefTimeFormat, Value: "24h"}))
	}

	rows, err := repo.ListByKey(ctx, ids[:2], user.PrefTimezone)
	require.NoError(t, err)
	got := map[uint]string{}
	for _, r := range rows {
		got[r.UserID] = r.Value
	}
	assert.Equal(t, map[uint]string{ids[0]: "zone-0", ids[1]: "zone-1"}, got, "only the asked users, only the asked key")

	rows, err = repo.ListByKey(ctx, nil, user.PrefTimezone)
	require.NoError(t, err)
	assert.Empty(t, rows)
}
