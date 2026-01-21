# Story 023: Calendar Sharing

## Title
Implement Calendar Sharing Between Users

## Description
As a user, I want to share my calendars with other users so that we can collaborate on scheduling and see each other's events.

## Related Acceptance Criteria

| ID | Criterion |
|----|-----------|
| SH-5.1.1 | Users can share calendars with other users by username/email |
| SH-5.1.2 | Users can grant read-only access |
| SH-5.1.3 | Users can grant read-write access |
| SH-5.1.4 | Users can view list of shares for their calendars |
| SH-5.1.5 | Users can modify share permissions |
| SH-5.1.6 | Users can revoke shares |
| SH-5.1.7 | Shared calendars appear in recipient's calendar list |
| SH-5.1.8 | Shared calendars are accessible via CalDAV |
| SH-5.1.9 | Changes by one user sync to other shared users |

## Acceptance Criteria

### Create Calendar Share

- [ ] REST endpoint `POST /api/v1/calendars/{calendar_id}/shares` (requires auth)
- [ ] Request body:
  ```json
  {
    "user_identifier": "jane@example.com",
    "permission": "read-write"
  }
  ```
- [ ] User identifier can be username or email
- [ ] Permission: `read` or `read-write`
- [ ] Cannot share with yourself
- [ ] Cannot share same calendar to same user twice
- [ ] Only calendar owner can create shares
- [ ] Returns 201 Created with share details

### List Calendar Shares

- [ ] REST endpoint `GET /api/v1/calendars/{calendar_id}/shares` (requires auth)
- [ ] Only calendar owner can view shares
- [ ] Returns list of shares:
  - [ ] Share ID
  - [ ] Shared with user (ID, username, display name, email)
  - [ ] Permission level
  - [ ] Created date

### Update Calendar Share

- [ ] REST endpoint `PATCH /api/v1/calendars/{calendar_id}/shares/{share_id}` (requires auth)
- [ ] Request body:
  ```json
  {
    "permission": "read"
  }
  ```
- [ ] Only calendar owner can update shares
- [ ] Returns updated share

### Revoke Calendar Share

- [ ] REST endpoint `DELETE /api/v1/calendars/{calendar_id}/shares/{share_id}` (requires auth)
- [ ] Only calendar owner can revoke shares
- [ ] Returns 204 No Content
- [ ] Shared user immediately loses access

### Recipient: View Shared Calendars

- [ ] `GET /api/v1/calendars` includes shared calendars
- [ ] Shared calendars marked with `shared: true`
- [ ] Include owner information
- [ ] Include permission level
- [ ] Shared calendars accessible by ID

### Recipient: Access Shared Calendar

- [ ] Can view events (read permission)
- [ ] Can create/update/delete events (read-write permission)
- [ ] Cannot modify calendar properties (name, color, etc.)
- [ ] Cannot delete calendar
- [ ] Cannot manage shares

### CalDAV Access to Shared Calendars

- [ ] Shared calendars appear in PROPFIND on calendar-home
- [ ] Path: `/dav/calendars/{owner-username}/{calendar-id}/`
- [ ] Shared user can access via their calendar-home
- [ ] ACL properties reflect actual permissions
- [ ] All standard CalDAV operations work within permission level

## Technical Notes

### Database Model
```go
type CalendarShare struct {
    ID           uint      `gorm:"primaryKey"`
    UUID         string    `gorm:"uniqueIndex;size:36;not null"`
    CalendarID   uint      `gorm:"index;not null"`
    SharedWithID uint      `gorm:"index;not null"` // User ID of recipient
    Permission   string    `gorm:"size:20;not null"` // "read" or "read-write"
    CreatedAt    time.Time
    UpdatedAt    time.Time
    Calendar     Calendar  `gorm:"foreignKey:CalendarID"`
    SharedWith   User      `gorm:"foreignKey:SharedWithID"`
}

// Unique constraint: (calendar_id, shared_with_id)
```

### Permission Checking
```go
type CalendarPermission int

const (
    PermissionNone CalendarPermission = iota
    PermissionRead
    PermissionReadWrite
    PermissionOwner
)

func (r *CalendarRepo) GetUserPermission(ctx context.Context, calendarID, userID uint) CalendarPermission {
    // Check ownership
    var calendar Calendar
    if err := r.db.First(&calendar, calendarID).Error; err != nil {
        return PermissionNone
    }
    if calendar.UserID == userID {
        return PermissionOwner
    }

    // Check share
    var share CalendarShare
    err := r.db.Where("calendar_id = ? AND shared_with_id = ?", calendarID, userID).
        First(&share).Error
    if err != nil {
        return PermissionNone
    }

    if share.Permission == "read-write" {
        return PermissionReadWrite
    }
    return PermissionRead
}
```

