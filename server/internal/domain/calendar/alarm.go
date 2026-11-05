package calendar

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-ical"
)

// VALARM parsing and trigger computation (#169, spec §6). Pure domain code: no
// I/O, no errors out of ParseAlarms, never a failed write. A VALARM the parser
// cannot read is returned with Valid=false rather than dropped, so the API can
// show it read-only.

// maxAlarmOffsetSeconds is the 400-day magnitude cap of a trigger duration; it
// keeps every later time.Duration arithmetic far from overflow.
const maxAlarmOffsetSeconds = int64(400 * 86400)

// AlarmOffset is a parsed trigger duration: nominal days (D and W, in the
// anchor's zone) plus exact seconds (H, M, S). Neg applies to both.
type AlarmOffset struct {
	Neg  bool
	Days int
	Secs int64
}

// AlarmSpec is one VALARM child of a VEVENT or VTODO.
type AlarmSpec struct {
	Index       int         // 0-based position among the component's VALARM children
	Action      string      // upper-cased ACTION; "" when absent
	Related     string      // "START" or "END" (relative triggers only)
	Relative    bool        // TRIGGER is a duration
	Offset      AlarmOffset // relative only
	Absolute    *time.Time  // absolute trigger as stored; UTC (a wall clock in UTC when Floating)
	Floating    bool        // absolute trigger without Z/TZID
	Repeat      bool        // REPEAT present (delivered once, never editable)
	Snooze      bool        // RELATED-TO;RELTYPE=SNOOZE present (RFC 9074)
	Valid       bool        // TRIGGER parsed
	Deliverable bool        // Valid && action in the delivered set (§6.1)
}

// TriggerBase is the anchor of a relative trigger. When Wall is false At is an
// instant in its own zone (UTC for a `Z` value, the TZID's zone otherwise) and
// the duration's days are nominal days in that zone. When Wall is true the value
// is floating or a DATE and At holds its wall clock in UTC (spec R3); the whole
// duration is then applied to the wall clock and the result is placed in the
// owner's zone.
type TriggerBase struct {
	At   time.Time
	Wall bool
}

// ParseAlarms reads the VALARM children of a VEVENT or VTODO. It never fails.
func ParseAlarms(comp *ical.Component) []AlarmSpec {
	if comp == nil {
		return nil
	}
	var out []AlarmSpec
	for _, child := range comp.Children {
		if child == nil || child.Name != ical.CompAlarm {
			continue
		}
		s := parseAlarm(child)
		s.Index = len(out)
		out = append(out, s)
	}
	return out
}

func parseAlarm(a *ical.Component) AlarmSpec {
	s := AlarmSpec{}
	if p := a.Props.Get(ical.PropAction); p != nil {
		s.Action = strings.ToUpper(strings.TrimSpace(p.Value))
	}
	s.Repeat = a.Props.Get(ical.PropRepeat) != nil
	for _, p := range a.Props[ical.PropRelatedTo] {
		if strings.EqualFold(strings.TrimSpace(p.Params.Get("RELTYPE")), "SNOOZE") {
			s.Snooze = true
		}
	}
	if p := a.Props.Get(ical.PropTrigger); p != nil {
		parseTrigger(p, &s)
	}
	s.Deliverable = s.Valid && (s.Action == "" || s.Action == "DISPLAY" || s.Action == "AUDIO")
	return s
}

func parseTrigger(p *ical.Prop, s *AlarmSpec) {
	val := strings.TrimSpace(p.Value)
	switch strings.ToUpper(strings.TrimSpace(p.Params.Get(ical.ParamValue))) {
	case "DATE":
		return
	case "DATE-TIME":
		parseAbsoluteTrigger(p, val, s)
		return
	case "", "DURATION":
	default:
		return
	}
	if looksLikeDuration(val) {
		related := strings.ToUpper(strings.TrimSpace(p.Params.Get(ical.ParamRelated)))
		switch related {
		case "":
			related = "START"
		case "START", "END":
		default:
			return
		}
		neg, days, secs, err := parseAlarmDuration(val)
		if err != nil {
			return
		}
		s.Relative = true
		s.Related = related
		s.Offset = AlarmOffset{Neg: neg, Days: days, Secs: secs}
		s.Valid = true
		return
	}
	// No VALUE and not a duration: accept a date-time leniently.
	if strings.EqualFold(p.Params.Get(ical.ParamValue), "DURATION") {
		return
	}
	parseAbsoluteTrigger(p, val, s)
}

