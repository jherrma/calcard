package importexport

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

// contactPageSize bounds how many contacts are loaded per ListObjects call
// while exporting an address book. Paging keeps peak memory proportional to a
// single page (including any hydrated PHOTO blobs) instead of the whole book:
// loading everything at once (limit -1) is what made a large account balloon
// memory during export.
const contactPageSize = 100

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

// Filename returns the download filename for a backup archive. It depends only
// on the current date, so the HTTP handler can compute it up front to set the
// Content-Disposition header BEFORE streaming starts — once the response body
// begins streaming, the status line and headers are already committed and can
// no longer change.
func (uc *BackupExportUseCase) Filename() string {
	return fmt.Sprintf("caldav-backup-%s.zip", time.Now().Format("2006-01-02"))
}

// Execute streams a ZIP backup of all the user's data to w and returns the
// download filename (the same value Filename reports). Rather than buffering
// the whole archive in memory, each collection's ZIP entry is written as it is
// read, and contacts are paged (contactPageSize at a time) so peak memory stays
// proportional to a single page instead of the entire account.
//
// Because output is streamed, a mid-stream read/write failure cannot change an
// already-sent HTTP status: the archive is simply truncated at the point of
// failure. Such an error is returned so the caller can log it — a truncated ZIP
// is the accepted failure mode here.
func (uc *BackupExportUseCase) Execute(ctx context.Context, userID uint, w io.Writer) (string, error) {
	archiveName := uc.Filename()
	zipWriter := zip.NewWriter(w)

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
		return archiveName, fmt.Errorf("failed to list calendars: %w", err)
	}

	for _, cal := range calendars {
		objects, err := uc.calendarRepo.GetCalendarObjects(ctx, cal.ID)
		if err != nil {
			continue // Skip calendars with errors
		}

		// Build iCalendar content
		icalContent := buildICalendarExport(cal, objects)

		entryName := uniqueZipEntry(usedNames, "calendars", sanitizeFilename(cal.Name), "ics")
		zw, err := zipWriter.Create(entryName)
		if err != nil {
			continue
		}
		if _, err := io.WriteString(zw, icalContent); err != nil {
			// The write target is the client stream; a failure here means the
			// archive is already truncated. Stop and report it.
			return archiveName, fmt.Errorf("failed to write calendar %q: %w", cal.Name, err)
		}

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
		return archiveName, fmt.Errorf("failed to list address books: %w", err)
	}

	for _, ab := range addressBooks {
		// Page through the contacts, writing each vCard straight into the ZIP
		// entry as it is read. The entry is created lazily on the first
		// successful page so a book whose very first read fails is skipped
		// entirely (preserving the prior "skip books with errors" behavior)
		// rather than leaving behind an empty entry.
		var zw io.Writer
		contactCount := 0
		offset := 0
		for {
			objects, _, err := uc.addressBookRepo.ListObjects(ctx, ab.ID, contactPageSize, offset, "name", "asc")
			if err != nil {
				break // Read error: stop paging this book.
			}
			if zw == nil {
				entryName := uniqueZipEntry(usedNames, "addressbooks", sanitizeFilename(ab.Name), "vcf")
				created, cerr := zipWriter.Create(entryName)
				if cerr != nil {
					break
				}
				zw = created
			}
			for i := range objects {
				data := objects[i].VCardData
				if _, err := io.WriteString(zw, data); err != nil {
					return archiveName, fmt.Errorf("failed to write contact data: %w", err)
				}
				if !strings.HasSuffix(data, "\n") {
					if _, err := io.WriteString(zw, "\r\n"); err != nil {
						return archiveName, fmt.Errorf("failed to write contact data: %w", err)
					}
				}
			}
			contactCount += len(objects)
			if len(objects) < contactPageSize {
				break // Short page: we've read the whole book.
			}
			offset += contactPageSize
		}
		if zw == nil {
			continue // First read (or entry creation) failed; nothing written.
		}

		metadata.AddressBooks = append(metadata.AddressBooks, AddressBookMetadata{
			Name:         ab.Name,
			ContactCount: contactCount,
		})
	}

	// Export metadata
	metadataJSON, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return archiveName, fmt.Errorf("failed to marshal metadata: %w", err)
	}
	mw, err := zipWriter.Create("metadata.json")
	if err != nil {
		return archiveName, fmt.Errorf("failed to create metadata file: %w", err)
	}
	if _, err := mw.Write(metadataJSON); err != nil {
		return archiveName, fmt.Errorf("failed to write metadata: %w", err)
	}

	// Close the ZIP writer to flush the central directory. Without this the
	// streamed archive would be unreadable.
	if err := zipWriter.Close(); err != nil {
		return archiveName, fmt.Errorf("failed to finalize ZIP: %w", err)
	}

	return archiveName, nil
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
