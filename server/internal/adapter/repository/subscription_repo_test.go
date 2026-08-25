package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newFeedDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&calendar.Calendar{},
		&calendar.CalendarObject{},
		&calendar.SyncChangeLog{},
		&calendar.CalendarSubscription{},
	))
	return db
}

func seedCalendar(t *testing.T, repo *repository.CalendarRepository, subscribed bool) *calendar.Calendar {
	t.Helper()
	cal := &calendar.Calendar{
		UUID:                uuid.New().String(),
		UserID:              1,
		Name:                "Feed",
		Color:               "#3788d8",
		Timezone:            "UTC",
		SupportedComponents: "VEVENT",
		Subscribed:          subscribed,
	}
	cal.Path = cal.UUID + ".ics"
	require.NoError(t, repo.Create(context.Background(), cal))
	return cal
}

func feedObject(uid, summary string) *calendar.CalendarObject {
	objUUID := uuid.New().String()
	obj := &calendar.CalendarObject{
		UUID: objUUID,
		UID:  uid,
		Path: objUUID + ".ics",
		ETag: calendar.NewETag(),
		ICalData: "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Test//EN\r\nBEGIN:VEVENT\r\nUID:" + uid +
			"\r\nDTSTAMP:20260101T000000Z\r\nDTSTART:20260301T100000Z\r\nSUMMARY:" + summary + "\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n",
	}
	_ = obj.PopulateDenormFieldsFromICal()
	return obj
}

func TestReplaceFeedObjectsCreatesUpdatesAndDeletes(t *testing.T) {
	db := newFeedDB(t)
	repo := repository.NewCalendarRepository(db)
	ctx := context.Background()
	cal := seedCalendar(t, repo, true)

	stats, err := repo.ReplaceFeedObjects(ctx, cal.ID, []*calendar.CalendarObject{
		feedObject("a@example.com", "A"),
		feedObject("b@example.com", "B"),
	})
	require.NoError(t, err)
	assert.Equal(t, calendar.FeedSyncStats{Created: 2}, stats)

	// The feed drops B, changes A and adds C.
	stats, err = repo.ReplaceFeedObjects(ctx, cal.ID, []*calendar.CalendarObject{
		feedObject("a@example.com", "A renamed"),
		feedObject("c@example.com", "C"),
	})
	require.NoError(t, err)
	assert.Equal(t, calendar.FeedSyncStats{Updated: 1, Created: 1, Deleted: 1}, stats)

	objs, err := repo.GetCalendarObjects(ctx, cal.ID)
	require.NoError(t, err)
	byUID := map[string]*calendar.CalendarObject{}
	for _, o := range objs {
		byUID[o.UID] = o
	}
	require.Len(t, byUID, 2)
	assert.Equal(t, "A renamed", byUID["a@example.com"].Summary)
	assert.Contains(t, byUID, "c@example.com")
	assert.NotContains(t, byUID, "b@example.com")
}

func TestReplaceFeedObjectsKeepsResourceIdentityAcrossAnUpdate(t *testing.T) {
	db := newFeedDB(t)
	repo := repository.NewCalendarRepository(db)
	ctx := context.Background()
	cal := seedCalendar(t, repo, true)

	_, err := repo.ReplaceFeedObjects(ctx, cal.ID, []*calendar.CalendarObject{feedObject("a@example.com", "A")})
	require.NoError(t, err)
	before, err := repo.GetCalendarObjects(ctx, cal.ID)
	require.NoError(t, err)
	require.Len(t, before, 1)

	_, err = repo.ReplaceFeedObjects(ctx, cal.ID, []*calendar.CalendarObject{feedObject("a@example.com", "A v2")})
	require.NoError(t, err)
	after, err := repo.GetCalendarObjects(ctx, cal.ID)
	require.NoError(t, err)
	require.Len(t, after, 1)

	// REVERT PROOF: a delete-plus-create would give the resource a new href on
	// every content change, so a DAV client sees an unrelated event appear and
	// its own one vanish instead of an edit.
	assert.Equal(t, before[0].UUID, after[0].UUID)
	assert.Equal(t, before[0].Path, after[0].Path)
	assert.NotEqual(t, before[0].ETag, after[0].ETag, "the ETag must change so caches revalidate")
	assert.Equal(t, "A v2", after[0].Summary)
}

