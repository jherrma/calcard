package alarm

import (
	"errors"
	"fmt"
)

// ErrInvalidUntil marks an `until` that is not RFC 3339 with an offset.
var ErrInvalidUntil = errors.New("invalid until")

// Refusals of POST /api/v1/alarms/acknowledge; the handler answers each as a
// 400 with the error's own text.
var (
	// ErrKeysRequired: the keys list is missing or empty.
	ErrKeysRequired = errors.New("keys is required")
	// ErrTooManyKeys: more than MaxAcknowledgeKeys keys.
	ErrTooManyKeys = fmt.Errorf("Too many keys: at most %d per request", MaxAcknowledgeKeys)
)

// InvalidKeyError names the first key that is not 32 lowercase hex characters.
type InvalidKeyError struct{ Index int }

func (e *InvalidKeyError) Error() string {
	return fmt.Sprintf("Invalid alarm key at index %d", e.Index)
}
