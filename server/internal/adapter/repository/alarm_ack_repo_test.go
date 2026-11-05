package repository_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newAckDB(t *testing.T) (*gorm.DB, calendar.AlarmAcknowledgementRepository) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "ack.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&calendar.AlarmAcknowledgement{}))
	return db, repository.NewAlarmAcknowledgementRepository(db)
}

func ackKey(i int) string { return fmt.Sprintf("%032x", i) }

func ackKeys(from, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = ackKey(from + i)
	}
	return out
}

func countAcks(t *testing.T, db *gorm.DB, where string, args ...any) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.Model(&calendar.AlarmAcknowledgement{}).Where(where, args...).Count(&n).Error)
	return n
}

func TestAlarmAckRepo_OnConflictDoNothing(t *testing.T) {
	db, repo := newAckDB(t)
	ctx := context.Background()
	first := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	require.NoError(t, repo.Acknowledge(ctx, 1, []string{ackKey(1)}, first))
	require.NoError(t, repo.Acknowledge(ctx, 1, []string{ackKey(1), ackKey(2)}, first.Add(time.Hour)))
	require.NoError(t, repo.Acknowledge(ctx, 1, []string{ackKey(1), ackKey(1)}, first.Add(2*time.Hour)), "duplicates in one call")

	assert.EqualValues(t, 2, countAcks(t, db, "user_id = 1"))
	var row calendar.AlarmAcknowledgement
	require.NoError(t, db.Where("user_id = 1 AND alarm_key = ?", ackKey(1)).First(&row).Error)
	assert.True(t, row.AcknowledgedAt.Equal(first), "an existing row is left as it is")
}

func TestAlarmAckRepo_CompositeKeyIsNotAutoIncrement(t *testing.T) {
	db, repo := newAckDB(t)
	ctx := context.Background()
	now := time.Now()
	// Two keys for one user both persist (a single auto-increment key would
	// have collided on the second).
	require.NoError(t, repo.Acknowledge(ctx, 1, []string{ackKey(1)}, now))
	require.NoError(t, repo.Acknowledge(ctx, 1, []string{ackKey(2)}, now))
	// The same key for two users both persist.
	require.NoError(t, repo.Acknowledge(ctx, 2, []string{ackKey(1)}, now))
	assert.EqualValues(t, 3, countAcks(t, db, "1 = 1"))

	got, err := repo.AcknowledgedKeys(ctx, 2, []string{ackKey(1), ackKey(2)})
	require.NoError(t, err)
	assert.Equal(t, map[string]bool{ackKey(1): true}, got, "user 2 sees only their own rows")

	// Exactly the three columns, acknowledged_at stored in UTC.
	cols, err := db.Migrator().ColumnTypes(&calendar.AlarmAcknowledgement{})
	require.NoError(t, err)
	var names []string
	for _, c := range cols {
		names = append(names, c.Name())
	}
	assert.ElementsMatch(t, []string{"user_id", "alarm_key", "acknowledged_at"}, names)
}

func TestAlarmAckRepo_StoresUTC(t *testing.T) {
	db, repo := newAckDB(t)
	at := time.Date(2026, 11, 4, 17, 0, 0, 0, time.FixedZone("p5", 5*3600))
	require.NoError(t, repo.Acknowledge(context.Background(), 1, []string{ackKey(1)}, at))
	var raw string
	require.NoError(t, db.Raw("SELECT CAST(acknowledged_at AS TEXT) FROM alarm_acknowledgements").Scan(&raw).Error)
	assert.Contains(t, raw, "2026-11-04 12:00:00", "stored as the UTC wall clock")
	assert.NotContains(t, raw, "17:00:00")
}

func TestAlarmAckRepo_FiveHundredKeysAreTwoChunksInOneTransaction(t *testing.T) {
	db, repo := newAckDB(t)
	ctx := context.Background()
	require.NoError(t, repo.Acknowledge(ctx, 1, ackKeys(0, 500), time.Now()))
	assert.EqualValues(t, 500, countAcks(t, db, "user_id = 1"))

	// A failure in the second chunk (rows 300..499) writes nothing: a trigger
	// refuses one key that lands there.
	require.NoError(t, db.Exec(fmt.Sprintf(
		"CREATE TRIGGER refuse_key BEFORE INSERT ON alarm_acknowledgements WHEN NEW.alarm_key = '%s' BEGIN SELECT RAISE(ABORT, 'refused'); END", ackKey(100450))).Error)
	err := repo.Acknowledge(ctx, 2, append(ackKeys(100000, 450), ackKey(100450)), time.Now())
	require.Error(t, err)
	assert.EqualValues(t, 0, countAcks(t, db, "user_id = 2"), "the first chunk is rolled back")
}

func TestAlarmAckRepo_MoreThanFiveHundredKeysIsRefused(t *testing.T) {
	db, repo := newAckDB(t)
	require.Error(t, repo.Acknowledge(context.Background(), 1, ackKeys(0, 501), time.Now()))
	assert.EqualValues(t, 0, countAcks(t, db, "1 = 1"))
}

func TestAlarmAckRepo_AcknowledgedKeysBeyondOneBatch(t *testing.T) {
	_, repo := newAckDB(t)
	ctx := context.Background()
	require.NoError(t, repo.Acknowledge(ctx, 1, ackKeys(0, 300), time.Now()))
	require.NoError(t, repo.Acknowledge(ctx, 1, ackKeys(900, 300), time.Now())) // 900..1199

	query := ackKeys(0, 1300) // 3 batches of at most 500
	got, err := repo.AcknowledgedKeys(ctx, 1, query)
	require.NoError(t, err)
	assert.Len(t, got, 600)
	assert.True(t, got[ackKey(299)])
	assert.False(t, got[ackKey(300)])
	assert.True(t, got[ackKey(1199)])

	none, err := repo.AcknowledgedKeys(ctx, 1, nil)
	require.NoError(t, err)
	assert.Empty(t, none)
}

func TestAlarmAckRepo_DeleteAcknowledgedBeforeWithAnOffsetCutoff(t *testing.T) {
	db, repo := newAckDB(t)
	ctx := context.Background()
	base := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	require.NoError(t, repo.Acknowledge(ctx, 1, []string{ackKey(1)}, base.Add(-time.Hour)))
	require.NoError(t, repo.Acknowledge(ctx, 1, []string{ackKey(2)}, base.Add(time.Hour)))

	// 12:00 UTC written as 17:00+05:00: a text comparison against the stored
	// UTC wall clock would delete both (17:00 > 13:00) or neither.
	cutoff := base.In(time.FixedZone("p5", 5*3600))
	n, err := repo.DeleteAcknowledgedBefore(ctx, cutoff)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)
	assert.EqualValues(t, 0, countAcks(t, db, "alarm_key = ?", ackKey(1)))
	assert.EqualValues(t, 1, countAcks(t, db, "alarm_key = ?", ackKey(2)))

	n, err = repo.DeleteAcknowledgedBefore(ctx, base.Add(-48*time.Hour))
	require.NoError(t, err)
	assert.Zero(t, n)
}
