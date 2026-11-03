package task

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"
	"github.com/google/uuid"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createClock is the injected clock of the create tests: 10:30:15 at +01:00,
// so a timestamp written in the clock's own zone (TZID or a local time) would
// differ from the expected UTC form 20261103T093015Z.
var createClock = time.Date(2026, 11, 3, 10, 30, 15, 0, time.FixedZone("CET", 3600))

const createStamp = "20261103T093015Z"

func taskListCalendar() *calendar.Calendar {
	return &calendar.Calendar{ID: 7, UUID: "cal-uuid", SupportedComponents: "VTODO"}
}

// runCreate executes the create use case with the fixed clock over a stub
// repository whose only working method is CreateCalendarObject.
func runCreate(input CreateTaskInput) (*Task, *stubRepo, error) {
	repo := &stubRepo{}
	uc := NewCreateTaskUseCase(repo, func() time.Time { return createClock })
	t, err := uc.Execute(context.Background(), input)
	return t, repo, err
}

// mustCreate runs a create that must succeed and returns the task and the
// stored object's iCalendar data.
func mustCreate(t *testing.T, input CreateTaskInput) (*Task, string) {
	t.Helper()
	task, repo, err := runCreate(input)
	require.NoError(t, err)
	require.Len(t, repo.created, 1)
	require.Same(t, repo.created[0], task.Object, "the returned task is the stored object")
	return task, task.Object.ICalData
}

// todoLines returns the content lines of the VTODO in data (go-ical folds
// lines only beyond 75 octets; the tests keep values shorter).
func todoLines(t *testing.T, data string) []string {
	t.Helper()
	_, rest, ok := strings.Cut(data, "BEGIN:VTODO\r\n")
	require.True(t, ok, data)
	body, _, ok := strings.Cut(rest, "END:VTODO\r\n")
	require.True(t, ok, data)
	return strings.Split(strings.TrimSuffix(body, "\r\n"), "\r\n")
}

// propLines returns the VTODO lines of property name (with or without
// parameters).
func propLines(t *testing.T, data, name string) []string {
	t.Helper()
	var out []string
	for _, line := range todoLines(t, data) {
		if strings.HasPrefix(line, name+":") || strings.HasPrefix(line, name+";") {
			out = append(out, line)
		}
	}
	return out
}

// propLine returns the one VTODO line of property name, "" when there is none.
func propLine(t *testing.T, data, name string) string {
	t.Helper()
	lines := propLines(t, data, name)
	require.LessOrEqual(t, len(lines), 1, "at most one %s line", name)
	if len(lines) == 0 {
		return ""
	}
	return lines[0]
}

func intPtr(n int) *int { return &n }

