package webdav

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav/caldav"
	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// ---- fixtures ---------------------------------------------------------------

// utc parses a "date with UTC time" as a CalDAV time-range carries it; "" is
// the zero time, an open bound.
func utc(t *testing.T, s string) time.Time {
	t.Helper()
	if s == "" {
		return time.Time{}
	}
	v, err := time.Parse("20060102T150405Z", s)
	require.NoError(t, err)
	return v
}

func utcPtr(t *testing.T, s string) *time.Time {
	v := utc(t, s)
	return &v
}

// component wraps content lines (or nested components) into one iCalendar
// component.
func component(name string, lines ...string) string {
	trimmed := make([]string, len(lines))
	for i, line := range lines {
		trimmed[i] = strings.TrimSuffix(line, "\r\n")
	}
	return "BEGIN:" + name + "\r\n" + strings.Join(trimmed, "\r\n") + "\r\nEND:" + name + "\r\n"
}

// vtodo is a VTODO with a UID and DTSTAMP plus the given lines.
func vtodo(uid string, lines ...string) string {
	return component(ical.CompToDo, append([]string{"UID:" + uid, "DTSTAMP:20200101T000000Z"}, lines...)...)
}

// candidate decodes a VCALENDAR made of components into a collection member.
func candidate(t *testing.T, name string, recurrenceEnd *time.Time, components ...string) queryCandidate {
	t.Helper()
	data := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//CalCard Tests//EN\r\n" +
		strings.Join(components, "") + "END:VCALENDAR\r\n"
	cal, err := ical.NewDecoder(strings.NewReader(data)).Decode()
	require.NoError(t, err, "fixture %s", name)
	return queryCandidate{
		object:        caldav.CalendarObject{Path: "/dav/u/calendars/c/" + name + ".ics", Data: cal},
		recurrenceEnd: recurrenceEnd,
	}
}

// vtodoQuery is <comp-filter VCALENDAR><comp-filter VTODO><time-range/> with
// optional prop-filters and nested comp-filters on the VTODO filter.
func vtodoQuery(t *testing.T, start, end string, props []caldav.PropFilter, comps ...caldav.CompFilter) *caldav.CalendarQuery {
	return &caldav.CalendarQuery{CompFilter: caldav.CompFilter{
		Name: ical.CompCalendar,
		Comps: []caldav.CompFilter{{
			Name: ical.CompToDo, Start: utc(t, start), End: utc(t, end), Props: props, Comps: comps,
		}},
	}}
}

// matched runs the query and returns the names of the matching candidates.
func matched(t *testing.T, query *caldav.CalendarQuery, candidates ...queryCandidate) []string {
	t.Helper()
	out, err := filterCalendarQuery(query, candidates)
	require.NoError(t, err)
	return objectNames(out)
}

func objectNames(objects []caldav.CalendarObject) []string {
	names := []string{}
	for _, o := range objects {
		names = append(names, strings.TrimSuffix(o.Path[strings.LastIndex(o.Path, "/")+1:], ".ics"))
	}
	return names
}

// window is one time-range and whether the task must be returned for it.
type window struct {
	start, end string // "" = open
	want       bool
	why        string
}

func runWindows(t *testing.T, c queryCandidate, windows []window) {
	t.Helper()
	for _, w := range windows {
		got := len(matched(t, vtodoQuery(t, w.start, w.end, nil), c)) == 1
		assert.Equalf(t, w.want, got, "time-range [%s, %s): %s", w.start, w.end, w.why)
	}
}

// ---- RFC 4791 §9.9 ----------------------------------------------------------

