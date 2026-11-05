package calendar

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-ical"
	"github.com/google/uuid"
)

// Writing VALARMs in place (#169, spec §7.5). Pure domain code: it edits the
// decoded component and never encodes it.

// MaxReminders is the most entries a request may carry in `reminders`.
const MaxReminders = 10

// ReminderKind selects the RELATED anchor of a relative reminder: START for an
// event, END for a task (a task is due at its end).
type ReminderKind string

const (
	ReminderKindEvent ReminderKind = "event"
	ReminderKindTask  ReminderKind = "task"
)

func (k ReminderKind) related() string {
	if k == ReminderKindTask {
		return "END"
	}
	return "START"
}

// ReminderInput is one desired reminder: exactly one of OffsetSeconds (relative,
// signed, a nominal day is 86400 s) and At (absolute).
type ReminderInput struct {
	OffsetSeconds *int64
	At            *time.Time
}

// Errors of the reminder write rules; the messages are the REST 400 texts.
var (
	ErrReminderCount       = errors.New("reminders must contain at most 10 entries")
	ErrReminderShape       = errors.New("a reminder needs exactly one of offset_seconds and at")
	ErrReminderOffsetRange = errors.New("reminder offset out of range")
	ErrReminderTimeRange   = errors.New("reminder time out of range")
)

// CheckReminderShape validates the count and the one-of rule of every entry.
func CheckReminderShape(desired []ReminderInput) error {
	if len(desired) > MaxReminders {
		return ErrReminderCount
	}
	for _, d := range desired {
		if (d.OffsetSeconds == nil) == (d.At == nil) {
			return ErrReminderShape
		}
	}
	return nil
}

// CheckReminderRanges applies the range rules to entries (the ones that match no
// existing VALARM, see UnmatchedReminders): an offset within [-400 d, +400 d],
// an absolute time within 400 days of the component's start.
func CheckReminderRanges(entries []ReminderInput, start time.Time) error {
	for _, d := range entries {
		if d.OffsetSeconds != nil {
			if *d.OffsetSeconds < -maxAlarmOffsetSeconds || *d.OffsetSeconds > maxAlarmOffsetSeconds {
				return ErrReminderOffsetRange
			}
			continue
		}
		if d.At != nil {
			diff := d.At.Sub(start)
			if diff < -time.Duration(maxAlarmOffsetSeconds)*time.Second || diff > time.Duration(maxAlarmOffsetSeconds)*time.Second {
				return ErrReminderTimeRange
			}
		}
	}
	return nil
}

// Editable reports whether the web form may change the alarm without losing
// information (spec §7.3): a valid DISPLAY/AUDIO/action-less VALARM without
// REPEAT or snooze that is relative to the kind's anchor, or absolute and not
// floating.
func (a AlarmSpec) Editable(kind ReminderKind) bool {
	if !a.Valid || a.Repeat || a.Snooze {
		return false
	}
	if !(a.Action == "" || a.Action == "DISPLAY" || a.Action == "AUDIO") {
		return false
	}
	switch {
	case a.Relative:
		return a.Related == kind.related()
	case a.Absolute != nil:
		return !a.Floating
	}
	return false
}

// relativeSeconds is the signed total of a relative spec in seconds.
func (a AlarmSpec) relativeSeconds() int64 {
	v := int64(a.Offset.Days)*86400 + a.Offset.Secs
	if a.Offset.Neg {
		v = -v
	}
	return v
}

func (a AlarmSpec) matches(d ReminderInput) bool {
	switch {
	case d.OffsetSeconds != nil:
		return a.Relative && a.relativeSeconds() == *d.OffsetSeconds
	case d.At != nil:
		return !a.Relative && a.Absolute != nil &&
			a.Absolute.UTC().Truncate(time.Second).Equal(d.At.UTC().Truncate(time.Second))
	}
	return false
}

// matchReminders classifies the component's VALARMs and matches desired as a
// multiset, greedily, in document order. It returns the VALARM children, their
// specs, which desired entries were used and which VALARM matched.
func matchReminders(comp *ical.Component, desired []ReminderInput, kind ReminderKind) (alarms []*ical.Component, specs []AlarmSpec, used []bool, keep []bool) {
	specs = ParseAlarms(comp)
	for _, ch := range comp.Children {
		if ch != nil && ch.Name == ical.CompAlarm {
			alarms = append(alarms, ch)
		}
	}
	used = make([]bool, len(desired))
	keep = make([]bool, len(alarms))
	for i, s := range specs {
		if !s.Editable(kind) {
			keep[i] = true // never touched
			continue
		}
		for j, d := range desired {
			if !used[j] && s.matches(d) {
				used[j] = true
				keep[i] = true
				break
			}
		}
	}
	return alarms, specs, used, keep
}