func looksLikeDuration(v string) bool {
	v = strings.TrimLeft(v, "+-")
	return strings.HasPrefix(v, "P") || strings.HasPrefix(v, "p")
}

func parseAbsoluteTrigger(p *ical.Prop, val string, s *AlarmSpec) {
	const layout = "20060102T150405"
	var t time.Time
	var err error
	floating := false
	switch {
	case len(val) == len(layout)+1 && (strings.HasSuffix(val, "Z") || strings.HasSuffix(val, "z")):
		t, err = time.ParseInLocation(layout, val[:len(layout)], time.UTC)
	case len(val) == len(layout):
		if tzid := p.Params.Get(ical.ParamTimezoneID); tzid != "" {
			loc, lerr := time.LoadLocation(tzid)
			if lerr != nil || loc == time.Local {
				return
			}
			t, err = time.ParseInLocation(layout, val, loc)
		} else {
			t, err = time.ParseInLocation(layout, val, time.UTC)
			floating = true
		}
	default:
		return
	}
	if err != nil {
		return
	}
	t = t.UTC()
	s.Absolute = &t
	s.Floating = floating
	s.Valid = true
}

var errAlarmDuration = errors.New("calendar: invalid alarm duration")

// parseAlarmDuration parses an RFC 5545 §3.3.6 duration, case-insensitively,
// as `[+|-]P(nW | nD[T…] | T nH nM nS)` and also `nWnD`. A bare P, a T without
// an element, fractions, units out of order and more than 400 days are errors.
func parseAlarmDuration(in string) (neg bool, days int, secs int64, err error) {
	s := strings.ToUpper(strings.TrimSpace(in))
	if s != "" && (s[0] == '+' || s[0] == '-') {
		neg = s[0] == '-'
		s = s[1:]
	}
	if s == "" || s[0] != 'P' {
		return false, 0, 0, errAlarmDuration
	}
	s = s[1:]
	if s == "" {
		return false, 0, 0, errAlarmDuration
	}
	datePart, timePart := s, ""
	hasT := false
	if i := strings.IndexByte(s, 'T'); i >= 0 {
		datePart, timePart, hasT = s[:i], s[i+1:], true
	}
	var d int64
	elems := 0
	rank := 0
	scan := func(part string, order string, mult []int64) (int64, error) {
		var total int64
		for part != "" {
			i := 0
			for i < len(part) && part[i] >= '0' && part[i] <= '9' {
				i++
			}
			if i == 0 || i > 9 || i == len(part) {
				return 0, errAlarmDuration
			}
			n, perr := strconv.ParseInt(part[:i], 10, 64)
			if perr != nil {
				return 0, errAlarmDuration
			}
			idx := strings.IndexByte(order, part[i])
			if idx < 0 || idx < rank {
				return 0, errAlarmDuration
			}
			rank = idx + 1
			total += n * mult[idx]
			elems++
			part = part[i+1:]
		}
		return total, nil
	}
	d, err = scan(datePart, "WD", []int64{7, 1})
	if err != nil {
		return false, 0, 0, err
	}
	rank = 0
	var sec int64
	if hasT {
		before := elems
		sec, err = scan(timePart, "HMS", []int64{3600, 60, 1})
		if err != nil || elems == before {
			return false, 0, 0, errAlarmDuration
		}
	}
	if elems == 0 || d > 400 || sec > maxAlarmOffsetSeconds || d*86400+sec > maxAlarmOffsetSeconds {
		return false, 0, 0, errAlarmDuration
	}
	return neg, int(d), sec, nil
}

// TriggerAt computes the trigger instant in UTC at second precision. start and
// end are the anchors of RELATED=START/END (nil when the component has none);
// ownerLoc places floating and DATE values (nil means UTC). The bool is false
// for an invalid alarm or a relative one without its anchor.
func (a AlarmSpec) TriggerAt(start, end *TriggerBase, ownerLoc *time.Location) (time.Time, bool) {
	if !a.Valid {
		return time.Time{}, false
	}
	if ownerLoc == nil {
		ownerLoc = time.UTC
	}
	if !a.Relative {
		if a.Absolute == nil {
			return time.Time{}, false
		}
		if a.Floating {
			return placeWall(*a.Absolute, ownerLoc), true
		}
		return a.Absolute.UTC().Truncate(time.Second), true
	}
	base := start
	if a.Related == "END" {
		base = end
	}
	if base == nil || base.At.IsZero() {
		return time.Time{}, false
	}
	sign := 1
	if a.Offset.Neg {
		sign = -1
	}
	dur := time.Duration(sign) * time.Duration(a.Offset.Secs) * time.Second
	if base.Wall {
		w := base.At.UTC().AddDate(0, 0, sign*a.Offset.Days).Add(dur)
		return placeWall(w, ownerLoc), true
	}
	loc := base.At.Location()
	return base.At.In(loc).AddDate(0, 0, sign*a.Offset.Days).Add(dur).UTC().Truncate(time.Second), true
}

