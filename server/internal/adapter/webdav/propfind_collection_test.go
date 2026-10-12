package webdav

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/xml"
	"io"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/sharing"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// discoveryBody requests displayname, getctag, sync-token, supported-report-set
// and a bogus <foo/> (which must land in the 404 propstat).
const discoveryBody = `<?xml version="1.0" encoding="utf-8" ?>
<D:propfind xmlns:D="DAV:" xmlns:CS="http://calendarserver.org/ns/">
  <D:prop>
    <D:displayname/>
    <CS:getctag/>
    <D:sync-token/>
    <D:supported-report-set/>
    <D:foo/>
  </D:prop>
</D:propfind>`

// propstatByStatus returns the local-name -> inner-text map for the propstat
// whose Status contains the given code (e.g. "200", "404").
func propstatByStatus(t *testing.T, ms SyncMultiStatus, code string) map[string]string {
	t.Helper()
	require.Len(t, ms.Responses, 1)
	for _, ps := range ms.Responses[0].PropStat {
		if strings.Contains(ps.Status, code) {
			out := map[string]string{}
			for _, raw := range ps.Prop.Raw {
				out[raw.XMLName.Local] = string(raw.Inner)
			}
			return out
		}
	}
	return map[string]string{}
}

// TestCollectionPropfindServesSyncDiscovery is the regression test for #41: a
// Depth:0 PROPFIND on a collection must serve sync-token / getctag /
// supported-report-set / displayname so clients discover the sync REPORT.
func TestCollectionPropfindServesSyncDiscovery(t *testing.T) {
	app, db, _ := setupTestApp(t)
	defer db.Close()

	userRepo := repository.NewUserRepository(db.DB())
	calRepo := repository.NewCalendarRepository(db.DB())
	abRepo := repository.NewAddressBookRepository(db.DB())
	passwordHash, _ := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.DefaultCost)
	u := &user.User{UUID: "u", Email: "u@example.com", Username: "u", PasswordHash: string(passwordHash), IsActive: true}
	require.NoError(t, userRepo.Create(context.Background(), u))
	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte("u@example.com:password"))

	cal := &calendar.Calendar{UserID: u.ID, UUID: "c", Path: "cal", Name: "My Calendar", Timezone: "UTC", CTag: "ctag-cal", SyncToken: "data:,synctoken-cal"}
	require.NoError(t, calRepo.Create(context.Background(), cal))
	ab := &addressbook.AddressBook{UUID: "ab", UserID: u.ID, Path: "book", Name: "My Book", CTag: "ctag-ab", SyncToken: "data:,synctoken-ab"}
	require.NoError(t, abRepo.Create(context.Background(), ab))

	do := func(path string) SyncMultiStatus {
		req, _ := http.NewRequest("PROPFIND", path, bytes.NewReader([]byte(discoveryBody)))
		req.Header.Set("Authorization", authHeader)
		req.Header.Set("Content-Type", "application/xml")
		req.Header.Set("Depth", "0")
		resp, err := app.Test(req, davTestTimeout)
		require.NoError(t, err)
		require.Equal(t, http.StatusMultiStatus, resp.StatusCode)
		var ms SyncMultiStatus
		require.NoError(t, xml.NewDecoder(resp.Body).Decode(&ms))
		return ms
	}

	t.Run("calendar", func(t *testing.T) {
		// Read back the authoritative stored values (Create may re-mint).
		stored, err := calRepo.GetByPath(context.Background(), u.ID, "cal")
		require.NoError(t, err)

		ms := do("/dav/u/calendars/cal/")
		ok := propstatByStatus(t, ms, "200")
		assert.Equal(t, "My Calendar", ok["displayname"])
		assert.Equal(t, stored.CTag, ok["getctag"], "getctag must equal the calendar's CTag")
		assert.Equal(t, stored.SyncToken, ok["sync-token"])
		assert.Contains(t, ok, "supported-report-set")
		assert.Contains(t, ok["supported-report-set"], "sync-collection")

		nf := propstatByStatus(t, ms, "404")
		assert.Contains(t, nf, "foo", "an unknown prop must sit in the 404 propstat")
	})

	t.Run("addressbook", func(t *testing.T) {
		ms := do("/dav/u/addressbooks/book/")
		ok := propstatByStatus(t, ms, "200")
		assert.Equal(t, "My Book", ok["displayname"])
		assert.NotEmpty(t, ok["getctag"])
		assert.Contains(t, ok["supported-report-set"], "addressbook-multiget")

		nf := propstatByStatus(t, ms, "404")
		assert.Contains(t, nf, "foo")
	})
}

