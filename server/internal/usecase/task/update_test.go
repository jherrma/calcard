package task

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests for PATCH …/tasks/:task_id (story 170-11). The clock is the
// complete tests' completeClock (+01:00), so every server-written timestamp
// must read completeStamp.

func updateUC(repo *stubRepo) *UpdateTaskUseCase {
	return NewUpdateTaskUseCase(repo, func() time.Time { return completeClock })
}

// change is a field of the request with a value, cleared one sent as null.
func change[T any](v T) Change[T] { return Change[T]{Set: true, Value: &v} }
func cleared[T any]() Change[T]   { return Change[T]{Set: true} }
func strPtr(s string) *string     { return &s }
func boolPtr(b bool) *bool        { return &b }

// runUpdate updates obj (a task of the task list, calendar 7) with in and
// returns the result and the stub repository.
func runUpdate(t *testing.T, obj *calendar.CalendarObject, in UpdateTaskInput) (*Task, *stubRepo, error) {
	t.Helper()
	repo := repoWith(obj)
	if in.Calendar == nil {
		in.Calendar = taskListCalendar()
	}
	if in.UUID == "" {
		in.UUID = obj.UUID
	}
	task, err := updateUC(repo).Execute(context.Background(), in)
	return task, repo, err
}

// mustUpdate runs an update that must write once and returns the stored data.
func mustUpdate(t *testing.T, obj *calendar.CalendarObject, in UpdateTaskInput) (*Task, string) {
	t.Helper()
	task, repo, err := runUpdate(t, obj, in)
	require.NoError(t, err)
	require.Len(t, repo.updated, 1, "stored once")
	require.Equal(t, repo.updated[0], *task.Object, "the returned task is what was stored")
	return task, repo.updated[0].ICalData
}

// replaceOnce applies old -> new replacements to data, each old occurring
// exactly once.
func replaceOnce(t *testing.T, data string, pairs ...string) string {
	t.Helper()
	require.Zero(t, len(pairs)%2)
	for i := 0; i < len(pairs); i += 2 {
		require.Equal(t, 1, strings.Count(data, pairs[i]), pairs[i])
		data = strings.Replace(data, pairs[i], pairs[i+1], 1)
	}
	return data
}

// bookkeeping is the replacement pairs of the three lines every write of
// clientTaskData changes besides the fields: SEQUENCE 3 -> 4, DTSTAMP and
// LAST-MODIFIED -> the clock.
var bookkeeping = []string{
	"DTSTAMP:20260102T080000Z\r\n", "DTSTAMP:" + completeStamp + "\r\n",
	"LAST-MODIFIED:20260102T080000Z\r\n", "LAST-MODIFIED:" + completeStamp + "\r\n",
	"SEQUENCE:3\r\n", "SEQUENCE:4\r\n",
}

// TestUpdateTask_SetsEveryField: each field sent is written into the stored
// VTODO, in its own value type; the task's other lines stay byte for byte, the
// columns follow, SEQUENCE/LAST-MODIFIED/DTSTAMP and the ETag advance. A TZID
// time whose zone the object already defines adds no VTIMEZONE.
func TestUpdateTask_SetsEveryField(t *testing.T) {
	obj := storedTask(t, "passport", 7, clientTaskData)
	task, data := mustUpdate(t, obj, UpdateTaskInput{
		Summary:         strPtr("  Renew passport "),
		Description:     change("Two photos; one old"),
		Start:           change("2026-11-04T08:00:00Z"),
		Due:             change("2026-11-12T17:00:00+01:00"),
		Priority:        change(5),
		PercentComplete: change(60),
		Status:          strPtr("needs-action"),
		Timezone:        "Europe/Berlin",
	})

	want := replaceOnce(t, clientTaskData, append([]string{
		`DESCRIPTION:Bring two photos\, and the old one` + "\r\n", `DESCRIPTION:Two photos\; one old` + "\r\n",
		"DTSTAMP:20260102T080000Z\r\n", "DTSTAMP:20260102T080000Z\r\nDTSTART;TZID=Europe/Berlin:20261104T090000\r\n",
		"DUE;TZID=Europe/Berlin:20261105T170000\r\n", "DUE;TZID=Europe/Berlin:20261112T170000\r\n",
		"PERCENT-COMPLETE:40\r\n", "PERCENT-COMPLETE:60\r\n",
		"PRIORITY:1\r\n", "PRIORITY:5\r\n",
		"STATUS:IN-PROCESS\r\n", "STATUS:NEEDS-ACTION\r\n",
		"SUMMARY;LANGUAGE=de:Reisepass erneuern\r\n", "SUMMARY:Renew passport\r\n",
	}, bookkeeping...)...)
	assert.Equal(t, want, data, "only the fields sent and the bookkeeping change")

	stored := task.Object
	assert.Equal(t, "Renew passport", stored.Summary)
	assert.Equal(t, calendar.TaskStatusNeedsAction, stored.Status)
	assert.Equal(t, time.Date(2026, 11, 12, 16, 0, 0, 0, time.UTC), *stored.DueAt)
	assert.Equal(t, intPtr(5), stored.Priority)
	assert.Equal(t, intPtr(60), stored.PercentComplete)
	assert.Nil(t, stored.CompletedAt)
	assert.False(t, stored.IsAllDay)
	assert.NotEqual(t, "etag-before", stored.ETag, "a new ETag")
	assert.NotEmpty(t, stored.ETag)
	assert.Equal(t, "passport", stored.UUID)
	require.NotNil(t, task.Details.Start)
	assert.Equal(t, time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC), *task.Details.Start)
}

// TestUpdateTask_AbsentFieldsUntouched: a request naming one field changes
// that property only.
func TestUpdateTask_AbsentFieldsUntouched(t *testing.T) {
	for _, tc := range []struct {
		name  string
		in    UpdateTaskInput
		pairs []string
	}{
		{"summary", UpdateTaskInput{Summary: strPtr("Renew passport")},
			[]string{"SUMMARY;LANGUAGE=de:Reisepass erneuern\r\n", "SUMMARY:Renew passport\r\n"}},
		{"priority", UpdateTaskInput{Priority: change(0)},
			[]string{"PRIORITY:1\r\n", "PRIORITY:0\r\n"}},
		{"percent", UpdateTaskInput{PercentComplete: change(0)},
			[]string{"PERCENT-COMPLETE:40\r\n", "PERCENT-COMPLETE:0\r\n"}},
		{"due in UTC without a timezone", UpdateTaskInput{Due: change("2026-11-06T09:00:00+01:00")},
			[]string{"DUE;TZID=Europe/Berlin:20261105T170000\r\n", "DUE:20261106T080000Z\r\n"}},
		{"status", UpdateTaskInput{Status: strPtr("cancelled")},
			[]string{"STATUS:IN-PROCESS\r\n", "STATUS:CANCELLED\r\n"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, data := mustUpdate(t, storedTask(t, "passport", 7, clientTaskData), tc.in)
			assert.Equal(t, replaceOnce(t, clientTaskData, append(tc.pairs, bookkeeping...)...), data)
		})
	}
}

