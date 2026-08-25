package subscription

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func feed(body string) string {
	return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Test//EN\r\n" + body + "END:VCALENDAR\r\n"
}

func event(uid, extra string) string {
	return "BEGIN:VEVENT\r\nUID:" + uid + "\r\nDTSTAMP:20260101T000000Z\r\n" + extra + "END:VEVENT\r\n"
}

func TestParseFeedReadsTheFeedsOwnMetadata(t *testing.T) {
	f, err := ParseFeed([]byte(feed(
		"X-WR-CALNAME:Vollmondzeiten\r\nX-WR-CALDESC:Phases\r\nX-WR-TIMEZONE:Europe/Berlin\r\n" +
			event("a@example.com", "DTSTART:20260301T100000Z\r\nSUMMARY:Full moon\r\n"))))
	require.NoError(t, err)

	assert.Equal(t, "Vollmondzeiten", f.Name)
	assert.Equal(t, "Phases", f.Description)
	assert.Equal(t, "Europe/Berlin", f.Timezone)
	require.Len(t, f.Objects, 1)
	assert.Equal(t, "Full moon", f.Objects[0].Summary)
}

func TestParseFeedGroupsRecurrenceOverridesIntoOneResource(t *testing.T) {
	f, err := ParseFeed([]byte(feed(
		event("series@example.com", "DTSTART:20260301T100000Z\r\nRRULE:FREQ=WEEKLY;COUNT=5\r\nSUMMARY:Standup\r\n") +
			event("series@example.com", "RECURRENCE-ID:20260308T100000Z\r\nDTSTART:20260308T140000Z\r\nSUMMARY:Standup (moved)\r\n") +
			event("other@example.com", "DTSTART:20260401T100000Z\r\nSUMMARY:Other\r\n"))))
	require.NoError(t, err)

	// REVERT PROOF: one object per VEVENT would publish the override as an
	// independent event, so the same meeting appears twice — once at its
	// original time and once at the moved one.
	require.Len(t, f.Objects, 2)

	series := f.Objects[0]
	assert.Equal(t, "series@example.com", series.UID)
	assert.Equal(t, 2, strings.Count(series.ICalData, "BEGIN:VEVENT"))
	// The master (no RECURRENCE-ID) must drive the denormalized columns.
	assert.Equal(t, "Standup", series.Summary)
	assert.NotNil(t, series.RecurrenceEndTime)
}

func TestParseFeedAttachesOnlyTheTimezonesAnObjectReferences(t *testing.T) {
	tz := func(id string) string {
		return "BEGIN:VTIMEZONE\r\nTZID:" + id + "\r\nBEGIN:STANDARD\r\nDTSTART:19701025T030000\r\n" +
			"TZOFFSETFROM:+0200\r\nTZOFFSETTO:+0100\r\nEND:STANDARD\r\nEND:VTIMEZONE\r\n"
	}
	f, err := ParseFeed([]byte(feed(
		tz("Europe/Berlin") + tz("America/New_York") +
			event("berlin@example.com", "DTSTART;TZID=Europe/Berlin:20260301T100000\r\nSUMMARY:Berlin\r\n"))))
	require.NoError(t, err)
	require.Len(t, f.Objects, 1)

	data := f.Objects[0].ICalData
	assert.Contains(t, data, "TZID:Europe/Berlin")
	// A feed declaring twenty zones must not attach all twenty to every event.
	assert.NotContains(t, data, "America/New_York")
}

func TestParseFeedSkipsComponentsItCannotIdentify(t *testing.T) {
	f, err := ParseFeed([]byte(feed(
		"BEGIN:VEVENT\r\nDTSTAMP:20260101T000000Z\r\nDTSTART:20260301T100000Z\r\nSUMMARY:No UID\r\nEND:VEVENT\r\n" +
			event("good@example.com", "DTSTART:20260301T100000Z\r\nSUMMARY:Fine\r\n"))))
	require.NoError(t, err)

	// One bad component in a feed of hundreds must not cost the user the rest.
	// A UID-less event is unmatchable, so keeping it would mean deleting and
	// recreating it on every single refresh.
	assert.Equal(t, 1, f.Skipped)
	require.Len(t, f.Objects, 1)
	assert.Equal(t, "good@example.com", f.Objects[0].UID)
}

