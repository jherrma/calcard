package calendar

import (
	"errors"
	"fmt"
	"time"
)

// Subscription status values, as reported by the REST API. They are derived
// from the row (see Status) rather than stored, so a status can never drift
// out of agreement with the counters it summarizes.
const (
	StatusPending  = "pending"  // created, never successfully synced
	StatusSynced   = "synced"   // last attempt succeeded
	StatusError    = "error"    // last attempt failed, retries remain
	StatusDisabled = "disabled" // auto-sync switched off after repeated failures
)

// DefaultRefreshInterval is used when a subscription is created without one.
const DefaultRefreshInterval = time.Hour

// MinRefreshInterval is the floor for any refresh interval. Polling a third
// party's feed more often than this is abusive, and no publisher regenerates
// an .ics faster than that anyway.
const MinRefreshInterval = 15 * time.Minute

// MaxRefreshBackoff caps the exponential backoff. Without a cap, a handful of
// failures on a 24h subscription would push the next attempt years out.
const MaxRefreshBackoff = 24 * time.Hour

// AllowedRefreshIntervals is the closed set a client may choose from. A closed
// set (rather than "anything >= 15m") keeps the UI a dropdown and stops a
// caller from picking 15m1s to look compliant while polling continuously.
var AllowedRefreshIntervals = []time.Duration{
	15 * time.Minute,
	30 * time.Minute,
	time.Hour,
	6 * time.Hour,
	12 * time.Hour,
	24 * time.Hour,
}

// ErrInvalidRefreshInterval is returned by ValidateRefreshInterval.
var ErrInvalidRefreshInterval = errors.New("invalid refresh interval")

// ValidateRefreshInterval reports whether d is one of AllowedRefreshIntervals.
func ValidateRefreshInterval(d time.Duration) error {
	for _, allowed := range AllowedRefreshIntervals {
		if d == allowed {
			return nil
		}
	}
	return fmt.Errorf("%w: %s (allowed: 15m, 30m, 1h, 6h, 12h, 24h)", ErrInvalidRefreshInterval, d)
}

