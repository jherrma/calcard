package calendar

import (
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-ical"
)

// Values of CalendarObject.Status for a VTODO: the four VTODO STATUS values of
// RFC 5545 §3.8.1.11, upper-cased. Every VTODO row carries exactly one of them;
// VEVENT rows keep an empty Status.
const (
	TaskStatusNeedsAction = "NEEDS-ACTION"
	TaskStatusInProcess   = "IN-PROCESS"
	TaskStatusCompleted   = "COMPLETED"
	TaskStatusCancelled   = "CANCELLED"
)

// populateTaskFields derives the task columns (Status, DueAt, CompletedAt,
// PercentComplete, Priority) and IsAllDay from a VTODO's primary component.
//
// A malformed optional property leaves its column nil and is otherwise
// ignored: a client's bad value must never make a PUT fail, and the stored
// iCalendar keeps the raw value. Floating times are read as UTC wall time, as
// for StartTime (spec R3).
func (o *CalendarObject) populateTaskFields(c *ical.Component) {
	o.Status = taskStatus(c)
	o.DueAt = taskDue(c)
	o.CompletedAt = nil
	if p := c.Props.Get(ical.PropCompleted); p != nil {
		o.CompletedAt = utcDateTime(p)
	}
	o.PercentComplete = taskPercentComplete(c)
	o.Priority = TaskPriority(c)
	o.IsAllDay = taskAllDay(c)
}

// clearTaskFields empties the task columns of a non-VTODO object, so a PUT
// that turns a task into an event keeps no stale task values.
func (o *CalendarObject) clearTaskFields() {
	o.Status = ""
	o.DueAt = nil
	o.CompletedAt = nil
	o.PercentComplete = nil
	o.Priority = nil
}

// taskStatus is the upper-cased STATUS. Without a STATUS the task counts as
// COMPLETED when it carries a COMPLETED property, else NEEDS-ACTION. A value
// outside the VTODO set reads NEEDS-ACTION, so SQL filters and counts never
// need a special case.
func taskStatus(c *ical.Component) string {
	status := ""
	if p := c.Props.Get(ical.PropStatus); p != nil {
		status = strings.ToUpper(strings.TrimSpace(p.Value))
	}
	switch status {
	case TaskStatusNeedsAction, TaskStatusInProcess, TaskStatusCompleted, TaskStatusCancelled:
		return status
	case "":
		if c.Props.Get(ical.PropCompleted) != nil {
			return TaskStatusCompleted
		}
	}
	return TaskStatusNeedsAction
}

// taskDue is DUE, else DTSTART+DURATION (RFC 5545 §3.6.2 allows either), in
// UTC. A date-only value is midnight UTC of that date. Nil when the task has
// no due date or the deciding property is malformed; a negative DURATION is
// malformed (RFC 5545 §3.8.2.5 requires a positive one in a VTODO).
func taskDue(c *ical.Component) *time.Time {
	if p := c.Props.Get(ical.PropDue); p != nil {
		return utcDateTime(p)
	}
	startProp := c.Props.Get(ical.PropDateTimeStart)
	durProp := c.Props.Get(ical.PropDuration)
	if startProp == nil || durProp == nil {
		return nil
	}
	start := utcDateTime(startProp)
	dur, err := durProp.Duration()
	if start == nil || err != nil || dur < 0 {
		return nil
	}
	due := start.Add(dur)
	return &due
}

// taskAllDay reports whether the task's due date is date-only: DUE, else
// DTSTART, has VALUE=DATE.
func taskAllDay(c *ical.Component) bool {
	p := c.Props.Get(ical.PropDue)
	if p == nil {
		p = c.Props.Get(ical.PropDateTimeStart)
	}
	return p != nil && p.ValueType() == ical.ValueDate
}

// taskPercentComplete is PERCENT-COMPLETE clamped to 0..100; nil when absent
// or not an integer.
func taskPercentComplete(c *ical.Component) *int {
	v, ok := intPropValue(c, ical.PropPercentComplete)
	if !ok {
		return nil
	}
	v = min(max(v, 0), 100)
	return &v
}

// TaskPriority is PRIORITY 1..9. 0 means "undefined" in RFC 5545 §3.8.1.9 and
// is stored as nil, like an absent, non-integer or out-of-range value.
func TaskPriority(c *ical.Component) *int {
	v, ok := intPropValue(c, ical.PropPriority)
	if !ok || v < 1 || v > 9 {
		return nil
	}
	return &v
}

// utcDateTime parses a DATE or DATE-TIME property (TZID honoured, floating and
// DATE read as UTC) and returns it in UTC, or nil when it does not parse.
func utcDateTime(p *ical.Prop) *time.Time {
	t, err := p.DateTime(time.UTC)
	if err != nil {
		return nil
	}
	t = t.UTC()
	return &t
}

// intPropValue parses an INTEGER property; ok is false when it is absent or
// not an integer.
func intPropValue(c *ical.Component, name string) (int, bool) {
	p := c.Props.Get(name)
	if p == nil {
		return 0, false
	}
	v, err := strconv.Atoi(strings.TrimSpace(p.Value))
	if err != nil {
		return 0, false
	}
	return v, true
}
