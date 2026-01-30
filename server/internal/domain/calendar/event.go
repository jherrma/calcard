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

// ToRRule converts RecurrenceRule to RFC 5545 RRULE string
func (r *RecurrenceRule) ToRRule() string {
	parts := []string{fmt.Sprintf("FREQ=%s", strings.ToUpper(r.Frequency))}
	if r.Interval > 1 {
		parts = append(parts, fmt.Sprintf("INTERVAL=%d", r.Interval))
	}
	if len(r.ByDay) > 0 {
		parts = append(parts, fmt.Sprintf("BYDAY=%s", strings.Join(r.ByDay, ",")))
	}
	if len(r.ByMonthDay) > 0 {
		var days []string
		for _, d := range r.ByMonthDay {
			days = append(days, fmt.Sprintf("%d", d))
		}
		parts = append(parts, fmt.Sprintf("BYMONTHDAY=%s", strings.Join(days, ",")))
	}
	if len(r.ByMonth) > 0 {
		var months []string
		for _, m := range r.ByMonth {
			months = append(months, fmt.Sprintf("%d", m))
		}
		parts = append(parts, fmt.Sprintf("BYMONTH=%s", strings.Join(months, ",")))
	}
	if r.Count != nil {
		parts = append(parts, fmt.Sprintf("COUNT=%d", *r.Count))
	}
	if r.Until != nil {
		parts = append(parts, fmt.Sprintf("UNTIL=%s", *r.Until))
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

	// Group components: Masters vs Exceptions
	var masters []ical.Event
	exceptions := make(map[string]ical.Event)
	for i := range allEvents {
		rid := allEvents[i].Props.Get(ical.PropRecurrenceID)
		if rid == nil {
			masters = append(masters, allEvents[i])
		} else {
			// Normalize rid string for matching (usually UTC format is best)
			exceptions[rid.Value] = allEvents[i]
		}
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

		// Collect EXDATES in UTC for this master
		exMap := make(map[string]bool)
		for _, p := range master.Props["EXDATE"] {
			for _, val := range strings.Split(p.Value, ",") {
				val = strings.TrimSpace(val)
				if val == "" {
					continue
				}
				// Try to parse val to normalize to UTC RID format
				var tEx time.Time
				var pErr error
				if strings.HasSuffix(val, "Z") {
					tEx, pErr = time.Parse("20060102T150405Z", val)
				} else {
					tEx, pErr = time.ParseInLocation("20060102T150405", val, loc)
				}
				if pErr == nil {
					exMap[tEx.UTC().Format("20060102T150405Z")] = true
				}
			}
		}

		// Generate within range: rule.Between works best in series timezone
		for _, dt := range rule.Between(start.In(loc), end.In(loc), true) {
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

				if tStart.After(end) || tEnd.Before(start) {
					continue
				}
				instances = append(instances, ToEventInstance(obj, tStart, tEnd, rid, master, &exc))
			} else {
				instances = append(instances, ToEventInstance(obj, dt, dt.Add(mDuration), rid, master, nil))
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
			if (tStart.Before(end) || tStart.Equal(end)) && (tEnd.After(start) || tEnd.Equal(start)) {
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