// calendarPropsBody / bookPropsBody ask for every collection property a client
// discovers a calendar or address book with, plus a bogus <foo/>.
const calendarPropsBody = `<?xml version="1.0" encoding="utf-8"?>
<D:propfind xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav" xmlns:CS="http://calendarserver.org/ns/" xmlns:A="http://apple.com/ns/ical/">
  <D:prop>
    <D:resourcetype/><D:displayname/><C:calendar-description/><C:supported-calendar-component-set/>
    <C:supported-calendar-data/><D:sync-token/><CS:getctag/><D:supported-report-set/>
    <D:current-user-privilege-set/><D:current-user-principal/><A:calendar-color/><D:getetag/><D:foo/>
  </D:prop>
</D:propfind>`

const bookPropsBody = `<?xml version="1.0" encoding="utf-8"?>
<D:propfind xmlns:D="DAV:" xmlns:CR="urn:ietf:params:xml:ns:carddav" xmlns:CS="http://calendarserver.org/ns/">
  <D:prop>
    <D:resourcetype/><D:displayname/><CR:addressbook-description/><CR:supported-address-data/>
    <CR:max-resource-size/><D:sync-token/><CS:getctag/><D:supported-report-set/>
    <D:current-user-privilege-set/><D:current-user-principal/><D:getetag/><D:foo/>
  </D:prop>
</D:propfind>`

// responseProps returns, for the response with the given href, the
// local-name -> inner-XML map of its 200 propstat and the local names of its
// 404 propstat. Fails the test if the multistatus has no such response.
func responseProps(t *testing.T, ms SyncMultiStatus, href string) (map[string]string, []string) {
	t.Helper()
	for _, r := range ms.Responses {
		if r.Href != href {
			continue
		}
		ok := map[string]string{}
		var missing []string
		for _, ps := range r.PropStat {
			for _, raw := range ps.Prop.Raw {
				if strings.Contains(ps.Status, "200") {
					ok[raw.XMLName.Local] = string(raw.Inner)
				} else {
					missing = append(missing, raw.XMLName.Local)
				}
			}
		}
		sort.Strings(missing)
		return ok, missing
	}
	t.Fatalf("no response for %s", href)
	return nil, nil
}

