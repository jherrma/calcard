package webdav

import "testing"

// TestRequiredScopeForPath pins the scope decision to the fixed collection-type
// path segment. The critical regression case is a collection (or username)
// literally named "calendars"/"addressbooks": a naive substring match would
// flip the scope decision so it disagrees with backend routing, letting a
// caldav-only credential reach an address book (and vice versa).
func TestRequiredScopeForPath(t *testing.T) {
	cases := []struct {
		name string
		path string
		want string
	}{
		{"calendar object", "/dav/alice/calendars/work/x.ics", "caldav"},
		{"addressbook object", "/dav/alice/addressbooks/personal/x.vcf", "carddav"},
		{"calendar home", "/dav/alice/calendars/", "caldav"},
		{"addressbook home", "/dav/alice/addressbooks/", "carddav"},
		// Regression: an address book named "calendars" must stay carddav,
		// matching the router which dispatches on the "addressbooks" segment.
		{"addressbook named calendars", "/dav/alice/addressbooks/calendars/x.vcf", "carddav"},
		// Regression: a calendar named "addressbooks" must stay caldav.
		{"calendar named addressbooks", "/dav/alice/calendars/addressbooks/x.ics", "caldav"},
		// Regression: a username of "calendars"/"addressbooks" must not decide.
		{"username calendars, addressbook path", "/dav/calendars/addressbooks/x.vcf", "carddav"},
		{"username addressbooks, calendar path", "/dav/addressbooks/calendars/x.ics", "caldav"},
		// Principal / root / discovery paths carry no required scope.
		{"principal", "/dav/alice/", ""},
		{"root", "/dav/", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := requiredScopeForPath(tc.path); got != tc.want {
				t.Fatalf("requiredScopeForPath(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

// TestDavCollectionTypeMatchesRouting asserts that the scope decision and the
// backend-routing decision are derived from the same segment, so they can never
// disagree (the root cause of the bypass).
func TestDavCollectionTypeMatchesRouting(t *testing.T) {
	routesToCardDAV := func(path string) bool { return davCollectionType(path) == "addressbooks" }

	// A vCard PUT under an address book named "calendars" routes to CardDAV,
	// and its required scope must be carddav — not caldav.
	path := "/dav/alice/addressbooks/calendars/contact.vcf"
	if !routesToCardDAV(path) {
		t.Fatalf("path %q should route to CardDAV backend", path)
	}
	if scope := requiredScopeForPath(path); scope != "carddav" {
		t.Fatalf("path %q routes to CardDAV but requires scope %q", path, scope)
	}
}
