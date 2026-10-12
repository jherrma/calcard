package importexport

import (
	"strings"
	"time"

	ical "github.com/emersion/go-ical"
	"github.com/google/uuid"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

// maxUIDPathLen bounds the UIDs that are used verbatim as a resource name.
// The path column holds 255 characters, and clients that mirror hrefs to disk
// hit the common 255-byte file name limit, so a long UID gets a UUID name.
const maxUIDPathLen = 200

// ensureDTStamp adds the DTSTAMP an imported VEVENT/VTODO lacks. RFC 5545
// requires it and go-ical's encoder refuses a component without one, but many
// real-world exports omit it; rejecting those events would lose the user's
// data over a bookkeeping property. As in the CalDAV PUT path, the stamp is
// the time the server stored the object, in UTC (RFC 5545 §3.8.7.2).
func ensureDTStamp(comp *ical.Component) {
	if comp.Props.Get(ical.PropDateTimeStamp) == nil {
		comp.Props.SetDateTime(ical.PropDateTimeStamp, time.Now().UTC())
	}
}

// importObjectPath picks the resource name (the last DAV path segment) for an
// imported object.
//
// A UID is an opaque string (RFC 5545 §3.8.4.7): it may contain '/', spaces,
// '?', '%' or "..". The DAV layer addresses an object by exactly one path
// segment, so "<uid>.ics" for such a UID is unreachable or, for "..", points
// outside the collection. Order of preference:
//
//  1. preferred — the path of the object being replaced, so a re-import keeps
//     the href DAV clients already know — if it is a usable segment;
//  2. "<uid><ext>" when the UID is short and made of URL-safe characters
//     (UUIDs, "id@host" and hex UIDs are, which covers nearly every client);
//  3. "<uuid><ext>", the scheme subscriptions use.
//
// A candidate that taken reports as occupied by another object is skipped:
// two objects at one path would make every lookup by path ambiguous. (A fresh
// random UUID is not checked; a collision is not a practical concern.)
// Duplicate detection never depends on the path — imports match by UID.
func importObjectPath(uid, ext, preferred string, taken func(string) bool) string {
	if preferred != "" && isUsableSegment(preferred) && !taken(preferred) {
		return preferred
	}
	if isPathSafeUID(uid) && !taken(uid+ext) {
		return uid + ext
	}
	return uuid.New().String() + ext
}

// isPathSafeUID reports whether uid can be used verbatim as a resource name:
// 1..maxUIDPathLen characters from RFC 3986's unreserved set plus '@', which
// is a valid path character and appears in most UIDs ("x@example.com").
func isPathSafeUID(uid string) bool {
	if uid == "" || len(uid) > maxUIDPathLen {
		return false
	}
	for _, r := range uid {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '.', r == '_', r == '~', r == '@':
		default:
			return false
		}
	}
	return true
}

// isUsableSegment reports whether an already stored path still works as a
// single DAV path segment. It is deliberately looser than isPathSafeUID: a
// path a DAV client chose itself (spaces and all) works, and keeping it keeps
// the client's href stable; only what cannot be one segment is rejected.
func isUsableSegment(p string) bool {
	return p != "" && p != "." && p != ".." && len(p) <= 255 && !strings.ContainsAny(p, `/\`)
}

// objectPaths is the set of resource names in use in one calendar, so the
// import can avoid placing two objects at the same path without a query per
// event. It is built from the object list the import fetches anyway.
type objectPaths map[string]bool

func newObjectPaths(objects []*calendar.CalendarObject) objectPaths {
	paths := make(objectPaths, len(objects))
	for _, obj := range objects {
		paths[obj.Path] = true
	}
	return paths
}

// assign picks a path for uid (see importObjectPath) and marks it as used. A
// path whose create then fails stays marked; that only costs a later object
// in the same import its "<uid>.ics" name, never correctness.
func (p objectPaths) assign(uid, preferred string) string {
	path := importObjectPath(uid, ".ics", preferred, func(s string) bool { return p[s] })
	p[path] = true
	return path
}