### Code Structure
```
internal/domain/sharing/
├── calendar_share.go      # CalendarShare entity
└── repository.go          # Repository interface

internal/usecase/sharing/
├── create_calendar_share.go   # Create share
├── list_calendar_shares.go    # List shares
├── update_calendar_share.go   # Update share
└── revoke_calendar_share.go   # Revoke share

internal/adapter/http/
└── calendar_share_handler.go  # HTTP handlers
```

### CalDAV Backend Updates
```go
// ListCalendars returns owned and shared calendars
func (b *CalDAVBackend) ListCalendars(ctx context.Context) ([]caldav.Calendar, error) {
    user := getUserFromContext(ctx)

    // Get owned calendars
    owned, err := b.calendarRepo.FindByUserID(ctx, user.ID)
    if err != nil {
        return nil, err
    }

    // Get shared calendars
    shared, err := b.shareRepo.FindCalendarsSharedWithUser(ctx, user.ID)
    if err != nil {
        return nil, err
    }

    calendars := make([]caldav.Calendar, 0, len(owned)+len(shared))

    for _, cal := range owned {
        calendars = append(calendars, toCalDAVCalendar(cal, PermissionOwner))
    }

    for _, share := range shared {
        perm := PermissionRead
        if share.Permission == "read-write" {
            perm = PermissionReadWrite
        }
        calendars = append(calendars, toCalDAVCalendar(share.Calendar, perm))
    }

    return calendars, nil
}
```

### WebDAV ACL Properties
```go
// Return appropriate current-user-privilege-set based on permission
func getPrivilegeSet(permission CalendarPermission) []string {
    switch permission {
    case PermissionOwner:
        return []string{"read", "write", "write-properties", "write-content", "bind", "unbind", "all"}
    case PermissionReadWrite:
        return []string{"read", "write", "write-content"}
    case PermissionRead:
        return []string{"read"}
    default:
        return []string{}
    }
}
```

## API Response Examples

### Create Share (201 Created)
```json
{
  "id": "aa0e8400-e29b-41d4-a716-446655440001",
  "calendar_id": "550e8400-e29b-41d4-a716-446655440001",
  "shared_with": {
    "id": "660e8400-e29b-41d4-a716-446655440002",
    "username": "janesmith",
    "display_name": "Jane Smith",
    "email": "jane@example.com"
  },
  "permission": "read-write",
  "created_at": "2024-01-21T10:00:00Z"
}
```

### List Shares (200 OK)
```json
{
  "shares": [
    {
      "id": "aa0e8400-e29b-41d4-a716-446655440001",
      "shared_with": {
        "id": "660e8400-e29b-41d4-a716-446655440002",
        "username": "janesmith",
        "display_name": "Jane Smith",
        "email": "jane@example.com"
      },
      "permission": "read-write",
      "created_at": "2024-01-21T10:00:00Z"
    },
    {
      "id": "aa0e8400-e29b-41d4-a716-446655440002",
      "shared_with": {
        "id": "770e8400-e29b-41d4-a716-446655440003",
        "username": "bobwilson",
        "display_name": "Bob Wilson",
        "email": "bob@example.com"
      },
      "permission": "read",
      "created_at": "2024-01-20T14:00:00Z"
    }
  ]
}
```

### List Calendars (includes shared)
```json
{
  "calendars": [
    {
      "id": "550e8400-e29b-41d4-a716-446655440000",
      "name": "Personal",
      "color": "#3788d8",
      "shared": false,
      "permission": "owner"
    },
    {
      "id": "550e8400-e29b-41d4-a716-446655440005",
      "name": "Team Calendar",
      "color": "#28a745",
      "shared": true,
      "permission": "read-write",
      "owner": {
        "id": "880e8400-e29b-41d4-a716-446655440006",
        "username": "teamlead",
        "display_name": "Team Lead"
      }
    }
  ]
}
```

### Share with Self (400)
```json
{
  "error": "bad_request",
  "message": "Cannot share calendar with yourself"
}
```

### User Not Found (404)
```json
{
  "error": "not_found",
  "message": "User 'unknownuser' not found"
}
```

### Permission Denied (403)
```json
{
  "error": "forbidden",
  "message": "You do not have permission to modify this calendar"
}
```

## Definition of Done

- [ ] `POST /api/v1/calendars/{id}/shares` creates share
- [ ] `GET /api/v1/calendars/{id}/shares` lists shares
- [ ] `PATCH /api/v1/calendars/{id}/shares/{id}` updates permission
- [ ] `DELETE /api/v1/calendars/{id}/shares/{id}` revokes share
- [ ] Shared calendars appear in recipient's calendar list
- [ ] Read permission allows viewing only
- [ ] Read-write permission allows event modifications
- [ ] Shared calendars accessible via CalDAV
- [ ] WebDAV ACL properties reflect actual permissions
- [ ] Event changes sync to all shared users
- [ ] Unit tests for permission checking
- [ ] Integration tests for sharing flow
