package calendar

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-ical"
)

// A generated VTIMEZONE always describes the same fixed window, whatever the
// event's dates: every object of a zone then carries the identical block, so
// an export that keeps one VTIMEZONE per TZID (ConcatObjectsDedupVTimezones)
// is right for all of them. The window starts earlier for an event before
// 1970. Inside the window the result reproduces Go's zone data to the second
// (checked against every tzdata zone). Beyond its end, observances that follow
// a yearly rule are left open-ended, which tzdata's own rules are by then; the
// one known approximation is a rule RRULE cannot state, such as Cairo's "last
// Thursday of October, 24:00", which after 2100 lands a day off when that
// Thursday is 31 October.
var (
	vtimezoneWindowStart = time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	vtimezoneWindowEnd   = time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)
)

// maxZoneSteps bounds the walk over a zone's transitions; the busiest zones
// have a few hundred in the window.
const maxZoneSteps = 10000

const icalLocalLayout = "20060102T150405"

// LoadEventLocation resolves an IANA zone name for an event's date-times. The
// name "Local" is refused: it is the server's own zone, so a TZID=Local would
// change meaning with the host's configuration.
func LoadEventLocation(name string) (*time.Location, bool) {
	if name == "" || name == "Local" {
		return nil, false
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, false
	}
	return loc, true
}

// SyncVTimezones makes the VTIMEZONE components of cal match the TZIDs its
// other components reference (RFC 5545 §3.2.19: every TZID parameter names a
// VTIMEZONE in the same object). A referenced zone without a VTIMEZONE gets one
// generated from Go's tz data; one that exists is kept (a client's definition
// wins), unless it starts after the earliest date-time that uses it and the
// zone is known, in which case it is regenerated. VTIMEZONEs nothing
// references any more are dropped. The VTIMEZONEs come first, sorted by TZID,
// so the encoding is stable. A TZID that is neither defined nor a known zone
// name is left as it is: there is nothing to define it with.
func SyncVTimezones(cal *ical.Calendar) {
	earliest := make(map[string]time.Time)
	var others []*ical.Component
	existing := make(map[string]*ical.Component)
	for _, child := range cal.Children {
		if child.Name != ical.CompTimezone {
			others = append(others, child)
			collectTimezoneRefs(child, earliest)
			continue
		}
		if p := child.Props.Get(ical.PropTimezoneID); p != nil && p.Value != "" {
			if _, dup := existing[p.Value]; !dup {
				existing[p.Value] = child
			}
		}
	}

	tzids := make([]string, 0, len(earliest))
	for tzid := range earliest {
		tzids = append(tzids, tzid)
	}
	sort.Strings(tzids)

	children := make([]*ical.Component, 0, len(tzids)+len(others))
	for _, tzid := range tzids {
		comp := existing[tzid]
		if loc, ok := LoadEventLocation(tzid); ok && (comp == nil || startsAfter(comp, earliest[tzid], loc)) {
			comp = GenerateVTimezone(tzid, loc, earliest[tzid])
		}
		if comp != nil {
			children = append(children, comp)
		}
	}
	cal.Children = append(children, others...)
}

// collectTimezoneRefs records, for every TZID a date-time property of comp (or
// of a sub-component such as VALARM) names, the earliest instant it is used
// for. A value that cannot be read still marks the zone as referenced.
func collectTimezoneRefs(comp *ical.Component, earliest map[string]time.Time) {
	for _, props := range comp.Props {
		for _, prop := range props {
			tzid := prop.Params.Get(ical.PropTimezoneID)
			if tzid == "" {
				continue
			}
			first, seen := earliest[tzid]
			if !seen {
				earliest[tzid] = time.Time{}
			}
			loc, ok := LoadEventLocation(tzid)
			if !ok {
				continue
			}
			for _, v := range strings.Split(prop.Value, ",") {
				// An RDATE may be a PERIOD (start/end); its start is enough.
				v, _, _ = strings.Cut(strings.TrimSpace(v), "/")
				t, err := time.ParseInLocation(icalLocalLayout, v, loc)
				if err != nil {
					continue
				}
				if first.IsZero() || t.Before(first) {
					first = t
				}
			}
			earliest[tzid] = first
		}
	}
	for _, child := range comp.Children {
		collectTimezoneRefs(child, earliest)
	}
}

