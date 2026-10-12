package webdav

import (
	"net/url"
	"path"
	"strings"
)

// canonicalDAVPath returns the one form of a /dav request path that every
// decision about the request is made on (F29). Fiber's c.Path() is the raw
// path, while emersion acts on the percent-decoded r.URL.Path and classifies it
// after path.Clean. Left alone, the two disagree: /dav/{me}/calendars (no
// slash) is a home set to emersion but never equals the home-set path it
// compares against, so it answered an empty 207, and /dav/{me}/%63alendars/...
// carried no protocol scope for Authenticate while emersion served it as a
// calendar.
//
// The canonical form is percent-decoded once, has no empty or dot segments and
// a lower-case "/dav" mount (Fiber routes that case-insensitively anyway).
// Every collection-depth path — root, principal, home set, collection, i.e. up
// to four segments — ends in a slash, the RFC 4918 §5.2 convention of treating
// a collection URL without the slash as the collection; deeper paths keep the
// trailing slash they came with. The fixed segments stay case-sensitive.
//
// slashAdded reports that the request named a collection without its slash.
// ok is false when the escapes are invalid; inDAV is false when dot segments
// lead out of /dav.
func canonicalDAVPath(raw string) (canonical string, slashAdded, inDAV, ok bool) {
	decoded, err := url.PathUnescape(raw)
	if err != nil {
		return "", false, false, false
	}
	trailing := strings.HasSuffix(decoded, "/")
	p := path.Clean("/" + decoded)
	if !strings.EqualFold(p, "/dav") && (len(p) < 5 || !strings.EqualFold(p[:5], "/dav/")) {
		return "", false, false, true
	}
	p = "/dav" + p[4:]

	if strings.Count(p, "/") <= 4 {
		return p + "/", !trailing, true, true
	}
	if trailing {
		p += "/"
	}
	return p, false, true, true
}
