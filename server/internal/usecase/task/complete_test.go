package task

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// completeClock is the injected clock of the complete/uncomplete tests, at
// +01:00 so a timestamp written in the clock's own zone would not read
// completeStamp.
var completeClock = time.Date(2026, 11, 3, 10, 30, 15, 0, time.FixedZone("CET", 3600))

const completeStamp = "20261103T093015Z"

// vcal joins iCalendar lines with CRLF, ending with one.
func vcal(lines ...string) string {
	return strings.Join(lines, "\r\n") + "\r\n"
}

// storedTask is a task object of calendarID as the repository returns it:
// the given iCalendar data, its denormalized columns and an ETag.
func storedTask(t *testing.T, uuid string, calendarID uint, data string) *calendar.CalendarObject {
	t.Helper()
	obj := &calendar.CalendarObject{ID: 41, UUID: uuid, CalendarID: calendarID, Path: uuid + ".ics", ETag: "etag-before", ICalData: data}
	require.NoError(t, obj.PopulateDenormFieldsFromICal())
	require.True(t, obj.IsTask(), "fixture must be a task")
	return obj
}

// simpleTask is a stored task of the task list (taskListCalendar, ID 7) with
// the given VTODO property lines, in canonical (sorted) order or not.
func simpleTask(t *testing.T, uuid string, props ...string) *calendar.CalendarObject {
	t.Helper()
	lines := []string{"BEGIN:VCALENDAR", "PRODID:-//test//EN", "VERSION:2.0", "BEGIN:VTODO",
		"DTSTAMP:20260102T080000Z", "SUMMARY:Task " + uuid, "UID:" + uuid + "@example.com"}
	lines = append(lines, props...)
	lines = append(lines, "END:VTODO", "END:VCALENDAR")
	return storedTask(t, uuid, 7, vcal(lines...))
}

func completeUC(repo *stubRepo) *CompleteTaskUseCase {
	return NewCompleteTaskUseCase(repo, func() time.Time { return completeClock })
}

func uncompleteUC(repo *stubRepo) *UncompleteTaskUseCase {
	return NewUncompleteTaskUseCase(repo, func() time.Time { return completeClock })
}

func repoWith(objs ...*calendar.CalendarObject) *stubRepo {
	repo := &stubRepo{objects: map[string]*calendar.CalendarObject{}}
	for _, o := range objs {
		repo.objects[o.UUID] = o
	}
	return repo
}

// clientTaskData is a VTODO as a phone's task app stores it, with everything
// the server must not touch: a client-defined VTIMEZONE, a second one nothing
// references (complete and uncomplete change no TZID, so they do not
// synchronise the VTIMEZONEs), a VALARM, RELATED-TO, X- properties with
// parameters, a parameter on SUMMARY, an escaped DESCRIPTION, CATEGORIES and
// a calendar-level X- property. It is
// written in go-ical's canonical order (properties sorted by name), so an
// untouched line must come back byte for byte at the same place.
var clientTaskData = vcal(
	"BEGIN:VCALENDAR",
	"PRODID:-//Apple Inc.//Reminders//EN",
	"VERSION:2.0",
	"X-WR-CALNAME:Errands",
	"BEGIN:VTIMEZONE",
	"TZID:Europe/Berlin",
	"X-LIC-LOCATION:Europe/Berlin",
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
	"CATEGORIES:Errands,Home",
	"CREATED:20260101T080000Z",
	`DESCRIPTION:Bring two photos\, and the old one`,
	"DTSTAMP:20260102T080000Z",
	"DUE;TZID=Europe/Berlin:20261105T170000",
	"LAST-MODIFIED:20260102T080000Z",
	"PERCENT-COMPLETE:40",
	"PRIORITY:1",
	"RELATED-TO;RELTYPE=PARENT:parent-uid@example.com",
	"SEQUENCE:3",
	"STATUS:IN-PROCESS",
	"SUMMARY;LANGUAGE=de:Reisepass erneuern",
	"UID:passport@example.com",
	"X-APPLE-SORT-ORDER:12",
	"X-FOO;X-PARAM=1:bar",
	"BEGIN:VALARM",
	"ACTION:DISPLAY",
	"DESCRIPTION:Reminder",
	"TRIGGER:-PT15M",
	"END:VALARM",
	"END:VTODO",
	"END:VCALENDAR",
)

// reencode is what go-ical makes of data without any edit.
func reencode(t *testing.T, data string) string {
	t.Helper()
	cal, err := ical.NewDecoder(strings.NewReader(data)).Decode()
	require.NoError(t, err)
	var sb strings.Builder
	require.NoError(t, ical.NewEncoder(&sb).Encode(cal))
	return sb.String()
}

