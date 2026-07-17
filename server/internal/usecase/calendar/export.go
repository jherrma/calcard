package calendar

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

// ExportCalendarUseCase handles calendar export as iCalendar
type ExportCalendarUseCase struct {
	repo calendar.CalendarRepository
}

// NewExportCalendarUseCase creates a new use case
func NewExportCalendarUseCase(repo calendar.CalendarRepository) *ExportCalendarUseCase {
	return &ExportCalendarUseCase{repo: repo}
}

// Execute exports a calendar as iCalendar format
func (uc *ExportCalendarUseCase) Execute(ctx context.Context, userID uint, calendarUUID string) (string, string, error) {
	// Get calendar
	cal, err := uc.repo.GetByUUID(ctx, calendarUUID)
	if err != nil {
		return "", "", fmt.Errorf("calendar not found")
	}

	// Verify ownership
	if cal.UserID != userID {
		return "", "", fmt.Errorf("access denied")
	}

	// Fetch all calendar objects (events/todos)
	objects, err := uc.repo.GetCalendarObjects(ctx, cal.ID)
	if err != nil {
		return "", "", fmt.Errorf("failed to fetch calendar objects: %w", err)
	}

	// Build iCalendar content. Escape calendar-supplied text per RFC 5545 so a
	// name/description with ';', ',', '\' or newlines can't corrupt the feed.
	icalContent := fmt.Sprintf("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//CalDAV Server//EN\r\nCALSCALE:GREGORIAN\r\nX-WR-CALNAME:%s\r\nX-WR-TIMEZONE:%s\r\n",
		escapeICalText(cal.Name), escapeICalText(cal.Timezone))

	if cal.Description != "" {
		icalContent += fmt.Sprintf("X-WR-CALDESC:%s\r\n", escapeICalText(cal.Description))
	}

	// Add all calendar objects (events/todos). obj.ICalData is now stored as a
	// full VCALENDAR (import/PUT wrap it), so strip that wrapper and append only
	// the VEVENT/VTODO block — otherwise we'd nest a VCALENDAR inside this one
	// and produce an unparseable feed. Per-object VTIMEZONEs are deduped by TZID
	// and hoisted to the top so we don't emit N identical VTIMEZONE blocks.
	icalContent += calendar.ConcatObjectsDedupVTimezones(objects)

	icalContent += "END:VCALENDAR\r\n"

	// Generate filename (sanitize calendar name for filesystem)
	filename := fmt.Sprintf("%s.ics", sanitizeFilename(cal.Name))

	return icalContent, filename, nil
}

// sanitizeFilename removes characters that are not safe for filenames
func sanitizeFilename(name string) string {
	// Replace common problematic characters
	replacer := map[rune]rune{
		'/':  '-',
		'\\': '-',
		':':  '-',
		'*':  '-',
		'?':  '-',
		'"':  '-',
		'<':  '-',
		'>':  '-',
		'|':  '-',
	}

	result := []rune(name)
	for i, r := range result {
		if replacement, ok := replacer[r]; ok {
			result[i] = replacement
		}
	}
	return string(result)
}

// generateTimestamp returns current timestamp in iCalendar format
func generateTimestamp() string {
	return time.Now().UTC().Format("20060102T150405Z")
}

// escapeICalText escapes a string per RFC 5545 text rules for property values.
func escapeICalText(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, ";", "\\;")
	s = strings.ReplaceAll(s, ",", "\\,")
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", "\\n")
	return s
}