// TestCreateTask_Validation is the spec §5.1 validation table: every refusal
// is ErrInvalidInput with the exact 400 message, and nothing is stored.
func TestCreateTask_Validation(t *testing.T) {
	valid := func() CreateTaskInput {
		return CreateTaskInput{Calendar: taskListCalendar(), Summary: "Renew passport"}
	}
	eventCalendar := &calendar.Calendar{ID: 8, SupportedComponents: "VEVENT"}

	for _, tc := range []struct {
		name   string
		mutate func(*CreateTaskInput)
		want   string
	}{
		{"empty summary", func(in *CreateTaskInput) { in.Summary = "" }, "summary is required"},
		{"blank summary", func(in *CreateTaskInput) { in.Summary = " \t\r\n " }, "summary is required"},
		{"501 runes", func(in *CreateTaskInput) { in.Summary = strings.Repeat("ä", 501) },
			"summary must be at most 500 characters"},
		{"due before start", func(in *CreateTaskInput) {
			in.Start, in.Due = "2026-11-05T17:00:00+01:00", "2026-11-05T15:59:59Z"
		}, "due must not be before start"},
		{"due before start in another offset", func(in *CreateTaskInput) {
			in.Start, in.Due = "2026-11-05T17:00:00+01:00", "2026-11-05T17:30:00+02:00"
		}, "due must not be before start"},
		{"all-day due before start", func(in *CreateTaskInput) {
			in.DueAllDay, in.Start, in.Due = true, "2026-11-06", "2026-11-05"
		}, "due must not be before start"},
		{"priority -1", func(in *CreateTaskInput) { in.Priority = intPtr(-1) }, "priority must be between 0 and 9"},
		{"priority 10", func(in *CreateTaskInput) { in.Priority = intPtr(10) }, "priority must be between 0 and 9"},
		{"percent -1", func(in *CreateTaskInput) { in.PercentComplete = intPtr(-1) },
			"percent_complete must be between 0 and 100"},
		{"percent 101", func(in *CreateTaskInput) { in.PercentComplete = intPtr(101) },
			"percent_complete must be between 0 and 100"},
		{"status done", func(in *CreateTaskInput) { in.Status = "done" }, statusMessage},
		{"status open", func(in *CreateTaskInput) { in.Status = "open" }, statusMessage},
		{"status upper case", func(in *CreateTaskInput) { in.Status = "COMPLETED" }, statusMessage},
		{"status mixed case", func(in *CreateTaskInput) { in.Status = "Needs-Action" }, statusMessage},
		{"status with a space", func(in *CreateTaskInput) { in.Status = " needs-action" }, statusMessage},
		{"status with an underscore", func(in *CreateTaskInput) { in.Status = "in_process" }, statusMessage},
		{"all-day with timed due", func(in *CreateTaskInput) {
			in.DueAllDay, in.Due = true, "2026-11-05T17:00:00+01:00"
		}, "due must be a date (YYYY-MM-DD) when due_all_day is true"},
		{"all-day with timed start", func(in *CreateTaskInput) {
			in.DueAllDay, in.Due, in.Start = true, "2026-11-05", "2026-11-04T09:00:00Z"
		}, "start must be a date (YYYY-MM-DD) when due_all_day is true"},
		{"all-day with a malformed date", func(in *CreateTaskInput) { in.DueAllDay, in.Due = true, "05.11.2026" },
			"due must be a date (YYYY-MM-DD) when due_all_day is true"},
		{"timed with a date-only due", func(in *CreateTaskInput) { in.Due = "2026-11-05" },
			"due must be an RFC 3339 date-time unless due_all_day is true"},
		{"timed due with a date-only start", func(in *CreateTaskInput) {
			in.Due, in.Start = "2026-11-05T17:00:00Z", "2026-11-04"
		}, "start must be an RFC 3339 date-time unless due_all_day is true"},
		{"due without an offset", func(in *CreateTaskInput) { in.Due = "2026-11-05T17:00:00" },
			"due must be an RFC 3339 date-time unless due_all_day is true"},
		{"due in words", func(in *CreateTaskInput) { in.Due = "tomorrow" },
			"due must be an RFC 3339 date-time unless due_all_day is true"},
		{"due with month 13", func(in *CreateTaskInput) { in.Due = "2026-13-01T00:00:00Z" },
			"due must be an RFC 3339 date-time unless due_all_day is true"},
		{"calendar without VTODO", func(in *CreateTaskInput) { in.Calendar = eventCalendar },
			"This calendar does not store tasks"},
		// No other correction helps: the calendar is checked first.
		{"calendar without VTODO and a blank summary", func(in *CreateTaskInput) {
			in.Calendar, in.Summary, in.Priority = eventCalendar, "", intPtr(42)
		}, "This calendar does not store tasks"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := valid()
			tc.mutate(&input)
			task, repo, err := runCreate(input)
			require.Error(t, err)
			assert.Nil(t, task)
			assert.ErrorIs(t, err, ErrInvalidInput)
			assert.Equal(t, tc.want, err.Error(), "the 400 body is the bare message")
			assert.Empty(t, repo.created, "nothing is stored")
		})
	}

	t.Run("the component-set refusal keeps the domain error", func(t *testing.T) {
		input := valid()
		input.Calendar = eventCalendar
		_, _, err := runCreate(input)
		assert.ErrorIs(t, err, calendar.ErrComponentNotStored)
		assert.ErrorIs(t, err, calendar.ErrInvalidInput)
	})
}

