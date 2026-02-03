# Story 013: Calendar Management

## Title
Implement Calendar Domain Model and REST API

## Description
As a user, I want to create, view, update, and delete calendars so that I can organize my events.

## Related Acceptance Criteria

| ID | Criterion |
|----|-----------|
| CD-3.1.1 | Users have a default calendar created on account creation |
| CD-3.1.2 | Users can create additional calendars |
| CD-3.1.3 | Users can rename calendars |
| CD-3.1.4 | Users can set calendar color |
| CD-3.1.5 | Users can set calendar timezone |
| CD-3.1.6 | Users can delete calendars |
| CD-3.1.7 | Calendar deletion requires confirmation |
| CD-3.1.8 | Users can export calendar as .ics file |

## Acceptance Criteria

### Default Calendar Creation

- [ ] When user account is created, a default calendar is created
- [ ] Default calendar name: "Personal"
- [ ] Default calendar color: `#3788d8` (blue)
- [ ] Default calendar timezone: UTC (or from user profile if available)

### Create Calendar

- [ ] REST endpoint `POST /api/v1/calendars` (requires auth)
- [ ] Request body:
  ```json
  {
    "name": "Work",
    "description": "Work meetings and deadlines",
    "color": "#ff5733",
    "timezone": "America/New_York"
  }
  ```
- [ ] Name is required, max 255 characters
- [ ] Description is optional, max 1000 characters
- [ ] Color must be valid hex color (defaults to random if not provided)
- [ ] Timezone must be valid IANA timezone (defaults to UTC)
- [ ] UUID generated for calendar
- [ ] Path generated from UUID: `{uuid}.ics`
- [ ] Initial sync_token and ctag generated
- [ ] Returns 201 Created with calendar data

### List Calendars

- [ ] REST endpoint `GET /api/v1/calendars` (requires auth)
- [ ] Returns all calendars owned by user
- [ ] Includes calendars shared with user (with `shared: true` flag)
- [ ] Returns:
  - [ ] ID (UUID)
  - [ ] Name
  - [ ] Description
  - [ ] Color
  - [ ] Timezone
  - [ ] Event count
  - [ ] Owner info (for shared calendars)
  - [ ] Permission level (for shared calendars)

### Get Single Calendar

- [ ] REST endpoint `GET /api/v1/calendars/{id}` (requires auth)
- [ ] Returns full calendar details
- [ ] Returns 404 if calendar not found or not accessible

### Update Calendar

- [ ] REST endpoint `PATCH /api/v1/calendars/{id}` (requires auth)
- [ ] Updatable fields: name, description, color, timezone
- [ ] Cannot update calendars shared with read-only permission
- [ ] Returns updated calendar data

### Delete Calendar

- [ ] REST endpoint `DELETE /api/v1/calendars/{id}` (requires auth)
- [ ] Request body (confirmation):
  ```json
  {
    "confirmation": "DELETE"
  }
  ```
- [ ] Returns 400 if confirmation not provided
- [ ] Cannot delete last calendar (user must have at least one)
- [ ] All events in calendar are deleted
- [ ] All shares are revoked
- [ ] Returns 204 No Content

### Export Calendar

- [ ] REST endpoint `GET /api/v1/calendars/{id}/export` (requires auth)
- [ ] Returns complete iCalendar (.ics) file
- [ ] Includes all events with recurrence rules
- [ ] Content-Type: `text/calendar`
- [ ] Content-Disposition: `attachment; filename="{calendar-name}.ics"`

## Technical Notes

### Database Model
```go
type Calendar struct {
    ID                  uint           `gorm:"primaryKey"`
    UUID                string         `gorm:"uniqueIndex;size:36;not null"`
    UserID              uint           `gorm:"index;not null"`
    Path                string         `gorm:"size:255;not null"` // URL path component
    Name                string         `gorm:"size:255;not null"`
    Description         string         `gorm:"size:1000"`
    Color               string         `gorm:"size:7;not null"`   // #RRGGBB
    Timezone            string         `gorm:"size:50;not null"`
    SupportedComponents string         `gorm:"size:100;not null"` // "VEVENT,VTODO"
    SyncToken           string         `gorm:"size:64;not null"`
    CTag                string         `gorm:"size:64;not null"`
    CreatedAt           time.Time
    UpdatedAt           time.Time
    DeletedAt           gorm.DeletedAt `gorm:"index"`
    User                User           `gorm:"foreignKey:UserID"`
    Events              []CalendarObject `gorm:"foreignKey:CalendarID"`
}

type CalendarObject struct {
    ID            uint           `gorm:"primaryKey"`
    UUID          string         `gorm:"uniqueIndex;size:36;not null"`
    CalendarID    uint           `gorm:"index;not null"`
    Path          string         `gorm:"size:255;not null"`
    UID           string         `gorm:"index;size:255;not null"` // iCalendar UID
    ETag          string         `gorm:"size:64;not null"`
    ComponentType string         `gorm:"size:20;not null"` // VEVENT, VTODO
    ICalData      string         `gorm:"type:text;not null"`
    ContentLength int            `gorm:"not null"`
    Summary       string         `gorm:"size:500"`          // Denormalized for search
    StartTime     *time.Time     `gorm:"index"`             // Denormalized
    EndTime       *time.Time
    IsAllDay      bool
    CreatedAt     time.Time
    UpdatedAt     time.Time
    DeletedAt     gorm.DeletedAt `gorm:"index"`
    Calendar      Calendar       `gorm:"foreignKey:CalendarID"`
}
```