// placeWall reads w's wall clock and resolves it in loc; a wall time in a DST
// gap or overlap resolves as time.Date does.
func placeWall(w time.Time, loc *time.Location) time.Time {
	return time.Date(w.Year(), w.Month(), w.Day(), w.Hour(), w.Minute(), w.Second(), 0, loc).UTC()
}

// ComponentTriggerBases returns the START and END anchors of a non-expanded
// VEVENT or VTODO (§6.4). Either is nil when unusable. For a VTODO the END is
// DUE, never componentEnd (which returns DTSTART when both exist).
func ComponentTriggerBases(comp *ical.Component) (start, end *TriggerBase) {
	if comp == nil {
		return nil, nil
	}
	dtstart, startIsDate := propTriggerBase(comp.Props.Get(ical.PropDateTimeStart))
	if comp.Name == ical.CompToDo {
		due, _ := propTriggerBase(comp.Props.Get(ical.PropDue))
		start = dtstart
		if start == nil {
			start = due
		}
		end = due
		if end == nil {
			end = addDurationProp(dtstart, comp)
		}
		if end == nil {
			end = dtstart
		}
		return start, end
	}
	start = dtstart
	if dtend, _ := propTriggerBase(comp.Props.Get(ical.PropDateTimeEnd)); dtend != nil {
		return start, dtend
	}
	if e := addDurationProp(dtstart, comp); e != nil {
		return start, e
	}
	if dtstart != nil && startIsDate {
		e := *dtstart
		e.At = e.At.AddDate(0, 0, 1)
		return start, &e
	}
	return start, dtstart
}

// addDurationProp returns base plus the component's DURATION, or nil.
func addDurationProp(base *TriggerBase, comp *ical.Component) *TriggerBase {
	p := comp.Props.Get(ical.PropDuration)
	if base == nil || p == nil {
		return nil
	}
	neg, days, secs, err := parseAlarmDuration(p.Value)
	if err != nil {
		return nil
	}
	sign := 1
	if neg {
		sign = -1
	}
	e := *base
	e.At = base.At.AddDate(0, 0, sign*days).Add(time.Duration(sign) * time.Duration(secs) * time.Second)
	return &e
}

// propTriggerBase reads a DATE or DATE-TIME property as an anchor; the bool
// reports a DATE value. nil when absent or unparseable (an unresolvable TZID
// included).
func propTriggerBase(p *ical.Prop) (*TriggerBase, bool) {
	if p == nil {
		return nil, false
	}
	const dateLayout, dtLayout = "20060102", "20060102T150405"
	v := strings.TrimSpace(p.Value)
	isDate := strings.EqualFold(p.Params.Get(ical.ParamValue), "DATE") || len(v) == len(dateLayout)
	if isDate {
		t, err := time.ParseInLocation(dateLayout, v, time.UTC)
		if err != nil {
			return nil, false
		}
		return &TriggerBase{At: t, Wall: true}, true
	}
	switch {
	case len(v) == len(dtLayout)+1 && (v[len(v)-1] == 'Z' || v[len(v)-1] == 'z'):
		t, err := time.ParseInLocation(dtLayout, v[:len(dtLayout)], time.UTC)
		if err != nil {
			return nil, false
		}
		return &TriggerBase{At: t}, false
	case len(v) == len(dtLayout):
		if tzid := p.Params.Get(ical.ParamTimezoneID); tzid != "" {
			loc, err := time.LoadLocation(tzid)
			if err != nil || loc == time.Local {
				return nil, false
			}
			t, err := time.ParseInLocation(dtLayout, v, loc)
			if err != nil {
				return nil, false
			}
			return &TriggerBase{At: t}, false
		}
		t, err := time.ParseInLocation(dtLayout, v, time.UTC)
		if err != nil {
			return nil, false
		}
		return &TriggerBase{At: t, Wall: true}, false
	}
	return nil, false
}