// TestCompleteTask_EditsInPlace: complete writes STATUS:COMPLETED,
// COMPLETED (the clock, UTC) and PERCENT-COMPLETE:100 into the stored VTODO,
// bumps SEQUENCE and stamps LAST-MODIFIED and DTSTAMP; every other byte of
// the object stays as the client stored it. The object is stored once with a
// new ETag and the re-derived columns.
func TestCompleteTask_EditsInPlace(t *testing.T) {
	require.Equal(t, clientTaskData, reencode(t, clientTaskData), "the fixture is in go-ical's canonical form")

	obj := storedTask(t, "passport", 7, clientTaskData)
	repo := repoWith(obj)

	task, err := completeUC(repo).Execute(context.Background(), CompleteTaskInput{Calendar: taskListCalendar(), UUID: "passport"})
	require.NoError(t, err)
	require.Len(t, repo.updated, 1, "stored once")
	stored := repo.updated[0]

	want := clientTaskData
	for old, repl := range map[string]string{
		"CATEGORIES:Errands,Home\r\n":        "CATEGORIES:Errands,Home\r\nCOMPLETED:" + completeStamp + "\r\n",
		"DTSTAMP:20260102T080000Z\r\n":       "DTSTAMP:" + completeStamp + "\r\n",
		"LAST-MODIFIED:20260102T080000Z\r\n": "LAST-MODIFIED:" + completeStamp + "\r\n",
		"PERCENT-COMPLETE:40\r\n":            "PERCENT-COMPLETE:100\r\n",
		"SEQUENCE:3\r\n":                     "SEQUENCE:4\r\n",
		"STATUS:IN-PROCESS\r\n":              "STATUS:COMPLETED\r\n",
	} {
		require.Equal(t, 1, strings.Count(want, old), old)
		want = strings.Replace(want, old, repl, 1)
	}
	assert.Equal(t, want, stored.ICalData, "only the touched properties change")

	// The columns follow the new data; identity stays.
	assert.Equal(t, calendar.TaskStatusCompleted, stored.Status)
	require.NotNil(t, stored.CompletedAt)
	assert.Equal(t, completeClock.UTC(), *stored.CompletedAt)
	assert.Equal(t, time.UTC, stored.CompletedAt.Location())
	assert.Equal(t, intPtr(100), stored.PercentComplete)
	assert.Equal(t, intPtr(1), stored.Priority)
	assert.Equal(t, len(stored.ICalData), stored.ContentLength)
	assert.NotEmpty(t, stored.ETag)
	assert.NotEqual(t, "etag-before", stored.ETag, "a new ETag")
	assert.Equal(t, uint(41), stored.ID)
	assert.Equal(t, "passport", stored.UUID)
	assert.Equal(t, "passport.ics", stored.Path)
	assert.Equal(t, "passport@example.com", stored.UID)
	assert.Equal(t, uint(7), stored.CalendarID)

	// The returned task is the stored object.
	assert.Same(t, obj, task.Object)
	assert.Equal(t, stored.ETag, task.Object.ETag)
	assert.Equal(t, stored.ICalData, task.Object.ICalData)
	assert.False(t, task.Details.Recurring)
}

// TestCompleteTask_FromEveryOpenState: a task that is not completed —
// whatever its status, percentage and leftovers — ends up completed with the
// clock's COMPLETED (replacing a stale one) and PERCENT-COMPLETE:100.
func TestCompleteTask_FromEveryOpenState(t *testing.T) {
	for _, tc := range []struct {
		name  string
		props []string
	}{
		{"no STATUS, no percent, no SEQUENCE", nil},
		{"NEEDS-ACTION at 0%", []string{"PERCENT-COMPLETE:0", "STATUS:NEEDS-ACTION", "SEQUENCE:0"}},
		{"IN-PROCESS at 40%", []string{"PERCENT-COMPLETE:40", "STATUS:IN-PROCESS"}},
		{"CANCELLED", []string{"STATUS:CANCELLED"}},
		{"NEEDS-ACTION with a stale COMPLETED", []string{"COMPLETED:20250101T000000Z", "STATUS:NEEDS-ACTION"}},
		{"lower-case needs-action", []string{"STATUS:needs-action"}},
		{"unknown STATUS", []string{"STATUS:TENTATIVE"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			obj := simpleTask(t, "t1", tc.props...)
			require.NotEqual(t, calendar.TaskStatusCompleted, obj.Status)
			repo := repoWith(obj)

			task, err := completeUC(repo).Execute(context.Background(), CompleteTaskInput{Calendar: taskListCalendar(), UUID: "t1"})
			require.NoError(t, err)
			require.Len(t, repo.updated, 1)
			data := repo.updated[0].ICalData
			assert.Equal(t, "STATUS:COMPLETED", propLine(t, data, "STATUS"))
			assert.Equal(t, "COMPLETED:"+completeStamp, propLine(t, data, "COMPLETED"))
			assert.Equal(t, "PERCENT-COMPLETE:100", propLine(t, data, "PERCENT-COMPLETE"))
			assert.Equal(t, "LAST-MODIFIED:"+completeStamp, propLine(t, data, "LAST-MODIFIED"))
			assert.Equal(t, "DTSTAMP:"+completeStamp, propLine(t, data, "DTSTAMP"))
			assert.Equal(t, calendar.TaskStatusCompleted, task.Object.Status)
			assert.Equal(t, intPtr(100), task.Object.PercentComplete)
		})
	}
}

