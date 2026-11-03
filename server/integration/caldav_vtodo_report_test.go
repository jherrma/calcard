//go:build integration

package integration_test

import (
	"encoding/xml"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCalDAVVTODOTimeRangeReport is the end-to-end test of the VTODO
// time-range calendar-query (#170, story 17): Thunderbird's and DAVx5's
// due-date views send <comp-filter name="VTODO"><time-range/></comp-filter>,
// which go-webdav answered with no non-recurring task at all. One DAV-written
// task per row of the RFC 4791 §9.9 VTODO table, three repeating tasks and an
// event share one `kind: "both"` collection; each window lists exactly the
// tasks the table says, at the row's boundaries (start inclusive, end
// exclusive, and each row's own < / <= / > / >=).
//
// Repeating tasks: one with DTSTART and one without (DUE+RRULE, as Tasks.org
// writes it), both first due in 2030, match every later window; a series that
// ended after three days does not, but matches up to its last occurrence's
// DUE. The VEVENT query, a VTODO query without a time-range, calendar-multiget
// and sync-collection on the same collection are unchanged.
func TestCalDAVVTODOTimeRangeReport(t *testing.T) {
	email := "dav-vtodo-range-170@example.test"
	password := "vtodoRange!123"
	token, username := registerAndLoginFull(t, email, password, "Due Date User")
	_, appPass := createAppPassword(t, token, "vtodo-range")

	cal := createTaskCalendar(t, token, "Due dates", "both")
	collection := "/dav/" + username + "/calendars/" + cal.Path + "/"

	todo := func(uid string, lines ...string) string {
		return ics(append([]string{"BEGIN:VTODO", "UID:" + uid + "@range.test", "DTSTAMP:20300101T000000Z"},
			append(lines, "END:VTODO")...)...)
	}
	objects := map[string]string{
		// One per row of the RFC 4791 §9.9 VTODO table, a day apart.
		"r1-dtstart-duration":  todo("r1", "SUMMARY:Row 1", "DTSTART:20330310T100000Z", "DURATION:PT2H"),
		"r2-dtstart-due":       todo("r2", "SUMMARY:Row 2", "DTSTART:20330311T100000Z", "DUE:20330311T120000Z"),
		"r3-dtstart-only":      todo("r3", "SUMMARY:Row 3", "DTSTART:20330312T100000Z"),
		"r4-due-only":          todo("r4", "SUMMARY:Call the dentist", "DUE:20330313T120000Z"),
		"r5-completed-created": todo("r5", "SUMMARY:Row 5", "CREATED:20330314T080000Z", "COMPLETED:20330315T080000Z", "STATUS:COMPLETED"),
		"r6-completed-only":    todo("r6", "SUMMARY:Row 6", "COMPLETED:20330316T080000Z", "STATUS:COMPLETED"),
		"r7-created-only":      todo("r7", "SUMMARY:Row 7", "CREATED:20330317T080000Z"),
		"r8-no-dates":          todo("r8", "SUMMARY:Row 8"),
		// Repeating tasks, first due in 2030.
		"rec-dtstart":    todo("rec-dtstart", "SUMMARY:Weekly", "DTSTART:20300101T090000Z", "DUE:20300101T100000Z", "RRULE:FREQ=WEEKLY"),
		"rec-no-dtstart": todo("rec-no-dtstart", "SUMMARY:Daily", "DUE:20300101T100000Z", "RRULE:FREQ=DAILY"),
		"rec-ended":      todo("rec-ended", "SUMMARY:Three days", "DTSTART:20300101T090000Z", "DUE:20300101T100000Z", "RRULE:FREQ=DAILY;COUNT=3"),
		// An event on row 1's day.
		"event-0310": ics("BEGIN:VEVENT", "UID:event-0310@range.test", "DTSTAMP:20300101T000000Z",
			"SUMMARY:Call the dentist", "DTSTART:20330310T100000Z", "DTEND:20330310T110000Z", "END:VEVENT"),
	}
	for name, body := range objects {
		davPut(t, collection+name+".ics", email, appPass, vcalendar(body))
	}

	// report runs a calendar-query and returns the names of the objects it
	// lists, sorted.
	report := func(t *testing.T, compFilter string) []string {
		t.Helper()
		body := `<?xml version="1.0" encoding="utf-8"?>
<C:calendar-query xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">
  <D:prop><D:getetag/></D:prop>
  <C:filter><C:comp-filter name="VCALENDAR">` + compFilter + `</C:comp-filter></C:filter>
</C:calendar-query>`
		status, _, resp := davCall(t, "REPORT", collection, email, appPass, body, depthHeader("1"))
		require.Equalf(t, http.StatusMultiStatus, status, "calendar-query: %s", string(resp))
		return reportedNames(t, resp)
	}
	timeRange := func(start, end string) string {
		attrs := ""
		if start != "" {
			attrs += ` start="` + start + `"`
		}
		if end != "" {
			attrs += ` end="` + end + `"`
		}
		return `<C:time-range` + attrs + `/>`
	}
	vtodoRange := func(t *testing.T, start, end string) []string {
		t.Helper()
		return report(t, `<C:comp-filter name="VTODO">`+timeRange(start, end)+`</C:comp-filter>`)
	}

	// Every 2033 window holds the dateless task and the two open-ended series.
	always := []string{"r8-no-dates", "rec-dtstart", "rec-no-dtstart"}
	with := func(names ...string) []string {
		out := append(slices.Clone(always), names...)
		slices.Sort(out)
		return out
	}

	cases := []struct {
		name       string
		start, end string
		want       []string
	}{
		// DTSTART Y, DURATION Y, DUE N: (start <= DTSTART+DURATION) AND ((end > DTSTART) OR (end >= DTSTART+DURATION))
		{"row 1: ends at DTSTART", "20330310T090000Z", "20330310T100000Z", with()},
		{"row 1: ends a second after DTSTART", "20330310T090000Z", "20330310T100001Z", with("r1-dtstart-duration")},
		{"row 1: inside", "20330310T103000Z", "20330310T110000Z", with("r1-dtstart-duration")},
		{"row 1: starts at DTSTART+DURATION", "20330310T120000Z", "20330310T130000Z", with("r1-dtstart-duration")},
		{"row 1: starts after DTSTART+DURATION", "20330310T120001Z", "20330310T130000Z", with()},
		// DTSTART Y, DURATION N, DUE Y: ((start < DUE) OR (start <= DTSTART)) AND ((end > DTSTART) OR (end >= DUE))
		{"row 2: ends at DTSTART", "20330311T090000Z", "20330311T100000Z", with()},
		{"row 2: ends a second after DTSTART", "20330311T090000Z", "20330311T100001Z", with("r2-dtstart-due")},
		{"row 2: inside", "20330311T110000Z", "20330311T113000Z", with("r2-dtstart-due")},
		{"row 2: starts a second before DUE", "20330311T115959Z", "20330311T130000Z", with("r2-dtstart-due")},
		{"row 2: starts at DUE", "20330311T120000Z", "20330311T130000Z", with()},
		// DTSTART Y, DURATION N, DUE N: (start <= DTSTART) AND (end > DTSTART)
		{"row 3: starts at DTSTART", "20330312T100000Z", "20330312T110000Z", with("r3-dtstart-only")},
		{"row 3: ends at DTSTART", "20330312T090000Z", "20330312T100000Z", with()},
		{"row 3: starts after DTSTART", "20330312T100001Z", "20330312T110000Z", with()},
		// DTSTART N, DURATION N, DUE Y: (start < DUE) AND (end >= DUE)
		{"row 4: ends at DUE", "20330313T110000Z", "20330313T120000Z", with("r4-due-only")},
		{"row 4: starts a second before DUE", "20330313T115959Z", "20330313T130000Z", with("r4-due-only")},
		{"row 4: starts at DUE", "20330313T120000Z", "20330313T130000Z", with()},
		{"row 4: ends before DUE", "20330313T110000Z", "20330313T115959Z", with()},
		// COMPLETED Y, CREATED Y: ((start <= CREATED) OR (start <= COMPLETED)) AND ((end >= CREATED) OR (end >= COMPLETED))
		{"row 5: between CREATED and COMPLETED", "20330314T120000Z", "20330314T130000Z", with("r5-completed-created")},
		{"row 5: ends at CREATED", "20330314T070000Z", "20330314T080000Z", with("r5-completed-created")},
		{"row 5: ends before CREATED", "20330314T070000Z", "20330314T075959Z", with()},
		{"row 5: starts at COMPLETED", "20330315T080000Z", "20330315T090000Z", with("r5-completed-created")},
		{"row 5: starts after COMPLETED", "20330315T080001Z", "20330315T090000Z", with()},
		// COMPLETED Y, CREATED N: (start <= COMPLETED) AND (end >= COMPLETED)
		{"row 6: ends at COMPLETED", "20330316T070000Z", "20330316T080000Z", with("r6-completed-only")},
		{"row 6: starts at COMPLETED", "20330316T080000Z", "20330316T090000Z", with("r6-completed-only")},
		{"row 6: starts after COMPLETED", "20330316T080001Z", "20330316T090000Z", with()},
		{"row 6: ends before COMPLETED", "20330316T070000Z", "20330316T075959Z", with()},
		// COMPLETED N, CREATED Y: (end > CREATED)
		{"row 7: ends at CREATED", "20330317T070000Z", "20330317T080000Z", with()},
		{"row 7: ends a second after CREATED", "20330317T070000Z", "20330317T080001Z", with("r7-created-only")},
		{"row 7: any later window", "20330318T000000Z", "20330318T010000Z", with("r7-created-only")},
		// None of them: TRUE (r8 is in every window above; this one predates everything else).
		{"row 8: a window before every other task", "19990101T000000Z", "19990102T000000Z", []string{"r8-no-dates"}},
		// A month holds every row's task, not the series that ended in 2030.
		{"the whole month", "20330301T000000Z", "20330401T000000Z", with("r1-dtstart-duration", "r2-dtstart-due",
			"r3-dtstart-only", "r4-due-only", "r5-completed-created", "r6-completed-only", "r7-created-only")},
		// Open-ended ranges (RFC 4791 §9.9: a missing start is -infinity, a missing end +infinity).
		{"open end", "20330317T080001Z", "", with("r7-created-only")},
		{"open start", "", "20330310T100001Z", with("r1-dtstart-duration", "rec-ended")},
		// Repeating tasks: [DTSTART else DUE, last occurrence's DUE].
		{"inside the ended series' second day", "20300102T093000Z", "20300102T094500Z", with("rec-ended")},
		{"inside its last occurrence, after that occurrence's DTSTART", "20300103T093000Z", "20300103T094500Z", with("rec-ended")},
		{"after its last DUE", "20300103T100001Z", "20300103T110000Z", with()},
		{"ends at the first DTSTART", "20291231T000000Z", "20300101T090000Z",
			[]string{"r8-no-dates", "rec-dtstart", "rec-ended"}},
		{"ends before every series begins", "20291231T000000Z", "20300101T085959Z", []string{"r8-no-dates"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, vtodoRange(t, tc.start, tc.end))
		})
	}

	t.Run("repeating tasks first due in 2030 are in a window years later", func(t *testing.T) {
		got := vtodoRange(t, "20350601T000000Z", "20350608T000000Z")
		assert.Contains(t, got, "rec-dtstart", "with DTSTART")
		assert.Contains(t, got, "rec-no-dtstart", "without DTSTART (DUE+RRULE)")
		assert.NotContains(t, got, "rec-ended")
	})

	t.Run("a text-match on the VTODO still applies", func(t *testing.T) {
		got := report(t, `<C:comp-filter name="VTODO">`+timeRange("20330301T000000Z", "20330401T000000Z")+
			`<C:prop-filter name="SUMMARY"><C:text-match>dentist</C:text-match></C:prop-filter></C:comp-filter>`)
		assert.Equal(t, []string{"r4-due-only"}, got, "the event with the same title is not a VTODO")
	})

	t.Run("VEVENT time-range is unchanged", func(t *testing.T) {
		assert.Equal(t, []string{"event-0310"},
			report(t, `<C:comp-filter name="VEVENT">`+timeRange("20330310T000000Z", "20330311T000000Z")+`</C:comp-filter>`))
		assert.Equal(t, []string{}, report(t, `<C:comp-filter name="VEVENT">`+timeRange("20330311T000000Z", "20330312T000000Z")+`</C:comp-filter>`))
	})

	allTasks := []string{}
	for name := range objects {
		if name != "event-0310" {
			allTasks = append(allTasks, name)
		}
	}
	slices.Sort(allTasks)

	t.Run("VTODO without a time-range lists every task", func(t *testing.T) {
		assert.Equal(t, allTasks, report(t, `<C:comp-filter name="VTODO"/>`))
	})

	t.Run("calendar-multiget and sync-collection are unchanged", func(t *testing.T) {
		multiget := `<?xml version="1.0" encoding="utf-8"?>
<C:calendar-multiget xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">
  <D:prop><D:getetag/></D:prop>
  <D:href>` + collection + `rec-ended.ics</D:href>
  <D:href>` + collection + `r6-completed-only.ics</D:href>
</C:calendar-multiget>`
		status, _, resp := davCall(t, "REPORT", collection, email, appPass, multiget, depthHeader("1"))
		require.Equalf(t, http.StatusMultiStatus, status, "calendar-multiget: %s", string(resp))
		assert.Equal(t, []string{"r6-completed-only", "rec-ended"}, reportedNames(t, resp),
			"multiget returns what it names, whatever any time-range would say")

		status, _, resp = davCall(t, "REPORT", collection, email, appPass, syncCollectionBody, depthHeader("1"))
		require.Equalf(t, http.StatusMultiStatus, status, "sync-collection: %s", string(resp))
		everything := append(slices.Clone(allTasks), "event-0310")
		slices.Sort(everything)
		assert.Equal(t, everything, reportedNames(t, resp), "an initial sync lists every member")
	})
}

// reportedNames parses a multistatus and returns the object names (the last
// href segment without ".ics") of its responses, sorted; never nil.
func reportedNames(t *testing.T, body []byte) []string {
	t.Helper()
	var ms struct {
		Responses []struct {
			Href string `xml:"href"`
		} `xml:"response"`
	}
	require.NoErrorf(t, xml.Unmarshal(body, &ms), "parse multistatus: %s", string(body))
	names := []string{}
	for _, r := range ms.Responses {
		href, err := url.PathUnescape(r.Href)
		require.NoError(t, err)
		names = append(names, strings.TrimSuffix(path.Base(href), ".ics"))
	}
	slices.Sort(names)
	return names
}
