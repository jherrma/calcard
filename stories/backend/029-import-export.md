# Story 029: Import/Export Functionality

## Title
Implement Calendar and Contact Import/Export

## Description
As a user, I want to import and export my calendars and contacts so that I can migrate data from other services or create backups.

## Acceptance Criteria

### Calendar Import

- [ ] REST endpoint `POST /api/v1/calendars/{calendar_id}/import` (requires auth)
- [ ] Accept multipart form data with .ics file
- [ ] Accept raw iCalendar data in request body
- [ ] Parse and validate iCalendar format
- [ ] Import all VEVENT and VTODO components
- [ ] Handle duplicate UIDs:
  - [ ] Option: `skip` - Skip existing events (default)
  - [ ] Option: `replace` - Replace existing events
  - [ ] Option: `duplicate` - Create new UIDs
- [ ] Return import summary:
  - [ ] Total events in file
  - [ ] Successfully imported
  - [ ] Skipped (duplicates)
  - [ ] Failed (with reasons)

### Calendar Export

- [ ] Already implemented in Story 013 (`GET /api/v1/calendars/{id}/export`)
- [ ] Verify complete iCalendar output
- [ ] Include all recurrence rules and exceptions

### Full Backup Export

- [ ] REST endpoint `GET /api/v1/users/me/export` (requires auth)
- [ ] Returns ZIP file containing:
  - [ ] All calendars as separate .ics files
  - [ ] All address books as separate .vcf files
  - [ ] Metadata JSON (names, colors, etc.)
- [ ] Content-Disposition: attachment

### Contact Import

- [ ] REST endpoint `POST /api/v1/addressbooks/{addressbook_id}/import` (requires auth)
- [ ] Accept multipart form data with .vcf file
- [ ] Accept raw vCard data in request body
- [ ] Support both vCard 3.0 and 4.0 formats
- [ ] Handle multiple contacts in single file
- [ ] Handle duplicate UIDs:
  - [ ] Option: `skip` - Skip existing contacts (default)
  - [ ] Option: `replace` - Replace existing contacts
  - [ ] Option: `duplicate` - Create new UIDs
- [ ] Return import summary

### Contact Export

- [ ] Already implemented in Story 017 (`GET /api/v1/addressbooks/{id}/export`)
- [ ] Verify complete vCard output

### Import from URL

- [ ] REST endpoint `POST /api/v1/calendars/{calendar_id}/import-url` (requires auth)
- [ ] Request body:
  ```json
  {
    "url": "https://example.com/calendar.ics",
    "duplicate_handling": "skip"
  }
  ```
- [ ] Fetch iCalendar from URL
- [ ] Validate URL (HTTPS only in production)
- [ ] Timeout after 30 seconds
- [ ] Maximum file size: 10MB

## Technical Notes

### Import Request
```go
type ImportRequest struct {
    File              *multipart.FileHeader `form:"file"`
    Data              string                `json:"data"` // Raw iCal/vCard
    DuplicateHandling string                `json:"duplicate_handling"` // skip, replace, duplicate
}

type ImportResult struct {
    Total     int            `json:"total"`
    Imported  int            `json:"imported"`
    Skipped   int            `json:"skipped"`
    Failed    int            `json:"failed"`
    Errors    []ImportError  `json:"errors,omitempty"`
}

type ImportError struct {
    Index   int    `json:"index"`
    UID     string `json:"uid,omitempty"`
    Summary string `json:"summary,omitempty"`
    Error   string `json:"error"`
}
```

### Calendar Import Logic
```go
func (uc *ImportCalendarUseCase) Execute(ctx context.Context, calendarID uint, data []byte, opts ImportOptions) (*ImportResult, error) {
    // Parse iCalendar
    cal, err := ical.ParseCalendar(bytes.NewReader(data))
    if err != nil {
        return nil, fmt.Errorf("invalid iCalendar format: %w", err)
    }

    result := &ImportResult{}
    events := cal.Events()
    result.Total = len(events)

    for i, event := range events {
        uid := event.Props.Get(ical.PropUID)
        if uid == nil {
            result.Failed++
            result.Errors = append(result.Errors, ImportError{
                Index: i,
                Error: "Missing UID property",
            })
            continue
        }

        // Check for existing event
        existing, _ := uc.eventRepo.FindByUID(ctx, calendarID, uid.Value)
        if existing != nil {
            switch opts.DuplicateHandling {
            case "skip":
                result.Skipped++
                continue
            case "replace":
                // Delete existing, will create new
                uc.eventRepo.Delete(ctx, existing.ID)
            case "duplicate":
                // Generate new UID
                event.Props.SetText(ical.PropUID, uuid.New().String()+"@imported")
            }
        }

        // Create event
        if err := uc.createEvent(ctx, calendarID, event); err != nil {
            result.Failed++
            result.Errors = append(result.Errors, ImportError{
                Index:   i,
                UID:     uid.Value,
                Summary: event.Props.Get(ical.PropSummary).Value,
                Error:   err.Error(),
            })
            continue
        }

        result.Imported++
    }

    // Update calendar CTag
    uc.calendarRepo.UpdateCTag(ctx, calendarID)

    return result, nil
}
```