const statusMessage = `status must be "needs-action", "in-process", "completed" or "cancelled"`

// TestCreateTask_AcceptedCalendars: every component set that holds VTODO
// takes tasks: a task list, a mixed calendar and a legacy "" calendar.
func TestCreateTask_AcceptedCalendars(t *testing.T) {
	for _, set := range []string{"VTODO", "VEVENT,VTODO", ""} {
		t.Run(set, func(t *testing.T) {
			cal := &calendar.Calendar{ID: 9, SupportedComponents: set}
			task, _ := mustCreate(t, CreateTaskInput{Calendar: cal, Summary: "Renew passport"})
			assert.Equal(t, uint(9), task.Object.CalendarID)
		})
	}
}

// TestCreateTask_BuildsACompleteVTODO: the stored object and its iCalendar
// (spec §3.1, §5 rule 1): identity, path, ETag, every server timestamp from
// the injected clock in UTC, SEQUENCE:0, the task's fields, a VTIMEZONE for
// the TZID, and the denormalized task columns.
func TestCreateTask_BuildsACompleteVTODO(t *testing.T) {
	task, data := mustCreate(t, CreateTaskInput{
		Calendar:        taskListCalendar(),
		Summary:         "  Renew passport  ",
		Description:     "Bring two photos",
		Start:           "2026-11-04T09:00:00+01:00",
		Due:             "2026-11-05T17:00:00+01:00",
		Priority:        intPtr(3),
		PercentComplete: intPtr(40),
		Status:          "in-process",
		Timezone:        "Europe/Berlin",
	})
	obj := task.Object

	_, err := uuid.Parse(obj.UUID)
	require.NoError(t, err, "the object UUID is a UUID")
	assert.Equal(t, obj.UUID+"@calcard.io", obj.UID)
	assert.Equal(t, obj.UUID+".ics", obj.Path)
	assert.NotEmpty(t, obj.ETag)
	assert.Equal(t, uint(7), obj.CalendarID)
	assert.Equal(t, calendar.ComponentToDo, obj.ComponentType)

	// Denormalized columns.
	assert.Equal(t, "Renew passport", obj.Summary, "stored trimmed")
	assert.Equal(t, "Bring two photos", obj.Description)
	assert.Equal(t, calendar.TaskStatusInProcess, obj.Status)
	require.NotNil(t, obj.DueAt)
	assert.Equal(t, time.Date(2026, 11, 5, 16, 0, 0, 0, time.UTC), obj.DueAt.UTC())
	assert.Equal(t, intPtr(3), obj.Priority)
	assert.Equal(t, intPtr(40), obj.PercentComplete)
	assert.Nil(t, obj.CompletedAt)
	assert.False(t, obj.IsAllDay)
	require.NotNil(t, task.Details.Start)
	assert.Equal(t, time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC), task.Details.Start.UTC())
	assert.False(t, task.Details.StartAllDay)
	assert.False(t, task.Details.Recurring)

	// The iCalendar object.
	cal, err := ical.NewDecoder(strings.NewReader(data)).Decode()
	require.NoError(t, err, data)
	assert.Equal(t, "-//CalCard//EN", cal.Props.Get(ical.PropProductID).Value)
	assert.Equal(t, "2.0", cal.Props.Get(ical.PropVersion).Value)
	var names []string
	for _, child := range cal.Children {
		names = append(names, child.Name)
	}
	assert.Equal(t, []string{ical.CompTimezone, ical.CompToDo}, names)
	assert.Equal(t, "Europe/Berlin", cal.Children[0].Props.Get(ical.PropTimezoneID).Value)

	lines := todoLines(t, data)
	for _, want := range []string{
		"UID:" + obj.UID,
		"DTSTAMP:" + createStamp,
		"CREATED:" + createStamp,
		"LAST-MODIFIED:" + createStamp,
		"SEQUENCE:0",
		"SUMMARY:Renew passport",
		"DESCRIPTION:Bring two photos",
		"DTSTART;TZID=Europe/Berlin:20261104T090000",
		"DUE;TZID=Europe/Berlin:20261105T170000",
		"PRIORITY:3",
		"PERCENT-COMPLETE:40",
		"STATUS:IN-PROCESS",
	} {
		assert.Contains(t, lines, want)
	}
	assert.Len(t, lines, 12, "nothing else in the VTODO: %q", lines)
	assert.NotContains(t, data, "TZID=Local")
	assert.NotContains(t, data, "TZID=:")
}

