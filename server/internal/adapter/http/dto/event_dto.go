package dto

import (
	"fmt"
	"strings"
	"time"
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

func (r *RecurrenceRuleDTO) ToRRule() string {
	if r == nil || r.Frequency == "" {
		return ""
	}

	parts := []string{"FREQ=" + strings.ToUpper(r.Frequency)}
	if r.Interval > 1 {
		parts = append(parts, fmt.Sprintf("INTERVAL=%d", r.Interval))
	}
	if len(r.ByDay) > 0 {
		parts = append(parts, "BYDAY="+strings.Join(r.ByDay, ","))
	}
	if len(r.ByMonthDay) > 0 {
		var days []string
		for _, d := range r.ByMonthDay {
			days = append(days, fmt.Sprintf("%d", d))
		}
		parts = append(parts, "BYMONTHDAY="+strings.Join(days, ","))
	}
	if len(r.ByMonth) > 0 {
		var months []string
		for _, m := range r.ByMonth {
			months = append(months, fmt.Sprintf("%d", m))
		}
		parts = append(parts, "BYMONTH="+strings.Join(months, ","))
	}
	if r.Count != nil {
		parts = append(parts, fmt.Sprintf("COUNT=%d", *r.Count))
	}
	if r.Until != nil {
		parts = append(parts, "UNTIL="+*r.Until)
	}

	return strings.Join(parts, ";")
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
