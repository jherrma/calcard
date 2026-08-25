package subscription

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- fakes -----------------------------------------------------------------

// fakeSubRepo is an in-memory CalendarSubscriptionRepository.
type fakeSubRepo struct {
	mu     sync.Mutex
	rows   map[string]*calendar.CalendarSubscription
	nextID uint
	// updates counts Update calls, so a test can prove the sync path persists
	// its outcome rather than only mutating the struct in memory.
	updates int
}

func newFakeSubRepo() *fakeSubRepo {
	return &fakeSubRepo{rows: map[string]*calendar.CalendarSubscription{}, nextID: 1}
}

func (f *fakeSubRepo) Create(_ context.Context, sub *calendar.CalendarSubscription) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sub.ID = f.nextID
	f.nextID++
	sub.CreatedAt = time.Now()
	cp := *sub
	f.rows[sub.UUID] = &cp
	return nil
}

func (f *fakeSubRepo) GetByUUID(_ context.Context, uuid string) (*calendar.CalendarSubscription, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	row, ok := f.rows[uuid]
	if !ok {
		return nil, nil
	}
	cp := *row
	return &cp, nil
}

func (f *fakeSubRepo) GetByCalendarID(_ context.Context, calendarID uint) (*calendar.CalendarSubscription, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, row := range f.rows {
		if row.CalendarID == calendarID {
			cp := *row
			return &cp, nil
		}
	}
	return nil, nil
}

func (f *fakeSubRepo) ListByUserID(_ context.Context, userID uint) ([]*calendar.CalendarSubscription, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*calendar.CalendarSubscription
	for _, row := range f.rows {
		if row.UserID == userID {
			cp := *row
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (f *fakeSubRepo) CountByUserID(_ context.Context, userID uint) (int64, error) {
	rows, _ := f.ListByUserID(context.Background(), userID)
	return int64(len(rows)), nil
}

func (f *fakeSubRepo) Update(_ context.Context, sub *calendar.CalendarSubscription) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates++
	cp := *sub
	f.rows[sub.UUID] = &cp
	return nil
}

func (f *fakeSubRepo) Delete(_ context.Context, id uint) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for k, row := range f.rows {
		if row.ID == id {
			delete(f.rows, k)
		}
	}
	return nil
}

func (f *fakeSubRepo) FindDue(_ context.Context, now time.Time, limit int) ([]*calendar.CalendarSubscription, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*calendar.CalendarSubscription
	for _, row := range f.rows {
		if row.IsDue(now) {
			cp := *row
			out = append(out, &cp)
		}
	}
	return out, nil
}

// fakeCalRepo implements only the CalendarRepository methods this package
// calls; everything else panics, so a new dependency cannot slip in untested.
type fakeCalRepo struct {
	calendar.CalendarRepository

	mu       sync.Mutex
	cals     map[uint]*calendar.Calendar
	objects  map[uint][]*calendar.CalendarObject
	nextID   uint
	replaces int
	failNext error
}

func newFakeCalRepo() *fakeCalRepo {
	return &fakeCalRepo{cals: map[uint]*calendar.Calendar{}, objects: map[uint][]*calendar.CalendarObject{}, nextID: 1}
}

func (f *fakeCalRepo) Create(_ context.Context, cal *calendar.Calendar) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cal.ID = f.nextID
	f.nextID++
	cp := *cal
	f.cals[cal.ID] = &cp
	return nil
}

func (f *fakeCalRepo) GetByID(_ context.Context, id uint) (*calendar.Calendar, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cal, ok := f.cals[id]
	if !ok {
		return nil, nil
	}
	cp := *cal
	return &cp, nil
}

func (f *fakeCalRepo) Delete(_ context.Context, id uint) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.cals, id)
	delete(f.objects, id)
	return nil
}

func (f *fakeCalRepo) GetEventCount(_ context.Context, calendarID uint) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return int64(len(f.objects[calendarID])), nil
}

func (f *fakeCalRepo) UpdateMetadata(_ context.Context, cal *calendar.Calendar) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *cal
	f.cals[cal.ID] = &cp
	return nil
}