// TestUpdateTask_ClearsWithNull: null (and "" for the text and time fields)
// removes description, due, start, priority and percent_complete; a removed
// due also takes a DURATION standing in for it.
func TestUpdateTask_ClearsWithNull(t *testing.T) {
	full := []string{"DESCRIPTION:Notes", "DTSTART:20261104T080000Z", "DUE:20261105T080000Z", "PRIORITY:3", "PERCENT-COMPLETE:20"}
	for _, tc := range []struct {
		name string
		in   UpdateTaskInput
		gone []string
	}{
		{"every field null", UpdateTaskInput{Description: cleared[string](), Due: cleared[string](), Start: cleared[string](),
			Priority: cleared[int](), PercentComplete: cleared[int]()},
			[]string{"DESCRIPTION", "DTSTART", "DUE", "PRIORITY", "PERCENT-COMPLETE"}},
		{"empty strings", UpdateTaskInput{Description: change(""), Due: change(""), Start: change("")},
			[]string{"DESCRIPTION", "DTSTART", "DUE"}},
		{"description", UpdateTaskInput{Description: cleared[string]()}, []string{"DESCRIPTION"}},
		{"due", UpdateTaskInput{Due: cleared[string]()}, []string{"DUE"}},
		{"start", UpdateTaskInput{Start: cleared[string]()}, []string{"DTSTART"}},
		{"priority", UpdateTaskInput{Priority: cleared[int]()}, []string{"PRIORITY"}},
		{"percent", UpdateTaskInput{PercentComplete: cleared[int]()}, []string{"PERCENT-COMPLETE"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task, data := mustUpdate(t, simpleTask(t, "t1", full...), tc.in)
			for _, line := range full {
				name, _, _ := strings.Cut(line, ":")
				if containsString(tc.gone, name) {
					assert.Empty(t, propLines(t, data, name), "%s removed", name)
				} else {
					assert.Equal(t, []string{line}, propLines(t, data, name), "%s kept", name)
				}
			}
			assert.Equal(t, "SEQUENCE:1", propLine(t, data, "SEQUENCE"))
			obj := task.Object
			assert.Equal(t, containsString(tc.gone, "DUE"), obj.DueAt == nil)
			assert.Equal(t, containsString(tc.gone, "PRIORITY"), obj.Priority == nil)
			assert.Equal(t, containsString(tc.gone, "PERCENT-COMPLETE"), obj.PercentComplete == nil)
			assert.Equal(t, containsString(tc.gone, "DTSTART"), task.Details.Start == nil)
			assert.Equal(t, containsString(tc.gone, "DESCRIPTION"), obj.Description == "")
		})
	}

	t.Run("due given as a duration", func(t *testing.T) {
		task, data := mustUpdate(t, simpleTask(t, "t1", "DTSTART:20261104T080000Z", "DURATION:PT2H"),
			UpdateTaskInput{Due: cleared[string]()})
		assert.Empty(t, propLines(t, data, "DURATION"))
		assert.Empty(t, propLines(t, data, "DUE"))
		assert.Equal(t, "DTSTART:20261104T080000Z", propLine(t, data, "DTSTART"))
		assert.Nil(t, task.Object.DueAt)
	})
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// TestUpdateTask_NoOp: a request that changes nothing — no field at all, or
// every field equal to what is stored — writes nothing and answers the task as
// stored with its ETag. A value equal to the stored one keeps the stored
// property: the SUMMARY's LANGUAGE, the DUE's TZID (same instant sent in UTC
// or another zone), a lower-case STATUS, PRIORITY with its padding.
func TestUpdateTask_NoOp(t *testing.T) {
	for _, tc := range []struct {
		name  string
		props []string // nil: clientTaskData
		in    UpdateTaskInput
	}{
		{"no field", nil, UpdateTaskInput{}},
		{"timezone alone", nil, UpdateTaskInput{Timezone: "America/New_York"}},
		{"every field as stored", nil, UpdateTaskInput{
			Summary:         strPtr("Reisepass erneuern"),
			Description:     change("Bring two photos, and the old one"),
			Due:             change("2026-11-05T16:00:00Z"),
			DueAllDay:       boolPtr(false),
			Priority:        change(1),
			PercentComplete: change(40),
			Status:          strPtr("in-process"),
		}},
		{"the due in another zone", nil, UpdateTaskInput{Due: change("2026-11-05T11:00:00-05:00"), Timezone: "America/New_York"}},
		{"removing what is not there", []string{"STATUS:needs-action"}, UpdateTaskInput{
			Description: cleared[string](), Due: change(""), Start: cleared[string](),
			Priority: cleared[int](), PercentComplete: cleared[int](),
		}},
		{"status needs-action without STATUS", []string{}, UpdateTaskInput{Status: strPtr("needs-action")}},
		{"lower-case STATUS", []string{"STATUS:in-process"}, UpdateTaskInput{Status: strPtr("in-process")}},
		{"priority null on PRIORITY:0", []string{"PRIORITY:0"}, UpdateTaskInput{Priority: cleared[int]()}},
		{"priority 0 on PRIORITY:0", []string{"PRIORITY:0"}, UpdateTaskInput{Priority: change(0)}},
		{"priority 0 without PRIORITY", []string{}, UpdateTaskInput{Priority: change(0)}},
		{"padded integers", []string{"PRIORITY: 3", "PERCENT-COMPLETE:070"}, UpdateTaskInput{Priority: change(3), PercentComplete: change(70)}},
		{"all-day dates as stored", []string{"DTSTART;VALUE=DATE:20261104", "DUE;VALUE=DATE:20261105"}, UpdateTaskInput{
			Start: change("2026-11-04"), Due: change("2026-11-05"), DueAllDay: boolPtr(true),
		}},
		{"due_all_day as stored, no times", []string{}, UpdateTaskInput{DueAllDay: boolPtr(true)}},
		{"completed again", []string{"STATUS:COMPLETED", "COMPLETED:20261101T100000Z", "PERCENT-COMPLETE:40"},
			UpdateTaskInput{Status: strPtr("completed"), PercentComplete: change(90)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			obj := storedTask(t, "passport", 7, clientTaskData)
			if tc.props != nil {
				obj = simpleTask(t, "passport", tc.props...)
			}
			before := *obj
			task, repo, err := runUpdate(t, obj, tc.in)
			require.NoError(t, err)
			assert.Empty(t, repo.updated, "nothing written")
			assert.Same(t, obj, task.Object)
			assert.Equal(t, before, *obj, "same data, same ETag")
		})
	}
}

// TestUpdateTask_TextRoundTrip: the API's text is plain (RFC 5545 escaping
// undone). Sending back what a read returned writes nothing, a changed text is
// escaped exactly once, and saving again and again never adds a backslash —
// the form-save loop of the web UI.
func TestUpdateTask_TextRoundTrip(t *testing.T) {
	const (
		summary     = `Buy milk, eggs; bread \ more`
		description = "Line one\nLine two, with C:\\path; done"
		storedDesc  = `DESCRIPTION:Line one\nLine two\, with C:\\path\; done`
	)
	created, data := mustCreate(t, CreateTaskInput{Calendar: taskListCalendar(), Summary: summary, Description: description})
	obj := created.Object
	require.Equal(t, `SUMMARY:Buy milk\, eggs\; bread \\ more`, propLine(t, data, ical.PropSummary))
	require.Equal(t, storedDesc, propLine(t, data, ical.PropDescription))
	require.Equal(t, summary, obj.Summary, "the API reads the plain text back")
	require.Equal(t, description, obj.Description)

	before := *obj
	task, repo, err := runUpdate(t, obj, UpdateTaskInput{Summary: strPtr(obj.Summary), Description: change(obj.Description)})
	require.NoError(t, err)
	assert.Empty(t, repo.updated, "the text as read, sent back: nothing written")
	assert.Equal(t, before, *task.Object, "same data, same ETag")

	for round := 1; round <= 3; round++ {
		edited := fmt.Sprintf("%s, round %d", summary, round)
		task, data := mustUpdate(t, obj, UpdateTaskInput{Summary: strPtr(edited), Description: change(obj.Description)})
		assert.Equal(t, fmt.Sprintf(`SUMMARY:Buy milk\, eggs\; bread \\ more\, round %d`, round), propLine(t, data, ical.PropSummary),
			"round %d: escaped exactly once", round)
		assert.Equal(t, storedDesc, propLine(t, data, ical.PropDescription), "round %d: the unchanged description is not rewritten", round)
		assert.Equal(t, edited, task.Object.Summary)
		assert.Equal(t, description, task.Object.Description)

		// The web form saves what it just got back: nothing changes.
		again := *task.Object
		_, repo, err := runUpdate(t, task.Object, UpdateTaskInput{Summary: strPtr(task.Object.Summary), Description: change(task.Object.Description)})
		require.NoError(t, err)
		assert.Empty(t, repo.updated, "round %d: saving the returned text writes nothing", round)
		assert.Equal(t, again, *task.Object)
		obj = task.Object
	}

	t.Run("plain-text comparison", func(t *testing.T) {
		for _, tc := range []struct {
			name, stored string
			in           UpdateTaskInput
		}{
			{"a client's unescaped comma is the same text", "SUMMARY:Buy milk, eggs", UpdateTaskInput{Summary: strPtr("Buy milk, eggs")}},
			{"a CRLF is the stored \\n", `DESCRIPTION:Line one\nLine two`, UpdateTaskInput{Description: change("Line one\r\nLine two")}},
			{"an upper-case \\N is a newline", `DESCRIPTION:Line one\NLine two`, UpdateTaskInput{Description: change("Line one\nLine two")}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				lines := []string{"BEGIN:VCALENDAR", "PRODID:-//test//EN", "VERSION:2.0", "BEGIN:VTODO",
					"DTSTAMP:20260102T080000Z", "UID:text@example.com", tc.stored}
				if !strings.HasPrefix(tc.stored, "SUMMARY") {
					lines = append(lines, "SUMMARY:Notes")
				}
				obj := storedTask(t, "text", 7, vcal(append(lines, "END:VTODO", "END:VCALENDAR")...))
				before := *obj
				task, repo, err := runUpdate(t, obj, tc.in)
				require.NoError(t, err)
				assert.Empty(t, repo.updated, "nothing written")
				assert.Equal(t, before, *task.Object)
			})
		}
	})
}

