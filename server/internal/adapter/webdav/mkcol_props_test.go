package webdav

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// iosMkcalendar is the body iOS / macOS Calendar sends to create a calendar:
// timezone, sort order, component set, colour with alpha, name and the
// scheduling transparency, in that order, with CRs as character references.
const iosMkcalendar = `<?xml version="1.0" encoding="UTF-8"?>
<B:mkcalendar xmlns:B="urn:ietf:params:xml:ns:caldav"><A:set xmlns:A="DAV:"><A:prop><B:calendar-timezone>BEGIN:VCALENDAR&#13;
VERSION:2.0&#13;
PRODID:-//Apple Inc.//iPhone OS 17.0//EN&#13;
CALSCALE:GREGORIAN&#13;
BEGIN:VTIMEZONE&#13;
TZID:Europe/Berlin&#13;
BEGIN:STANDARD&#13;
DTSTART:19701025T030000&#13;
RRULE:FREQ=YEARLY;BYMONTH=10;BYDAY=-1SU&#13;
TZNAME:CET&#13;
TZOFFSETFROM:+0200&#13;
TZOFFSETTO:+0100&#13;
END:STANDARD&#13;
END:VTIMEZONE&#13;
END:VCALENDAR&#13;
</B:calendar-timezone><D:calendar-order xmlns:D="http://apple.com/ns/ical/">3</D:calendar-order><B:supported-calendar-component-set><B:comp name="VEVENT"/></B:supported-calendar-component-set><D:calendar-color xmlns:D="http://apple.com/ns/ical/" symbolic-color="custom">#1BADF8FF</D:calendar-color><A:displayname>Work</A:displayname><B:schedule-calendar-transp><B:opaque/></B:schedule-calendar-transp></A:prop></A:set></B:mkcalendar>`

// davx5Props is what DAVx5 puts in a calendar creation body: resourcetype,
// name, #RRGGBBAA colour, description, a CDATA timezone and, when the user
// unticked journals, the component set.
const davx5Props = `<resourcetype><collection/><CAL:calendar/></resourcetype>` +
	`<displayname>Family</displayname>` +
	`<ICAL:calendar-color>#FF0000FF</ICAL:calendar-color>` +
	`<CAL:calendar-description>Shared with the family</CAL:calendar-description>` +
	`<CAL:calendar-timezone><![CDATA[BEGIN:VCALENDAR
VERSION:2.0
PRODID:DAVx5
BEGIN:VTIMEZONE
TZID:Europe/Vienna
BEGIN:STANDARD
DTSTART:19701025T030000
TZOFFSETFROM:+0200
TZOFFSETTO:+0100
END:STANDARD
END:VTIMEZONE
END:VCALENDAR
]]></CAL:calendar-timezone>` +
	`<CAL:supported-calendar-component-set><CAL:comp name="VEVENT"/><CAL:comp name="VTODO"/></CAL:supported-calendar-component-set>`

const davx5NS = `xmlns="DAV:" xmlns:CAL="urn:ietf:params:xml:ns:caldav" xmlns:ICAL="http://apple.com/ns/ical/"`

func mkcalendarWith(props string) string {
	return `<?xml version="1.0" encoding="UTF-8"?><CAL:mkcalendar ` + davx5NS + `><set><prop>` + props + `</prop></set></CAL:mkcalendar>`
}

