package event

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-ical"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/teambition/rrule-go"
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
	Timezone     *string
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
		// Normalize the requested instance to a canonical UTC key so an
		// exception stored with a TZID= or floating RECURRENCE-ID still matches.
		occStart, err := calendar.ParseRecurrenceIDString(input.RecurrenceID)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
		}
		wantKey := occStart.UTC().Format(calendar.RecurrenceIDLayout)
		docLoc := seriesLocation(allEvents)

		// Look for an existing exception with this RECURRENCE-ID
		for i := range allEvents {
			rid := allEvents[i].Props.Get(ical.PropRecurrenceID)
			if rid == nil {
				continue
			}
			key, err := calendar.RecurrenceIDKeyFromProp(rid, docLoc)
			if err != nil {
				// Can't read this exception's RECURRENCE-ID, so we can't
				// confidently match it to the requested instance — skip
				// it rather than pretend the raw value is canonical. A new
				// canonical exception is created below if none matches.
				continue
			}
			if key == wantKey {
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
			// Default the exception's DTSTART/DTEND to its own occurrence time
			// (RECURRENCE-ID + the master's duration) so a summary-only edit
			// keeps this instance in place instead of snapping to the series'
			// first occurrence. input.Start/End (handled below) still override.
			occDuration := time.Hour
			if mStart, err := master.DateTimeStart(time.UTC); err == nil {
				if mEnd, err := master.DateTimeEnd(time.UTC); err == nil && mEnd.After(mStart) {
					occDuration = mEnd.Sub(mStart)
				}
			}
			if obj.IsAllDay {
				event.Props.SetDate(ical.PropDateTimeStart, occStart)
				event.Props.SetDate(ical.PropDateTimeEnd, occStart.Add(occDuration))
			} else {
				event.Props.SetDateTime(ical.PropDateTimeStart, occStart)
				event.Props.SetDateTime(ical.PropDateTimeEnd, occStart.Add(occDuration))
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

		// 2. Format split time for UNTIL (one second before split). Fail loudly
		// on an unparseable RECURRENCE-ID rather than splitting at the zero time
		// (which would write UNTIL=0001-… and wipe the series). The helper also
		// accepts the VALUE=DATE form, so all-day splits work.
		splitTime, err := calendar.ParseRecurrenceIDString(input.RecurrenceID)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
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
			// Count how many instances the old (truncated) master produces
			// so the new master's COUNT can be reduced accordingly.
			oldMasterInstances := 0
			if mStart, err := master.DateTimeStart(time.UTC); err == nil {
				truncatedRule := master.Props.Get(ical.PropRecurrenceRule)
				if truncatedRule != nil {
					if r, err := rrule.StrToRRule(truncatedRule.Value); err == nil {
						r.DTStart(mStart)
						// Count instances up to a generous horizon.
						oldMasterInstances = len(r.Between(
							mStart.Add(-time.Second), splitTime, true))
					}
				}
			}

			// Build the new master's RRULE from the original, stripping
			// UNTIL (the new series runs from splitTime forward) and
			// adjusting COUNT so the total stays correct.
			parts := strings.Split(originalRRule, ";")
			var filtered []string
			for _, p := range parts {
				if strings.HasPrefix(p, "UNTIL=") {
					continue
				}
				if strings.HasPrefix(p, "COUNT=") && oldMasterInstances > 0 {
					if orig, err := strconv.Atoi(strings.TrimPrefix(p, "COUNT=")); err == nil {
						remaining := orig - oldMasterInstances
						if remaining < 1 {
							remaining = 1
						}
						filtered = append(filtered, fmt.Sprintf("COUNT=%d", remaining))
						continue
					}
				}
				filtered = append(filtered, p)
			}
			newMaster.Props.Set(&ical.Prop{
				Name:  ical.PropRecurrenceRule,
				Value: strings.Join(filtered, ";"),
			})
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
		docLoc := seriesLocation(allEvents)
		var newChildren []*ical.Component
		for _, child := range cal.Children {
			keep := true
			if child.Name == "VEVENT" {
				if rid := child.Props.Get(ical.PropRecurrenceID); rid != nil {
					if t, err := rid.DateTime(docLoc); err == nil && !t.UTC().Before(splitTime) {
						keep = false // drop exceptions at/after the split
					}
					// On parse failure keep the component — never silently
					// drop an exception we can't read.
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
		// Denormalized obj.Summary is rederived by PopulateDenormFieldsFromICal
		// after re-encoding; only the iCal property needs setting here.
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
		} else {
			return nil, fmt.Errorf("%w: invalid start time format: %v", ErrInvalidInput, err)
		}
	}

	if input.End != nil {
		if t, err := time.Parse(time.RFC3339, *input.End); err == nil {
			effectiveEnd = t
		} else {
			return nil, fmt.Errorf("%w: invalid end time format: %v", ErrInvalidInput, err)
		}
	}

	// Convert times to the named IANA timezone so go-ical produces TZID parameters
	if input.Timezone != nil && *input.Timezone != "" {
		if loc, err := time.LoadLocation(*input.Timezone); err == nil {
			effectiveStart = effectiveStart.In(loc)
			effectiveEnd = effectiveEnd.In(loc)
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

	if effectiveEnd.Before(effectiveStart) {
		return nil, fmt.Errorf("%w: end time is before start time", ErrInvalidInput)
	}

	if input.RRule != nil {
		// RRULE usually only makes sense on the master event
		if input.Scope == "all" {
			if *input.RRule == "" {
				targetEvent.Props.Del(ical.PropRecurrenceRule)
			} else {
				if _, err := rrule.StrToRRule(*input.RRule); err != nil {
					return nil, fmt.Errorf("%w: invalid recurrence rule: %v", ErrInvalidInput, err)
				}
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
	// Rederive all denormalized columns (incl. recurrence_end_time) and bump the
	// ETag so DAV clients re-fetch the edited event.
	if err := obj.PopulateDenormFieldsFromICal(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	obj.ETag = calendar.NewETag()

	err = uc.calendarRepo.UpdateCalendarObject(ctx, obj)
	if err != nil {
		return nil, err
	}

	return obj, nil
}

// seriesLocation returns the base timezone of a recurring series — the
// location of the first master's DTSTART — defaulting to UTC. RECURRENCE-ID /
// EXDATE values written without an explicit TZID are interpreted in this zone.
func seriesLocation(events []ical.Event) *time.Location {
	for i := range events {
		if events[i].Props.Get(ical.PropRecurrenceID) != nil {
			continue
		}
		if p := events[i].Props.Get(ical.PropDateTimeStart); p != nil {
			if t, err := p.DateTime(time.UTC); err == nil {
				return t.Location()
			}
		}
		break
	}
	return time.UTC
}
