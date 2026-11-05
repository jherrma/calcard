# Server (Go Backend)

The Go backend implements the CalDAV/CardDAV server, REST API, and protocol handlers. It follows **Clean Architecture** principles with strict dependency rules between layers.

## Directory Structure

```
server/
├── cmd/server/          # Application entrypoint (main.go)
├── configs/             # Configuration examples (config.yaml.example)
├── integration/         # End-to-end integration tests (build-tag `integration`) — see integration/CLAUDE.md
├── internal/            # Core application code (see AGENT.md files below)
│   ├── config/          # Configuration loading (env vars + YAML)
│   ├── domain/          # Domain models, repository interfaces (zero dependencies)
│   ├── usecase/         # Business logic (depends only on domain)
│   ├── adapter/         # HTTP handlers, repositories, auth, WebDAV, MCP (depends on domain + usecase)
│   └── infrastructure/  # Database, server, email, logging (outermost layer)
├── scripts/             # Helper scripts
│   ├── build.sh         # Build binary to bin/server
│   └── test.sh          # Run tests
├── tools/               # Build-time generators (not part of the server binary)
│   └── genlicenses/     # Writes internal/usecase/about/open_source_go.json (#101)
├── Dockerfile           # Multi-stage Alpine build (static binary)
└── (compose files live in ../docker-compose/)
```

## Startup Sequence

The entrypoint (`cmd/server/main.go`) follows this sequence:

1. Load configuration from environment variables and `config.yaml`.
2. Validate configuration.
3. Initialize database connection (SQLite or PostgreSQL via GORM).
4. Handle CLI commands (e.g., `./server migrate` for manual migrations).
5. Auto-migrate database schema if `database.auto_migrate` is enabled.
6. Initialize Fiber server, register routes (dependency injection happens in `infrastructure/server/routes.go`), and start listening.

## API Surface

The server exposes three types of endpoints:

### REST API (`/api/v1/...`)

JSON API for the web frontend. Two response shapes coexist: `/auth/*` (setup, register, login, refresh, logout, verify, forgot/reset password, OAuth link), `/system/*`, `/users/me` (+ password, preferences), `/app-passwords`, `/about/open-source` and `GET /health` wrap their payload in `{ "status": "ok", "data": ... }` via `SuccessResponse()`; everything else returns raw JSON (see below).

