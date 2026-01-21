# Story 014: CalDAV Protocol Implementation

## Title
Implement CalDAV Protocol Endpoints

## Description
As a DAV client user, I want to access my calendars via CalDAV protocol so that I can sync events with applications like DAVx5, Apple Calendar, and Thunderbird.

## Related Acceptance Criteria

| ID | Criterion |
|----|-----------|
| CD-3.3.1 | Server responds to OPTIONS with correct DAV headers |
| CD-3.3.2 | /.well-known/caldav redirects to DAV root |
| CD-3.3.3 | PROPFIND on principal returns calendar-home-set |
| CD-3.3.4 | PROPFIND on calendar-home lists all calendars |
| CD-3.3.5 | MKCALENDAR creates new calendar collection |
| CD-3.3.6 | PUT creates new event in calendar |
| CD-3.3.7 | PUT updates existing event (with correct ETag) |
| CD-3.3.8 | PUT with wrong ETag returns 412 Precondition Failed |
| CD-3.3.9 | GET retrieves event iCalendar data |
| CD-3.3.10 | DELETE removes event |
| CD-3.3.11 | REPORT calendar-query returns filtered events |
| CD-3.3.12 | REPORT calendar-multiget returns specific events by URL |
| CD-3.3.13 | ETags change when events are modified |
| CD-3.3.14 | CTag changes when calendar contents change |

## Acceptance Criteria

### Service Discovery

- [ ] `GET /.well-known/caldav` returns 301 redirect to `/dav/`
- [ ] `OPTIONS /dav/` returns DAV capabilities:
  - [ ] Header: `DAV: 1, 2, 3, calendar-access, addressbook`
  - [ ] Header: `Allow: OPTIONS, GET, HEAD, PUT, DELETE, PROPFIND, PROPPATCH, MKCALENDAR, REPORT`

### Principal Discovery

- [ ] `PROPFIND /dav/principals/{username}/` returns:
  - [ ] `current-user-principal`
  - [ ] `calendar-home-set` -> `/dav/calendars/{username}/`
  - [ ] `addressbook-home-set` -> `/dav/addressbooks/{username}/`
  - [ ] `displayname`

### Calendar Home

- [ ] `PROPFIND /dav/calendars/{username}/` (Depth: 0) returns:
  - [ ] `resourcetype` (collection)
  - [ ] `displayname`
  - [ ] `current-user-privilege-set`
- [ ] `PROPFIND /dav/calendars/{username}/` (Depth: 1) returns:
  - [ ] List of all calendars
  - [ ] Each calendar's properties

### Calendar Collection Properties

- [ ] `PROPFIND /dav/calendars/{username}/{calendar-id}/` returns:
  - [ ] `resourcetype` (collection, calendar)
  - [ ] `displayname`
  - [ ] `calendar-description`
  - [ ] `calendar-color`
  - [ ] `calendar-timezone`
  - [ ] `supported-calendar-component-set` (VEVENT, VTODO)
  - [ ] `getctag`
  - [ ] `sync-token`

### Calendar Operations

- [ ] `MKCALENDAR /dav/calendars/{username}/{new-calendar}/`
  - [ ] Creates new calendar
  - [ ] Request body can include properties (displayname, color)
  - [ ] Returns 201 Created
- [ ] `DELETE /dav/calendars/{username}/{calendar-id}/`
  - [ ] Deletes calendar and all events
  - [ ] Returns 204 No Content
- [ ] `PROPPATCH /dav/calendars/{username}/{calendar-id}/`
  - [ ] Updates calendar properties
  - [ ] Returns 207 Multi-Status

### Event Operations

- [ ] `PUT /dav/calendars/{username}/{calendar-id}/{event-uid}.ics`
  - [ ] Creates new event if not exists
  - [ ] Updates existing event with `If-Match: {etag}` header
  - [ ] Returns 201 Created (new) or 204 No Content (update)
  - [ ] Returns `ETag` header
  - [ ] Updates calendar CTag and sync-token
