package event

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/emersion/go-ical"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

type DeleteEventUseCase struct {
	calendarRepo calendar.CalendarRepository
}

func NewDeleteEventUseCase(calendarRepo calendar.CalendarRepository) *DeleteEventUseCase {
	return &DeleteEventUseCase{calendarRepo: calendarRepo}
}

func (uc *DeleteEventUseCase) Execute(ctx context.Context, uuid string, scope string, recurrenceID string) error {
	obj, err := uc.calendarRepo.GetCalendarObjectByUUID(ctx, uuid)
	if err != nil {
		return err
	}

	if scope == "all" || (scope == "" && recurrenceID == "") {
		return uc.calendarRepo.DeleteCalendarObject(ctx, obj)
	}

	if scope == "this" && recurrenceID != "" {
		// Parse ICalData
		cal, err := ical.NewDecoder(strings.NewReader(obj.ICalData)).Decode()
		if err != nil {
			return fmt.Errorf("failed to parse iCalendar data: %w", err)
		}

		if len(cal.Events()) == 0 {
			return fmt.Errorf("no VEVENT found")
		}

		// Find master event component
		var master *ical.Component
		for _, child := range cal.Children {
			if child.Name == "VEVENT" {
				rid := child.Props.Get("RECURRENCE-ID")
				if rid == nil {
					master = child
					break
				}
			}
		}

		if master == nil {
			return uc.calendarRepo.DeleteCalendarObject(ctx, obj)
		}

		// Add EXDATE to master if not already there
		exists := false
		for _, p := range master.Props["EXDATE"] {
			if p.Value == recurrenceID {
				exists = true
				break
			}
		}
		if !exists {
			master.Props.Add(&ical.Prop{
				Name:  "EXDATE",
				Value: recurrenceID,
			})
		}

		// Also remove any exception VEVENT with this recurrenceID
		var newChildren []*ical.Component
		for _, child := range cal.Children {
			if child.Name == "VEVENT" {
				rid := child.Props.Get("RECURRENCE-ID")
				if rid != nil && rid.Value == recurrenceID {
					continue // skip this exception
				}
			}
			newChildren = append(newChildren, child)
		}
		cal.Children = newChildren

		// Regenerate ICalData
		var sb strings.Builder
		if err := ical.NewEncoder(&sb).Encode(cal); err != nil {
			return fmt.Errorf("failed to encode iCalendar data: %w", err)
		}
		obj.ICalData = sb.String()

		return uc.calendarRepo.UpdateCalendarObject(ctx, obj)
	}

	if scope == "this_and_future" && recurrenceID != "" {
		// TERMINATE SERIES:
		// 1. Parse ICalData
		cal, err := ical.NewDecoder(strings.NewReader(obj.ICalData)).Decode()
		if err != nil {
			return fmt.Errorf("failed to parse iCalendar data: %w", err)
		}

		// 2. Find master event component
		var master *ical.Component
		for _, child := range cal.Children {
			if child.Name == "VEVENT" {
				rid := child.Props.Get("RECURRENCE-ID")
				if rid == nil {
					master = child
					break
				}
			}
		}
		if master == nil {
			return uc.calendarRepo.DeleteCalendarObject(ctx, obj)
		}

		// 3. Format split time for UNTIL (one second before split)
		splitTime, _ := time.Parse("20060102T150405Z", recurrenceID)
		if splitTime.IsZero() {
			splitTime, _ = time.Parse("20060102T150405", recurrenceID)
		}
		untilTime := splitTime.Add(-time.Second)
		untilStr := untilTime.UTC().Format("20060102T150405Z")

		// 4. Update RRULE with UNTIL
		rruleProp := master.Props.Get(ical.PropRecurrenceRule)
		if rruleProp != nil {
			parts := strings.Split(rruleProp.Value, ";")
			var newParts []string
			for _, p := range parts {
				if !strings.HasPrefix(p, "UNTIL=") && !strings.HasPrefix(p, "COUNT=") {
					newParts = append(newParts, p)
				}
			}
			newParts = append(newParts, "UNTIL="+untilStr)
			rruleProp.Value = strings.Join(newParts, ";")
		} else {
			// If no RRULE, it's just a single event.
			// If deleting this and future, and it matches this event's start, it's a full delete.
			return uc.calendarRepo.DeleteCalendarObject(ctx, obj)
		}

		// 5. Cleanup future exceptions
		var newChildren []*ical.Component
		for _, child := range cal.Children {
			if child.Name == "VEVENT" {
				rid := child.Props.Get("RECURRENCE-ID")
				if rid != nil {
					t, _ := time.Parse("20060102T150405Z", rid.Value)
					if !t.Before(splitTime) {
						continue // delete future exceptions
					}
				}
			}
			newChildren = append(newChildren, child)
		}
		cal.Children = newChildren

		// Regenerate ICalData
		var sb strings.Builder
		if err := ical.NewEncoder(&sb).Encode(cal); err != nil {
			return fmt.Errorf("failed to encode iCalendar data: %w", err)
		}
		obj.ICalData = sb.String()

		return uc.calendarRepo.UpdateCalendarObject(ctx, obj)
	}

	return fmt.Errorf("invalid scope or recurrence_id for deletion")
}
