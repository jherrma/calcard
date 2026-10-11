package user

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// SCOPE: P02 — the rule that decides whether an access token issued at a given
// instant survives the user's last logout / password change.
func TestAcceptsAccessTokenIssuedAt(t *testing.T) {
	marker := time.Date(2026, 10, 11, 12, 0, 0, 500_000_000, time.UTC)

	t.Run("no marker accepts everything", func(t *testing.T) {
		u := &User{}
		assert.True(t, u.AcceptsAccessTokenIssuedAt(time.Time{}))
		assert.True(t, u.AcceptsAccessTokenIssuedAt(marker))
	})

	u := &User{TokensValidAfter: &marker}

	t.Run("issued before the marker is rejected", func(t *testing.T) {
		assert.False(t, u.AcceptsAccessTokenIssuedAt(marker.Add(-time.Hour)))
	})

	// The case a whole-second iat would get wrong: issued in the SAME second
	// as the invalidation but before it.
	t.Run("issued earlier within the same second is rejected", func(t *testing.T) {
		assert.False(t, u.AcceptsAccessTokenIssuedAt(marker.Add(-time.Nanosecond)))
		assert.False(t, u.AcceptsAccessTokenIssuedAt(marker.Truncate(time.Second)))
	})

	// ...and its mirror image: the replacement token change-password mints
	// right after moving the marker must live.
	t.Run("issued later within the same second is accepted", func(t *testing.T) {
		assert.True(t, u.AcceptsAccessTokenIssuedAt(marker.Add(time.Nanosecond)))
	})

	t.Run("issued at exactly the marker is accepted", func(t *testing.T) {
		assert.True(t, u.AcceptsAccessTokenIssuedAt(marker))
	})

	t.Run("comparison is time-zone independent", func(t *testing.T) {
		later := marker.Add(time.Millisecond).In(time.FixedZone("UTC+2", 2*3600))
		assert.True(t, u.AcceptsAccessTokenIssuedAt(later))
	})
}
