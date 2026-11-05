package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	// maxAcknowledgeKeys is the endpoint's cap per Acknowledge call.
	maxAcknowledgeKeys = 500
	// ackInsertChunk is the rows per INSERT: 3 columns, 900 bind parameters,
	// under even the old SQLite limit of 999.
	ackInsertChunk = 300
	// ackLookupBatch is the keys per AcknowledgedKeys SELECT.
	ackLookupBatch = 500
)

type gormAlarmAckRepo struct {
	db *gorm.DB
}

// NewAlarmAcknowledgementRepository creates the GORM-based repository (#169).
func NewAlarmAcknowledgementRepository(db *gorm.DB) calendar.AlarmAcknowledgementRepository {
	return &gormAlarmAckRepo{db: db}
}

func (r *gormAlarmAckRepo) Acknowledge(ctx context.Context, userID uint, keys []string, at time.Time) error {
	if len(keys) == 0 {
		return nil
	}
	if len(keys) > maxAcknowledgeKeys {
		return fmt.Errorf("acknowledge: %d keys, at most %d", len(keys), maxAcknowledgeKeys)
	}
	at = at.UTC()
	// Collapse duplicates: one statement must not name a row twice (PostgreSQL
	// accepts it with DO NOTHING, but there is no reason to send it).
	seen := make(map[string]bool, len(keys))
	rows := make([]calendar.AlarmAcknowledgement, 0, len(keys))
	for _, k := range keys {
		if seen[k] {
			continue
		}
		seen[k] = true
		rows = append(rows, calendar.AlarmAcknowledgement{UserID: userID, AlarmKey: k, AcknowledgedAt: at})
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for start := 0; start < len(rows); start += ackInsertChunk {
			end := start + ackInsertChunk
			if end > len(rows) {
				end = len(rows)
			}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(rows[start:end]).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *gormAlarmAckRepo) AcknowledgedKeys(ctx context.Context, userID uint, keys []string) (map[string]bool, error) {
	out := map[string]bool{}
	for start := 0; start < len(keys); start += ackLookupBatch {
		end := start + ackLookupBatch
		if end > len(keys) {
			end = len(keys)
		}
		var found []string
		if err := r.db.WithContext(ctx).Model(&calendar.AlarmAcknowledgement{}).
			Where("user_id = ? AND alarm_key IN ?", userID, keys[start:end]).
			Pluck("alarm_key", &found).Error; err != nil {
			return nil, err
		}
		for _, k := range found {
			out[k] = true
		}
	}
	return out, nil
}

func (r *gormAlarmAckRepo) DeleteAcknowledgedBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	// Rows are stored in UTC; bind the cutoff in UTC too (on SQLite the column
	// is TEXT, so a cutoff in another offset would compare wrongly).
	res := r.db.WithContext(ctx).Where("acknowledged_at < ?", cutoff.UTC()).Delete(&calendar.AlarmAcknowledgement{})
	return res.RowsAffected, res.Error
}
