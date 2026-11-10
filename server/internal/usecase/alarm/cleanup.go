package alarm

import (
	"context"
	"log/slog"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

const (
	// cleanupFirstDelay is how long Run waits before its first sweep, so a
	// process that is redeployed more often than the interval still sweeps.
	cleanupFirstDelay = time.Minute
	// cleanupTimeout bounds one sweep (a single DELETE statement).
	cleanupTimeout = time.Minute
)

// CleanupJob deletes acknowledgements older than the retention (spec §9.2).
// The DELETE is idempotent, so several server instances running it are harmless.
type CleanupJob struct {
	acks      calendar.AlarmAcknowledgementRepository
	interval  time.Duration
	retention time.Duration
	logger    *slog.Logger

	// now is injectable for tests; nil means time.Now.
	now        func() time.Time
	firstDelay time.Duration
}

// NewCleanupJob builds the job. interval <= 0 makes Run a no-op; a nil logger
// is slog.Default().
func NewCleanupJob(acks calendar.AlarmAcknowledgementRepository, interval, retention time.Duration, logger *slog.Logger) *CleanupJob {
	if logger == nil {
		logger = slog.Default()
	}
	return &CleanupJob{acks: acks, interval: interval, retention: retention, logger: logger, firstDelay: cleanupFirstDelay}
}

// Run sweeps once after the first delay and then every interval until ctx is
// cancelled. It blocks, so the caller owns the goroutine.
func (j *CleanupJob) Run(ctx context.Context) {
	if j.interval <= 0 {
		j.logger.Info("alarm acknowledgement cleanup disabled")
		return
	}
	j.logger.Info("alarm acknowledgement cleanup started",
		"interval", j.interval.String(), "retention", j.retention.String())

	first := time.NewTimer(j.firstDelay)
	defer first.Stop()
	select {
	case <-ctx.Done():
		j.logger.Info("alarm acknowledgement cleanup stopped")
		return
	case <-first.C:
		j.RunOnce(ctx)
	}

	ticker := time.NewTicker(j.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			j.logger.Info("alarm acknowledgement cleanup stopped")
			return
		case <-ticker.C:
			j.RunOnce(ctx)
		}
	}
}

// RunOnce deletes every acknowledgement made before now − retention. Errors
// are logged and retried on the next tick.
func (j *CleanupJob) RunOnce(ctx context.Context) {
	now := time.Now
	if j.now != nil {
		now = j.now
	}
	cutoff := now().UTC().Add(-j.retention)

	ctx, cancel := context.WithTimeout(ctx, cleanupTimeout)
	defer cancel()

	n, err := j.acks.DeleteAcknowledgedBefore(ctx, cutoff)
	if err != nil {
		j.logger.Error("alarm acknowledgement cleanup failed", "error", err)
		return
	}
	if n > 0 {
		j.logger.Info("deleted old alarm acknowledgements", "count", n, "cutoff", cutoff)
	} else {
		j.logger.Debug("no old alarm acknowledgements to delete", "cutoff", cutoff)
	}
}