// TestCompleteTask_SequenceBump: SEQUENCE goes up by one; absent, malformed
// or negative counts as 0 (RFC 5545 default).
func TestCompleteTask_SequenceBump(t *testing.T) {
	for _, tc := range []struct {
		name, prop, want string
	}{
		{"absent", "", "SEQUENCE:1"},
		{"zero", "SEQUENCE:0", "SEQUENCE:1"},
		{"seven", "SEQUENCE:7", "SEQUENCE:8"},
		{"padded", "SEQUENCE: 2 ", "SEQUENCE:3"},
		{"malformed", "SEQUENCE:two", "SEQUENCE:1"},
		{"negative", "SEQUENCE:-4", "SEQUENCE:1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var props []string
			if tc.prop != "" {
				props = append(props, tc.prop)
			}
			repo := repoWith(simpleTask(t, "t1", props...))
			_, err := completeUC(repo).Execute(context.Background(), CompleteTaskInput{Calendar: taskListCalendar(), UUID: "t1"})
			require.NoError(t, err)
			require.Len(t, repo.updated, 1)
			assert.Equal(t, tc.want, propLine(t, repo.updated[0].ICalData, "SEQUENCE"))
		})
	}
}

// TestCompleteTask_AlreadyCompletedIsANoOp: completing a completed task
// answers the task as it is: nothing stored, same ETag, same data — also when
// the client completed it differently (COMPLETED without STATUS, no
// timestamp, a percentage below 100).
func TestCompleteTask_AlreadyCompletedIsANoOp(t *testing.T) {
	for _, tc := range []struct {
		name  string
		props []string
	}{
		{"completed by complete", []string{"COMPLETED:20261101T100000Z", "PERCENT-COMPLETE:100", "STATUS:COMPLETED"}},
		{"COMPLETED without STATUS", []string{"COMPLETED:20261101T100000Z"}},
		{"STATUS without COMPLETED", []string{"STATUS:COMPLETED"}},
		{"completed at 40%", []string{"PERCENT-COMPLETE:40", "STATUS:COMPLETED"}},
		{"lower-case completed", []string{"STATUS:completed"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			obj := simpleTask(t, "t1", tc.props...)
			require.Equal(t, calendar.TaskStatusCompleted, obj.Status)
			before := *obj
			repo := repoWith(obj)

			task, err := completeUC(repo).Execute(context.Background(), CompleteTaskInput{Calendar: taskListCalendar(), UUID: "t1"})
			require.NoError(t, err)
			assert.Empty(t, repo.updated, "nothing written")
			assert.Same(t, obj, task.Object)
			assert.Equal(t, before, *task.Object, "the object is unchanged")
			assert.Equal(t, "etag-before", task.Object.ETag)
		})
	}

	t.Run("twice in a row", func(t *testing.T) {
		repo := repoWith(simpleTask(t, "t1"))
		uc := completeUC(repo)
		first, err := uc.Execute(context.Background(), CompleteTaskInput{Calendar: taskListCalendar(), UUID: "t1"})
		require.NoError(t, err)
		etag, data := first.Object.ETag, first.Object.ICalData

		second, err := uc.Execute(context.Background(), CompleteTaskInput{Calendar: taskListCalendar(), UUID: "t1"})
		require.NoError(t, err)
		assert.Len(t, repo.updated, 1, "the second complete writes nothing")
		assert.Equal(t, etag, second.Object.ETag)
		assert.Equal(t, data, second.Object.ICalData)
	})
}

