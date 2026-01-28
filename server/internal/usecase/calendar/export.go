package calendar

import (
	"context"
	"fmt"
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

	// Build iCalendar content
	icalContent := fmt.Sprintf("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//CalDAV Server//EN\r\nCALSCALE:GREGORIAN\r\nX-WR-CALNAME:%s\r\nX-WR-TIMEZONE:%s\r\n",
		cal.Name, cal.Timezone)

	if cal.Description != "" {
		icalContent += fmt.Sprintf("X-WR-CALDESC:%s\r\n", cal.Description)
	}

	// Add all calendar objects (events/todos)
	for _, obj := range objects {
		// The ICalData field already contains the complete VEVENT or VTODO block
		// Just append it to the calendar
		icalContent += obj.ICalData
		// Ensure proper line ending
		if len(obj.ICalData) > 0 && obj.ICalData[len(obj.ICalData)-1] != '\n' {
			icalContent += "\r\n"
		}
	}

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
