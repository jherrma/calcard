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

// ErrConflict marks a well-formed request that the calendar's current state
// refuses (#170: a PATCH that would narrow the component set). The REST layer
// answers it with 409 and the error's own message, MCP with that message as a
// tool error. Build it with Conflictf.
var ErrConflict = errors.New("calendar request conflicts with the calendar's state")

// Conflictf builds an ErrConflict error whose message is exactly the formatted
// text (no prefix), like Invalidf. %w in format is honoured.
func Conflictf(format string, a ...any) error {
	return &conflictError{err: fmt.Errorf(format, a...)}
}

type conflictError struct{ err error }

func (e *conflictError) Error() string   { return e.err.Error() }
func (e *conflictError) Unwrap() []error { return []error{ErrConflict, e.err} }

// ErrComponentNotStored marks a write refused because the target calendar does
// not store the object's component (#170): an event into a task list, a task
// into an event calendar. RequireComponent returns it; the error is also an
// ErrInvalidInput (the caller can pick another calendar), so REST answers 400.
var ErrComponentNotStored = errors.New("calendar does not store this component")

// componentNotStoredError is the error RequireComponent returns; its value is
// the refused component name.
type componentNotStoredError string

func (e componentNotStoredError) Error() string {
	switch string(e) {
	case ComponentEvent:
		return "This calendar does not store events"
	case ComponentToDo:
		return "This calendar does not store tasks"
	}
	return "This calendar does not store " + string(e) + " components"
}

func (e componentNotStoredError) Unwrap() []error {
	return []error{ErrInvalidInput, ErrComponentNotStored}
}
