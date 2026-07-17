package event

import "errors"

// ErrInvalidInput marks a user-correctable validation failure (bad times,
// malformed RRULE, unparseable recurrence id). Handlers map it to HTTP 400.
var ErrInvalidInput = errors.New("invalid event input")