- [ ] `GET /dav/calendars/{username}/{calendar-id}/{event-uid}.ics`
  - [ ] Returns iCalendar data
  - [ ] Returns `ETag` header
  - [ ] Content-Type: `text/calendar; charset=utf-8`
- [ ] `DELETE /dav/calendars/{username}/{calendar-id}/{event-uid}.ics`
  - [ ] Deletes event
  - [ ] Updates calendar CTag and sync-token
  - [ ] Returns 204 No Content
- [ ] ETag validation:
  - [ ] `If-Match: *` allows any existing resource
  - [ ] `If-Match: "etag"` requires exact match
  - [ ] `If-None-Match: *` only allows creating new (no overwrite)
  - [ ] Returns 412 Precondition Failed on mismatch

### REPORT Queries

- [ ] `REPORT calendar-query`:
  ```xml
  <calendar-query xmlns="urn:ietf:params:xml:ns:caldav">
    <prop>
      <getetag/>
      <calendar-data/>
    </prop>
    <filter>
      <comp-filter name="VCALENDAR">
        <comp-filter name="VEVENT">
          <time-range start="20240101T000000Z" end="20240201T000000Z"/>
        </comp-filter>
      </comp-filter>
    </filter>
  </calendar-query>
  ```
  - [ ] Returns events matching filter
  - [ ] Supports time-range filtering
  - [ ] Supports component type filtering
- [ ] `REPORT calendar-multiget`:
  ```xml
  <calendar-multiget xmlns="urn:ietf:params:xml:ns:caldav">
    <prop>
      <getetag/>
      <calendar-data/>
    </prop>
    <href>/dav/calendars/user/cal/event1.ics</href>
    <href>/dav/calendars/user/cal/event2.ics</href>
  </calendar-multiget>
  ```
  - [ ] Returns requested events by URL
  - [ ] Non-existent URLs return 404 in multistatus

## Technical Notes

### Dependencies
```go
github.com/emersion/go-webdav  // CalDAV/CardDAV protocol
github.com/emersion/go-ical    // iCalendar parsing
```

### CalDAV Backend Interface
```go
// Implement caldav.Backend from go-webdav
type CalDAVBackend struct {
    calendarRepo calendar.Repository
    userRepo     user.Repository
}

func (b *CalDAVBackend) CalendarHomeSetPath(ctx context.Context) (string, error)
func (b *CalDAVBackend) ListCalendars(ctx context.Context) ([]caldav.Calendar, error)
func (b *CalDAVBackend) GetCalendar(ctx context.Context, path string) (*caldav.Calendar, error)
func (b *CalDAVBackend) CreateCalendar(ctx context.Context, calendar *caldav.Calendar) error
func (b *CalDAVBackend) DeleteCalendar(ctx context.Context, path string) error
func (b *CalDAVBackend) GetCalendarObject(ctx context.Context, path string) (*caldav.CalendarObject, error)
func (b *CalDAVBackend) ListCalendarObjects(ctx context.Context, path string, req *caldav.CalendarQuery) ([]caldav.CalendarObject, error)
func (b *CalDAVBackend) PutCalendarObject(ctx context.Context, path string, calendar *ical.Calendar, opts *caldav.PutCalendarObjectOptions) (string, error)
func (b *CalDAVBackend) DeleteCalendarObject(ctx context.Context, path string) error
```

### URL Structure
```
/dav/                                    # DAV root
/dav/principals/{username}/              # User principal
/dav/calendars/{username}/               # Calendar home
/dav/calendars/{username}/{cal-uuid}/    # Calendar collection
/dav/calendars/{username}/{cal-uuid}/{event-uid}.ics  # Event resource
```

### Code Structure
```
internal/adapter/webdav/
├── caldav_backend.go     # Implements caldav.Backend
├── caldav_handler.go     # HTTP handler setup
├── principal_backend.go  # Principal discovery
├── props.go              # Property handling
└── reports.go            # REPORT method handling

internal/infrastructure/server/
└── routes.go             # Mount WebDAV at /dav/
```

