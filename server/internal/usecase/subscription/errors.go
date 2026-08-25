package subscription

import "errors"

// Sentinel errors the HTTP adapter maps to status codes. Anything else is a
// feed-specific failure whose message is shown to the owner verbatim.
var (
	// ErrNotFound is returned for an unknown subscription id AND for one owned
	// by somebody else, so the endpoint cannot be used to probe which ids
	// exist.
	ErrNotFound = errors.New("subscription not found")
	// ErrInvalidInput covers request validation (name, colour, interval).
	ErrInvalidInput = errors.New("invalid input")
	// ErrLimitReached means the account is at its subscription cap.
	ErrLimitReached = errors.New("subscription limit reached")
	// ErrEmptyFeed means the URL returned a valid but eventless VCALENDAR.
	// It is refused only at creation time — a feed that legitimately empties
	// out later just syncs to an empty calendar.
	ErrEmptyFeed = errors.New("the feed contains no events")
)
