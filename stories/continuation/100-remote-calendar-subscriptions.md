# Story 100: Remote Calendar Subscriptions

## Title

Subscribe to Remote Calendars via URL

## Description

As a user, I want to subscribe to external calendars via URL (WebCal/iCalendar feeds) so that I can view events from other services (e.g., Google Calendar, Outlook, sports schedules) that automatically stay synchronized with the remote source.

## Implementation status: **Done** (2026-08-25)

Implemented end to end — domain model, repository, use cases, background worker, REST endpoints,
read-only enforcement across every write surface, and a settings page. Verified against a real
public feed (`https://www.Der-Mond.de/ical/vollmond.ics`, 86 events): the create, refresh and
worker paths all round-trip it with a byte-stable result, so a re-fetch of unchanged content
reports zero changes and does not touch the collection.

### Where the code lives

| Piece | File |
| --- | --- |
| Entity, status/backoff rules, repository interfaces | `internal/domain/calendar/subscription.go`, `repository.go` |
| `Calendar.Subscribed` + `EffectivePermission` | `internal/domain/calendar/calendar.go` |
| GORM repository | `internal/adapter/repository/subscription_repo.go` |
| Transactional feed diff | `CalendarRepository.ReplaceFeedObjects` in `internal/adapter/repository/calendar_repo.go` |
| SSRF-guarded fetcher | `internal/usecase/subscription/fetch.go` |
| Feed parser (UID grouping, VTIMEZONE) | `internal/usecase/subscription/parse.go` |
| Sync + CRUD use cases | `internal/usecase/subscription/{sync,create,manage}.go` |
| Background worker | `internal/usecase/subscription/worker.go` |
| REST handler | `internal/adapter/http/subscription_handler.go` |
| Settings page | `webinterface/pages/settings/subscriptions.vue` |

### Departures from this story, and why

1. **Read-only is enforced by capping the resolved permission, not by a check per write site.**
   `calendar.EffectivePermission` caps a subscribed calendar at `PermissionRead`, and the three
   adapters that resolve a permission (REST, CalDAV, MCP) all funnel through it. A write path that
   already refuses `PermissionRead` therefore needs no new code and cannot be forgotten. It caps
   only OBJECT access: the owner keeps rename, recolour, share and delete, because those check
   `cal.UserID` directly. The CalDAV collection-DELETE and PROPPATCH paths were changed from
   `perm == PermissionOwner` to an ownership comparison for exactly that reason.

2. **"Preserve local modifications (optional setting)" was dropped.** The calendar is read-only,
   so there are no local modifications to preserve. Keeping the setting would mean allowing writes
   that the next refresh silently discards, which is worse than refusing them.

3. **The resourcetype stays `<C:calendar/>`; `CS:subscribed` is NOT advertised.** A client that
   does not know the CalendarServer subscribed type would stop treating the collection as a
   calendar and hide its events entirely. The read-only nature is advertised through
   `DAV:current-user-privilege-set` instead — which every CalDAV client understands — plus
   `CS:source` naming the feed. Adding privileges also fixed the same gap for read-only *shares*,
   which previously advertised write.

4. **`RefreshInterval` is a closed set, not "anything >= 15m".** A minimum alone lets a caller
   pick `15m1s` to look compliant while polling continuously; a closed set also keeps the UI a
   dropdown.

5. **Backoff is anchored to the subscription's own interval**, not a fixed base: a 15-minute feed
   should retry sooner than a daily one. It is capped at 24h and the shift is guarded, so a
   long-broken feed cannot overflow the duration into a negative value and become a hot loop.

6. **The feed is fetched and validated BEFORE anything is written** (as the story asks), and a
   failure after the calendar is created rolls the calendar back. A subscribed calendar without
   its subscription row is unreachable wreckage: read-only to every write path, and refreshed by
   nothing.

7. **Deleting a calendar cascades to its subscription**, in `CalendarRepository.Delete`. A
   calendar can be deleted from three places and only one of them knows subscriptions exist; an
   orphan would keep fetching a third party's feed forever for a calendar the user believes is
   gone.

8. **A failed manual refresh answers 200, not 5xx.** The request succeeded; the third-party feed
   is what failed, and the client needs the updated subscription state to render the reason.

### Security notes

A subscription URL is attacker-chosen input that the SERVER fetches — the classic SSRF shape.
The guard lives in the HTTP client's dial `Control` hook, which sees the ALREADY RESOLVED
`ip:port` immediately before connect, so DNS rebinding (the standard bypass for a pre-flight
lookup) is caught, and every redirect hop and every address in a round-robin is checked for free.
Non-public addresses, plain `http://`, non-http schemes and URLs with embedded credentials are all
refused; `CALDAV_SUBSCRIPTIONS_ALLOW_PRIVATE_HOSTS` / `..._ALLOW_INSECURE_URLS` widen this
deliberately. Failure messages never repeat the feed URL, which routinely carries a secret token.

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