// TestCompleteTask_RecurringIsRefused (spec §1c): a task with an RRULE or an
// RDATE on its master is ErrRecurringNotSupported with the exact sentence,
// also when it is already completed, and nothing is written.
func TestCompleteTask_RecurringIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name  string
		props []string
	}{
		{"RRULE", []string{"DUE:20261105T090000Z", "DTSTART:20261105T080000Z", "RRULE:FREQ=WEEKLY"}},
		{"RRULE without DTSTART (Tasks.org)", []string{"DUE:20261105T090000Z", "RRULE:FREQ=DAILY"}},
		{"RDATE", []string{"DTSTART:20261105T080000Z", "RDATE:20261112T080000Z"}},
		{"unparseable RRULE", []string{"RRULE:bogus"}},
		{"already completed", []string{"COMPLETED:20261101T100000Z", "RRULE:FREQ=WEEKLY", "STATUS:COMPLETED"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			obj := simpleTask(t, "t1", tc.props...)
			before := *obj
			repo := repoWith(obj)

			task, err := completeUC(repo).Execute(context.Background(), CompleteTaskInput{Calendar: taskListCalendar(), UUID: "t1"})
			require.Error(t, err)
			assert.Nil(t, task)
			assert.ErrorIs(t, err, ErrRecurringNotSupported)
			assert.NotErrorIs(t, err, ErrInvalidInput)
			assert.Equal(t, "Repeating tasks cannot be completed here yet. Complete it in your task app.", err.Error())
			assert.Empty(t, repo.updated)
			assert.Equal(t, before, *obj, "the loaded object is untouched")
		})
	}

	t.Run("a series with an override", func(t *testing.T) {
		obj := storedTask(t, "series", 7, vcal("BEGIN:VCALENDAR", "PRODID:-//test//EN", "VERSION:2.0",
			"BEGIN:VTODO", "UID:series", "DTSTAMP:20260101T000000Z", "SUMMARY:Override",
			"RECURRENCE-ID:20261112T080000Z", "DTSTART:20261113T080000Z", "STATUS:COMPLETED", "END:VTODO",
			"BEGIN:VTODO", "UID:series", "DTSTAMP:20260101T000000Z", "SUMMARY:Master",
			"DTSTART:20261105T080000Z", "RRULE:FREQ=WEEKLY", "END:VTODO", "END:VCALENDAR"))
		repo := repoWith(obj)
		_, err := completeUC(repo).Execute(context.Background(), CompleteTaskInput{Calendar: taskListCalendar(), UUID: "series"})
		assert.ErrorIs(t, err, ErrRecurringNotSupported)
		assert.Empty(t, repo.updated)
	})
}

// TestCompleteTask_UsesTheInjectedClockInUTC (spec §5 rule 4): COMPLETED,
// LAST-MODIFIED and DTSTAMP are one instant, the clock's, in UTC; without a
// clock the use case uses the time of the call.
func TestCompleteTask_UsesTheInjectedClockInUTC(t *testing.T) {
	for _, zone := range []*time.Location{time.UTC, time.FixedZone("EST", -5*3600), time.FixedZone("IST", 5*3600+1800)} {
		t.Run(zone.String(), func(t *testing.T) {
			clock := time.Date(2026, 11, 3, 23, 59, 58, 0, time.UTC).In(zone)
			repo := repoWith(simpleTask(t, "t1"))
			_, err := NewCompleteTaskUseCase(repo, func() time.Time { return clock }).
				Execute(context.Background(), CompleteTaskInput{Calendar: taskListCalendar(), UUID: "t1"})
			require.NoError(t, err)
			data := repo.updated[0].ICalData
			for _, name := range []string{"COMPLETED", "LAST-MODIFIED", "DTSTAMP"} {
				assert.Equal(t, name+":20261103T235958Z", propLine(t, data, name))
			}
			assert.NotContains(t, data, "TZID=")
		})
	}

	t.Run("default clock", func(t *testing.T) {
		repo := repoWith(simpleTask(t, "t1"))
		before := time.Now().UTC().Truncate(time.Second)
		task, err := NewCompleteTaskUseCase(repo, nil).Execute(context.Background(), CompleteTaskInput{Calendar: taskListCalendar(), UUID: "t1"})
		after := time.Now().UTC()
		require.NoError(t, err)
		require.NotNil(t, task.Object.CompletedAt)
		assert.False(t, task.Object.CompletedAt.Before(before))
		assert.False(t, task.Object.CompletedAt.After(after))
		assert.Regexp(t, `^COMPLETED:\d{8}T\d{6}Z$`, propLine(t, task.Object.ICalData, "COMPLETED"))
	})
}

// TestUncompleteTask_EditsInPlace: uncomplete removes COMPLETED, writes
// STATUS:NEEDS-ACTION and PERCENT-COMPLETE:0 (it was 100), with the same
// bookkeeping as complete; nothing else changes.
func TestUncompleteTask_EditsInPlace(t *testing.T) {
	completed := strings.NewReplacer(
		"CATEGORIES:Errands,Home\r\n", "CATEGORIES:Errands,Home\r\nCOMPLETED:20261101T100000Z\r\n",
		"PERCENT-COMPLETE:40\r\n", "PERCENT-COMPLETE:100\r\n",
		"STATUS:IN-PROCESS\r\n", "STATUS:COMPLETED\r\n",
	).Replace(clientTaskData)
	require.Equal(t, completed, reencode(t, completed), "the fixture is in go-ical's canonical form")

	obj := storedTask(t, "passport", 7, completed)
	require.Equal(t, calendar.TaskStatusCompleted, obj.Status)
	repo := repoWith(obj)

	task, err := uncompleteUC(repo).Execute(context.Background(), CompleteTaskInput{Calendar: taskListCalendar(), UUID: "passport"})
	require.NoError(t, err)
	require.Len(t, repo.updated, 1)
	stored := repo.updated[0]

	want := strings.NewReplacer(
		"COMPLETED:20261101T100000Z\r\n", "",
		"DTSTAMP:20260102T080000Z\r\n", "DTSTAMP:"+completeStamp+"\r\n",
		"LAST-MODIFIED:20260102T080000Z\r\n", "LAST-MODIFIED:"+completeStamp+"\r\n",
		"PERCENT-COMPLETE:100\r\n", "PERCENT-COMPLETE:0\r\n",
		"SEQUENCE:3\r\n", "SEQUENCE:4\r\n",
		"STATUS:COMPLETED\r\n", "STATUS:NEEDS-ACTION\r\n",
	).Replace(completed)
	assert.Equal(t, want, stored.ICalData)

	assert.Equal(t, calendar.TaskStatusNeedsAction, stored.Status)
	assert.Nil(t, stored.CompletedAt)
	assert.Equal(t, intPtr(0), stored.PercentComplete)
	assert.NotEqual(t, "etag-before", stored.ETag)
	assert.NotEmpty(t, stored.ETag)
	assert.Same(t, obj, task.Object)
	assert.Equal(t, stored.ETag, task.Object.ETag)
}

