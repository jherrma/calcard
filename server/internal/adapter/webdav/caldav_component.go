package webdav

import (
	"net/http"
	"slices"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/caldav"
)

// calendarComponentSet is the CALDAV:supported-calendar-component-set of every
// CalCard calendar collection (RFC 4791 §5.2.3). mapCalendar advertises exactly
// this list and checkSupportedComponents enforces it, so what a client is told
// and what a PUT may store cannot drift apart.
var calendarComponentSet = []string{ical.CompEvent, ical.CompToDo}

// checkSupportedComponents refuses a calendar object holding a component the
// collection does not store, such as a VJOURNAL or a VFREEBUSY, with the
// CALDAV:supported-calendar-component precondition (RFC 4791 §5.3.2.1).
// VTIMEZONE is not a calendar component in that sense and is always allowed.
//
// The answer is a 403, not the 409 caldav.NewPreconditionError carries: the
// collection will never accept the component, so resubmitting cannot succeed
// (RFC 4918 §16). go-webdav serves the outer status with the inner DAV:error
// body.
func checkSupportedComponents(cal *ical.Calendar) error {
	for _, comp := range cal.Children {
		if comp.Name == ical.CompTimezone || slices.Contains(calendarComponentSet, comp.Name) {
			continue
		}
		return webdav.NewHTTPError(http.StatusForbidden,
			caldav.NewPreconditionError(caldav.PreconditionSupportedCalendarComponent))
	}
	return nil
}
