package alarm

import (
	"context"
	"fmt"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

// MaxAcknowledgeKeys is the most keys one acknowledge request carries (spec §7.2).
const MaxAcknowledgeKeys = 500

// AcknowledgeUseCase stores a user's dismissals (spec §7.2).
type AcknowledgeUseCase struct {
	acks calendar.AlarmAcknowledgementRepository
	now  func() time.Time
}

// NewAcknowledgeUseCase builds the use case; a nil clock is time.Now.
func NewAcknowledgeUseCase(acks calendar.AlarmAcknowledgementRepository, now func() time.Time) *AcknowledgeUseCase {
	if now == nil {
		now = time.Now
	}
	return &AcknowledgeUseCase{acks: acks, now: now}
}

// validAlarmKey reports whether k matches ^[0-9a-f]{32}$.
func validAlarmKey(k string) bool {
	if len(k) != 32 {
		return false
	}
	for i := 0; i < len(k); i++ {
		c := k[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// Execute validates every key first (1 to 500, each 32 lowercase hex
// characters, duplicates allowed and collapsed) and then writes them for userID
// only. It does not check that a key belongs to one of the caller's alarms:
// the row harms nobody and the answer must not probe existence (§7.2). A
// refused batch writes nothing.
func (uc *AcknowledgeUseCase) Execute(ctx context.Context, userID uint, keys []string) error {
	if len(keys) == 0 {
		return ErrKeysRequired
	}
	if len(keys) > MaxAcknowledgeKeys {
		return ErrTooManyKeys
	}
	for i, k := range keys {
		if !validAlarmKey(k) {
			return &InvalidKeyError{Index: i}
		}
	}
	seen := make(map[string]bool, len(keys))
	unique := make([]string, 0, len(keys))
	for _, k := range keys {
		if !seen[k] {
			seen[k] = true
			unique = append(unique, k)
		}
	}
	if err := uc.acks.Acknowledge(ctx, userID, unique, uc.now().UTC()); err != nil {
		return fmt.Errorf("acknowledge alarms: %w", err)
	}
	return nil
}