// recurringClientData is a repeating VTODO as a phone stores it: a master
// with RRULE, RDATE, EXDATE, an alarm, a parent link and X- properties, and an
// override (RECURRENCE-ID) in front of it, plus a VTIMEZONE nothing
// references. Canonical go-ical order.
var recurringClientData = vcal(
	"BEGIN:VCALENDAR",
	"PRODID:-//Apple Inc.//Reminders//EN",
	"VERSION:2.0",
	"BEGIN:VTIMEZONE",
	"TZID:Europe/Berlin",
	"BEGIN:DAYLIGHT",
	"DTSTART:19700329T020000",
	"RRULE:FREQ=YEARLY;BYMONTH=3;BYDAY=-1SU",
	"TZNAME:CEST",
	"TZOFFSETFROM:+0100",
	"TZOFFSETTO:+0200",
	"END:DAYLIGHT",
	"BEGIN:STANDARD",
	"DTSTART:19701025T030000",
	"RRULE:FREQ=YEARLY;BYMONTH=10;BYDAY=-1SU",
	"TZNAME:CET",
	"TZOFFSETFROM:+0200",
	"TZOFFSETTO:+0100",
	"END:STANDARD",
	"END:VTIMEZONE",
	"BEGIN:VTIMEZONE",
	"TZID:Asia/Tokyo",
	"BEGIN:STANDARD",
	"DTSTART:19700101T000000",
	"TZNAME:JST",
	"TZOFFSETFROM:+0900",
	"TZOFFSETTO:+0900",
	"END:STANDARD",
	"END:VTIMEZONE",
	"BEGIN:VTODO",
	"DTSTAMP:20260103T080000Z",
	"DUE;TZID=Europe/Berlin:20261113T170000",
	"RECURRENCE-ID;TZID=Europe/Berlin:20261112T090000",
	"STATUS:COMPLETED",
	"SUMMARY:Water the plants (moved)",
	"UID:plants@example.com",
	"END:VTODO",
	"BEGIN:VTODO",
	"CREATED:20260101T080000Z",
	"DTSTAMP:20260102T080000Z",
	"DTSTART;TZID=Europe/Berlin:20261105T090000",
	"DUE;TZID=Europe/Berlin:20261105T170000",
	"EXDATE;TZID=Europe/Berlin:20261119T090000",
	"LAST-MODIFIED:20260102T080000Z",
	"PRIORITY:9",
	"RDATE;TZID=Europe/Berlin:20261127T090000",
	"RELATED-TO;RELTYPE=PARENT:parent-uid@example.com",
	"RRULE:FREQ=WEEKLY;COUNT=10",
	"SEQUENCE:2",
	"SUMMARY:Water the plants",
	"UID:plants@example.com",
	"X-APPLE-SORT-ORDER:7",
	"X-FOO;X-PARAM=1:bar",
	"BEGIN:VALARM",
	"ACTION:DISPLAY",
	"DESCRIPTION:Reminder",
	"TRIGGER:-PT15M",
	"END:VALARM",
	"END:VTODO",
	"END:VCALENDAR",
)

// overrideBlock is the override of recurringClientData, which no edit of the
// master may touch.
const overrideBlock = "BEGIN:VTODO\r\nDTSTAMP:20260103T080000Z\r\nDUE;TZID=Europe/Berlin:20261113T170000\r\n" +
	"RECURRENCE-ID;TZID=Europe/Berlin:20261112T090000\r\nSTATUS:COMPLETED\r\nSUMMARY:Water the plants (moved)\r\n" +
	"UID:plants@example.com\r\nEND:VTODO\r\n"

