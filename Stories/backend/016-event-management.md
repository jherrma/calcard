# Story 016: Event Management REST API

## Title
Implement Event CRUD Operations via REST API

## Description
As a web UI user, I want to create, view, update, and delete calendar events so that I can manage my schedule through the browser.

## Related Acceptance Criteria

| ID | Criterion |
|----|-----------|
| CD-3.2.1 | Users can view calendar in month view |
| CD-3.2.4 | Users can create events with title, start time, end time |
| CD-3.2.5 | Users can create all-day events |
| CD-3.2.6 | Users can add event description |
| CD-3.2.7 | Users can add event location |
| CD-3.2.8 | Users can edit existing events |
| CD-3.2.9 | Users can delete events |
| CD-3.2.12 | Users can create recurring events (daily, weekly, monthly, yearly) |
| CD-3.2.13 | Users can edit single instance of recurring event |
| CD-3.2.14 | Users can edit all instances of recurring event |
| CD-3.2.15 | Users can delete single instance of recurring event |

## Acceptance Criteria

### List Events

- [ ] REST endpoint `GET /api/v1/calendars/{calendar_id}/events` (requires auth)
- [ ] Query parameters:
  - [ ] `start` (required): ISO 8601 datetime, range start
  - [ ] `end` (required): ISO 8601 datetime, range end
  - [ ] `expand` (optional): Expand recurring events into instances (default: true)
- [ ] Returns events within time range
- [ ] Recurring events expanded to individual instances (with `recurrence_id`)
- [ ] Each event includes:
  - [ ] ID (UUID)
  - [ ] Calendar ID
  - [ ] Title/Summary
  - [ ] Start/End time
  - [ ] All-day flag
  - [ ] Description
  - [ ] Location
  - [ ] Recurrence rule (if recurring)
  - [ ] Is exception (if modified instance)

### Get Single Event

- [ ] REST endpoint `GET /api/v1/calendars/{calendar_id}/events/{event_id}` (requires auth)
- [ ] Returns full event details
- [ ] Includes recurrence information if applicable
- [ ] Returns 404 if not found

### Create Event

- [ ] REST endpoint `POST /api/v1/calendars/{calendar_id}/events` (requires auth)
- [ ] Request body:
  ```json
  {
    "summary": "Team Meeting",
    "description": "Weekly sync with the team",
    "location": "Conference Room A",
    "start": "2024-01-22T09:00:00",
    "end": "2024-01-22T10:00:00",
    "timezone": "America/New_York",
    "all_day": false,
    "recurrence": {
      "frequency": "weekly",
      "interval": 1,
      "by_day": ["MO", "WE", "FR"],
      "until": "2024-12-31"
    }
  }
  ```
- [ ] Summary required, max 500 characters
- [ ] Start required
- [ ] End required (or duration)
- [ ] For all-day events: dates only (no time component)
- [ ] Generates iCalendar UID
- [ ] Stores as iCalendar format internally
- [ ] Updates calendar CTag and sync-token
- [ ] Returns 201 Created with event data

### Update Event

- [ ] REST endpoint `PATCH /api/v1/calendars/{calendar_id}/events/{event_id}` (requires auth)
- [ ] All fields optional (partial update)
- [ ] For recurring events, query param `scope`:
  - [ ] `this` - Update only this instance (creates EXDATE + new event)
  - [ ] `this_and_future` - Update this and future instances
  - [ ] `all` - Update entire series
- [ ] Updates calendar CTag and sync-token
- [ ] Returns updated event

### Delete Event

- [ ] REST endpoint `DELETE /api/v1/calendars/{calendar_id}/events/{event_id}` (requires auth)
- [ ] For recurring events, query param `scope`:
  - [ ] `this` - Delete only this instance (adds EXDATE)
  - [ ] `this_and_future` - Delete this and future instances
  - [ ] `all` - Delete entire series
- [ ] Updates calendar CTag and sync-token
- [ ] Returns 204 No Content

### Move Event