// TestUncompleteTask_Percent: PERCENT-COMPLETE is reset to 0 only when it
// says 100 (or more, which reads as 100); any other value, or none, stays.
func TestUncompleteTask_Percent(t *testing.T) {
	for _, tc := range []struct {
		name, prop, want string
	}{
		{"100 becomes 0", "PERCENT-COMPLETE:100", "PERCENT-COMPLETE:0"},
		{"150 reads as 100 and becomes 0", "PERCENT-COMPLETE:150", "PERCENT-COMPLETE:0"},
		{"padded 100 becomes 0", "PERCENT-COMPLETE: 100", "PERCENT-COMPLETE:0"},
		{"40 is kept", "PERCENT-COMPLETE:40", "PERCENT-COMPLETE:40"},
		{"99 is kept", "PERCENT-COMPLETE:99", "PERCENT-COMPLETE:99"},
		{"0 is kept", "PERCENT-COMPLETE:0", "PERCENT-COMPLETE:0"},
		{"malformed is kept", "PERCENT-COMPLETE:most", "PERCENT-COMPLETE:most"},
		{"absent stays absent", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			props := []string{"COMPLETED:20261101T100000Z", "STATUS:COMPLETED"}
			if tc.prop != "" {
				props = append(props, tc.prop)
			}
			repo := repoWith(simpleTask(t, "t1", props...))
			task, err := uncompleteUC(repo).Execute(context.Background(), CompleteTaskInput{Calendar: taskListCalendar(), UUID: "t1"})
			require.NoError(t, err)
			require.Len(t, repo.updated, 1)
			data := repo.updated[0].ICalData
			assert.Equal(t, tc.want, propLine(t, data, "PERCENT-COMPLETE"))
			assert.Equal(t, "STATUS:NEEDS-ACTION", propLine(t, data, "STATUS"))
			assert.Empty(t, propLine(t, data, "COMPLETED"))
			assert.Equal(t, calendar.TaskStatusNeedsAction, task.Object.Status)
			assert.Nil(t, task.Object.CompletedAt)
		})
	}
}

// TestUncompleteTask_CompletedWithoutStatus: a task a client completed with
// only a COMPLETED property is reopened too: COMPLETED goes, STATUS:NEEDS-ACTION
// is written so it reads open.
func TestUncompleteTask_CompletedWithoutStatus(t *testing.T) {
	repo := repoWith(simpleTask(t, "t1", "COMPLETED:20261101T100000Z"))
	task, err := uncompleteUC(repo).Execute(context.Background(), CompleteTaskInput{Calendar: taskListCalendar(), UUID: "t1"})
	require.NoError(t, err)
	require.Len(t, repo.updated, 1)
	assert.Empty(t, propLine(t, task.Object.ICalData, "COMPLETED"))
	assert.Equal(t, "STATUS:NEEDS-ACTION", propLine(t, task.Object.ICalData, "STATUS"))
	assert.Equal(t, calendar.TaskStatusNeedsAction, task.Object.Status)
}