func TestReplaceFeedObjectsLeavesAnUnchangedFeedCompletelyAlone(t *testing.T) {
	db := newFeedDB(t)
	repo := repository.NewCalendarRepository(db)
	ctx := context.Background()
	cal := seedCalendar(t, repo, true)

	objs := []*calendar.CalendarObject{feedObject("a@example.com", "A"), feedObject("b@example.com", "B")}
	_, err := repo.ReplaceFeedObjects(ctx, cal.ID, objs)
	require.NoError(t, err)

	afterFirst, err := repo.GetByID(ctx, cal.ID)
	require.NoError(t, err)
	stored, err := repo.GetCalendarObjects(ctx, cal.ID)
	require.NoError(t, err)
	etags := map[string]string{}
	for _, o := range stored {
		etags[o.UID] = o.ETag
	}

	// The publisher republishes byte-identical content, as a static .ics does
	// every hour of its life.
	stats, err := repo.ReplaceFeedObjects(ctx, cal.ID, []*calendar.CalendarObject{
		feedObject("a@example.com", "A"), feedObject("b@example.com", "B"),
	})
	require.NoError(t, err)
	assert.Equal(t, calendar.FeedSyncStats{Unchanged: 2}, stats)
	assert.False(t, stats.Changed())

	afterSecond, err := repo.GetByID(ctx, cal.ID)
	require.NoError(t, err)
	// REVERT PROOF: bumping the CTag on an unchanged feed makes every
	// connected DAV client re-download the whole collection, hourly, forever.
	assert.Equal(t, afterFirst.CTag, afterSecond.CTag)
	assert.Equal(t, afterFirst.SyncToken, afterSecond.SyncToken)

	stored, err = repo.GetCalendarObjects(ctx, cal.ID)
	require.NoError(t, err)
	for _, o := range stored {
		assert.Equal(t, etags[o.UID], o.ETag, "an untouched object keeps its ETag")
	}
}

func TestReplaceFeedObjectsGivesEveryChangeItsOwnSyncToken(t *testing.T) {
	db := newFeedDB(t)
	repo := repository.NewCalendarRepository(db)
	ctx := context.Background()
	cal := seedCalendar(t, repo, true)

	_, err := repo.ReplaceFeedObjects(ctx, cal.ID, []*calendar.CalendarObject{
		feedObject("a@example.com", "A"), feedObject("b@example.com", "B"), feedObject("c@example.com", "C"),
	})
	require.NoError(t, err)

	var rows []calendar.SyncChangeLog
	require.NoError(t, db.Where("calendar_id = ? AND change_type <> ?", cal.ID, "collection").Order("id ASC").Find(&rows).Error)
	require.Len(t, rows, 3)

	seen := map[string]bool{}
	for _, r := range rows {
		// REVERT PROOF: GetChangesSinceToken resolves a token to the FIRST row
		// carrying it. If a batch shared one token, a client resuming from it
		// would replay the entire batch on every subsequent sync, forever.
		require.False(t, seen[r.SyncToken], "duplicate sync token %q in one batch", r.SyncToken)
		seen[r.SyncToken] = true
	}

	// The collection's token must equal the LAST row's, or the client's next
	// sync starts from a token that has changes after it that it already saw.
	updated, err := repo.GetByID(ctx, cal.ID)
	require.NoError(t, err)
	assert.Equal(t, rows[len(rows)-1].SyncToken, updated.SyncToken)
	assert.Equal(t, updated.SyncToken, updated.CTag)

	// And an incremental sync from the first token really returns only the rest.
	changes, err := repo.GetChangesSinceToken(ctx, cal.ID, rows[0].SyncToken)
	require.NoError(t, err)
	assert.Len(t, changes, 2)
}

func TestReplaceFeedObjectsKeepsTheFirstOfTwoComponentsSharingAUID(t *testing.T) {
	db := newFeedDB(t)
	repo := repository.NewCalendarRepository(db)
	ctx := context.Background()
	cal := seedCalendar(t, repo, true)

	stats, err := repo.ReplaceFeedObjects(ctx, cal.ID, []*calendar.CalendarObject{
		feedObject("dup@example.com", "First"),
		feedObject("dup@example.com", "Second"),
	})
	require.NoError(t, err)

	// A duplicate would otherwise be created and then deleted again on every
	// refresh, churning the change log without end.
	assert.Equal(t, 1, stats.Created)
	objs, err := repo.GetCalendarObjects(ctx, cal.ID)
	require.NoError(t, err)
	require.Len(t, objs, 1)
	assert.Equal(t, "First", objs[0].Summary)
}