// TestVTODOTimeRangeTable walks every row of the RFC 4791 §9.9 VTODO table,
// each at its boundaries (start inclusive, end exclusive, and the row's own
// < / <= / > / >= choices), with open-ended ranges, plus the value forms the
// table does not spell out.
func TestVTODOTimeRangeTable(t *testing.T) {
	cases := []struct {
		name    string
		lines   []string
		windows []window
	}{
		{
			name:  "DTSTART+DURATION: (start <= DTSTART+DURATION) AND ((end > DTSTART) OR (end >= DTSTART+DURATION))",
			lines: []string{"DTSTART:20301105T100000Z", "DURATION:PT2H"},
			windows: []window{
				{"20301105T090000Z", "20301105T100000Z", false, "ends at DTSTART"},
				{"20301105T090000Z", "20301105T100001Z", true, "ends a second after DTSTART"},
				{"20301105T103000Z", "20301105T110000Z", true, "inside"},
				{"20301105T080000Z", "20301105T140000Z", true, "covers the task"},
				{"20301105T120000Z", "20301105T130000Z", true, "starts at DTSTART+DURATION"},
				{"20301105T120001Z", "20301105T130000Z", false, "starts after DTSTART+DURATION"},
				{"", "20301105T100000Z", false, "open start, ends at DTSTART"},
				{"", "20301105T100001Z", true, "open start"},
				{"20301105T120000Z", "", true, "open end from DTSTART+DURATION"},
				{"20301105T120001Z", "", false, "open end after DTSTART+DURATION"},
			},
		},
		{
			name:  "DTSTART+DURATION of zero: end >= DTSTART+DURATION takes the window that ends at DTSTART",
			lines: []string{"DTSTART:20301105T100000Z", "DURATION:PT0S"},
			windows: []window{
				{"20301105T090000Z", "20301105T100000Z", true, "ends at DTSTART = DTSTART+DURATION"},
				{"20301105T100000Z", "20301105T110000Z", true, "starts at DTSTART"},
				{"20301105T100001Z", "20301105T110000Z", false, "starts after it"},
				{"20301105T090000Z", "20301105T095959Z", false, "ends before it"},
			},
		},
		{
			name:  "DTSTART+DUE: ((start < DUE) OR (start <= DTSTART)) AND ((end > DTSTART) OR (end >= DUE))",
			lines: []string{"DTSTART:20301105T100000Z", "DUE:20301105T120000Z"},
			windows: []window{
				{"20301105T090000Z", "20301105T100000Z", false, "ends at DTSTART"},
				{"20301105T090000Z", "20301105T100001Z", true, "ends a second after DTSTART"},
				{"20301105T110000Z", "20301105T113000Z", true, "inside"},
				{"20301105T080000Z", "20301105T140000Z", true, "covers the task"},
				{"20301105T115959Z", "20301105T130000Z", true, "starts a second before DUE"},
				{"20301105T120000Z", "20301105T130000Z", false, "starts at DUE"},
				{"", "20301105T100000Z", false, "open start, ends at DTSTART"},
				{"", "20301105T100001Z", true, "open start"},
				{"20301105T115959Z", "", true, "open end"},
				{"20301105T120000Z", "", false, "open end from DUE"},
			},
		},
		{
			name:  "DTSTART equal to DUE: both ends of the instant match",
			lines: []string{"DTSTART:20301105T100000Z", "DUE:20301105T100000Z"},
			windows: []window{
				{"20301105T090000Z", "20301105T100000Z", true, "ends at the instant (end >= DUE)"},
				{"20301105T100000Z", "20301105T110000Z", true, "starts at the instant (start <= DTSTART)"},
				{"20301105T100001Z", "20301105T110000Z", false, "starts after it"},
				{"20301105T090000Z", "20301105T095959Z", false, "ends before it"},
			},
		},
		{
			name:  "DTSTART only: (start <= DTSTART) AND (end > DTSTART)",
			lines: []string{"DTSTART:20301105T100000Z"},
			windows: []window{
				{"20301105T100000Z", "20301105T110000Z", true, "starts at DTSTART"},
				{"20301105T090000Z", "20301105T100000Z", false, "ends at DTSTART"},
				{"20301105T090000Z", "20301105T100001Z", true, "ends a second after DTSTART"},
				{"20301105T100001Z", "20301105T110000Z", false, "started before the window: not in it (RFC)"},
				{"20301105T100000Z", "", true, "open end from DTSTART"},
				{"", "20301105T100000Z", false, "open start, ends at DTSTART"},
				{"", "", true, "both open"},
			},
		},
		{
			name:  "DUE only: (start < DUE) AND (end >= DUE)",
			lines: []string{"DUE:20301105T120000Z"},
			windows: []window{
				{"20301105T110000Z", "20301105T120000Z", true, "ends at DUE"},
				{"20301105T110000Z", "20301105T115959Z", false, "ends before DUE"},
				{"20301105T120000Z", "20301105T130000Z", false, "starts at DUE"},
				{"20301105T115959Z", "20301105T130000Z", true, "starts a second before DUE"},
				{"", "20301105T120000Z", true, "open start"},
				{"20301105T115959Z", "", true, "open end"},
				{"20301105T120000Z", "", false, "open end from DUE"},
			},
		},
		{
			name:  "COMPLETED+CREATED: ((start <= CREATED) OR (start <= COMPLETED)) AND ((end >= CREATED) OR (end >= COMPLETED))",
			lines: []string{"CREATED:20301101T080000Z", "COMPLETED:20301103T080000Z", "STATUS:COMPLETED"},
			windows: []window{
				{"20301102T000000Z", "20301102T010000Z", true, "between CREATED and COMPLETED"},
				{"20301101T070000Z", "20301101T080000Z", true, "ends at CREATED"},
				{"20301101T070000Z", "20301101T075959Z", false, "ends before CREATED"},
				{"20301103T080000Z", "20301103T090000Z", true, "starts at COMPLETED"},
				{"20301103T080001Z", "20301103T090000Z", false, "starts after COMPLETED"},
				{"", "20301101T080000Z", true, "open start"},
				{"20301103T080001Z", "", false, "open end after COMPLETED"},
			},
		},
		{
			name:  "COMPLETED before CREATED (client clock skew): the ORs still hold",
			lines: []string{"CREATED:20301103T080000Z", "COMPLETED:20301101T080000Z"},
			windows: []window{
				{"20301102T000000Z", "20301102T010000Z", true, "between them"},
				{"20301101T070000Z", "20301101T080000Z", true, "ends at COMPLETED"},
				{"20301103T080000Z", "20301103T090000Z", true, "starts at CREATED"},
				{"20301103T080001Z", "20301103T090000Z", false, "after both"},
			},
		},
		{
			name:  "COMPLETED only: (start <= COMPLETED) AND (end >= COMPLETED)",
			lines: []string{"COMPLETED:20301103T080000Z"},
			windows: []window{
				{"20301103T070000Z", "20301103T080000Z", true, "ends at COMPLETED"},
				{"20301103T080000Z", "20301103T090000Z", true, "starts at COMPLETED"},
				{"20301103T080001Z", "20301103T090000Z", false, "starts after COMPLETED"},
				{"20301103T070000Z", "20301103T075959Z", false, "ends before COMPLETED"},
			},
		},
		{
			name:  "CREATED only: (end > CREATED)",
			lines: []string{"CREATED:20301101T080000Z"},
			windows: []window{
				{"20301101T070000Z", "20301101T080000Z", false, "ends at CREATED"},
				{"20301101T070000Z", "20301101T080001Z", true, "ends a second after CREATED"},
				{"20310101T000000Z", "20310102T000000Z", true, "any later window"},
				{"", "20301101T080000Z", false, "open start, ends at CREATED"},
				{"20990101T000000Z", "", true, "open end"},
			},
		},
		{
			name:  "none of DTSTART, DURATION, DUE, COMPLETED, CREATED: TRUE",
			lines: []string{"SUMMARY:Someday"},
			windows: []window{
				{"20301105T000000Z", "20301106T000000Z", true, "any window"},
				{"19700101T000000Z", "19700101T000001Z", true, "any window"},
				{"", "19700101T000001Z", true, "open start"},
				{"20990101T000000Z", "", true, "open end"},
			},
		},

		// Value forms.
		{
			name:  "all-day DUE is midnight UTC of its date",
			lines: []string{"DUE;VALUE=DATE:20301105"},
			windows: []window{
				{"20301104T230000Z", "20301105T000000Z", true, "ends at midnight"},
				{"20301105T000000Z", "20301106T000000Z", false, "the day itself starts at DUE"},
			},
		},
		{
			name:  "all-day DTSTART and DUE",
			lines: []string{"DTSTART;VALUE=DATE:20301104", "DUE;VALUE=DATE:20301106"},
			windows: []window{
				{"20301105T000000Z", "20301105T010000Z", true, "between them"},
				{"20301103T000000Z", "20301104T000000Z", false, "ends at DTSTART"},
				{"20301106T000000Z", "20301107T000000Z", false, "starts at DUE"},
			},
		},
		{
			name:  "a TZID DUE is compared as its instant",
			lines: []string{"DUE;TZID=Europe/Berlin:20301105T120000"},
			windows: []window{
				{"20301105T100000Z", "20301105T110000Z", true, "ends at 11:00Z = 12:00 Berlin"},
				{"20301105T110000Z", "20301105T120000Z", false, "starts at 11:00Z"},
			},
		},
		{
			name:  "a floating DUE is read as UTC wall time (R3)",
			lines: []string{"DUE:20301105T120000"},
			windows: []window{
				{"20301105T110000Z", "20301105T120000Z", true, "ends at 12:00Z"},
				{"20301105T120000Z", "20301105T130000Z", false, "starts at 12:00Z"},
			},
		},
		{
			name:  "DURATION next to DUE (RFC 5545 forbids it): DUE wins",
			lines: []string{"DTSTART:20301105T100000Z", "DURATION:PT1H", "DUE:20301105T120000Z"},
			windows: []window{
				{"20301105T113000Z", "20301105T114500Z", true, "after DTSTART+DURATION, before DUE"},
				{"20301105T120000Z", "20301105T130000Z", false, "starts at DUE"},
			},
		},
		{
			name:  "DURATION without DTSTART is ignored: the DUE row decides",
			lines: []string{"DURATION:PT5H", "DUE:20301105T120000Z"},
			windows: []window{
				{"20301105T110000Z", "20301105T120000Z", true, "ends at DUE"},
				{"20301105T120000Z", "20301105T130000Z", false, "starts at DUE"},
			},
		},
		{
			name:  "a negative DURATION is malformed: the DTSTART-only row decides",
			lines: []string{"DTSTART:20301105T100000Z", "DURATION:-PT1H"},
			windows: []window{
				{"20301105T100000Z", "20301105T110000Z", true, "starts at DTSTART (DTSTART-only row)"},
				{"20301105T083000Z", "20301105T090000Z", false, "would hold DTSTART+DURATION"},
			},
		},
		{
			name:  "an unparseable DTSTART is absent: the DUE row decides",
			lines: []string{"DTSTART:tomorrow", "DUE:20301105T120000Z"},
			windows: []window{
				{"20301105T110000Z", "20301105T120000Z", true, "ends at DUE"},
				{"20301105T120000Z", "20301105T130000Z", false, "starts at DUE"},
			},
		},
		{
			name:  "a DUE in an unknown TZID is absent: the CREATED row decides",
			lines: []string{"DUE;TZID=Mars/Olympus_Mons:20301105T120000", "CREATED:20301101T080000Z"},
			windows: []window{
				{"20301101T070000Z", "20301101T080001Z", true, "ends after CREATED"},
				{"20301101T070000Z", "20301101T080000Z", false, "ends at CREATED"},
			},
		},
		{
			name:  "STATUS plays no part: a cancelled task with a DUE is matched by its DUE",
			lines: []string{"DUE:20301105T120000Z", "STATUS:CANCELLED"},
			windows: []window{
				{"20301105T110000Z", "20301105T120000Z", true, "ends at DUE"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runWindows(t, candidate(t, "task", nil, vtodo("task@test", tc.lines...)), tc.windows)
		})
	}
}

// ---- recurring VTODOs -------------------------------------------------------

// farFutureEnd is what PopulateDenormFieldsFromICal stores as the
// RecurrenceEndTime of an unbounded series.
var farFutureEnd = time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)