func (f *fakeCalRepo) ReplaceFeedObjects(_ context.Context, calendarID uint, objs []*calendar.CalendarObject) (calendar.FeedSyncStats, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replaces++
	if f.failNext != nil {
		err := f.failNext
		f.failNext = nil
		return calendar.FeedSyncStats{}, err
	}
	prev := len(f.objects[calendarID])
	f.objects[calendarID] = objs
	return calendar.FeedSyncStats{Created: len(objs), Deleted: prev}, nil
}

// --- helpers ---------------------------------------------------------------

// feedServer serves whatever body the test currently wants and counts hits.
type feedServer struct {
	*httptest.Server
	mu    sync.Mutex
	body  string
	etag  string
	code  int
	hits  int
	delay time.Duration
}

func newFeedServer(t *testing.T, body string) *feedServer {
	t.Helper()
	fs := &feedServer{body: body, code: http.StatusOK}
	fs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fs.mu.Lock()
		defer fs.mu.Unlock()
		fs.hits++
		if fs.delay > 0 {
			time.Sleep(fs.delay)
		}
		if fs.etag != "" {
			if r.Header.Get("If-None-Match") == fs.etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", fs.etag)
		}
		if fs.code != http.StatusOK {
			w.WriteHeader(fs.code)
			return
		}
		_, _ = w.Write([]byte(fs.body))
	}))
	t.Cleanup(fs.Close)
	return fs
}

func (fs *feedServer) set(body, etag string, code int) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.body, fs.etag, fs.code = body, etag, code
}

func (fs *feedServer) hitCount() int {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return fs.hits
}

type harness struct {
	subs    *fakeSubRepo
	cals    *fakeCalRepo
	fetcher *Fetcher
	sync    *SyncUseCase
	create  *CreateUseCase
	refresh *RefreshUseCase
	update  *UpdateUseCase
	del     *DeleteUseCase
	list    *ListUseCase
	now     time.Time
}

func newHarness(t *testing.T, maxFailures int) *harness {
	t.Helper()
	h := &harness{
		subs:    newFakeSubRepo(),
		cals:    newFakeCalRepo(),
		fetcher: localFetcher(t),
		now:     time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC),
	}
	clock := func() time.Time { return h.now }
	h.sync = NewSyncUseCase(h.subs, h.cals, h.fetcher, maxFailures, clock)
	h.create = NewCreateUseCase(h.subs, h.cals, h.fetcher, 20, true, clock)
	h.refresh = NewRefreshUseCase(h.subs, h.cals, h.sync)
	h.update = NewUpdateUseCase(h.subs, h.cals, h.sync, true, clock)
	h.del = NewDeleteUseCase(h.subs, h.cals)
	h.list = NewListUseCase(h.subs, h.cals)
	return h
}

const twoEventFeed = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Test//EN\r\nX-WR-CALNAME:Moon phases\r\n" +
	"BEGIN:VEVENT\r\nUID:a@example.com\r\nDTSTAMP:20260101T000000Z\r\nDTSTART:20260301T100000Z\r\nSUMMARY:A\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nUID:b@example.com\r\nDTSTAMP:20260101T000000Z\r\nDTSTART:20260302T100000Z\r\nSUMMARY:B\r\nEND:VEVENT\r\n" +
	"END:VCALENDAR\r\n"

// --- create ----------------------------------------------------------------

func TestCreateValidatesTheFeedBeforeWritingAnything(t *testing.T) {
	h := newHarness(t, 5)
	fs := newFeedServer(t, "<html>not a feed</html>")

	_, err := h.create.Execute(context.Background(), 1, CreateInput{URL: fs.URL})

	require.ErrorIs(t, err, ErrNotCalendar)
	// REVERT PROOF: creating first and validating later leaves a calendar the
	// user has to clean up after a typo, and a subscription whose failure only
	// surfaces an hour later in a background worker.
	assert.Empty(t, h.cals.cals, "no calendar may survive a failed create")
	assert.Empty(t, h.subs.rows)
}

func TestCreatePopulatesTheCalendarAndTakesDefaultsFromTheFeed(t *testing.T) {
	h := newHarness(t, 5)
	fs := newFeedServer(t, twoEventFeed)

	view, err := h.create.Execute(context.Background(), 1, CreateInput{URL: fs.URL})
	require.NoError(t, err)

	assert.Equal(t, "Moon phases", view.Calendar.Name, "X-WR-CALNAME is the only sensible default name")
	assert.True(t, view.Calendar.Subscribed, "the calendar must be marked read-only")
	assert.Equal(t, int64(2), view.EventCount)
	assert.Equal(t, calendar.DefaultRefreshInterval, view.Subscription.RefreshInterval)
	assert.Equal(t, h.now.Add(time.Hour), view.Subscription.NextSyncAt)
	require.NotNil(t, view.Subscription.LastSyncedAt)
	assert.Equal(t, calendar.StatusSynced, view.Subscription.Status())
	assert.Len(t, h.cals.objects[view.Calendar.ID], 2)
}