// TestCollectionPropsSameAtEveryDepth is the regression test for F07: a
// collection must report the same properties whether it is the target of a
// Depth: 0 PROPFIND, listed by a Depth: 1 PROPFIND on its home, or the first
// entry of a Depth: 1 PROPFIND on itself. Before the fix Depth 0 lacked
// calendar-description / supported-calendar-component-set, Depth 1 lacked
// sync-token / getctag, a read-only share advertised write at Depth 1, and
// calendar-color was returned by neither.
func TestCollectionPropsSameAtEveryDepth(t *testing.T) {
	app, db, _ := setupTestApp(t)
	defer db.Close()
	ctx := context.Background()

	userRepo := repository.NewUserRepository(db.DB())
	calRepo := repository.NewCalendarRepository(db.DB())
	abRepo := repository.NewAddressBookRepository(db.DB())
	passwordHash, _ := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	u := &user.User{UUID: "u", Email: "u@example.com", Username: "u", PasswordHash: string(passwordHash), IsActive: true}
	require.NoError(t, userRepo.Create(ctx, u))
	owner := &user.User{UUID: "o", Email: "o@example.com", Username: "o", PasswordHash: string(passwordHash), IsActive: true}
	require.NoError(t, userRepo.Create(ctx, owner))
	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte("u@example.com:password"))

	cal := &calendar.Calendar{UserID: u.ID, UUID: "c", Path: "cal", Name: "Work", Description: "Work & stuff", Color: "#FF8800", Timezone: "UTC"}
	require.NoError(t, calRepo.Create(ctx, cal))
	ab := &addressbook.AddressBook{UUID: "ab", UserID: u.ID, Path: "book", Name: "Friends", Description: "Close friends"}
	require.NoError(t, abRepo.Create(ctx, ab))
	// A calendar of another user, shared read-only with u.
	theirs := &calendar.Calendar{UserID: owner.ID, UUID: "shared-cal-uuid", Path: "theirs", Name: "Theirs", Color: "#00AA00", Timezone: "UTC"}
	require.NoError(t, calRepo.Create(ctx, theirs))
	require.NoError(t, db.DB().Create(&sharing.CalendarShare{UUID: "s", CalendarID: theirs.ID, SharedWithID: u.ID, Permission: "read"}).Error)

	do := func(method, path, depth, body string) (int, []byte) {
		req, _ := http.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", authHeader)
		if method == "PUT" {
			req.Header.Set("Content-Type", "text/calendar")
		} else {
			req.Header.Set("Content-Type", "application/xml")
		}
		if depth != "" {
			req.Header.Set("Depth", depth)
		}
		resp, err := app.Test(req, davTestTimeout)
		require.NoError(t, err)
		defer resp.Body.Close()
		out, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		return resp.StatusCode, out
	}
	propfind := func(path, depth, body string) SyncMultiStatus {
		status, out := do("PROPFIND", path, depth, body)
		require.Equal(t, http.StatusMultiStatus, status, string(out))
		var ms SyncMultiStatus
		require.NoError(t, xml.Unmarshal(out, &ms), string(out))
		return ms
	}

	// An object in the calendar, so a Depth: 1 PROPFIND on it lists a member.
	status, out := do("PUT", "/dav/u/calendars/cal/ev.ics", "",
		"BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//T//EN\r\nBEGIN:VEVENT\r\nUID:ev@x\r\nDTSTAMP:20300101T000000Z\r\n"+
			"DTSTART:20300101T100000Z\r\nDTEND:20300101T110000Z\r\nSUMMARY:x\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")
	require.Less(t, status, 300, string(out))

	t.Run("calendar", func(t *testing.T) {
		stored, err := calRepo.GetByPath(ctx, u.ID, "cal")
		require.NoError(t, err)
		href := "/dav/u/calendars/cal/"

		ok0, missing0 := responseProps(t, propfind(href, "0", calendarPropsBody), href)
		assert.Equal(t, "Work", ok0["displayname"])
		assert.Equal(t, "Work &amp; stuff", ok0["calendar-description"])
		assert.Contains(t, ok0["supported-calendar-component-set"], `name="VEVENT"`)
		assert.Contains(t, ok0["supported-calendar-data"], `content-type="text/calendar"`)
		assert.Equal(t, stored.SyncToken, ok0["sync-token"])
		assert.Equal(t, stored.CTag, ok0["getctag"])
		assert.Equal(t, "#FF8800", ok0["calendar-color"], "the stored colour must be readable")
		assert.Contains(t, ok0["current-user-privilege-set"], "<write/>")
		assert.Contains(t, ok0["current-user-principal"], "/dav/u/")
		assert.Equal(t, []string{"foo", "getetag"}, missing0)

		home := propfind("/dav/u/calendars/", "1", calendarPropsBody)
		ok1, missing1 := responseProps(t, home, href)
		assert.Equal(t, ok0, ok1, "Depth 1 on the home must describe the calendar like Depth 0")
		assert.Equal(t, missing0, missing1)

		self := propfind(href, "1", calendarPropsBody)
		okSelf, missingSelf := responseProps(t, self, href)
		assert.Equal(t, ok0, okSelf, "Depth 1 on the calendar must describe it like Depth 0")
		assert.Equal(t, missing0, missingSelf)
		// The member keeps the properties emersion serves for objects.
		okObj, _ := responseProps(t, self, href+"ev.ics")
		assert.NotEmpty(t, okObj["getetag"], "members must pass through untouched")

		// No Depth header means infinity (RFC 4918 §9.1); still the same answer.
		okInf, _ := responseProps(t, propfind("/dav/u/calendars/", "", calendarPropsBody), href)
		assert.Equal(t, ok0, okInf)
	})

	t.Run("read-only shared calendar", func(t *testing.T) {
		href := "/dav/u/calendars/shared-cal-uuid/"
		ok0, _ := responseProps(t, propfind(href, "0", calendarPropsBody), href)
		assert.NotContains(t, ok0["current-user-privilege-set"], "<write/>")
		assert.Equal(t, "#00AA00", ok0["calendar-color"])

		ok1, _ := responseProps(t, propfind("/dav/u/calendars/", "1", calendarPropsBody), href)
		assert.Equal(t, ok0, ok1, "a read-only share must not advertise write at Depth 1")
	})

	t.Run("addressbook", func(t *testing.T) {
		href := "/dav/u/addressbooks/book/"
		ok0, missing0 := responseProps(t, propfind(href, "0", bookPropsBody), href)
		assert.Equal(t, "Friends", ok0["displayname"])
		assert.Equal(t, "Close friends", ok0["addressbook-description"])
		assert.Contains(t, ok0["supported-address-data"], `version="4.0"`)
		assert.Equal(t, "10485760", ok0["max-resource-size"])
		assert.NotEmpty(t, ok0["sync-token"])
		assert.NotEmpty(t, ok0["getctag"])
		assert.Equal(t, []string{"foo", "getetag"}, missing0)

		ok1, missing1 := responseProps(t, propfind("/dav/u/addressbooks/", "1", bookPropsBody), href)
		assert.Equal(t, ok0, ok1, "Depth 1 on the home must describe the book like Depth 0")
		assert.Equal(t, missing0, missing1)
	})

	t.Run("propname lists names without values", func(t *testing.T) {
		body := `<?xml version="1.0" encoding="utf-8"?><D:propfind xmlns:D="DAV:"><D:propname/></D:propfind>`
		ok0, _ := responseProps(t, propfind("/dav/u/calendars/cal/", "0", body), "/dav/u/calendars/cal/")
		require.Contains(t, ok0, "sync-token")
		require.Contains(t, ok0, "calendar-color")
		for name, inner := range ok0 {
			assert.Empty(t, inner, "propname must not return the value of %s", name)
		}
	})
}