// TestCreateTask_DueValueTypes (spec §5 rule 3): an all-day task writes
// VALUE=DATE, a timed one with a timezone TZID=<zone> plus its VTIMEZONE, and
// a timed one without a usable zone UTC "Z" — never an empty TZID or
// TZID=Local (F22). DTSTART follows the same rule.
func TestCreateTask_DueValueTypes(t *testing.T) {
	// An offset that equals the host's own parses into time.Local.
	localDue := time.Date(2026, 11, 5, 17, 0, 0, 0, time.Local)

	for _, tc := range []struct {
		name                 string
		due, start, timezone string
		allDay               bool
		wantDue, wantStart   string
		wantVTimezone        bool
		wantDueAt            *time.Time
	}{
		{name: "all-day", due: "2026-11-05", allDay: true,
			wantDue: "DUE;VALUE=DATE:20261105", wantDueAt: ptrUTC(2026, 11, 5, 0, 0)},
		{name: "all-day with a start, the timezone is ignored", due: "2026-11-05", start: "2026-11-01",
			allDay: true, timezone: "Europe/Berlin",
			wantDue: "DUE;VALUE=DATE:20261105", wantStart: "DTSTART;VALUE=DATE:20261101", wantDueAt: ptrUTC(2026, 11, 5, 0, 0)},
		{name: "all-day due on its start date", due: "2026-11-05", start: "2026-11-05", allDay: true,
			wantDue: "DUE;VALUE=DATE:20261105", wantStart: "DTSTART;VALUE=DATE:20261105", wantDueAt: ptrUTC(2026, 11, 5, 0, 0)},
		{name: "timed with a timezone", due: "2026-11-05T17:00:00+01:00", timezone: "Europe/Berlin",
			wantDue: "DUE;TZID=Europe/Berlin:20261105T170000", wantVTimezone: true, wantDueAt: ptrUTC(2026, 11, 5, 16, 0)},
		{name: "timed UTC instant shown in summer time", due: "2026-07-01T10:00:00Z", timezone: "Europe/Berlin",
			wantDue: "DUE;TZID=Europe/Berlin:20260701T120000", wantVTimezone: true, wantDueAt: ptrUTC(2026, 7, 1, 10, 0)},
		{name: "timed with start and due in a timezone", due: "2026-11-05T17:00:00+01:00",
			start: "2026-11-05T17:00:00+01:00", timezone: "America/New_York",
			wantDue: "DUE;TZID=America/New_York:20261105T110000", wantStart: "DTSTART;TZID=America/New_York:20261105T110000",
			wantVTimezone: true, wantDueAt: ptrUTC(2026, 11, 5, 16, 0)},
		{name: "timed with an offset and no timezone", due: "2026-11-05T17:00:00+05:00",
			wantDue: "DUE:20261105T120000Z", wantDueAt: ptrUTC(2026, 11, 5, 12, 0)},
		{name: "timed in UTC and no timezone", due: "2026-11-05T17:00:00Z",
			wantDue: "DUE:20261105T170000Z", wantDueAt: ptrUTC(2026, 11, 5, 17, 0)},
		{name: "timed with fractional seconds", due: "2026-11-05T17:00:00.750Z",
			wantDue: "DUE:20261105T170000Z", wantDueAt: ptrUTC(2026, 11, 5, 17, 0)},
		{name: "timed in the host's own offset", due: localDue.Format(time.RFC3339),
			wantDue: "DUE:" + localDue.UTC().Format("20060102T150405Z"), wantDueAt: ptr(localDue.UTC())},
		{name: "unknown timezone is ignored", due: "2026-11-05T17:00:00+01:00", timezone: "Mars/Olympus_Mons",
			wantDue: "DUE:20261105T160000Z", wantDueAt: ptrUTC(2026, 11, 5, 16, 0)},
		{name: "timezone Local is ignored", due: "2026-11-05T17:00:00+01:00", timezone: "Local",
			wantDue: "DUE:20261105T160000Z", wantDueAt: ptrUTC(2026, 11, 5, 16, 0)},
		{name: "timezone UTC", due: "2026-11-05T17:00:00+01:00", timezone: "UTC",
			wantDue: "DUE:20261105T160000Z", wantDueAt: ptrUTC(2026, 11, 5, 16, 0)},
		{name: "start only", start: "2026-11-04T09:00:00+01:00", timezone: "Europe/Berlin",
			wantStart: "DTSTART;TZID=Europe/Berlin:20261104T090000", wantVTimezone: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task, data := mustCreate(t, CreateTaskInput{
				Calendar: taskListCalendar(), Summary: "Renew passport",
				Due: tc.due, Start: tc.start, DueAllDay: tc.allDay, Timezone: tc.timezone,
			})
			assert.Equal(t, tc.wantDue, propLine(t, data, ical.PropDue))
			assert.Equal(t, tc.wantStart, propLine(t, data, ical.PropDateTimeStart))
			assert.Equal(t, tc.wantVTimezone, strings.Contains(data, "BEGIN:VTIMEZONE"), data)
			assert.NotContains(t, data, "TZID=Local")
			assert.NotContains(t, data, "TZID=:")
			if tc.wantDueAt == nil {
				assert.Nil(t, task.Object.DueAt)
			} else {
				require.NotNil(t, task.Object.DueAt)
				assert.Equal(t, *tc.wantDueAt, task.Object.DueAt.UTC())
			}
			assert.Equal(t, tc.allDay, task.Object.IsAllDay)
			assert.Equal(t, tc.allDay && tc.start != "", task.Details.StartAllDay)
		})
	}
}