func TestCreatePrefersAnExplicitNameOverTheFeeds(t *testing.T) {
	h := newHarness(t, 5)
	fs := newFeedServer(t, twoEventFeed)

	view, err := h.create.Execute(context.Background(), 1, CreateInput{URL: fs.URL, Name: "Mine", Color: "#ff0000"})
	require.NoError(t, err)
	assert.Equal(t, "Mine", view.Calendar.Name)
	assert.Equal(t, "#ff0000", view.Calendar.Color)
}

func TestCreateRejectsAnEmptyFeed(t *testing.T) {
	h := newHarness(t, 5)
	fs := newFeedServer(t, "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Test//EN\r\nBEGIN:VEVENT\r\nUID:x\r\nDTSTAMP:20260101T000000Z\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")
	fs.set("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Test//EN\r\nX-WR-CALNAME:Empty\r\nBEGIN:VTIMEZONE\r\nTZID:UTC\r\nBEGIN:STANDARD\r\nDTSTART:19700101T000000\r\nTZOFFSETFROM:+0000\r\nTZOFFSETTO:+0000\r\nEND:STANDARD\r\nEND:VTIMEZONE\r\nEND:VCALENDAR\r\n", "", http.StatusOK)

	_, err := h.create.Execute(context.Background(), 1, CreateInput{URL: fs.URL})
	assert.ErrorIs(t, err, ErrEmptyFeed)
}

func TestCreateEnforcesTheRefreshIntervalSetAndThePerUserCap(t *testing.T) {
	h := newHarness(t, 5)
	fs := newFeedServer(t, twoEventFeed)

	_, err := h.create.Execute(context.Background(), 1, CreateInput{URL: fs.URL, RefreshInterval: time.Minute})
	assert.ErrorIs(t, err, ErrInvalidInput)
	assert.Zero(t, fs.hitCount(), "validation must run before the outbound request")

	capped := NewCreateUseCase(h.subs, h.cals, h.fetcher, 1, true, func() time.Time { return h.now })
	_, err = capped.Execute(context.Background(), 1, CreateInput{URL: fs.URL})
	require.NoError(t, err)
	_, err = capped.Execute(context.Background(), 1, CreateInput{URL: fs.URL})
	assert.ErrorIs(t, err, ErrLimitReached)
}

// --- sync ------------------------------------------------------------------

func TestSyncTreatsA304AsSuccessWithoutTouchingTheCalendar(t *testing.T) {
	h := newHarness(t, 5)
	fs := newFeedServer(t, twoEventFeed)
	fs.set(twoEventFeed, `W/"v1"`, http.StatusOK)

	view, err := h.create.Execute(context.Background(), 1, CreateInput{URL: fs.URL})
	require.NoError(t, err)
	replacesAfterCreate := h.cals.replaces

	sub, err := h.subs.GetByUUID(context.Background(), view.Subscription.UUID)
	require.NoError(t, err)
	h.now = h.now.Add(2 * time.Hour)

	outcome, err := h.sync.Sync(context.Background(), sub)
	require.NoError(t, err)

	assert.True(t, outcome.NotModified)
	// REVERT PROOF: re-applying the feed on a 304 would rewrite every object
	// and bump the CTag on content the origin just told us has not changed.
	assert.Equal(t, replacesAfterCreate, h.cals.replaces)
	assert.Equal(t, h.now.Add(time.Hour), sub.NextSyncAt, "still rescheduled")
	assert.Equal(t, calendar.StatusSynced, sub.Status())
}