// TestUpdateTask_PreservesClientData (spec §5.1 preservation test, Goal 5):
// editing title, notes, priority and due of a client's repeating task changes
// exactly those lines of the master (plus the bookkeeping); VALARM,
// RELATED-TO, RRULE, RDATE, EXDATE, X- properties, the override and both
// VTIMEZONEs (one unreferenced) stay byte for byte.
func TestUpdateTask_PreservesClientData(t *testing.T) {
	require.Equal(t, recurringClientData, reencode(t, recurringClientData), "the fixture is in go-ical's canonical form")
	obj := storedTask(t, "plants", 7, recurringClientData)
	require.True(t, obj.TaskDetails().Recurring)

	task, data := mustUpdate(t, obj, UpdateTaskInput{
		Summary:     strPtr("Water the ferns"),
		Description: change("Use rain water"),
		Priority:    change(5),
		Due:         change("2026-11-05T16:30:00Z"),
		Timezone:    "Europe/Berlin",
	})
	want := replaceOnce(t, recurringClientData,
		"CREATED:20260101T080000Z\r\n", "CREATED:20260101T080000Z\r\nDESCRIPTION:Use rain water\r\n",
		"DTSTAMP:20260102T080000Z\r\n", "DTSTAMP:"+completeStamp+"\r\n",
		"DUE;TZID=Europe/Berlin:20261105T170000\r\n", "DUE;TZID=Europe/Berlin:20261105T173000\r\n",
		"LAST-MODIFIED:20260102T080000Z\r\n", "LAST-MODIFIED:"+completeStamp+"\r\n",
		"PRIORITY:9\r\n", "PRIORITY:5\r\n",
		"SEQUENCE:2\r\n", "SEQUENCE:3\r\n",
		"SUMMARY:Water the plants\r\n", "SUMMARY:Water the ferns\r\n",
	)
	assert.Equal(t, want, data)
	for _, kept := range []string{
		"BEGIN:VALARM\r\nACTION:DISPLAY\r\nDESCRIPTION:Reminder\r\nTRIGGER:-PT15M\r\nEND:VALARM\r\n",
		"\r\nRELATED-TO;RELTYPE=PARENT:parent-uid@example.com\r\n",
		"\r\nRRULE:FREQ=WEEKLY;COUNT=10\r\n",
		"\r\nRDATE;TZID=Europe/Berlin:20261127T090000\r\n",
		"\r\nEXDATE;TZID=Europe/Berlin:20261119T090000\r\n",
		"\r\nX-FOO;X-PARAM=1:bar\r\n",
		"\r\nX-APPLE-SORT-ORDER:7\r\n",
		"\r\nDTSTART;TZID=Europe/Berlin:20261105T090000\r\n",
		overrideBlock,
		"BEGIN:VTIMEZONE\r\nTZID:Asia/Tokyo\r\n",
		"BEGIN:VTIMEZONE\r\nTZID:Europe/Berlin\r\n",
	} {
		assert.Contains(t, data, kept, "byte-present after the edit")
	}
	assert.True(t, task.Details.Recurring)
	assert.Equal(t, "FREQ=WEEKLY;COUNT=10", task.Details.RRule)
	assert.Equal(t, "Water the ferns", task.Object.Summary, "the columns come from the master")
	assert.Equal(t, calendar.TaskStatusNeedsAction, task.Object.Status)
	assert.Equal(t, time.Date(2026, 11, 5, 16, 30, 0, 0, time.UTC), *task.Object.DueAt)
}

// TestUpdateTask_TZIDAddsVTimezone: a due or start written with a TZID gets a
// VTIMEZONE (calendar.GenerateVTimezone) when the object has none for it,
// after the ones it has; the object's own VTIMEZONEs, the unreferenced one
// included, stay as they are and are not duplicated. UTC adds none.
func TestUpdateTask_TZIDAddsVTimezone(t *testing.T) {
	t.Run("object without VTIMEZONE", func(t *testing.T) {
		_, data := mustUpdate(t, simpleTask(t, "t1"), UpdateTaskInput{Due: change("2026-11-05T22:00:00Z"), Timezone: "America/New_York"})
		assert.Equal(t, "DUE;TZID=America/New_York:20261105T170000", propLine(t, data, "DUE"))
		assert.Equal(t, 1, strings.Count(data, "BEGIN:VTIMEZONE"))
		assert.Less(t, strings.Index(data, "TZID:America/New_York"), strings.Index(data, "BEGIN:VTODO"), "VTIMEZONE first")
		cal, err := ical.NewDecoder(strings.NewReader(data)).Decode()
		require.NoError(t, err)
		require.Equal(t, ical.CompTimezone, cal.Children[0].Name)
		loc, _ := time.LoadLocation("America/New_York")
		generated := ical.NewCalendar()
		generated.Props.SetText(ical.PropProductID, "-//x//EN")
		generated.Props.SetText(ical.PropVersion, "2.0")
		generated.Children = append(generated.Children, calendar.GenerateVTimezone("America/New_York", loc, time.Date(2026, 11, 5, 22, 0, 0, 0, time.UTC)))
		var sb strings.Builder
		require.NoError(t, ical.NewEncoder(&sb).Encode(generated))
		_, block, ok := strings.Cut(sb.String(), "VERSION:2.0\r\n")
		require.True(t, ok)
		block = strings.TrimSuffix(block, "END:VCALENDAR\r\n")
		assert.Contains(t, data, block, "the generated VTIMEZONE")
	})

	t.Run("object with VTIMEZONEs of its own", func(t *testing.T) {
		_, data := mustUpdate(t, storedTask(t, "passport", 7, clientTaskData), UpdateTaskInput{
			Start: change("2026-11-04T14:00:00Z"), Timezone: "America/New_York"})
		assert.Equal(t, "DTSTART;TZID=America/New_York:20261104T090000", propLine(t, data, "DTSTART"))
		berlin := clientTaskData[strings.Index(clientTaskData, "BEGIN:VTIMEZONE"):strings.Index(clientTaskData, "BEGIN:VTODO")]
		assert.Contains(t, data, berlin, "Berlin and the unreferenced Tokyo block unchanged")
		assert.Less(t, strings.Index(data, "TZID:Asia/Tokyo"), strings.Index(data, "TZID:America/New_York"))
		assert.Less(t, strings.Index(data, "TZID:America/New_York"), strings.Index(data, "BEGIN:VTODO"))
		assert.Equal(t, 3, strings.Count(data, "BEGIN:VTIMEZONE"))
	})

	t.Run("a zone the object defines is not added again", func(t *testing.T) {
		_, data := mustUpdate(t, storedTask(t, "passport", 7, clientTaskData), UpdateTaskInput{
			Due: change("2026-12-01T10:00:00Z"), Timezone: "Europe/Berlin"})
		assert.Equal(t, "DUE;TZID=Europe/Berlin:20261201T110000", propLine(t, data, "DUE"))
		assert.Equal(t, 2, strings.Count(data, "BEGIN:VTIMEZONE"))
		assert.Equal(t, 1, strings.Count(data, "TZID:Europe/Berlin\r\n"))
	})

	t.Run("UTC and unusable zones", func(t *testing.T) {
		for _, zone := range []string{"UTC", "", "Local", "Mars/Olympus"} {
			_, data := mustUpdate(t, simpleTask(t, "t1"), UpdateTaskInput{Due: change("2026-11-05T22:00:00+01:00"), Timezone: zone})
			assert.Equal(t, "DUE:20261105T210000Z", propLine(t, data, "DUE"), zone)
			assert.NotContains(t, data, "VTIMEZONE", zone)
		}
	})
}

