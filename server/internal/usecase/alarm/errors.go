package alarm

import "errors"

// ErrInvalidUntil marks an `until` that is not RFC 3339 with an offset.
var ErrInvalidUntil = errors.New("invalid until")
