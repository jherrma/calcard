# Story 103: Event Default Settings

## Title

User-Configurable Default Event Duration and All-Day Toggle

## Description

As a user, I want to configure my default event duration, whether new events are all-day by default, and my preferred time format (12-hour or 24-hour), so that the event creation form and calendar display match my typical scheduling habits without requiring manual adjustment every time.

## Acceptance Criteria

### Backend: User Preferences Model

- [ ] New `UserPreference` domain entity in `server/internal/domain/user/`:
  ```go
  type UserPreference struct {
      ID        uint   `gorm:"primaryKey"`
      UserID    uint   `gorm:"uniqueIndex:idx_user_pref_key;not null"`
      Key       string `gorm:"uniqueIndex:idx_user_pref_key;size:100;not null"`
      Value     string `gorm:"size:500;not null"`
      CreatedAt time.Time
      UpdatedAt time.Time
  }
  ```
- [ ] Register `UserPreference` in GORM auto-migration (`server/internal/infrastructure/database/migrations.go`)
- [ ] Define preference key constants:
  - `default_event_duration` — integer, minutes (default: `60`)
  - `default_all_day` — boolean string `"true"` / `"false"` (default: `"false"`)
  - `time_format` — string `"12h"` / `"24h"` (default: `"24h"`)

### Backend: Preferences Repository

- [ ] Add `UserPreferenceRepository` interface to `server/internal/domain/user/repository.go`:
  ```go
  type UserPreferenceRepository interface {
      GetByUserID(ctx context.Context, userID uint) ([]UserPreference, error)
      GetByKey(ctx context.Context, userID uint, key string) (*UserPreference, error)
      Upsert(ctx context.Context, pref *UserPreference) error
      Delete(ctx context.Context, userID uint, key string) error
  }
  ```
- [ ] GORM implementation in `server/internal/adapter/repository/user_preference_repo.go`
- [ ] `Upsert` uses `ON CONFLICT (user_id, key) DO UPDATE` semantics

### Backend: Preferences API

- [ ] `GET /api/v1/users/me/preferences` — returns all preferences for the authenticated user
  - Response (200):
    ```json
    {
      "status": "ok",
      "data": {
        "preferences": {
          "default_event_duration": "60",
          "default_all_day": "false",
          "time_format": "24h"
        }
      }
    }
    ```
  - Returns defaults for keys that have not been explicitly set
- [ ] `PATCH /api/v1/users/me/preferences` — upserts one or more preferences
  - Request body:
    ```json
    {
      "preferences": {
        "default_event_duration": "30",
        "default_all_day": "true",
        "time_format": "12h"
      }
    }
    ```
  - Validate `default_event_duration` is an integer in `[15, 30, 45, 60, 90, 120, 180, 240, 480]`
  - Validate `default_all_day` is `"true"` or `"false"`
  - Validate `time_format` is `"12h"` or `"24h"`
  - Reject unknown preference keys with 400
  - Response (200): same shape as GET, returning the full updated preference map

### Backend: Use Case

- [ ] `GetPreferencesUseCase` in `server/internal/usecase/user/`
  - Fetches user preferences, merges with defaults for missing keys
- [ ] `UpdatePreferencesUseCase` in `server/internal/usecase/user/`
  - Validates values, upserts each key

### Frontend: Preferences Store

- [ ] New store `webinterface/stores/preferences.ts`:
  ```typescript
  interface PreferencesState {
    preferences: Record<string, string>;
    isLoaded: boolean;
  }
  ```
  - `fetchPreferences()` — GET, populates state
  - `updatePreferences(prefs: Record<string, string>)` — PATCH, updates state
  - `defaultEventDuration` getter — returns number (parsed from string, fallback 60)
  - `defaultAllDay` getter — returns boolean (parsed from string, fallback false)
  - `timeFormat` getter — returns `"12h"` or `"24h"` (fallback `"24h"`)

### Frontend: Calendar Settings Page

- [ ] New page `webinterface/pages/settings/calendar.vue`
- [ ] Add "Calendar" entry to the settings sidebar navigation (in the existing settings layout)
- [ ] Settings form with:
  - **Default event duration** — `Select` dropdown with options: 15 min, 30 min, 45 min, 1 hour, 1.5 hours, 2 hours, 3 hours, 4 hours, 8 hours
  - **Default all-day** — `InputSwitch` toggle
  - **Time format** — `SelectButton` with two options: 12-hour (`1:00 PM`) and 24-hour (`13:00`)
- [ ] Auto-save on change (with toast confirmation) or explicit Save button
- [ ] Load preferences on mount; show current values

### Frontend: EventForm Integration

- [ ] Update `webinterface/components/calendar/EventForm.vue`:
  - Import the preferences store
  - `defaultEnd()` uses `preferencesStore.defaultEventDuration` (minutes) instead of hardcoded 60
  - `form.all_day` initial value uses `preferencesStore.defaultAllDay` when creating (not editing)
  - `watch(form.start)` uses `preferencesStore.defaultEventDuration` instead of hardcoded `+1 hour`
  - `watch(form.all_day)` toggling off all-day uses `preferencesStore.defaultEventDuration` instead of hardcoded `+1 hour`
  - DatePicker `hour-format` bound to `preferencesStore.timeFormat === '12h' ? '12' : '24'`