// UnmatchedReminders returns the desired entries that match no existing
// editable VALARM: the ones ApplyReminders would append, and the only ones the
// range rules apply to.
func UnmatchedReminders(comp *ical.Component, desired []ReminderInput, kind ReminderKind) []ReminderInput {
	if comp == nil {
		return append([]ReminderInput(nil), desired...)
	}
	_, _, used, _ := matchReminders(comp, desired, kind)
	var out []ReminderInput
	for j, d := range desired {
		if !used[j] {
			out = append(out, d)
		}
	}
	return out
}

// ApplyReminders makes the editable VALARMs of comp the desired multiset, in
// place (spec §7.5): matched VALARMs stay untouched, unmatched editable ones are
// removed, non-editable ones are never touched, and each unmatched desired
// entry is appended after the existing children. It reports whether anything
// changed and the appended VALARMs.
func ApplyReminders(comp *ical.Component, desired []ReminderInput, kind ReminderKind) (changed bool, appended []*ical.Component) {
	if comp == nil {
		return false, nil
	}
	alarms, _, used, keep := matchReminders(comp, desired, kind)
	drop := map[*ical.Component]bool{}
	for i, a := range alarms {
		if !keep[i] {
			drop[a] = true
		}
	}
	if len(drop) > 0 {
		kept := comp.Children[:0:0]
		for _, ch := range comp.Children {
			if !drop[ch] {
				kept = append(kept, ch)
			}
		}
		comp.Children = kept
		changed = true
	}
	for j, d := range desired {
		if used[j] {
			continue
		}
		a := newReminderAlarm(d, kind)
		comp.Children = append(comp.Children, a)
		appended = append(appended, a)
		changed = true
	}
	return changed, appended
}

func newReminderAlarm(d ReminderInput, kind ReminderKind) *ical.Component {
	a := ical.NewComponent(ical.CompAlarm)
	a.Props.SetText(ical.PropAction, "DISPLAY")
	a.Props.SetText(ical.PropDescription, "Reminder")
	trigger := &ical.Prop{Name: ical.PropTrigger}
	if d.OffsetSeconds != nil {
		trigger.Value = FormatAlarmDuration(*d.OffsetSeconds)
		if kind == ReminderKindTask {
			trigger.Params = ical.Params{ical.ParamRelated: {"END"}}
		}
	} else {
		trigger.Value = d.At.UTC().Format("20060102T150405Z")
		trigger.Params = ical.Params{ical.ParamValue: {"DATE-TIME"}}
	}
	a.Props.Set(trigger)
	a.Props.SetText(ical.PropUID, uuid.New().String())
	return a
}

// FormatAlarmDuration is the canonical RFC 5545 duration of a signed number of
// seconds: whole weeks `P{n}W`, else `P{d}DT{h}H{m}M{s}S` without zero parts,
// `PT0S` for zero. go-ical's SetDuration is never used (it writes PT900S).
func FormatAlarmDuration(seconds int64) string {
	if seconds == 0 {
		return "PT0S"
	}
	var sb strings.Builder
	if seconds < 0 {
		sb.WriteByte('-')
		seconds = -seconds
	}
	sb.WriteByte('P')
	if seconds%(7*86400) == 0 {
		sb.WriteString(strconv.FormatInt(seconds/(7*86400), 10))
		sb.WriteByte('W')
		return sb.String()
	}
	d, rest := seconds/86400, seconds%86400
	if d > 0 {
		fmt.Fprintf(&sb, "%dD", d)
	}
	if rest > 0 {
		sb.WriteByte('T')
		if h := rest / 3600; h > 0 {
			fmt.Fprintf(&sb, "%dH", h)
		}
		if m := rest % 3600 / 60; m > 0 {
			fmt.Fprintf(&sb, "%dM", m)
		}
		if s := rest % 60; s > 0 {
			fmt.Fprintf(&sb, "%dS", s)
		}
	}
	return sb.String()
}
