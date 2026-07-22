package calendar

import (
	"fmt"
	"strings"
	"time"

	"github.com/emersion/go-ical"
	"github.com/teambition/rrule-go"
)

// EventInstance represents a single instance of a recurring event
type EventInstance struct {
	Event        *CalendarObject `json:"event"`
	ID           string          `json:"id"`
	CalendarID   uint            `json:"calendar_id"`
	UID          string          `json:"uid"`
	Summary      string          `json:"summary"`
	Description  string          `json:"description"`
	Location     string          `json:"location"`
	Start        time.Time       `json:"start"`
	End          time.Time       `json:"end"`
	IsAllDay     bool            `json:"is_all_day"`
	RecurrenceID string          `json:"recurrence_id,omitempty"`
	IsException  bool            `json:"is_exception"`
}

// RecurrenceRule represents the recurrence rules for an event
type RecurrenceRule struct {
	Frequency  string   `json:"frequency"`    // daily, weekly, monthly, yearly
	Interval   int      `json:"interval"`     // Every N frequency units
	ByDay      []string `json:"by_day"`       // MO, TU, WE, TH, FR, SA, SU
	ByMonthDay []int    `json:"by_month_day"` // 1-31
	ByMonth    []int    `json:"by_month"`     // 1-12
	Count      *int     `json:"count"`        // Number of occurrences
	Until      *string  `json:"until"`        // End date (ISO 8601)
}

// ToRRule renders the RecurrenceRule as an RFC 5545 RRULE string. It is the
// single canonical renderer: the HTTP DTO delegates here via ToDomain so both
// the create and update paths produce identical output.
//
// allDay selects the value type used for the UNTIL boundary: RFC 5545 §3.3.10
// requires UNTIL to share the series' DTSTART value type, so an all-day
// (VALUE=DATE) series must emit a bare DATE UNTIL (20060102) while a timed
// series emits a UTC DATE-TIME (20060102T150405Z). A mismatched type makes
// strict clients (Apple, DAVx5) reject the whole RRULE. Because the caller
// passes the *raw* Until (still carrying its RFC 3339 offset) together with the
// effective all-day state, the all-day branch preserves the sender's local
// calendar date instead of shifting it a day west of UTC.
func (r *RecurrenceRule) ToRRule(allDay bool) string {
	if r == nil || r.Frequency == "" {
		return ""
	}

	parts := []string{"FREQ=" + strings.ToUpper(r.Frequency)}
	if r.Interval > 1 {
		parts = append(parts, fmt.Sprintf("INTERVAL=%d", r.Interval))
	}
	if len(r.ByDay) > 0 {
		parts = append(parts, "BYDAY="+strings.Join(r.ByDay, ","))
	}
	if len(r.ByMonthDay) > 0 {
		var days []string
		for _, d := range r.ByMonthDay {
			days = append(days, fmt.Sprintf("%d", d))
		}
		parts = append(parts, "BYMONTHDAY="+strings.Join(days, ","))
	}
	if len(r.ByMonth) > 0 {
		var months []string
		for _, m := range r.ByMonth {
			months = append(months, fmt.Sprintf("%d", m))
		}
		parts = append(parts, "BYMONTH="+strings.Join(months, ","))
	}
	if r.Count != nil {
		parts = append(parts, fmt.Sprintf("COUNT=%d", *r.Count))
	}
	if r.Until != nil && *r.Until != "" {
		until := *r.Until
		// The frontend sends UNTIL as RFC 3339 (e.g. 2026-08-01T00:00:00+02:00),
		// but rrule.StrToRRule requires the iCal basic forms (DATE 20060102 or
		// DATE-TIME UTC 20060102T150405Z). Normalize here so both formats are
		// accepted; anything that isn't RFC 3339 is passed through and validated
		// later by StrToRRule.
		if t, err := time.Parse(time.RFC3339, until); err == nil {
			if allDay {
				// Keep the sender's local calendar date (t retains its offset,
				// so 2026-08-01T00:00:00+02:00 renders as 20260801). This both
				// matches DTSTART;VALUE=DATE and — since UNTIL is inclusive —
				// stops the chosen end date's own occurrence being dropped east
				// of UTC, which .UTC() would have done.
				until = t.Format("20060102")
			} else {
				until = t.UTC().Format("20060102T150405Z")
			}
		}
		parts = append(parts, "UNTIL="+until)
	}

	return strings.Join(parts, ";")
}

