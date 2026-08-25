package http

import (
	"testing"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/stretchr/testify/assert"
)

func TestFormatIntervalRendersTheAcceptedSpelling(t *testing.T) {
	for _, d := range calendar.AllowedRefreshIntervals {
		got := formatInterval(d)
		back, err := time.ParseDuration(got)
		assert.NoError(t, err, got)
		assert.Equal(t, d, back, "%s must round-trip", got)
	}
	assert.Equal(t, "1h", formatInterval(time.Hour))
	assert.Equal(t, "15m", formatInterval(15*time.Minute))
	assert.Equal(t, "24h", formatInterval(24*time.Hour))
	// A zero stored interval falls back to the default rather than rendering
	// "0s", which the client would offer as a selectable option.
	assert.Equal(t, "1h", formatInterval(0))
}