func ptr(t time.Time) *time.Time { return &t }

func ptrUTC(y int, m time.Month, d, h, min int) *time.Time {
	return ptr(time.Date(y, m, d, h, min, 0, 0, time.UTC))
}

// TestCreateTask_Status: the four statuses (default needs-action);
// "completed" also stamps COMPLETED with the injected clock in UTC and
// PERCENT-COMPLETE:100 over any given percentage.
func TestCreateTask_Status(t *testing.T) {
	for _, tc := range []struct {
		name, status, wantStatus string
		percent                  *int
		wantPercent              string
		wantCompleted            bool
	}{
		{"default", "", calendar.TaskStatusNeedsAction, nil, "", false},
		{"needs-action", "needs-action", calendar.TaskStatusNeedsAction, intPtr(10), "PERCENT-COMPLETE:10", false},
		{"in-process", "in-process", calendar.TaskStatusInProcess, nil, "", false},
		{"completed", "completed", calendar.TaskStatusCompleted, nil, "PERCENT-COMPLETE:100", true},
		{"completed over a given percentage", "completed", calendar.TaskStatusCompleted, intPtr(40), "PERCENT-COMPLETE:100", true},
		{"cancelled keeps its percentage", "cancelled", calendar.TaskStatusCancelled, intPtr(40), "PERCENT-COMPLETE:40", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task, data := mustCreate(t, CreateTaskInput{
				Calendar: taskListCalendar(), Summary: "Renew passport", Status: tc.status, PercentComplete: tc.percent,
			})
			assert.Equal(t, "STATUS:"+tc.wantStatus, propLine(t, data, ical.PropStatus))
			assert.Equal(t, tc.wantPercent, propLine(t, data, ical.PropPercentComplete))
			assert.Equal(t, tc.wantStatus, task.Object.Status)
			if tc.wantCompleted {
				assert.Equal(t, "COMPLETED:"+createStamp, propLine(t, data, ical.PropCompleted))
				require.NotNil(t, task.Object.CompletedAt)
				assert.Equal(t, createClock.UTC(), task.Object.CompletedAt.UTC())
				assert.Equal(t, intPtr(100), task.Object.PercentComplete)
			} else {
				assert.Empty(t, propLine(t, data, ical.PropCompleted))
				assert.Nil(t, task.Object.CompletedAt)
			}
		})
	}
}

