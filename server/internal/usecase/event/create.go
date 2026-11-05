package event

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/emersion/go-ical"
	"github.com/google/uuid"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/teambition/rrule-go"
)

type CreateEventInput struct {
	CalendarID  uint
	Summary     string
	Description string
	Location    string
	Start       time.Time
	End         time.Time
	IsAllDay    bool
	RRule       string
	Timezone    string
	// Reminders are the simple reminders to write as VALARMs (#169); MCP passes none.
	Reminders []calendar.ReminderInput
}

type CreateEventUseCase struct {
	calendarRepo calendar.CalendarRepository
}

func NewCreateEventUseCase(calendarRepo calendar.CalendarRepository) *CreateEventUseCase {
	return &CreateEventUseCase{calendarRepo: calendarRepo}
}

func (uc *CreateEventUseCase) Execute(ctx context.Context, input CreateEventInput) (*calendar.CalendarObject, error) {
	// A task list (supported_components "VTODO") does not take events (#170).
	// Checked first: no other correction to the request would make it succeed.
	if _, err := requireEventCalendar(ctx, uc.calendarRepo, input.CalendarID); err != nil {
		return nil, err
	}
	// A missing (or JSON null) start/end arrives as the zero time. Without this
	// check two zero times pass the ordering test below and the event is stored
	// at 0001-01-01 (F13).
	if input.Start.IsZero() {
		return nil, fmt.Errorf("%w: start is required", ErrInvalidInput)
	}
	if input.End.IsZero() {
		return nil, fmt.Errorf("%w: end is required", ErrInvalidInput)
	}
	if input.End.Before(input.Start) {
		return nil, fmt.Errorf("%w: end time is before start time", ErrInvalidInput)
	}
	if input.RRule != "" {
		if _, err := rrule.StrToRRule(input.RRule); err != nil {
			return nil, fmt.Errorf("%w: invalid recurrence rule: %v", ErrInvalidInput, err)
		}
	}

	// A new event has no VALARM to match, so every entry is range-checked
	// against its start (#169, spec §7.3). Nothing is written on a refusal.
	if err := calendar.CheckReminderShape(input.Reminders); err != nil {
		return nil, invalidInput(err)
	}
	if err := calendar.CheckReminderRanges(input.Reminders, input.Start); err != nil {
		return nil, invalidInput(err)
	}

	eventUUID := uuid.New().String()
	eventUID := fmt.Sprintf("%s@%s", eventUUID, calendar.UIDDomain)

	// Convert times to the named IANA timezone so go-ical produces TZID parameters
	// (generateICal adds the matching VTIMEZONE).
	if loc, ok := calendar.LoadEventLocation(input.Timezone); ok {
		input.Start = input.Start.In(loc)
		input.End = input.End.In(loc)
	}
	// Without a usable timezone, an offset-bearing time is stored in UTC rather
	// than with an empty TZID (F22). All-day values keep their wall-clock date.
	if !input.IsAllDay {
		input.Start = icalDateTime(input.Start)
		input.End = icalDateTime(input.End)
	}

	icalData, err := uc.generateICal(input, eventUID)
	if err != nil {
		return nil, fmt.Errorf("failed to generate iCalendar: %w", err)
	}

	obj := &calendar.CalendarObject{
		UUID:       eventUUID,
		CalendarID: input.CalendarID,
		UID:        eventUID,
		Path:       fmt.Sprintf("%s.ics", eventUUID),
		ETag:       calendar.NewETag(),
		ICalData:   icalData,
	}
	// Derive all denormalized columns (incl. recurrence_end_time, so recurring
	// events stay visible past their first occurrence) from the stored data.
	if err := obj.PopulateDenormFieldsFromICal(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}

	err = uc.calendarRepo.CreateCalendarObject(ctx, obj)
	if err != nil {
		return nil, err
	}

	return obj, nil
}

func (uc *CreateEventUseCase) generateICal(input CreateEventInput, uid string) (string, error) {
	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropProductID, calendar.ProductID)
	cal.Props.SetText(ical.PropVersion, "2.0")

	event := ical.NewEvent()
	event.Props.SetText(ical.PropUID, uid)
	// DTSTAMP must be UTC (RFC 5545 §3.8.7.2). go-ical writes the "Z" form only
	// for time.UTC; a time.Now() in time.Local becomes DTSTAMP;TZID=Local:… (F04).
	event.Props.SetDateTime(ical.PropDateTimeStamp, time.Now().UTC())
	// Text is escaped once, by calendar.NewTextProp (RFC 5545 §3.3.11).
	event.Props.Set(calendar.NewTextProp(ical.PropSummary, input.Summary))

	if input.IsAllDay {
		event.Props.SetDate(ical.PropDateTimeStart, input.Start)
		event.Props.SetDate(ical.PropDateTimeEnd, input.End)
	} else {
		event.Props.SetDateTime(ical.PropDateTimeStart, input.Start)
		event.Props.SetDateTime(ical.PropDateTimeEnd, input.End)
	}

	if input.Description != "" {
		event.Props.Set(calendar.NewTextProp(ical.PropDescription, input.Description))
	}
	if input.Location != "" {
		event.Props.Set(calendar.NewTextProp(ical.PropLocation, input.Location))
	}
	if input.RRule != "" {
		event.Props.Set(&ical.Prop{
			Name:  ical.PropRecurrenceRule,
			Value: input.RRule,
		})
	}

	calendar.ApplyReminders(event.Component, input.Reminders, calendar.ReminderKindEvent)

	cal.Children = append(cal.Children, event.Component)
	// RFC 5545 §3.2.19: a TZID must name a VTIMEZONE in the same object.
	calendar.SyncVTimezones(cal)

	var sb strings.Builder
	err := ical.NewEncoder(&sb).Encode(cal)
	if err != nil {
		return "", err
	}

	return sb.String(), nil
}