- [ ] REST endpoint `POST /api/v1/calendars/{calendar_id}/events/{event_id}/move` (requires auth)
- [ ] Request body:
  ```json
  {
    "target_calendar_id": "uuid-of-target-calendar"
  }
  ```
- [ ] Moves event to different calendar
- [ ] Updates both calendars' CTag and sync-token
- [ ] Returns updated event

## Technical Notes

### iCalendar Generation
```go
func eventToICalendar(event *Event) string {
    cal := ical.NewCalendar()
    cal.Props.SetText(ical.PropProductID, "-//CalDAV Server//EN")
    cal.Props.SetText(ical.PropVersion, "2.0")

    vevent := ical.NewEvent()
    vevent.Props.SetText(ical.PropUID, event.UID)
    vevent.Props.SetDateTime(ical.PropDateTimeStamp, time.Now())
    vevent.Props.SetText(ical.PropSummary, event.Summary)

    if event.AllDay {
        vevent.Props.SetDate(ical.PropDateTimeStart, event.Start)
        vevent.Props.SetDate(ical.PropDateTimeEnd, event.End)
    } else {
        vevent.Props.SetDateTime(ical.PropDateTimeStart, event.Start)
        vevent.Props.SetDateTime(ical.PropDateTimeEnd, event.End)
    }

    if event.Description != "" {
        vevent.Props.SetText(ical.PropDescription, event.Description)
    }
    if event.Location != "" {
        vevent.Props.SetText(ical.PropLocation, event.Location)
    }
    if event.RRule != "" {
        vevent.Props.SetText(ical.PropRecurrenceRule, event.RRule)
    }

    cal.Children = append(cal.Children, vevent.Component)
    return cal.Serialize()
}
```

### Recurrence Rule Handling
```go
type RecurrenceRule struct {
    Frequency string   `json:"frequency"` // daily, weekly, monthly, yearly
    Interval  int      `json:"interval"`  // Every N frequency units
    ByDay     []string `json:"by_day"`    // MO, TU, WE, TH, FR, SA, SU
    ByMonthDay []int   `json:"by_month_day"` // 1-31
    ByMonth   []int    `json:"by_month"`  // 1-12
    Count     *int     `json:"count"`     // Number of occurrences
    Until     *string  `json:"until"`     // End date (ISO 8601)
}

// Convert to RRULE string
func (r *RecurrenceRule) ToRRule() string {
    parts := []string{fmt.Sprintf("FREQ=%s", strings.ToUpper(r.Frequency))}
    if r.Interval > 1 {
        parts = append(parts, fmt.Sprintf("INTERVAL=%d", r.Interval))
    }
    if len(r.ByDay) > 0 {
        parts = append(parts, fmt.Sprintf("BYDAY=%s", strings.Join(r.ByDay, ",")))
    }
    if r.Count != nil {
        parts = append(parts, fmt.Sprintf("COUNT=%d", *r.Count))
    }
    if r.Until != nil {
        parts = append(parts, fmt.Sprintf("UNTIL=%s", *r.Until))
    }
    return strings.Join(parts, ";")
}
```

### Event Expansion
```go
// Expand recurring event into instances within time range
func expandRecurringEvent(event *CalendarObject, start, end time.Time) []EventInstance {
    cal, _ := ical.ParseCalendar(strings.NewReader(event.ICalData))
    vevent := cal.Events()[0]

    rruleProp := vevent.Props.Get(ical.PropRecurrenceRule)
    if rruleProp == nil {
        // Single event
        return []EventInstance{{Event: event, Start: event.StartTime, End: event.EndTime}}
    }

    // Parse RRULE and generate instances
    rule, _ := rrule.StrToRRule(rruleProp.Value)
    rule.DTStart(event.StartTime)

    instances := []EventInstance{}
    for _, dt := range rule.Between(start, end, true) {
        duration := event.EndTime.Sub(event.StartTime)
        instances = append(instances, EventInstance{
            Event:        event,
            Start:        dt,
            End:          dt.Add(duration),
            RecurrenceID: dt.Format("20060102T150405Z"),
        })
    }

    // Apply exceptions (EXDATE)
    // Apply modifications (RECURRENCE-ID events)

    return instances
}
```

