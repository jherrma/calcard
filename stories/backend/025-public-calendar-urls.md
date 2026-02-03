# Story 025: Public Calendar URLs (iCal Feed)

## Title
Implement Public iCal URLs for Calendar Subscription

## Description
As a user, I want to generate a public iCal URL for my calendars so that others can subscribe to my calendar in read-only mode using applications like Google Calendar.

## Related Acceptance Criteria

| ID | Criterion |
|----|-----------|
| CL-6.4.1 | Users can get public iCal URL for their calendars |
| CL-6.4.2 | Google Calendar can subscribe to iCal URL |
| CL-6.4.3 | Events appear in Google Calendar (read-only) |
| CL-6.4.4 | Updates on server reflect in Google Calendar |

## Acceptance Criteria

### Enable Public Access

- [ ] REST endpoint `POST /api/v1/calendars/{calendar_id}/public` (requires auth)
- [ ] Request body:
  ```json
  {
    "enabled": true
  }
  ```
- [ ] Generates unique public token (32 bytes, URL-safe base64)
- [ ] Only calendar owner can enable/disable
- [ ] Returns public URL
- [ ] Public URL does not require authentication

### Get Public URL

- [ ] REST endpoint `GET /api/v1/calendars/{calendar_id}/public` (requires auth)
- [ ] Returns public access status:
  - [ ] Enabled/disabled
  - [ ] Public URL (if enabled)
  - [ ] Token (for regeneration purposes)
  - [ ] Created date

### Disable Public Access

- [ ] REST endpoint `POST /api/v1/calendars/{calendar_id}/public` with `enabled: false`
- [ ] Invalidates existing public URL
- [ ] Returns 200 OK with disabled status

### Regenerate Public Token

- [ ] REST endpoint `POST /api/v1/calendars/{calendar_id}/public/regenerate` (requires auth)
- [ ] Generates new token, invalidating old URL
- [ ] Returns new public URL
- [ ] Useful if URL was accidentally shared

### Public iCal Endpoint

- [ ] `GET /public/calendar/{token}.ics` (no auth required)
- [ ] Returns complete iCalendar feed
- [ ] Includes all events (with recurrence rules)
- [ ] Content-Type: `text/calendar; charset=utf-8`
- [ ] Read-only (no write operations)
- [ ] Returns 404 if token invalid or public access disabled

### iCal Feed Content

- [ ] Valid iCalendar format (RFC 5545)
- [ ] Includes VCALENDAR properties:
  - [ ] VERSION: 2.0
  - [ ] PRODID: server identifier
  - [ ] X-WR-CALNAME: calendar name
  - [ ] X-WR-TIMEZONE: calendar timezone
- [ ] All VEVENT components with:
  - [ ] UID, DTSTAMP, DTSTART, DTEND
  - [ ] SUMMARY, DESCRIPTION, LOCATION
  - [ ] RRULE (for recurring events)
  - [ ] EXDATE (for exceptions)
- [ ] VTIMEZONE components for referenced timezones

### Caching

- [ ] Set appropriate cache headers:
  - [ ] `Cache-Control: public, max-age=300` (5 minutes)
  - [ ] `ETag` based on calendar CTag
- [ ] Support `If-None-Match` for 304 Not Modified
- [ ] Google Calendar typically polls every few hours

## Technical Notes

### Database Model Extension
```go
type Calendar struct {
    // ... existing fields ...

    PublicToken   string     `gorm:"uniqueIndex;size:64"`
    PublicEnabled bool       `gorm:"default:false"`
    PublicEnabledAt *time.Time
}
```

### Token Generation
```go
func generatePublicToken() string {
    b := make([]byte, 32)
    rand.Read(b)
    return base64.URLEncoding.EncodeToString(b)
}
```

### Code Structure
```
internal/usecase/calendar/
├── enable_public.go       # Enable/disable public access
├── get_public_status.go   # Get public access status
└── regenerate_token.go    # Regenerate public token

internal/adapter/http/
├── public_calendar_handler.go  # Public iCal endpoint (no auth)
└── calendar_public_handler.go  # Management endpoints (auth required)
```

### iCal Generation
```go
func generateICalFeed(calendar *Calendar, events []*CalendarObject) string {
    var b strings.Builder

    // VCALENDAR header
    b.WriteString("BEGIN:VCALENDAR\r\n")
    b.WriteString("VERSION:2.0\r\n")
    b.WriteString("PRODID:-//CalDAV Server//EN\r\n")
    b.WriteString("CALSCALE:GREGORIAN\r\n")
    b.WriteString("METHOD:PUBLISH\r\n")
    b.WriteString(fmt.Sprintf("X-WR-CALNAME:%s\r\n", escapeICalText(calendar.Name)))
    if calendar.Timezone != "" {
        b.WriteString(fmt.Sprintf("X-WR-TIMEZONE:%s\r\n", calendar.Timezone))
    }

    // Collect unique timezones
    timezones := collectTimezones(events)
    for _, tz := range timezones {
        b.WriteString(generateVTimezone(tz))
    }

    // Events
    for _, event := range events {
        // Extract VEVENT from stored iCalendar data
        vevent := extractVEvent(event.ICalData)
        b.WriteString(vevent)
    }

    b.WriteString("END:VCALENDAR\r\n")
    return b.String()
}
```

