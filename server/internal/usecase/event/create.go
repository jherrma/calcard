package event

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/emersion/go-ical"
	"github.com/google/uuid"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
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
}

type CreateEventUseCase struct {
	calendarRepo calendar.CalendarRepository
}

func NewCreateEventUseCase(calendarRepo calendar.CalendarRepository) *CreateEventUseCase {
	return &CreateEventUseCase{calendarRepo: calendarRepo}
}

func (uc *CreateEventUseCase) Execute(ctx context.Context, input CreateEventInput) (*calendar.CalendarObject, error) {
	eventUUID := uuid.New().String()
	eventUID := fmt.Sprintf("%s@calcard.io", eventUUID)

	// Convert times to the named IANA timezone so go-ical produces TZID parameters
	if input.Timezone != "" {
		if loc, err := time.LoadLocation(input.Timezone); err == nil {
			input.Start = input.Start.In(loc)
			input.End = input.End.In(loc)
		}
	}

	icalData, err := uc.generateICal(input, eventUID)
	if err != nil {
		return nil, fmt.Errorf("failed to generate iCalendar: %w", err)
	}

	obj := &calendar.CalendarObject{
		UUID:          eventUUID,
		CalendarID:    input.CalendarID,
		UID:           eventUID,
		Path:          fmt.Sprintf("%s.ics", eventUUID),
		ComponentType: "VEVENT",
		Summary:       input.Summary,
		Description:   input.Description,
		Location:      input.Location,
		StartTime:     &input.Start,
		EndTime:       &input.End,
		IsAllDay:      input.IsAllDay,
		ICalData:      icalData,
	}

	err = uc.calendarRepo.CreateCalendarObject(ctx, obj)
	if err != nil {
		return nil, err
	}

	return obj, nil
}

func (uc *CreateEventUseCase) generateICal(input CreateEventInput, uid string) (string, error) {
	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropProductID, "-//CalCard//EN")
	cal.Props.SetText(ical.PropVersion, "2.0")

	event := ical.NewEvent()
	event.Props.SetText(ical.PropUID, uid)
	event.Props.SetDateTime(ical.PropDateTimeStamp, time.Now())
	event.Props.SetText(ical.PropSummary, input.Summary)

	if input.IsAllDay {
		event.Props.SetDate(ical.PropDateTimeStart, input.Start)
		event.Props.SetDate(ical.PropDateTimeEnd, input.End)
	} else {
		event.Props.SetDateTime(ical.PropDateTimeStart, input.Start)
		event.Props.SetDateTime(ical.PropDateTimeEnd, input.End)
	}

	if input.Description != "" {
		event.Props.SetText(ical.PropDescription, input.Description)
	}
	if input.Location != "" {
		event.Props.SetText(ical.PropLocation, input.Location)
	}
	if input.RRule != "" {
		event.Props.Set(&ical.Prop{
			Name:  ical.PropRecurrenceRule,
			Value: input.RRule,
		})
	}

	cal.Children = append(cal.Children, event.Component)

	var sb strings.Builder
	err := ical.NewEncoder(&sb).Encode(cal)
	if err != nil {
		return "", err
	}

	return sb.String(), nil
}
