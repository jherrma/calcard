package task

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-ical"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

// Building and editing a task's VTODO (spec §5 rules 1-3). Create builds a new
// VCALENDAR with newTaskCalendar; update, complete and uncomplete go through
// editTask, which edits the parsed master (calendar.TaskMaster) in place with
// the setters below and never regenerates it, so a client's VALARM,
// RELATED-TO, RRULE and X- properties survive. Every timestamp the server writes is the caller's clock in UTC:
// go-ical writes the "Z" form only for time.UTC (F04). The update setters
// (setTextProp, setIntChange, delProps) leave a property alone when it
// already says what the request asks, so an unchanged field keeps its stored
// form and an update that changes nothing is detected (errUnchanged).

// newTaskCalendar returns a VCALENDAR (PRODID, VERSION) holding one new VTODO
// with its identity and bookkeeping properties: UID, DTSTAMP, CREATED and
// LAST-MODIFIED (all now, in UTC) and SEQUENCE:0. The caller adds the task's
// fields to the returned VTODO.
func newTaskCalendar(uid string, now time.Time) (*ical.Calendar, *ical.Component) {
	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropProductID, calendar.ProductID)
	cal.Props.SetText(ical.PropVersion, "2.0")

	now = now.UTC()
	todo := ical.NewComponent(ical.CompToDo)
	todo.Props.SetText(ical.PropUID, uid)
	todo.Props.SetDateTime(ical.PropDateTimeStamp, now)
	todo.Props.SetDateTime(ical.PropCreated, now)
	todo.Props.SetDateTime(ical.PropLastModified, now)
	setIntProp(todo, ical.PropSequence, 0)

	cal.Children = append(cal.Children, todo)
	return cal, todo
}

// setTaskTime writes DUE or DTSTART (name) from v: a date as VALUE=DATE; an
// instant as TZID=<loc> when a named zone is given (encodeTaskCalendar adds
// its VTIMEZONE), else in UTC. An RFC 3339 offset is never written as an
// empty TZID or TZID=Local (F22): without a zone the instant is stored as UTC.
func setTaskTime(comp *ical.Component, name string, v *taskTime, loc *time.Location) {
	switch {
	case v.allDay:
		comp.Props.SetDate(name, v.t)
	case loc != nil:
		comp.Props.SetDateTime(name, v.t.In(loc))
	default:
		comp.Props.SetDateTime(name, v.t.UTC())
	}
}

// setIntProp writes an INTEGER property (PRIORITY, PERCENT-COMPLETE,
// SEQUENCE), replacing any value it had.
func setIntProp(comp *ical.Component, name string, n int) {
	prop := ical.NewProp(name)
	prop.Value = strconv.Itoa(n)
	comp.Props.Set(prop)
}

// markCompleted completes the task: STATUS:COMPLETED, COMPLETED at now (UTC)
// and PERCENT-COMPLETE:100.
func markCompleted(comp *ical.Component, now time.Time) {
	comp.Props.SetText(ical.PropStatus, calendar.TaskStatusCompleted)
	comp.Props.SetDateTime(ical.PropCompleted, now.UTC())
	setIntProp(comp, ical.PropPercentComplete, 100)
}

// markUncompleted reopens the task, the inverse of markCompleted: it removes
// COMPLETED, sets STATUS:NEEDS-ACTION and resets PERCENT-COMPLETE to 0 only
// when it says 100 (or more, which reads as 100). Any other percentage is
// kept, and a task that was at 40% before it was completed is not put back
// to 40%: that value is gone once completing wrote 100.
func markUncompleted(comp *ical.Component) {
	comp.Props.Del(ical.PropCompleted)
	comp.Props.SetText(ical.PropStatus, calendar.TaskStatusNeedsAction)
	if p := comp.Props.Get(ical.PropPercentComplete); p != nil {
		if n, err := strconv.Atoi(strings.TrimSpace(p.Value)); err == nil && n >= 100 {
			setIntProp(comp, ical.PropPercentComplete, 0)
		}
	}
}

// editTask edits a stored task in place (spec §5 rule 2), never regenerating
// it: it decodes obj's iCalendar data and hands the master VTODO
// (calendar.TaskMaster) and its calendar to edit, which sets or deletes only
// the properties it is about. Then it bumps the master's SEQUENCE, sets its
// LAST-MODIFIED and DTSTAMP to now (UTC; DTSTAMP is the last revision in the
// store when there is no METHOD, RFC 5545 §3.8.7.2), re-encodes the object,
// re-derives the denormalized columns and gives it a new ETag.
//
// Everything edit does not touch is kept as stored: the other components
// (VTIMEZONEs, RECURRENCE-ID overrides), the master's sub-components (VALARM)
// and its other properties with their parameters — RRULE, RELATED-TO, X- and
// unknown ones alike. The VTIMEZONEs are not synchronised here: an edit that
// writes a TZID time must define its zone itself (writeTaskTime does, with
// addVTimezone, which unlike calendar.SyncVTimezones drops nothing).
//
// An error from edit is returned as is (a validation error stays a 400), and
// so is errUnchanged, which an edit returns when it found nothing to change:
// the caller then answers with the task as stored and writes nothing. obj is
// only changed when everything succeeded; storing it (UpdateCalendarObject)
// is the caller's job.
func editTask(obj *calendar.CalendarObject, now time.Time, edit func(cal *ical.Calendar, todo *ical.Component) error) error {
	cal, err := ical.NewDecoder(strings.NewReader(obj.ICalData)).Decode()
	if err != nil {
		return fmt.Errorf("decode task: %w", err)
	}
	todo := calendar.TaskMaster(cal)
	if todo == nil {
		return errors.New("decode task: the object holds no VTODO")
	}
	if err := edit(cal, todo); err != nil {
		return err
	}

	now = now.UTC()
	setIntProp(todo, ical.PropSequence, taskSequence(todo)+1)
	todo.Props.SetDateTime(ical.PropLastModified, now)
	todo.Props.SetDateTime(ical.PropDateTimeStamp, now)

	var sb strings.Builder
	if err := ical.NewEncoder(&sb).Encode(cal); err != nil {
		return fmt.Errorf("encode task: %w", err)
	}
	edited := *obj
	edited.ICalData = sb.String()
	if err := edited.PopulateDenormFieldsFromICal(); err != nil {
		return fmt.Errorf("derive task columns: %w", err)
	}
	edited.ETag = calendar.NewETag()
	*obj = edited
	return nil
}