### Sync Token Generation
```go
// Generate sync token from timestamp + random component
func generateSyncToken() string {
    timestamp := time.Now().UnixNano()
    random := make([]byte, 8)
    rand.Read(random)
    return fmt.Sprintf("%d-%x", timestamp, random)
}

// CTag can use same format or simple hash
func generateCTag() string {
    return generateSyncToken()
}
```

### Code Structure
```
internal/domain/calendar/
├── calendar.go          # Calendar entity
├── calendar_object.go   # CalendarObject entity
├── repository.go        # Repository interface
└── validation.go        # Validation rules

internal/usecase/calendar/
├── create.go            # Create calendar
├── list.go              # List calendars
├── get.go               # Get calendar
├── update.go            # Update calendar
├── delete.go            # Delete calendar
└── export.go            # Export calendar

internal/adapter/http/
└── calendar_handler.go  # HTTP handlers

internal/adapter/repository/
└── calendar_repo.go     # GORM repository
```

## API Response Examples

### Create Calendar (201 Created)
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440001",
  "name": "Work",
  "description": "Work meetings and deadlines",
  "color": "#ff5733",
  "timezone": "America/New_York",
  "event_count": 0,
  "created_at": "2024-01-21T10:00:00Z",
  "updated_at": "2024-01-21T10:00:00Z",
  "caldav_url": "/dav/calendars/johndoe/550e8400-e29b-41d4-a716-446655440001/"
}
```

### List Calendars (200 OK)
```json
{
  "calendars": [
    {
      "id": "550e8400-e29b-41d4-a716-446655440000",
      "name": "Personal",
      "description": null,
      "color": "#3788d8",
      "timezone": "UTC",
      "event_count": 42,
      "is_default": true,
      "shared": false,
      "created_at": "2024-01-15T10:00:00Z"
    },
    {
      "id": "550e8400-e29b-41d4-a716-446655440001",
      "name": "Work",
      "description": "Work meetings and deadlines",
      "color": "#ff5733",
      "timezone": "America/New_York",
      "event_count": 15,
      "is_default": false,
      "shared": false,
      "created_at": "2024-01-21T10:00:00Z"
    },
    {
      "id": "660e8400-e29b-41d4-a716-446655440002",
      "name": "Team Calendar",
      "color": "#28a745",
      "timezone": "UTC",
      "event_count": 8,
      "shared": true,
      "owner": {
        "id": "770e8400-e29b-41d4-a716-446655440003",
        "display_name": "Jane Smith"
      },
      "permission": "read-write"
    }
  ]
}
```

### Delete Calendar - Missing Confirmation (400)
```json
{
  "error": "validation_error",
  "message": "Please type DELETE to confirm calendar deletion"
}
```

### Delete Calendar - Last Calendar (400)
```json
{
  "error": "bad_request",
  "message": "Cannot delete your last calendar"
}
```

### Export Calendar (200 OK)
```
Content-Type: text/calendar; charset=utf-8
Content-Disposition: attachment; filename="Work.ics"

BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//CalDAV Server//EN
CALSCALE:GREGORIAN
X-WR-CALNAME:Work
X-WR-TIMEZONE:America/New_York
BEGIN:VEVENT
UID:event-uuid-123@caldav.example.com
DTSTAMP:20240121T100000Z
DTSTART:20240122T090000
DTEND:20240122T100000
SUMMARY:Team Meeting
END:VEVENT
END:VCALENDAR
```

## Definition of Done

- [ ] Default calendar created when user registers
- [ ] `POST /api/v1/calendars` creates new calendar
- [ ] `GET /api/v1/calendars` lists all owned and shared calendars
- [ ] `GET /api/v1/calendars/{id}` returns single calendar
- [ ] `PATCH /api/v1/calendars/{id}` updates calendar properties
- [ ] `DELETE /api/v1/calendars/{id}` deletes with confirmation
- [ ] Cannot delete last calendar
- [ ] `GET /api/v1/calendars/{id}/export` returns .ics file
- [ ] Sync token and CTag generated and updated appropriately
- [ ] Unit tests for calendar operations
- [ ] Integration tests for CRUD flow
