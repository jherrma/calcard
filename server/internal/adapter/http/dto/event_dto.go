package dto

import (
	"time"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

type CreateEventRequest struct {
	Summary     string             `json:"summary" validate:"required"`
	Description string             `json:"description"`
	Location    string             `json:"location"`
	Start       time.Time          `json:"start" validate:"required"`
	End         time.Time          `json:"end" validate:"required"`
	Timezone    string             `json:"timezone"`
	AllDay      bool               `json:"all_day"`
	Recurrence  *RecurrenceRuleDTO `json:"recurrence"`
}

type RecurrenceRuleDTO struct {
	Frequency  string   `json:"frequency"`
	Interval   int      `json:"interval"`
	ByDay      []string `json:"by_day"`
	ByMonthDay []int    `json:"by_month_day"`
	ByMonth    []int    `json:"by_month"`
	Until      *string  `json:"until"`
	Count      *int     `json:"count"`
}

// ToDomain maps the transport DTO onto the domain RecurrenceRule, copying every
// field verbatim. It lets both the create and update paths funnel through the
// domain's single canonical RRULE renderer (calendar.RecurrenceRule.ToRRule),
// so there is exactly one place that decides the UNTIL value type.
func (r *RecurrenceRuleDTO) ToDomain() *calendar.RecurrenceRule {
	if r == nil {
		return nil
	}
	return &calendar.RecurrenceRule{
		Frequency:  r.Frequency,
		Interval:   r.Interval,
		ByDay:      r.ByDay,
		ByMonthDay: r.ByMonthDay,
		ByMonth:    r.ByMonth,
		Count:      r.Count,
		Until:      r.Until,
	}
}

// ToRRule renders the DTO as an iCal RRULE string by delegating to the domain
// renderer. allDay selects the value type used for the UNTIL boundary: RFC 5545
// §3.3.10 requires UNTIL to share the series' DTSTART value type, so an all-day
// (VALUE=DATE) series must emit a bare DATE UNTIL while a timed series emits a
// UTC DATE-TIME. A mismatched type makes strict clients (Apple, DAVx5) reject
// the whole RRULE. The create path relies on this; the update path renders in
// the use case (via ToDomain) where the *effective* all-day state is known.
func (r *RecurrenceRuleDTO) ToRRule(allDay bool) string {
	return r.ToDomain().ToRRule(allDay)
}

type UpdateEventRequest struct {
	Summary     *string            `json:"summary"`
	Description *string            `json:"description"`
	Location    *string            `json:"location"`
	Start       *string            `json:"start"`
	End         *string            `json:"end"`
	Timezone    *string            `json:"timezone"`
	AllDay      *bool              `json:"all_day"`
	Recurrence  *RecurrenceRuleDTO `json:"recurrence"`
}

type MoveEventRequest struct {
	TargetCalendarID string `json:"target_calendar_id" validate:"required"`
}

type EventResponse struct {
	ID           string             `json:"id"`
	CalendarID   uint               `json:"calendar_id"`
	UID          string             `json:"uid"`
	Summary      string             `json:"summary"`
	Description  string             `json:"description"`
	Location     string             `json:"location"`
	Start        time.Time          `json:"start"`
	End          time.Time          `json:"end"`
	IsAllDay     bool               `json:"all_day"`
	IsRecurring  bool               `json:"is_recurring"`
	RecurrenceID *string            `json:"recurrence_id"`
	Recurrence   *RecurrenceRuleDTO `json:"recurrence,omitempty"`
}

type EventListResponse struct {
	Events []EventResponse `json:"events"`
	Count  int             `json:"count"`
}
