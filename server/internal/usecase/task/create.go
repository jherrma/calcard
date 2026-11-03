package task

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/emersion/go-ical"
	"github.com/google/uuid"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

// CreateTaskInput is a new task (spec §3.1). There is no recurrence: a
// repeating task cannot be created over REST (spec §1c).
type CreateTaskInput struct {
	// Calendar is the task list, already resolved and authorised for writing
	// by the caller. It must store VTODO (calendar.SupportsComponent).
	Calendar *calendar.Calendar
	// Summary is required and not blank; at most 500 runes. Stored trimmed.
	Summary string
	// Description is stored as given; empty writes none.
	Description string
	// Due and Start are optional (empty: none). With DueAllDay both must be a
	// date ("YYYY-MM-DD"), otherwise an RFC 3339 date-time, so DUE and DTSTART
	// always share a value type. Due must not be before Start.
	Due       string
	DueAllDay bool
	Start     string
	// Priority (0..9) and PercentComplete (0..100) are written as given; nil
	// writes none.
	Priority        *int
	PercentComplete *int
	// Status is needs-action (also when empty), in-process, completed or
	// cancelled. Completed also stamps COMPLETED (now) and PERCENT-COMPLETE:100,
	// whatever PercentComplete says.
	Status string
	// Timezone is an IANA zone for a timed Due/Start (TZID plus a VTIMEZONE).
	// Without a usable one (empty, unknown or "Local") they are stored in UTC,
	// as for events.
	Timezone string
}

type CreateTaskUseCase struct {
	calendarRepo calendar.CalendarRepository
	now          func() time.Time
}

// NewCreateTaskUseCase builds the use case. now is the clock for every
// timestamp it writes (DTSTAMP, CREATED, LAST-MODIFIED, COMPLETED); nil means
// time.Now.
func NewCreateTaskUseCase(calendarRepo calendar.CalendarRepository, now func() time.Time) *CreateTaskUseCase {
	if now == nil {
		now = time.Now
	}
	return &CreateTaskUseCase{calendarRepo: calendarRepo, now: now}
}

// Execute validates the input, builds a complete VCALENDAR with one VTODO and
// stores it as a new object of input.Calendar (spec §5 rule 1): path
// "<uuid>.ics", UID "<uuid>@calcard.io", a fresh ETag and the denormalized
// task columns; CreateCalendarObject records the change and the new sync
// token. A calendar that does not store tasks is refused first, with
// ErrInvalidInput "This calendar does not store tasks" (no other correction
// to the request would help); every other refusal is an ErrInvalidInput
// naming the field. Nothing is written on a refusal.
func (uc *CreateTaskUseCase) Execute(ctx context.Context, input CreateTaskInput) (*Task, error) {
	if input.Calendar == nil {
		return nil, errors.New("create task: no calendar given")
	}
	if err := calendar.RequireComponent(input.Calendar, calendar.ComponentToDo); err != nil {
		return nil, invalidInput(err)
	}
	summary, err := taskSummary(input.Summary)
	if err != nil {
		return nil, err
	}
	status, err := taskStatus(input.Status)
	if err != nil {
		return nil, err
	}
	if err := checkPriority(input.Priority); err != nil {
		return nil, err
	}
	if err := checkPercent(input.PercentComplete); err != nil {
		return nil, err
	}
	due, err := parseTaskTime("due", input.Due, input.DueAllDay)
	if err != nil {
		return nil, err
	}
	start, err := parseTaskTime("start", input.Start, input.DueAllDay)
	if err != nil {
		return nil, err
	}
	if err := checkDueAfterStart(due, start); err != nil {
		return nil, err
	}

	taskUUID := uuid.New().String()
	uid := taskUUID + "@calcard.io"
	now := uc.now().UTC()

	cal, todo := newTaskCalendar(uid, now)
	// Text is escaped once (calendar.NewTextProp); the API reads it back
	// unescaped, so the returned summary can be PATCHed back unchanged.
	todo.Props.Set(calendar.NewTextProp(ical.PropSummary, summary))
	if input.Description != "" {
		todo.Props.Set(calendar.NewTextProp(ical.PropDescription, input.Description))
	}
	loc, _ := calendar.LoadEventLocation(input.Timezone)
	if start != nil {
		setTaskTime(todo, ical.PropDateTimeStart, start, loc)
	}
	if due != nil {
		setTaskTime(todo, ical.PropDue, due, loc)
	}
	if input.Priority != nil {
		setIntProp(todo, ical.PropPriority, *input.Priority)
	}
	if input.PercentComplete != nil {
		setIntProp(todo, ical.PropPercentComplete, *input.PercentComplete)
	}
	todo.Props.SetText(ical.PropStatus, status)
	if status == calendar.TaskStatusCompleted {
		markCompleted(todo, now)
	}

	data, err := encodeTaskCalendar(cal)
	if err != nil {
		return nil, fmt.Errorf("encode task: %w", err)
	}
	obj := &calendar.CalendarObject{
		UUID:       taskUUID,
		CalendarID: input.Calendar.ID,
		UID:        uid,
		Path:       taskUUID + ".ics",
		ETag:       calendar.NewETag(),
		ICalData:   data,
	}
	// The object was built here from validated input, so a failure is a
	// server bug, not the caller's to fix.
	if err := obj.PopulateDenormFieldsFromICal(); err != nil {
		return nil, fmt.Errorf("derive task columns: %w", err)
	}
	if err := uc.calendarRepo.CreateCalendarObject(ctx, obj); err != nil {
		return nil, err
	}
	return newTask(obj), nil
}