// TestUncompleteTask_NotCompletedIsANoOp: uncompleting a task that is not
// completed writes nothing and keeps the ETag — an in-process task keeps its
// status, a cancelled one stays cancelled, a stale COMPLETED next to an
// explicit open STATUS stays where it is.
func TestUncompleteTask_NotCompletedIsANoOp(t *testing.T) {
	for _, tc := range []struct {
		name  string
		props []string
	}{
		{"no STATUS", nil},
		{"NEEDS-ACTION", []string{"STATUS:NEEDS-ACTION"}},
		{"IN-PROCESS at 100%", []string{"PERCENT-COMPLETE:100", "STATUS:IN-PROCESS"}},
		{"CANCELLED", []string{"STATUS:CANCELLED"}},
		{"NEEDS-ACTION with a stale COMPLETED", []string{"COMPLETED:20250101T000000Z", "STATUS:NEEDS-ACTION"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			obj := simpleTask(t, "t1", tc.props...)
			before := *obj
			repo := repoWith(obj)
			task, err := uncompleteUC(repo).Execute(context.Background(), CompleteTaskInput{Calendar: taskListCalendar(), UUID: "t1"})
			require.NoError(t, err)
			assert.Empty(t, repo.updated)
			assert.Same(t, obj, task.Object)
			assert.Equal(t, before, *task.Object)
		})
	}

	t.Run("complete, uncomplete, uncomplete", func(t *testing.T) {
		repo := repoWith(simpleTask(t, "t1", "PERCENT-COMPLETE:40", "STATUS:IN-PROCESS"))
		in := CompleteTaskInput{Calendar: taskListCalendar(), UUID: "t1"}
		_, err := completeUC(repo).Execute(context.Background(), in)
		require.NoError(t, err)
		reopened, err := uncompleteUC(repo).Execute(context.Background(), in)
		require.NoError(t, err)
		require.Len(t, repo.updated, 2)
		// The 40% from before completing is not restored (documented).
		assert.Equal(t, intPtr(0), reopened.Object.PercentComplete)
		assert.Equal(t, "SEQUENCE:2", propLine(t, reopened.Object.ICalData, "SEQUENCE"))
		etag := reopened.Object.ETag

		again, err := uncompleteUC(repo).Execute(context.Background(), in)
		require.NoError(t, err)
		assert.Len(t, repo.updated, 2, "the second uncomplete writes nothing")
		assert.Equal(t, etag, again.Object.ETag)
	})
}

// TestUncompleteTask_RecurringIsAllowed: reopening a recurring task whose
// master a client completed is not refused; the rule, EXDATE, the override
// and the alarm stay exactly as stored.
func TestUncompleteTask_RecurringIsAllowed(t *testing.T) {
	override := vcal(
		"BEGIN:VTODO",
		"DTSTAMP:20260101T000000Z",
		"DTSTART:20261113T080000Z",
		"RECURRENCE-ID:20261112T080000Z",
		"STATUS:IN-PROCESS",
		"SUMMARY:Weekly review (moved)",
		"UID:series@example.com",
		"END:VTODO",
	)
	data := vcal(
		"BEGIN:VCALENDAR",
		"PRODID:-//test//EN",
		"VERSION:2.0",
		"BEGIN:VTODO",
		"COMPLETED:20261101T100000Z",
		"DTSTAMP:20260101T000000Z",
		"DTSTART:20261105T080000Z",
		"EXDATE:20261119T080000Z",
		"PERCENT-COMPLETE:100",
		"RRULE:FREQ=WEEKLY;COUNT=6",
		"STATUS:COMPLETED",
		"SUMMARY:Weekly review",
		"UID:series@example.com",
		"BEGIN:VALARM",
		"ACTION:DISPLAY",
		"DESCRIPTION:Review",
		"TRIGGER:-PT5M",
		"END:VALARM",
		"END:VTODO",
	) + override + "END:VCALENDAR\r\n"
	require.Equal(t, data, reencode(t, data))

	obj := storedTask(t, "series", 7, data)
	repo := repoWith(obj)
	task, err := uncompleteUC(repo).Execute(context.Background(), CompleteTaskInput{Calendar: taskListCalendar(), UUID: "series"})
	require.NoError(t, err)
	require.Len(t, repo.updated, 1)

	want := strings.NewReplacer(
		"COMPLETED:20261101T100000Z\r\n", "",
		"DTSTAMP:20260101T000000Z\r\nDTSTART:20261105", "DTSTAMP:"+completeStamp+"\r\nDTSTART:20261105",
		"PERCENT-COMPLETE:100\r\n", "LAST-MODIFIED:"+completeStamp+"\r\nPERCENT-COMPLETE:0\r\n",
		"RRULE:FREQ=WEEKLY;COUNT=6\r\nSTATUS:COMPLETED\r\n", "RRULE:FREQ=WEEKLY;COUNT=6\r\nSEQUENCE:1\r\nSTATUS:NEEDS-ACTION\r\n",
	).Replace(data)
	assert.Equal(t, want, task.Object.ICalData)
	assert.Contains(t, task.Object.ICalData, override, "the override is untouched")
	assert.True(t, task.Details.Recurring)
}

