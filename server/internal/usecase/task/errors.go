package task

import (
	"errors"
	"fmt"
)

// ErrInvalidInput marks a request the caller can correct (an unknown status
// filter, invalid task fields, a calendar that does not store tasks). Handlers
// answer it with 400 and the error's own message, which is why it is built
// with invalidInputf or invalidInput: the message stays verbatim, without this
// sentinel's text in front.
var ErrInvalidInput = errors.New("invalid task input")

// ErrNotFound is returned when the UUID names no task in the given calendar:
// an unknown object, an event (VEVENT), or a task filed under another
// calendar. Handlers answer all of them with the same 404 "Task not found", so
// the URL cannot be used to probe what an object is or where it lives.
var ErrNotFound = errors.New("task not found")

// ErrCalendarNotFound is returned when the cross-list query is narrowed to a
// calendar the user cannot read: an unknown UUID and a calendar neither owned
// by nor shared with the user are the same error, so the answer (404
// "Calendar not found") cannot be used to probe which calendars exist. Never
// an empty page: the caller asked about one calendar.
var ErrCalendarNotFound = errors.New("calendar not found")

// ErrRecurringNotSupported refuses completing a recurring task over REST (spec
// §1c): clients disagree on what completing a repeating VTODO means, and
// picking one convention corrupts the data of the others. Handlers answer 409
// with this message.
var ErrRecurringNotSupported = errors.New("Repeating tasks cannot be completed here yet. Complete it in your task app.")

// invalidInputf builds an ErrInvalidInput whose message is exactly the
// formatted text.
func invalidInputf(format string, a ...any) error {
	return &invalidInputError{err: fmt.Errorf(format, a...)}
}

// invalidInput marks err, a domain refusal such as
// calendar.ErrComponentNotStored, as ErrInvalidInput while keeping its message
// verbatim (the 400 body). err stays reachable through errors.Is.
func invalidInput(err error) error {
	return &invalidInputError{err: err}
}

type invalidInputError struct{ err error }

func (e *invalidInputError) Error() string   { return e.err.Error() }
func (e *invalidInputError) Unwrap() []error { return []error{ErrInvalidInput, e.err} }