// TestUpdateTask_TimeValueTypes: due_all_day decides the type of the due and
// start sent (as for create; absent, the task's current due_all_day), and the
// type of DUE and DTSTART must match afterwards (RFC 5545 §3.8.2.3).
func TestUpdateTask_TimeValueTypes(t *testing.T) {
	timed := []string{"DTSTART:20261104T080000Z", "DUE:20261105T080000Z"}
	for _, tc := range []struct {
		name            string
		props           []string
		in              UpdateTaskInput
		due, start      string // expected lines, "" = none
		allDay          bool
		dueAt, startUTC string
	}{
		{"timed to all-day, start removed", timed,
			UpdateTaskInput{DueAllDay: boolPtr(true), Due: change("2026-11-20"), Start: cleared[string]()},
			"DUE;VALUE=DATE:20261120", "", true, "2026-11-20T00:00:00Z", ""},
		{"timed to all-day, both sent", timed,
			UpdateTaskInput{DueAllDay: boolPtr(true), Due: change("2026-11-20"), Start: change("2026-11-18"), Timezone: "Europe/Berlin"},
			"DUE;VALUE=DATE:20261120", "DTSTART;VALUE=DATE:20261118", true, "2026-11-20T00:00:00Z", "2026-11-18T00:00:00Z"},
		{"all-day to timed", []string{"DUE;VALUE=DATE:20261105"},
			UpdateTaskInput{DueAllDay: boolPtr(false), Due: change("2026-11-20T09:00:00Z")},
			"DUE:20261120T090000Z", "", false, "2026-11-20T09:00:00Z", ""},
		{"all-day to timed at the same instant", []string{"DTSTART;VALUE=DATE:20261104", "DUE;VALUE=DATE:20261105"},
			UpdateTaskInput{DueAllDay: boolPtr(false), Due: change("2026-11-05T00:00:00Z"), Start: change("2026-11-04T00:00:00Z")},
			"DUE:20261105T000000Z", "DTSTART:20261104T000000Z", false, "2026-11-05T00:00:00Z", "2026-11-04T00:00:00Z"},
		{"timed to all-day at the same instant", []string{"DTSTART:20261104T000000Z", "DUE:20261105T000000Z"},
			UpdateTaskInput{DueAllDay: boolPtr(true), Due: change("2026-11-05"), Start: change("2026-11-04")},
			"DUE;VALUE=DATE:20261105", "DTSTART;VALUE=DATE:20261104", true, "2026-11-05T00:00:00Z", "2026-11-04T00:00:00Z"},
		{"all-day task: a date without due_all_day", []string{"DUE;VALUE=DATE:20261105"},
			UpdateTaskInput{Due: change("2026-11-07"), Start: change("2026-11-06")},
			"DUE;VALUE=DATE:20261107", "DTSTART;VALUE=DATE:20261106", true, "2026-11-07T00:00:00Z", "2026-11-06T00:00:00Z"},
		{"start only task: the start's type", []string{"DTSTART;VALUE=DATE:20261104"},
			UpdateTaskInput{Due: change("2026-11-08")},
			"DUE;VALUE=DATE:20261108", "DTSTART;VALUE=DATE:20261104", true, "2026-11-08T00:00:00Z", "2026-11-04T00:00:00Z"},
		{"both removed with due_all_day", timed,
			UpdateTaskInput{DueAllDay: boolPtr(true), Due: cleared[string](), Start: cleared[string]()},
			"", "", false, "", ""},
		{"due removed from a stored mismatch", []string{"DTSTART:20261104T080000Z", "DUE;VALUE=DATE:20261105"},
			UpdateTaskInput{Due: cleared[string]()},
			"", "DTSTART:20261104T080000Z", false, "", "2026-11-04T08:00:00Z"},
		{"new start on a duration task: due moves with it", []string{"DTSTART:20261104T080000Z", "DURATION:PT2H"},
			UpdateTaskInput{Start: change("2026-11-06T08:00:00Z")},
			"", "DTSTART:20261106T080000Z", false, "2026-11-06T10:00:00Z", "2026-11-06T08:00:00Z"},
		{"new due on a duration task replaces the duration", []string{"DTSTART:20261104T080000Z", "DURATION:PT2H"},
			UpdateTaskInput{Due: change("2026-11-04T12:00:00Z")},
			"DUE:20261104T120000Z", "DTSTART:20261104T080000Z", false, "2026-11-04T12:00:00Z", "2026-11-04T08:00:00Z"},
		{"start removed from a duration task with a new due", []string{"DTSTART:20261104T080000Z", "DURATION:PT2H"},
			UpdateTaskInput{Start: cleared[string](), Due: change("2026-11-04T10:00:00Z")},
			"DUE:20261104T100000Z", "", false, "2026-11-04T10:00:00Z", ""},
		{"due equal to start", timed,
			UpdateTaskInput{Due: change("2026-11-04T09:00:00+01:00")},
			"DUE:20261104T080000Z", "DTSTART:20261104T080000Z", false, "2026-11-04T08:00:00Z", "2026-11-04T08:00:00Z"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task, data := mustUpdate(t, simpleTask(t, "t1", tc.props...), tc.in)
			assert.Equal(t, tc.due, propLine(t, data, "DUE"))
			assert.Equal(t, tc.start, propLine(t, data, "DTSTART"))
			if tc.in.Due.Set {
				assert.Empty(t, propLines(t, data, "DURATION"))
			}
			assert.Equal(t, tc.allDay, task.Object.IsAllDay)
			if tc.dueAt == "" {
				assert.Nil(t, task.Object.DueAt)
			} else {
				require.NotNil(t, task.Object.DueAt)
				assert.Equal(t, tc.dueAt, task.Object.DueAt.Format(time.RFC3339))
			}
			if tc.startUTC == "" {
				assert.Nil(t, task.Details.Start)
			} else {
				require.NotNil(t, task.Details.Start)
				assert.Equal(t, tc.startUTC, task.Details.Start.Format(time.RFC3339))
			}
		})
	}
}

