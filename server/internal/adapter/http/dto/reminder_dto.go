package dto

import (
	"strings"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

// ReminderResponse is one VALARM of an event or task as the REST API reports it
// (#169, spec §7.3/§7.4). Index is the alarm's 0-based position among the
// component's VALARMs. Action is lower-case ("display" when absent).
// OffsetSeconds is the signed relative trigger (a nominal day is 86400 s), null
// for an absolute or invalid trigger; At the stored absolute trigger in UTC (a
// floating one as its wall clock in UTC), null otherwise. Related is "start" or
// "end" for a relative trigger, else null. Editable says whether the web form
// may change the alarm without losing information.
type ReminderResponse struct {
	Index         int        `json:"index"`
	Action        string     `json:"action"`
	Related       *string    `json:"related"`
	OffsetSeconds *int64     `json:"offset_seconds"`
	At            *time.Time `json:"at"`
	Editable      bool       `json:"editable"`
}

// ReminderKind selects which RELATED value of a relative trigger the web form
// edits: start for events, end for tasks (a task is due at its end).
type ReminderKind string

const (
	ReminderKindEvent ReminderKind = "event"
	ReminderKindTask  ReminderKind = "task"
)

// NewReminderResponses renders specs; it never returns nil, so the wire has
// `[]` for a component without VALARM.
func NewReminderResponses(specs []calendar.AlarmSpec, kind ReminderKind) []ReminderResponse {
	out := make([]ReminderResponse, 0, len(specs))
	for _, s := range specs {
		out = append(out, newReminderResponse(s, kind))
	}
	return out
}

func newReminderResponse(s calendar.AlarmSpec, kind ReminderKind) ReminderResponse {
	r := ReminderResponse{Index: s.Index, Action: "display"}
	if s.Action != "" {
		r.Action = strings.ToLower(s.Action)
	}
	if s.Valid {
		if s.Relative {
			off := int64(s.Offset.Days)*86400 + int64(s.Offset.Secs)
			if s.Offset.Neg {
				off = -off
			}
			r.OffsetSeconds = &off
			rel := strings.ToLower(s.Related)
			if rel != "" {
				r.Related = &rel
			}
		} else if s.Absolute != nil {
			at := s.Absolute.UTC()
			r.At = &at
		}
	}

	wantRelated := "start"
	if kind == ReminderKindTask {
		wantRelated = "end"
	}
	actionOK := s.Action == "" || s.Action == "DISPLAY" || s.Action == "AUDIO"
	if s.Valid && actionOK && !s.Repeat && !s.Snooze {
		switch {
		case s.Relative:
			r.Editable = r.Related != nil && *r.Related == wantRelated
		case s.Absolute != nil:
			r.Editable = !s.Floating
		}
	}
	return r
}
