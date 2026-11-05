package alarm

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cleanupAcks records DeleteAcknowledgedBefore calls and applies a strict
// "acknowledged_at < cutoff" rule to its rows, like the real repository.
type cleanupAcks struct {
	calendar.AlarmAcknowledgementRepository
	mu      sync.Mutex
	rows    map[string]time.Time
	cutoffs []time.Time
	err     error
	called  chan struct{}
}

func newCleanupAcks() *cleanupAcks {
	return &cleanupAcks{rows: map[string]time.Time{}, called: make(chan struct{}, 16)}
}

func (c *cleanupAcks) DeleteAcknowledgedBefore(_ context.Context, cutoff time.Time) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cutoffs = append(c.cutoffs, cutoff)
	defer func() { c.called <- struct{}{} }()
	if c.err != nil {
		return 0, c.err
	}
	var n int64
	for k, at := range c.rows {
		if at.Before(cutoff) {
			delete(c.rows, k)
			n++
		}
	}
	return n, nil
}

func (c *cleanupAcks) calls() []time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Time(nil), c.cutoffs...)
}

func TestCleanupRunOnceCutoffBoundary(t *testing.T) {
	// A non-UTC clock proves the cutoff is converted to UTC.
	zone := time.FixedZone("x", 5*3600)
	now := time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC)
	cutoff := now.Add(-720 * time.Hour)

	acks := newCleanupAcks()
	acks.rows["old"] = cutoff.Add(-time.Second)
	acks.rows["exact"] = cutoff
	acks.rows["new"] = cutoff.Add(time.Second)

	j := NewCleanupJob(acks, time.Hour, 720*time.Hour, nil)
	j.now = func() time.Time { return now.In(zone) }
	j.RunOnce(context.Background())

	got := acks.calls()
	require.Len(t, got, 1)
	assert.True(t, got[0].Equal(cutoff))
	assert.Equal(t, time.UTC, got[0].Location())
	assert.NotContains(t, acks.rows, "old")
	assert.Contains(t, acks.rows, "exact", "a row exactly at the cutoff is kept")
	assert.Contains(t, acks.rows, "new")
}

func TestCleanupRunOnceRetentionSign(t *testing.T) {
	now := time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC)
	acks := newCleanupAcks()
	acks.rows["recent"] = now.Add(-time.Hour)
	j := NewCleanupJob(acks, time.Hour, 192*time.Hour, nil)
	j.now = func() time.Time { return now }
	j.RunOnce(context.Background())
	assert.Equal(t, now.Add(-192*time.Hour), acks.calls()[0], "cutoff lies in the past")
	assert.Contains(t, acks.rows, "recent")
}

func TestCleanupRunOnceLogsCountAndErrors(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	now := time.Now()
	acks := newCleanupAcks()
	acks.rows["a"] = now.Add(-2 * time.Hour)
	j := NewCleanupJob(acks, time.Hour, time.Hour, logger)
	j.RunOnce(context.Background())
	assert.Contains(t, buf.String(), "level=INFO")
	assert.Contains(t, buf.String(), "count=1")

	buf.Reset()
	j.RunOnce(context.Background())
	assert.Contains(t, buf.String(), "level=DEBUG", "nothing deleted is debug, not info")

	buf.Reset()
	acks.err = errors.New("db down")
	j.RunOnce(context.Background())
	assert.Contains(t, buf.String(), "level=ERROR")
	assert.Contains(t, buf.String(), "db down")

	// Retried on the next tick once the database is back.
	acks.err = nil
	acks.rows["b"] = now.Add(-2 * time.Hour)
	j.RunOnce(context.Background())
	assert.NotContains(t, acks.rows, "b")
}

func TestCleanupRunDisabledAtZeroInterval(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	acks := newCleanupAcks()
	j := NewCleanupJob(acks, 0, time.Hour, logger)
	j.firstDelay = time.Millisecond

	done := make(chan struct{})
	go func() { j.Run(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run must return at once when the interval is 0")
	}
	assert.Contains(t, buf.String(), "alarm acknowledgement cleanup disabled")
	assert.Empty(t, acks.calls())
}

func TestCleanupRunSweepsAfterFirstDelayThenEveryInterval(t *testing.T) {
	acks := newCleanupAcks()
	j := NewCleanupJob(acks, 30*time.Millisecond, time.Hour, nil)
	j.firstDelay = 20 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { j.Run(ctx); close(done) }()

	for i := 0; i < 3; i++ {
		select {
		case <-acks.called:
		case <-time.After(3 * time.Second):
			t.Fatalf("sweep %d did not happen", i+1)
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run must return after cancel")
	}
}

func TestCleanupRunNoSweepBeforeFirstDelayAndCancelDuringIt(t *testing.T) {
	acks := newCleanupAcks()
	j := NewCleanupJob(acks, time.Millisecond, time.Hour, nil)
	j.firstDelay = time.Hour

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { j.Run(ctx); close(done) }()
	time.Sleep(50 * time.Millisecond)
	assert.Empty(t, acks.calls(), "no sweep during the first delay")
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run must return when cancelled during the first delay")
	}
	assert.Empty(t, acks.calls())
}

func TestNewCleanupJobDefaultFirstDelay(t *testing.T) {
	assert.Equal(t, time.Minute, NewCleanupJob(nil, time.Hour, time.Hour, nil).firstDelay)
}