### Full Backup Export
```go
func (uc *ExportBackupUseCase) Execute(ctx context.Context, userID uint) (io.Reader, error) {
    buf := new(bytes.Buffer)
    zipWriter := zip.NewWriter(buf)

    // Export calendars
    calendars, _ := uc.calendarRepo.FindByUserID(ctx, userID)
    for _, cal := range calendars {
        events, _ := uc.eventRepo.FindByCalendarID(ctx, cal.ID)
        icalData := generateICalFeed(&cal, events)

        filename := fmt.Sprintf("calendars/%s.ics", sanitizeFilename(cal.Name))
        w, _ := zipWriter.Create(filename)
        w.Write([]byte(icalData))
    }

    // Export address books
    addressBooks, _ := uc.addressBookRepo.FindByUserID(ctx, userID)
    for _, ab := range addressBooks {
        contacts, _ := uc.contactRepo.FindByAddressBookID(ctx, ab.ID)
        vcardData := generateVCardExport(contacts)

        filename := fmt.Sprintf("addressbooks/%s.vcf", sanitizeFilename(ab.Name))
        w, _ := zipWriter.Create(filename)
        w.Write([]byte(vcardData))
    }

    // Export metadata
    metadata := ExportMetadata{
        ExportedAt: time.Now(),
        Calendars:  calendarMetadata(calendars),
        AddressBooks: addressBookMetadata(addressBooks),
    }
    metadataJSON, _ := json.MarshalIndent(metadata, "", "  ")
    w, _ := zipWriter.Create("metadata.json")
    w.Write(metadataJSON)

    zipWriter.Close()
    return buf, nil
}
```

### Code Structure
```
internal/usecase/import/
├── calendar_import.go      # Calendar import
├── contact_import.go       # Contact import
├── url_import.go           # Import from URL
└── backup_export.go        # Full backup export

internal/adapter/http/
└── import_handler.go       # HTTP handlers
```

## API Response Examples

### Calendar Import (200 OK)
```json
{
  "total": 150,
  "imported": 145,
  "skipped": 3,
  "failed": 2,
  "errors": [
    {
      "index": 45,
      "uid": "invalid-event@example.com",
      "summary": "Broken Event",
      "error": "Invalid DTSTART format"
    },
    {
      "index": 89,
      "uid": "missing-end@example.com",
      "summary": "No End Time",
      "error": "Event must have DTEND or DURATION"
    }
  ]
}
```

### Contact Import (200 OK)
```json
{
  "total": 500,
  "imported": 498,
  "skipped": 0,
  "failed": 2,
  "errors": [
    {
      "index": 123,
      "error": "Missing required FN property"
    },
    {
      "index": 456,
      "error": "Invalid vCard format"
    }
  ]
}
```

### Import from URL (200 OK)
```json
{
  "source_url": "https://calendar.google.com/calendar/ical/xxx/public/basic.ics",
  "total": 75,
  "imported": 75,
  "skipped": 0,
  "failed": 0
}
```

### Full Backup Export (200 OK)
```
Content-Type: application/zip
Content-Disposition: attachment; filename="caldav-backup-2024-01-21.zip"

[ZIP binary data]

Archive contents:
├── calendars/
│   ├── Personal.ics
│   └── Work.ics
├── addressbooks/
│   ├── Contacts.vcf
│   └── Work Contacts.vcf
└── metadata.json
```

### Metadata JSON Example
```json
{
  "exported_at": "2024-01-21T10:00:00Z",
  "version": "1.0",
  "calendars": [
    {
      "name": "Personal",
      "color": "#3788d8",
      "timezone": "America/New_York",
      "event_count": 150
    },
    {
      "name": "Work",
      "color": "#ff5733",
      "timezone": "UTC",
      "event_count": 75
    }
  ],
  "addressbooks": [
    {
      "name": "Contacts",
      "contact_count": 500
    },
    {
      "name": "Work Contacts",
      "contact_count": 45
    }
  ]
}
```

### Import Validation Error (400)
```json
{
  "error": "validation_error",
  "message": "Invalid file format",
  "details": "Expected iCalendar format, got: text/plain"
}
```

### Import File Too Large (413)
```json
{
  "error": "payload_too_large",
  "message": "File exceeds maximum size of 10MB"
}
```

## Definition of Done

- [ ] `POST /api/v1/calendars/{id}/import` imports iCalendar files
- [ ] `POST /api/v1/addressbooks/{id}/import` imports vCard files
- [ ] `POST /api/v1/calendars/{id}/import-url` imports from URL
- [ ] Duplicate handling options work correctly
- [ ] Import summary shows success/skip/fail counts
- [ ] `GET /api/v1/users/me/export` downloads complete backup ZIP
- [ ] Backup includes all calendars, contacts, and metadata
- [ ] File size limits enforced
- [ ] Invalid formats rejected with clear errors
- [ ] Unit tests for import parsing
- [ ] Integration tests for import/export flow