// TestVTODOTimeRangeRecurring covers a recurring master: it overlaps a window
// when [DTSTART else DUE, RecurrenceEndTime + DTSTART..DUE] does, and a nil
// RecurrenceEndTime is unbounded.
func TestVTODOTimeRangeRecurring(t *testing.T) {
	t.Run("DTSTART with a past first DUE matches a future window", func(t *testing.T) {
		weekly := vtodo("weekly@test", "DTSTART:20200101T090000Z", "DUE:20200101T100000Z", "RRULE:FREQ=WEEKLY")
		for _, end := range []*time.Time{&farFutureEnd, nil} {
			runWindows(t, candidate(t, "weekly", end, weekly), []window{
				{"20301105T000000Z", "20301112T000000Z", true, "a week in 2030"},
				{"20301105T100000Z", "", true, "open end"},
				{"", "20200101T085959Z", false, "ends before the first DTSTART"},
				{"20191201T000000Z", "20200101T090000Z", true, "ends at the first DTSTART"},
			})
		}
	})

	t.Run("no DTSTART (DUE+RRULE, Tasks.org) with a past first DUE matches a future window", func(t *testing.T) {
		// PopulateDenormFieldsFromICal leaves RecurrenceEndTime nil without a
		// DTSTART: unbounded, anchored on DUE.
		daily := vtodo("daily@test", "DUE:20200101T100000Z", "RRULE:FREQ=DAILY", "SUMMARY:Water the plants")
		runWindows(t, candidate(t, "daily", nil, daily), []window{
			{"20301105T000000Z", "20301112T000000Z", true, "a week in 2030"},
			{"20200101T100000Z", "20200101T110000Z", true, "starts at the first DUE"},
			{"20191231T000000Z", "20200101T100000Z", true, "ends at the first DUE"},
			{"20191231T000000Z", "20200101T095959Z", false, "ends before the first DUE"},
			{"", "20200101T095959Z", false, "open start, before the first DUE"},
		})
	})

	t.Run("a bounded series ends at RecurrenceEndTime plus the DTSTART..DUE length", func(t *testing.T) {
		// COUNT=3 from 2020-01-01T09:00: denorm stores the last DTSTART,
		// 2020-01-03T09:00; that occurrence is due an hour later.
		series := vtodo("count@test", "DTSTART:20200101T090000Z", "DUE:20200101T100000Z", "RRULE:FREQ=DAILY;COUNT=3")
		runWindows(t, candidate(t, "count", utcPtr(t, "20200103T090000Z"), series), []window{
			{"20200103T093000Z", "20200103T094500Z", true, "inside the last occurrence, after its DTSTART"},
			{"20200103T100000Z", "20200103T110000Z", true, "starts at the last DUE"},
			{"20200103T100001Z", "20200103T110000Z", false, "starts after the last DUE"},
			{"20301105T000000Z", "20301112T000000Z", false, "long after the series"},
			{"20301105T000000Z", "", false, "open end after the series"},
			{"", "20200101T090000Z", true, "open start, ends at the first DTSTART"},
			{"", "", true, "both open"},
		})
	})

	t.Run("a bounded series without DTSTART is anchored on DUE", func(t *testing.T) {
		series := vtodo("due-count@test", "DUE:20200101T100000Z", "RRULE:FREQ=DAILY;COUNT=5")
		runWindows(t, candidate(t, "due-count", utcPtr(t, "20200105T100000Z"), series), []window{
			{"20200101T090000Z", "20200101T100000Z", true, "ends at the first DUE"},
			{"20200101T090000Z", "20200101T095959Z", false, "ends before the first DUE"},
			{"20200105T100000Z", "20200105T110000Z", true, "starts at the last DUE"},
			{"20200105T100001Z", "20200105T110000Z", false, "after the series"},
		})
	})

	t.Run("RDATE without RRULE is recurring too", func(t *testing.T) {
		rdate := vtodo("rdate@test", "DTSTART:20200101T090000Z", "RDATE:20300101T090000Z")
		runWindows(t, candidate(t, "rdate", utcPtr(t, "20300101T090000Z"), rdate), []window{
			{"20250101T000000Z", "20250102T000000Z", true, "between the instances: over-included, which is safe"},
			{"20300101T090000Z", "20300101T100000Z", true, "starts at the RDATE"},
			{"20310101T000000Z", "20310102T000000Z", false, "after the last instance"},
		})
	})

	t.Run("an RRULE go-ical cannot parse matches instead of failing the REPORT", func(t *testing.T) {
		// go-webdav's RecurrenceSet would return an error and fail the whole
		// REPORT; denorm stores the far future for such a rule.
		broken := vtodo("broken@test", "DTSTART:20200101T090000Z", "RRULE:FREQ=SOMETIMES")
		runWindows(t, candidate(t, "broken", &farFutureEnd, broken), []window{
			{"20301105T000000Z", "20301112T000000Z", true, "after the first DTSTART"},
		})
	})

	t.Run("a series with neither DTSTART nor DUE has no anchor: the table decides", func(t *testing.T) {
		unanchored := vtodo("unanchored@test", "RRULE:FREQ=DAILY", "CREATED:20301101T080000Z")
		runWindows(t, candidate(t, "unanchored", nil, unanchored), []window{
			{"20301001T000000Z", "20301101T080000Z", false, "ends at CREATED"},
			{"20301105T000000Z", "20301106T000000Z", true, "after CREATED"},
		})
	})

	t.Run("an override is matched by its own dates", func(t *testing.T) {
		master := vtodo("moved@test", "DTSTART:20200101T090000Z", "DUE:20200101T100000Z",
			"RRULE:FREQ=DAILY;COUNT=3", "SUMMARY:Series")
		override := vtodo("moved@test", "RECURRENCE-ID:20200102T090000Z",
			"DTSTART:20310601T090000Z", "DUE:20310601T100000Z", "SUMMARY:Moved")
		c := candidate(t, "moved", utcPtr(t, "20200103T090000Z"), override, master)
		runWindows(t, c, []window{
			{"20310601T093000Z", "20310601T094500Z", true, "the moved instance (override listed first)"},
			{"20200102T093000Z", "20200102T094500Z", true, "the series"},
			{"20301105T000000Z", "20301112T000000Z", false, "neither"},
			{"20310601T100000Z", "20310601T110000Z", false, "starts at the override's DUE"},
		})
	})
}

