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
		sb.WriteString(obj.ICalData)
		if !strings.HasSuffix(obj.ICalData, "\n") {
			sb.WriteString("\r\n")
		}
	}

	sb.WriteString("END:VCALENDAR\r\n")
	return sb.String()
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
