package webdav

import (
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav/caldav"
)

// VTODO time-range matching for calendar-query REPORTs (#170).
//
// Thunderbird and DAVx5 ask for due-date views ("Next 7 days") with
// <comp-filter name="VTODO"><time-range …/></comp-filter>. go-webdav v0.7.0
// (caldav/match.go matchCompTimeRange) gets that wrong in both directions: a
// non-recurring VTODO never matches (it only knows VEVENT), and a recurring one
// is expanded with go-ical's RecurrenceSet, which anchors on DTSTART — the zero
// time for a task that has only DUE (Tasks.org), so every REPORT iterated each
// such series from year 1.
//
// So a VTODO comp-filter that carries a time-range (directly under the
// VCALENDAR filter) is decided here, never by go-webdav:
//   - a non-recurring VTODO, and every RECURRENCE-ID override, by the RFC 4791
//     §9.9 VTODO table over DTSTART, DURATION, DUE, COMPLETED and CREATED;
//   - a recurring master (RRULE or RDATE) by the overlap of its span — from
//     DTSTART, else DUE, to the denormalised RecurrenceEndTime plus the
//     DTSTART..DUE length — with the window. A nil RecurrenceEndTime (no
//     DTSTART, or a value that never parsed) counts as unbounded. This
//     over-includes a little, which is safe (a client re-filters), and it keeps
//     a repeating task whose first due date is long past in a future window.
//     RecurrenceSet is never called on a VTODO, so nothing is expanded.
//
// Every other condition of the query — other comp-filters (VEVENT time-ranges
// included), prop-filters, text-match, and the VTODO filter's own prop-filters
// and nested comp-filters, which must hold for the same component — is still
// evaluated by go-webdav's caldav.Match, and a query without a VTODO time-range
// goes to caldav.Filter untouched.
//
// Times: a VTODO time-range without "start" is open towards the past, one
// without "end" towards the future (RFC 4791 §9.9; go-webdav ignores a
// time-range that has no "start", which stays so for VEVENT). DATE and
// floating values are read as UTC, as the task columns do (spec R3); a value
// that does not parse (an unknown TZID too) counts as absent, as does a
// negative DURATION or a DURATION without DTSTART.

// queryCandidate is one member of the queried collection: the decoded object
// go-webdav serves plus the denormalised end of its recurrence set.
type queryCandidate struct {
	object        caldav.CalendarObject
	recurrenceEnd *time.Time
}

// filterCalendarQuery returns the candidates that match query, in their order.
func filterCalendarQuery(query *caldav.CalendarQuery, candidates []queryCandidate) ([]caldav.CalendarObject, error) {
	if query == nil || !hasVTODOTimeRange(query.CompFilter) {
		objects := make([]caldav.CalendarObject, len(candidates))
		for i := range candidates {
			objects[i] = candidates[i].object
		}
		return caldav.Filter(query, objects)
	}

	rest, vtodoFilters := splitVTODOTimeRanges(query.CompFilter)
	var out []caldav.CalendarObject
	for i := range candidates {
		ok, err := matchCandidate(rest, vtodoFilters, &candidates[i])
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, candidates[i].object)
		}
	}
	return out, nil
}

func hasTimeRange(filter caldav.CompFilter) bool {
	return !filter.Start.IsZero() || !filter.End.IsZero()
}

// hasVTODOTimeRange reports whether the query's VCALENDAR filter holds a VTODO
// comp-filter with a time-range.
func hasVTODOTimeRange(filter caldav.CompFilter) bool {
	if filter.Name != ical.CompCalendar {
		return false
	}
	for _, child := range filter.Comps {
		if child.Name == ical.CompToDo && hasTimeRange(child) {
			return true
		}
	}
	return false
}

// splitVTODOTimeRanges returns the VCALENDAR filter without its VTODO
// time-range comp-filters, and those comp-filters.
func splitVTODOTimeRanges(filter caldav.CompFilter) (caldav.CompFilter, []caldav.CompFilter) {
	rest := filter
	rest.Comps = nil
	var vtodo []caldav.CompFilter
	for _, child := range filter.Comps {
		if child.Name == ical.CompToDo && hasTimeRange(child) {
			vtodo = append(vtodo, child)
		} else {
			rest.Comps = append(rest.Comps, child)
		}
	}
	return rest, vtodo
}

// matchCandidate ANDs go-webdav's verdict on everything but the VTODO
// time-ranges with each VTODO time-range filter.
func matchCandidate(rest caldav.CompFilter, vtodoFilters []caldav.CompFilter, candidate *queryCandidate) (bool, error) {
	ok, err := caldav.Match(rest, &candidate.object)
	if err != nil || !ok {
		return false, err
	}
	for _, filter := range vtodoFilters {
		ok, err := matchVTODOFilter(filter, candidate)
		if err != nil || !ok {
			return false, err
		}
	}
	return true, nil
}

