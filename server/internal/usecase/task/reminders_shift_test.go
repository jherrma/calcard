package task

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// #169 story 13: the all-day due shift of the task update (spec §7.5 step 4).

type fakeZones struct {
	loc       *time.Location
	calls     int
	owner     uint
	fallbacks int
}

func (f *fakeZones) One(_ context.Context, id uint) (*time.Location, bool, error) {
	f.calls++
	f.owner = id
	if f.loc == nil {
		return time.UTC, false, nil
	}
	return f.loc, true, nil
}

func (f *fakeZones) Fallback(context.Context, uint) { f.fallbacks++ }

const (
	tAbs = "BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER;VALUE=DATE-TIME:20261104T080000Z\r\nEND:VALARM\r\n"
	tRel = "BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER;RELATED=END:PT9H\r\nEND:VALARM\r\n"
	tFlt = "BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER;VALUE=DATE-TIME:20261104T090000\r\nEND:VALARM\r\n"
)

func shiftTask(t *testing.T, due string, alarms string) *calendar.CalendarObject {
	t.Helper()
	data := "BEGIN:VCALENDAR\r\nPRODID:-//test//EN\r\nVERSION:2.0\r\nBEGIN:VTODO\r\nDTSTAMP:20260102T080000Z\r\n" + due +
		"SUMMARY:Task\r\nUID:s@example.com\r\n" + alarms + "END:VTODO\r\nEND:VCALENDAR\r\n"
	return storedTask(t, "s", 7, data)
}

func runShift(t *testing.T, obj *calendar.CalendarObject, z *fakeZones, in UpdateTaskInput) (*stubRepo, error) {
	t.Helper()
	repo := repoWith(obj)
	cal := taskListCalendar()
	cal.UserID = 42
	in.Calendar, in.UUID = cal, obj.UUID
	uc := updateUC(repo)
	if z != nil {
		uc.WithOwnerZones(z)
	}
	_, err := uc.Execute(context.Background(), in)
	return repo, err
}

func triggers(data string) []string {
	var out []string
	for _, l := range strings.Split(data, "\r\n") {
		if strings.HasPrefix(l, "TRIGGER") {
			out = append(out, l)
		}
	}
	return out
}

const dueAllDay = "DUE;VALUE=DATE:20261105\r\n"

func TestUpdateTask_AllDayDueMoveShiftsAbsoluteTriggers(t *testing.T) {
	berlin, _ := time.LoadLocation("Europe/Berlin")
	cases := map[string]struct{ due, want string }{
		"+7 days":   {"2026-11-12", "TRIGGER;VALUE=DATE-TIME:20261111T080000Z"},
		"into CEST": {"2027-04-02", "TRIGGER;VALUE=DATE-TIME:20270401T070000Z"},
		"-2 days":   {"2026-11-03", "TRIGGER;VALUE=DATE-TIME:20261102T080000Z"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			z := &fakeZones{loc: berlin}
			repo, err := runShift(t, shiftTask(t, dueAllDay, tAbs+tRel+tFlt), z, UpdateTaskInput{Due: change(c.due)})
			require.NoError(t, err)
			require.Len(t, repo.updated, 1)
			assert.Equal(t, []string{c.want, "TRIGGER;RELATED=END:PT9H", "TRIGGER;VALUE=DATE-TIME:20261104T090000"}, triggers(repo.updated[0].ICalData))
			assert.Equal(t, 1, z.calls)
			assert.Equal(t, uint(42), z.owner, "the calendar owner's zone")
		})
	}
}