### Public Endpoint Handler
```go
func (h *PublicCalendarHandler) GetICalFeed(c fiber.Ctx) error {
    token := c.Params("token")
    // Remove .ics extension if present
    token = strings.TrimSuffix(token, ".ics")

    // Find calendar by public token
    calendar, err := h.calendarRepo.FindByPublicToken(c.Context(), token)
    if err != nil || !calendar.PublicEnabled {
        return c.Status(404).SendString("Calendar not found")
    }

    // Check ETag for caching
    clientETag := c.Get("If-None-Match")
    currentETag := fmt.Sprintf(`"%s"`, calendar.CTag)
    if clientETag == currentETag {
        return c.SendStatus(304)
    }

    // Get all events
    events, err := h.eventRepo.FindByCalendarID(c.Context(), calendar.ID)
    if err != nil {
        return c.Status(500).SendString("Internal error")
    }

    // Generate iCal
    ical := generateICalFeed(calendar, events)

    // Set headers
    c.Set("Content-Type", "text/calendar; charset=utf-8")
    c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.ics"`, calendar.Name))
    c.Set("Cache-Control", "public, max-age=300")
    c.Set("ETag", currentETag)

    return c.SendString(ical)
}
```

## API Response Examples

### Enable Public Access (200 OK)
```json
{
  "enabled": true,
  "public_url": "https://caldav.example.com/public/calendar/xK9mN2pL4qR7sT1vW3yZ5bD8eF0gH6iJ.ics",
  "token": "xK9mN2pL4qR7sT1vW3yZ5bD8eF0gH6iJ",
  "enabled_at": "2024-01-21T10:00:00Z"
}
```

### Get Public Status (200 OK) - Enabled
```json
{
  "enabled": true,
  "public_url": "https://caldav.example.com/public/calendar/xK9mN2pL4qR7sT1vW3yZ5bD8eF0gH6iJ.ics",
  "token": "xK9mN2pL4qR7sT1vW3yZ5bD8eF0gH6iJ",
  "enabled_at": "2024-01-21T10:00:00Z"
}
```

### Get Public Status (200 OK) - Disabled
```json
{
  "enabled": false,
  "public_url": null,
  "token": null,
  "enabled_at": null
}
```

### Regenerate Token (200 OK)
```json
{
  "enabled": true,
  "public_url": "https://caldav.example.com/public/calendar/newTokenABC123xyz789.ics",
  "token": "newTokenABC123xyz789",
  "enabled_at": "2024-01-21T10:00:00Z",
  "message": "Previous public URL is no longer valid"
}
```

### Public iCal Feed (200 OK)
```
Content-Type: text/calendar; charset=utf-8
Content-Disposition: attachment; filename="Work.ics"
Cache-Control: public, max-age=300
ETag: "1705833600-abc123"

BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//CalDAV Server//EN
CALSCALE:GREGORIAN
METHOD:PUBLISH
X-WR-CALNAME:Work
X-WR-TIMEZONE:America/New_York
BEGIN:VTIMEZONE
TZID:America/New_York
BEGIN:DAYLIGHT
TZOFFSETFROM:-0500
TZOFFSETTO:-0400
DTSTART:20240310T020000
RRULE:FREQ=YEARLY;BYMONTH=3;BYDAY=2SU
TZNAME:EDT
END:DAYLIGHT
BEGIN:STANDARD
TZOFFSETFROM:-0400
TZOFFSETTO:-0500
DTSTART:20241103T020000
RRULE:FREQ=YEARLY;BYMONTH=11;BYDAY=1SU
TZNAME:EST
END:STANDARD
END:VTIMEZONE
BEGIN:VEVENT
UID:meeting-123@caldav.example.com
DTSTAMP:20240121T100000Z
DTSTART;TZID=America/New_York:20240122T090000
DTEND;TZID=America/New_York:20240122T100000
SUMMARY:Team Meeting
DESCRIPTION:Weekly team sync
LOCATION:Conference Room A
RRULE:FREQ=WEEKLY;BYDAY=MO,WE,FR
END:VEVENT
END:VCALENDAR
```

### Invalid Token (404)
```
Calendar not found
```

## Google Calendar Setup Instructions

For the web UI setup page:

1. Copy the public iCal URL
2. In Google Calendar, click the "+" next to "Other calendars"
3. Select "From URL"
4. Paste the URL
5. Click "Add calendar"

**Note:** Google Calendar syncs every few hours. Changes may not appear immediately.

## Security Considerations

1. **Token Security**: Use cryptographically random tokens
2. **No Write Access**: Public URLs are strictly read-only
3. **Token Regeneration**: Provide easy way to invalidate leaked URLs
4. **Rate Limiting**: Apply rate limits to public endpoint
5. **No User Enumeration**: 404 for invalid tokens (same as disabled)

## Definition of Done

- [ ] `POST /api/v1/calendars/{id}/public` enables/disables public access
- [ ] `GET /api/v1/calendars/{id}/public` returns public status
- [ ] `POST /api/v1/calendars/{id}/public/regenerate` creates new token
- [ ] `GET /public/calendar/{token}.ics` returns iCal feed
- [ ] iCal feed is valid RFC 5545 format
- [ ] Feed includes all events with recurrence rules
- [ ] Appropriate cache headers set
- [ ] ETag-based caching works (304 responses)
- [ ] Google Calendar can subscribe and display events
- [ ] Invalid/disabled tokens return 404
- [ ] Unit tests for iCal generation
- [ ] Integration tests for public endpoint
