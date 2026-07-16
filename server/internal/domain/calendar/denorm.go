package calendar

import (
	"fmt"
	"strings"
	"time"

	"github.com/emersion/go-ical"
	"github.com/teambition/rrule-go"
)

// farFuture is the RecurrenceEndTime assigned to unbounded recurring series (or
// series whose RRULE cannot be parsed) so they always match listing windows.
var farFuture = time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)

// PopulateDenormFieldsFromICal re-derives every denormalized column on the
// CalendarObject from its ICalData. It is the single source of truth used by
// all write paths (REST create/update, DAV PUT, import) so the denormalized
// fields never drift between paths. It does NOT touch ETag — callers bump that
// explicitly via NewETag().
//
// Returns an error when ICalData is not a decodable VCALENDAR containing at
// least one VEVENT/VTODO component.
func (o *CalendarObject) PopulateDenormFieldsFromICal() error {
	cal, err := ical.NewDecoder(strings.NewReader(o.ICalData)).Decode()
	if err != nil {
		return fmt.Errorf("failed to parse iCalendar data: %w", err)
	}

	o.ContentLength = len(o.ICalData)

	var comps []*ical.Component
	for _, child := range cal.Children {
		if child.Name == ical.CompEvent || child.Name == ical.CompToDo {
			comps = append(comps, child)
		}
	}
	if len(comps) == 0 {
		return fmt.Errorf("iCalendar data contains no VEVENT or VTODO component")
	}

	o.ComponentType = comps[0].Name

	// Masters = components without a RECURRENCE-ID (fall back to all if every
	// component is an override, which shouldn't normally happen).
	var masters []*ical.Component
	for _, c := range comps {
		if c.Props.Get(ical.PropRecurrenceID) == nil {
			masters = append(masters, c)
		}
	}
	if len(masters) == 0 {
		masters = comps
	}

	primary := masters[0]
	o.Summary = propValue(primary, ical.PropSummary)
	o.Description = propValue(primary, ical.PropDescription)
	o.Location = propValue(primary, ical.PropLocation)
	if o.UID == "" {
		o.UID = propValue(primary, ical.PropUID)
	}

	// Earliest start / latest end across masters; nil when absent.
	var start, end *time.Time
	for _, c := range masters {
		if p := c.Props.Get(ical.PropDateTimeStart); p != nil {
			if t, err := p.DateTime(time.UTC); err == nil {
				if start == nil || t.Before(*start) {
					tt := t
					start = &tt
				}
			}
		}
		if t, ok := componentEnd(c); ok {
			if end == nil || t.After(*end) {
				tt := t
				end = &tt
			}
		}
	}
	o.StartTime = start
	o.EndTime = end

	o.IsAllDay = false
	if p := primary.Props.Get(ical.PropDateTimeStart); p != nil {
		if p.Params.Get(ical.ParamValue) == string(ical.ValueDate) {
			o.IsAllDay = true
		}
	}

	o.RecurrenceEndTime = recurrenceEndTime(masters)

	return nil
}

// recurrenceEndTime computes the latest instant any recurring master produces an
// occurrence end. Returns nil when no master carries an RRULE.
func recurrenceEndTime(masters []*ical.Component) *time.Time {
	var result *time.Time
	for _, c := range masters {
		rruleProp := c.Props.Get(ical.PropRecurrenceRule)
		if rruleProp == nil {
			continue
		}

		startProp := c.Props.Get(ical.PropDateTimeStart)
		if startProp == nil {
			continue
		}
		mStart, err := startProp.DateTime(time.UTC)
		if err != nil {
			continue
		}
		dur := time.Duration(0)
		if mEnd, ok := componentEnd(c); ok {
			if d := mEnd.Sub(mStart); d > 0 {
				dur = d
			}
		}

		var candidate time.Time
		rule, err := rrule.StrToRRule(rruleProp.Value)
		if err != nil {
			candidate = farFuture
		} else {
			rule.DTStart(mStart)
			// Inspect OrigOptions (the parsed input) rather than GetUntil():
			// rrule-go synthesizes a ~292-year UNTIL for unbounded rules, which
			// we must treat as far-future, not a real bound.
			switch {
			case rule.OrigOptions.Count > 0:
				all := rule.All()
				if len(all) > 0 {
					candidate = all[len(all)-1].Add(dur)
				} else {
					candidate = mStart.Add(dur)
				}
			case !rule.OrigOptions.Until.IsZero():
				candidate = rule.OrigOptions.Until.Add(dur)
			default:
				candidate = farFuture
			}
		}

		if result == nil || candidate.After(*result) {
			c := candidate
			result = &c
		}
	}
	return result
}

// componentEnd resolves the non-inclusive end instant of a VEVENT/VTODO using
// go-ical's full RFC 5545 semantics: a literal DTEND, else DTSTART+DURATION,
// else the one-day default for an all-day (VALUE=DATE) DTSTART. For a VTODO
// that carries only DUE (no DTEND/DTSTART/DURATION) it falls back to DUE. The
// bool is false when no end can be derived, so callers keep EndTime nil rather
// than fabricating one.
func componentEnd(c *ical.Component) (time.Time, bool) {
	ev := &ical.Event{Component: c}
	if t, err := ev.DateTimeEnd(time.UTC); err == nil && !t.IsZero() {
		return t, true
	}
	if c.Name == ical.CompToDo {
		if p := c.Props.Get(ical.PropDue); p != nil {
			if t, err := p.DateTime(time.UTC); err == nil {
				return t, true
			}
		}
	}
	return time.Time{}, false
}

func propValue(c *ical.Component, name string) string {
	if p := c.Props.Get(name); p != nil {
		return p.Value
	}
	return ""
}
