package repository_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/sharing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// readableNames lists userID's readable calendars and returns their names in
// the order the repository answered.
func readableNames(t *testing.T, repo *repository.CalendarRepository, userID uint) []string {
	t.Helper()
	got, err := repo.ListReadableCalendarIDs(context.Background(), userID)
	require.NoError(t, err)
	names := make([]string, 0, len(got))
	for _, c := range got {
		names = append(names, c.Name)
	}
	return names
}

// TestListReadableCalendarIDs (#170): the calendars a user can read are the
// ones they own (a subscribed mirror included, whatever its component set)
// and the ones shared with them through a live share, read or read-write —
// never another user's unshared calendar, one shared with someone else, one
// whose share was soft-deleted, or a soft-deleted calendar. One row per
// calendar, ordered by id, carrying exactly the ReadableCalendar columns.
func TestListReadableCalendarIDs(t *testing.T) {
	db := newFeedDB(t)
	repo := repository.NewCalendarRepository(db)
	ctx := context.Background()
	const alice, bob, carol uint = 1, 2, 3

	mk := func(owner uint, name, components string, subscribed bool) *calendar.Calendar {
		cal := &calendar.Calendar{UUID: uuid.NewString(), UserID: owner, Name: name, Color: "#a1b2c3",
			Timezone: "Europe/Berlin", SupportedComponents: components, Subscribed: subscribed}
		cal.Path = cal.UUID + ".ics"
		require.NoError(t, repo.Create(ctx, cal))
		return cal
	}
	share := func(cal *calendar.Calendar, with uint, permission string) *sharing.CalendarShare {
		s := &sharing.CalendarShare{UUID: uuid.NewString(), CalendarID: cal.ID, SharedWithID: with, Permission: permission}
		require.NoError(t, db.Create(s).Error)
		return s
	}

	aliceTasks := mk(alice, "alice tasks", "VTODO", false)
	aliceEvents := mk(alice, "alice events", "VEVENT", false)
	aliceFeed := mk(alice, "alice feed", "VEVENT,VTODO", true)
	aliceLegacy := mk(alice, "alice legacy", "", false)
	bobRead := mk(bob, "bob read", "VTODO", false)
	share(bobRead, alice, "read")
	bobRW := mk(bob, "bob read-write", "VEVENT,VTODO", false)
	share(bobRW, alice, "read-write")
	share(bobRW, carol, "read") // a second share of the same calendar: still one row
	mk(bob, "bob private", "VTODO", false)
	bobForCarol := mk(bob, "bob for carol", "VTODO", false)
	share(bobForCarol, carol, "read-write")
	bobRevoked := mk(bob, "bob revoked", "VTODO", false)
	require.NoError(t, db.Delete(share(bobRevoked, alice, "read")).Error) // soft-deleted share
	bobDeleted := mk(bob, "bob deleted", "VTODO", false)
	share(bobDeleted, alice, "read")
	require.NoError(t, db.Delete(bobDeleted).Error) // soft-deleted calendar, live share
	aliceDeleted := mk(alice, "alice deleted", "VTODO", false)
	require.NoError(t, db.Delete(aliceDeleted).Error)
	bobAliceOwn := mk(alice, "alice shared with bob", "VTODO", false)
	share(bobAliceOwn, bob, "read")
	share(bobAliceOwn, alice, "read") // a share with the owner herself: still one row

	t.Run("owned and shared, nothing else", func(t *testing.T) {
		assert.Equal(t, []string{
			"alice tasks", "alice events", "alice feed", "alice legacy", "bob read", "bob read-write",
			"alice shared with bob",
		}, readableNames(t, repo, alice))
		assert.Equal(t, []string{
			"bob read", "bob read-write", "bob private", "bob for carol", "bob revoked", "alice shared with bob",
		}, readableNames(t, repo, bob))
		assert.Equal(t, []string{"bob read-write", "bob for carol"}, readableNames(t, repo, carol))
	})

	t.Run("rows carry the ReadableCalendar columns", func(t *testing.T) {
		got, err := repo.ListReadableCalendarIDs(ctx, alice)
		require.NoError(t, err)
		want := []*calendar.Calendar{aliceTasks, aliceEvents, aliceFeed, aliceLegacy, bobRead, bobRW, bobAliceOwn}
		require.Len(t, got, len(want))
		for i, cal := range want {
			assert.Equal(t, calendar.ReadableCalendar{
				ID: cal.ID, UUID: cal.UUID, Name: cal.Name, Color: "#a1b2c3", SupportedComponents: cal.SupportedComponents,
			}, got[i], cal.Name)
		}
	})

	t.Run("a user with no calendar", func(t *testing.T) {
		got, err := repo.ListReadableCalendarIDs(ctx, 99)
		require.NoError(t, err)
		assert.NotNil(t, got)
		assert.Empty(t, got)
	})

	t.Run("one query", func(t *testing.T) {
		var statements []string
		require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:count_readable", func(tx *gorm.DB) {
			// Building the share subquery runs the callbacks in dry-run mode;
			// only what reaches the database counts.
			if !tx.DryRun {
				statements = append(statements, tx.Statement.SQL.String())
			}
		}))
		t.Cleanup(func() { _ = db.Callback().Query().Remove("test:count_readable") })
		_, err := repo.ListReadableCalendarIDs(ctx, alice)
		require.NoError(t, err)
		require.Len(t, statements, 1, "%v", statements)
		assert.Contains(t, statements[0], "calendar_shares", "the shares are a subquery of the one SELECT")
		assert.NotContains(t, statements[0], "*", "only the ReadableCalendar columns are read")
	})

	t.Run("a storage failure is returned", func(t *testing.T) {
		require.NoError(t, db.Exec("ALTER TABLE calendar_shares RENAME TO calendar_shares_gone").Error)
		defer func() {
			require.NoError(t, db.Exec("ALTER TABLE calendar_shares_gone RENAME TO calendar_shares").Error)
		}()
		got, err := repo.ListReadableCalendarIDs(ctx, alice)
		require.Error(t, err)
		assert.Nil(t, got)
	})
}
