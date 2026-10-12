package calendar

import (
	"errors"
	"fmt"
)

// ErrNotFound is returned for a calendar that does not exist AND for one the
// caller may not access. Both cases must stay indistinguishable (same error,
// same HTTP 404) so a caller cannot probe whether another user's calendar
// exists.
var ErrNotFound = errors.New("calendar not found")

// ErrInvalidInput marks a request the caller can correct (validation, a wrong
// DELETE confirmation, the last-calendar guard, an unparseable import). The
// REST layer answers it with 400 and the error's own message; any calendar
// error that is neither this nor ErrNotFound is a server failure (500).
var ErrInvalidInput = errors.New("invalid calendar request")

// Invalidf builds an ErrInvalidInput error whose message is exactly the
// formatted text (no prefix), so the 400 bodies keep the wording clients have
// always seen. %w in format is honoured.
func Invalidf(format string, a ...any) error {
	return &invalidError{err: fmt.Errorf(format, a...)}
}

type invalidError struct{ err error }

func (e *invalidError) Error() string   { return e.err.Error() }
func (e *invalidError) Unwrap() []error { return []error{ErrInvalidInput, e.err} }
