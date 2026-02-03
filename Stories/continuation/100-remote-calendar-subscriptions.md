# Story 100: Remote Calendar Subscriptions

## Title

Subscribe to Remote Calendars via URL

## Description

As a user, I want to subscribe to external calendars via URL (WebCal/iCalendar feeds) so that I can view events from other services (e.g., Google Calendar, Outlook, sports schedules) that automatically stay synchronized with the remote source.

## Acceptance Criteria

### Calendar Subscription Creation

- [ ] REST endpoint `POST /api/v1/calendar-subscriptions` (requires auth)
- [ ] Request body:
  ```json
  {
    "url": "https://example.com/calendar.ics",
    "name": "My Subscription",
    "color": "#3788d8",
    "refresh_interval": "1h"
  }
  ```
- [ ] Validate URL (HTTPS only in production)
- [ ] Fetch and validate iCalendar data on creation
- [ ] Create read-only calendar with events from feed
- [ ] Store subscription metadata (URL, refresh interval, last synced)

### Subscription Management

- [ ] REST endpoint `GET /api/v1/calendar-subscriptions` - List subscriptions
- [ ] REST endpoint `GET /api/v1/calendar-subscriptions/:id` - Get subscription details
- [ ] REST endpoint `PATCH /api/v1/calendar-subscriptions/:id` - Update settings
  - [ ] Allow changing name, color, refresh interval
  - [ ] Allow changing URL (triggers immediate resync)
- [ ] REST endpoint `DELETE /api/v1/calendar-subscriptions/:id` - Remove subscription
- [ ] REST endpoint `POST /api/v1/calendar-subscriptions/:id/refresh` - Force immediate sync

### Automatic Synchronization

- [ ] Background worker to refresh subscriptions based on `refresh_interval`
- [ ] Supported intervals: 15m, 30m, 1h (default), 6h, 12h, 24h
- [ ] Minimum interval: 15 minutes to prevent abuse
- [ ] Handle transient failures gracefully (exponential backoff)
- [ ] Track sync status: last_synced_at, last_error, error_count
- [ ] Disable auto-sync after N consecutive failures (configurable)

### Sync Behavior

- [ ] Full sync: Replace all events with those from the feed
- [ ] Preserve local modifications (optional setting)
- [ ] Handle deleted events (remove from local calendar)
- [ ] Handle modified events (update local copy)
- [ ] ETag/Last-Modified support for conditional fetching

### CalDAV Integration

- [ ] Subscribed calendars appear in CalDAV PROPFIND responses
- [ ] Events are readable via CalDAV GET
- [ ] PUT/DELETE operations are rejected (read-only)
- [ ] Calendar displays `CS:subscribed` property for clients

## Technical Notes

### Database Schema

```go
type CalendarSubscription struct {
    ID              uint           `gorm:"primaryKey"`
    CalendarID      uint           `gorm:"uniqueIndex;not null"` // Links to Calendar
    UserID          uint           `gorm:"index;not null"`
    RemoteURL       string         `gorm:"size:2048;not null"`
    RefreshInterval time.Duration  `gorm:"not null;default:3600000000000"` // 1 hour in nanoseconds
    LastSyncedAt    *time.Time
    LastError       string         `gorm:"size:500"`
    ErrorCount      int            `gorm:"default:0"`
    Enabled         bool           `gorm:"default:true"`
    ETag            string         `gorm:"size:256"` // For conditional requests
    CreatedAt       time.Time
    UpdatedAt       time.Time
}
```

### Background Worker

```go
type SubscriptionSyncWorker struct {
    repo         CalendarSubscriptionRepository
    calendarRepo calendar.CalendarRepository
    httpClient   *http.Client
    interval     time.Duration // Worker tick interval (e.g., 1 minute)
}

func (w *SubscriptionSyncWorker) Run(ctx context.Context) {
    ticker := time.NewTicker(w.interval)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            w.syncDueSubscriptions(ctx)
        }
    }
}

func (w *SubscriptionSyncWorker) syncDueSubscriptions(ctx context.Context) {
    // Find subscriptions where next_sync_at <= now
    subs, _ := w.repo.FindDueForSync(ctx)
    for _, sub := range subs {
        w.syncSubscription(ctx, sub)
    }
}
```

### Code Structure

```
internal/domain/calendar/
└── subscription.go             # CalendarSubscription entity

internal/usecase/subscription/
├── create.go                   # Create subscription
├── list.go                     # List subscriptions
├── update.go                   # Update subscription
├── delete.go                   # Delete subscription
├── refresh.go                  # Manual refresh
└── sync_worker.go              # Background sync worker

internal/adapter/http/
└── subscription_handler.go     # HTTP handlers

internal/adapter/repository/
└── subscription_repository.go  # Database operations
```

## API Response Examples

### Create Subscription (201 Created)

```json
{
  "id": 1,
  "calendar_id": "a1b2c3d4-uuid",
  "name": "Work Holidays",
  "color": "#3788d8",
  "url": "https://example.com/holidays.ics",
  "refresh_interval": "1h",
  "last_synced_at": "2024-01-21T10:00:00Z",
  "event_count": 42,
  "status": "synced",
  "created_at": "2024-01-21T10:00:00Z"
}
```

### Subscription Sync Status (200 OK)

```json
{
  "id": 1,
  "status": "error",
  "last_synced_at": "2024-01-20T10:00:00Z",
  "last_error": "HTTP 503: Service Unavailable",
  "error_count": 3,
  "next_sync_at": "2024-01-21T14:00:00Z"
}
```

### Sync Failure After Max Retries

```json
{
  "id": 1,
  "status": "disabled",
  "last_error": "Max retry attempts reached (5). Sync disabled.",
  "error_count": 5,
  "enabled": false
}
```

## Definition of Done

- [ ] `POST /api/v1/calendar-subscriptions` creates a new subscription
- [ ] Subscription calendar is created and populated with events
- [ ] Background worker syncs subscriptions based on interval
- [ ] Subscribed calendars are read-only
- [ ] Failed syncs are retried with exponential backoff
- [ ] CalDAV clients can read subscribed calendars
- [ ] Manual refresh endpoint works
- [ ] Unit tests for sync logic
- [ ] Integration tests for subscription lifecycle
