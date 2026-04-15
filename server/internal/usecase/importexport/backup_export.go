package importexport

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

// BackupExportUseCase handles full user data backup export
type BackupExportUseCase struct {
	calendarRepo    calendar.CalendarRepository
	addressBookRepo addressbook.Repository
}

// NewBackupExportUseCase creates a new backup export use case
func NewBackupExportUseCase(calendarRepo calendar.CalendarRepository, addressBookRepo addressbook.Repository) *BackupExportUseCase {
	return &BackupExportUseCase{
		calendarRepo:    calendarRepo,
		addressBookRepo: addressBookRepo,
	}
}

// Execute generates a ZIP backup of all user data
func (uc *BackupExportUseCase) Execute(ctx context.Context, userID uint) ([]byte, string, error) {
	buf := new(bytes.Buffer)
	zipWriter := zip.NewWriter(buf)

	metadata := ExportMetadata{
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Version:    "1.0",
	}

	// Export calendars
	calendars, err := uc.calendarRepo.ListByUserID(ctx, userID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to list calendars: %w", err)
	}

	for _, cal := range calendars {
		objects, err := uc.calendarRepo.GetCalendarObjects(ctx, cal.ID)
		if err != nil {
			continue // Skip calendars with errors
		}

		// Build iCalendar content
		icalContent := buildICalendarExport(cal, objects)

		filename := fmt.Sprintf("calendars/%s.ics", sanitizeFilename(cal.Name))
		w, err := zipWriter.Create(filename)
		if err != nil {
			continue
		}
		w.Write([]byte(icalContent))

		metadata.Calendars = append(metadata.Calendars, CalendarMetadata{
			Name:       cal.Name,
			Color:      cal.Color,
			Timezone:   cal.Timezone,
			EventCount: len(objects),
		})
	}

	// Export address books
	addressBooks, err := uc.addressBookRepo.ListByUserID(ctx, userID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to list address books: %w", err)
	}

	for _, ab := range addressBooks {
		objects, _, err := uc.addressBookRepo.ListObjects(ctx, ab.ID, -1, 0, "name", "asc")
		if err != nil {
			continue // Skip address books with errors
		}

		// Build vCard content
		var vcardContent strings.Builder
		for _, obj := range objects {
			vcardContent.WriteString(obj.VCardData)
			if !strings.HasSuffix(obj.VCardData, "\n") {
				vcardContent.WriteString("\r\n")
			}
		}

		filename := fmt.Sprintf("addressbooks/%s.vcf", sanitizeFilename(ab.Name))
		w, err := zipWriter.Create(filename)
		if err != nil {
			continue
		}
		w.Write([]byte(vcardContent.String()))

		metadata.AddressBooks = append(metadata.AddressBooks, AddressBookMetadata{
			Name:         ab.Name,
			ContactCount: len(objects),
		})
	}

	// Export metadata
	metadataJSON, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal metadata: %w", err)
	}
	w, err := zipWriter.Create("metadata.json")
	if err != nil {
		return nil, "", fmt.Errorf("failed to create metadata file: %w", err)
	}
	w.Write(metadataJSON)

	// Close ZIP writer
	if err := zipWriter.Close(); err != nil {
		return nil, "", fmt.Errorf("failed to finalize ZIP: %w", err)
	}

	// Generate filename
	filename := fmt.Sprintf("caldav-backup-%s.zip", time.Now().Format("2006-01-02"))

	return buf.Bytes(), filename, nil
}

// buildICalendarExport builds iCalendar content from a calendar and its objects
func buildICalendarExport(cal *calendar.Calendar, objects []*calendar.CalendarObject) string {
	var sb strings.Builder
	sb.WriteString("BEGIN:VCALENDAR\r\n")
	sb.WriteString("VERSION:2.0\r\n")
	sb.WriteString("PRODID:-//CalDAV Server//EN\r\n")
	sb.WriteString("CALSCALE:GREGORIAN\r\n")
	sb.WriteString(fmt.Sprintf("X-WR-CALNAME:%s\r\n", cal.Name))
	if cal.Timezone != "" {
		sb.WriteString(fmt.Sprintf("X-WR-TIMEZONE:%s\r\n", cal.Timezone))
	}
	if cal.Description != "" {
		sb.WriteString(fmt.Sprintf("X-WR-CALDESC:%s\r\n", cal.Description))
	}

	for _, obj := range objects {
		// Stored ICalData may already be wrapped in BEGIN:VCALENDAR (that's
		// what event.CreateEventUseCase writes) or be a bare VEVENT block
		// (that's what calendar_import.go writes). Strip any existing wrapper
		// so we don't emit nested VCALENDARs, which no parser understands.
		sb.WriteString(stripVCalendarWrapper(obj.ICalData))
		if !strings.HasSuffix(obj.ICalData, "\n") {
			sb.WriteString("\r\n")
		}
	}

	sb.WriteString("END:VCALENDAR\r\n")
	return sb.String()
}

// stripVCalendarWrapper removes any BEGIN:VCALENDAR / END:VCALENDAR and its
// header properties (VERSION, PRODID, CALSCALE, X-WR-*) from the given iCal
// payload, returning just the contained VEVENT/VTODO/VJOURNAL/VALARM blocks.
// If the input is already a bare component (no VCALENDAR wrapper), it is
// returned unchanged except for whitespace trimming.
func stripVCalendarWrapper(data string) string {
	// Normalize to \r\n so splitting is predictable.
	data = strings.ReplaceAll(data, "\r\n", "\n")
	lines := strings.Split(data, "\n")

	var out []string
	depth := 0            // how deep we are inside nested VCALENDARs
	componentDepth := 0   // how deep we are inside VEVENT/VTODO/etc.
	for _, line := range lines {
		upper := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case upper == "BEGIN:VCALENDAR":
			depth++
			continue
		case upper == "END:VCALENDAR":
			if depth > 0 {
				depth--
			}
			continue
		case depth > 0 && componentDepth == 0 && isVCalendarHeader(upper):
			// Drop calendar-level header props that belong on the outer wrapper.
			continue
		}
		if strings.HasPrefix(upper, "BEGIN:") && depth > 0 {
			componentDepth++
		}
		if strings.HasPrefix(upper, "END:") && componentDepth > 0 {
			componentDepth--
		}
		out = append(out, line)
	}
	result := strings.Join(out, "\r\n")
	return strings.TrimSpace(result) + "\r\n"
}

func isVCalendarHeader(upperLine string) bool {
	switch {
	case strings.HasPrefix(upperLine, "VERSION:"),
		strings.HasPrefix(upperLine, "PRODID:"),
		strings.HasPrefix(upperLine, "CALSCALE:"),
		strings.HasPrefix(upperLine, "METHOD:"),
		strings.HasPrefix(upperLine, "X-WR-"):
		return true
	}
	return false
}

// sanitizeFilename removes characters that are not safe for filenames
func sanitizeFilename(name string) string {
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