- [ ] Update `webinterface/pages/calendar/index.vue`:
  - FullCalendar `slotLabelFormat` and `eventTimeFormat` use `preferencesStore.timeFormat` to switch between 12h/24h display
- [ ] Update `webinterface/components/calendar/EventDetailDialog.vue`:
  - Format displayed start/end times using `preferencesStore.timeFormat`
- [ ] Ensure preferences store is loaded before EventForm renders (fetch in calendar page `onMounted` or use a Nuxt plugin/middleware)

## Technical Notes

### Preference Defaults

| Key | Type | Default | Allowed Values |
|-----|------|---------|----------------|
| `default_event_duration` | string (integer minutes) | `"60"` | `"15"`, `"30"`, `"45"`, `"60"`, `"90"`, `"120"`, `"180"`, `"240"`, `"480"` |
| `default_all_day` | string (boolean) | `"false"` | `"true"`, `"false"` |
| `time_format` | string | `"24h"` | `"12h"`, `"24h"` |

### Backend Code Structure

```
server/internal/domain/user/
├── user.go                    # Existing
├── repository.go              # Add UserPreferenceRepository interface
└── preference.go              # NEW — UserPreference entity + defaults

server/internal/adapter/repository/
└── user_preference_repo.go    # NEW — GORM implementation

server/internal/adapter/http/
├── user_handler.go            # Add GetPreferences, UpdatePreferences handlers
└── dto/user.go                # Add PreferencesResponse, UpdatePreferencesRequest

server/internal/usecase/user/
├── get_preferences.go         # NEW
└── update_preferences.go      # NEW

server/internal/infrastructure/
├── database/migrations.go     # Register UserPreference
└── server/routes.go           # Register preference endpoints
```

### Frontend Code Structure

```
webinterface/
├── stores/preferences.ts                    # NEW — preferences state
├── pages/settings/calendar.vue              # NEW — calendar settings page
├── pages/calendar/index.vue                 # MODIFY — use time format for FullCalendar
├── components/calendar/EventForm.vue        # MODIFY — use preferences for defaults + time format
└── components/calendar/EventDetailDialog.vue # MODIFY — use time format for displayed times
```

### EventForm Changes (Pseudocode)

```typescript
// In EventForm.vue setup
const preferencesStore = usePreferencesStore();
const durationMinutes = computed(() => preferencesStore.defaultEventDuration);
const hourFormat = computed(() => preferencesStore.timeFormat === '12h' ? '12' : '24');

// defaultEnd() — for create mode only
const defaultEnd = () => {
  if (props.initialEnd) return new Date(props.initialEnd);
  if (props.event) return new Date(props.event.end);
  const d = new Date(defaultStart());
  d.setMinutes(d.getMinutes() + durationMinutes.value);
  return d;
};

// form.all_day default
all_day: props.event?.all_day ?? props.initialAllDay ?? preferencesStore.defaultAllDay,

// DatePicker uses computed hourFormat
// <DatePicker :hour-format="hourFormat" ... />

// watch(form.start) — apply configured duration
watch(() => form.start, (newStart) => {
  if (newStart && !form.all_day) {
    const end = new Date(newStart);
    end.setMinutes(end.getMinutes() + durationMinutes.value);
    form.end = end;
  }
});

// watch(form.all_day) — toggling off all-day
watch(() => form.all_day, (newVal, oldVal) => {
  if (oldVal && !newVal) {
    const now = new Date();
    const start = new Date(form.start);
    start.setHours(now.getHours(), 0, 0, 0);
    form.start = start;
    const end = new Date(start);
    end.setMinutes(end.getMinutes() + durationMinutes.value);
    form.end = end;
  }
});
```

### Calendar View Time Format (Pseudocode)

```typescript
// In pages/calendar/index.vue
const preferencesStore = usePreferencesStore();
const is12h = computed(() => preferencesStore.timeFormat === '12h');

// FullCalendar options
const calendarOptions = computed(() => ({
  // ...existing options...
  slotLabelFormat: {
    hour: 'numeric',
    minute: '2-digit',
    hour12: is12h.value,
  },
  eventTimeFormat: {
    hour: 'numeric',
    minute: '2-digit',
    hour12: is12h.value,
  },
}));
```

### API Route Registration

```go
// In routes.go, under authenticated user routes:
users.Get("/me/preferences", userHandler.GetPreferences)
users.Patch("/me/preferences", userHandler.UpdatePreferences)
```

## Definition of Done

- [ ] `UserPreference` model created and auto-migrated
- [ ] `GET /api/v1/users/me/preferences` returns user preferences with defaults
- [ ] `PATCH /api/v1/users/me/preferences` validates and persists preferences
- [ ] Settings page at `/settings/calendar` allows changing default duration, all-day, and time format
- [ ] EventForm uses configured duration for new events (not hardcoded 1 hour)
- [ ] EventForm uses configured all-day default for new events
- [ ] EventForm DatePicker uses configured time format (12h/24h)
- [ ] Calendar view (FullCalendar) displays times in the configured format
- [ ] Event detail dialog displays times in the configured format
- [ ] Changing start time adjusts end time by the configured duration
- [ ] Backend unit tests for preference use cases
- [ ] Frontend preferences store works correctly
