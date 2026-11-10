package task

import (
	"strings"
	"testing"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests for reminder writes on tasks (#169 story 12, spec §7.4/§7.5).

func off(s int64) calendar.ReminderInput { return calendar.ReminderInput{OffsetSeconds: &s} }

func absAt(t time.Time) calendar.ReminderInput { return calendar.ReminderInput{At: &t} }

func remindersPtr(in ...calendar.ReminderInput) *[]calendar.ReminderInput {
	if in == nil {
		in = []calendar.ReminderInput{}
	}
	return &in
}

func valarmCount(data string) int { return strings.Count(data, "BEGIN:VALARM") }

func TestCreateTask_Reminders(t *testing.T) {
	t.Run("a relative reminder is written against DUE", func(t *testing.T) {
		_, data := mustCreate(t, CreateTaskInput{Calendar: taskListCalendar(), Summary: "Pay", Due: "2026-11-05T10:00:00Z",
			Reminders: []calendar.ReminderInput{off(-1800)}})
		assert.Contains(t, data, "TRIGGER;RELATED=END:-PT30M\r\n")
		assert.Equal(t, 1, valarmCount(data))
		assert.Contains(t, data, "ACTION:DISPLAY\r\n")
	})
	t.Run("an all-day task takes PT9H", func(t *testing.T) {
		_, data := mustCreate(t, CreateTaskInput{Calendar: taskListCalendar(), Summary: "Pay", Due: "2026-11-05", DueAllDay: true,
			Reminders: []calendar.ReminderInput{off(32400)}})
		assert.Contains(t, data, "TRIGGER;RELATED=END:PT9H\r\n")
	})
	t.Run("an absolute reminder needs no due date", func(t *testing.T) {
		at := createClock.UTC().Add(48 * time.Hour)
		_, data := mustCreate(t, CreateTaskInput{Calendar: taskListCalendar(), Summary: "Pay", Reminders: []calendar.ReminderInput{absAt(at)}})
		assert.Contains(t, data, "TRIGGER;VALUE=DATE-TIME:"+at.Format("20060102T150405Z")+"\r\n")
	})
	t.Run("none writes no VALARM", func(t *testing.T) {
		_, data := mustCreate(t, CreateTaskInput{Calendar: taskListCalendar(), Summary: "Pay", Due: "2026-11-05T10:00:00Z"})
		assert.Zero(t, valarmCount(data))
	})
}

func TestCreateTask_ReminderRefusals(t *testing.T) {
	far := createClock.UTC().Add(401 * 24 * time.Hour)
	cases := map[string]struct {
		in   CreateTaskInput
		want string
	}{
		"offset without due": {CreateTaskInput{Reminders: []calendar.ReminderInput{off(-60)}}, "A reminder needs a due date"},
		"offset out of range": {CreateTaskInput{Due: "2026-11-05T10:00:00Z", Reminders: []calendar.ReminderInput{off(401 * 86400)}},
			"reminder offset out of range"},
		"at out of range": {CreateTaskInput{Reminders: []calendar.ReminderInput{absAt(far)}}, "reminder time out of range"},
		"eleven": {CreateTaskInput{Due: "2026-11-05T10:00:00Z", Reminders: make([]calendar.ReminderInput, 11)},
			"reminders must contain at most 10 entries"},
		"neither": {CreateTaskInput{Due: "2026-11-05T10:00:00Z", Reminders: []calendar.ReminderInput{{}}},
			"a reminder needs exactly one of offset_seconds and at"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			tc.in.Calendar, tc.in.Summary = taskListCalendar(), "Pay"
			task, repo, err := runCreate(tc.in)
			require.ErrorIs(t, err, ErrInvalidInput)
			assert.EqualError(t, err, tc.want)
			assert.Nil(t, task)
			assert.Empty(t, repo.created)
		})
	}
	t.Run("a field 400 comes before a reminder 400", func(t *testing.T) {
		_, _, err := runCreate(CreateTaskInput{Calendar: taskListCalendar(), Summary: " ", Reminders: []calendar.ReminderInput{off(-60)}})
		assert.EqualError(t, err, "summary is required")
	})
	t.Run("ten are accepted", func(t *testing.T) {
		var list []calendar.ReminderInput
		for i := 0; i < 10; i++ {
			list = append(list, off(int64(-60*(i+1))))
		}
		_, data := mustCreate(t, CreateTaskInput{Calendar: taskListCalendar(), Summary: "Pay", Due: "2026-11-05T10:00:00Z", Reminders: list})
		assert.Equal(t, 10, valarmCount(data))
	})
}

const foreignAlarmTask = "BEGIN:VCALENDAR\r\nPRODID:-//test//EN\r\nVERSION:2.0\r\nBEGIN:VTODO\r\n" +
	"DTSTAMP:20260102T080000Z\r\nDUE:20261105T080000Z\r\nRELATED-TO;RELTYPE=PARENT:parent\r\nSEQUENCE:3\r\nSUMMARY:Task\r\nUID:t@example.com\r\n" +
	"X-FOO;X-PARAM=1:bar\r\n" +
	"BEGIN:VALARM\r\nACTION:EMAIL\r\nDESCRIPTION:mail\r\nSUMMARY:mail\r\nTRIGGER:-PT30M\r\nEND:VALARM\r\n" +
	"BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER;RELATED=END:-PT15M\r\nUID:mine\r\nEND:VALARM\r\n" +
	"END:VTODO\r\nEND:VCALENDAR\r\n"

func TestUpdateTask_Reminders(t *testing.T) {
	t.Run("changes the VALARMs, bumps bookkeeping, keeps foreign ones", func(t *testing.T) {
		obj := storedTask(t, "t", 7, foreignAlarmTask)
		_, data := mustUpdate(t, obj, UpdateTaskInput{Reminders: remindersPtr(off(-3600))})
		assert.Contains(t, data, "TRIGGER;RELATED=END:-PT1H\r\n")
		assert.NotContains(t, data, "-PT15M", "the old editable one is gone")
		assert.Contains(t, data, "ACTION:EMAIL\r\nDESCRIPTION:mail\r\nSUMMARY:mail\r\nTRIGGER:-PT30M\r\nEND:VALARM", "foreign VALARM byte for byte")
		assert.Contains(t, data, "SEQUENCE:4\r\n")
		assert.Contains(t, data, "LAST-MODIFIED:"+completeStamp+"\r\n")
		assert.Contains(t, data, "DTSTAMP:"+completeStamp+"\r\n")
		assert.Contains(t, data, "RELATED-TO;RELTYPE=PARENT:parent\r\n")
		assert.Contains(t, data, "X-FOO;X-PARAM=1:bar\r\n")
	})
	t.Run("the same list writes nothing", func(t *testing.T) {
		obj := storedTask(t, "t", 7, foreignAlarmTask)
		task, repo, err := runUpdate(t, obj, UpdateTaskInput{Reminders: remindersPtr(off(-900))})
		require.NoError(t, err)
		assert.Empty(t, repo.updated)
		assert.Equal(t, "etag-before", task.Object.ETag)
	})
	t.Run("an empty list removes the editable ones only", func(t *testing.T) {
		obj := storedTask(t, "t", 7, foreignAlarmTask)
		_, data := mustUpdate(t, obj, UpdateTaskInput{Reminders: remindersPtr()})
		assert.Equal(t, 1, valarmCount(data))
		assert.Contains(t, data, "ACTION:EMAIL")
	})
	t.Run("absent leaves every VALARM, also with other edits", func(t *testing.T) {
		obj := storedTask(t, "t", 7, foreignAlarmTask)
		_, data := mustUpdate(t, obj, UpdateTaskInput{Summary: strPtr("New")})
		assert.Equal(t, 2, valarmCount(data))
		assert.Contains(t, data, "TRIGGER;RELATED=END:-PT15M\r\nUID:mine")
	})
	t.Run("a reminder with other edits is one write", func(t *testing.T) {
		obj := storedTask(t, "t", 7, foreignAlarmTask)
		_, data := mustUpdate(t, obj, UpdateTaskInput{Summary: strPtr("New"), Reminders: remindersPtr(off(-900), off(-60))})
		assert.Contains(t, data, "SUMMARY:New")
		assert.Equal(t, 3, valarmCount(data))
	})
	t.Run("an offset on a task without due is refused, at is accepted", func(t *testing.T) {
		obj := simpleTask(t, "nodue")
		task, repo, err := runUpdate(t, obj, UpdateTaskInput{Reminders: remindersPtr(off(-60))})
		assert.EqualError(t, err, "A reminder needs a due date")
		require.ErrorIs(t, err, ErrInvalidInput)
		assert.Nil(t, task)
		assert.Empty(t, repo.updated)

		at := completeClock.UTC().Add(24 * time.Hour)
		_, data := mustUpdate(t, simpleTask(t, "nodue"), UpdateTaskInput{Reminders: remindersPtr(absAt(at))})
		assert.Contains(t, data, "TRIGGER;VALUE=DATE-TIME:"+at.Format("20060102T150405Z"))
	})
	t.Run("the due of the same request counts", func(t *testing.T) {
		_, data := mustUpdate(t, simpleTask(t, "nodue"), UpdateTaskInput{Due: change("2026-11-09T10:00:00Z"), Reminders: remindersPtr(off(-60))})
		assert.Contains(t, data, "TRIGGER;RELATED=END:-PT1M")
		// ...and removing it in the same request does not.
		_, _, err := runUpdate(t, simpleTask(t, "hasdue", "DUE:20261105T080000Z"), UpdateTaskInput{Due: cleared[string](), Reminders: remindersPtr(off(-60))})
		assert.EqualError(t, err, "A reminder needs a due date")
	})
	t.Run("DTSTART with DURATION counts as a due date", func(t *testing.T) {
		_, data := mustUpdate(t, simpleTask(t, "dur", "DTSTART:20261105T080000Z", "DURATION:PT1H"), UpdateTaskInput{Reminders: remindersPtr(off(-60))})
		assert.Contains(t, data, "TRIGGER;RELATED=END:-PT1M")
	})
	t.Run("removing the due keeps the VALARMs", func(t *testing.T) {
		obj := storedTask(t, "t", 7, foreignAlarmTask)
		_, data := mustUpdate(t, obj, UpdateTaskInput{Due: cleared[string]()})
		assert.NotContains(t, data, "DUE:")
		assert.Equal(t, 2, valarmCount(data))
		// the stored reminder can be sent back on the due-less task
		task, repo, err := runUpdate(t, storedTask(t, "t", 7, strings.Replace(foreignAlarmTask, "DUE:20261105T080000Z\r\n", "", 1)),
			UpdateTaskInput{Reminders: remindersPtr(off(-900))})
		require.NoError(t, err)
		assert.Empty(t, repo.updated)
		assert.NotNil(t, task)
	})
	t.Run("range refusals only for new entries", func(t *testing.T) {
		_, _, err := runUpdate(t, storedTask(t, "t", 7, foreignAlarmTask), UpdateTaskInput{Reminders: remindersPtr(off(500 * 86400))})
		assert.EqualError(t, err, "reminder offset out of range")
		_, _, err = runUpdate(t, storedTask(t, "t", 7, foreignAlarmTask), UpdateTaskInput{Reminders: remindersPtr(absAt(completeClock.Add(500 * 24 * time.Hour)))})
		assert.EqualError(t, err, "reminder time out of range")
		_, _, err = runUpdate(t, storedTask(t, "t", 7, foreignAlarmTask), UpdateTaskInput{Reminders: remindersPtr(make([]calendar.ReminderInput, 11)...)})
		assert.EqualError(t, err, "reminders must contain at most 10 entries")
	})
}

func TestUpdateTask_RemindersOnRecurring(t *testing.T) {
	series := func() *calendar.CalendarObject {
		return simpleTask(t, "rec", "DTSTART:20261105T080000Z", "DUE:20261105T090000Z", "RRULE:FREQ=DAILY")
	}
	t.Run("a reminders-only PATCH edits the master", func(t *testing.T) {
		_, data := mustUpdate(t, series(), UpdateTaskInput{Reminders: remindersPtr(off(-600))})
		assert.Contains(t, data, "TRIGGER;RELATED=END:-PT10M")
		assert.Contains(t, data, "RRULE:FREQ=DAILY")
	})
	t.Run("an invalid reminder beats the 409", func(t *testing.T) {
		obj := series()
		task, repo, err := runUpdate(t, obj, UpdateTaskInput{Status: strPtr("completed"), Reminders: remindersPtr(off(500 * 86400))})
		require.ErrorIs(t, err, ErrInvalidInput)
		assert.NotErrorIs(t, err, ErrRecurringNotSupported)
		assert.Nil(t, task)
		assert.Empty(t, repo.updated)
		// shape too
		_, _, err = runUpdate(t, series(), UpdateTaskInput{Status: strPtr("completed"), Reminders: remindersPtr(calendar.ReminderInput{})})
		assert.NotErrorIs(t, err, ErrRecurringNotSupported)
		require.ErrorIs(t, err, ErrInvalidInput)
	})
	t.Run("a valid reminder with completing is still the 409", func(t *testing.T) {
		_, repo, err := runUpdate(t, series(), UpdateTaskInput{Status: strPtr("completed"), Reminders: remindersPtr(off(-600))})
		require.ErrorIs(t, err, ErrRecurringNotSupported)
		assert.Empty(t, repo.updated)
	})
}