### iCalendar Validation
```go
func validateICalendar(data []byte) error {
    cal, err := ical.ParseCalendar(bytes.NewReader(data))
    if err != nil {
        return fmt.Errorf("invalid iCalendar: %w", err)
    }

    // Must have at least one VEVENT or VTODO
    events := cal.Events()
    todos := cal.Todos()
    if len(events) == 0 && len(todos) == 0 {
        return errors.New("calendar must contain at least one event or todo")
    }

    // Each component must have a UID
    for _, event := range events {
        if event.Props.Get("UID") == nil {
            return errors.New("event missing required UID property")
        }
    }

    return nil
}
```

## Response Examples

### OPTIONS Response
```http
HTTP/1.1 200 OK
DAV: 1, 2, 3, calendar-access, addressbook
Allow: OPTIONS, GET, HEAD, PUT, DELETE, PROPFIND, PROPPATCH, MKCALENDAR, REPORT
```

### PROPFIND Calendar Response (207 Multi-Status)
```xml
<?xml version="1.0" encoding="UTF-8"?>
<multistatus xmlns="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav" xmlns:IC="http://apple.com/ns/ical/">
  <response>
    <href>/dav/calendars/johndoe/550e8400-e29b-41d4-a716-446655440001/</href>
    <propstat>
      <prop>
        <resourcetype>
          <collection/>
          <C:calendar/>
        </resourcetype>
        <displayname>Work</displayname>
        <C:calendar-description>Work meetings</C:calendar-description>
        <IC:calendar-color>#ff5733</IC:calendar-color>
        <C:supported-calendar-component-set>
          <C:comp name="VEVENT"/>
          <C:comp name="VTODO"/>
        </C:supported-calendar-component-set>
        <getctag>1705833600-abc123</getctag>
        <sync-token>https://caldav.example.com/sync/1705833600-abc123</sync-token>
      </prop>
      <status>HTTP/1.1 200 OK</status>
    </propstat>
  </response>
</multistatus>
```

### PUT Event (201 Created)
```http
HTTP/1.1 201 Created
ETag: "a1b2c3d4e5f6"
Location: /dav/calendars/johndoe/work/meeting-123.ics
```

### calendar-multiget Response
```xml
<?xml version="1.0" encoding="UTF-8"?>
<multistatus xmlns="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">
  <response>
    <href>/dav/calendars/johndoe/work/meeting-123.ics</href>
    <propstat>
      <prop>
        <getetag>"a1b2c3d4e5f6"</getetag>
        <C:calendar-data>BEGIN:VCALENDAR
VERSION:2.0
BEGIN:VEVENT
UID:meeting-123@caldav.example.com
DTSTART:20240122T090000Z
DTEND:20240122T100000Z
SUMMARY:Team Meeting
END:VEVENT
END:VCALENDAR</C:calendar-data>
      </prop>
      <status>HTTP/1.1 200 OK</status>
    </propstat>
  </response>
  <response>
    <href>/dav/calendars/johndoe/work/nonexistent.ics</href>
    <status>HTTP/1.1 404 Not Found</status>
  </response>
</multistatus>
```

## Definition of Done

- [ ] `/.well-known/caldav` redirects to DAV root
- [ ] OPTIONS returns correct DAV headers
- [ ] PROPFIND on principal returns calendar-home-set
- [ ] PROPFIND on calendar-home lists calendars
- [ ] MKCALENDAR creates calendars
- [ ] PUT creates/updates events with ETag validation
- [ ] GET retrieves events
- [ ] DELETE removes events
- [ ] REPORT calendar-query returns filtered events
- [ ] REPORT calendar-multiget returns specific events
- [ ] ETags update on modification
- [ ] CTag updates when calendar changes
- [ ] DAVx5 can sync calendars
- [ ] Apple Calendar can sync calendars
- [ ] Integration tests for all DAV operations