// TestVTODOTimeRangeNeverExpandsTheSeries pins that a recurring VTODO is never
// expanded: go-webdav's matcher would call RecurrenceSet, which anchors a
// DUE-only task at year 1 (and a SECONDLY rule makes that 6·10^10 instances).
// The match must be a comparison of two instants.
func TestVTODOTimeRangeNeverExpandsTheSeries(t *testing.T) {
	candidates := []queryCandidate{
		candidate(t, "no-dtstart", nil, vtodo("no-dtstart@test", "DUE:20200101T100000Z", "RRULE:FREQ=SECONDLY")),
		candidate(t, "dtstart", &farFutureEnd, vtodo("dtstart@test", "DTSTART:20000101T000000Z", "RRULE:FREQ=SECONDLY")),
	}
	query := vtodoQuery(t, "20301105T000000Z", "20301112T000000Z", nil)

	done := make(chan []string, 1)
	go func() {
		out, err := filterCalendarQuery(query, candidates)
		if err != nil {
			done <- []string{"error: " + err.Error()}
			return
		}
		done <- objectNames(out)
	}()
	select {
	case got := <-done:
		assert.Equal(t, []string{"no-dtstart", "dtstart"}, got)
	case <-time.After(10 * time.Second):
		t.Fatal("the VTODO time-range match is expanding the recurrence set")
	}
}