// ExpandRecurringEvent expands a recurring event into instances within a time range
func ExpandRecurringEvent(obj *CalendarObject, start, end time.Time) ([]EventInstance, error) {
	cal, err := ical.NewDecoder(strings.NewReader(obj.ICalData)).Decode()
	if err != nil {
		return nil, fmt.Errorf("failed to parse iCalendar data: %w", err)
	}

	allEvents := cal.Events()
	if len(allEvents) == 0 {
		return nil, nil
	}

	// Group components: masters (no RECURRENCE-ID) vs exception overrides.
	var masters, rawExceptions []ical.Event
	for i := range allEvents {
		if allEvents[i].Props.Get(ical.PropRecurrenceID) == nil {
			masters = append(masters, allEvents[i])
		} else {
			rawExceptions = append(rawExceptions, allEvents[i])
		}
	}

	// docLoc is the series' base timezone, taken from the first master's
	// DTSTART (default UTC). A RECURRENCE-ID written without an explicit TZID
	// is interpreted in this zone, matching how clients emit exception/split
	// overrides.
	docLoc := time.UTC
	if len(masters) > 0 {
		if dtstartProp := masters[0].Props.Get(ical.PropDateTimeStart); dtstartProp != nil {
			if t, err := dtstartProp.DateTime(time.UTC); err == nil {
				docLoc = t.Location()
			}
		}
	}

	// Key exceptions by their canonical UTC RECURRENCE-ID so TZID= / VALUE=DATE
	// / floating forms all collapse to the same key the generated occurrences
	// use. Fall back to the raw value when the property can't be parsed.
	exceptions := make(map[string]ical.Event, len(rawExceptions))
	for i := range rawExceptions {
		rid := rawExceptions[i].Props.Get(ical.PropRecurrenceID)
		key := rid.Value
		if k, err := RecurrenceIDKeyFromProp(rid, docLoc); err == nil {
			key = k
		}
		exceptions[key] = rawExceptions[i]
	}

	var instances []EventInstance

	// Process each master series
	for i := range masters {
		master := &masters[i]
		rruleProp := master.Props.Get(ical.PropRecurrenceRule)
		mStart, err := master.DateTimeStart(time.UTC)
		if err != nil {
			continue // skip events without start date
		}
		mEnd, _ := master.DateTimeEnd(time.UTC)
		mDuration := mEnd.Sub(mStart)
		if mDuration < 0 {
			mDuration = 0
		}

		// Determine the base timezone
		loc := time.UTC
		if dtstartProp := master.Props.Get(ical.PropDateTimeStart); dtstartProp != nil {
			if t, err := dtstartProp.DateTime(time.UTC); err == nil {
				loc = t.Location()
			}
		}

		if rruleProp == nil {
			// Single occurrence master
			rid := mStart.UTC().Format("20060102T150405Z")
			if mStart.Before(end) && mEnd.After(start) {
				if exc, ok := exceptions[rid]; ok {
					instances = append(instances, ToEventInstance(obj, mStart, mEnd, rid, master, &exc))
				} else {
					instances = append(instances, ToEventInstance(obj, mStart, mEnd, "", master, nil))
				}
			}
			continue
		}

		// Expand RRULE
		rule, err := rrule.StrToRRule(rruleProp.Value)
		if err != nil {
			continue // skip invalid rules
		}
		rule.DTStart(mStart.In(loc))

		// Collect EXDATEs as canonical UTC keys for this master. EXDATEKeys
		// understands comma-separated lists, TZID=, and VALUE=DATE forms.
		exMap := make(map[string]bool)
		for _, p := range master.Props["EXDATE"] {
			for _, key := range EXDATEKeys(&p, loc) {
				exMap[key] = true
			}
		}

		// Generate within range: rule.Between works best in series timezone.
		// Widen the lower bound by the event duration so an occurrence that
		// STARTED before the window but is still running inside it is produced;
		// the overlap filter below discards any that don't actually intersect.
		for _, dt := range rule.Between(start.Add(-mDuration).In(loc), end.In(loc), true) {
			dtUTC := dt.UTC()
			rid := dtUTC.Format("20060102T150405Z")

			if exMap[rid] {
				continue
			}

			if exc, ok := exceptions[rid]; ok {
				// Use exception data
				excStartProp := exc.Props.Get(ical.PropDateTimeStart)
				excEndProp := exc.Props.Get(ical.PropDateTimeEnd)

				tStart := dt
				if excStartProp != nil {
					tStart, _ = excStartProp.DateTime(time.UTC)
				}
				tEnd := tStart.Add(mDuration)
				if excEndProp != nil {
					tEnd, _ = excEndProp.DateTime(time.UTC)
				}

				if !tStart.Before(end) || !tEnd.After(start) {
					continue
				}
				instances = append(instances, ToEventInstance(obj, tStart, tEnd, rid, master, &exc))
			} else {
				// Keep only occurrences that actually overlap [start, end): the
				// widened lower bound can surface beats that ended before the
				// window opened.
				occEnd := dt.Add(mDuration)
				if !dt.Before(end) || !occEnd.After(start) {
					continue
				}
				instances = append(instances, ToEventInstance(obj, dt, occEnd, rid, master, nil))
			}
		}
	}

	// Add any "stray" exceptions that weren't picked up by any master expansion
	// (This can happen if an exception's recurrence-id doesn't match expansion beats)
	for rid, e := range exceptions {
		found := false
		for _, inst := range instances {
			if inst.RecurrenceID == rid {
				found = true
				break
			}
		}
		if !found {
			tStart, _ := e.DateTimeStart(time.UTC)
			tEnd, _ := e.DateTimeEnd(time.UTC)
			if tStart.Before(end) && tEnd.After(start) {
				instances = append(instances, ToEventInstance(obj, tStart, tEnd, rid, nil, &e))
			}
		}
	}

	return instances, nil
}