// TestCreateTask_OptionalFields: a summary alone makes a valid task with no
// dates, priority, percentage or description; priority and percentage are
// written as given (PRIORITY:0 is the RFC's "undefined", which the column
// reads as none); text is escaped and read back.
func TestCreateTask_OptionalFields(t *testing.T) {
	t.Run("summary only", func(t *testing.T) {
		task, data := mustCreate(t, CreateTaskInput{Calendar: taskListCalendar(), Summary: "Renew passport"})
		for _, name := range []string{ical.PropDue, ical.PropDateTimeStart, ical.PropPriority,
			ical.PropPercentComplete, ical.PropDescription, ical.PropCompleted, ical.PropRecurrenceRule} {
			assert.Empty(t, propLines(t, data, name), name)
		}
		assert.Equal(t, "STATUS:NEEDS-ACTION", propLine(t, data, ical.PropStatus))
		assert.NotContains(t, data, "BEGIN:VTIMEZONE")
		obj := task.Object
		assert.Nil(t, obj.DueAt)
		assert.Nil(t, obj.Priority)
		assert.Nil(t, obj.PercentComplete)
		assert.Nil(t, task.Details.Start)
	})

	t.Run("priority and percentage as given", func(t *testing.T) {
		for _, tc := range []struct {
			priority, percent         int
			wantPriorityColumn        *int
			wantPriority, wantPercent string
		}{
			{0, 0, nil, "PRIORITY:0", "PERCENT-COMPLETE:0"},
			{1, 100, intPtr(1), "PRIORITY:1", "PERCENT-COMPLETE:100"},
			{9, 55, intPtr(9), "PRIORITY:9", "PERCENT-COMPLETE:55"},
		} {
			task, data := mustCreate(t, CreateTaskInput{Calendar: taskListCalendar(), Summary: "Renew passport",
				Priority: intPtr(tc.priority), PercentComplete: intPtr(tc.percent)})
			assert.Equal(t, tc.wantPriority, propLine(t, data, ical.PropPriority))
			assert.Equal(t, tc.wantPercent, propLine(t, data, ical.PropPercentComplete))
			assert.Equal(t, tc.wantPriorityColumn, task.Object.Priority)
			assert.Equal(t, intPtr(tc.percent), task.Object.PercentComplete)
			assert.Equal(t, calendar.TaskStatusNeedsAction, task.Object.Status, "a percentage does not change the status")
		}
	})

	t.Run("500 runes after trimming", func(t *testing.T) {
		summary := strings.Repeat("ä", 500)
		task, _ := mustCreate(t, CreateTaskInput{Calendar: taskListCalendar(), Summary: "  " + summary + "\n"})
		assert.Equal(t, summary, task.Object.Summary)
	})

	t.Run("text is escaped", func(t *testing.T) {
		task, data := mustCreate(t, CreateTaskInput{Calendar: taskListCalendar(),
			Summary: "Buy milk, eggs; bread", Description: "Line one\nLine two"})
		assert.Equal(t, `SUMMARY:Buy milk\, eggs\; bread`, propLine(t, data, ical.PropSummary))
		assert.Equal(t, `DESCRIPTION:Line one\nLine two`, propLine(t, data, ical.PropDescription))
		// The columns the API answers from hold the plain text.
		assert.Equal(t, "Buy milk, eggs; bread", task.Object.Summary)
		assert.Equal(t, "Line one\nLine two", task.Object.Description)
		cal, err := ical.NewDecoder(strings.NewReader(task.Object.ICalData)).Decode()
		require.NoError(t, err)
		todo := calendar.TaskMaster(cal)
		summary, err := todo.Props.Text(ical.PropSummary)
		require.NoError(t, err)
		assert.Equal(t, "Buy milk, eggs; bread", summary)
		description, err := todo.Props.Text(ical.PropDescription)
		require.NoError(t, err)
		assert.Equal(t, "Line one\nLine two", description)
	})

	t.Run("a backslash is escaped once and reads back as one", func(t *testing.T) {
		task, data := mustCreate(t, CreateTaskInput{Calendar: taskListCalendar(),
			Summary: `Back up C:\Users`, Description: `a\, b looks escaped but is text`})
		assert.Equal(t, `SUMMARY:Back up C:\\Users`, propLine(t, data, ical.PropSummary))
		assert.Equal(t, `DESCRIPTION:a\\\, b looks escaped but is text`, propLine(t, data, ical.PropDescription))
		assert.Equal(t, `Back up C:\Users`, task.Object.Summary)
		assert.Equal(t, `a\, b looks escaped but is text`, task.Object.Description)
	})

	t.Run("a CRLF line break is stored as \\n, not refused", func(t *testing.T) {
		// go-ical's SetText copies a CR into the value and its encoder then
		// refuses the object: before NewTextProp this was a 500.
		task, data := mustCreate(t, CreateTaskInput{Calendar: taskListCalendar(),
			Summary: "Notes", Description: "Line one\r\nLine two\rLine three"})
		assert.Equal(t, `DESCRIPTION:Line one\nLine two\nLine three`, propLine(t, data, ical.PropDescription))
		assert.Equal(t, "Line one\nLine two\nLine three", task.Object.Description)
	})
}