func TestDeletingACalendarRemovesItsSubscription(t *testing.T) {
	db := newFeedDB(t)
	calRepo := repository.NewCalendarRepository(db)
	subRepo := repository.NewCalendarSubscriptionRepository(db)
	ctx := context.Background()
	cal := seedCalendar(t, calRepo, true)

	require.NoError(t, subRepo.Create(ctx, &calendar.CalendarSubscription{
		UUID:            uuid.New().String(),
		CalendarID:      cal.ID,
		UserID:          1,
		URL:             "https://example.com/feed.ics",
		RefreshInterval: time.Hour,
		NextSyncAt:      time.Now(),
		Enabled:         true,
	}))

	// A calendar can be deleted from the REST endpoint or by a DAV client, and
	// neither knows subscriptions exist.
	require.NoError(t, calRepo.Delete(ctx, cal.ID))

	got, err := subRepo.GetByCalendarID(ctx, cal.ID)
	require.NoError(t, err)
	// REVERT PROOF: an orphaned subscription is not inert — the worker keeps
	// fetching a third party's feed on a schedule, forever, for a calendar the
	// user believes they deleted.
	assert.Nil(t, got)
}

func TestFindDueReturnsOnlyEnabledOverdueSubscriptionsOldestFirst(t *testing.T) {
	db := newFeedDB(t)
	calRepo := repository.NewCalendarRepository(db)
	subRepo := repository.NewCalendarSubscriptionRepository(db)
	ctx := context.Background()
	now := time.Now()

	mk := func(name string, next time.Time, enabled bool) string {
		cal := seedCalendar(t, calRepo, true)
		id := uuid.New().String()
		require.NoError(t, subRepo.Create(ctx, &calendar.CalendarSubscription{
			UUID: id, CalendarID: cal.ID, UserID: 1, URL: "https://example.com/" + name,
			RefreshInterval: time.Hour, NextSyncAt: next, Enabled: enabled,
		}))
		return id
	}
	oldest := mk("oldest", now.Add(-2*time.Hour), true)
	recent := mk("recent", now.Add(-time.Minute), true)
	mk("future", now.Add(time.Hour), true)
	mk("disabled", now.Add(-time.Hour), false)

	due, err := subRepo.FindDue(ctx, now, 10)
	require.NoError(t, err)
	require.Len(t, due, 2)
	// Oldest due first, so a backlog after downtime drains in the order it
	// accumulated rather than starving whoever waited longest.
	assert.Equal(t, oldest, due[0].UUID)
	assert.Equal(t, recent, due[1].UUID)
}

func TestSubscriptionUpdateWritesOnlyMutableColumns(t *testing.T) {
	db := newFeedDB(t)
	calRepo := repository.NewCalendarRepository(db)
	subRepo := repository.NewCalendarSubscriptionRepository(db)
	ctx := context.Background()
	cal := seedCalendar(t, calRepo, true)

	sub := &calendar.CalendarSubscription{
		UUID: uuid.New().String(), CalendarID: cal.ID, UserID: 7,
		URL: "https://example.com/feed.ics", RefreshInterval: time.Hour,
		NextSyncAt: time.Now(), Enabled: true,
	}
	require.NoError(t, subRepo.Create(ctx, sub))

	// The sync path mutates a struct it loaded earlier and writes it back; a
	// blanket Save would also rewrite ownership from that stale copy.
	stale := *sub
	stale.UserID = 0
	stale.CalendarID = 0
	stale.ErrorCount = 3
	stale.LastError = "HTTP 503: Service Unavailable"
	stale.Enabled = false
	require.NoError(t, subRepo.Update(ctx, &stale))

	got, err := subRepo.GetByUUID(ctx, sub.UUID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, uint(7), got.UserID)
	assert.Equal(t, cal.ID, got.CalendarID)
	assert.Equal(t, 3, got.ErrorCount)
	assert.False(t, got.Enabled)
}

func TestSubscriptionLookupsReturnNilForUnknownIds(t *testing.T) {
	db := newFeedDB(t)
	subRepo := repository.NewCalendarSubscriptionRepository(db)
	ctx := context.Background()

	// (nil, nil) rather than a driver error, so a handler can answer 404
	// without unwrapping GORM's sentinel.
	got, err := subRepo.GetByUUID(ctx, "does-not-exist")
	require.NoError(t, err)
	assert.Nil(t, got)

	got, err = subRepo.GetByCalendarID(ctx, 4242)
	require.NoError(t, err)
	assert.Nil(t, got)
}