// TestUpdateTask_StatusLikeCompleteAndUncomplete: on a task that does not
// repeat, status completed writes exactly what complete writes, and a
// non-completed status on a completed task what uncomplete writes (then that
// STATUS); both are no-ops where complete and uncomplete are.
func TestUpdateTask_StatusLikeCompleteAndUncomplete(t *testing.T) {
	strip := func(obj calendar.CalendarObject) calendar.CalendarObject { obj.ETag = ""; return obj }

	open := map[string]func() *calendar.CalendarObject{
		"client task in process": func() *calendar.CalendarObject { return storedTask(t, "passport", 7, clientTaskData) },
		"no STATUS":              func() *calendar.CalendarObject { return simpleTask(t, "t1", "PERCENT-COMPLETE:30") },
		"cancelled":              func() *calendar.CalendarObject { return simpleTask(t, "t1", "STATUS:CANCELLED") },
		"stale COMPLETED": func() *calendar.CalendarObject {
			return simpleTask(t, "t1", "STATUS:NEEDS-ACTION", "COMPLETED:20200101T000000Z")
		},
	}
	for name, mk := range open {
		t.Run("completed like complete: "+name, func(t *testing.T) {
			viaComplete := repoWith(mk())
			_, err := completeUC(viaComplete).Execute(context.Background(), CompleteTaskInput{Calendar: taskListCalendar(), UUID: mk().UUID})
			require.NoError(t, err)
			_, viaUpdate, err := runUpdate(t, mk(), UpdateTaskInput{Status: strPtr("completed"), PercentComplete: change(30)})
			require.NoError(t, err)
			require.Len(t, viaComplete.updated, 1)
			require.Len(t, viaUpdate.updated, 1)
			assert.Equal(t, strip(viaComplete.updated[0]), strip(viaUpdate.updated[0]), "a percent_complete next to completed is ignored")
			assert.Contains(t, viaUpdate.updated[0].ICalData, "\r\nCOMPLETED:"+completeStamp+"\r\n")
			assert.Contains(t, viaUpdate.updated[0].ICalData, "\r\nPERCENT-COMPLETE:100\r\n")
		})
	}

	completed := map[string]func() *calendar.CalendarObject{
		"completed at 100": func() *calendar.CalendarObject {
			return simpleTask(t, "t1", "STATUS:COMPLETED", "COMPLETED:20261101T100000Z", "PERCENT-COMPLETE:100")
		},
		"COMPLETED without STATUS": func() *calendar.CalendarObject { return simpleTask(t, "t1", "COMPLETED:20261101T100000Z") },
		"completed at 40": func() *calendar.CalendarObject {
			return simpleTask(t, "t1", "STATUS:COMPLETED", "COMPLETED:20261101T100000Z", "PERCENT-COMPLETE:40")
		},
	}
	for name, mk := range completed {
		t.Run("needs-action like uncomplete: "+name, func(t *testing.T) {
			viaUncomplete := repoWith(mk())
			_, err := uncompleteUC(viaUncomplete).Execute(context.Background(), CompleteTaskInput{Calendar: taskListCalendar(), UUID: "t1"})
			require.NoError(t, err)
			_, viaUpdate, err := runUpdate(t, mk(), UpdateTaskInput{Status: strPtr("needs-action")})
			require.NoError(t, err)
			require.Len(t, viaUpdate.updated, 1)
			assert.Equal(t, strip(viaUncomplete.updated[0]), strip(viaUpdate.updated[0]))
		})
	}

	for _, tc := range []struct {
		name, status, percent string
		in                    UpdateTaskInput
		want                  []string // VTODO lines after the edit (STATUS, COMPLETED, PERCENT-COMPLETE)
	}{
		{"in-process reopens a completed task", "COMPLETED", "100", UpdateTaskInput{Status: strPtr("in-process")},
			[]string{"STATUS:IN-PROCESS", "", "PERCENT-COMPLETE:0"}},
		{"cancelled reopens and cancels a completed task", "COMPLETED", "100", UpdateTaskInput{Status: strPtr("cancelled")},
			[]string{"STATUS:CANCELLED", "", "PERCENT-COMPLETE:0"}},
		{"cancelled on an open task", "IN-PROCESS", "100", UpdateTaskInput{Status: strPtr("cancelled")},
			[]string{"STATUS:CANCELLED", "", "PERCENT-COMPLETE:100"}},
		{"a percent next to a reopening wins", "COMPLETED", "100", UpdateTaskInput{Status: strPtr("needs-action"), PercentComplete: change(100)},
			[]string{"STATUS:NEEDS-ACTION", "", "PERCENT-COMPLETE:100"}},
		{"percent 100 alone keeps the status", "IN-PROCESS", "30", UpdateTaskInput{PercentComplete: change(100)},
			[]string{"STATUS:IN-PROCESS", "", "PERCENT-COMPLETE:100"}},
		{"empty status is needs-action", "IN-PROCESS", "100", UpdateTaskInput{Status: strPtr("")},
			[]string{"STATUS:NEEDS-ACTION", "", "PERCENT-COMPLETE:100"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			props := []string{"STATUS:" + tc.status, "PERCENT-COMPLETE:" + tc.percent}
			if tc.status == "COMPLETED" {
				props = append(props, "COMPLETED:20261101T100000Z")
			}
			task, data := mustUpdate(t, simpleTask(t, "t1", props...), tc.in)
			assert.Equal(t, tc.want, []string{propLine(t, data, "STATUS"), propLine(t, data, "COMPLETED"), propLine(t, data, "PERCENT-COMPLETE")})
			assert.Nil(t, task.Object.CompletedAt)
		})
	}
}