// TestCreateTask_FreshIdentity: every create gets its own UUID, UID, path and
// ETag.
func TestCreateTask_FreshIdentity(t *testing.T) {
	a, _ := mustCreate(t, CreateTaskInput{Calendar: taskListCalendar(), Summary: "A"})
	b, _ := mustCreate(t, CreateTaskInput{Calendar: taskListCalendar(), Summary: "A"})
	assert.NotEqual(t, a.Object.UUID, b.Object.UUID)
	assert.NotEqual(t, a.Object.UID, b.Object.UID)
	assert.NotEqual(t, a.Object.Path, b.Object.Path)
	assert.NotEqual(t, a.Object.ETag, b.Object.ETag)
}

// TestCreateTask_DefaultClock: without an injected clock the timestamps are
// the current time, still in UTC.
func TestCreateTask_DefaultClock(t *testing.T) {
	repo := &stubRepo{}
	before := time.Now().UTC().Truncate(time.Second)
	task, err := NewCreateTaskUseCase(repo, nil).Execute(context.Background(),
		CreateTaskInput{Calendar: taskListCalendar(), Summary: "Renew passport", Status: "completed"})
	require.NoError(t, err)
	after := time.Now().UTC()

	data := task.Object.ICalData
	for _, name := range []string{ical.PropDateTimeStamp, ical.PropCreated, ical.PropLastModified, ical.PropCompleted} {
		line := propLine(t, data, name)
		value := strings.TrimPrefix(line, name+":")
		require.True(t, strings.HasSuffix(value, "Z"), "%s is UTC: %q", name, line)
		stamp, err := time.Parse("20060102T150405Z", value)
		require.NoError(t, err, line)
		assert.False(t, stamp.Before(before) || stamp.After(after), "%s %v within [%v, %v]", name, stamp, before, after)
	}
}

// TestCreateTask_Failures: a server-side failure is passed on and is not
// ErrInvalidInput (the handler's generic 500).
func TestCreateTask_Failures(t *testing.T) {
	t.Run("no calendar", func(t *testing.T) {
		task, repo, err := runCreate(CreateTaskInput{Summary: "Renew passport"})
		require.Error(t, err)
		assert.Nil(t, task)
		assert.NotErrorIs(t, err, ErrInvalidInput)
		assert.Empty(t, repo.created)
	})

	t.Run("storage failure", func(t *testing.T) {
		storageErr := errors.New("disk I/O error")
		repo := &stubRepo{createErr: storageErr}
		task, err := NewCreateTaskUseCase(repo, nil).Execute(context.Background(),
			CreateTaskInput{Calendar: taskListCalendar(), Summary: "Renew passport"})
		assert.Nil(t, task)
		assert.ErrorIs(t, err, storageErr)
		assert.NotErrorIs(t, err, ErrInvalidInput)
	})
}
