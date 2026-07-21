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

	// Collection names are not unique per user, but a ZIP can't hold two entries
	// with the same path. Track the entry names already written so a collision
	// (two calendars both named "Personal", or names that sanitize to the same
	// string like "a/b" and "a:b") gets a deterministic suffix instead of
	// silently overwriting the earlier entry and dropping its data.
	usedNames := make(map[string]bool)

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

		filename := uniqueZipEntry(usedNames, "calendars", sanitizeFilename(cal.Name), "ics")
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

		filename := uniqueZipEntry(usedNames, "addressbooks", sanitizeFilename(ab.Name), "vcf")
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
	// Escape calendar-supplied text per RFC 5545 so a name/description with
	// ';', ',', '\' or newlines can't corrupt the feed inside the backup ZIP.
	sb.WriteString(fmt.Sprintf("X-WR-CALNAME:%s\r\n", escapeICalText(cal.Name)))
	if cal.Timezone != "" {
		sb.WriteString(fmt.Sprintf("X-WR-TIMEZONE:%s\r\n", escapeICalText(cal.Timezone)))
	}
	if cal.Description != "" {
		sb.WriteString(fmt.Sprintf("X-WR-CALDESC:%s\r\n", escapeICalText(cal.Description)))
	}

	// Stored ICalData may already be wrapped in BEGIN:VCALENDAR (that's what
	// event.CreateEventUseCase writes) or be a bare VEVENT block (that's what
	// calendar_import.go writes). Strip any existing wrapper so we don't emit
	// nested VCALENDARs, which no parser understands, and dedup per-object
	// VTIMEZONEs by TZID. The helper (via StripVCalendarWrapper) guarantees a
	// trailing CRLF, so no manual newline fix-up is needed.
	sb.WriteString(calendar.ConcatObjectsDedupVTimezones(objects))

	sb.WriteString("END:VCALENDAR\r\n")
	return sb.String()
}

// uniqueZipEntry returns a ZIP entry name "<dir>/<base>.<ext>" that has not yet
// been recorded in used, disambiguating collisions deterministically. The first
// use of a name is unsuffixed; each subsequent collision gets "-2", "-3", ... so
// two collections that sanitize to the same base ("Personal") become
// "Personal.ics" and "Personal-2.ics" rather than overwriting each other. The
// chosen name is added to used before returning. base must already be sanitized.
func uniqueZipEntry(used map[string]bool, dir, base, ext string) string {
	name := fmt.Sprintf("%s/%s.%s", dir, base, ext)
	for i := 2; used[name]; i++ {
		name = fmt.Sprintf("%s/%s-%d.%s", dir, base, i, ext)
	}
	used[name] = true
	return name
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

// escapeICalText escapes a string per RFC 5545 text rules for property values.
func escapeICalText(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, ";", "\\;")
	s = strings.ReplaceAll(s, ",", "\\,")
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", "\\n")
	return s
}