// TestUpdateTask_RecurringIsRefused (spec §1c, M8): on a repeating task,
// status completed or cancelled and percent_complete 100 are the 409 of
// complete, whatever the task's state and whatever else the request says;
// nothing is written. Every other edit applies to the master.
func TestUpdateTask_RecurringIsRefused(t *testing.T) {
	series := map[string]func() *calendar.CalendarObject{
		"client series with override": func() *calendar.CalendarObject { return storedTask(t, "plants", 7, recurringClientData) },
		"RRULE without DTSTART": func() *calendar.CalendarObject {
			return simpleTask(t, "plants", "DUE:20261105T080000Z", "RRULE:FREQ=DAILY")
		},
		"RDATE only": func() *calendar.CalendarObject {
			return simpleTask(t, "plants", "DTSTART:20261105T080000Z", "RDATE:20261110T080000Z")
		},
		"already completed": func() *calendar.CalendarObject {
			return simpleTask(t, "plants", "DTSTART:20261105T080000Z", "RRULE:FREQ=WEEKLY", "STATUS:COMPLETED", "PERCENT-COMPLETE:100")
		},
	}
	refused := map[string]UpdateTaskInput{
		"status completed":           {Status: strPtr("completed")},
		"status cancelled":           {Status: strPtr("cancelled")},
		"percent 100":                {PercentComplete: change(100)},
		"completed with a new title": {Status: strPtr("completed"), Summary: strPtr("New title")},
		"percent 100 with a due":     {PercentComplete: change(100), Due: change("2026-11-06T08:00:00Z")},
	}
	for sName, mk := range series {
		for rName, in := range refused {
			t.Run(sName+"/"+rName, func(t *testing.T) {
				obj := mk()
				before := *obj
				task, repo, err := runUpdate(t, obj, in)
				require.ErrorIs(t, err, ErrRecurringNotSupported)
				assert.NotErrorIs(t, err, ErrInvalidInput)
				assert.EqualError(t, err, "Repeating tasks cannot be completed here yet. Complete it in your task app.")
				assert.Nil(t, task)
				assert.Empty(t, repo.updated, "nothing written")
				assert.Equal(t, before, *obj)
			})
		}
	}

	for _, tc := range []struct {
		name string
		in   UpdateTaskInput
		line string // a line the master must carry afterwards
	}{
		{"title", UpdateTaskInput{Summary: strPtr("Water the ferns")}, "SUMMARY:Water the ferns"},
		{"notes", UpdateTaskInput{Description: change("Rain water")}, "DESCRIPTION:Rain water"},
		{"priority", UpdateTaskInput{Priority: change(2)}, "PRIORITY:2"},
		{"due", UpdateTaskInput{Due: change("2026-11-05T17:00:00Z")}, "DUE:20261105T170000Z"},
		{"status in-process", UpdateTaskInput{Status: strPtr("in-process")}, "STATUS:IN-PROCESS"},
		{"percent 50", UpdateTaskInput{PercentComplete: change(50)}, "PERCENT-COMPLETE:50"},
	} {
		t.Run("allowed: "+tc.name, func(t *testing.T) {
			_, data := mustUpdate(t, storedTask(t, "plants", 7, recurringClientData), tc.in)
			assert.Contains(t, data, overrideBlock, "the override is untouched")
			_, master, ok := strings.Cut(data, overrideBlock)
			require.True(t, ok)
			assert.Contains(t, master, "\r\n"+tc.line+"\r\n", "written into the master")
			assert.Contains(t, master, "\r\nRRULE:FREQ=WEEKLY;COUNT=10\r\n")
			assert.Contains(t, master, "\r\nSEQUENCE:3\r\n")
		})
	}

	t.Run("allowed: reopening a completed series like uncomplete", func(t *testing.T) {
		props := []string{"DTSTART:20261105T080000Z", "RRULE:FREQ=WEEKLY", "STATUS:COMPLETED", "COMPLETED:20261101T100000Z", "PERCENT-COMPLETE:100"}
		task, data := mustUpdate(t, simpleTask(t, "plants", props...), UpdateTaskInput{Status: strPtr("needs-action")})
		assert.Equal(t, "STATUS:NEEDS-ACTION", propLine(t, data, "STATUS"))
		assert.Empty(t, propLine(t, data, "COMPLETED"))
		assert.Equal(t, "PERCENT-COMPLETE:0", propLine(t, data, "PERCENT-COMPLETE"))
		assert.True(t, task.Details.Recurring)
	})
}

// TestUpdateTask_Validation: every refusal is ErrInvalidInput with its exact
// message (the 400 body), and nothing is written.
func TestUpdateTask_Validation(t *testing.T) {
	timed := []string{"DTSTART:20261104T080000Z", "DUE:20261105T080000Z"}
	allDay := []string{"DUE;VALUE=DATE:20261105"}
	const statusMsg = `status must be "needs-action", "in-process", "completed" or "cancelled"`
	for _, tc := range []struct {
		name  string
		props []string
		in    UpdateTaskInput
		msg   string
	}{
		{"empty summary", nil, UpdateTaskInput{Summary: strPtr("")}, "summary is required"},
		{"blank summary", nil, UpdateTaskInput{Summary: strPtr(" \t ")}, "summary is required"},
		{"zero-width summary", nil, UpdateTaskInput{Summary: strPtr("\u200b\ufeff")}, "summary is required"},
		{"501 runes", nil, UpdateTaskInput{Summary: strPtr(strings.Repeat("ä", 501))}, "summary must be at most 500 characters"},
		{"unknown status", nil, UpdateTaskInput{Status: strPtr("done")}, statusMsg},
		{"upper-case status", nil, UpdateTaskInput{Status: strPtr("COMPLETED")}, statusMsg},
		{"priority -1", nil, UpdateTaskInput{Priority: change(-1)}, "priority must be between 0 and 9"},
		{"priority 10", nil, UpdateTaskInput{Priority: change(10)}, "priority must be between 0 and 9"},
		{"percent 101", nil, UpdateTaskInput{PercentComplete: change(101)}, "percent_complete must be between 0 and 100"},
		{"percent -1", nil, UpdateTaskInput{PercentComplete: change(-1)}, "percent_complete must be between 0 and 100"},
		{"malformed due", timed, UpdateTaskInput{Due: change("tomorrow")}, "due must be an RFC 3339 date-time unless due_all_day is true"},
		{"due without offset", timed, UpdateTaskInput{Due: change("2026-11-05T17:00:00")}, "due must be an RFC 3339 date-time unless due_all_day is true"},
		{"date on a timed task", timed, UpdateTaskInput{Due: change("2026-11-05")}, "due must be an RFC 3339 date-time unless due_all_day is true"},
		{"date-time with due_all_day", timed, UpdateTaskInput{DueAllDay: boolPtr(true), Due: change("2026-11-05T17:00:00Z")},
			"due must be a date (YYYY-MM-DD) when due_all_day is true"},
		{"date-time on an all-day task", allDay, UpdateTaskInput{Start: change("2026-11-04T08:00:00Z")},
			"start must be a date (YYYY-MM-DD) when due_all_day is true"},
		{"due before start, both sent", nil, UpdateTaskInput{Due: change("2026-11-04T08:00:00Z"), Start: change("2026-11-05T08:00:00Z")},
			"due must not be before start"},
		{"new due before the stored start", timed, UpdateTaskInput{Due: change("2026-11-04T07:59:59Z")}, "due must not be before start"},
		{"new start after the stored due", timed, UpdateTaskInput{Start: change("2026-11-05T09:00:01+01:00")}, "due must not be before start"},
		{"new start after the stored due, other offset", timed, UpdateTaskInput{Start: change("2026-11-05T03:30:00-05:00")}, "due must not be before start"},
		{"all-day due before start", allDay, UpdateTaskInput{Start: change("2026-11-06")}, "due must not be before start"},
		{"due_all_day true alone on a timed due", []string{"DUE:20261105T080000Z"}, UpdateTaskInput{DueAllDay: boolPtr(true)},
			"due_all_day is true, but the task's due is a date-time: send due as a date (YYYY-MM-DD) or null"},
		{"due_all_day false alone on an all-day due", allDay, UpdateTaskInput{DueAllDay: boolPtr(false)},
			"due_all_day is false, but the task's due is a date: send due as an RFC 3339 date-time or null"},
		{"all-day due next to a stored timed start", timed, UpdateTaskInput{DueAllDay: boolPtr(true), Due: change("2026-11-06")},
			"due_all_day is true, but the task's start is a date-time: send start as a date (YYYY-MM-DD) or null"},
		{"timed start next to a stored all-day due", allDay, UpdateTaskInput{DueAllDay: boolPtr(false), Start: change("2026-11-04T08:00:00Z")},
			"due_all_day is false, but the task's due is a date: send due as an RFC 3339 date-time or null"},
		{"due_all_day alone, stored start of the other type", []string{"DTSTART;VALUE=DATE:20261104"}, UpdateTaskInput{DueAllDay: boolPtr(false)},
			"due_all_day is false, but the task's start is a date: send start as an RFC 3339 date-time or null"},
		{"new due next to a mismatched stored start", []string{"DTSTART:20261104T080000Z", "DUE;VALUE=DATE:20261105"},
			UpdateTaskInput{Due: change("2026-11-07")},
			"due_all_day is true, but the task's start is a date-time: send start as a date (YYYY-MM-DD) or null"},
		{"start removed from a duration task", []string{"DTSTART:20261104T080000Z", "DURATION:PT2H"}, UpdateTaskInput{Start: cleared[string]()},
			"start cannot be removed while the due date is stored as a duration after it: send due as well"},
		{"calendar first", nil, UpdateTaskInput{Calendar: &calendar.Calendar{ID: 7, SupportedComponents: "VEVENT"},
			Summary: strPtr(""), Priority: change(42)}, "This calendar does not store tasks"},
		{"fields before the recurring refusal", []string{"DUE:20261105T080000Z", "RRULE:FREQ=DAILY"},
			UpdateTaskInput{Status: strPtr("completed"), Summary: strPtr(" ")}, "summary is required"},
		{"times before the recurring refusal", []string{"DUE:20261105T080000Z", "RRULE:FREQ=DAILY"},
			UpdateTaskInput{Status: strPtr("completed"), DueAllDay: boolPtr(true)},
			"due_all_day is true, but the task's due is a date-time: send due as a date (YYYY-MM-DD) or null"},
		{"summary before status", nil, UpdateTaskInput{Status: strPtr("done"), Summary: strPtr("")}, "summary is required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			obj := storedTask(t, "passport", 7, clientTaskData)
			if tc.props != nil {
				obj = simpleTask(t, "passport", tc.props...)
			}
			before := *obj
			task, repo, err := runUpdate(t, obj, tc.in)
			require.ErrorIs(t, err, ErrInvalidInput)
			assert.EqualError(t, err, tc.msg)
			assert.NotErrorIs(t, err, ErrRecurringNotSupported)
			assert.Nil(t, task)
			assert.Empty(t, repo.updated)
			assert.Equal(t, before, *obj)
		})
	}

	t.Run("the calendar refusal keeps ErrComponentNotStored", func(t *testing.T) {
		_, _, err := runUpdate(t, simpleTask(t, "t1"), UpdateTaskInput{Calendar: &calendar.Calendar{ID: 7, SupportedComponents: "VEVENT"},
			Summary: strPtr("x")})
		assert.ErrorIs(t, err, calendar.ErrComponentNotStored)
	})

	for _, set := range []string{"VTODO", "VEVENT,VTODO", ""} {
		t.Run("calendar "+set+" accepted", func(t *testing.T) {
			_, _, err := runUpdate(t, simpleTask(t, "t1"), UpdateTaskInput{Calendar: &calendar.Calendar{ID: 7, SupportedComponents: set},
				Summary: strPtr("x")})
			assert.NoError(t, err)
		})
	}
}