### Code Structure
```
internal/usecase/event/
├── list.go              # List events with expansion
├── get.go               # Get single event
├── create.go            # Create event
├── update.go            # Update event (with recurrence handling)
├── delete.go            # Delete event (with recurrence handling)
├── move.go              # Move event between calendars
└── recurrence.go        # Recurrence rule handling

internal/adapter/http/
└── event_handler.go     # HTTP handlers

internal/domain/calendar/
└── event.go             # Event domain types
```

## API Response Examples

### List Events (200 OK)
```json
{
  "events": [
    {
      "id": "550e8400-e29b-41d4-a716-446655440010",
      "calendar_id": "550e8400-e29b-41d4-a716-446655440001",
      "summary": "Team Meeting",
      "description": "Weekly sync",
      "location": "Conference Room A",
      "start": "2024-01-22T09:00:00-05:00",
      "end": "2024-01-22T10:00:00-05:00",
      "timezone": "America/New_York",
      "all_day": false,
      "is_recurring": true,
      "recurrence_id": null,
      "recurrence": {
        "frequency": "weekly",
        "interval": 1,
        "by_day": ["MO", "WE", "FR"]
      }
    },
    {
      "id": "550e8400-e29b-41d4-a716-446655440010",
      "calendar_id": "550e8400-e29b-41d4-a716-446655440001",
      "summary": "Team Meeting",
      "start": "2024-01-24T09:00:00-05:00",
      "end": "2024-01-24T10:00:00-05:00",
      "all_day": false,
      "is_recurring": true,
      "recurrence_id": "20240124T140000Z",
      "is_instance": true
    }
  ],
  "count": 2
}
```

### Create Event (201 Created)
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440010",
  "calendar_id": "550e8400-e29b-41d4-a716-446655440001",
  "uid": "550e8400-e29b-41d4-a716-446655440010@caldav.example.com",
  "summary": "Team Meeting",
  "description": "Weekly sync with the team",
  "location": "Conference Room A",
  "start": "2024-01-22T09:00:00-05:00",
  "end": "2024-01-22T10:00:00-05:00",
  "timezone": "America/New_York",
  "all_day": false,
  "recurrence": {
    "frequency": "weekly",
    "interval": 1,
    "by_day": ["MO", "WE", "FR"],
    "until": "2024-12-31"
  },
  "created_at": "2024-01-21T10:00:00Z",
  "updated_at": "2024-01-21T10:00:00Z"
}
```

### Update Recurring Event - This Instance Only (200 OK)
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440011",
  "calendar_id": "550e8400-e29b-41d4-a716-446655440001",
  "summary": "Team Meeting (Moved)",
  "start": "2024-01-24T14:00:00-05:00",
  "end": "2024-01-24T15:00:00-05:00",
  "recurrence_id": "20240124T140000Z",
  "is_exception": true,
  "original_event_id": "550e8400-e29b-41d4-a716-446655440010"
}
```

### Validation Error (400)
```json
{
  "error": "validation_error",
  "message": "Validation failed",
  "details": [
    {"field": "summary", "message": "Summary is required"},
    {"field": "end", "message": "End time must be after start time"}
  ]
}
```

## Definition of Done

- [ ] `GET /api/v1/calendars/{id}/events` returns events in time range
- [ ] Recurring events are expanded into instances
- [ ] `POST /api/v1/calendars/{id}/events` creates single and recurring events
- [ ] `PATCH /api/v1/calendars/{id}/events/{id}` updates events
- [ ] Recurring event updates support this/this_and_future/all scopes
- [ ] `DELETE /api/v1/calendars/{id}/events/{id}` deletes events
- [ ] Recurring event deletion supports scopes
- [ ] Event changes update calendar CTag and sync-token
- [ ] iCalendar data correctly generated and stored
- [ ] Unit tests for recurrence expansion
- [ ] Integration tests for CRUD operations
