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

		// Normalize the requested instance to a canonical UTC value/key so a
		// TZID=/floating RECURRENCE-ID supplied by the client still matches.
		parsed, err := calendar.ParseRecurrenceIDString(recurrenceID)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidInput, err)
		}
		wantKey := parsed.UTC().Format(calendar.RecurrenceIDLayout)
		docLoc := seriesLocation(cal.Events())

		// Add EXDATE (canonical UTC) to master if not already present.
		exists := false
		for _, p := range master.Props["EXDATE"] {
			for _, k := range calendar.EXDATEKeys(&p, docLoc) {
				if k == wantKey {
					exists = true
					break
				}
			}
			if exists {
				break
			}
		}
		if !exists {
			master.Props.Add(&ical.Prop{Name: "EXDATE", Value: wantKey})
		}

		// Also remove any exception VEVENT with this RECURRENCE-ID.
		var newChildren []*ical.Component
		for _, child := range cal.Children {
			if child.Name == "VEVENT" {
				if rid := child.Props.Get("RECURRENCE-ID"); rid != nil {
					key, err := calendar.RecurrenceIDKeyFromProp(rid, docLoc)
					if err != nil {
						// Can't read this exception's RECURRENCE-ID, so we
						// can't confidently target it — keep it rather than
						// silently drop an exception we can't match (mirrors
						// the this_and_future cleanup below).
						newChildren = append(newChildren, child)
						continue
					}
					if key == wantKey {
						continue // skip (remove) this exception
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
		// Rederive denorm columns (recurrence_end_time shrinks when an UNTIL was
		// written) and bump the ETag so DAV clients re-fetch.
		if err := obj.PopulateDenormFieldsFromICal(); err != nil {
			return fmt.Errorf("failed to derive denormalized fields: %w", err)
		}
		obj.ETag = calendar.NewETag()

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

		// 3. Format split time for UNTIL (one second before split). Fail loudly
		// on an unparseable RECURRENCE-ID rather than terminating at the zero
		// time (which would write UNTIL=0001-… and wipe the series). The helper
		// also accepts the VALUE=DATE form, so all-day splits work.
		splitTime, err := calendar.ParseRecurrenceIDString(recurrenceID)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidInput, err)
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
		docLoc := seriesLocation(cal.Events())
		var newChildren []*ical.Component
		for _, child := range cal.Children {
			if child.Name == "VEVENT" {
				if rid := child.Props.Get("RECURRENCE-ID"); rid != nil {
					if t, err := rid.DateTime(docLoc); err == nil && !t.UTC().Before(splitTime) {
						continue // drop exceptions at/after the split
					}
					// On parse failure keep the component.
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
		// Rederive denorm columns (recurrence_end_time shrinks when an UNTIL was
		// written) and bump the ETag so DAV clients re-fetch.
		if err := obj.PopulateDenormFieldsFromICal(); err != nil {
			return fmt.Errorf("failed to derive denormalized fields: %w", err)
		}
		obj.ETag = calendar.NewETag()

		return uc.calendarRepo.UpdateCalendarObject(ctx, obj)
	}

	return fmt.Errorf("invalid scope or recurrence_id for deletion")
}
