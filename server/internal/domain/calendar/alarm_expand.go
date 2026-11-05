package calendar

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/emersion/go-ical"
)

// AlarmInstanceCap is the most alarm instances ExpandAlarms returns for one
// object (spec §6.6).
const AlarmInstanceCap = 500

// alarmKeyPrefix versions the alarm key. A change to the key's inputs bumps it;
// every key then changes once.
const alarmKeyPrefix = "calcard-alarm-v1|"

// alarmWallSlack is the zone shift a floating or DATE series can see between its
// UTC-wall expansion and the owner's zone.
const alarmWallSlack = 14 * time.Hour

// AlarmInstance is one alarm to deliver: one VALARM of one component or
// occurrence, resolved to a trigger instant.
type AlarmInstance struct {
	Key          string    // AlarmKey(UID, RecurrenceID, TriggerAt), D22
	TriggerAt    time.Time // UTC, second precision
	UID          string
	RecurrenceID string // "" or the canonical key (a master's absolute alarm is always "")
	// Index is informational: the lowest index among the VALARMs sharing this
	// key (D22). Action and Description are that VALARM's (an absent ACTION
	// reads DISPLAY).
	Index       int
	Action      string
	Description string

	Object *CalendarObject
	// Summary is the component's SUMMARY (an override's own, else the master's,
	// else the object column). Start and End are the occurrence's anchors as
	// the expansion produced them (a UTC wall clock when Wall); zero for a task
	// without DTSTART or DUE.
	Summary    string
	Start, End time.Time
	Wall       bool
	// Location is the component's LOCATION (an override's own, else the master's,
	// else the object column). ZoneDependent reports that the trigger instant
	// depended on the owner's time zone: a relative trigger on a floating or DATE
	// anchor, or a floating absolute trigger (spec §6.5).
	Location      string
	ZoneDependent bool
}

// AlarmKey is the stable identity of an alarm (spec §6.7, D22): lowercase hex of
// the first 16 bytes of SHA-256 over the version prefix, the trigger instant in
// UTC at second precision, the recurrence key and the UID. It has no VALARM
// index: two VALARMs of one occurrence with the same instant are one alarm.
// The key match is the join: the key the expansion computes is the key stored
// at acknowledgement, so any change to these inputs changes every key.
func AlarmKey(uid, rid string, triggerAt time.Time) string {
	sum := sha256.Sum256([]byte(alarmKeyPrefix + triggerAt.UTC().Format(RecurrenceIDLayout) + "|" + rid + "|" + uid))
	return hex.EncodeToString(sum[:16])
}

// alarmCollector gathers instances of one object, collapsing same-key alarms
// and enforcing the cap.
type alarmCollector struct {
	obj         *CalendarObject
	from, until time.Time
	ownerLoc    *time.Location
	byKey       map[string]int
	out         []AlarmInstance
	capped      bool
}

// add records one deliverable alarm; false once the cap is exceeded.
func (c *alarmCollector) add(comp *ical.Component, spec AlarmSpec, rid string, base *ical.Component, start, end *TriggerBase) bool {
	t, ok := spec.TriggerAt(start, end, c.ownerLoc)
	if !ok || t.Before(c.from) || t.After(c.until) {
		return true
	}
	key := AlarmKey(c.obj.UID, rid, t)
	action, desc := alarmActionDescription(comp, spec)
	if i, dup := c.byKey[key]; dup {
		if spec.Index < c.out[i].Index {
			c.out[i].Index, c.out[i].Action, c.out[i].Description = spec.Index, action, desc
		}
		return true
	}
	if len(c.out) >= AlarmInstanceCap {
		c.capped = true
		return false
	}
	inst := AlarmInstance{
		Key: key, TriggerAt: t, UID: c.obj.UID, RecurrenceID: rid,
		Index: spec.Index, Action: action, Description: desc,
		Object: c.obj, Summary: c.obj.Summary, Location: c.obj.Location,
	}
	if spec.Relative {
		zoneBase := start
		if spec.Related == "END" {
			zoneBase = end
		}
		inst.ZoneDependent = zoneBase != nil && zoneBase.Wall
	} else {
		inst.ZoneDependent = spec.Floating
	}
	for _, src := range []*ical.Component{base, comp} {
		if src == nil {
			continue
		}
		if p := src.Props.Get(ical.PropSummary); p != nil {
			inst.Summary = UnescapeText(p.Value)
		}
		if p := src.Props.Get(ical.PropLocation); p != nil {
			inst.Location = UnescapeText(p.Value)
		}
	}
	if start != nil {
		inst.Start, inst.Wall = start.At, start.Wall
	}
	if end != nil {
		inst.End = end.At
	}
	c.byKey[key] = len(c.out)
	c.out = append(c.out, inst)
	return true
}

// alarmActionDescription reads ACTION and DESCRIPTION of the VALARM child the
// spec was parsed from.
func alarmActionDescription(comp *ical.Component, spec AlarmSpec) (action, desc string) {
	action = spec.Action
	if action == "" {
		action = "DISPLAY"
	}
	n := 0
	for _, child := range comp.Children {
		if child == nil || child.Name != ical.CompAlarm {
			continue
		}
		if n == spec.Index {
			if p := child.Props.Get(ical.PropDescription); p != nil {
				desc = UnescapeText(p.Value)
			}
			break
		}
		n++
	}
	return action, desc
}

