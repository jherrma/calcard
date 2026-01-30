package event

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/emersion/go-ical"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

type UpdateEventInput struct {
	UUID         string
	Summary      *string
	Description  *string
	Location     *string
	Start        *string // ISO 8601
	End          *string // ISO 8601
	IsAllDay     *bool
	RRule        *string
	RecurrenceID string // Specific instance to update (RFC 5545 format, e.g., 20230101T100000Z)
	Scope        string // this, this_and_future, all
}

type UpdateEventUseCase struct {
	calendarRepo calendar.CalendarRepository
}

func NewUpdateEventUseCase(calendarRepo calendar.CalendarRepository) *UpdateEventUseCase {
	return &UpdateEventUseCase{calendarRepo: calendarRepo}
}

func (uc *UpdateEventUseCase) Execute(ctx context.Context, input UpdateEventInput) (*calendar.CalendarObject, error) {
	obj, err := uc.calendarRepo.GetCalendarObjectByUUID(ctx, input.UUID)
	if err != nil {
		return nil, err
	}

	cal, err := ical.NewDecoder(strings.NewReader(obj.ICalData)).Decode()
	if err != nil {
		return nil, fmt.Errorf("failed to parse iCalendar data: %w", err)
	}

	if len(cal.Events()) == 0 {
		return nil, fmt.Errorf("no VEVENT found in iCalendar data")
	}

	// Find or create the target event component based on scope
	var targetEvent *ical.Event
	allEvents := cal.Events()

	if input.Scope == "this" && input.RecurrenceID != "" {
		// Look for an existing exception with this RECURRENCE-ID
		for i := range allEvents {
			rid := allEvents[i].Props.Get(ical.PropRecurrenceID)
			if rid != nil && rid.Value == input.RecurrenceID {
				targetEvent = &allEvents[i]
				break
			}
		}

		if targetEvent == nil {
			// Create a new exception component
			master := allEvents[0]
			event := ical.NewEvent()
			event.Props.SetText(ical.PropUID, obj.UID)
			event.Props.Set(&ical.Prop{
				Name:  ical.PropRecurrenceID,
				Value: input.RecurrenceID,
			})
			if p := master.Props.Get(ical.PropSummary); p != nil {
				event.Props.Set(p)
			}
			if p := master.Props.Get(ical.PropDescription); p != nil {
				event.Props.Set(p)
			}
			if p := master.Props.Get(ical.PropLocation); p != nil {
				event.Props.Set(p)
			}
			cal.Children = append(cal.Children, event.Component)
			targetEvent = event
		}
	} else if input.Scope == "this_and_future" && input.RecurrenceID != "" {
		// SPLIT SERIES:
		var originalRRule string
		// 1. Find the current master series
		var master *ical.Event
		for i := range allEvents {
			if allEvents[i].Props.Get(ical.PropRecurrenceID) == nil {
				master = &allEvents[i]
				break
			}
		}
		if master == nil {
			return nil, fmt.Errorf("master event not found for split")
		}

		// 2. Format split time for UNTIL (one second before split)
		splitTime, err := time.Parse("20060102T150405Z", input.RecurrenceID)
		if err != nil {
			// Try without Z if necessary, but DTO uses Z
			splitTime, _ = time.Parse("20060102T150405", input.RecurrenceID)
		}
		untilTime := splitTime.Add(-time.Second)
		untilStr := untilTime.UTC().Format("20060102T150405Z")

		// 3. Update old master with UNTIL
		rruleProp := master.Props.Get(ical.PropRecurrenceRule)
		if rruleProp != nil {
			// Capture original RRULE before modification
			originalRRule = rruleProp.Value

			// Remove existing UNTIL/COUNT if any and add new UNTIL
			parts := strings.Split(rruleProp.Value, ";")
			var newParts []string
			for _, p := range parts {
				if !strings.HasPrefix(p, "UNTIL=") && !strings.HasPrefix(p, "COUNT=") {
					newParts = append(newParts, p)
				}
			}
			newParts = append(newParts, "UNTIL="+untilStr)
			rruleProp.Value = strings.Join(newParts, ";")
		}

		// 4. Create NEW master series
		newMaster := ical.NewEvent()
		newMaster.Props.SetText(ical.PropUID, obj.UID)
		// Copy base props from old master
		if p := master.Props.Get(ical.PropSummary); p != nil {
			newMaster.Props.Set(p)
		}
		if p := master.Props.Get(ical.PropDescription); p != nil {
			newMaster.Props.Set(p)
		}
		if p := master.Props.Get(ical.PropLocation); p != nil {
			newMaster.Props.Set(p)
		}
		if originalRRule != "" {
			newMaster.Props.Set(&ical.Prop{
				Name:  ical.PropRecurrenceRule,
				Value: originalRRule,
			})
			// Adjust RRULE for new master (remove UNTIL)
			newRRule := newMaster.Props.Get(ical.PropRecurrenceRule)
			parts := strings.Split(newRRule.Value, ";")
			var filtered []string
			for _, p := range parts {
				if !strings.HasPrefix(p, "UNTIL=") {
					filtered = append(filtered, p)
				}
			}
			newRRule.Value = strings.Join(filtered, ";")
		}

		cal.Children = append(cal.Children, newMaster.Component)
		targetEvent = newMaster

		// 4b. Ensure the new master has valid DTSTART/DTEND from the split point
		// so that if input.Start/End are nil, it still starts at the right place.
		mDuration := time.Hour
		if mStart, err := master.DateTimeStart(time.UTC); err == nil {
			if mEnd, err := master.DateTimeEnd(time.UTC); err == nil {
				mDuration = mEnd.Sub(mStart)
			}
		}
		if obj.IsAllDay {
			targetEvent.Props.SetDate(ical.PropDateTimeStart, splitTime)
			targetEvent.Props.SetDate(ical.PropDateTimeEnd, splitTime.Add(mDuration))
		} else {
			targetEvent.Props.SetDateTime(ical.PropDateTimeStart, splitTime)
			targetEvent.Props.SetDateTime(ical.PropDateTimeEnd, splitTime.Add(mDuration))
		}

		// 5. Cleanup future exceptions that belonged to the old series
		var newChildren []*ical.Component
		for _, child := range cal.Children {
			keep := true
			if child.Name == "VEVENT" {
				rid := child.Props.Get("RECURRENCE-ID")
				if rid != nil {
					t, err := time.Parse("20060102T150405Z", rid.Value)
					err = nil // Force ignore parse error for cleanup
					if err == nil && !t.Before(splitTime) {
						keep = false // delete future exceptions
					}
				}
				// Always keep masters (rid == nil) and past exceptions
			}
			if keep {
				newChildren = append(newChildren, child)
			}
		}
		cal.Children = newChildren

	} else {
		// Default to the first VEVENT (master series)
		targetEvent = &allEvents[0]
	}

	if input.Summary != nil {
		if input.Scope == "all" {
			obj.Summary = *input.Summary
		}
		targetEvent.Props.SetText(ical.PropSummary, *input.Summary)
	}

	if input.Description != nil {
		targetEvent.Props.SetText(ical.PropDescription, *input.Description)
	}

	if input.Location != nil {
		targetEvent.Props.SetText(ical.PropLocation, *input.Location)
	}

	// Determine current effective times
	effectiveStart := time.Now()
	if obj.StartTime != nil {
		effectiveStart = *obj.StartTime
	}
	if p := targetEvent.Props.Get(ical.PropDateTimeStart); p != nil {
		if t, err := p.DateTime(time.UTC); err == nil {
			effectiveStart = t
		}
	}

	effectiveEnd := effectiveStart.Add(time.Hour)
	if obj.EndTime != nil {
		effectiveEnd = *obj.EndTime
	}
	if p := targetEvent.Props.Get(ical.PropDateTimeEnd); p != nil {
		if t, err := p.DateTime(time.UTC); err == nil {
			effectiveEnd = t
		}
	}

	if input.IsAllDay != nil {
		if input.Scope == "all" {
			obj.IsAllDay = *input.IsAllDay
		}
	}

	if input.Start != nil {
		if t, err := time.Parse(time.RFC3339, *input.Start); err == nil {
			effectiveStart = t
			if input.Scope == "all" {
				obj.StartTime = &effectiveStart
			}
		} else {
			return nil, fmt.Errorf("invalid start time format: %w", err)
		}
	}

	if input.End != nil {
		if t, err := time.Parse(time.RFC3339, *input.End); err == nil {
			effectiveEnd = t
			if input.Scope == "all" {
				obj.EndTime = &effectiveEnd
			}
		} else {
			return nil, fmt.Errorf("invalid end time format: %w", err)
		}
	}

	// Always set DTSTART and DTEND on targetEvent to ensure they match obj.IsAllDay format
	if obj.IsAllDay {
		targetEvent.Props.SetDate(ical.PropDateTimeStart, effectiveStart)
		targetEvent.Props.SetDate(ical.PropDateTimeEnd, effectiveEnd)
	} else {
		targetEvent.Props.SetDateTime(ical.PropDateTimeStart, effectiveStart)
		targetEvent.Props.SetDateTime(ical.PropDateTimeEnd, effectiveEnd)
	}

	if input.RRule != nil {
		// RRULE usually only makes sense on the master event
		if input.Scope == "all" {
			if *input.RRule == "" {
				targetEvent.Props.Del(ical.PropRecurrenceRule)
			} else {
				targetEvent.Props.Set(&ical.Prop{
					Name:  ical.PropRecurrenceRule,
					Value: *input.RRule,
				})
			}
		}
	}

	// Update DTSTAMP
	targetEvent.Props.SetDateTime(ical.PropDateTimeStamp, time.Now())

	// Regenerate ICalData
	var sb strings.Builder
	if err := ical.NewEncoder(&sb).Encode(cal); err != nil {
		return nil, fmt.Errorf("failed to encode iCalendar: %w", err)
	}
	obj.ICalData = sb.String()

	err = uc.calendarRepo.UpdateCalendarObject(ctx, obj)
	if err != nil {
		return nil, err
	}

	return obj, nil
}