// TestMkcolProperties is the regression test for F25: MKCALENDAR and extended
// MKCOL kept only the displayname and answered 201 for every other property,
// applied or not. Now each property is either stored or the whole request is
// refused without creating anything (RFC 4791 §5.3.1, RFC 5689 §3).
func TestMkcolProperties(t *testing.T) {
	app, db, _ := setupTestApp(t)
	defer db.Close()

	ctx := context.Background()
	userRepo := repository.NewUserRepository(db.DB())
	calRepo := repository.NewCalendarRepository(db.DB())
	abRepo := repository.NewAddressBookRepository(db.DB())
	passwordHash, _ := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.DefaultCost)
	u := &user.User{UUID: "u-uuid", Email: "test@example.com", Username: "testuser", PasswordHash: string(passwordHash), IsActive: true}
	require.NoError(t, userRepo.Create(ctx, u))
	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte("test@example.com:password"))

	do := func(method, path, body string) (int, string) {
		req, _ := http.NewRequest(method, path, bytes.NewReader([]byte(body)))
		req.Header.Set("Authorization", authHeader)
		if body != "" {
			req.Header.Set("Content-Type", "application/xml; charset=utf-8")
		}
		resp := davDo(t, app, req)
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	calendarExists := func(path string) bool {
		c, err := calRepo.GetByPath(ctx, u.ID, path)
		return err == nil && c != nil
	}

	t.Run("iOS MKCALENDAR stores every property", func(t *testing.T) {
		status, body := do("MKCALENDAR", "/dav/testuser/calendars/ios/", iosMkcalendar)
		require.Equal(t, fiber.StatusCreated, status, body)
		cal, err := calRepo.GetByPath(ctx, u.ID, "ios")
		require.NoError(t, err)
		assert.Equal(t, "Work", cal.Name)
		assert.Equal(t, "#1BADF8", cal.Color, "alpha is dropped, the colour kept")
		assert.Equal(t, "Europe/Berlin", cal.Timezone)
		assert.Equal(t, "VEVENT", cal.SupportedComponents)

		// The narrowed component set is what the collection now reports.
		status, body = do("PROPFIND", "/dav/testuser/calendars/ios/",
			`<d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav"><d:prop><c:supported-calendar-component-set/></d:prop></d:propfind>`)
		require.Equal(t, http.StatusMultiStatus, status, body)
		assert.Contains(t, body, `name="VEVENT"`)
		assert.NotContains(t, body, `name="VTODO"`)
	})

	t.Run("DAVx5 MKCALENDAR with resourcetype and CDATA timezone", func(t *testing.T) {
		status, body := do("MKCALENDAR", "/dav/testuser/calendars/davx5/", mkcalendarWith(davx5Props))
		require.Equal(t, fiber.StatusCreated, status, body)
		cal, err := calRepo.GetByPath(ctx, u.ID, "davx5")
		require.NoError(t, err)
		assert.Equal(t, "Family", cal.Name)
		assert.Equal(t, "Shared with the family", cal.Description)
		assert.Equal(t, "#FF0000", cal.Color)
		assert.Equal(t, "Europe/Vienna", cal.Timezone)
		assert.Equal(t, "VEVENT,VTODO", cal.SupportedComponents)
	})

	t.Run("DAVx5 extended MKCOL calendar", func(t *testing.T) {
		status, body := do("MKCOL", "/dav/testuser/calendars/davx5-mkcol/",
			`<?xml version="1.0" encoding="UTF-8"?><mkcol `+davx5NS+`><set><prop>`+davx5Props+`</prop></set></mkcol>`)
		require.Equal(t, fiber.StatusCreated, status, body)
		cal, err := calRepo.GetByPath(ctx, u.ID, "davx5-mkcol")
		require.NoError(t, err)
		assert.Equal(t, "Family", cal.Name)
		assert.Equal(t, "Shared with the family", cal.Description)
		assert.Equal(t, "#FF0000", cal.Color)
		assert.Equal(t, "Europe/Vienna", cal.Timezone)
	})

	t.Run("libical-prefixed TZID and timezone-id resolve to IANA names", func(t *testing.T) {
		status, body := do("MKCALENDAR", "/dav/testuser/calendars/evo/", mkcalendarWith(
			`<CAL:calendar-timezone>BEGIN:VCALENDAR
BEGIN:VTIMEZONE
TZID:/freeassociation.sourceforge.net/Europe/Paris
END:VTIMEZONE
END:VCALENDAR</CAL:calendar-timezone>`))
		require.Equal(t, fiber.StatusCreated, status, body)
		cal, err := calRepo.GetByPath(ctx, u.ID, "evo")
		require.NoError(t, err)
		assert.Equal(t, "Europe/Paris", cal.Timezone)

		status, body = do("MKCALENDAR", "/dav/testuser/calendars/tzid/", mkcalendarWith(
			`<CAL:calendar-timezone-id>America/New_York</CAL:calendar-timezone-id>`))
		require.Equal(t, fiber.StatusCreated, status, body)
		cal, err = calRepo.GetByPath(ctx, u.ID, "tzid")
		require.NoError(t, err)
		assert.Equal(t, "America/New_York", cal.Timezone)
	})

	refusals := []struct {
		name, slug, props, mustContain string
	}{
		{"unknown property", "unknown", `<displayname>X</displayname><X:frobnicate xmlns:X="urn:x-example">1</X:frobnicate>`, "frobnicate"},
		{"journal component set", "journal", `<CAL:supported-calendar-component-set><CAL:comp name="VEVENT"/><CAL:comp name="VJOURNAL"/></CAL:supported-calendar-component-set>`, "supported-calendar-component"},
		{"timezone without VTIMEZONE", "badtz", `<CAL:calendar-timezone>not a calendar</CAL:calendar-timezone>`, "valid-calendar-data"},
		{"unknown TZID", "unknowntz", `<CAL:calendar-timezone-id>Mars/Olympus_Mons</CAL:calendar-timezone-id>`, "calendar-timezone-id"},
		{"colour name", "red", `<ICAL:calendar-color>red</ICAL:calendar-color>`, "calendar-color"},
		{"transparent calendar", "transp", `<CAL:schedule-calendar-transp><CAL:transparent/></CAL:schedule-calendar-transp>`, "schedule-calendar-transp"},
		{"address book resourcetype", "wrongtype", `<resourcetype><collection/><C:addressbook xmlns:C="urn:ietf:params:xml:ns:carddav"/></resourcetype>`, "valid-resourcetype"},
		{"overlong name", "longname", `<displayname>` + strings.Repeat("n", 256) + `</displayname>`, "displayname"},
	}
	for _, tc := range refusals {
		t.Run("MKCALENDAR refuses "+tc.name, func(t *testing.T) {
			status, body := do("MKCALENDAR", "/dav/testuser/calendars/"+tc.slug+"/", mkcalendarWith(`<CAL:calendar-description>Kept?</CAL:calendar-description>`+tc.props))
			assert.Equal(t, http.StatusMultiStatus, status, "RFC 4791 5.3.1.1: 207 when a DAV:set instruction fails")
			assert.Contains(t, body, "HTTP/1.1 403 Forbidden")
			assert.Contains(t, body, "HTTP/1.1 424 Failed Dependency", "the other properties failed with it")
			assert.Contains(t, body, tc.mustContain)
			assert.False(t, calendarExists(tc.slug), "a refused MKCALENDAR must not create the calendar")
		})
	}

	t.Run("extended MKCOL refuses an unknown calendar property with 403", func(t *testing.T) {
		status, body := do("MKCOL", "/dav/testuser/calendars/mkcol-unknown/", `<?xml version="1.0" encoding="UTF-8"?><mkcol `+davx5NS+`><set><prop>`+
			`<resourcetype><collection/><CAL:calendar/></resourcetype><displayname>X</displayname><CAL:max-resource-size>10</CAL:max-resource-size></prop></set></mkcol>`)
		assert.Equal(t, http.StatusForbidden, status, "RFC 5689 3.5")
		assert.Contains(t, body, "mkcol-response")
		assert.Contains(t, body, "max-resource-size")
		assert.False(t, calendarExists("mkcol-unknown"))
	})

	t.Run("malformed MKCALENDAR body is a 400", func(t *testing.T) {
		status, _ := do("MKCALENDAR", "/dav/testuser/calendars/malformed/", `<C:mkcalendar xmlns:C="urn:ietf:params:xml:ns:caldav"><D:set`)
		assert.Equal(t, http.StatusBadRequest, status)
		assert.False(t, calendarExists("malformed"))
	})

	abMkcol := func(props string) string {
		return `<?xml version="1.0" encoding="UTF-8"?><mkcol xmlns="DAV:" xmlns:CARD="urn:ietf:params:xml:ns:carddav"><set><prop>` +
			`<resourcetype><collection/><CARD:addressbook/></resourcetype>` + props + `</prop></set></mkcol>`
	}

	t.Run("DAVx5 address book MKCOL keeps name and description", func(t *testing.T) {
		status, body := do("MKCOL", "/dav/testuser/addressbooks/friends/",
			abMkcol(`<displayname>Friends</displayname><CARD:addressbook-description>People I know</CARD:addressbook-description>`))
		require.Equal(t, fiber.StatusCreated, status, body)
		ab, err := abRepo.GetByUserAndPath(ctx, u.ID, "friends")
		require.NoError(t, err)
		require.NotNil(t, ab)
		assert.Equal(t, "Friends", ab.Name)
		assert.Equal(t, "People I know", ab.Description)
	})

	t.Run("address book MKCOL refuses an unknown property", func(t *testing.T) {
		status, body := do("MKCOL", "/dav/testuser/addressbooks/strange/",
			abMkcol(`<displayname>Strange</displayname><ICAL:calendar-color xmlns:ICAL="http://apple.com/ns/ical/">#FF0000</ICAL:calendar-color>`))
		assert.Equal(t, http.StatusForbidden, status, body)
		assert.Contains(t, body, "calendar-color")
		ab, _ := abRepo.GetByUserAndPath(ctx, u.ID, "strange")
		assert.Nil(t, ab, "a refused MKCOL must not create the address book")
	})
}
