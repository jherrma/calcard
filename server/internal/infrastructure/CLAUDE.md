# Infrastructure Layer

This directory contains the technical implementations and external library integrations. It follows the principles of **Clean Architecture** by providing concrete implementations for the interfaces required by the application's upper layers.

## Subdirectories

### [database/](database/)

- **Purpose**: Manages the persistence layer.
- **Key Components**:
  - `database.go` — Unified database initialization based on configuration (auto-selects SQLite or PostgreSQL).
  - `sqlite.go` — SQLite driver setup using GORM. Opens through its own `database/sql` driver (`sqlite3_calcard`) that replaces SQLite's ASCII-only `lower()` with Go's Unicode `strings.ToLower` on every connection, so `LOWER()` folds case like PostgreSQL's (F26). Case-insensitive queries must use `LOWER(col) LIKE ?` with a Go-lower-cased, `escapeLike`d parameter (`repository.foldedLike`) — a plain `LIKE` folds ASCII on SQLite and nothing on PostgreSQL.
  - `postgres.go` — PostgreSQL driver setup using GORM.
  - `migrations.go` — Automatic database schema updates using GORM's `AutoMigrate`. Registers all domain models (User, Calendar, CalendarSubscription, Event, AddressBook, Contact, Sharing, etc.).
  - `data_migrations.go` — `RunDataMigrations`: data fix-ups `AutoMigrate` cannot express, run by `cmd/server` after it on **every boot** (with `auto_migrate`) and by `./server migrate`. Each step must be idempotent — a no-op once its rows are fixed — and runs in order (two are one-shot, see `unescapeTextColumns` and `narrowCalendarComponentSets`; `migrationBatchSize` is the one `FindInBatches` size): strip quoted ETags, wrap bare iCalendar objects, backfill `recurrence_end_time`, `dropRedundantCalendarIDIndex` (drops the old single-column `idx_calendar_objects_calendar_id` of `calendar_objects` when the composite `idx_calobj_cal_comp (calendar_id, component_type)` exists; `AutoMigrate` never drops an index, and fresh databases no longer create it), `backfillTaskFields` (#170: fills the task columns of VTODOs stored before they existed, soft-deleted rows included, via `UpdateColumns` that never touch `e_tag`/`updated_at`, so no DAV client re-downloads), `unescapeTextColumns` (**one-shot**: claims the marker `migration.unescape_text_columns.v1` in the same transaction as its writes, so the `LIKE` scan never runs again; an install that ran the earlier unmarked version runs it once more, idempotently, without waking DAV clients; re-derives the `summary`/`description`/`location` columns of rows stored while they kept the iCalendar's TEXT escaping, `Buy milk\, eggs`; only rows with a backslash in one of them are read, soft-deleted ones included, and only a changed row is written, via `UpdateColumns` of the three columns — never `e_tag`/`updated_at`/`i_cal_data`; re-derived from the iCalendar, so it never unescapes a column twice), `recordTaskKindCutoff` (writes `tasks.kind_introduced_at` = boot time, RFC 3339 UTC, to `system_settings` once and never overwrites it: the cutoff that tells legacy calendars from ones created with a deliberate `kind`), `narrowCalendarComponentSets` (#170, one-shot: it claims the `system_settings` marker `migration.narrow_component_sets.v1` with `INSERT … ON CONFLICT DO NOTHING` in the same transaction as its writes and does nothing once the marker exists, so a calendar widened afterwards is never narrowed again; a live, non-subscribed calendar created before the cutoff whose set is exactly `VEVENT,VTODO` becomes `VEVENT` when its live objects are all events and `VTODO` when they are all tasks; mixed, empty, `''`, subscribed and post-cutoff calendars, and any holding another component type, stay as they are; each narrowing goes through `CalendarRepository.UpdateSupportedComponents` (ctag, sync token, change-log anchor); `created_at` is compared with the cutoff in Go, never in SQL, because SQLite stores it as TEXT in the host's offset; a missing cutoff is an error), collection anchor rows, purge soft-deleted shares, `purgeDeletedAccounts` (F18: the data of accounts deleted before account deletion cascaded), `purgeDeletedCollections` (the remains of calendars and address books deleted before a collection delete removed its contents: soft-deleted collection rows and every row whose collection is not live, via `repository.PurgeDeletedCollections`; one transaction; logs the rows removed per table as a `WARN` and nothing when there was nothing to do), release deleted user identities (F19), release revoked credential usernames (F24). **The two purges permanently delete data** that its owners had already deleted and nobody can reach — that is their purpose, but it cannot be undone, so the README's upgrade notes advise a backup first. Neither may ever match a live row: test a new purge with live data next to the orphans.

### [server/](server/)

- **Purpose**: Manages the HTTP server lifecycle and request pipeline.
- **Key Components**:
  - `server.go` — Configures the Fiber application instance, including custom WebDAV HTTP methods (PROPFIND, PROPPATCH, MKCOL, REPORT, MKCALENDAR, etc.).
  - `routes.go` — Registers all API endpoints and injects handler dependencies. Initializes OAuth providers. This is the dependency injection root of the application. It also RETURNS the `BackgroundServices` it built (currently the story-100 subscription worker) rather than starting them: a goroutine spawned during wiring has no owner, and every test that constructs a server would leave one polling the database for the rest of the run. `server.go` starts them after the listener is up and stops them before closing the database.
  - `middleware.go` — Configures global HTTP middleware (CORS, Recovery, Request ID logging, security headers, rate limiting, TLS).

### [email/](email/)

- **Purpose**: Handles external communication services.
- **Key Components**:
  - `smtp.go` — SMTP email sender implementation for verification emails, password resets, etc. Satisfies the email service interface used by auth use cases. When SMTP is not configured (`cfg.SMTP.Host == ""`), new accounts are created verified (they are active either way).

### [logging/](logging/)

- **Purpose**: Security audit logging.
- **Key Components**:
  - `security_logger.go` — Logs security-relevant events (authentication attempts, password changes, etc.).

## Design Philosophy

The infrastructure layer is the "outermost" layer. It depends on internal layers (like `config` or `adapter`) but the internal business logic (in `domain` and `usecase`) never depends on code in this directory. Instead, they use interfaces which are fulfilled by the implementations found here.
