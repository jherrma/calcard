package event

import "errors"

// ErrInvalidInput marks a user-correctable validation failure (bad times,
// malformed RRULE, unparseable recurrence id, a calendar that does not store
// events). Handlers map it to HTTP 400.
var ErrInvalidInput = errors.New("invalid event input")

// ErrNotFound is returned by the get, update, delete and move use cases when
// the UUID names an object that is not an event: a task (VTODO), which a CalDAV
// client may have written, or turned an event into, under the same UUID (#170,
// spec §5 rule 7). Handlers answer it exactly like an unknown event: REST 404
// "Event not found", MCP "No event with id … is readable by you.".
var ErrNotFound = errors.New("event not found")

// invalidInput marks err, a domain refusal such as
// calendar.ErrComponentNotStored, as ErrInvalidInput while keeping its message
// verbatim: the handlers answer ErrInvalidInput with err.Error() as the body.
// err stays reachable through errors.Is.
func invalidInput(err error) error { return &invalidInputError{err: err} }

type invalidInputError struct{ err error }

func (e *invalidInputError) Error() string   { return e.err.Error() }
func (e *invalidInputError) Unwrap() []error { return []error{ErrInvalidInput, e.err} }
