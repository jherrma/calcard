package subscription

import (
	"context"
	"errors"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

// SyncOutcome describes what one refresh did.
type SyncOutcome struct {
	// NotModified is true when the feed answered 304 and nothing was touched.
	NotModified bool
	Stats       calendar.FeedSyncStats
	// Skipped is how many components in the feed could not be stored.
	Skipped int
}

// SyncUseCase refreshes a single subscription. It is shared by the background
// worker, the manual refresh endpoint and the initial sync on creation, so all
// three record status, backoff and validators identically — a manual refresh
// that forgot to clear ErrorCount would leave a working feed permanently
// showing "error".
type SyncUseCase struct {
	subRepo     calendar.CalendarSubscriptionRepository
	calRepo     calendar.CalendarRepository
	fetcher     *Fetcher
	maxFailures int
	now         func() time.Time
}

// NewSyncUseCase creates the sync use case. now may be nil (time.Now is used);
// it exists so tests can drive backoff without sleeping.
func NewSyncUseCase(
	subRepo calendar.CalendarSubscriptionRepository,
	calRepo calendar.CalendarRepository,
	fetcher *Fetcher,
	maxFailures int,
	now func() time.Time,
) *SyncUseCase {
	if now == nil {
		now = time.Now
	}
	return &SyncUseCase{
		subRepo:     subRepo,
		calRepo:     calRepo,
		fetcher:     fetcher,
		maxFailures: maxFailures,
		now:         now,
	}
}

// Sync fetches the feed and makes the calendar match it, then persists the
// subscription's new status.
//
// Both outcomes update the row: a success clears the error state and schedules
// the next attempt one interval out, a failure records the reason and backs
// off. The returned error is the failure reason, already sanitized for display
// to the subscription's owner — callers must not wrap it with anything that
// could carry the feed URL, which may contain a secret token.
//
// A persistence failure while recording the outcome is deliberately NOT
// returned in the success path: the events are already committed, and turning
// a bookkeeping problem into a reported failure would make the next run treat
// a correctly synced calendar as broken.
func (uc *SyncUseCase) Sync(ctx context.Context, sub *calendar.CalendarSubscription) (*SyncOutcome, error) {
	res, err := uc.fetcher.Fetch(ctx, sub.URL, sub.ETag, sub.LastModified)
	if err != nil {
		return nil, uc.recordFailure(ctx, sub, err)
	}

	if res.NotModified {
		sub.RecordSuccess(uc.now(), res.ETag, res.LastModified)
		_ = uc.subRepo.Update(ctx, sub)
		return &SyncOutcome{NotModified: true}, nil
	}

	feed, err := ParseFeed(res.Body)
	if err != nil {
		return nil, uc.recordFailure(ctx, sub, err)
	}

	stats, err := uc.calRepo.ReplaceFeedObjects(ctx, sub.CalendarID, feed.Objects)
	if err != nil {
		// A storage failure is ours, not the feed's. Record it so the owner
		// sees the subscription is not updating, but say nothing about the
		// internals.
		return nil, uc.recordFailure(ctx, sub, errStorage)
	}

	sub.RecordSuccess(uc.now(), res.ETag, res.LastModified)
	_ = uc.subRepo.Update(ctx, sub)

	return &SyncOutcome{Stats: stats, Skipped: feed.Skipped}, nil
}

// recordFailure persists the failed attempt and returns the reason unchanged,
// so the caller reports exactly what was stored.
func (uc *SyncUseCase) recordFailure(ctx context.Context, sub *calendar.CalendarSubscription, reason error) error {
	sub.RecordFailure(uc.now(), reason.Error(), uc.maxFailures)
	_ = uc.subRepo.Update(ctx, sub)
	return reason
}

// errStorage is the owner-facing message for a local persistence failure. It
// is deliberately NOT a FeedError: the feed did nothing wrong, so a caller
// mapping this to a 4xx would blame the user for our failure.
var errStorage = errors.New("the feed was fetched but could not be saved; it will be retried")