// startsAfter reports whether the first observance of an existing VTIMEZONE
// begins after t, i.e. leaves t (in loc) undefined. An unreadable block counts
// as starting after everything.
func startsAfter(vtz *ical.Component, t time.Time, loc *time.Location) bool {
	if t.IsZero() {
		return false
	}
	var onset time.Time
	for _, obs := range vtz.Children {
		p := obs.Props.Get(ical.PropDateTimeStart)
		if p == nil {
			continue
		}
		o, err := time.ParseInLocation(icalLocalLayout, p.Value, time.UTC)
		if err != nil {
			continue
		}
		if onset.IsZero() || o.Before(onset) {
			onset = o
		}
	}
	if onset.IsZero() {
		return true
	}
	wall := t.In(loc)
	return onset.After(time.Date(wall.Year(), wall.Month(), wall.Day(), wall.Hour(), wall.Minute(), wall.Second(), 0, time.UTC))
}

// zoneTransition is one change of a zone's offset, name or DST flag.
type zoneTransition struct {
	at      time.Time // the instant, UTC
	local   time.Time // wall-clock time just before it (offset fromOff), in UTC fields
	fromOff int       // seconds east of UTC before
	toOff   int       // and after
	name    string
	dst     bool
	changes bool // false for a bound Go reports where nothing changes
}

func transitionAt(loc *time.Location, at time.Time) zoneTransition {
	before, after := at.Add(-time.Second).In(loc), at.In(loc)
	fromName, fromOff := before.Zone()
	name, toOff := after.Zone()
	return zoneTransition{
		at:      at.UTC(),
		local:   at.UTC().Add(time.Duration(fromOff) * time.Second),
		fromOff: fromOff,
		toOff:   toOff,
		name:    name,
		dst:     after.IsDST(),
		changes: fromOff != toOff || fromName != name || before.IsDST() != after.IsDST(),
	}
}

// GenerateVTimezone builds a VTIMEZONE for loc under the given TZID, valid for
// every instant from min(from, 1970-01-01) on. Consecutive yearly transitions
// that follow one rule (e.g. "last Sunday of March, 02:00") collapse into one
// STANDARD/DAYLIGHT observance with an RRULE; anything irregular is listed one
// onset at a time.
func GenerateVTimezone(tzid string, loc *time.Location, from time.Time) *ical.Component {
	if from.IsZero() || from.After(vtimezoneWindowStart) {
		from = vtimezoneWindowStart
	}

	// The observance in force at the window start, then every transition up to
	// the window end.
	var transitions []zoneTransition
	cur := from.In(loc)
	start, _ := cur.ZoneBounds()
	synthetic := start.IsZero()
	if synthetic {
		// No earlier transition: the zone has been in this state since the
		// beginning of its data, so any onset at or before `from` will do.
		name, off := cur.Zone()
		midnight := time.Date(cur.Year(), cur.Month(), cur.Day(), 0, 0, 0, 0, time.UTC)
		transitions = append(transitions, zoneTransition{
			at: midnight.Add(-time.Duration(off) * time.Second), local: midnight,
			fromOff: off, toOff: off, name: name, dst: cur.IsDST(),
		})
	} else {
		transitions = append(transitions, transitionAt(loc, start))
	}
	for steps := 0; steps < maxZoneSteps; steps++ {
		_, next := cur.ZoneBounds()
		if next.IsZero() || !next.Before(vtimezoneWindowEnd) {
			break
		}
		if !next.After(cur) {
			// Past a zone's explicit data Go derives the bounds from its POSIX
			// rule one year at a time, cut at year ends, and can report an end
			// at or before cur (seen on 31 December of leap years): step over it.
			cur = cur.Add(time.Hour)
			continue
		}
		cur = next.In(loc)
		if t := transitionAt(loc, next); t.changes {
			transitions = append(transitions, t)
		}
	}

	type runKey struct {
		fromOff, toOff int
		name           string
		dst            bool
		month          time.Month
		clock          int
	}
	keyOf := func(t zoneTransition) runKey {
		return runKey{t.fromOff, t.toOff, t.name, t.dst, t.local.Month(),
			t.local.Hour()*3600 + t.local.Minute()*60 + t.local.Second()}
	}
	var runs []*zoneRun
	open := make(map[runKey]*zoneRun)
	for i, t := range transitions {
		k := keyOf(t)
		if r := open[k]; r != nil && r.extend(t) {
			continue
		}
		r := newZoneRun(t)
		runs = append(runs, r)
		if i > 0 || !synthetic { // a made-up onset is no yearly rule
			open[k] = r
		}
	}

	vtz := ical.NewComponent(ical.CompTimezone)
	vtz.Props.SetText(ical.PropTimezoneID, tzid)
	for _, r := range runs {
		vtz.Children = append(vtz.Children, r.component())
	}
	return vtz
}

