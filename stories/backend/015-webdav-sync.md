# Story 015: WebDAV-Sync for Calendars

## Title
Implement WebDAV-Sync Protocol (RFC 6578) for Calendars

## Description
As a DAV client user, I want efficient synchronization using sync-tokens so that my client only downloads changes since the last sync, reducing bandwidth and improving performance.

## Related Acceptance Criteria

| ID | Criterion |
|----|-----------|
| CD-3.4.1 | Server provides sync-token for calendars |
| CD-3.4.2 | REPORT sync-collection returns changes since token |
| CD-3.4.3 | Sync response includes created events |
| CD-3.4.4 | Sync response includes modified events |
| CD-3.4.5 | Sync response includes deleted events (as 404 responses) |
| CD-3.4.6 | Initial sync (no token) returns all events |

## Acceptance Criteria

### Sync Token in Properties

- [ ] `PROPFIND /dav/calendars/{user}/{cal}/` returns:
  - [ ] `sync-token` property with current sync token
  - [ ] Format: `https://caldav.example.com/sync/{token-value}`
- [ ] Sync token changes on any modification to calendar contents
- [ ] Sync token is calendar-specific (not global)

### Sync REPORT - Initial Sync

- [ ] `REPORT sync-collection` without sync-token:
  ```xml
  <sync-collection xmlns="DAV:">
    <sync-token/>
    <sync-level>1</sync-level>
    <prop>
      <getetag/>
      <calendar-data xmlns="urn:ietf:params:xml:ns:caldav"/>
    </prop>
  </sync-collection>
  ```
- [ ] Returns all events in calendar
- [ ] Returns new sync-token for subsequent syncs
- [ ] Each event includes requested properties

### Sync REPORT - Incremental Sync

- [ ] `REPORT sync-collection` with sync-token:
  ```xml
  <sync-collection xmlns="DAV:">
    <sync-token>https://caldav.example.com/sync/abc123</sync-token>
    <sync-level>1</sync-level>
    <prop>
      <getetag/>
      <calendar-data xmlns="urn:ietf:params:xml:ns:caldav"/>
    </prop>
  </sync-collection>
  ```
- [ ] Returns only changes since provided token:
  - [ ] Created events: Full event data with 200 status
  - [ ] Modified events: Full event data with 200 status
  - [ ] Deleted events: href only with 404 status
- [ ] Returns new sync-token in response

### Sync Token Validation

- [ ] Invalid sync-token returns 403 Forbidden with:
  ```xml
  <error xmlns="DAV:">
    <valid-sync-token/>
  </error>
  ```
- [ ] Client should perform full sync on 403 error
- [ ] Old but valid tokens should work (return all changes since that point)

### Change Tracking

- [ ] Track changes in database for sync queries:
  - [ ] Event created: Record creation with sync-token
  - [ ] Event modified: Record modification with new sync-token
  - [ ] Event deleted: Record deletion with sync-token
- [ ] Support configurable retention of change history
- [ ] Prune old change records periodically

## Technical Notes

### Database Model
```go
type SyncChangeLog struct {
    ID             uint      `gorm:"primaryKey"`
    CalendarID     uint      `gorm:"index;not null"`
    ResourcePath   string    `gorm:"size:255;not null"`  // Event path
    ResourceUID    string    `gorm:"size:255"`           // iCal UID
    ChangeType     string    `gorm:"size:20;not null"`   // created, modified, deleted
    SyncToken      string    `gorm:"index;size:64;not null"`
    CreatedAt      time.Time `gorm:"index"`
    Calendar       Calendar  `gorm:"foreignKey:CalendarID"`
}

// Index: (calendar_id, sync_token) for efficient range queries
// Index: (created_at) for cleanup of old records
```

### Sync Token Format
```go
// Token format: timestamp-random for ordering and uniqueness
func generateSyncToken() string {
    timestamp := time.Now().UnixNano()
    random := make([]byte, 8)
    rand.Read(random)
    return fmt.Sprintf("%d-%x", timestamp, random)
}

// Full URL format for DAV compliance
func formatSyncTokenURL(baseURL, token string) string {
    return fmt.Sprintf("%s/sync/%s", baseURL, token)
}

// Extract token value from URL
func parseSyncTokenURL(tokenURL string) (string, error) {
    // Parse "https://caldav.example.com/sync/abc123" -> "abc123"
}
```

### Change Recording
```go
// On event creation
func (r *CalendarRepo) CreateEvent(ctx context.Context, event *CalendarObject) error {
    return r.db.Transaction(func(tx *gorm.DB) error {
        // Create event
        if err := tx.Create(event).Error; err != nil {
            return err
        }

        // Update calendar sync token
        newToken := generateSyncToken()
        if err := tx.Model(&Calendar{}).
            Where("id = ?", event.CalendarID).
            Updates(map[string]interface{}{
                "sync_token": newToken,
                "ctag":       newToken,
            }).Error; err != nil {
            return err
        }

        // Record change
        return tx.Create(&SyncChangeLog{
            CalendarID:   event.CalendarID,
            ResourcePath: event.Path,
            ResourceUID:  event.UID,
            ChangeType:   "created",
            SyncToken:    newToken,
        }).Error
    })
}
```

