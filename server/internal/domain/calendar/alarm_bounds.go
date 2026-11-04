package calendar

import (
	"time"

	"github.com/emersion/go-ical"
)

// alarmBoundsBuilder accumulates min(lo) / max(hi) over the contributions of an
// object's deliverable alarms (spec §6.8).
type alarmBoundsBuilder struct {
	lo, hi time.Time
	any    bool
}

func (b *alarmBoundsBuilder) add(lo, hi time.Time) {
	if !b.any || lo.Before(b.lo) {
		b.lo = lo
	}
	if !b.any || hi.After(b.hi) {
		b.hi = hi
	}
	b.any = true
}

// populateAlarmBounds derives AlarmFirstAt / AlarmLastAt (UTC): bounds of every
// trigger instant the object's deliverable alarms can produce, whatever the
// owner's zone. They are a coarse pre-filter for ExpandAlarms, so over-including
// is allowed and under-including is a bug. NULL when there is no deliverable
// alarm, a VTODO is done or every component is cancelled. It never fails: a
// malformed VALARM contributes nothing. Call it after Status and
// RecurrenceEndTime are set.
//
// Nominal days count as 24 h plus 1 h slack; a floating or DATE anchor (or a
// floating absolute trigger) adds +-14 h, the widest zone shift.
func (o *CalendarObject) populateAlarmBounds(cal *ical.Calendar) {
	o.AlarmFirstAt, o.AlarmLastAt = nil, nil
	var b alarmBoundsBuilder
	if o.ComponentType == ComponentToDo {
		o.taskAlarmBounds(cal, &b)
	} else {
		o.eventAlarmBounds(cal, &b)
	}
	if !b.any {
		return
	}
	lo, hi := b.lo.UTC(), b.hi.UTC()
	o.AlarmFirstAt, o.AlarmLastAt = &lo, &hi
}

func (o *CalendarObject) taskAlarmBounds(cal *ical.Calendar, b *alarmBoundsBuilder) {
	for _, s := range TaskDoneStatuses() {
		if o.Status == s {
			return
		}
	}
	master := TaskMaster(cal)
	if master == nil {
		return
	}
	start, end := ComponentTriggerBases(master)
	for _, spec := range ParseAlarms(master) {
		if spec.Deliverable {
			addSingleBound(b, spec, start, end)
		}
	}
}

func (o *CalendarObject) eventAlarmBounds(cal *ical.Calendar, b *alarmBoundsBuilder) {
	for _, comp := range cal.Children {
		if comp.Name != ical.CompEvent || isCancelled(comp) {
			continue
		}
		start, end := ComponentTriggerBases(comp)
		recurring := comp.Props.Get(ical.PropRecurrenceID) == nil &&
			(comp.Props.Get(ical.PropRecurrenceRule) != nil || comp.Props.Get(ical.PropRecurrenceDates) != nil)
		for _, spec := range ParseAlarms(comp) {
			if !spec.Deliverable {
				continue
			}
			if !recurring || !spec.Relative {
				addSingleBound(b, spec, start, end)
				continue
			}
			// Relative alarm on a recurring master: from the first anchor's
			// trigger to the end of the series plus the offset.
			first, ok := spec.TriggerAt(start, end, nil)
			if !ok {
				continue
			}
			slack := relativeSlack(spec, start, end)
			hiBase := farFuture
			if o.RecurrenceEndTime != nil {
				hiBase = *o.RecurrenceEndTime
			}
			if pos := offsetDuration(spec.Offset); pos > 0 {
				hiBase = hiBase.Add(pos)
			}
			if hiBase.Before(farFuture) {
				hiBase = hiBase.Add(slack)
			}
			b.add(first.Add(-slack), hiBase)
		}
	}
}

// addSingleBound adds the contribution of an alarm that fires once.
func addSingleBound(b *alarmBoundsBuilder, spec AlarmSpec, start, end *TriggerBase) {
	t, ok := spec.TriggerAt(start, end, nil)
	if !ok {
		return
	}
	slack := time.Duration(0)
	if spec.Relative {
		slack = relativeSlack(spec, start, end)
	} else if spec.Floating {
		slack = alarmWallSlack
	}
	b.add(t.Add(-slack), t.Add(slack))
}

// relativeSlack is the DST slack of a nominal-day offset, or the zone shift of a
// floating/DATE anchor.
func relativeSlack(spec AlarmSpec, start, end *TriggerBase) time.Duration {
	base := start
	if spec.Related == "END" {
		base = end
	}
	if base != nil && base.Wall {
		return alarmWallSlack + time.Hour
	}
	if spec.Offset.Days > 0 {
		return time.Hour
	}
	return 0
}

// offsetDuration is the signed offset with a nominal day as 24 h.
func offsetDuration(o AlarmOffset) time.Duration {
	d := time.Duration(o.Days)*24*time.Hour + time.Duration(o.Secs)*time.Second
	if o.Neg {
		return -d
	}
	return d
}