// ---- the rest of the query --------------------------------------------------

// TestVTODOTimeRangeWithOtherConditions: the VTODO filter's prop-filters and
// nested comp-filters must hold for the same component the time-range picked,
// and the rest of the query is still go-webdav's.
func TestVTODOTimeRangeWithOtherConditions(t *testing.T) {
	inRange := candidate(t, "dentist", nil, vtodo("dentist@test", "DUE:20301105T120000Z", "SUMMARY:Call the dentist"))
	alsoInRange := candidate(t, "groceries", nil, vtodo("groceries@test", "DUE:20301105T150000Z", "SUMMARY:Groceries"))
	done := candidate(t, "done", nil, vtodo("done@test", "DUE:20301105T130000Z", "SUMMARY:Dentist bill",
		"STATUS:COMPLETED", "COMPLETED:20301104T080000Z"))
	withAlarm := candidate(t, "alarm", nil, vtodo("alarm@test", "DUE:20301105T160000Z", "SUMMARY:Pills",
		component(ical.CompAlarm, "ACTION:DISPLAY", "TRIGGER:-PT15M", "DESCRIPTION:Pills")))
	outOfRange := candidate(t, "later", nil, vtodo("later@test", "DUE:20301205T120000Z", "SUMMARY:Dentist again"))
	event := candidate(t, "event", nil, component(ical.CompEvent, "UID:event@test", "DTSTAMP:20200101T000000Z",
		"DTSTART:20301105T100000Z", "DTEND:20301105T110000Z", "SUMMARY:Dentist"))
	all := []queryCandidate{inRange, alsoInRange, done, withAlarm, outOfRange, event}
	const start, end = "20301105T000000Z", "20301106T000000Z"

	t.Run("time-range alone: the tasks due that day, never the event", func(t *testing.T) {
		assert.Equal(t, []string{"dentist", "groceries", "done", "alarm"}, matched(t, vtodoQuery(t, start, end, nil), all...))
	})

	t.Run("plus a text-match", func(t *testing.T) {
		text := []caldav.PropFilter{{Name: ical.PropSummary, TextMatch: &caldav.TextMatch{Text: "entist"}}}
		assert.Equal(t, []string{"dentist", "done"}, matched(t, vtodoQuery(t, start, end, text), all...))
	})

	t.Run("plus a nested VALARM comp-filter", func(t *testing.T) {
		assert.Equal(t, []string{"alarm"},
			matched(t, vtodoQuery(t, start, end, nil, caldav.CompFilter{Name: ical.CompAlarm}), all...))
	})

	t.Run("a VCALENDAR-level prop-filter still applies", func(t *testing.T) {
		q := vtodoQuery(t, start, end, nil)
		q.CompFilter.Props = []caldav.PropFilter{{Name: ical.PropProductID, TextMatch: &caldav.TextMatch{Text: "Other"}}}
		assert.Equal(t, []string{}, matched(t, q, all...))
		q.CompFilter.Props[0].TextMatch.Text = "CalCard"
		assert.Equal(t, []string{"dentist", "groceries", "done", "alarm"}, matched(t, q, all...))
	})

	t.Run("a second comp-filter is ANDed (a task-only object has no VEVENT)", func(t *testing.T) {
		q := vtodoQuery(t, start, end, nil)
		q.CompFilter.Comps = append(q.CompFilter.Comps, caldav.CompFilter{Name: ical.CompEvent})
		assert.Equal(t, []string{}, matched(t, q, all...))
	})

	t.Run("the conditions must hold for the same VTODO", func(t *testing.T) {
		master := vtodo("series@test", "DTSTART:20200101T090000Z", "DUE:20200101T100000Z",
			"RRULE:FREQ=DAILY;COUNT=3", "SUMMARY:Series")
		override := vtodo("series@test", "RECURRENCE-ID:20200102T090000Z",
			"DTSTART:20310601T090000Z", "DUE:20310601T100000Z", "SUMMARY:Moved")
		series := candidate(t, "series", utcPtr(t, "20200103T090000Z"), master, override)
		moved := []caldav.PropFilter{{Name: ical.PropSummary, TextMatch: &caldav.TextMatch{Text: "Moved"}}}
		assert.Equal(t, []string{}, matched(t, vtodoQuery(t, "20200102T000000Z", "20200103T000000Z", moved), series),
			"the master is in range but is not 'Moved'; the override is 'Moved' but not in range")
		assert.Equal(t, []string{"series"}, matched(t, vtodoQuery(t, "20310601T000000Z", "20310602T000000Z", moved), series))
	})
}