// CalendarSubscription is the remote iCalendar feed behind a subscribed
// calendar (story 100). It is a sidecar to a Calendar row rather than columns
// on it: only a vanishing minority of calendars are subscriptions, and every
// field here is meaningless — and would have to be nulled and ignored — for a
// normal one.
//
// The Calendar it points at carries Subscribed=true, which is what makes it
// read-only to every write path. Deleting either side deletes the other (see
// the delete use case): a subscription without its calendar has nowhere to put
// events, and a subscribed calendar without its subscription would be a
// permanently frozen collection nobody can write to.
type CalendarSubscription struct {
	ID uint `gorm:"primaryKey" json:"-"`
	// UUID is the external identifier. Numeric ids are never exposed (#52).
	UUID string `gorm:"uniqueIndex;size:36;not null" json:"id"`
	// CalendarID is unique: one feed backs exactly one calendar.
	CalendarID uint `gorm:"uniqueIndex;not null" json:"-"`
	UserID     uint `gorm:"index;not null" json:"-"`
	// URL is the feed location, always stored normalized to http(s) — a
	// webcal:// URL is rewritten on the way in (see usecase/subscription).
	URL string `gorm:"size:2048;not null" json:"url"`
	// RefreshInterval is stored as a duration (int64 nanoseconds, GORM's
	// mapping for time.Duration) and rendered as "1h" over the wire.
	RefreshInterval time.Duration `gorm:"not null" json:"-"`
	// NextSyncAt is when the worker should next attempt this feed. It is
	// indexed because the worker's only query filters on it.
	NextSyncAt   time.Time  `gorm:"index;not null" json:"next_sync_at"`
	LastSyncedAt *time.Time `json:"last_synced_at"`
	// LastError is the most recent failure reason, cleared on success. It is
	// shown to the owner, so it must never contain more than the feed's own
	// response — no internal paths, no credentials from the URL.
	LastError  string `gorm:"size:500;not null;default:''" json:"last_error"`
	ErrorCount int    `gorm:"not null;default:0" json:"error_count"`
	// Enabled is switched off automatically after MaxFailures consecutive
	// errors, and back on by an explicit manual refresh or update.
	//
	// It deliberately carries NO `default:true` tag. GORM omits a zero-valued
	// field from an INSERT when the column has a default, so a row created
	// with Enabled=false would come back enabled — silently resurrecting a
	// subscription the caller meant to create switched off. Creators set the
	// value explicitly instead.
	Enabled bool `gorm:"not null" json:"enabled"`
	// ETag and LastModified back conditional GETs (RFC 9110 §13). Feeds in the
	// wild support one, the other, or neither, so both are kept.
	//
	// ETag needs an explicit column name for the same reason Calendar.CTag
	// does: GORM's naming strategy renders the field as "e_tag", and every
	// write that names the column "etag" would fail at runtime.
	ETag         string `gorm:"column:etag;size:256;not null;default:''" json:"-"`
	LastModified string `gorm:"size:64;not null;default:''" json:"-"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	Calendar Calendar `gorm:"foreignKey:CalendarID" json:"-"`
}

// TableName specifies the table name for CalendarSubscription.
func (CalendarSubscription) TableName() string {
	return "calendar_subscriptions"
}

// Status derives the reported status from the row's own counters.
func (s *CalendarSubscription) Status() string {
	switch {
	case !s.Enabled:
		return StatusDisabled
	case s.ErrorCount > 0:
		return StatusError
	case s.LastSyncedAt == nil:
		return StatusPending
	default:
		return StatusSynced
	}
}

// IsDue reports whether the worker should attempt this subscription now.
func (s *CalendarSubscription) IsDue(now time.Time) bool {
	return s.Enabled && !s.NextSyncAt.After(now)
}

// RecordSuccess clears the error state and schedules the next refresh one
// interval out. etag and lastModified are the validators the feed returned;
// they are stored verbatim for the next conditional request. A feed that stops
// sending a validator it used to send clears the stored one, so we never
// present a stale validator the origin no longer recognizes.
func (s *CalendarSubscription) RecordSuccess(now time.Time, etag, lastModified string) {
	t := now
	s.LastSyncedAt = &t
	s.LastError = ""
	s.ErrorCount = 0
	s.ETag = etag
	s.LastModified = lastModified
	s.NextSyncAt = now.Add(s.interval())
}

// RecordFailure records a failed attempt and schedules a retry with
// exponential backoff, disabling auto-sync once maxFailures consecutive
// attempts have failed.
//
// The backoff is deliberately anchored to the refresh interval rather than to
// a fixed base: a 15-minute feed should retry sooner than a daily one, and
// both should stop hammering an origin that is down. reason is truncated to
// fit the column, and is expected to be already sanitized by the caller.
func (s *CalendarSubscription) RecordFailure(now time.Time, reason string, maxFailures int) {
	s.ErrorCount++
	s.LastError = truncate(reason, 500)

	if maxFailures > 0 && s.ErrorCount >= maxFailures {
		s.Enabled = false
		// Leave NextSyncAt where the backoff would have put it: if the owner
		// re-enables the subscription without triggering a manual refresh, it
		// should not immediately re-attempt a feed that just failed N times.
	}

	backoff := s.interval()
	// Shift by ErrorCount-1, guarding the shift count so a long-broken feed
	// cannot overflow the duration into a negative value.
	if shift := s.ErrorCount - 1; shift > 0 {
		if shift > 20 {
			shift = 20
		}
		backoff <<= uint(shift)
	}
	if backoff > MaxRefreshBackoff || backoff <= 0 {
		backoff = MaxRefreshBackoff
	}
	s.NextSyncAt = now.Add(backoff)
}

// interval returns the effective refresh interval, defending against a zero or
// sub-minimum value read from a row written before validation existed.
func (s *CalendarSubscription) interval() time.Duration {
	if s.RefreshInterval < MinRefreshInterval {
		return DefaultRefreshInterval
	}
	return s.RefreshInterval
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

// FeedSyncStats reports what a single feed sync changed in the calendar.
type FeedSyncStats struct {
	Created   int `json:"created"`
	Updated   int `json:"updated"`
	Deleted   int `json:"deleted"`
	Unchanged int `json:"unchanged"`
}

// Changed reports whether the sync altered the collection at all.
func (s FeedSyncStats) Changed() bool {
	return s.Created > 0 || s.Updated > 0 || s.Deleted > 0
}