| Area | Endpoints |
|------|-----------|
| Auth | First-run setup (`POST /auth/setup`, creates the admin, 409 once any account has ever existed; optionally guarded by `CALDAV_SETUP_TOKEN`, which also locks registration until setup is done), login, register, verify (`GET /auth/verify?token=`; the activation mail links the SPA page `/auth/verify?token=`, which calls it, as the reset mail links `/auth/reset-password`), refresh, logout, forgot/reset password. Every endpoint that sets a password (setup, register, reset, `PUT /users/me/password`) applies `user.ValidatePassword` — 8 to 72 **bytes** (bcrypt's limit), upper, lower, digit, special — and answers a violation with a 400 naming the rule; login applies none of it |
| OAuth | Initiate, callback, link/unlink providers |
| System | Settings (admin configured, SMTP enabled, registration enabled, setup token required), auth methods |
| User | Get/update profile, delete account, get/update preferences (incl. `timezone`, an IANA zone, default `UTC`, "not set" = no row; #169), `PUT /users/me/preferences/timezone-default` (set-if-empty: stores `{timezone}` only when the user has none, answers `{status,data:{timezone}}` with the stored zone; 400 for an invalid zone or body). The profile (and the login response's `user`) carries `username`, the account's `/dav/{username}/` segment, exactly as stored |
| Calendars | CRUD, public sharing, export. `POST /calendars` takes an optional `kind` (#170): `events` (the default, stores `VEVENT`), `tasks` (a task list, `VTODO`) or `both` (`VEVENT,VTODO`); anything else is 400 `kind must be "events", "tasks" or "both"`. Every calendar answer — list, get, the `POST` 201 and `PATCH` 200 bodies, `/search` calendar hits, MCP calendar output — carries the derived, read-only `kind` next to `supported_components` (a legacy `""` set is `both`). `PATCH /calendars/:id` accepts `kind` only to **widen** to `both` (ctag and sync token advance); the calendar's own kind is a no-op 200; any kind that drops a component is 409 `Cannot narrow a calendar's component set`, whether or not objects of that type exist. Widening is API-only in v1 (the web UI does not offer it). `event_count` counts events only (a task list reports 0); there is no task count — per-list open counts come from `/tasks/summary`. `DELETE` refuses the user's last owned calendar that can hold events (`VEVENT`, mixed or legacy `""`) with 400 `cannot delete your last calendar`; a task list is always deletable |
| Events | CRUD, move between calendars. An event is reachable only under the calendar that holds it: a mismatched or unknown `:calendar_id` is the same 404 as an unknown event (F09). Event routes serve events only (#170): a task's UUID gets the unknown-event answer on every verb (404 `Event not found`, before any 403/412/400 check; the MCP event tools answer `No event with id <id> is readable by you.`). Creating an event in, updating one in, or moving one into a calendar that does not store events (a task list) is 400 `This calendar does not store events`; DELETE still works |
| Tasks | `GET`/`POST /calendars/:calendar_id/tasks`, `GET`/`PATCH`/`DELETE /calendars/:calendar_id/tasks/:task_id`, `POST …/tasks/:task_id/complete`, `POST …/tasks/:task_id/uncomplete` — the VTODOs of one task list (#170). Raw JSON `TaskResponse`; access, 404 and 403/412 rules as for events. Details under "Tasks (#170)" below |
| Tasks (cross-list) | `GET /tasks` — one page of the tasks of every task list the caller can read, one query; `GET /tasks/summary?as_of=` — open/overdue/due-today/completed counts, overall and per list (#170). Raw JSON. Details under "Tasks (#170)" below |
| Alarms | `GET /alarms/upcoming?until=` — the VALARM alarms with a trigger in `[now − 24 h, until]` (`until` RFC 3339 with offset, default now + 24 h, clamped to now + 7 d; `until` before `from` is an empty list, a bad value 400 `Invalid 'until' value`) (#169). Raw JSON `{alarms, count, from, until, server_time, truncated}`, `Cache-Control: no-store`, at most 500 entries sorted by `trigger_at` then `key`. Scope: the calendars the caller can read, owned and shared, never a subscribed one, resolved on every request (`ListReadableCalendarIDs`); floating and all-day triggers resolve in the calendar OWNER's time zone (`timezone` preference, UTC when unset), so every reader gets the same instants and keys. Routes exist only while `alarms.enabled` (default true); off, `/api/v1/alarms/*` is a 404, never the SPA |
| Address Books | CRUD, export |
| Contacts | CRUD, search (over every readable address book, shared included — #162), move, photo. The contact must live in the URL's `:addressbook_id` (move included), else 404 |
| Search | `GET /api/v1/search` — unified search over events (no date bound), contacts, calendars and address books (#156). The events group holds VEVENTs only (tasks are not searched); calendar hits carry `kind` |
| Sharing | Calendar and address book share CRUD |
| Credentials | App passwords, CalDAV/CardDAV credentials. Creation validates (F31): the name is trimmed and required (max 100 characters, as for MCP tokens); app-password `scopes` must be a non-empty subset of `caldav`/`carddav` and default to both when omitted; a DAV credential's `expires_at` must lie in the future, its password must be 8 to 72 bytes, and its username must be 3–50 lowercase letters `a`–`z` — upper case is refused, never folded (the SPA generates 16). Each violation is a 400; a username in use is a 409. Only creation is checked: rows stored before keep working unchanged |
| Import/Export | Calendar import (.ics), contact import (.vcf), full backup export. A calendar import item whose component the target does not store (an event into a task list, a task into an event calendar, #170) is counted as `skipped` and listed in `errors` with the reason (`This calendar does not store events`/`tasks`) |
| About | Open-source attribution list (`GET /api/v1/about/open-source`, authenticated) |
| MCP tokens | `GET`/`POST /api/v1/mcp-tokens`, `DELETE /api/v1/mcp-tokens/:id` — the bearer credentials MCP clients use (#104) |
| Subscriptions | `GET`/`POST /api/v1/calendar-subscriptions`, `GET`/`PATCH`/`DELETE /:id`, `POST /:id/refresh` — remote iCalendar feeds mirrored into read-only calendars (story 100). Raw JSON |
| Health | `GET /health` |

**Raw JSON (no envelope)**: calendars (incl. shares, public access), events, tasks (`/calendars/:calendar_id/tasks`, `/tasks`, `/tasks/summary`), address books (incl. shares), contacts, `/contacts/search`, `/search`, CalDAV/CardDAV credentials, import, `/mcp-tokens`, `/calendar-subscriptions`, `GET /auth/oauth/providers` and `GET /ready`. Errors are mostly `{ "status": "error", "message" }` (`ErrorResponse()`), though some raw-JSON handlers answer `{ "error": ... }`. `POST /auth/register` answers 200, not 201; that is kept for client compatibility.

#### Tasks (#170)

A **task** is a VTODO calendar object; a **task list** is a calendar whose `supported_components` is `VTODO` (`kind: "tasks"`). There is no second collection model: a task list lives at `/dav/{user}/calendars/{path}/` like any calendar and shares its sharing, sync token, ctag, change log, export and public link. A mixed calendar (`VEVENT,VTODO`, `kind: "both"`) holds both. Handler `adapter/http/task_handler.go`, wire shapes `adapter/http/dto/task_dto.go`, use cases `usecase/task/`.

`TaskResponse`: `id` (the object UUID), `calendar_id` (numeric, parity with events) and `calendar_uuid`, `uid`, `summary`, `description` (plain text, see "iCalendar text" below), `status` lower-case (`needs-action`, `in-process`, `completed`, `cancelled`), `completed`, `completed_at`, `percent_complete`, `priority` (the RFC 5545 integer 1–9, 1 highest, or null for none/0; the web UI maps 1–4 high, 5 medium, 6–9 low), `due` (RFC 3339 in UTC, `Z`) or, for an all-day task (`due_all_day`), `due_date` `YYYY-MM-DD` with `due` null (`start`/`start_date` likewise, by DTSTART's own value type), `is_recurring` and a read-only `rrule`, `etag`, `created_at`, `updated_at`. Every nullable key is always present.

`TaskResponse` and `EventResponse` also carry `reminders` (#169, `dto/reminder_dto.go`, `ReminderResponse`/`NewReminderResponses`): always an array, one entry per VALARM (`index`, lower-case `action`, `related`, signed `offset_seconds`, absolute `at` in UTC, `editable`; rules in the root `CLAUDE.md`). Events fill it from `EventInstance.Alarms` (the override's VALARMs when the occurrence has an override, else the master's; set in `ToEventInstance` from the already decoded components) or, for a stored object, from `CalendarObject.EventAlarms()` (one decode); tasks from `TaskDetails.Alarms`. No extra query. The MCP task tools render `dto.TaskResponse`, so their output gained `reminders` too; the MCP event tools use their own views and are unchanged.

Access is the event routes' (`adapter/http/calendar_access.go`): the list and create routes answer an unknown or unreadable calendar with 404 `Calendar not found`; on a task route every miss — unknown or unreadable calendar, unknown task, a task filed under another calendar, an event's UUID — is the same 404 `Task not found`. A read-only share or a subscribed calendar reads, and every write is 403 `You have read-only access to this calendar`. An optional `If-Match` on PATCH, DELETE, complete and uncomplete that does not match is 412 `ETag precondition failed`. The header takes the ETag **in quotes** (`If-Match: "1793779818037591359-099993a1529372e5"`, or `*`), as the DAV `getetag` shows it; the `etag` field of `TaskResponse` is the bare token, so a bare value is a 412 (events behave the same). For PATCH, complete and uncomplete the check is atomic with the write: the use case re-checks the ETag on the task it loaded and the UPDATE only matches that ETag (`UpdateCalendarObjectIfETag`), so of two writers holding the same ETag one gets 200 and the other 412. Without an If-Match a write that meets a concurrent change is redone on the fresh task (at most 3 attempts). A write that races a DELETE is 404 `Task not found` and never brings the row back. Write routes on an existing task check 404 → 403 → 412 before the body. A server failure is a generic 500 with the cause in the log.

- `GET /calendars/:calendar_id/tasks` → `{ "tasks": [TaskResponse], "count", "total" }`. `status` = `open` (default) | `completed` | `all`, compared exactly, anything else 400. **"Done" is COMPLETED or CANCELLED everywhere** (filters and counts). `q` = case-insensitive literal substring of summary or description. `limit` defaults to 500 and is capped at 1000; a `limit`/`offset` that is not a non-negative integer is 400. Order: due date (undated last), priority (unset last), creation, id. A recurring task is one row (its master, never expanded). A calendar that does not store VTODO answers 200 with no tasks.
- `POST /calendars/:calendar_id/tasks` → 201 `TaskResponse`. Body: `summary` (required, trimmed, at most 500 characters), `description` (summary and description must not contain C0 control characters other than TAB — NUL, BEL, ESC … — else 400 `summary must not contain control characters`/`description must not contain control characters`; CR and LF are line breaks, stored as `\n`), `due`, `start`, `due_all_day`, `priority` 0–9 (stored as given), `percent_complete` 0–100, `status` (default `needs-action`; `completed` also writes `COMPLETED` = now and `PERCENT-COMPLETE:100`), `timezone`. `due_all_day` decides the format of `due` and `start` alike — `YYYY-MM-DD`, else RFC 3339 with an offset — and `due` must not be before `start`. A timed value is written `TZID=<timezone>` with its VTIMEZONE, or in UTC (`Z`) without a usable zone. There is no recurrence field (unknown fields are ignored). Refusals in order: 404, 403, 400 `Invalid request body`, 400 `This calendar does not store tasks`, 400 per field. The server writes a full VCALENDAR (`UID` `<uuid>@calcard.io`, `DTSTAMP`/`CREATED`/`LAST-MODIFIED` in UTC, `SEQUENCE:0`) at `<uuid>.ics`.
- `GET /calendars/:calendar_id/tasks/:task_id` → 200 `TaskResponse`.
- `PATCH /calendars/:calendar_id/tasks/:task_id` → 200 `TaskResponse`. Only the fields sent change: an explicit `null` (or `""`, for every one of these fields) removes `description`, `due`, `start`, `priority` or `percent_complete`; an absent key keeps it; `summary`, `status` and `due_all_day` cannot be removed (null = absent). A request that changes nothing (no field, or every value as stored) writes nothing and keeps the ETag. The compare is of what the API shows: `priority` null or 0 on a task whose PRIORITY is 0 or absent changes nothing. A summary made only of white space or zero-width/format characters (U+200B, U+200C, U+200D, U+FEFF) is 400 `summary is required`. `due_all_day` (absent: the task's current one) sets the format of the due/start sent; once a request touches due, start or `due_all_day`, a stored DUE/DTSTART of the other value type, a due before the start, or removing a DTSTART behind a DURATION is 400. Turning the DTSTART of a repeating task that has `RECURRENCE-ID` overrides (written by a DAV client) from a date into a date-time or back is 400 `start cannot change between a date and a date-time while the repeating task has changed occurrences` (RFC 5545 §3.8.4.4 gives a RECURRENCE-ID the value type of the master's DTSTART, so the overrides would be orphaned). A timed due/start sent without `timezone` is written in UTC, even where the stored one had a TZID; a TZID time adds its VTIMEZONE when the object lacks one (add-only: a client's own VTIMEZONEs are never dropped or rewritten). `status: "completed"` acts like complete, another status on a completed task like uncomplete followed by that status. On a recurring task, `status` `completed`/`cancelled` or `percent_complete: 100` is the 409 of complete, also when the series is already completed (checked after every 400); other edits apply to the master.
- `DELETE /calendars/:calendar_id/tasks/:task_id` → 204. A **hard delete** (unlike an event DELETE, which soft-deletes): `CalendarRepository.HardDeleteCalendarObject` removes the object row for good — no `deleted_at` tombstone — and writes the `deleted` change-log row with the calendar's new sync token/ctag in the same transaction; that row (path + UID) is all a sync-collection REPORT needs to answer the href with a 404, so DAV clients drop the task on their next sync. Nothing can be restored. A second DELETE, or one that loses a race, is 404 `Task not found` and records nothing. Allowed in a calendar that no longer stores VTODO. A DAV client's DELETE of a task object is the same hard delete (`CalDAVBackend.DeleteCalendarObject` branches on `CalendarObject.IsTask`; a row already gone is a 404 with nothing recorded); an event's DELETE, over REST, MCP or DAV, still soft-deletes.
- `POST /calendars/:calendar_id/tasks/:task_id/complete` → 200 `TaskResponse`: `STATUS:COMPLETED`, `COMPLETED` (now, UTC), `PERCENT-COMPLETE:100`. Idempotent: an already completed task answers unchanged, same ETag, nothing written. No body is read. A recurring task (RRULE or RDATE on the master) is **409** `Repeating tasks cannot be completed here yet. Complete it in your task app.`: clients disagree on how to complete one, so that is deferred.
- `POST /calendars/:calendar_id/tasks/:task_id/uncomplete` → 200 `TaskResponse`: removes `COMPLETED`, writes `STATUS:NEEDS-ACTION` and resets `PERCENT-COMPLETE` to 0 **only when it was 100** — a task at 40% before it was completed is not restored to 40%. Idempotent: a task that is not completed (in-process and cancelled included) answers unchanged. Allowed on a recurring task. No body is read.
- `GET /tasks` → `{ "tasks": [TaskResponse + "calendar_name", "calendar_color"], "count", "total" }` over every task list the caller can read (own, subscribed and shared; calendars without VTODO skipped), in ONE task query whatever the number of lists, in the per-list order. Same `status`, `q`, `limit`, `offset`, plus `calendar_uuid` (narrows to one calendar: one the caller cannot read, or an unknown one, is 404 `Calendar not found`, never an empty 200; a readable calendar without VTODO answers an empty page) and `due_after`/`due_before` (RFC 3339 with an offset, a `+` sent as `%2B`, else 400 `Invalid 'due_after' value`/`…'due_before'…`): the half-open range `[due_after, due_before)` compared as instants; an undated task never matches a bound; an inverted range is an empty page, not a 400.
- `GET /tasks/summary?as_of=…` → `{ "open", "overdue", "due_today", "completed", "by_calendar": { "<calendar uuid>": { "open", "overdue", "completed" } } }` over the same task lists, counted in ONE database query, so the numbers hold beyond a list page. `as_of` is required (400 `Query parameter 'as_of' is required`) and must be RFC 3339 with the user's UTC offset (else 400 `Invalid 'as_of' value`; send `+` as `%2B`); its offset alone decides which date is today, never the server's zone. `open` = needs-action + in-process; `completed` = completed + cancelled, equal to the total of `?status=completed`; a cancelled task never counts as open, overdue or due today. A timed task is overdue once due before `as_of`; an all-day task only once its date is before `as_of`'s date. `due_today` = due on `as_of`'s date, so a timed task due earlier today is overdue AND due today. `by_calendar` has an entry for every readable task list, zeros included; each entry's `completed` is that list's done count (completed + cancelled), so the SPA can tell "this list has nothing done" from "nothing in view".

`/tasks` and `/tasks/summary` hang off `/api/v1` with their own per-route `Authenticate`, like `/search`.

**Edits never regenerate a VTODO.** PATCH, complete and uncomplete change only the touched properties of the master (the VTODO without `RECURRENCE-ID`), bump `SEQUENCE` and set `LAST-MODIFIED`/`DTSTAMP` (UTC), so `VALARM`, `RELATED-TO` (subtasks, shown flat in v1), `RRULE`/`RDATE`/`EXDATE`, overrides, `X-` and unknown properties and the client's VTIMEZONEs survive a web edit. Every write advances the calendar's sync token and writes a change-log row, so DAV clients see REST changes and REST sees DAV-written tasks.

**Time zones (R3, a known approximation):** a floating DUE (no `Z`, no TZID) and a date-only DUE are stored and compared as UTC wall time — in the task columns, in `due_after`/`due_before`, in the summary counts and in the CalDAV VTODO time-range query — while overdue and due today are judged in `as_of`'s offset. That offset is fixed: "today" is the 24 hours of `as_of`'s date in that offset, not the user's zone, so on a daylight-saving change day (23 or 25 local hours) the window is an hour off at one edge — on Berlin's fall-back day a task due 00:30 local counts as overdue, not due today, for `as_of` at +01:00 — while the SPA's row marker uses the local calendar date. Accepted for v1 (spec §3.3); an IANA zone parameter would fix it. A TZID the server cannot resolve (a non-IANA name defined only by the object's own VTIMEZONE) leaves the task's due date empty in the columns, the limit event start times already have.

`/search` has no task group: it sees a task list only as a calendar with `kind: "tasks"`. The MCP task tools (`list_tasks`, `create_task`, `update_task`, `complete_task`, `uncomplete_task`, `delete_task`, `get_task_summary`, see "MCP" below) call these same use cases and answer in these same shapes.

### CalDAV/CardDAV (`/dav/...`)

WebDAV protocol endpoints for native calendar/contact client sync (Apple Calendar, Thunderbird, DAVx5, etc.). Uses HTTP Basic Auth with app passwords or DAV credentials. The Basic-auth username — the account's (or its e-mail), a CalDAV or a CardDAV credential's — is accepted in any case. New usernames are stored in lower case (registration generates 16 lowercase letters, setup lower-cases the admin's, a credential's must be lowercase). The CalDAV credential lookup lower-cases the input in Go and matches it exactly; account and CardDAV credential lookups try the exact spelling, then an unambiguous `LOWER()` match in SQL (F02). The `/dav/{username}/` URL segment is compared exactly.

Basic auth accepts the account's primary password, so with `rate_limit.enabled` **failed** Basic attempts are limited per identifier — the user name or e-mail sent, trimmed and lower-cased — (`auth_email_requests`) within `rate_limit.window` (#193, `http.FailedAuthLimiter`, wired with `webdav.Handler.WithBasicAuthLimiter`). There is deliberately no per-IP bucket: behind a reverse proxy `c.IP()` is the proxy for every client, so one would let an outsider throttle every DAV client of the server. Only settled failures count: a check counts once it has failed (a 401), never while it is in flight, and a valid credential never counts, so concurrent correct logins cannot use the allowance up; a request without credentials never counts either. Checks in flight are capped separately at 2×`auth_email_requests` per identifier (over the cap: 429, `Retry-After: 1`), which bounds the bcrypt work of a flood; the price is that a burst of parallel guesses can overshoot the allowance by what is in flight (at most 3×max−1 per window). Once an identifier is spent, every Basic attempt for it — a correct one included — is the auth limiters' 429 with `Retry-After` until the window ends, checked before any lookup or bcrypt. Bearer tokens are not limited.

**Component sets (#170).** A calendar collection advertises in `supported-calendar-component-set` exactly what its `supported_components` stores — `VEVENT` (a calendar), `VTODO` (a task list) or both (`VEVENT,VTODO`, or a legacy `""`) — through `calendar.SupportsComponent`, the one rule DAV PUT, the REST event and task writes and the calendar import share. A PUT of a component the collection does not store is 403 with the `CALDAV:supported-calendar-component` precondition. That check runs before the object-validity check, so a VEVENT+VTODO body PUT into an event-only collection is that 403, while a mixed collection answers it with the 409 `valid-calendar-object-resource`. Feed sync is exempt. What new collections store: register and first-admin setup create "Personal" (`VEVENT`) and "Tasks" (`VTODO`); an account provisioned by OAuth sign-up gets no calendar; `POST /api/v1/calendars` without `kind` stores `VEVENT`; a MKCALENDAR or extended MKCOL without a component set (RFC 4791 §5.2.3: absent means all) and every subscription store `VEVENT,VTODO`.

**The one-shot narrowing on upgrade.** The first boot of #170 runs the data migration `narrowCalendarComponentSets` (see `internal/infrastructure/CLAUDE.md`): every live, non-subscribed calendar created before the `tasks.kind_introduced_at` cutoff whose set is `VEVENT,VTODO` becomes `VEVENT` when it holds only events and a task list (`VTODO`) when it holds only tasks — a user's first "Personal" calendar included. Empty, mixed, `""` and subscribed calendars stay as they are. When it narrows anything it logs `data migration: narrowed the component set of legacy calendars to what they hold (#170)` with the counts (`event_calendars`, `task_lists`). Consequences:
- A calendar that held only events now **refuses VTODO PUTs with the `supported-calendar-component` precondition (403)**. That is intended: it is an event calendar now.
- The narrowing bumps the ctag and sync token, but **a ctag bump does not make a client re-read `supported-calendar-component-set`**. Clients pick up the new set only on a home-set PROPFIND or a collection refresh (DAVx5: "Refresh collection list"; Thunderbird 156: a restart, since it reads the set only on its first refresh after start-up — "Synchronize" or disabling and re-enabling the calendar is not enough, verified in the #170 interop pass).
- Wanting tasks in a narrowed calendar again means widening it through the API — `PATCH /api/v1/calendars/:id` with `{"kind":"both"}`; the web UI does not offer it in v1 — or re-creating it over DAV.
- It runs once: the `system_settings` marker `migration.narrow_component_sets.v1` stops every later boot, so a calendar widened afterwards is never narrowed again. Deleting that row makes the next boot run it again against the stored cutoff.

**VTODO time-range `calendar-query`** (a client asking the server for a due-date window; Thunderbird 156 never sends one, it syncs every object and filters its own cache — #170 interop pass) is matched by `adapter/webdav/caldav_vtodo_filter.go`, not go-webdav: a non-recurring task follows the RFC 4791 §9.9 VTODO table; a repeating one matches when [DTSTART, else DUE, through the end of its series] meets the window and is never expanded. DATE and floating values are compared as UTC (R3). Every other filter stays go-webdav's.

### MCP (`/mcp`)

Model Context Protocol server (story 104) — JSON-RPC 2.0 over Streamable HTTP, so an AI
assistant can read and manage the caller's calendars, tasks and contacts. `POST /mcp` carries the
JSON-RPC messages, `GET /mcp` returns a manifest (or opens the SSE notification stream when
`Accept: text/event-stream`), `DELETE /mcp` terminates a session.

Authentication is a bearer **MCP access token** (`calcard_mcp_…`, minted per user at
`/api/v1/mcp-tokens`); a JWT access token is also accepted but expires in minutes. 26 tools
(event and contact CRUD, creating/renaming/deleting calendars and address books, unified search,
`find_free_slots`, and the seven task tools) and four resources are advertised. Calendar output
carries the read-only `kind` (#170); `create_calendar` takes an optional `kind` (`events`, the REST
default, `tasks` or `both`) and `update_calendar` a widen-only one (`both`; narrowing answers
`Cannot narrow a calendar's component set`), exactly as `POST`/`PATCH /calendars`.

The task tools (#170, `adapter/mcp/tools_task.go`) are `list_tasks` (REST `GET /tasks`: across every
readable task list or, with `calendar_id`, one; `status`, `query`, `due_after`/`due_before`,
`limit` — default **50** here — and `offset`), `create_task`, `update_task` (null removes the five
removable fields, as the PATCH body), `complete_task`, `uncomplete_task`, `delete_task` (the hard
delete) and `get_task_summary` (`as_of` required, RFC 3339 with the user's offset). Each calls the
use case the REST route calls and renders with the REST DTOs, so a task reads byte-for-byte as REST
answers it; argument names are the REST ones (`query` = `q`). Access mirrors the task routes: a task
id the caller cannot read — unknown, an event's UUID, another user's — is
`No task with id <id> is readable by you.`, a read share or a subscribed calendar
`You have read-only access to the task list holding that task.`; the use cases' refusals keep their
REST text (the recurring 409 sentence, `This calendar does not store tasks`, the field 400s). The
event tools never see a task: `get_events`, `search_events`, `find_free_slots` and the
`calendars://{id}/events` resource read events only, `find_free_slots` leaves task lists out of its
default calendars, `resources/list` advertises no events resource for a task list, and
`update_event` / `delete_event` answer a task's id like an unknown event's.

**It is a facade, not a second API.** Every tool delegates to the same use case the REST
handler calls and resolves permissions through the same `GetUserPermission` methods, so MCP can
never reach data REST would refuse. Preserve that when adding tools: no MCP-specific query, no
MCP-specific permission logic. Managing a collection itself (`create_`/`update_`/`delete_calendar`,
`…_address_book`) is owner-only exactly as over REST — an unknown, foreign or merely shared
collection gets one identical "not owned" answer. The calendar use cases check ownership
themselves; the address-book tools go through `resolveOwnedAddressBook`, the mirror of the REST
handler's check. Never derive ownership from the capped calendar permission: it would lock an owner
out of renaming or deleting their own subscription. Disable the whole surface with
`CALDAV_MCP_ENABLED=false`.

Like REST, a tool never hands the model a storage error: anything the caller cannot fix is one
generic sentence with the cause in the server log (`toolInternalError`), and only validation,
permission and not-found answers carry their own text. Tool arguments are strict — an argument the
schema does not declare (a typo like `calender_id`, or a case variant) is refused as a tool error
that lists the accepted ones, instead of being dropped while the tool runs with its default.

## Context Files

Each `internal/` subdirectory has its own AGENT.md with detailed file listings:

- `internal/AGENT.md` — Architecture overview and request flow
- `internal/domain/AGENT.md` — Domain models and interfaces
- `internal/usecase/AGENT.md` — Business logic (auth, calendar, event, task, contact, sharing, import/export)
- `internal/usecase/auth/AGENT.md` — Authentication use cases (local, OAuth)
- `internal/adapter/AGENT.md` — HTTP handlers, repositories, auth implementations, WebDAV
- `internal/infrastructure/AGENT.md` — Database, server, email, logging
- `integration/CLAUDE.md` — End-to-end integration test suite: how it boots the real server in-process, what each test file covers, and how to add new tests

## Development

```bash
go build ./...                                # Build
go test ./...                                 # Unit and handler tests (fast, default)
go test -tags=integration ./integration/...   # End-to-end suite — boots the real server on a random port
go run ./cmd/server                           # Run dev server
./scripts/build.sh                            # Build binary to bin/server
go run ./tools/genlicenses                    # Regenerate the open-source attribution manifest (#101)
```

**There is no generated API reference.** Swagger/OpenAPI was removed along with story 030 —
no `swag`, no `docs/` package, no `/api/docs` route. Each handler carries a
`// METHOD /api/v1/...` doc comment instead; keep it in sync when a route changes, and
document endpoint areas in the API Surface table above.

Integration tests are gated behind the `integration` build tag so they're excluded from the default run. They cover the first-boot flow, REST CRUD, the export/re-import roundtrip, and CalDAV/CardDAV protocol endpoints. See `integration/CLAUDE.md` for details.

## Key Conventions

- **Configuration**: Loaded from environment variables (`CALDAV_*`) and/or `config.yaml`. See `configs/config.yaml.example`. Every env-bound field of `config.Config` (and every `CALDAV_*` that `cmd/server` reads directly) must have a row in `/CONFIGURATION.md` naming its YAML key and env var — `internal/config/config_doc_test.go` fails otherwise.
- **Database**: SQLite by default (zero config), PostgreSQL when `CALDAV_DB_HOST` is set.
- **Migrations**: GORM `AutoMigrate` — all models registered in `infrastructure/database/migrations.go`.
- **Auth**: JWT for REST API, HTTP Basic Auth for DAV endpoints. Backend uses `expires_at` (Unix timestamp) for JWT expiry.
- **Access-token revocation** (P02): a JWT is stateless, so a per-user marker `User.TokensValidAfter` kills every access token issued before it. Logout, password change, password reset and account deletion set it to "now" via `UserRepository.InvalidateAccessTokens` (reset and deletion through the same single-column UPDATE inside their transactions; never via `Update`, which omits the column so a stale `User` cannot roll it back). Every place that accepts a JWT — `http.Authenticate`, the MCP transport, the DAV bearer path — must call `User.AcceptsAccessTokenIssuedAt`; a new JWT acceptance path that skips it reopens the hole. Tokens carry a nanosecond `iat_ns` claim because the whole-second `iat` would leave a one-second window in either direction.
- **SMTP**: A new account is active (can log in) at once, SMTP or not. SMTP only decides `EmailVerified`: without SMTP it is true at once; with SMTP it stays false until the mailed link is opened (`verify.go`, which sets nothing else) or a password reset completes (which also deletes every session and credential of the account, `UserRepository.CompletePasswordReset` — but not its outgoing shares, public links or subscriptions). Login and sharing do not wait for it (a share by e-mail resolves an unconfirmed account like any other — owner decision); only the OAuth callback refuses to auto-link a provider identity onto an account whose address is unconfirmed (`ErrLocalEmailNotVerified`). `IsActive=false` only means a deactivated account. A duplicate registration is a 409 in both modes (with SMTP the owner is also mailed a notice — never an activation link, even for an unconfirmed address, because that account holds the first registrant's password; the notice points at the password reset, #193); the earlier P01 trick of answering it like a new address cannot work once the account is usable before confirmation. See CONFIGURATION.md, Registration section. Forgot-password follows the same rule: an unknown address is a silent 200, but a send that really failed (mail server unreachable, storage error) is a 500 with a generic message and the cause in the log, and the SPA shows it as an error instead of the green notice.
- **Base URL**: `cfg.BaseURL` (`CALDAV_BASE_URL`) is optional and has no default. URLs returned to the caller of a request (app-password `server_url`, calendar `public_url`) go through `adapter/http.RequestBaseURL`, which falls back to the request's own origin; anything that leaves the request (e-mail links, OAuth redirect URIs) uses `cfg.BaseURL` only, and `Validate` refuses to start with SMTP or OAuth configured but no base URL.
- **Open-source attribution**: `internal/usecase/about` embeds `open_source_go.json`, generated by `go run ./tools/genlicenses` and committed. Regenerate it after any `go.mod` change; the Dockerfile re-runs it but tolerates failure because the committed file is authoritative.
- **Time zones**: `cmd/server` imports `time/tzdata`, so zone names resolve even without a system zoneinfo (the image installs `tzdata`, which takes precedence). Event `timezone`s, calendar time-zone validation and the VTIMEZONEs generated for stored events (`domain/calendar/vtimezone.go`) depend on it.
- **WebDAV methods**: Custom HTTP methods (PROPFIND, REPORT, MKCALENDAR, etc.) registered in `infrastructure/server/server.go`.
- **MCP**: `/mcp` is a facade over the existing use cases (story 104). Tools must never query or authorize on their own — reuse the use case and the `GetUserPermission` helpers in `adapter/mcp/access.go`, or the MCP surface silently becomes a way around the sharing model.
- **Subscribed calendars** (story 100): `Calendar.Subscribed` marks a read-only mirror of a remote iCalendar feed. Read-only is enforced by `calendar.EffectivePermission`, which caps the resolved permission at read — REST, CalDAV and MCP all funnel through it, so a new write path inherits the rule for free. **Never add a write that bypasses it**: the next refresh replaces the collection wholesale, so anything written there disappears silently. Collection-level ownership (rename, share, delete) is deliberately unaffected. The only permitted writer is `CalendarRepository.ReplaceFeedObjects`, used by the sync path.
- **Tasks and component sets** (#170): a write path that stores a calendar object must go through the one component-set rule, `calendar.SupportsComponent` / `calendar.RequireComponent` (REST and import: "This calendar does not store events|tasks"; DAV: the 403 via `calendarComponentSet`), never a comparison of its own; only the feed sync of a subscription (always `VEVENT,VTODO`) is exempt. A REST edit of a stored VTODO goes through `usecase/task`'s `editTask`, which edits the master in place; never re-encode a task from the API's fields, or a client's alarms, subtask links and repeat rules are lost. `supported_components` changes after create only through `CalendarRepository.UpdateSupportedComponents` (the widen-only PATCH, the narrowing migration).
- **Outbound HTTP**: subscriptions are the only feature that fetches a URL a user supplies. Anything similar must go through `usecase/subscription.Fetcher` or reproduce its dial-time address guard — a pre-flight DNS check is not equivalent (rebinding), and a failure message must never repeat the URL.
- **Alarm bounds** (#169): every `calendar_objects` row carries `alarm_first_at`/`alarm_last_at` (UTC, NULL without a deliverable alarm), derived by `PopulateDenormFieldsFromICal` like `recurrence_end_time`, so a writer of `ICalData` that skips it leaves them stale. The partial index `idx_calobj_cal_alarm (calendar_id, alarm_last_at) WHERE alarm_last_at IS NOT NULL` is for the upcoming-alarms query. Rows stored before are filled once at boot by `backfillAlarmBounds` (marker `migration.backfill_alarm_bounds.v1`); after a downgrade to a binary without the columns and an upgrade again, delete that marker row to re-run it. Details: `internal/domain/CLAUDE.md`, `internal/infrastructure/CLAUDE.md`.
- **iCalendar text is plain text on the API**: SUMMARY, DESCRIPTION and LOCATION are RFC 5545 TEXT (`\,` `\;` `\\` `\n`) in the stored iCalendar but plain text in every REST/MCP answer, in the denormalized `summary`/`description`/`location` columns and in search. Read them with `calendar.UnescapeText`/`TextPropValue` (never go-ical's `Prop.Text`, which splits at an unescaped comma) and write them with `calendar.NewTextProp` (escapes once, turns CR/CRLF into `\n`; go-ical's `SetText` copies a CR and its encoder then refuses the object). So a text the API answered can be sent back unchanged: tasks and events compare and write it without gaining a backslash per save. The boot step `unescapeTextColumns` re-derives rows stored before. The UID is an identifier and stays raw.
- **Server-written timestamps are UTC**: go-ical's `SetDateTime` writes the `…Z` form only for `time.UTC`; a `time.Now()` (zone `time.Local`) becomes `DTSTAMP;TZID=Local:…`, which RFC 5545 §3.8.7.2 forbids. Always pass `time.Now().UTC()`, and the same for a vCard `REV` formatted with a literal `Z` (F04).
- **Deleting a collection is permanent**: a calendar or address book deleted over REST, MCP, CalDAV/CardDAV or the subscription endpoints is hard-deleted with everything in it — events/contacts, photos, change log, shares, subscription, public link — in one transaction, by the collection cascade in `adapter/repository/collection_purge.go` (account deletion runs the same cascade for each of the account's collections). Callers just call the repository `Delete`; a new table that hangs off a collection must be added to that cascade, or deleted collections leak its rows. The boot data migration `purgeDeletedCollections` removes what older versions left behind (they soft-deleted only the collection row).
- **Dependency injection**: All wiring happens in `infrastructure/server/routes.go`.