// TestCalendarQueryWithoutVTODOTimeRangeIsGoWebdavs: every query that has no
// VTODO time-range — VEVENT time-ranges, plain component filters, prop-filters,
// text-match, is-not-defined, a prop-level time-range — returns exactly what
// caldav.Filter returns, in the same order.
func TestCalendarQueryWithoutVTODOTimeRangeIsGoWebdavs(t *testing.T) {
	event := func(uid string, lines ...string) string {
		return component(ical.CompEvent, append([]string{"UID:" + uid, "DTSTAMP:20200101T000000Z"}, lines...)...)
	}
	candidates := []queryCandidate{
		candidate(t, "meeting", nil, event("meeting@test", "DTSTART:20301105T100000Z", "DTEND:20301105T110000Z", "SUMMARY:Dentist")),
		candidate(t, "standup", &farFutureEnd, event("standup@test", "DTSTART:20300101T090000Z", "DTEND:20300101T091500Z",
			"RRULE:FREQ=WEEKLY", "SUMMARY:Standup")),
		candidate(t, "due", nil, vtodo("due@test", "DUE:20301105T120000Z", "SUMMARY:Call the dentist")),
		candidate(t, "done", nil, vtodo("done@test", "DUE:20301105T120000Z", "COMPLETED:20301104T080000Z", "SUMMARY:Done")),
		candidate(t, "repeat", nil, vtodo("repeat@test", "DTSTART:20300101T090000Z", "RRULE:FREQ=DAILY;COUNT=2",
			component(ical.CompAlarm, "ACTION:DISPLAY", "TRIGGER:-PT15M", "DESCRIPTION:x"))),
	}
	objects := make([]caldav.CalendarObject, len(candidates))
	for i := range candidates {
		objects[i] = candidates[i].object
	}

	under := func(comps ...caldav.CompFilter) *caldav.CalendarQuery {
		return &caldav.CalendarQuery{CompFilter: caldav.CompFilter{Name: ical.CompCalendar, Comps: comps}}
	}
	queries := map[string]*caldav.CalendarQuery{
		"nil query":         nil,
		"VCALENDAR only":    under(),
		"VEVENT":            under(caldav.CompFilter{Name: ical.CompEvent}),
		"VTODO":             under(caldav.CompFilter{Name: ical.CompToDo}),
		"VEVENT time-range": under(caldav.CompFilter{Name: ical.CompEvent, Start: utc(t, "20301105T000000Z"), End: utc(t, "20301106T000000Z")}),
		"recurring VEVENT time-range": under(caldav.CompFilter{Name: ical.CompEvent,
			Start: utc(t, "20310101T000000Z"), End: utc(t, "20310201T000000Z")}),
		"VEVENT time-range without start (go-webdav ignores it)": under(caldav.CompFilter{Name: ical.CompEvent,
			End: utc(t, "20200101T000000Z")}),
		"VTODO text-match": under(caldav.CompFilter{Name: ical.CompToDo,
			Props: []caldav.PropFilter{{Name: ical.PropSummary, TextMatch: &caldav.TextMatch{Text: "dentist"}}}}),
		"VTODO COMPLETED is-not-defined": under(caldav.CompFilter{Name: ical.CompToDo,
			Props: []caldav.PropFilter{{Name: ical.PropCompleted, IsNotDefined: true}}}),
		"VTODO with VALARM":     under(caldav.CompFilter{Name: ical.CompToDo, Comps: []caldav.CompFilter{{Name: ical.CompAlarm}}}),
		"VEVENT is-not-defined": under(caldav.CompFilter{Name: ical.CompEvent, IsNotDefined: true}),
		"VTODO prop-level DUE time-range": under(caldav.CompFilter{Name: ical.CompToDo,
			Props: []caldav.PropFilter{{Name: ical.PropDue, Start: utc(t, "20301105T000000Z"), End: utc(t, "20301106T000000Z")}}}),
		"VTODO time-range not under VCALENDAR": {CompFilter: caldav.CompFilter{Name: ical.CompToDo,
			Start: utc(t, "20301105T000000Z"), End: utc(t, "20301106T000000Z")}},
		"VTODO time-range one level too deep": under(caldav.CompFilter{Name: ical.CompToDo,
			Comps: []caldav.CompFilter{{Name: ical.CompToDo, Start: utc(t, "20301105T000000Z")}}}),
	}
	for name, q := range queries {
		t.Run(name, func(t *testing.T) {
			want, wantErr := caldav.Filter(q, slices.Clone(objects))
			got, gotErr := filterCalendarQuery(q, candidates)
			assert.Equal(t, wantErr, gotErr)
			assert.Equal(t, objectNames(want), objectNames(got))
		})
	}
}

