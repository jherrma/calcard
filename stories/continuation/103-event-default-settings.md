# Story 103: Event Default Settings

## Title

User-Configurable Default Event Duration and All-Day Toggle

## Description

As a user, I want to configure my default event duration and whether new events are all-day by default, so that the event creation form matches my typical scheduling habits without requiring manual adjustment every time.

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
          "default_all_day": "false"
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
        "default_all_day": "true"
      }
    }
    ```
  - Validate `default_event_duration` is an integer in `[15, 30, 45, 60, 90, 120, 180, 240, 480]`
  - Validate `default_all_day` is `"true"` or `"false"`
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

### Frontend: Calendar Settings Page

- [ ] New page `webinterface/pages/settings/calendar.vue`
- [ ] Add "Calendar" entry to the settings sidebar navigation (in the existing settings layout)
- [ ] Settings form with:
  - **Default event duration** — `Select` dropdown with options: 15 min, 30 min, 45 min, 1 hour, 1.5 hours, 2 hours, 3 hours, 4 hours, 8 hours
  - **Default all-day** — `InputSwitch` toggle
- [ ] Auto-save on change (with toast confirmation) or explicit Save button
- [ ] Load preferences on mount; show current values

### Frontend: EventForm Integration

- [ ] Update `webinterface/components/calendar/EventForm.vue`:
  - Import the preferences store
  - `defaultEnd()` uses `preferencesStore.defaultEventDuration` (minutes) instead of hardcoded 60
  - `form.all_day` initial value uses `preferencesStore.defaultAllDay` when creating (not editing)
  - `watch(form.start)` uses `preferencesStore.defaultEventDuration` instead of hardcoded `+1 hour`
  - `watch(form.all_day)` toggling off all-day uses `preferencesStore.defaultEventDuration` instead of hardcoded `+1 hour`
- [ ] Ensure preferences store is loaded before EventForm renders (fetch in calendar page `onMounted` or use a Nuxt plugin/middleware)

## Technical Notes

### Preference Defaults

| Key | Type | Default | Allowed Values |
|-----|------|---------|----------------|
| `default_event_duration` | string (integer minutes) | `"60"` | `"15"`, `"30"`, `"45"`, `"60"`, `"90"`, `"120"`, `"180"`, `"240"`, `"480"` |
| `default_all_day` | string (boolean) | `"false"` | `"true"`, `"false"` |

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
└── components/calendar/EventForm.vue        # MODIFY — use preferences for defaults
```

### EventForm Changes (Pseudocode)

```typescript
// In EventForm.vue setup
const preferencesStore = usePreferencesStore();
const durationMinutes = computed(() => preferencesStore.defaultEventDuration);

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
- [ ] Settings page at `/settings/calendar` allows changing default duration and all-day
- [ ] EventForm uses configured duration for new events (not hardcoded 1 hour)
- [ ] EventForm uses configured all-day default for new events
- [ ] Changing start time adjusts end time by the configured duration
- [ ] Backend unit tests for preference use cases
- [ ] Frontend preferences store works correctly