// TestCalendarColorReadBackAfterProppatch: the colour a client sets with
// PROPPATCH must come back from PROPFIND (F07: it was write-only).
func TestCalendarColorReadBackAfterProppatch(t *testing.T) {
	app, db, _ := setupTestApp(t)
	defer db.Close()
	ctx := context.Background()

	userRepo := repository.NewUserRepository(db.DB())
	calRepo := repository.NewCalendarRepository(db.DB())
	passwordHash, _ := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	u := &user.User{UUID: "u", Email: "u@example.com", Username: "u", PasswordHash: string(passwordHash), IsActive: true}
	require.NoError(t, userRepo.Create(ctx, u))
	require.NoError(t, calRepo.Create(ctx, &calendar.Calendar{UserID: u.ID, UUID: "c", Path: "cal", Name: "Cal", Color: "#3788d8", Timezone: "UTC"}))
	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte("u@example.com:password"))

	req, _ := http.NewRequest("PROPPATCH", "/dav/u/calendars/cal/", strings.NewReader(`<?xml version="1.0" encoding="utf-8"?>
<D:propertyupdate xmlns:D="DAV:" xmlns:A="http://apple.com/ns/ical/"><D:set><D:prop><A:calendar-color>#FF8800FF</A:calendar-color></D:prop></D:set></D:propertyupdate>`))
	req.Header.Set("Authorization", authHeader)
	req.Header.Set("Content-Type", "application/xml")
	resp, err := app.Test(req, davTestTimeout)
	require.NoError(t, err)
	require.Equal(t, http.StatusMultiStatus, resp.StatusCode)

	for _, depth := range []string{"0", "1"} {
		path := "/dav/u/calendars/cal/"
		if depth == "1" {
			path = "/dav/u/calendars/"
		}
		req, _ = http.NewRequest("PROPFIND", path, strings.NewReader(`<?xml version="1.0" encoding="utf-8"?>
<D:propfind xmlns:D="DAV:" xmlns:A="http://apple.com/ns/ical/"><D:prop><A:calendar-color/></D:prop></D:propfind>`))
		req.Header.Set("Authorization", authHeader)
		req.Header.Set("Content-Type", "application/xml")
		req.Header.Set("Depth", depth)
		resp, err = app.Test(req, davTestTimeout)
		require.NoError(t, err)
		require.Equal(t, http.StatusMultiStatus, resp.StatusCode)
		var ms SyncMultiStatus
		require.NoError(t, xml.NewDecoder(resp.Body).Decode(&ms))
		ok, _ := responseProps(t, ms, "/dav/u/calendars/cal/")
		assert.Equal(t, "#FF8800", ok["calendar-color"], "Depth %s", depth)
	}
}
