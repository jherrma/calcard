package dto

import (
	"strings"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
	alarmuc "github.com/jherrma/caldav-server/internal/usecase/alarm"
)

// UpcomingAlarmResponse is one alarm of GET /api/v1/alarms/upcoming (#169, spec
// §7.1). Every nullable key is always present. Events fill Start/End (UTC) or
// StartDate for an all-day occurrence; tasks fill Due or DueDate (the
// TaskResponse shapes).
type UpcomingAlarmResponse struct {
	Key           string     `json:"key"`
	TriggerAt     time.Time  `json:"trigger_at"`
	UID           string     `json:"uid"`
	RecurrenceID  *string    `json:"recurrence_id"`
	AlarmIndex    int        `json:"alarm_index"`
	Action        string     `json:"action"`
	Description   *string    `json:"description"`
	ObjectType    string     `json:"object_type"`
	ObjectID      string     `json:"object_id"`
	CalendarID    uint       `json:"calendar_id"`
	CalendarUUID  string     `json:"calendar_uuid"`
	CalendarName  string     `json:"calendar_name"`
	CalendarColor string     `json:"calendar_color"`
	Summary       string     `json:"summary"`
	Location      string     `json:"location"`
	AllDay        bool       `json:"all_day"`
	Start         *time.Time `json:"start"`
	StartDate     *string    `json:"start_date"`
	End           *time.Time `json:"end"`
	Due           *time.Time `json:"due"`
	DueDate       *string    `json:"due_date"`
}

// UpcomingAlarmsResponse is the body of GET /api/v1/alarms/upcoming.
type UpcomingAlarmsResponse struct {
	Alarms     []UpcomingAlarmResponse `json:"alarms"`
	Count      int                     `json:"count"`
	From       time.Time               `json:"from"`
	Until      time.Time               `json:"until"`
	ServerTime time.Time               `json:"server_time"`
	Truncated  bool                    `json:"truncated"`
}

// NewUpcomingAlarmsResponse renders the use case's answer.
func NewUpcomingAlarmsResponse(out *alarmuc.UpcomingOutput) UpcomingAlarmsResponse {
	alarms := make([]UpcomingAlarmResponse, 0, len(out.Alarms))
	for _, a := range out.Alarms {
		alarms = append(alarms, newUpcomingAlarmResponse(a))
	}
	return UpcomingAlarmsResponse{
		Alarms:     alarms,
		Count:      len(alarms),
		From:       out.From.UTC(),
		Until:      out.Until.UTC(),
		ServerTime: out.ServerTime.UTC(),
		Truncated:  out.Truncated,
	}
}

func newUpcomingAlarmResponse(a alarmuc.Upcoming) UpcomingAlarmResponse {
	obj := a.Object
	r := UpcomingAlarmResponse{
		Key:           a.Key,
		TriggerAt:     a.TriggerAt.UTC(),
		UID:           a.UID,
		AlarmIndex:    a.Index,
		Action:        strings.ToLower(a.Action),
		ObjectType:    "event",
		ObjectID:      obj.UUID,
		CalendarID:    a.Calendar.ID,
		CalendarUUID:  a.Calendar.UUID,
		CalendarName:  a.Calendar.Name,
		CalendarColor: a.Calendar.Color,
		Summary:       a.Summary,
		Location:      a.Location,
		AllDay:        obj.IsAllDay,
	}
	if a.RecurrenceID != "" {
		rid := a.RecurrenceID
		r.RecurrenceID = &rid
	}
	if a.Description != "" {
		d := a.Description
		r.Description = &d
	}
	if obj.ComponentType == calendar.ComponentToDo {
		r.ObjectType = "task"
		r.Due, r.DueDate = instantOrDate(obj.DueAt, obj.IsAllDay)
		return r
	}
	// An absolute trigger has no occurrence anchor: the object's own columns.
	start, end := &a.Start, &a.End
	if a.Start.IsZero() {
		start, end = obj.StartTime, obj.EndTime
	} else if a.End.IsZero() {
		end = obj.EndTime
	}
	r.Start, r.StartDate = instantOrDate(nonZero(start), obj.IsAllDay)
	if !obj.IsAllDay {
		r.End = utcPtr(nonZero(end))
	}
	return r
}

func nonZero(t *time.Time) *time.Time {
	if t == nil || t.IsZero() {
		return nil
	}
	return t
}