func TestSyncRecordsAFailureAndBacksOff(t *testing.T) {
	h := newHarness(t, 5)
	fs := newFeedServer(t, twoEventFeed)
	view, err := h.create.Execute(context.Background(), 1, CreateInput{URL: fs.URL})
	require.NoError(t, err)

	fs.set("", "", http.StatusServiceUnavailable)
	sub, _ := h.subs.GetByUUID(context.Background(), view.Subscription.UUID)
	updatesBefore := h.subs.updates

	_, err = h.sync.Sync(context.Background(), sub)
	require.Error(t, err)
	assert.Equal(t, "HTTP 503: Service Unavailable", err.Error())

	// The outcome must be PERSISTED, not just held in the struct: the worker
	// discards the struct, and a status the user cannot see is no status.
	assert.Greater(t, h.subs.updates, updatesBefore)
	stored, _ := h.subs.GetByUUID(context.Background(), view.Subscription.UUID)
	assert.Equal(t, 1, stored.ErrorCount)
	assert.Equal(t, "HTTP 503: Service Unavailable", stored.LastError)
	assert.Equal(t, calendar.StatusError, stored.Status())
	assert.True(t, stored.Enabled)

	// The events already mirrored stay put — a feed being down must not empty
	// the user's calendar.
	assert.Len(t, h.cals.objects[view.Calendar.ID], 2)
}

func TestSyncDisablesAfterRepeatedFailures(t *testing.T) {
	h := newHarness(t, 3)
	fs := newFeedServer(t, twoEventFeed)
	view, err := h.create.Execute(context.Background(), 1, CreateInput{URL: fs.URL})
	require.NoError(t, err)
	fs.set("", "", http.StatusNotFound)

	for i := 0; i < 3; i++ {
		sub, _ := h.subs.GetByUUID(context.Background(), view.Subscription.UUID)
		_, _ = h.sync.Sync(context.Background(), sub)
	}

	stored, _ := h.subs.GetByUUID(context.Background(), view.Subscription.UUID)
	assert.False(t, stored.Enabled)
	assert.Equal(t, calendar.StatusDisabled, stored.Status())
	assert.Contains(t, stored.LastError, "404")
}

func TestSyncDoesNotBlameTheFeedForAStorageFailure(t *testing.T) {
	h := newHarness(t, 5)
	fs := newFeedServer(t, twoEventFeed)
	view, err := h.create.Execute(context.Background(), 1, CreateInput{URL: fs.URL})
	require.NoError(t, err)

	sub, _ := h.subs.GetByUUID(context.Background(), view.Subscription.UUID)
	sub.ETag = "" // force a real fetch
	h.cals.failNext = errors.New("database is locked")

	_, err = h.sync.Sync(context.Background(), sub)
	require.Error(t, err)

	// A FeedError is mapped to a 4xx at the HTTP boundary. Our own storage
	// failure must not be, or the user is told their URL is wrong.
	var feedErr *FeedError
	assert.False(t, errors.As(err, &feedErr))
	assert.NotContains(t, err.Error(), "database is locked", "internals must not reach the user")
}

// --- refresh / update / delete ---------------------------------------------

func TestRefreshReEnablesADisabledSubscriptionAndIgnoresValidators(t *testing.T) {
	h := newHarness(t, 2)
	fs := newFeedServer(t, twoEventFeed)
	fs.set(twoEventFeed, `W/"v1"`, http.StatusOK)
	view, err := h.create.Execute(context.Background(), 1, CreateInput{URL: fs.URL})
	require.NoError(t, err)

	fs.set("", "", http.StatusInternalServerError)
	for i := 0; i < 2; i++ {
		sub, _ := h.subs.GetByUUID(context.Background(), view.Subscription.UUID)
		_, _ = h.sync.Sync(context.Background(), sub)
	}
	stored, _ := h.subs.GetByUUID(context.Background(), view.Subscription.UUID)
	require.False(t, stored.Enabled)

	fs.set(twoEventFeed, `W/"v1"`, http.StatusOK)
	hitsBefore := fs.hitCount()

	got, outcome, syncErr := h.refresh.Execute(context.Background(), 1, view.Subscription.UUID)
	require.NoError(t, syncErr)
	require.NotNil(t, outcome)

	// Pressing "refresh" on a subscription showing "disabled after N failures"
	// can only mean "try again now".
	assert.True(t, got.Subscription.Enabled)
	assert.Equal(t, calendar.StatusSynced, got.Subscription.Status())
	assert.False(t, outcome.NotModified, "a manual refresh must not answer 304 from the stored validator")
	assert.Greater(t, fs.hitCount(), hitsBefore)
}