func TestParseFeedSuppliesAMissingDTSTAMP(t *testing.T) {
	body := feed("BEGIN:VEVENT\r\nUID:a@example.com\r\nDTSTART:20260301T100000Z\r\nSUMMARY:No stamp\r\nEND:VEVENT\r\n")

	f, err := ParseFeed([]byte(body))
	require.NoError(t, err)
	require.Len(t, f.Objects, 1, "an event without DTSTAMP is common enough that dropping it loses real data")
	assert.Contains(t, f.Objects[0].ICalData, "DTSTAMP:")

	// And the synthetic stamp must be FIXED, not time.Now(): a per-sync value
	// would change the object's bytes on every refresh, so every sync would
	// report every event as modified and bump the CTag, waking every
	// connected DAV client hourly.
	again, err := ParseFeed([]byte(body))
	require.NoError(t, err)
	assert.Equal(t, f.Objects[0].ICalData, again.Objects[0].ICalData)
}

func TestParseFeedIsByteStableAcrossRuns(t *testing.T) {
	body := feed(
		"BEGIN:VTIMEZONE\r\nTZID:Europe/Berlin\r\nBEGIN:STANDARD\r\nDTSTART:19701025T030000\r\n" +
			"TZOFFSETFROM:+0200\r\nTZOFFSETTO:+0100\r\nEND:STANDARD\r\nEND:VTIMEZONE\r\n" +
			"BEGIN:VTIMEZONE\r\nTZID:America/New_York\r\nBEGIN:STANDARD\r\nDTSTART:19701101T020000\r\n" +
			"TZOFFSETFROM:-0400\r\nTZOFFSETTO:-0500\r\nEND:STANDARD\r\nEND:VTIMEZONE\r\n" +
			event("multi@example.com",
				"DTSTART;TZID=Europe/Berlin:20260301T100000\r\nDTEND;TZID=America/New_York:20260301T060000\r\nSUMMARY:Two zones\r\n"))

	// Props is a Go map, so anything that iterates it without sorting produces
	// a different byte order per run. That would make the unchanged-feed
	// comparison in ReplaceFeedObjects report every event as modified.
	first, err := ParseFeed([]byte(body))
	require.NoError(t, err)
	for i := 0; i < 25; i++ {
		next, err := ParseFeed([]byte(body))
		require.NoError(t, err)
		require.Equal(t, first.Objects[0].ICalData, next.Objects[0].ICalData, "run %d differs", i)
	}
}

func TestParseFeedGivesEachObjectAURLSafePath(t *testing.T) {
	f, err := ParseFeed([]byte(feed(
		event("weird uid/with?chars#and spaces", "DTSTART:20260301T100000Z\r\nSUMMARY:Odd\r\n"))))
	require.NoError(t, err)
	require.Len(t, f.Objects, 1)

	obj := f.Objects[0]
	assert.Equal(t, "weird uid/with?chars#and spaces", obj.UID, "the feed's UID is preserved as identity")
	// The DAV href is derived from our own UUID: a UID is an opaque string
	// that need not survive being a URL path segment.
	assert.Equal(t, obj.UUID+".ics", obj.Path)
	assert.NotContains(t, obj.Path, " ")
	assert.NotContains(t, obj.Path, "/")
}

func TestParseFeedHandlesAnEventWithNoEndTime(t *testing.T) {
	// Verified against a real publisher: a point-in-time event carries only
	// DTSTART, with neither DTEND nor DURATION.
	f, err := ParseFeed([]byte(feed(event("a@example.com", "DTSTART:20260301T100000Z\r\nSUMMARY:Full moon\r\n"))))
	require.NoError(t, err)
	require.Len(t, f.Objects, 1)

	obj := f.Objects[0]
	require.NotNil(t, obj.StartTime)
	require.NotNil(t, obj.EndTime)
	assert.Equal(t, *obj.StartTime, *obj.EndTime, "a zero-length event, not a parse failure")
}

func TestParseFeedRejectsNonCalendarInput(t *testing.T) {
	_, err := ParseFeed([]byte("<html><body>not a calendar</body></html>"))
	assert.ErrorIs(t, err, ErrNotCalendar)
}

func TestParseFeedAcceptsAnEmptyCalendar(t *testing.T) {
	// A holiday calendar for a quiet year is a legitimate feed. Whether that
	// is acceptable is the create use case's call, not the parser's.
	f, err := ParseFeed([]byte(feed("X-WR-CALNAME:Quiet\r\nBEGIN:VEVENT\r\nUID:x\r\nDTSTAMP:20260101T000000Z\r\nEND:VEVENT\r\n")))
	require.NoError(t, err)
	assert.Equal(t, "Quiet", f.Name)
}

func TestParseFeedKeepsVTODOs(t *testing.T) {
	f, err := ParseFeed([]byte(feed(
		"BEGIN:VTODO\r\nUID:t@example.com\r\nDTSTAMP:20260101T000000Z\r\nSUMMARY:Task\r\nDUE:20260301T100000Z\r\nEND:VTODO\r\n")))
	require.NoError(t, err)
	require.Len(t, f.Objects, 1)
	assert.Equal(t, "VTODO", f.Objects[0].ComponentType)
}