// ---- over the DAV handler ---------------------------------------------------

// TestCalDAVReportVTODOTimeRange runs a VTODO time-range calendar-query through
// the real handler and repository: what QueryCalendarObjects hands the matcher
// (decoded objects with their denormalised RecurrenceEndTime), a series that
// ended in 2020 staying out of a 2030 window, the Tasks.org no-DTSTART
// SECONDLY series coming back without being expanded, and the VEVENT query
// on the same collection unchanged.
func TestCalDAVReportVTODOTimeRange(t *testing.T) {
	app, db, _ := setupTestApp(t)
	defer db.Close()

	userRepo := repository.NewUserRepository(db.DB())
	hash, _ := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	require.NoError(t, userRepo.Create(context.Background(), &user.User{
		UUID: "vtodo-range-user", Email: "vtodorange@example.com", Username: "vtodorangeuser",
		PasswordHash: string(hash), IsActive: true,
	}))
	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte("vtodorange@example.com:password"))
	do := func(method, url, depth, body string) (int, string) {
		req, _ := http.NewRequest(method, url, strings.NewReader(body))
		req.Header.Set("Authorization", auth)
		if depth != "" {
			req.Header.Set("Depth", depth)
		}
		switch method {
		case http.MethodPut:
			req.Header.Set("Content-Type", "text/calendar; charset=utf-8")
		case "REPORT":
			req.Header.Set("Content-Type", "application/xml; charset=utf-8")
		}
		resp := davDo(t, app, req)
		data, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(data)
	}

	const collection = "/dav/vtodorangeuser/calendars/mixed/"
	status, _ := do("MKCOL", collection, "", "")
	require.Equal(t, fiber.StatusCreated, status)

	objects := map[string]string{
		"due-in":   vtodo("due-in@test", "DUE:20301105T120000Z"),
		"due-out":  vtodo("due-out@test", "DUE:20301205T120000Z"),
		"tasksorg": vtodo("tasksorg@test", "DUE:20200101T100000Z", "RRULE:FREQ=SECONDLY"),
		"ended":    vtodo("ended@test", "DTSTART:20200101T090000Z", "DUE:20200101T100000Z", "RRULE:FREQ=DAILY;COUNT=3"),
		"weekly":   vtodo("weekly@test", "DTSTART:20200101T090000Z", "DUE:20200101T100000Z", "RRULE:FREQ=WEEKLY"),
		"event": component(ical.CompEvent, "UID:event@test", "DTSTAMP:20200101T000000Z",
			"DTSTART:20301105T100000Z", "DTEND:20301105T110000Z"),
	}
	for name, comp := range objects {
		status, body := do(http.MethodPut, collection+name+".ics", "",
			"BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//CalCard Tests//EN\r\n"+comp+"END:VCALENDAR\r\n")
		require.Equalf(t, http.StatusCreated, status, "PUT %s: %s", name, body)
	}

	report := func(comp, start, end string) []string {
		t.Helper()
		body := `<?xml version="1.0" encoding="utf-8"?>` +
			`<C:calendar-query xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">` +
			`<D:prop><D:getetag/></D:prop><C:filter><C:comp-filter name="VCALENDAR">` +
			`<C:comp-filter name="` + comp + `"><C:time-range start="` + start + `" end="` + end + `"/>` +
			`</C:comp-filter></C:comp-filter></C:filter></C:calendar-query>`
		status, resp := do("REPORT", collection, "1", body)
		require.Equalf(t, http.StatusMultiStatus, status, "REPORT: %s", resp)
		var names []string
		for _, m := range regexp.MustCompile(`([a-z-]+)\.ics</`).FindAllStringSubmatch(resp, -1) {
			names = append(names, m[1])
		}
		slices.Sort(names)
		return names
	}

	assert.Equal(t, []string{"due-in", "tasksorg", "weekly"}, report(ical.CompToDo, "20301105T000000Z", "20301106T000000Z"),
		"non-recurring task due that day, the repeating ones; not the ended series, the later task or the event")
	assert.Equal(t, []string{"ended", "tasksorg", "weekly"}, report(ical.CompToDo, "20200103T093000Z", "20200103T094500Z"),
		"inside the ended series' last occurrence (its stored RecurrenceEndTime plus DTSTART..DUE)")
	assert.Equal(t, []string{"event"}, report(ical.CompEvent, "20301105T000000Z", "20301106T000000Z"),
		"the VEVENT query on the same collection")
}
