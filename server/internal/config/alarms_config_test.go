package config

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestAlarmWindowConstants(t *testing.T) {
	assert.Equal(t, 24*time.Hour, AlarmLookBack)
	assert.Equal(t, 24*time.Hour, AlarmDefaultLookAhead)
	assert.Equal(t, 7*24*time.Hour, AlarmMaxLookAhead)
}

func TestAlarmCleanupConfigDefaultsEnvAndClamps(t *testing.T) {
	t.Cleanup(os.Clearenv)
	os.Clearenv()
	cfg, err := Load("")
	assert.NoError(t, err)
	assert.Equal(t, 24*time.Hour, cfg.Alarms.CleanupInterval)
	assert.Equal(t, 720*time.Hour, cfg.Alarms.AckRetention)

	os.Setenv("CALDAV_ALARMS_CLEANUP_INTERVAL", "6h")
	os.Setenv("CALDAV_ALARMS_ACK_RETENTION", "240h")
	cfg, err = Load("")
	assert.NoError(t, err)
	assert.Equal(t, 6*time.Hour, cfg.Alarms.CleanupInterval)
	assert.Equal(t, 240*time.Hour, cfg.Alarms.AckRetention)

	// 0 switches the job off and stays 0; retention has no "off".
	os.Setenv("CALDAV_ALARMS_CLEANUP_INTERVAL", "0")
	os.Setenv("CALDAV_ALARMS_ACK_RETENTION", "0")
	cfg, err = Load("")
	assert.NoError(t, err)
	assert.Equal(t, time.Duration(0), cfg.Alarms.CleanupInterval)
	assert.Equal(t, 720*time.Hour, cfg.Alarms.AckRetention)

	os.Setenv("CALDAV_ALARMS_CLEANUP_INTERVAL", "-5m")
	os.Setenv("CALDAV_ALARMS_ACK_RETENTION", "-1h")
	cfg, err = Load("")
	assert.NoError(t, err)
	assert.Equal(t, 24*time.Hour, cfg.Alarms.CleanupInterval)
	assert.Equal(t, 720*time.Hour, cfg.Alarms.AckRetention)
}

func TestAlarmAckRetentionFloor(t *testing.T) {
	assert.Equal(t, AlarmMaxLookAhead+AlarmLookBack, AlarmAckRetentionFloor)
	assert.Equal(t, 192*time.Hour, AlarmAckRetentionFloor)

	base := func(r time.Duration) *Config {
		return &Config{BaseURL: "http://localhost:8080", Alarms: AlarmConfig{AckRetention: r}}
	}
	err := base(100 * time.Hour).Validate()
	if assert.Error(t, err) {
		assert.Contains(t, err.Error(), "CALDAV_ALARMS_ACK_RETENTION must be at least 192h")
	}
	assert.Error(t, base(AlarmAckRetentionFloor-time.Nanosecond).Validate())
	assert.NoError(t, base(192*time.Hour).Validate())
	assert.NoError(t, base(720*time.Hour).Validate())
	assert.NoError(t, base(0).Validate(), "an unset literal config is not a misconfiguration")
}
