package addressbook

import "errors"

// ErrNotFound is returned when the address book does not exist or is not owned
// by the caller. The two cases share one error on purpose: the handler answers
// both with the same 404, so a foreign book cannot be told apart from a missing
// one (F05).
var ErrNotFound = errors.New("address book not found")

// ErrLastAddressBook is returned when deleting the caller's only address book.
var ErrLastAddressBook = errors.New("cannot delete your last address book")
