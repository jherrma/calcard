package task

import (
	"context"
	"errors"
	"time"

	"github.com/emersion/go-ical"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

// Change is one optional field of a partial update that tells "absent" from
// "null": Set false leaves the property as stored, Set with a nil Value
// removes it, Set with a Value writes it. The HTTP layer converts its
// dto.OptionalField into it.
type Change[T any] struct {
	Set   bool
	Value *T
}

// UpdateTaskInput is a partial update of one task (PATCH, spec §3.1). Only
// the fields given are touched; there is no recurrence field and no list
// change.
type UpdateTaskInput struct {
	// Calendar is the calendar the URL addresses, already resolved and
	// authorised for writing by the caller; UUID is the task's.
	Calendar *calendar.Calendar
	UUID     string
	// Summary, when given, must not be blank and is at most 500 runes, stored
	// trimmed. It cannot be removed (null is "not given").
	Summary *string
	// Description: null or "" removes DESCRIPTION.
	Description Change[string]
	// Due and Start: null or "" removes the property (Due also a DURATION
	// standing in for it). A value is a date ("YYYY-MM-DD") when the task is
	// all-day after the update (DueAllDay, else its current due_all_day),
	// otherwise an RFC 3339 date-time, as for create.
	Due   Change[string]
	Start Change[string]
	// DueAllDay is the value type DUE and DTSTART have after the update; nil
	// keeps the task's current one.
	DueAllDay *bool
	// Priority (0..9) and PercentComplete (0..100) are written as given; null
	// removes them.
	Priority        Change[int]
	PercentComplete Change[int]
	// Status, when given, is needs-action (also ""), in-process, completed or
	// cancelled; see Execute.
	Status *string
	// Timezone is the IANA zone a timed Due/Start of this request is written
	// in (TZID plus a VTIMEZONE); without a usable one they go out in UTC, as
	// for create. It does not move a time the request does not send.
	Timezone string
	// IfMatch is the ETag the caller's If-Match matched ("" when it sent
	// none): the write happens only on a task that still has it, else
	// ErrPreconditionFailed.
	IfMatch string
}

type UpdateTaskUseCase struct {
	calendarRepo calendar.CalendarRepository
	now          func() time.Time
}

// NewUpdateTaskUseCase builds the use case. now is the clock for the
// timestamps it writes (LAST-MODIFIED, DTSTAMP, COMPLETED); nil means
// time.Now.
func NewUpdateTaskUseCase(calendarRepo calendar.CalendarRepository, now func() time.Time) *UpdateTaskUseCase {
	if now == nil {
		now = time.Now
	}
	return &UpdateTaskUseCase{calendarRepo: calendarRepo, now: now}
}

// Execute edits the task in place (editTask, spec §5 rule 2): only the
// properties of the fields given are set or removed, so RRULE, RDATE, EXDATE,
// RECURRENCE-ID overrides, VALARM, RELATED-TO, X- and unknown properties and
// the VTIMEZONEs survive; a field that already holds the requested value
// keeps its stored form (a SUMMARY's LANGUAGE, a DUE's TZID). A timed due or
// start written with a TZID gets its VTIMEZONE when the object has none.
// SEQUENCE, LAST-MODIFIED, the ETag and the calendar's sync token advance
// (UpdateCalendarObjectIfETag); an update that changes nothing writes nothing and
// keeps the ETag.
//
// Status on a task that does not repeat: completed is what complete does
// (STATUS:COMPLETED, COMPLETED now, PERCENT-COMPLETE:100; a percent_complete
// in the same request is ignored) and a no-op on a completed task; another
// status reopens a completed task as uncomplete does (COMPLETED removed,
// PERCENT-COMPLETE 100 reset to 0) and then writes that STATUS. A recurring
// task (RRULE or RDATE on the master) refuses status completed or cancelled
// and percent_complete 100 with ErrRecurringNotSupported, whatever its state
// (spec §1c: the edit form must not get round the complete refusal); every
// other edit applies to its master.
//
// In this order: anything but a task of input.Calendar is ErrNotFound; a
// calendar that does not store tasks, then each invalid field, then a due or
// start whose value type or order does not fit the task's other time are
// ErrInvalidInput; then ErrRecurringNotSupported. Nothing is written on a
// refusal.
func (uc *UpdateTaskUseCase) Execute(ctx context.Context, input UpdateTaskInput) (*Task, error) {
	return writeTask(input.IfMatch, func() (*Task, error) { return uc.attempt(ctx, input) })
}

func (uc *UpdateTaskUseCase) attempt(ctx context.Context, input UpdateTaskInput) (*Task, error) {
	obj, err := loadWritableTask(ctx, uc.calendarRepo, input.Calendar, input.UUID)
	if err != nil {
		return nil, err
	}
	if err := checkIfMatch(obj, input.IfMatch); err != nil {
		return nil, err
	}
	loadedETag := obj.ETag
	update, err := newTaskUpdate(input, obj.IsAllDay)
	if err != nil {
		return nil, err
	}
	stored := loadedTask{status: obj.Status, dueAt: obj.DueAt, details: obj.TaskDetails()}

	now := uc.now().UTC()
	err = editTask(obj, now, func(cal *ical.Calendar, todo *ical.Component) error {
		return update.apply(cal, todo, stored, now)
	})
	if errors.Is(err, errUnchanged) {
		return newTask(obj), nil
	}
	if err != nil {
		return nil, err
	}
	if err := storeTask(ctx, uc.calendarRepo, obj, loadedETag); err != nil {
		return nil, err
	}
	return newTask(obj), nil
}

// loadedTask is what the update compares the request with: the task's status
// and due columns and its details, as loaded.
type loadedTask struct {
	status  string
	dueAt   *time.Time
	details calendar.TaskDetails
}

// taskUpdate is a validated UpdateTaskInput.
type taskUpdate struct {
	summary     *string // trimmed
	description Change[string]
	status      string // the STATUS value; "" when not given
	priority    Change[int]
	percent     Change[int]
	// allDay is the value type of a due/start this request writes;
	// allDaySent says the request named it.
	allDay, allDaySent bool
	due, start         timeChange
	loc                *time.Location
}

// timeChange is a due or start of the request: set says it was given, v is
// the new value (nil with set: remove).
type timeChange struct {
	set bool
	v   *taskTime
}

// newTaskUpdate validates the fields that need nothing but the request, in
// create's order (summary, status, priority, percent, due, start).
// storedAllDay is the task's current due_all_day.
func newTaskUpdate(in UpdateTaskInput, storedAllDay bool) (*taskUpdate, error) {
	u := &taskUpdate{
		description: in.Description,
		priority:    in.Priority,
		percent:     in.PercentComplete,
		allDay:      storedAllDay,
	}
	if in.Summary != nil {
		summary, err := taskSummary(*in.Summary)
		if err != nil {
			return nil, err
		}
		u.summary = &summary
	}
	if u.description.Value != nil {
		if err := taskDescription(*u.description.Value); err != nil {
			return nil, err
		}
	}
	if in.Status != nil {
		status, err := taskStatus(*in.Status)
		if err != nil {
			return nil, err
		}
		u.status = status
	}
	if err := checkPriority(in.Priority.Value); err != nil {
		return nil, err
	}
	if err := checkPercent(in.PercentComplete.Value); err != nil {
		return nil, err
	}
	if in.DueAllDay != nil {
		u.allDay, u.allDaySent = *in.DueAllDay, true
	}
	var err error
	if u.due, err = parseTimeChange("due", in.Due, u.allDay); err != nil {
		return nil, err
	}
	if u.start, err = parseTimeChange("start", in.Start, u.allDay); err != nil {
		return nil, err
	}
	u.loc, _ = calendar.LoadEventLocation(in.Timezone)
	return u, nil
}

func parseTimeChange(field string, c Change[string], allDay bool) (timeChange, error) {
	if !c.Set || c.Value == nil {
		return timeChange{set: c.Set}, nil
	}
	v, err := parseTaskTime(field, *c.Value, allDay) // "" is nil: remove
	return timeChange{set: true, v: v}, err
}

// completes reports a request that would complete or close the task: status
// completed or cancelled, or percent_complete 100.
func (u *taskUpdate) completes() bool {
	return u.status == calendar.TaskStatusCompleted || u.status == calendar.TaskStatusCancelled ||
		(u.percent.Value != nil && *u.percent.Value == 100)
}

// apply is the editTask edit: it checks the times against the stored master,
// refuses completing a recurring task, then writes the fields. It returns
// errUnchanged when no property changed.
func (u *taskUpdate) apply(cal *ical.Calendar, todo *ical.Component, stored loadedTask, now time.Time) error {
	if err := u.checkTimes(todo, stored); err != nil {
		return err
	}
	if err := u.checkOverrides(cal, todo); err != nil {
		return err
	}
	if stored.details.Recurring && u.completes() {
		return ErrRecurringNotSupported
	}

	changed := false
	if u.summary != nil {
		changed = setTextProp(todo, ical.PropSummary, *u.summary) || changed
	}
	if u.description.Set {
		if u.description.Value == nil || *u.description.Value == "" {
			changed = delProps(todo, ical.PropDescription) || changed
		} else {
			changed = setTextProp(todo, ical.PropDescription, *u.description.Value) || changed
		}
	}
	changed = u.applyTimes(cal, todo, stored) || changed
	if u.priority.Set {
		changed = setIntChange(todo, ical.PropPriority, u.priority.Value) || changed
	}
	if u.status != "" && u.status != stored.status {
		if u.status == calendar.TaskStatusCompleted {
			markCompleted(todo, now)
		} else {
			if stored.status == calendar.TaskStatusCompleted {
				markUncompleted(todo)
			}
			todo.Props.SetText(ical.PropStatus, u.status)
		}
		changed = true
	}
	// Completing sets PERCENT-COMPLETE:100 (as create does), whatever the
	// request says; any other status leaves the percentage to the request.
	if u.percent.Set && u.status != calendar.TaskStatusCompleted {
		changed = setIntChange(todo, ical.PropPercentComplete, u.percent.Value) || changed
	}

	if !changed {
		return errUnchanged
	}
	return nil
}

// checkTimes refuses a due/start that does not fit the task's other time,
// once the request touches due, start or due_all_day (a title edit is never
// refused for what a client stored). A time the request does not send is
// "kept"; a DURATION standing in for DUE moves with DTSTART and is not
// checked.
//   - DUE and DTSTART must share a value type (RFC 5545 §3.8.2.3, spec §5
//     rule 3): a kept one must be of the request's type when due_all_day is
//     sent or the other time is written by the request.
//   - Due must not be before start when the request writes either.
//   - DTSTART cannot be removed while DUE is a DURATION after it, unless the
//     request also sends due (RFC 5545 requires DTSTART with DURATION).
func (u *taskUpdate) checkTimes(todo *ical.Component, stored loadedTask) error {
	if !u.due.set && !u.start.set && !u.allDaySent {
		return nil
	}
	dueProp := todo.Props.Get(ical.PropDue)
	startProp := todo.Props.Get(ical.PropDateTimeStart)
	dueKept := !u.due.set && dueProp != nil
	startKept := !u.start.set && startProp != nil
	dueNew, startNew := u.due.v != nil, u.start.v != nil

	if u.start.set && !startNew && !u.due.set && dueProp == nil && todo.Props.Get(ical.PropDuration) != nil {
		return invalidInputf("start cannot be removed while the due date is stored as a duration after it: send due as well")
	}
	// Without due_all_day, allDay is a stored DUE's own type (the task's
	// due_all_day is read from DUE first), so a kept DUE only clashes with a
	// due_all_day that was sent. A kept DTSTART can be of the other type in
	// what a client stored; that only matters next to a due written now (a
	// removed due leaves DTSTART alone, whatever its type).
	if dueKept && isDateProp(dueProp) != u.allDay {
		return valueTypeClash("due", u.allDay)
	}
	if startKept && (u.allDaySent || dueNew) && isDateProp(startProp) != u.allDay {
		return valueTypeClash("start", u.allDay)
	}

	if !dueNew && !startNew {
		return nil
	}
	due, start := u.due.v, u.start.v
	if dueKept && stored.dueAt != nil {
		due = &taskTime{t: *stored.dueAt}
	}
	if startKept && stored.details.Start != nil {
		start = &taskTime{t: *stored.details.Start}
	}
	return checkDueAfterStart(due, start)
}

// checkOverrides refuses turning the DTSTART of a repeating master that has
// RECURRENCE-ID overrides from a date into a date-time or back: RFC 5545
// §3.8.4.4 gives a RECURRENCE-ID the value type of the master's DTSTART, so
// the overrides would be orphaned and clients drop them.
func (u *taskUpdate) checkOverrides(cal *ical.Calendar, todo *ical.Component) error {
	startProp := todo.Props.Get(ical.PropDateTimeStart)
	if !u.start.set || u.start.v == nil || startProp == nil || isDateProp(startProp) == u.start.v.allDay {
		return nil
	}
	for _, child := range cal.Children {
		if child != todo && child.Name == ical.CompToDo && child.Props.Get(ical.PropRecurrenceID) != nil {
			return invalidInputf("start cannot change between a date and a date-time while the repeating task has changed occurrences")
		}
	}
	return nil
}

// applyTimes writes the due and start of the request and reports whether it
// changed anything. A value equal to the stored one (same instant, same value
// type) keeps the stored property, TZID included.
func (u *taskUpdate) applyTimes(cal *ical.Calendar, todo *ical.Component, stored loadedTask) bool {
	changed := false
	if u.start.set {
		switch {
		case u.start.v == nil:
			changed = delProps(todo, ical.PropDateTimeStart) || changed
		case todo.Props.Get(ical.PropDateTimeStart) != nil &&
			sameTime(stored.details.Start, stored.details.StartAllDay, u.start.v):
		default:
			writeTaskTime(cal, todo, ical.PropDateTimeStart, u.start.v, u.loc)
			changed = true
		}
	}
	if u.due.set {
		dueProp := todo.Props.Get(ical.PropDue)
		switch {
		case u.due.v == nil:
			changed = delProps(todo, ical.PropDue, ical.PropDuration) || changed
		case dueProp != nil && sameTime(stored.dueAt, isDateProp(dueProp), u.due.v):
		default:
			// DUE replaces a DURATION (RFC 5545 allows only one of them).
			todo.Props.Del(ical.PropDuration)
			writeTaskTime(cal, todo, ical.PropDue, u.due.v, u.loc)
			changed = true
		}
	}
	return changed
}

// sameTime reports whether a stored time (UTC, nil when absent or malformed;
// date says it is a DATE) is the value v.
func sameTime(stored *time.Time, date bool, v *taskTime) bool {
	return stored != nil && date == v.allDay && stored.Equal(v.t)
}

func isDateProp(p *ical.Prop) bool {
	return p.ValueType() == ical.ValueDate
}

// valueTypeClash is the 400 for a kept due/start whose value type is not the
// one the task has after the update.
func valueTypeClash(field string, allDay bool) error {
	if allDay {
		return invalidInputf("due_all_day is true, but the task's %s is a date-time: send %s as a date (YYYY-MM-DD) or null", field, field)
	}
	return invalidInputf("due_all_day is false, but the task's %s is a date: send %s as an RFC 3339 date-time or null", field, field)
}
