package task

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

// Validation of the writable task fields (spec §3.1). Every refusal is an
// ErrInvalidInput whose message is the 400 body; create and update share
// them (update adds the checks against the stored times, update.go).

// maxSummaryRunes bounds a task's title, counted in runes after trimming.
const maxSummaryRunes = 500

// taskSummary validates a title: required, not blank, at most maxSummaryRunes
// runes. It returns the title without surrounding white space, the form that
// is stored.
func taskSummary(summary string) (string, error) {
	s := strings.TrimSpace(summary)
	if s == "" {
		return "", invalidInputf("summary is required")
	}
	if utf8.RuneCountInString(s) > maxSummaryRunes {
		return "", invalidInputf("summary must be at most %d characters", maxSummaryRunes)
	}
	if hasControlChar(s) {
		return "", invalidInputf("summary must not contain control characters")
	}
	return s, nil
}

// taskDescription validates a description: any text without control
// characters (a line break is fine).
func taskDescription(description string) error {
	if hasControlChar(description) {
		return invalidInputf("description must not contain control characters")
	}
	return nil
}

// hasControlChar reports a C0 control character other than TAB, CR and LF.
// RFC 5545 §3.3.11 keeps them out of TEXT, and a NUL makes PostgreSQL refuse
// the row. CR and LF are line breaks, stored as the iCalendar \n escape.
func hasControlChar(s string) bool {
	for _, r := range s {
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
			return true
		}
	}
	return false
}

// taskStatuses are the STATUS values a task can be given, in the order the
// error message names them. Their wire form is the lower-case spelling.
var taskStatuses = []string{
	calendar.TaskStatusNeedsAction,
	calendar.TaskStatusInProcess,
	calendar.TaskStatusCompleted,
	calendar.TaskStatusCancelled,
}

// taskStatus maps a wire status (needs-action, in-process, completed,
// cancelled; compared exactly, like a list's status filter) to the STATUS
// value it stores. Empty is needs-action, the default.
func taskStatus(status string) (string, error) {
	if status == "" {
		return calendar.TaskStatusNeedsAction, nil
	}
	for _, s := range taskStatuses {
		if status == strings.ToLower(s) {
			return s, nil
		}
	}
	return "", invalidInputf(`status must be "needs-action", "in-process", "completed" or "cancelled"`)
}

// checkPriority accepts no priority (nil) or the RFC 5545 range 0..9, where 0
// means undefined and 1 is the highest.
func checkPriority(p *int) error {
	if p != nil && (*p < 0 || *p > 9) {
		return invalidInputf("priority must be between 0 and 9")
	}
	return nil
}

// checkPercent accepts no percentage (nil) or 0..100.
func checkPercent(p *int) error {
	if p != nil && (*p < 0 || *p > 100) {
		return invalidInputf("percent_complete must be between 0 and 100")
	}
	return nil
}

// taskTime is a parsed due or start value: a calendar date (allDay, at
// midnight UTC) or an instant.
type taskTime struct {
	t      time.Time
	allDay bool
}

// parseTaskTime reads the due or start value named field. Empty means none
// (nil). With allDay the value must be a date ("YYYY-MM-DD"), otherwise an RFC
// 3339 date-time. One flag decides the value type of DUE and DTSTART alike,
// so the two always match (RFC 5545 §3.8.2.3).
func parseTaskTime(field, value string, allDay bool) (*taskTime, error) {
	if value == "" {
		return nil, nil
	}
	if allDay {
		t, err := time.Parse(time.DateOnly, value)
		if err != nil {
			return nil, invalidInputf("%s must be a date (YYYY-MM-DD) when due_all_day is true", field)
		}
		return &taskTime{t: t, allDay: true}, nil
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, invalidInputf("%s must be an RFC 3339 date-time unless due_all_day is true", field)
	}
	return &taskTime{t: t}, nil
}

// checkDueAfterStart refuses a due date before the start; equal is allowed.
// Either may be absent.
func checkDueAfterStart(due, start *taskTime) error {
	if due != nil && start != nil && due.t.Before(start.t) {
		return invalidInputf("due must not be before start")
	}
	return nil
}
