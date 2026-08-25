package subscription

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

// Worker tuning that is not worth an operator-facing knob.
const (
	// workerBatchSize bounds how many subscriptions one tick claims. It keeps
	// a large backlog (a server that was down for a day) from being fetched in
	// one burst; the leftovers are still due on the next tick.
	workerBatchSize = 50
	// workerConcurrency bounds simultaneous outbound fetches. Feeds are slow
	// and mostly idle, so a little parallelism helps a lot, but this is also
	// what stops the server from looking like a small crawler.
	workerConcurrency = 4
	// perFeedTimeout bounds one subscription's whole refresh, including the
	// database work. It is above the fetch timeout on purpose: a fetch that
	// used its full budget should still get to store what it retrieved.
	perFeedTimeout = 2 * time.Minute
)

// Worker refreshes due subscriptions on a ticker (story 100).
//
// It is deliberately a poller over a NextSyncAt column rather than a timer per
// subscription: the schedule then survives a restart with no recovery step,
// and a subscription created or retimed by another process is picked up
// without any coordination between them.
type Worker struct {
	subRepo  calendar.CalendarSubscriptionRepository
	syncUC   *SyncUseCase
	interval time.Duration
	logger   *slog.Logger

	// now is injectable for tests; nil means time.Now.
	now func() time.Time
}

func NewWorker(
	subRepo calendar.CalendarSubscriptionRepository,
	syncUC *SyncUseCase,
	interval time.Duration,
	logger *slog.Logger,
) *Worker {
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{subRepo: subRepo, syncUC: syncUC, interval: interval, logger: logger}
}

// Run refreshes due subscriptions until ctx is cancelled. It blocks, so the
// caller owns the goroutine and can wait for it to finish during shutdown.
//
// It does NOT sweep immediately on start. A process that crash-loops would
// otherwise re-fetch every feed on every boot, which is precisely the traffic
// pattern that gets a server blocked by a publisher.
func (w *Worker) Run(ctx context.Context) {
	if w.interval <= 0 {
		w.logger.Info("calendar subscription worker disabled (worker_interval is zero)")
		return
	}

	w.logger.Info("calendar subscription worker started", "interval", w.interval.String())
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("calendar subscription worker stopped")
			return
		case <-ticker.C:
			w.RunOnce(ctx)
		}
	}
}

// RunOnce processes one batch of due subscriptions. Exported so a test can
// drive a tick without waiting for one.
func (w *Worker) RunOnce(ctx context.Context) {
	now := time.Now
	if w.now != nil {
		now = w.now
	}

	due, err := w.subRepo.FindDue(ctx, now(), workerBatchSize)
	if err != nil {
		w.logger.Error("failed to list due calendar subscriptions", "error", err)
		return
	}
	if len(due) == 0 {
		return
	}

	w.logger.Debug("refreshing calendar subscriptions", "count", len(due))

	sem := make(chan struct{}, workerConcurrency)
	var wg sync.WaitGroup
	for _, sub := range due {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		go func(sub *calendar.CalendarSubscription) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			w.refresh(ctx, sub)
		}(sub)
	}
	wg.Wait()
}

// refresh syncs one subscription, logging the outcome.
//
// A failure is logged at INFO, not ERROR: a third party's feed being down is a
// normal event that the subscription's own status already records, and logging
// it as a server error would make a broken feed indistinguishable from a fault
// in this server for anyone watching the logs.
func (w *Worker) refresh(ctx context.Context, sub *calendar.CalendarSubscription) {
	ctx, cancel := context.WithTimeout(ctx, perFeedTimeout)
	defer cancel()

	outcome, err := w.syncUC.Sync(ctx, sub)
	if err != nil {
		w.logger.Info("calendar subscription refresh failed",
			"subscription_id", sub.UUID,
			"error_count", sub.ErrorCount,
			"enabled", sub.Enabled,
			"reason", err.Error(),
		)
		return
	}
	if outcome.NotModified {
		w.logger.Debug("calendar subscription unchanged", "subscription_id", sub.UUID)
		return
	}
	w.logger.Debug("calendar subscription refreshed",
		"subscription_id", sub.UUID,
		"created", outcome.Stats.Created,
		"updated", outcome.Stats.Updated,
		"deleted", outcome.Stats.Deleted,
		"skipped", outcome.Skipped,
	)
}