// TestUpdateTask_NotATaskOfThisCalendar: an event's UUID and a task of
// another calendar are ErrNotFound, before the calendar's component set and
// the fields; an unknown UUID passes the repository's error on.
func TestUpdateTask_NotATaskOfThisCalendar(t *testing.T) {
	bad := UpdateTaskInput{Calendar: &calendar.Calendar{ID: 7, SupportedComponents: "VEVENT"}, Summary: strPtr("")}
	for name, obj := range map[string]*calendar.CalendarObject{
		"event":                    stubEvent("passport", 7),
		"task of another calendar": stubTask("passport", 8),
	} {
		t.Run(name, func(t *testing.T) {
			task, repo, err := runUpdate(t, obj, bad)
			require.ErrorIs(t, err, ErrNotFound)
			assert.NotErrorIs(t, err, ErrInvalidInput)
			assert.Nil(t, task)
			assert.Empty(t, repo.updated)
		})
	}

	repo := repoWith()
	_, err := updateUC(repo).Execute(context.Background(), UpdateTaskInput{Calendar: taskListCalendar(), UUID: "missing", Summary: strPtr("x")})
	assert.ErrorIs(t, err, errStubNotFound)
	assert.NotErrorIs(t, err, ErrNotFound)
}

// TestUpdateTask_Failures: storage and decoding failures are passed on as
// untyped errors (a 500), never as one of the typed refusals; nothing is
// stored and the loaded object stays as it was.
func TestUpdateTask_Failures(t *testing.T) {
	typed := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		for _, sentinel := range []error{ErrInvalidInput, ErrNotFound, ErrRecurringNotSupported} {
			assert.NotErrorIs(t, err, sentinel)
		}
	}

	t.Run("update fails", func(t *testing.T) {
		obj := simpleTask(t, "t1")
		repo := repoWith(obj)
		repo.updateErr = errors.New("database is locked")
		_, err := updateUC(repo).Execute(context.Background(), UpdateTaskInput{Calendar: taskListCalendar(), UUID: "t1", Summary: strPtr("x")})
		typed(t, err)
		assert.ErrorIs(t, err, repo.updateErr)
	})

	t.Run("lookup fails", func(t *testing.T) {
		repo := &stubRepo{lookErr: errors.New("disk I/O error")}
		_, err := updateUC(repo).Execute(context.Background(), UpdateTaskInput{Calendar: taskListCalendar(), UUID: "t1", Summary: strPtr("x")})
		typed(t, err)
		assert.ErrorIs(t, err, repo.lookErr)
	})

	t.Run("undecodable data", func(t *testing.T) {
		obj := simpleTask(t, "t1")
		obj.ICalData = "not iCalendar"
		before := *obj
		_, repo, err := runUpdate(t, obj, UpdateTaskInput{Summary: strPtr("x")})
		typed(t, err)
		assert.Empty(t, repo.updated)
		assert.Equal(t, before, *obj)
	})

	t.Run("no calendar", func(t *testing.T) {
		repo := repoWith(simpleTask(t, "t1"))
		_, err := updateUC(repo).Execute(context.Background(), UpdateTaskInput{UUID: "t1", Summary: strPtr("x")})
		typed(t, err)
	})
}

// TestUpdateTask_DefaultClock: without an injected clock the stamps are now,
// in UTC.
func TestUpdateTask_DefaultClock(t *testing.T) {
	repo := repoWith(simpleTask(t, "t1"))
	before := time.Now().UTC().Truncate(time.Second)
	task, err := NewUpdateTaskUseCase(repo, nil).Execute(context.Background(),
		UpdateTaskInput{Calendar: taskListCalendar(), UUID: "t1", Summary: strPtr("x")})
	require.NoError(t, err)
	after := time.Now().UTC()
	line := propLine(t, task.Object.ICalData, "LAST-MODIFIED")
	stamp, err := time.Parse("20060102T150405Z", strings.TrimPrefix(line, "LAST-MODIFIED:"))
	require.NoError(t, err, line)
	assert.False(t, stamp.Before(before) || stamp.After(after), "%s not in [%s, %s]", stamp, before, after)
}
