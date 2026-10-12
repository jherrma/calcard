package calendar

import "errors"

// ErrNotFound is returned for a calendar that does not exist AND for one the
// caller may not access. Both cases must stay indistinguishable (same error,
// same HTTP 404) so a caller cannot probe whether another user's calendar
// exists.
var ErrNotFound = errors.New("calendar not found")