// ToEventInstance converts a CalendarObject to an EventInstance, with optional property overrides from a VEVENT component
func ToEventInstance(obj *CalendarObject, start, end time.Time, recurrenceID string, master *ical.Event, override *ical.Event) EventInstance {
	inst := EventInstance{
		Event:        obj,
		ID:           obj.UUID,
		CalendarID:   obj.CalendarID,
		UID:          obj.UID,
		Summary:      obj.Summary,
		Description:  obj.Description,
		Location:     obj.Location,
		Start:        start,
		End:          end,
		IsAllDay:     obj.IsAllDay,
		RecurrenceID: recurrenceID,
	}

	// Use master properties if available
	if master != nil {
		if p := master.Props.Get(ical.PropSummary); p != nil {
			inst.Summary = p.Value
		}
		if p := master.Props.Get(ical.PropDescription); p != nil {
			inst.Description = p.Value
		}
		if p := master.Props.Get(ical.PropLocation); p != nil {
			inst.Location = p.Value
		}
	}

	// Override with exception properties
	if override != nil {
		if p := override.Props.Get(ical.PropSummary); p != nil {
			inst.Summary = p.Value
		}
		if p := override.Props.Get(ical.PropDescription); p != nil {
			inst.Description = p.Value
		}
		if p := override.Props.Get(ical.PropLocation); p != nil {
			inst.Location = p.Value
		}
		inst.IsException = true
	}

	return inst
}