// errUnchanged is what an editTask edit returns when the request changes
// nothing in the stored task (spec §3.1 PATCH): no SEQUENCE bump, no new
// ETag, nothing stored, so a form saved without changes wakes no DAV client.
var errUnchanged = errors.New("task unchanged")

// writeTaskTime writes DUE or DTSTART (name) like setTaskTime and, when the
// value went out with a TZID, makes sure cal defines that zone (addVTimezone).
func writeTaskTime(cal *ical.Calendar, comp *ical.Component, name string, v *taskTime, loc *time.Location) {
	setTaskTime(comp, name, v, loc)
	if p := comp.Props.Get(name); p != nil && p.Params.Get(ical.PropTimezoneID) != "" {
		addVTimezone(cal, loc, v.t)
	}
}

// addVTimezone adds a VTIMEZONE for loc (TZID = its name), generated from
// Go's tz data (calendar.GenerateVTimezone, valid from at on), when cal has
// none with that TZID; it goes after the VTIMEZONEs cal already has (first
// when there are none). Unlike calendar.SyncVTimezones it never drops,
// reorders or regenerates a VTIMEZONE the object holds: an edit keeps a
// client's own definitions and the ones nothing references any more (RFC
// 5545 §3.2.19 asks for one per TZID used and forbids no others).
func addVTimezone(cal *ical.Calendar, loc *time.Location, at time.Time) {
	tzid := loc.String()
	insert := 0
	for i, child := range cal.Children {
		if child.Name != ical.CompTimezone {
			continue
		}
		if p := child.Props.Get(ical.PropTimezoneID); p != nil && p.Value == tzid {
			return
		}
		insert = i + 1
	}
	cal.Children = slices.Insert(cal.Children, insert, calendar.GenerateVTimezone(tzid, loc, at))
}

// setTextProp writes a TEXT property (SUMMARY, DESCRIPTION) unless it already
// holds that text, in which case the stored property is kept with its
// parameters (a LANGUAGE, an ALTREP). It reports whether it wrote.
//
// "Holds that text" is compared as plain text (calendar.UnescapeText), the
// form the API shows: a client's sloppy, unescaped "Buy milk, eggs" is the
// same text as "Buy milk\, eggs" and is not rewritten when a form saves the
// title unchanged. Line breaks compare as LF (what calendar.EscapeText writes
// for a CRLF).
func setTextProp(comp *ical.Component, name, text string) bool {
	want := calendar.NewTextProp(name, text)
	if p := comp.Props.Get(name); p != nil && calendar.UnescapeText(p.Value) == calendar.UnescapeText(want.Value) {
		return false
	}
	comp.Props.Set(want)
	return true
}

// setIntChange writes an INTEGER property (PRIORITY, PERCENT-COMPLETE) unless
// it already holds that number; a nil n removes it. It reports whether it
// changed the component.
func setIntChange(comp *ical.Component, name string, n *int) bool {
	p := comp.Props.Get(name)
	if n == nil {
		return delProps(comp, name)
	}
	if p != nil {
		if v, err := strconv.Atoi(strings.TrimSpace(p.Value)); err == nil && v == *n {
			return false
		}
	}
	setIntProp(comp, name, *n)
	return true
}

// setPriorityChange is setIntChange for PRIORITY, comparing what the API
// shows: PRIORITY 0 ("undefined", RFC 5545 §3.8.1.9) or no PRIORITY at all is
// a null priority, so a request for none (null or 0) on such a task changes
// nothing, where a literal compare would write a property (or remove PRIORITY:0)
// and bump SEQUENCE and the ETag.
func setPriorityChange(comp *ical.Component, n *int) bool {
	if (n == nil || *n == 0) && calendar.TaskPriority(comp) == nil {
		return false
	}
	return setIntChange(comp, ical.PropPriority, n)
}

// delProps removes the named properties and reports whether any was there.
func delProps(comp *ical.Component, names ...string) bool {
	removed := false
	for _, name := range names {
		if comp.Props.Get(name) != nil {
			comp.Props.Del(name)
			removed = true
		}
	}
	return removed
}

// taskSequence is the component's SEQUENCE, 0 when it is absent (the RFC 5545
// default), not an integer or negative.
func taskSequence(comp *ical.Component) int {
	p := comp.Props.Get(ical.PropSequence)
	if p == nil {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(p.Value))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// encodeTaskCalendar adds a VTIMEZONE for every TZID the object uses (and
// drops unused ones, calendar.SyncVTimezones, RFC 5545 §3.2.19) and encodes
// it.
func encodeTaskCalendar(cal *ical.Calendar) (string, error) {
	calendar.SyncVTimezones(cal)
	var sb strings.Builder
	if err := ical.NewEncoder(&sb).Encode(cal); err != nil {
		return "", err
	}
	return sb.String(), nil
}