// matchVTODOFilter reports whether one VTODO of the object lies in the
// filter's time-range and also meets the filter's other conditions.
func matchVTODOFilter(filter caldav.CompFilter, candidate *queryCandidate) (bool, error) {
	window := timeWindow{start: filter.Start, end: filter.End}
	others := filter
	others.Start, others.End = time.Time{}, time.Time{}
	for _, comp := range candidate.object.Data.Children {
		if comp.Name != ical.CompToDo || !vtodoInWindow(comp, window, candidate.recurrenceEnd) {
			continue
		}
		ok, err := caldav.Match(others, &caldav.CalendarObject{Data: &ical.Calendar{Component: comp}})
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

// vtodoInWindow decides the time-range for one VTODO component.
func vtodoInWindow(comp *ical.Component, window timeWindow, recurrenceEnd *time.Time) bool {
	times := readVTODOTimes(comp)
	if times.recurring && comp.Props.Get(ical.PropRecurrenceID) == nil {
		first := times.dtstart
		if first == nil {
			first = times.due
		}
		if first != nil {
			if recurrenceEnd == nil {
				return window.endGE(*first)
			}
			last := *recurrenceEnd
			if times.dtstart != nil && times.due != nil && times.due.After(*times.dtstart) {
				// RecurrenceEndTime is the last occurrence's DTSTART (go-ical's
				// end of a task ignores DUE); its due date comes this much later.
				last = last.Add(times.due.Sub(*times.dtstart))
			}
			return window.endGE(*first) && window.startLE(last)
		}
		// Neither DTSTART nor DUE: the series has no anchor; use the table.
	}
	return window.overlapsVTODO(times)
}

// vtodoTimes are the properties the RFC 4791 §9.9 VTODO table looks at, as UTC
// instants; nil when absent or unparseable.
type vtodoTimes struct {
	dtstart, due, completed, created *time.Time
	duration                         *time.Duration
	recurring                        bool
}

func readVTODOTimes(comp *ical.Component) vtodoTimes {
	instant := func(name string) *time.Time {
		p := comp.Props.Get(name)
		if p == nil {
			return nil
		}
		value, err := p.DateTime(time.UTC)
		if err != nil {
			return nil
		}
		value = value.UTC()
		return &value
	}
	times := vtodoTimes{
		dtstart:   instant(ical.PropDateTimeStart),
		due:       instant(ical.PropDue),
		completed: instant(ical.PropCompleted),
		created:   instant(ical.PropCreated),
		recurring: comp.Props.Get(ical.PropRecurrenceRule) != nil || comp.Props.Get(ical.PropRecurrenceDates) != nil,
	}
	if p := comp.Props.Get(ical.PropDuration); p != nil && times.dtstart != nil {
		if duration, err := p.Duration(); err == nil && duration >= 0 {
			times.duration = &duration
		}
	}
	return times
}

// timeWindow is a time-range's [start, end); a zero bound is open.
type timeWindow struct {
	start, end time.Time
}

func (w timeWindow) startLT(t time.Time) bool { return w.start.IsZero() || w.start.Before(t) } // start <  t
func (w timeWindow) startLE(t time.Time) bool { return w.start.IsZero() || !w.start.After(t) } // start <= t
func (w timeWindow) endGT(t time.Time) bool   { return w.end.IsZero() || w.end.After(t) }      // end   >  t
func (w timeWindow) endGE(t time.Time) bool   { return w.end.IsZero() || !w.end.Before(t) }    // end   >= t

// overlapsVTODO is the RFC 4791 §9.9 VTODO table, row by row. A DUE next to a
// DURATION (RFC 5545 forbids both) is read as DTSTART+DUE: DUE wins, as it
// does for the task columns.
func (w timeWindow) overlapsVTODO(times vtodoTimes) bool {
	switch {
	case times.dtstart != nil && times.due != nil: // DTSTART Y, DURATION N, DUE Y
		return (w.startLT(*times.due) || w.startLE(*times.dtstart)) &&
			(w.endGT(*times.dtstart) || w.endGE(*times.due))
	case times.dtstart != nil && times.duration != nil: // DTSTART Y, DURATION Y, DUE N
		end := times.dtstart.Add(*times.duration)
		return w.startLE(end) && (w.endGT(*times.dtstart) || w.endGE(end))
	case times.dtstart != nil: // DTSTART Y, DURATION N, DUE N
		return w.startLE(*times.dtstart) && w.endGT(*times.dtstart)
	case times.due != nil: // DTSTART N, DURATION N, DUE Y
		return w.startLT(*times.due) && w.endGE(*times.due)
	case times.completed != nil && times.created != nil: // COMPLETED Y, CREATED Y
		return (w.startLE(*times.created) || w.startLE(*times.completed)) &&
			(w.endGE(*times.created) || w.endGE(*times.completed))
	case times.completed != nil: // COMPLETED Y, CREATED N
		return w.startLE(*times.completed) && w.endGE(*times.completed)
	case times.created != nil: // COMPLETED N, CREATED Y
		return w.endGT(*times.created)
	default: // none of them
		return true
	}
}
