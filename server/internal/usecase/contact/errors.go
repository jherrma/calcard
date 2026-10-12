package contact

import "errors"

// ErrContactNotFound is returned when the contact does not exist or does not
// live in the address book the caller named. Handlers map it to 404 — the same
// answer GET gives — rather than a 500 (F05).
var ErrContactNotFound = errors.New("contact not found")
