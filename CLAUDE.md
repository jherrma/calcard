# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a CalDAV/CardDAV server project with a Go backend and Nuxt 4 web interface. The server implements RFC 4791 (CalDAV) and RFC 6352 (CardDAV) protocols for calendar and contact synchronization.

## Project Structure

```
/
├── README.md                # Quick start, deployment, API doc pointers
├── CONFIGURATION.md         # Every YAML key and CALDAV_* environment variable
├── STORIES.md               # Implementation tracker + the consolidated remaining-work list
├── docs/                    # README screenshots; release-notes/ = drafts for the GitHub release body
├── docker-compose/          # Example compose files (SQLite, PostgreSQL) using herrj/calcard
├── stories/                 # User stories for implementation
│   ├── backend/             # Backend stories (Go server)
│   ├── continuation/        # Stories after core features are implemented
│   └── frontend/            # Frontend stories (web interface)
├── server/                  # Go backend implementation
│   ├── cmd/server/          # Application entrypoint
│   ├── configs/             # Configuration examples
│   ├── integration/         # End-to-end integration tests (build-tag `integration`)
│   ├── internal/            # Internal packages
│   │   ├── adapter/         # HTTP handlers, repositories, WebDAV
│   │   ├── domain/          # Domain models and interfaces
│   │   ├── infrastructure/  # Database, email, server setup
│   │   └── usecase/         # Business logic use cases
│   └── Dockerfile
└── webinterface/            # Nuxt 4 SPA frontend
    ├── pages/               # File-based routing
    │   ├── index.vue        # Dashboard — the landing page after login (story 042)
    │   ├── auth/            # Login, register, setup, forgot/reset password, OAuth callback, verify
    │   ├── calendar/        # Calendar view with FullCalendar
    │   ├── contacts/        # Contact list with search, grouping, detail panel
    │   ├── tasks/           # Tasks section (#170): list sidebar, toolbar, quick-add, grouped list; ?list=<calendar uuid>
    │   ├── search.vue       # Full global-search results page (?q=…)
    │   └── settings/        # Profile, appearance (theme + accent), calendar, credentials, MCP access, sharing, admin, …
    ├── components/
    │   ├── auth/            # PasswordStrength
    │   ├── calendar/        # CalendarSidebar, CalendarToolbar, EventForm, Event*Dialog, RecurrenceScopeDialog
    │   ├── contacts/        # ContactsSidebar, ContactListItem, AlphabetNavigation
    │   ├── tasks/           # TasksSidebar, TaskToolbar, TaskQuickAdd, TaskList, TaskGroupHeader, TaskRow, TaskCheck,
    │   │                    #   TaskEmptyState, TaskForm, Task*Dialog, AddTaskListDialog, TaskListSettingsDialog (#170)
    │   ├── dashboard/       # WidgetCard, TodayAgendaCard, UpcomingEventsCard, MiniCalendarCard, RecentContacts*, QuickStatsCard
    │   ├── settings/        # DavCredentialManager, DavUrlBox (copyable /dav/<username>/… URLs)
    │   ├── sharing/         # SharePanel, PublicLinkPanel, ShareDialog (calendars + address books)
    │   └── common/          # AppHeader, AppSidebar (Tasks entry + overdue badge), GlobalSearch, Search*, HighlightText, ThemeToggle,
    │                        #   LoadingSpinner, SkeletonList, ActionToast ("Task completed" + Undo), ReminderToast (#169), ReminderField (#169, event form)
    ├── stores/              # Pinia stores: alarms (#169 reminders), auth, calendars, contacts, dashboard, preferences, search, sharing, tasks
    ├── composables/         # useApi, useAppToast, useTheme (light/dark/system), useAccentColor — both singletons
    ├── utils/               # Auto-imported helpers: dashboardDates, agendaLayout, contactAvatar, theme, accent, tasks, taskForm, taskLists, alarmSchedule, …
    ├── middleware/           # auth (requires login), guest (redirects if logged in)
    ├── layouts/             # default (sidebar + header), auth (centered card)
    ├── types/               # TypeScript interfaces: auth, calendar, contacts, search, api, tasks, alarms
    └── plugins/             # PrimeVue services, theme boot (story 046), timezone-default PUT (#169), alarms.client.ts (#169)
```

## AGENT.md and CLAUDE.md Files

`AGENT.md` and `CLAUDE.md` files are placed throughout the project to provide context-specific guidance:

- `/AGENT.md` - Root level project overview
- `/server/AGENT.md` - Server directory structure, startup sequence, API surface
- `/server/internal/AGENT.md` - Backend architecture overview
- `/server/internal/adapter/AGENT.md` - Adapter layer details
- `/server/internal/domain/AGENT.md` - Domain layer details
- `/server/internal/infrastructure/AGENT.md` - Infrastructure layer details
- `/server/internal/usecase/AGENT.md` - Use case layer details
- `/server/internal/usecase/auth/AGENT.md` - Authentication use cases
- `/webinterface/AGENT.md` - Frontend architecture and conventions

- `/CLAUDE.md` - Root level project overview
- `/server/CLAUDE.md` - Server directory structure, startup sequence, API surface
- `/server/integration/CLAUDE.md` - End-to-end integration test suite (how it boots the server, what it covers, how to add new tests)
- `/server/internal/CLAUDE.md` - Backend architecture overview
- `/server/internal/adapter/CLAUDE.md` - Adapter layer details
- `/server/internal/domain/CLAUDE.md` - Domain layer details
- `/server/internal/infrastructure/CLAUDE.md` - Infrastructure layer details
- `/server/internal/usecase/CLAUDE.md` - Use case layer details
- `/server/internal/usecase/auth/CLAUDE.md` - Authentication use cases
- `/webinterface/CLAUDE.md` - Frontend architecture and conventions

**Always check relevant AGENT.md files when working in a specific area of the codebase.**

These files ARE the architecture documentation. `Overview.md`, `Technical Overview.md` and
`Acceptance Criteria.md` used to sit at the repo root and were deleted in `61f9e04` when their
content was split into the per-directory `CLAUDE.md` / `AGENT.md` files above — don't cite or
look for them. Feature status lives in `STORIES.md`; per-story detail in `stories/`.

## Development Commands

```bash
# Backend
cd server && go build ./...
cd server && go test ./...                                 # unit / handler tests (fast)
cd server && go test -tags=integration ./integration/...   # full end-to-end suite

# Frontend
cd webinterface && pnpm dev                          # Dev server
cd webinterface && pnpm nuxt typecheck && pnpm test  # Verification gate (typecheck + Vitest specs)
cd webinterface && pnpm test                         # Vitest specs (run once)
cd webinterface && pnpm test:watch                   # Vitest specs (watch mode)
cd webinterface && pnpm build                        # Production build

# Docker
cd docker-compose && docker compose up                                    # SQLite (herrj/calcard image)
cd docker-compose && docker compose -f docker-compose.postgres.yml up     # PostgreSQL
```

## Key Technologies

- **Backend**: Go 1.22+, Fiber v3, GORM
- **Database**: SQLite (default), PostgreSQL (production)
- **Protocols**: CalDAV (RFC 4791), CardDAV (RFC 6352), WebDAV-Sync
- **Auth**: JWT, OAuth2/OIDC
- **Frontend**: Nuxt 4 (SPA mode), Vue 3 Composition API, TypeScript (strict)
- **UI**: PrimeVue 4.x (Material preset), Tailwind CSS 3, PrimeIcons
- **Frontend Libraries**: Pinia (state), VueUse, Vuelidate, FullCalendar

## Frontend Architecture

### Auto-imports (do NOT use explicit imports for these)

- `defineStore` — auto-imported by `@pinia/nuxt`
- `useDebounceFn`, `useMediaQuery`, etc. — auto-imported by `@vueuse/nuxt`
- Vue APIs (`ref`, `computed`, `reactive`, `onMounted`, etc.) — auto-imported by Nuxt
- Components in `components/` — auto-imported by Nuxt (use `<AuthPasswordStrength>`, not `<PasswordStrength>`)

### API Layer

- `useApi()` composable wraps `$fetch.create()` with auth headers and response unwrapping
- Two response shapes. Only `/auth/*` (setup, register, login, refresh, logout, verify, forgot/reset password, OAuth link), `/system/*`, `/users/me` (+ password, preferences), `/app-passwords`, `/about/open-source` and `GET /health` wrap their payload in `{ "status": "ok", "data": ... }` (`SuccessResponse`); the `onResponse` handler in `useApi` unwraps `.data` automatically
- **Everything else returns raw JSON**, which comes through as-is: calendars (incl. shares, public access), events, tasks (`/calendars/:calendar_id/tasks`, `/tasks`, `/tasks/summary`), address books (incl. shares), contacts, `/contacts/search`, `/search`, CalDAV/CardDAV credentials, import, `/mcp-tokens`, `/calendar-subscriptions`, `GET /auth/oauth/providers` and `GET /ready`. Check the handler before assuming a shape
- Errors are usually `{ "status": "error", "message": ... }` (`ErrorResponse`), but some raw-JSON handlers answer `{ "error": ... }`
- `POST /api/v1/auth/register` answers **200**, not 201, although it creates an account; kept as is so existing clients don't break
- Backend uses `expires_at` (Unix timestamp) for JWT expiry, NOT `expires_in`

