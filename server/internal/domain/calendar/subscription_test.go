package calendar_test

import (
	"strings"
	"testing"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var refTime = time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)

func sub(interval time.Duration) *calendar.CalendarSubscription {
	return &calendar.CalendarSubscription{RefreshInterval: interval, Enabled: true}
}

func TestValidateRefreshInterval(t *testing.T) {
	for _, d := range calendar.AllowedRefreshIntervals {
		assert.NoError(t, calendar.ValidateRefreshInterval(d), d.String())
	}
	// A value just above the minimum is still refused: the set is closed, so
	// "15m1s" cannot be used to look compliant while polling continuously.
	for _, d := range []time.Duration{0, time.Minute, 15*time.Minute + time.Second, 2 * time.Hour, 48 * time.Hour} {
		assert.ErrorIs(t, calendar.ValidateRefreshInterval(d), calendar.ErrInvalidRefreshInterval, d.String())
	}
}

func TestRecordSuccessClearsErrorStateAndReschedules(t *testing.T) {
	s := sub(time.Hour)
	s.ErrorCount = 3
	s.LastError = "HTTP 503: Service Unavailable"

	s.RecordSuccess(refTime, `W/"abc"`, "Mon, 24 Aug 2026 22:02:57 GMT")

	assert.Equal(t, 0, s.ErrorCount)
	assert.Empty(t, s.LastError)
	require.NotNil(t, s.LastSyncedAt)
	assert.Equal(t, refTime, *s.LastSyncedAt)
	assert.Equal(t, refTime.Add(time.Hour), s.NextSyncAt)
	assert.Equal(t, `W/"abc"`, s.ETag)
	assert.Equal(t, "Mon, 24 Aug 2026 22:02:57 GMT", s.LastModified)
	assert.Equal(t, calendar.StatusSynced, s.Status())
}

func TestRecordSuccessDropsAValidatorTheFeedStoppedSending(t *testing.T) {
	s := sub(time.Hour)
	s.ETag = `W/"old"`

	s.RecordSuccess(refTime, "", "")

	// REVERT PROOF: keeping the old ETag here would make every later request
	// present a validator the origin no longer recognizes; a strict origin
	// answers 304 to it and the mirror freezes on stale content forever.
	assert.Empty(t, s.ETag)
}

func TestRecordFailureBacksOffExponentiallyFromTheRefreshInterval(t *testing.T) {
	s := sub(15 * time.Minute)

	s.RecordFailure(refTime, "boom", 0)
	assert.Equal(t, refTime.Add(15*time.Minute), s.NextSyncAt, "first failure retries after one interval")

	s.RecordFailure(refTime, "boom", 0)
	assert.Equal(t, refTime.Add(30*time.Minute), s.NextSyncAt)

	s.RecordFailure(refTime, "boom", 0)
	assert.Equal(t, refTime.Add(time.Hour), s.NextSyncAt)

	assert.Equal(t, 3, s.ErrorCount)
	assert.Equal(t, calendar.StatusError, s.Status())
	assert.True(t, s.Enabled, "maxFailures 0 means never auto-disable")
}

func TestRecordFailureCapsBackoffAndCannotOverflow(t *testing.T) {
	s := sub(24 * time.Hour)
	for i := 0; i < 200; i++ {
		s.RecordFailure(refTime, "boom", 0)
		// REVERT PROOF: without the shift guard, ErrorCount past ~63 shifts the
		// duration into a negative value and NextSyncAt lands in the past —
		// turning a long-broken feed into a hot loop.
		require.True(t, s.NextSyncAt.After(refTime), "attempt %d scheduled in the past", i+1)
	}
	assert.Equal(t, refTime.Add(calendar.MaxRefreshBackoff), s.NextSyncAt)
}

func TestRecordFailureDisablesAfterMaxFailures(t *testing.T) {
	s := sub(time.Hour)
	for i := 0; i < 4; i++ {
		s.RecordFailure(refTime, "boom", 5)
		require.True(t, s.Enabled, "still enabled after %d failures", i+1)
	}

	s.RecordFailure(refTime, "HTTP 503: Service Unavailable", 5)

	assert.False(t, s.Enabled)
	assert.Equal(t, calendar.StatusDisabled, s.Status())
	assert.False(t, s.IsDue(refTime.Add(100*time.Hour)), "a disabled subscription is never due")
}

func TestRecordFailureTruncatesTheReasonToTheColumnWidth(t *testing.T) {
	s := sub(time.Hour)
	s.RecordFailure(refTime, strings.Repeat("x", 900), 0)
	assert.Len(t, s.LastError, 500)
}

func TestStatusIsPendingUntilTheFirstSuccess(t *testing.T) {
	s := sub(time.Hour)
	assert.Equal(t, calendar.StatusPending, s.Status())
}

func TestIsDue(t *testing.T) {
	s := sub(time.Hour)
	s.NextSyncAt = refTime

	assert.True(t, s.IsDue(refTime), "due exactly at the scheduled instant")
	assert.True(t, s.IsDue(refTime.Add(time.Second)))
	assert.False(t, s.IsDue(refTime.Add(-time.Second)))
}

func TestIntervalFallsBackWhenTheStoredValueIsUnusable(t *testing.T) {
	// A row written before validation existed (or hand-edited) must not make
	// the server poll continuously.
	s := sub(0)
	s.RecordSuccess(refTime, "", "")
	assert.Equal(t, refTime.Add(calendar.DefaultRefreshInterval), s.NextSyncAt)
}

func TestEffectivePermissionCapsSubscribedCalendarsAtRead(t *testing.T) {
	subscribed := &calendar.Calendar{Subscribed: true}
	normal := &calendar.Calendar{}

	assert.Equal(t, calendar.PermissionRead, calendar.EffectivePermission(subscribed, calendar.PermissionOwner))
	assert.Equal(t, calendar.PermissionRead, calendar.EffectivePermission(subscribed, calendar.PermissionReadWrite))
	// No access stays no access — the cap must not hand out read.
	assert.Equal(t, calendar.PermissionNone, calendar.EffectivePermission(subscribed, calendar.PermissionNone))

	assert.Equal(t, calendar.PermissionOwner, calendar.EffectivePermission(normal, calendar.PermissionOwner))
	assert.Equal(t, calendar.PermissionReadWrite, calendar.EffectivePermission(normal, calendar.PermissionReadWrite))
	assert.Equal(t, calendar.PermissionOwner, calendar.EffectivePermission(nil, calendar.PermissionOwner))
}

func TestFeedSyncStatsChanged(t *testing.T) {
	assert.False(t, calendar.FeedSyncStats{Unchanged: 42}.Changed())
	assert.True(t, calendar.FeedSyncStats{Created: 1}.Changed())
	assert.True(t, calendar.FeedSyncStats{Updated: 1}.Changed())
	assert.True(t, calendar.FeedSyncStats{Deleted: 1}.Changed())
}