// isCancelled reports STATUS:CANCELLED on a component.
func isCancelled(comp *ical.Component) bool {
	if comp == nil {
		return false
	}
	p := comp.Props.Get(ical.PropStatus)
	return p != nil && strings.EqualFold(strings.TrimSpace(p.Value), "CANCELLED")
}

// ExpandAlarms resolves every deliverable alarm of obj whose trigger falls in
// [from, until] (inclusive), at most AlarmInstanceCap per object (spec §6.6).
//
//   - Events: master VALARMs per generated occurrence (forEachOccurrence); an
//     override uses only its own VALARMs, so one without VALARM has no alarm; a
//     stray override is an occurrence of its own. An absolute trigger fires once
//     per component: a master's with RecurrenceID "", an override's with the
//     override's. EXDATEd beats yield nothing. RDATE is NOT expanded (a
//     pre-existing gap of the expansion). STATUS:CANCELLED yields nothing for that
//     component; a cancelled master silences its generated beats while a
//     non-cancelled override still delivers.
//   - Tasks: never expanded; the master's current DUE/DTSTART; overrides ignored;
//     a task whose Status column is done yields nothing.
//
// ownerLoc places floating and DATE anchors (nil = UTC). capped reports that
// more instances existed than were returned. An undecodable ICalData is an
// error; the caller skips the object.
func ExpandAlarms(obj *CalendarObject, from, until time.Time, ownerLoc *time.Location) (instances []AlarmInstance, capped bool, err error) {
	if obj == nil {
		return nil, false, fmt.Errorf("nil calendar object")
	}
	cal, err := ical.NewDecoder(strings.NewReader(obj.ICalData)).Decode()
	if err != nil {
		return nil, false, fmt.Errorf("failed to parse iCalendar data: %w", err)
	}
	from, until = from.UTC(), until.UTC()
	c := &alarmCollector{obj: obj, from: from, until: until, ownerLoc: ownerLoc, byKey: map[string]int{}}

	if obj.ComponentType == ComponentToDo {
		expandTaskAlarms(c, cal)
	} else {
		expandEventAlarms(c, cal)
	}
	sort.SliceStable(c.out, func(i, j int) bool { return c.out[i].TriggerAt.Before(c.out[j].TriggerAt) })
	return c.out, c.capped, nil
}

func expandTaskAlarms(c *alarmCollector, cal *ical.Calendar) {
	for _, s := range TaskDoneStatuses() {
		if c.obj.Status == s {
			return
		}
	}
	master := TaskMaster(cal)
	if master == nil {
		return
	}
	start, end := ComponentTriggerBases(master)
	for _, spec := range ParseAlarms(master) {
		if spec.Deliverable && !c.add(master, spec, "", nil, start, end) {
			return
		}
	}
}

func expandEventAlarms(c *alarmCollector, cal *ical.Calendar) {
	events := cal.Events()
	var masters []ical.Event
	for i := range events {
		if events[i].Props.Get(ical.PropRecurrenceID) == nil {
			masters = append(masters, events[i])
		}
	}
	docLoc := seriesLocation(masters)

	// Absolute triggers fire once per component, independent of the
	// occurrences; collect the relative offsets for the window on the way.
	var minOff, maxOff time.Duration
	haveRelative, anyWall := false, false
	for i := range events {
		ev := &events[i]
		if isWallProp(ev.Props.Get(ical.PropDateTimeStart)) {
			anyWall = true
		}
		if isCancelled(ev.Component) {
			continue
		}
		rid := ""
		if p := ev.Props.Get(ical.PropRecurrenceID); p != nil {
			rid = p.Value
			if k, err := RecurrenceIDKeyFromProp(p, docLoc); err == nil {
				rid = k
			}
		}
		for _, spec := range ParseAlarms(ev.Component) {
			if !spec.Deliverable {
				continue
			}
			if !spec.Relative {
				if !c.add(ev.Component, spec, rid, nil, nil, nil) {
					return
				}
				continue
			}
			off := time.Duration(spec.Offset.Days)*24*time.Hour + time.Duration(spec.Offset.Secs)*time.Second
			var slack time.Duration
			if spec.Offset.Days > 0 {
				slack = time.Hour
			}
			if spec.Offset.Neg {
				off = -off
			}
			lo, hi := off-slack, off+slack
			if !haveRelative || lo < minOff {
				minOff = lo
			}
			if !haveRelative || hi > maxOff {
				maxOff = hi
			}
			haveRelative = true
		}
	}
	if !haveRelative {
		return
	}

	// anchor + offset must land in [from, until]: the anchor lies in
	// [from-maxOff, until-minOff]. The iterator's overlap test over-includes;
	// exact triggers are filtered in add.
	lo, hi := c.from.Add(-maxOff-time.Second), c.until.Add(-minOff+time.Second)
	if anyWall {
		lo, hi = lo.Add(-alarmWallSlack), hi.Add(alarmWallSlack)
	}
	forEachOccurrence(cal, lo, hi, func(o Occurrence) bool {
		comp := o.Override
		if comp == nil {
			comp = o.Master
		}
		if comp == nil || isCancelled(comp.Component) {
			return true
		}
		start, end := &TriggerBase{At: o.Start, Wall: o.Wall}, &TriggerBase{At: o.End, Wall: o.Wall}
		var base *ical.Component
		if o.Master != nil {
			base = o.Master.Component
		}
		for _, spec := range ParseAlarms(comp.Component) {
			if spec.Deliverable && spec.Relative && !c.add(comp.Component, spec, o.RecurrenceID, base, start, end) {
				return false
			}
		}
		return true
	})
}
