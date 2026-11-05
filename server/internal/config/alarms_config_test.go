package config

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestAlarmConfigDefaultsAndEnv(t *testing.T) {
	os.Clearenv()
	cfg, err := Load("")
	assert.NoError(t, err)
	assert.True(t, cfg.Alarms.Enabled, "reminders are on by default")

	os.Setenv("CALDAV_ALARMS_ENABLED", "false")
	t.Cleanup(os.Clearenv)
	cfg, err = Load("")
	assert.NoError(t, err)
	assert.False(t, cfg.Alarms.Enabled)
}

func TestAlarmWindowConstants(t *testing.T) {
	assert.Equal(t, 24*time.Hour, AlarmLookBack)
	assert.Equal(t, 24*time.Hour, AlarmDefaultLookAhead)
	assert.Equal(t, 7*24*time.Hour, AlarmMaxLookAhead)
}