// zoneRun is a sequence of transitions of one kind in consecutive years, all
// matching one yearly rule.
type zoneRun struct {
	first, last zoneTransition
	n           int
	weekday     bool // every onset falls on first's weekday …
	lo, hi      int  // … and is that weekday's first on or after day N, for every N in [lo, hi]
	lastWeek    bool // … or is the last such weekday of the month
	sameDay     bool // every onset is on first's day of the month
}

func newZoneRun(t zoneTransition) *zoneRun {
	d := t.local.Day()
	return &zoneRun{first: t, last: t, n: 1, weekday: true, lo: max(1, d-6), hi: d,
		lastWeek: isLastWeekdayOfMonth(t.local), sameDay: true}
}

func (r *zoneRun) extend(t zoneTransition) bool {
	if t.local.Year() != r.last.local.Year()+1 {
		return false
	}
	d := t.local.Day()
	weekday := r.weekday && t.local.Weekday() == r.first.local.Weekday()
	lo, hi := max(r.lo, d-6), min(r.hi, d)
	lastWeek := r.lastWeek && isLastWeekdayOfMonth(t.local)
	sameDay := r.sameDay && d == r.first.local.Day()
	weekday = weekday && (lastWeek || lo <= hi)
	if !weekday && !sameDay {
		return false
	}
	r.last, r.n = t, r.n+1
	r.weekday, r.lo, r.hi, r.lastWeek, r.sameDay = weekday, lo, hi, lastWeek, sameDay
	return true
}

func isLastWeekdayOfMonth(t time.Time) bool {
	return t.AddDate(0, 0, 7).Month() != t.Month()
}

var icalWeekdays = [...]string{"SU", "MO", "TU", "WE", "TH", "FR", "SA"}

func (r *zoneRun) component() *ical.Component {
	name := ical.CompTimezoneStandard
	if r.first.dst {
		name = ical.CompTimezoneDaylight
	}
	obs := ical.NewComponent(name)
	obs.Props.Set(&ical.Prop{Name: ical.PropDateTimeStart, Params: ical.Params{}, Value: r.first.local.Format(icalLocalLayout)})
	obs.Props.Set(&ical.Prop{Name: ical.PropTimezoneOffsetFrom, Params: ical.Params{}, Value: formatUTCOffset(r.first.fromOff)})
	obs.Props.Set(&ical.Prop{Name: ical.PropTimezoneOffsetTo, Params: ical.Params{}, Value: formatUTCOffset(r.first.toOff)})
	if r.first.name != "" {
		obs.Props.SetText(ical.PropTimezoneName, r.first.name)
	}
	if r.n > 1 {
		obs.Props.Set(&ical.Prop{Name: ical.PropRecurrenceRule, Params: ical.Params{}, Value: r.rrule()})
	}
	return obs
}

func (r *zoneRun) rrule() string {
	parts := []string{"FREQ=YEARLY", fmt.Sprintf("BYMONTH=%d", int(r.first.local.Month()))}
	wd := icalWeekdays[r.first.local.Weekday()]
	switch {
	case r.weekday && r.lastWeek:
		parts = append(parts, "BYDAY=-1"+wd)
	case r.weekday:
		if n := (r.lo + 5) / 7 * 7; n+1 <= r.hi {
			// [lo, hi] holds a day 1, 8, 15, …: "the n-th <weekday>".
			parts = append(parts, fmt.Sprintf("BYDAY=%d%s", n/7+1, wd))
		} else {
			days := make([]string, 0, 7)
			for d := r.lo; d < r.lo+7 && d <= 31; d++ {
				days = append(days, strconv.Itoa(d))
			}
			parts = append(parts, "BYDAY="+wd, "BYMONTHDAY="+strings.Join(days, ","))
		}
	default: // sameDay
		parts = append(parts, fmt.Sprintf("BYMONTHDAY=%d", r.first.local.Day()))
	}
	// A run still going at the window end continues as the zone's current rule;
	// one that stopped inside the window ends with its last onset.
	if r.last.at.AddDate(1, 0, 0).Before(vtimezoneWindowEnd) {
		parts = append(parts, "UNTIL="+r.last.at.Format("20060102T150405Z"))
	}
	return strings.Join(parts, ";")
}

// formatUTCOffset renders seconds east of UTC as an RFC 5545 UTC-OFFSET
// (±HHMM, or ±HHMMSS when there are seconds).
func formatUTCOffset(sec int) string {
	sign := byte('+')
	if sec < 0 {
		sign, sec = '-', -sec
	}
	if sec%60 != 0 {
		return fmt.Sprintf("%c%02d%02d%02d", sign, sec/3600, sec/60%60, sec%60)
	}
	return fmt.Sprintf("%c%02d%02d", sign, sec/3600, sec/60%60)
}