func TestUpdateTask_ShiftNeedsAnAllDayDueMove(t *testing.T) {
	berlin, _ := time.LoadLocation("Europe/Berlin")
	t.Run("a title edit shifts nothing and loads no zone", func(t *testing.T) {
		z := &fakeZones{loc: berlin}
		repo, err := runShift(t, shiftTask(t, dueAllDay, tAbs), z, UpdateTaskInput{Summary: strPtr("New")})
		require.NoError(t, err)
		assert.Equal(t, []string{"TRIGGER;VALUE=DATE-TIME:20261104T080000Z"}, triggers(repo.updated[0].ICalData))
		assert.Zero(t, z.calls)
	})
	t.Run("the same due date shifts nothing", func(t *testing.T) {
		z := &fakeZones{loc: berlin}
		repo, err := runShift(t, shiftTask(t, dueAllDay, tAbs), z, UpdateTaskInput{Due: change("2026-11-05")})
		require.NoError(t, err)
		assert.Empty(t, repo.updated)
	})
	t.Run("a timed task never shifts", func(t *testing.T) {
		z := &fakeZones{loc: berlin}
		repo, err := runShift(t, shiftTask(t, "DUE:20261105T100000Z\r\n", tAbs), z, UpdateTaskInput{Due: change("2026-11-12T10:00:00Z")})
		require.NoError(t, err)
		assert.Equal(t, []string{"TRIGGER;VALUE=DATE-TIME:20261104T080000Z"}, triggers(repo.updated[0].ICalData))
		assert.Zero(t, z.calls)
	})
	t.Run("all-day to timed shifts nothing", func(t *testing.T) {
		z := &fakeZones{loc: berlin}
		repo, err := runShift(t, shiftTask(t, dueAllDay, tAbs), z, UpdateTaskInput{Due: change("2026-11-12T10:00:00Z"), DueAllDay: boolPtr(false)})
		require.NoError(t, err)
		assert.Equal(t, []string{"TRIGGER;VALUE=DATE-TIME:20261104T080000Z"}, triggers(repo.updated[0].ICalData))
	})
	t.Run("timed to all-day shifts nothing", func(t *testing.T) {
		z := &fakeZones{loc: berlin}
		repo, err := runShift(t, shiftTask(t, "DUE:20261105T100000Z\r\n", tAbs), z, UpdateTaskInput{Due: change("2026-11-12"), DueAllDay: boolPtr(true)})
		require.NoError(t, err)
		assert.Equal(t, []string{"TRIGGER;VALUE=DATE-TIME:20261104T080000Z"}, triggers(repo.updated[0].ICalData))
		assert.Zero(t, z.calls)
	})
	t.Run("removing the due shifts nothing", func(t *testing.T) {
		z := &fakeZones{loc: berlin}
		repo, err := runShift(t, shiftTask(t, dueAllDay, tAbs), z, UpdateTaskInput{Due: cleared[string]()})
		require.NoError(t, err)
		assert.Equal(t, []string{"TRIGGER;VALUE=DATE-TIME:20261104T080000Z"}, triggers(repo.updated[0].ICalData))
	})
	t.Run("no resolver is whole UTC days", func(t *testing.T) {
		repo, err := runShift(t, shiftTask(t, dueAllDay, tAbs), nil, UpdateTaskInput{Due: change("2026-11-12")})
		require.NoError(t, err)
		assert.Equal(t, []string{"TRIGGER;VALUE=DATE-TIME:20261111T080000Z"}, triggers(repo.updated[0].ICalData))
	})
	t.Run("an owner without a preference is UTC and logs the fallback", func(t *testing.T) {
		z := &fakeZones{}
		repo, err := runShift(t, shiftTask(t, dueAllDay, tAbs), z, UpdateTaskInput{Due: change("2026-11-12")})
		require.NoError(t, err)
		assert.Equal(t, []string{"TRIGGER;VALUE=DATE-TIME:20261111T080000Z"}, triggers(repo.updated[0].ICalData))
		assert.Equal(t, 1, z.fallbacks)
	})
}

func TestUpdateTask_ShiftSkipsRemindersOfTheSameRequest(t *testing.T) {
	berlin, _ := time.LoadLocation("Europe/Berlin")
	z := &fakeZones{loc: berlin}
	at := func(s string) calendar.ReminderInput {
		v, err := time.Parse(time.RFC3339, s)
		require.NoError(t, err)
		return absAt(v)
	}
	repo, err := runShift(t, shiftTask(t, dueAllDay, tAbs), z, UpdateTaskInput{
		Due:       change("2026-11-12"),
		Reminders: remindersPtr(at("2026-11-04T08:00:00Z"), at("2026-11-20T08:00:00Z"), off(32400)),
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"TRIGGER;VALUE=DATE-TIME:20261111T080000Z", "TRIGGER;VALUE=DATE-TIME:20261120T080000Z", "TRIGGER;RELATED=END:PT9H"},
		triggers(repo.updated[0].ICalData))
}