### API Response Shapes (Contacts/AddressBooks)

- `GET /api/v1/addressbooks` → `{ "addressbooks": [...] }`
- `GET /api/v1/addressbooks/:id/contacts` → `{ "Contacts": [...], "Total", "Limit", "Offset" }` (capital C)
- `GET /api/v1/contacts/search?q=...` → `{ "contacts": [...], "query", "count" }` (lowercase).
  Searches every address book the caller can read — own **and shared** (#162). Optional
  `addressbook_id` narrows to one book by UUID; a book they can't read is a **404**, never an
  empty 200
- `GET /api/v1/search?q=&types=&limit=&offset=` → grouped unified search (#156): `events`,
  `contacts`, `calendars`, `addressbooks`, each `{ items, count, has_more, searched }`. Raw JSON.
  Events have **no date bound** unless `start`/`end` are passed; `limit` is per group, capped at
  `max_limit` (100)
- `DELETE` endpoints return 204 No Content. Deleting a calendar or address book is permanent on the server: everything in it (events/contacts, photos, shares, public link) is hard-deleted with it — there is nothing to restore
- Not found is a **404** on every verb (F05): an unknown contact (also for `DELETE` and the
  photo routes), and an address book that is unknown, another user's, or only shared with the
  caller on `GET`/`PATCH`/`DELETE`/`export` — these are owner-only, and a foreign book answers
  byte-for-byte like an unknown one, checked before the body is parsed

### API Response Shapes (Calendars/Tasks, #170)

- `GET /api/v1/calendars` → `{ "calendars": [...] }`. Every calendar answer (list, get, create, update,
  `/search` calendar hits) carries a read-only `kind` — `events` (`VEVENT`), `tasks` (a task list,
  `VTODO`) or `both` (`VEVENT,VTODO`, also a legacy `""`) — next to `supported_components`. `POST`
  takes an optional `kind` (default `events`); `PATCH` accepts only `"kind": "both"` (widening) or the calendar's own kind (a no-op 200); any
  narrowing is a **409**). `event_count` counts events only
- `GET /api/v1/calendars/:calendar_id/tasks` → `{ "tasks": [...], "count", "total" }` (lowercase).
  `status` = `open` (default) | `completed` (completed **and** cancelled) | `all`; `limit` default 500,
  max 1000. Task `due` is UTC (`Z`); an all-day task has `due: null` and `due_date: "YYYY-MM-DD"`
- `GET /api/v1/tasks` → the same shape across every readable task list, each task plus
  `calendar_name`/`calendar_color`; `calendar_uuid` narrows (unreadable = **404**), `due_after`/`due_before`
  bound the due date
- `GET /api/v1/tasks/summary?as_of=<RFC 3339 with offset>` → `{ "open", "overdue", "due_today",
  "completed", "by_calendar": { "<calendar uuid>": { "open", "overdue", "completed" } } }`. Pass `as_of` (and the
  `due_*` bounds) through ofetch's `query` option: an interpolated `+01:00` arrives as a space and is a 400
- `reminders` on events and tasks (#169): every event (`GET …/events`, `GET …/events/:id`, create/update answers, `/search` event hits) and every task answer carries `reminders`, an array (`[]` for none, never null) of `{ "index", "action" (lower-case, `display` when absent), "related" (`start`/`end`/null), "offset_seconds" (signed, a nominal day = 86400, null for an absolute or unreadable trigger), "at" (UTC instant of an absolute trigger, a floating one as its wall clock in UTC, else null), "editable" }`. It lists VALARMs written by DAV clients too. `editable` is true only for DISPLAY/AUDIO/absent action, no `REPEAT`, no snooze, and a relative trigger anchored at `start` (events) / `end` (tasks) or a non-floating absolute one. An expanded occurrence reports its override's alarms when it has an override, else the master's. Events and tasks also accept `reminders` on create and update (the write side is described in the bullets below)
- Writing `reminders` on events (#169, story 11): `POST …/events` takes `reminders: [{ "offset_seconds": -900 } | { "at": "<RFC 3339 with offset>" }]` (optional, at most 10, exactly one key per entry); `PATCH …/events/:id` takes the same with three meanings: key **absent** = every VALARM untouched, **`null` or `[]`** = remove every editable VALARM of the target component, **an array** = the desired multiset of editable reminders (matched ones stay byte for byte, unmatched editable ones go, new ones are appended). A relative entry is written `TRIGGER:-PT15M` (no `RELATED`, = start), an absolute one `TRIGGER;VALUE=DATE-TIME:<UTC>Z`, in a `VALARM` with `ACTION:DISPLAY`, `DESCRIPTION:Reminder` and a UID. Send a stored reminder back unchanged (an absolute one as the `at` the server returned) and nothing is written, so an otherwise unchanged PATCH keeps the ETag. 400 texts: `reminders must contain at most 10 entries`, `a reminder needs exactly one of offset_seconds and at`, `reminder offset out of range` (±400 days), `Invalid reminder time`, `reminder time out of range` (±400 days of the start); the two range checks skip entries that match a stored VALARM. Non-editable VALARMs (EMAIL, REPEAT, RELATED=END, floating, snooze) are never touched
- Writing `reminders` on tasks (#169, story 12): `POST …/tasks` and `PATCH …/tasks/:task_id` take the same `reminders` field as events (absent / `null` or `[]` / array, at most 10, the same 400 texts), but a relative entry counts from the **due date** and is written `TRIGGER;RELATED=END:-PT30M` (an all-day task's `{"offset_seconds":32400}` is `TRIGGER;RELATED=END:PT9H`); an absolute `at` is the same as for events, its range checked against now (±400 days). `400 A reminder needs a due date` for an `offset_seconds` entry on a task that after the request has neither DUE nor DTSTART+DURATION (an `at` entry needs no date; entries that match a stored VALARM are never refused, so a due-less task can send its list back). Removing the due keeps the VALARMs. A reminders-only PATCH on a recurring task edits the master; the order of refusals is the field 400s (reminders included), then the 409. The same list again writes nothing (ETag kept); a PATCH without `reminders`, `complete` and `uncomplete` leave every VALARM byte for byte. MCP `create_task`/`update_task` pass no reminders.
- `GET /api/v1/alarms/upcoming?until=<RFC 3339 with offset>` (#169) → raw JSON `{ "alarms": [...], "count", "from", "until", "server_time", "truncated" }`, `Cache-Control: no-store`. Window `[server now − 24 h, until]`, `until` default now + 24 h and clamped to now + 7 d (the answer's `until` is the value used); `until` before `from` is an empty list; an `until` without an offset (also an unencoded `+`, which arrives as a space) is a **400** `Invalid 'until' value`: pass it through ofetch's `query`. Each alarm carries `key` (32 hex), `trigger_at` (UTC), `uid`, `recurrence_id` (null for a non-series alarm), `alarm_index`, `action` (lower-case), `description`, `object_type` (`event`|`task`), `object_id`, `calendar_id`/`_uuid`/`_name`/`_color`, `summary`, `location`, `all_day`, and `start`/`start_date`/`end` (events) or `due`/`due_date` (tasks); every nullable key is present. Sorted by `trigger_at` then `key`, at most 500 (`truncated`). Covers readable calendars (own and shared, never subscribed); floating and all-day triggers use the calendar OWNER's time zone, so there is no zone parameter. 404 (not the SPA) when the operator sets `alarms.enabled: false`. `POST /api/v1/alarms/acknowledge` (#169) takes `{ "keys": [...] }` (1 to 500 keys from the list, each `^[0-9a-f]{32}$`, duplicates allowed) and answers **204** with no body, also on a repeat; it is idempotent and per user (an acknowledged key leaves only the caller's `upcoming`, on every device). 400 `Invalid request body`, `keys is required`, `Too many keys: at most 500 per request`, `Invalid alarm key at index <n>`; a refused batch writes nothing; a key that belongs to no alarm is accepted. A moved alarm has a new key and is listed again. 404 with `alarms.enabled: false`. An absolute alarm of a recurrence override reports the same `start`/`end` as its relative alarms: the override's `DTSTART` (else the occurrence's RECURRENCE-ID time) and its `DTEND` (else start plus the master's duration), so a moved override shows the moved time; a master's absolute alarm (`recurrence_id` null) reports the object's own start.
- A recurring task (`is_recurring`) cannot be completed over REST: complete, or PATCH `status`
  `completed`/`cancelled`/`percent_complete: 100`, answers **409**. Uncomplete resets the percentage to 0
  only when it was 100
- `DELETE /api/v1/calendars/:calendar_id/tasks/:task_id` is **permanent** (a hard delete; nothing to
  restore, so an "Undo" must re-create the task, not un-delete it); 204, then 404 `Task not found`
- Event and task `summary`, `description` and `location` are **plain text** (the server undoes and
  redoes the iCalendar escaping): show them as they are and send them back unchanged, never escape or
  unescape `,` `;` `\` or newlines in the SPA

### Patterns

- **Stores**: Use `useApi()` inside actions, follow `stores/calendars.ts` and `stores/contacts.ts` as reference
- **Composables**: `useApi()`, `useAppToast()`, `useConfirm()` (from PrimeVue)
- **Styling**: Tailwind with `surface-*` colors, `dark:` variants, PrimeVue components
- **Icons**: PrimeIcons (`pi pi-*`)
- **Sidebar pattern**: `w-64`, `hidden lg:flex`, border-right, same bg as CalendarSidebar
- **PrimeVue theming**: Component design tokens require `root` nesting (e.g., `card: { root: { borderRadius: '...' } }`)
- **TypeScript strict mode** with `noUncheckedIndexedAccess` — array `[0]` returns `T | undefined`, use `.charAt(0)` for strings or `!` assertions for known-valid access

### Key Endpoints

- `GET /api/v1/system/settings` — Returns `admin_configured`, `smtp_enabled`, `registration_enabled`, `setup_token_required`, `version`
- `GET /api/v1/auth/methods` — Returns available auth methods (local, OAuth providers)
- `GET /api/v1/about/open-source` — Authenticated: Go dependency attribution list (embedded, generated by `server/tools/genlicenses`)
- `PUT /api/v1/users/me/preferences/timezone-default` — sets the user's `timezone` preference only if none is stored; body `{ "timezone" }`, answers `{status,data:{timezone}}` with the stored value (#169). Sent once per tab by `plugins/timezone.client.ts`
- `GET`/`POST /api/v1/mcp-tokens`, `DELETE /api/v1/mcp-tokens/:id` — MCP access tokens (#104). Raw JSON, no `{status,data}` envelope. `POST` is the ONLY time the secret exists: it is stored as a SHA-256 and can never be shown again
- `GET`/`POST /api/v1/calendar-subscriptions`, `GET`/`PATCH`/`DELETE /:id`, `POST /:id/refresh` — remote calendar subscriptions (story 100). Raw JSON. A subscription mirrors an iCalendar feed into a **read-only** calendar (`Calendar.subscribed`); a failed refresh answers **200** with `status: "error"`, because the request succeeded and the third-party feed is what failed
- `GET`/`POST /api/v1/calendars/:calendar_id/tasks`, `GET`/`PATCH`/`DELETE /:task_id`, `POST /:task_id/complete`, `POST /:task_id/uncomplete` — the tasks (VTODOs) of one task list (#170). Raw JSON. A task list is a calendar with `kind: "tasks"`; every miss on a task route (unknown or unreadable calendar, unknown task, an event's UUID) is the same 404 `Task not found`, except the list and create routes, which answer an unknown or unreadable calendar with 404 `Calendar not found`
- `GET /api/v1/tasks` — tasks across every task list the caller can read, one query (#170). Raw JSON
- `GET /api/v1/tasks/summary?as_of=` — open/overdue/due-today/completed counts, overall and per list (`by_calendar`), counted in the database; `as_of` is required and its UTC offset decides which date is today (#170). Raw JSON. Full rules for every task route: `server/CLAUDE.md`, "Tasks (#170)"
- `POST /mcp` — Model Context Protocol endpoint (#104). Outside `/api/v1` on purpose: MCP addresses one URL that clients are configured with directly. `GET` returns a manifest, `DELETE` ends a session
- Auth pages: login, register, setup (first admin), forgot-password, reset-password, verify, OAuth callback

## Working Agreements

1. **Follow the stories**: implementation details are in `stories/backend/`, `stories/frontend/` and `stories/continuation/`. Check `/STORIES.md` for what is already implemented.
2. **Check existing patterns**: review existing code in a similar area before adding a feature. The calendar and contacts implementations share many patterns.
3. **Propose alternatives**: if a story's proposed approach has a problem, propose a different one rather than implementing a known-bad design.
4. **Write tests**: write unit and integration tests for every implementation.
5. **Check the CLAUDE.md / AGENT.md files**: read the relevant file before working in a sub-area of the codebase.

## Story Tracking

After implementing a story, update `/STORIES.md` to mark it as **Done**. This file tracks the implementation status of all stories (backend, frontend, and continuation).