func TestRefreshReportsAFailureWithoutLosingTheSubscription(t *testing.T) {
	h := newHarness(t, 5)
	fs := newFeedServer(t, twoEventFeed)
	view, err := h.create.Execute(context.Background(), 1, CreateInput{URL: fs.URL})
	require.NoError(t, err)
	fs.set("", "", http.StatusBadGateway)

	got, _, syncErr := h.refresh.Execute(context.Background(), 1, view.Subscription.UUID)
	require.Error(t, syncErr)
	// The caller still needs the updated state to render the error.
	require.NotNil(t, got)
	assert.Equal(t, calendar.StatusError, got.Subscription.Status())
}

func TestUpdateResyncsWhenTheURLChanges(t *testing.T) {
	h := newHarness(t, 5)
	first := newFeedServer(t, twoEventFeed)
	second := newFeedServer(t, "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Test//EN\r\n"+
		"BEGIN:VEVENT\r\nUID:z@example.com\r\nDTSTAMP:20260101T000000Z\r\nDTSTART:20260401T100000Z\r\nSUMMARY:Z\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")

	view, err := h.create.Execute(context.Background(), 1, CreateInput{URL: first.URL})
	require.NoError(t, err)

	newURL := second.URL
	got, err := h.update.Execute(context.Background(), 1, view.Subscription.UUID, UpdateInput{URL: &newURL})
	require.NoError(t, err)

	assert.Equal(t, newURL, got.Subscription.URL)
	// REVERT PROOF: without the immediate resync the calendar keeps showing
	// the old feed's events while the settings page insists they came from the
	// new URL.
	require.Len(t, h.cals.objects[view.Calendar.ID], 1)
	assert.Equal(t, "z@example.com", h.cals.objects[view.Calendar.ID][0].UID)
}

func TestUpdateClearsValidatorsWhenTheURLChanges(t *testing.T) {
	h := newHarness(t, 5)
	first := newFeedServer(t, twoEventFeed)
	first.set(twoEventFeed, `W/"shared"`, http.StatusOK)
	second := newFeedServer(t, twoEventFeed)
	// The new origin happens to use the same ETag value — which is legal, the
	// value is opaque and scoped to its own resource.
	second.set(twoEventFeed, `W/"shared"`, http.StatusOK)

	view, err := h.create.Execute(context.Background(), 1, CreateInput{URL: first.URL})
	require.NoError(t, err)

	newURL := second.URL
	_, err = h.update.Execute(context.Background(), 1, view.Subscription.UUID, UpdateInput{URL: &newURL})
	require.NoError(t, err)

	// REVERT PROOF: carrying the old feed's validator to a different origin can
	// produce a 304 for content we have never seen, freezing the calendar on
	// the previous feed forever.
	assert.Equal(t, 1, second.hitCount())
	stored, _ := h.subs.GetByUUID(context.Background(), view.Subscription.UUID)
	assert.Equal(t, `W/"shared"`, stored.ETag, "the NEW feed's validator is stored")
	assert.NotNil(t, stored.LastSyncedAt)
}

func TestUpdateReanchorsTheScheduleToANewInterval(t *testing.T) {
	h := newHarness(t, 5)
	fs := newFeedServer(t, twoEventFeed)
	view, err := h.create.Execute(context.Background(), 1, CreateInput{URL: fs.URL, RefreshInterval: 24 * time.Hour})
	require.NoError(t, err)
	require.Equal(t, h.now.Add(24*time.Hour), view.Subscription.NextSyncAt)

	quarter := 15 * time.Minute
	got, err := h.update.Execute(context.Background(), 1, view.Subscription.UUID, UpdateInput{RefreshInterval: &quarter})
	require.NoError(t, err)

	// Leaving NextSyncAt computed from the old interval would make a user who
	// switched from daily to quarter-hourly wait a full day for it to apply.
	assert.Equal(t, h.now.Add(15*time.Minute), got.Subscription.NextSyncAt)
}

