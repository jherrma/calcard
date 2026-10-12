package event

import "time"

// icalDateTime prepares a timed DTSTART/DTEND value for go-ical's SetDateTime,
// which writes the UTC form (…Z) only for time.UTC and otherwise copies
// t.Location().String() into a TZID parameter. That is right for a named IANA
// zone — one from the request's `timezone` or from the event's stored TZID —
// but not for the zones an RFC 3339 timestamp parses into (F22): an offset like
// +05:00 becomes an unnamed time.FixedZone, written as an empty TZID that every
// reader takes for UTC (the instant shifts by the offset), and an offset that
// happens to equal the server's own becomes time.Local, written as TZID=Local
// (no client can resolve it) or, with TZ set, as the server's zone, which the
// client never asked for. A time read back from the database may be in
// time.Local too. Those values are absolute instants, so they are stored in UTC
// (RFC 5545 3.3.5, form #2): the same instant, now unambiguous.
func icalDateTime(t time.Time) time.Time {
	loc := t.Location()
	if loc == time.Local || loc.String() == "" || loc.String() == "UTC" {
		return t.UTC()
	}
	return t
}