// TestTaskCompletion_NotATaskOfThisCalendar: complete and uncomplete answer
// an event's UUID and a task filed under another calendar with ErrNotFound
// and write nothing; a failed lookup is passed on as is.
func TestTaskCompletion_NotATaskOfThisCalendar(t *testing.T) {
	event := stubEvent("event", 7)
	elsewhere := simpleTask(t, "elsewhere", "STATUS:COMPLETED")
	elsewhere.CalendarID = 8
	ops := map[string]func(*stubRepo, CompleteTaskInput) (*Task, error){
		"complete": func(r *stubRepo, in CompleteTaskInput) (*Task, error) {
			return completeUC(r).Execute(context.Background(), in)
		},
		"uncomplete": func(r *stubRepo, in CompleteTaskInput) (*Task, error) {
			return uncompleteUC(r).Execute(context.Background(), in)
		},
	}
	for name, op := range ops {
		t.Run(name, func(t *testing.T) {
			repo := repoWith(event, elsewhere)
			for _, uuid := range []string{"event", "elsewhere"} {
				task, err := op(repo, CompleteTaskInput{Calendar: taskListCalendar(), UUID: uuid})
				assert.ErrorIs(t, err, ErrNotFound, uuid)
				assert.Nil(t, task, uuid)
			}
			// Even on a calendar that does not store tasks: not found comes first.
			_, err := op(repo, CompleteTaskInput{Calendar: &calendar.Calendar{ID: 7, SupportedComponents: "VEVENT"}, UUID: "event"})
			assert.ErrorIs(t, err, ErrNotFound)
			assert.NotErrorIs(t, err, ErrInvalidInput)

			_, err = op(repo, CompleteTaskInput{Calendar: taskListCalendar(), UUID: "unknown"})
			assert.ErrorIs(t, err, errStubNotFound)
			assert.NotErrorIs(t, err, ErrNotFound)

			failing := &stubRepo{lookErr: errors.New("disk I/O error")}
			_, err = op(failing, CompleteTaskInput{Calendar: taskListCalendar(), UUID: "t1"})
			assert.EqualError(t, err, "disk I/O error")
			assert.Empty(t, repo.updated)
		})
	}
}

// TestTaskCompletion_CalendarComponentSet: a task left in a calendar that does
// not store tasks is ErrInvalidInput "This calendar does not store tasks" (as
// create), whatever its state; the calendars that store VTODO work.
func TestTaskCompletion_CalendarComponentSet(t *testing.T) {
	for _, op := range []string{"complete", "uncomplete"} {
		run := func(repo *stubRepo, cal *calendar.Calendar, uuid string) (*Task, error) {
			in := CompleteTaskInput{Calendar: cal, UUID: uuid}
			if op == "complete" {
				return completeUC(repo).Execute(context.Background(), in)
			}
			return uncompleteUC(repo).Execute(context.Background(), in)
		}
		t.Run(op+" refused on an events calendar", func(t *testing.T) {
			for _, props := range [][]string{nil, {"STATUS:COMPLETED"}, {"RRULE:FREQ=DAILY"}} {
				repo := repoWith(simpleTask(t, "t1", props...))
				task, err := run(repo, &calendar.Calendar{ID: 7, SupportedComponents: "VEVENT"}, "t1")
				require.Error(t, err)
				assert.Nil(t, task)
				assert.ErrorIs(t, err, ErrInvalidInput)
				assert.ErrorIs(t, err, calendar.ErrComponentNotStored)
				assert.Equal(t, "This calendar does not store tasks", err.Error())
				assert.Empty(t, repo.updated)
			}
		})
		t.Run(op+" accepted where tasks are stored", func(t *testing.T) {
			for _, set := range []string{"VTODO", "VEVENT,VTODO", ""} {
				status := "STATUS:NEEDS-ACTION"
				if op == "uncomplete" {
					status = "STATUS:COMPLETED"
				}
				repo := repoWith(simpleTask(t, "t1", status))
				_, err := run(repo, &calendar.Calendar{ID: 7, SupportedComponents: set}, "t1")
				require.NoError(t, err, set)
				assert.Len(t, repo.updated, 1, set)
			}
		})
	}
}

// TestTaskCompletion_Failures: a storage error is passed on, not turned into
// a typed error; data that no longer decodes is a server failure, not the
// caller's; a missing calendar is a programming error.
func TestTaskCompletion_Failures(t *testing.T) {
	t.Run("update fails", func(t *testing.T) {
		repo := repoWith(simpleTask(t, "t1"))
		repo.updateErr = errors.New("database is locked")
		task, err := completeUC(repo).Execute(context.Background(), CompleteTaskInput{Calendar: taskListCalendar(), UUID: "t1"})
		assert.Nil(t, task)
		assert.EqualError(t, err, "database is locked")

		repo = repoWith(simpleTask(t, "t1", "STATUS:COMPLETED"))
		repo.updateErr = errors.New("database is locked")
		task, err = uncompleteUC(repo).Execute(context.Background(), CompleteTaskInput{Calendar: taskListCalendar(), UUID: "t1"})
		assert.Nil(t, task)
		assert.EqualError(t, err, "database is locked")
	})

	t.Run("undecodable data", func(t *testing.T) {
		broken := &calendar.CalendarObject{UUID: "t1", CalendarID: 7, ComponentType: calendar.ComponentToDo,
			Status: calendar.TaskStatusCompleted, ETag: "etag-before", ICalData: "not iCalendar"}
		repo := repoWith(broken)
		_, err := uncompleteUC(repo).Execute(context.Background(), CompleteTaskInput{Calendar: taskListCalendar(), UUID: "t1"})
		require.Error(t, err)
		assert.NotErrorIs(t, err, ErrInvalidInput)
		assert.NotErrorIs(t, err, ErrNotFound)
		assert.Empty(t, repo.updated)
		assert.Equal(t, "etag-before", broken.ETag)
		assert.Equal(t, "not iCalendar", broken.ICalData)

		broken.Status = ""
		_, err = completeUC(repo).Execute(context.Background(), CompleteTaskInput{Calendar: taskListCalendar(), UUID: "t1"})
		require.Error(t, err)
		assert.NotErrorIs(t, err, ErrInvalidInput)
		assert.Empty(t, repo.updated)
	})

	t.Run("no calendar", func(t *testing.T) {
		repo := repoWith(simpleTask(t, "t1"))
		_, err := completeUC(repo).Execute(context.Background(), CompleteTaskInput{UUID: "t1"})
		require.Error(t, err)
		assert.NotErrorIs(t, err, ErrInvalidInput)
		_, err = uncompleteUC(repo).Execute(context.Background(), CompleteTaskInput{UUID: "t1"})
		require.Error(t, err)
		assert.Empty(t, repo.updated)
	})
}

