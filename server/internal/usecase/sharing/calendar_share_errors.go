package sharing

import "errors"

// ErrCalendarShareNotFound is returned when the share addressed by the URL does
// not exist or belongs to a different calendar. The handler answers it with a
// 404, like any other unknown resource in the path.
var ErrCalendarShareNotFound = errors.New("share not found")