func TestUpdateReEnablingClearsTheErrorCounter(t *testing.T) {
	h := newHarness(t, 2)
	fs := newFeedServer(t, twoEventFeed)
	view, err := h.create.Execute(context.Background(), 1, CreateInput{URL: fs.URL})
	require.NoError(t, err)

	fs.set("", "", http.StatusInternalServerError)
	for i := 0; i < 2; i++ {
		sub, _ := h.subs.GetByUUID(context.Background(), view.Subscription.UUID)
		_, _ = h.sync.Sync(context.Background(), sub)
	}

	enabled := true
	got, err := h.update.Execute(context.Background(), 1, view.Subscription.UUID, UpdateInput{Enabled: &enabled})
	require.NoError(t, err)

	// REVERT PROOF: re-enabling without clearing the counter re-trips the
	// limit on the very next failure, so the subscription switches itself off
	// again immediately.
	assert.Equal(t, 0, got.Subscription.ErrorCount)
	assert.True(t, got.Subscription.Enabled)
}

func TestDeleteRemovesBothTheSubscriptionAndItsCalendar(t *testing.T) {
	h := newHarness(t, 5)
	fs := newFeedServer(t, twoEventFeed)
	view, err := h.create.Execute(context.Background(), 1, CreateInput{URL: fs.URL})
	require.NoError(t, err)

	require.NoError(t, h.del.Execute(context.Background(), 1, view.Subscription.UUID))

	assert.Empty(t, h.subs.rows)
	assert.Empty(t, h.cals.cals, "a subscribed calendar without its subscription is unwritable wreckage")
}

func TestForeignSubscriptionsAreReportedAsNotFound(t *testing.T) {
	h := newHarness(t, 5)
	fs := newFeedServer(t, twoEventFeed)
	view, err := h.create.Execute(context.Background(), 1, CreateInput{URL: fs.URL})
	require.NoError(t, err)

	const otherUser = uint(2)
	_, err = h.update.Execute(context.Background(), otherUser, view.Subscription.UUID, UpdateInput{})
	assert.ErrorIs(t, err, ErrNotFound)
	assert.ErrorIs(t, h.del.Execute(context.Background(), otherUser, view.Subscription.UUID), ErrNotFound)
	_, _, err = h.refresh.Execute(context.Background(), otherUser, view.Subscription.UUID)
	assert.ErrorIs(t, err, ErrNotFound)

	// Not-found, not forbidden: a distinct answer would let anyone probe which
	// subscription ids exist.
	_, err = h.update.Execute(context.Background(), 1, "no-such-id", UpdateInput{})
	assert.ErrorIs(t, err, ErrNotFound)

	rows, err := h.list.Execute(context.Background(), otherUser)
	require.NoError(t, err)
	assert.Empty(t, rows)
}

// --- worker ----------------------------------------------------------------

func TestWorkerRefreshesOnlyDueSubscriptions(t *testing.T) {
	h := newHarness(t, 5)
	due := newFeedServer(t, twoEventFeed)
	notDue := newFeedServer(t, twoEventFeed)

	dueView, err := h.create.Execute(context.Background(), 1, CreateInput{URL: due.URL})
	require.NoError(t, err)
	_, err = h.create.Execute(context.Background(), 1, CreateInput{URL: notDue.URL})
	require.NoError(t, err)

	// Make one overdue.
	sub, _ := h.subs.GetByUUID(context.Background(), dueView.Subscription.UUID)
	sub.NextSyncAt = h.now.Add(-time.Minute)
	sub.ETag = ""
	require.NoError(t, h.subs.Update(context.Background(), sub))

	dueHits, notDueHits := due.hitCount(), notDue.hitCount()

	w := NewWorker(h.subs, h.sync, time.Minute, discardLogger())
	w.now = func() time.Time { return h.now }
	w.RunOnce(context.Background())

	assert.Equal(t, dueHits+1, due.hitCount())
	assert.Equal(t, notDueHits, notDue.hitCount(), "a subscription that is not due must not be fetched")
}

func TestWorkerRunReturnsWhenTheContextIsCancelled(t *testing.T) {
	h := newHarness(t, 5)
	w := NewWorker(h.subs, h.sync, 10*time.Millisecond, discardLogger())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after its context was cancelled — shutdown would hang")
	}
}

func TestWorkerWithAZeroIntervalDoesNothing(t *testing.T) {
	h := newHarness(t, 5)
	w := NewWorker(h.subs, h.sync, 0, discardLogger())

	done := make(chan struct{})
	go func() { w.Run(context.Background()); close(done) }()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("a zero interval must disable the worker, not spin a ticker")
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}