// TestEditTask: the in-place edit helper the complete, uncomplete and (story
// 11) update use cases share. It edits the master, not an override that comes
// first; it keeps everything it is not told to change; an edit error comes
// back as is and leaves the object untouched; an object without a VTODO is an
// error.
func TestEditTask(t *testing.T) {
	data := vcal("BEGIN:VCALENDAR", "PRODID:-//test//EN", "VERSION:2.0",
		"BEGIN:VTODO", "DTSTAMP:20260101T000000Z", "RECURRENCE-ID:20261112T080000Z", "SEQUENCE:5",
		"SUMMARY:Override", "UID:s@x", "END:VTODO",
		"BEGIN:VTODO", "DTSTAMP:20260101T000000Z", "DTSTART:20261105T080000Z", "RRULE:FREQ=WEEKLY",
		"SEQUENCE:2", "SUMMARY:Master", "UID:s@x", "END:VTODO", "END:VCALENDAR")
	require.Equal(t, data, reencode(t, data))

	t.Run("edits the master only", func(t *testing.T) {
		obj := storedTask(t, "s", 7, data)
		err := editTask(obj, completeClock, func(_ *ical.Calendar, todo *ical.Component) error {
			todo.Props.SetText(ical.PropSummary, "Edited")
			return nil
		})
		require.NoError(t, err)
		want := strings.NewReplacer(
			"DTSTAMP:20260101T000000Z\r\nDTSTART", "DTSTAMP:"+completeStamp+"\r\nDTSTART",
			"RRULE:FREQ=WEEKLY\r\nSEQUENCE:2\r\nSUMMARY:Master\r\n",
			"LAST-MODIFIED:"+completeStamp+"\r\nRRULE:FREQ=WEEKLY\r\nSEQUENCE:3\r\nSUMMARY:Edited\r\n",
		).Replace(data)
		assert.Equal(t, want, obj.ICalData)
		assert.Equal(t, "Edited", obj.Summary)
		assert.NotEqual(t, "etag-before", obj.ETag)
	})

	t.Run("an edit error leaves the object as it was", func(t *testing.T) {
		obj := storedTask(t, "s", 7, data)
		before := *obj
		refusal := invalidInputf("due must not be before start")
		err := editTask(obj, completeClock, func(_ *ical.Calendar, todo *ical.Component) error {
			todo.Props.SetText(ical.PropSummary, "half-done")
			return refusal
		})
		assert.Same(t, refusal, err)
		assert.Equal(t, before, *obj)
	})

	t.Run("data that cannot be encoded leaves the object as it was", func(t *testing.T) {
		obj := storedTask(t, "s", 7, data)
		before := *obj
		err := editTask(obj, completeClock, func(_ *ical.Calendar, todo *ical.Component) error {
			// DUE and DURATION together: go-ical refuses to encode it.
			todo.Props.SetText(ical.PropDuration, "PT1H")
			todo.Props.SetDateTime(ical.PropDue, time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC))
			return nil
		})
		require.Error(t, err)
		assert.Equal(t, before, *obj)
	})

	t.Run("columns that cannot be derived leave the object as it was", func(t *testing.T) {
		obj := simpleTask(t, "t1")
		before := *obj
		err := editTask(obj, completeClock, func(_ *ical.Calendar, todo *ical.Component) error {
			// Encodes fine, but leaves no VEVENT or VTODO to derive from.
			todo.Name = ical.CompJournal
			return nil
		})
		require.Error(t, err)
		assert.Equal(t, before, *obj)
	})

	t.Run("no VTODO", func(t *testing.T) {
		obj := stubEvent("e", 7)
		before := *obj
		err := editTask(obj, completeClock, func(*ical.Calendar, *ical.Component) error {
			t.Fatal("edit must not be called")
			return nil
		})
		require.Error(t, err)
		assert.Equal(t, before, *obj)
	})
}