### Code Structure
```
internal/adapter/webdav/
├── sync.go               # WebDAV-Sync implementation
├── sync_token.go         # Token generation/validation
└── sync_report.go        # REPORT sync-collection handler

internal/adapter/repository/
└── sync_changelog_repo.go  # Change log repository

internal/usecase/calendar/
└── sync.go               # Sync business logic
```

## Response Examples

### Initial Sync Response (207 Multi-Status)
```xml
<?xml version="1.0" encoding="UTF-8"?>
<multistatus xmlns="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">
  <response>
    <href>/dav/calendars/johndoe/work/event1.ics</href>
    <propstat>
      <prop>
        <getetag>"etag1"</getetag>
        <C:calendar-data>BEGIN:VCALENDAR...END:VCALENDAR</C:calendar-data>
      </prop>
      <status>HTTP/1.1 200 OK</status>
    </propstat>
  </response>
  <response>
    <href>/dav/calendars/johndoe/work/event2.ics</href>
    <propstat>
      <prop>
        <getetag>"etag2"</getetag>
        <C:calendar-data>BEGIN:VCALENDAR...END:VCALENDAR</C:calendar-data>
      </prop>
      <status>HTTP/1.1 200 OK</status>
    </propstat>
  </response>
  <sync-token>https://caldav.example.com/sync/1705833600-abc123</sync-token>
</multistatus>
```

### Incremental Sync Response (207 Multi-Status)
```xml
<?xml version="1.0" encoding="UTF-8"?>
<multistatus xmlns="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">
  <!-- New event -->
  <response>
    <href>/dav/calendars/johndoe/work/new-event.ics</href>
    <propstat>
      <prop>
        <getetag>"etag-new"</getetag>
        <C:calendar-data>BEGIN:VCALENDAR...END:VCALENDAR</C:calendar-data>
      </prop>
      <status>HTTP/1.1 200 OK</status>
    </propstat>
  </response>
  <!-- Modified event -->
  <response>
    <href>/dav/calendars/johndoe/work/modified-event.ics</href>
    <propstat>
      <prop>
        <getetag>"etag-modified"</getetag>
        <C:calendar-data>BEGIN:VCALENDAR...END:VCALENDAR</C:calendar-data>
      </prop>
      <status>HTTP/1.1 200 OK</status>
    </propstat>
  </response>
  <!-- Deleted event -->
  <response>
    <href>/dav/calendars/johndoe/work/deleted-event.ics</href>
    <status>HTTP/1.1 404 Not Found</status>
  </response>
  <sync-token>https://caldav.example.com/sync/1705920000-def456</sync-token>
</multistatus>
```

### Invalid Sync Token (403 Forbidden)
```xml
<?xml version="1.0" encoding="UTF-8"?>
<error xmlns="DAV:">
  <valid-sync-token/>
</error>
```

### No Changes (207 Multi-Status)
```xml
<?xml version="1.0" encoding="UTF-8"?>
<multistatus xmlns="DAV:">
  <sync-token>https://caldav.example.com/sync/1705833600-abc123</sync-token>
</multistatus>
```

## Configuration

```
CALDAV_SYNC_HISTORY_DAYS    (default: "30")   # Days to retain change history
CALDAV_SYNC_CLEANUP_INTERVAL (default: "24h") # Cleanup job interval
```

## Cleanup Job

```go
// Run periodically to prune old sync records
func (s *SyncService) CleanupOldChanges(ctx context.Context) error {
    cutoff := time.Now().AddDate(0, 0, -s.config.SyncHistoryDays)

    return s.db.Where("created_at < ?", cutoff).
        Delete(&SyncChangeLog{}).Error
}
```

## Definition of Done

- [ ] PROPFIND returns sync-token property
- [ ] Sync-token updates on calendar modifications
- [ ] REPORT sync-collection without token returns all events
- [ ] REPORT sync-collection with token returns only changes
- [ ] Created events appear in sync response
- [ ] Modified events appear in sync response
- [ ] Deleted events appear as 404 in sync response
- [ ] Invalid sync-token returns 403 with valid-sync-token error
- [ ] Change history recorded for all operations
- [ ] Old change records cleaned up periodically
- [ ] DAVx5 incremental sync works correctly
- [ ] Apple Calendar incremental sync works correctly
- [ ] Unit tests for sync token generation
- [ ] Integration tests for sync scenarios
