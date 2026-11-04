package calendar

import (
	"strings"
	"time"

	"github.com/emersion/go-ical"
)

// Values of TaskQuery.Status: which tasks a task list returns (#170). There is
// one cancelled rule everywhere: a task is "done" when its Status is COMPLETED
// or CANCELLED (TaskDoneStatuses). TaskFilterOpen is every task that is not
// done (NEEDS-ACTION, IN-PROCESS), TaskFilterCompleted every done one,
// TaskFilterAll every task.
const (
	TaskFilterOpen      = "open"
	TaskFilterCompleted = "completed"
	TaskFilterAll       = "all"
)

// TaskDoneStatuses returns the Status values that make a task done: COMPLETED
// and CANCELLED. The list filter and the counts (open, overdue, completed) use
// this one rule, so `?status=completed` and a "completed" count always agree.
// A new slice on every call, so no caller can change the rule for the others.
func TaskDoneStatuses() []string {
	return []string{TaskStatusCompleted, TaskStatusCancelled}
}

// TaskDay is the moment and the calendar day the task counts are taken at
// (GET /api/v1/tasks/summary, #170), derived from the caller's as_of: an
// instant written with the user's UTC offset. That offset alone decides which
// date "today" is; the server's time zone never does. Every field is in UTC,
// ready to be compared with DueAt.
//
// An open task (not done, TaskDoneStatuses) with a due date is
//   - overdue: a timed task due before Now (instants compared); an all-day
//     task due on a date before Date. An all-day task due today is not
//     overdue, all day long.
//   - due today: a timed task due in [Start, End); an all-day task due in
//     [Date, NextDate), i.e. on as_of's date.
//
// The two overlap: a timed task due earlier today is overdue and due today.
// A floating due time is stored as UTC wall time (denorm), so it is compared
// as if the user were at UTC: a known approximation.
type TaskDay struct {
	// Now is as_of in UTC.
	Now time.Time
	// Start and End bound as_of's calendar day in as_of's own UTC offset,
	// as instants: midnight of that day and 24 hours later. A fixed offset
	// has no DST change, so the day is always 24 hours long.
	Start, End time.Time
	// Date and NextDate are midnight UTC of as_of's date (in its own offset)
	// and of the following date: how an all-day due date is stored in DueAt.
	Date, NextDate time.Time
}

// TaskDayOf returns the TaskDay of asOf, read in asOf's own UTC offset. A
// time whose location is a zone with DST changes (time.Parse picks time.Local
// when the offset matches it) is read in the fixed offset it carries at that
// instant, so the answer does not depend on the server's time zone.
func TaskDayOf(asOf time.Time) TaskDay {
	_, offset := asOf.Zone()
	local := asOf.In(time.FixedZone("", offset))
	y, m, d := local.Date()
	start := time.Date(y, m, d, 0, 0, 0, 0, local.Location())
	date := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	return TaskDay{
		Now:      asOf.UTC(),
		Start:    start.UTC(),
		End:      start.Add(24 * time.Hour).UTC(),
		Date:     date,
		NextDate: date.AddDate(0, 0, 1),
	}
}

// TaskDetails are the parts of a stored task that the task API reports and
// that have no denormalized column, so they are read from the iCalendar data:
// whether DTSTART is a date or a date-time, and the recurrence. Everything
// else (status, due, completion, percent, priority) comes from the columns.
type TaskDetails struct {
	// Start is the master's DTSTART in UTC (a TZID is honoured, a floating or
	// date-only value is read as UTC); nil when absent or malformed.
	Start *time.Time
	// StartAllDay reports a date-only DTSTART (VALUE=DATE); Start is then
	// midnight UTC of that date.
	StartAllDay bool
	// RRule is the master's RRULE value as stored, "" when it has none (or an
	// empty one).
	RRule string
	// Recurring reports an RRULE or RDATE on the master. A recurring task is
	// served as one row, its master, and never expanded (spec §1c).
	Recurring bool
	// Alarms are the master VTODO's VALARMs (#169), in document order.
	Alarms []AlarmSpec
}

// TaskDetails decodes the object's iCalendar data and reads the TaskDetails
// from its master VTODO (TaskMaster). The zero value comes back when the data
// does not decode or holds no VTODO: the denormalized columns still describe
// the task, so one unreadable object must not break a whole list.
func (o *CalendarObject) TaskDetails() TaskDetails {
	cal, err := ical.NewDecoder(strings.NewReader(o.ICalData)).Decode()
	if err != nil {
		return TaskDetails{}
	}
	master := TaskMaster(cal)
	if master == nil {
		return TaskDetails{}
	}
	var details TaskDetails
	if p := master.Props.Get(ical.PropDateTimeStart); p != nil {
		details.Start = utcDateTime(p)
		details.StartAllDay = details.Start != nil && p.ValueType() == ical.ValueDate
	}
	rule := master.Props.Get(ical.PropRecurrenceRule)
	if rule != nil {
		details.RRule = rule.Value
	}
	// Any RRULE counts, an unparseable one too: RecurrenceEndTime treats it
	// as an unbounded series as well.
	details.Recurring = rule != nil || master.Props.Get(ical.PropRecurrenceDates) != nil
	details.Alarms = ParseAlarms(master)
	return details
}

// TaskMaster returns the VTODO a task is read from and edited in: the first
// VTODO without a RECURRENCE-ID, else the first VTODO (an object holding only
// overrides, which should not happen). Nil when cal holds no VTODO. For a task
// object it is the same component PopulateDenormFieldsFromICal derives the
// task columns from.
func TaskMaster(cal *ical.Calendar) *ical.Component {
	var first *ical.Component
	for _, child := range cal.Children {
		if child.Name != ical.CompToDo {
			continue
		}
		if child.Props.Get(ical.PropRecurrenceID) == nil {
			return child
		}
		if first == nil {
			first = child
		}
	}
	return first
}
