# Usecase Layer

This directory contains the application-specific business rules. It orchestrates the flow of data to and from the domain entities, and directs those domain entities to use their critical business rules to achieve the goals of the use case.

## Responsibility

The usecase layer is responsible for:

- Implementing the user stories (e.g., "User Login", "Account Registration").
- Coordinating interactions between domain models and data access (repositories).
- Validation of business constraints that cross multiple entities.
- Ensuring the data is in the correct format for the domain layer.

## Subdirectories

### [auth/](auth/)

Authentication and authorization logic. See [auth/AGENT.md](auth/AGENT.md) for details.

- `login.go`, `register.go`, `verify.go`, `refresh.go`, `logout.go` — Standard email/password auth flows.
- `change_password.go`, `forgot_password.go`, `reset_password.go` — Password management.
- `oauth_initiate.go`, `oauth_callback.go`, `oauth_link.go`, `oauth_providers.go` — OAuth2/OIDC flows.
- `email_service.go` — Email service interface for auth-related emails.
- `setup.go` — First-run setup: creates the initial administrator (`IsAdmin`), only while no account has ever existed.
- Default collections (#170): registration and first-admin setup both go through `RegisterUseCase.createDefaultCalendars`, which creates "Personal" (`VEVENT`, an event calendar) and "Tasks" (`VTODO`, a task list), each independently (a failure is logged and never fails the account), plus the "Contacts" address book. An account provisioned by OAuth sign-up gets no calendar.
- `username_util.go` — Username generation for registration and OAuth sign-up: 16 random lowercase letters (older accounts keep their mixed-case names).

### [calendar/](calendar/)

Calendar management:

- `create.go`, `get.go`, `list.go`, `update.go`, `delete.go` — CRUD operations. `delete.go` (REST and MCP `delete_calendar`): owner check, `DELETE` confirmation, last-calendar guard (per kind, #170: refuses the user's last OWNED calendar that supports VEVENT — mixed and legacy `""` sets count, calendars shared with the user do not — with the unchanged 400 "cannot delete your last calendar"; a VTODO-only task list is never protected, the last one included), then one `CalendarRepository.Delete`, which permanently removes the calendar with its events, change log, shares, subscription and public link in a single transaction — no second clean-up step. Its `shareRepo` constructor parameter is unused and only kept so existing wiring compiles.
- `create.go` takes an optional `kind` (#170): `events` (the default, stores `VEVENT`), `tasks` (`VTODO`, a task list) or `both` (`VEVENT,VTODO`); anything else is `ErrInvalidInput` (400). `list.go`'s `CalendarWithEventCount` (REST list, `/search`, MCP) and `CalendarWithKind` (REST create/get/update) carry the derived, read-only `kind` (`calendar.KindOf`); there is deliberately no task count next to the event count (it would be a second COUNT per calendar).
- `update.go` (REST PATCH, MCP `update_calendar`) takes `kind` widen-only (#170): `both` on an event calendar or task list is written through `CalendarRepository.UpdateSupportedComponents` (own transaction, advances ctag/sync token); the calendar's current kind is a no-op that writes nothing; any kind that drops a component is `calendar.ErrConflict` "Cannot narrow a calendar's component set" (REST 409), whether or not objects of that component exist; an unknown kind is the create 400. Every field and the kind are checked before anything is written. MCP `update_calendar` passes its `kind` argument straight through (the same rule, the conflict as a tool error).
- `owned.go` — `LoadOwnedCalendar` / `LoadOwnedCalendarByID`: the one owner lookup (missing or not owned → `calendar.ErrNotFound`, a storage error stays an internal error). Reused by the calendar import and the calendar share use cases.
- `enable_public.go`, `get_public_status.go`, `regenerate_token.go` — Public calendar sharing.
- `export.go` — iCalendar export.

### [event/](event/)

Event management:

- `create.go`, `get.go`, `list.go`, `update.go`, `delete.go` — CRUD operations. An optional IANA `timezone` (REST and MCP alike) stores DTSTART/DTEND with `TZID=<zone>`; create and update then call `calendar.SyncVTimezones`, so the object carries a VTIMEZONE for every TZID it uses and none it no longer uses (F20, RFC 5545 §3.2.19). Objects written before that heal on their next REST/MCP edit. The zone name `Local` is ignored like an unknown one (it would mean the server host's zone).
- `move.go` — Move event between calendars.
- Series splits keep reminders (#169): `update.go`'s `copyRelativeAlarms` deep-copies the master's RELATIVE VALARMs (classified with `calendar.ParseAlarms`, document order, every property/parameter, no shared maps) into the NEW override of `scope=this` and the NEW master of `scope=this_and_future`, because an override uses only its own VALARMs. Absolute triggers stay on the old master (they fire once for the series); the old master keeps all its VALARMs; an existing override edited in place keeps its own. Copied regardless of ACTION (classification is by trigger). The same use case serves REST and MCP `update_event`.
- Text: create and update write SUMMARY/DESCRIPTION/LOCATION with `calendar.NewTextProp` (escaped once; the API's text is plain, see `domain/calendar/text.go`), so a PATCH of the text a GET answered stores the same bytes again.
- `component_set.go` — `requireEventCalendar`: create, update and move refuse a calendar that does not store VEVENT (`calendar.RequireComponent`, #170) with `ErrInvalidInput` "This calendar does not store events" (REST 400, MCP tool error). `loadEvent`: get, update, delete and move answer a non-event object (a task, `CalendarObject.IsEvent`) with `ErrNotFound`, checked before the calendar's component set, so a task UUID is an unknown event (REST 404 "Event not found", MCP "No event with id … is readable by you.").
- Input validation lives here, not in the DTOs (the server has no struct validator). `create.go`
  rejects a missing (zero) start or end and an end before the start with `ErrInvalidInput` (a 400 over
  REST, an error result over MCP); without that a create with no times was stored at 0001-01-01 (F13).
- `datetime.go` — `icalDateTime`: a timed start/end written without a `timezone` (an RFC 3339
  offset such as `+05:00`, or a `time.Local` value) is stored in UTC (`DTSTART:…Z`), never with an
  empty `TZID=` or `TZID=Local` (F22). A `timezone` (IANA) or the event's stored TZID is kept;
  all-day values keep their wall-clock date.

### [task/](task/)

Tasks (VTODO, #170), mirroring `event/`. A task is a VTODO calendar object, a task list a calendar whose component set stores VTODO (`calendar.SupportsComponent`); every route and its rules are in `server/CLAUDE.md`, "Tasks (#170)". **The in-place edit rule:** only create builds a VTODO; update, complete and uncomplete never regenerate one — they go through `editTask` (`ical.go`), which changes just the touched properties of the master, so `VALARM`, `RELATED-TO`, `RRULE`/`RDATE`/`EXDATE`, `RECURRENCE-ID` overrides, `X-` and unknown properties and a client's VTIMEZONEs survive a web edit. Every server-written timestamp comes from the injected clock (`New…UseCase(repo, now)`, nil = `time.Now`) in UTC. Recurring tasks are never expanded and cannot be completed here (`ErrRecurringNotSupported`). The REST task routes and the MCP task tools (`adapter/mcp/tools_task.go`) share one instance of each write, cross-list and summary use case (built once in `routes.go`), so both surfaces refuse exactly the same.

- `get.go`, `list.go` — read one task / one page of a task list. `task.go`: `Task` is the object plus `calendar.TaskDetails` (DTSTART's value type and the recurrence, decoded from the iCalendar; status, due, completion, percent and priority come from the denormalized columns); `writeTask`/`checkIfMatch`/`storeTask` make every edit conditional (`UpdateCalendarObjectIfETag`: a task deleted meanwhile is `ErrNotFound`, one changed meanwhile is retried up to 3 times, or `ErrPreconditionFailed` when the caller's `IfMatch` ETag no longer holds); `loadTask` maps the repository's record-not-found to `ErrNotFound` and answers an event's UUID or a task filed under another calendar with `ErrNotFound`. `list.go`: `status` is `open` (default) | `completed` | `all`, compared exactly — done is COMPLETED or CANCELLED (`calendar.TaskDoneStatuses`) — anything else `ErrInvalidInput`; `limit` defaults to 500 and is capped at 1000; a calendar that does not store VTODO (`calendar.SupportsComponent`) lists empty without a query.
- `list_all.go` — `GET /api/v1/tasks`, the cross-list page: the caller's readable calendars come from ONE `CalendarRepository.ListReadableCalendarIDs` (own, subscribed and shared; never `ListCalendarsUseCase`, which counts every calendar's events), those that store VTODO (`ReadableCalendar.SupportsComponent`) go into ONE `ListTasks` whatever their number, and each task comes back with its calendar (`ListedTask`: UUID, name, colour). Status, text, limit and offset as `list.go` (status checked before anything is read); `DueAfter`/`DueBefore` are normalised to UTC; `CalendarUUID` narrows to one readable calendar and is `ErrCalendarNotFound` (404, never an empty page) for anything outside the readable set, while a readable calendar without VTODO gives an empty page. A row of a calendar outside the queried set is an error, not a shown task.
- `summary.go` — `GET /api/v1/tasks/summary`: the same readable task lists as `list_all.go` (ONE `ListReadableCalendarIDs`), counted in ONE `CalendarRepository.CountTasks` at the caller's `AsOf` (passed as is: its UTC offset decides which date is today, `calendar.TaskDayOf`), so the counts hold beyond a list page. Open/overdue/due today count open tasks only, completed counts done ones (COMPLETED and CANCELLED, as the `completed` filter); a timed task is overdue once due before `AsOf`, an all-day one only once its date is before `AsOf`'s date; overdue and due today overlap for a timed task due earlier today. `ByCalendar` is keyed by calendar UUID with an entry for every readable task list, zeros included; no task list = zero counts and no count query. Counts of a calendar outside the queried set are an error.
- `create.go` — `POST …/tasks`: builds a complete VCALENDAR/VTODO (`PRODID`, `VERSION`, `UID` `<uuid>@calcard.io`, `DTSTAMP`/`CREATED`/`LAST-MODIFIED` from the injected clock in UTC, `SEQUENCE:0`), path `<uuid>.ics`, fresh ETag, denormalized columns, then `CreateCalendarObject` (change log + sync token). A calendar that does not store VTODO is refused first (`calendar.RequireComponent`, 400 "This calendar does not store tasks"). `due_all_day` decides the value type of DUE and DTSTART alike (date `YYYY-MM-DD` or RFC 3339); a timed value is written `TZID=<timezone>` (+ VTIMEZONE) or, without a usable zone, in UTC (F22); `completed` also writes `COMPLETED` (now) and `PERCENT-COMPLETE:100`. No recurrence field.
- `complete.go` — `POST …/complete` and `…/uncomplete`, edited in place through `editTask`. Complete writes `STATUS:COMPLETED`, `COMPLETED` (injected clock, UTC) and `PERCENT-COMPLETE:100`; uncomplete removes `COMPLETED`, writes `STATUS:NEEDS-ACTION` and resets `PERCENT-COMPLETE` to 0 only when it was 100 (the percentage from before completing is not restored). Both are idempotent on the `status` column: complete of a completed task and uncomplete of one that is not completed (in-process and cancelled included) write nothing and keep the ETag. Order: not a task of the URL's calendar → `ErrNotFound`; calendar without VTODO → 400 "This calendar does not store tasks" (as create); complete of a recurring task (RRULE/RDATE on the master) → `ErrRecurringNotSupported` (409), also when already completed; uncomplete of a recurring task is allowed.
- `update.go` — `PATCH …/tasks/:id`, edited in place through `editTask`: only the fields given change (`Change[T]` tells absent from null; null or "" removes description, due, start, priority, percent; summary, status, `due_all_day` cannot be removed). Field rules as create; `due_all_day` (absent: the task's current one) sets the value type of the due/start sent, and once the request touches due, start or `due_all_day` a kept DUE/DTSTART of the other type, due before start, or removing DTSTART behind a DURATION is a 400. A field equal to what is stored keeps its stored form (SUMMARY params, a DUE's TZID for the same instant), and a request that changes nothing writes nothing and keeps the ETag (`errUnchanged`). A TZID time adds its VTIMEZONE when missing (`addVTimezone`: add-only, never `SyncVTimezones`, so a client's blocks, unreferenced ones too, survive). `status` completed = complete (a `percent_complete` next to it is ignored), another status on a completed task = uncomplete, then that STATUS; on a recurring task status completed/cancelled or `percent_complete` 100 is `ErrRecurringNotSupported` (409) whatever its state, after the 400s.
- `delete.go` — `DELETE …/tasks/:id`: `loadTask`, then `HardDeleteCalendarObject` — a hard delete (user decision after story 23; an event DELETE soft-deletes): the row is removed for good and the "deleted" change-log row + new sync token are written in the same transaction; a row already gone (`calendar.ErrNotFound`, a lost race) is `ErrNotFound`. Allowed in a calendar that no longer stores VTODO.
- `validate.go` (field rules shared with update: summary trimmed, 1..500 runes; status `needs-action|in-process|completed|cancelled` exactly; priority 0..9; percent 0..100; due not before start) and `ical.go` (VTODO builders/setters: `newTaskCalendar`, `setTaskTime`, `setIntProp`, `markCompleted`, `markUncompleted`, `encodeTaskCalendar`, and for update `setTextProp`/`setIntChange`/`delProps` (write only on a real change; `setTextProp` compares plain text, `calendar.UnescapeText`, and writes with `calendar.NewTextProp`), `writeTaskTime`/`addVTimezone`; `editTask` is the one in-place edit (spec §5 rule 2) for complete, uncomplete and update: decode, edit the master (`calendar.TaskMaster`), bump `SEQUENCE`, `LAST-MODIFIED` + `DTSTAMP` = now UTC, re-encode WITHOUT `SyncVTimezones` (an edit that writes a TZID time adds its zone with `addVTimezone`; an edit that changes nothing returns `errUnchanged` and nothing is written), re-derive the columns, new ETag; the object changes only on success, storing it is the caller's job).
- `errors.go` — `ErrInvalidInput` (built with `invalidInputf`, or `invalidInput(err)` for a domain refusal, so the 400 body is the bare message), `ErrNotFound`, `ErrCalendarNotFound` (the cross-list `calendar_uuid` filter), `ErrRecurringNotSupported`.

### [addressbook/](addressbook/)

Address book management:

- `create.go`, `get.go`, `list.go`, `update.go`, `delete.go` — CRUD operations. `delete.go` (REST and MCP `delete_address_book`): owner check and last-book guard, then one `Repository.Delete`, which permanently removes the book with its contacts, photos, change log and shares in a single transaction. Its `shareRepo` constructor parameter is unused and only kept so existing wiring compiles.
- `create_contact.go` — Create contact within an address book.
- `export.go` — vCard export.

### [contact/](contact/)

Contact management:

- `create.go`, `get.go`, `list.go`, `update.go`, `delete.go` — CRUD operations.
- `search.go` — Contact search over every address book the caller can READ — owned plus shared —
  resolved through `addressbook.ListUseCase` so owner+share resolution (#53) stays in one place.
  It was owner-scoped until #162, which made a contact in a shared book vanish as soon as you
  searched for it. The optional address-book filter (a UUID) narrows that set and returns
  `ErrAddressBookNotFound` — a 404 — for anything outside it.
- `move.go` — Move contact between address books.
- `photo.go` — Contact photo handling.
- `mapper.go` — Contact-to-DTO mapping utilities.
  `applyContactToCard` fills a missing FN from N / ORG / NICKNAME / EMAIL / TEL (RFC 6350 §6.2.1,
  `addressbook.EnsureFormattedName`), so a REST contact saved without `formatted_name` is never nameless.

### [search/](search/)

Unified cross-resource search (#156):

- `search.go` — `GET /api/v1/search`: events (over the denormalized summary/location/description,
  with **no implicit date bound**), contacts, calendars and address books, across every collection
  the caller can read. Resolves readable collections through the calendar/address-book list use
  cases so owner+share resolution (#53) stays in one place, and picks one representative
  occurrence per matching recurring series (next one, or the last one for a finished series).

### [apppassword/](apppassword/)

Application password management (for DAV client access):

- `create.go`, `list.go`, `revoke.go` — App password CRUD.
- `caldav_credential.go`, `carddav_credential.go` — CalDAV/CardDAV-specific credential management. Create validates the username with `user.ValidateDAVCredentialUsername` (lowercase letters `a`–`z` only, 3–50); a taken one answers "username '<name>' is already in use", which the handlers turn into a 409. The password only has a length rule, 8 to `user.MaxPasswordBytes` (72) bytes; over 72 is `user.ErrPasswordTooLong` (a 400), because bcrypt would refuse to hash it.

### [user/](user/)

User profile management:

- `get_profile.go`, `update_profile.go` — Profile CRUD.
- `delete_account.go` — Account deletion: password + `DELETE` confirmation, then `UserRepository.Delete`, which removes all of the account's data (its collections, the shares in both directions, credentials) and soft-deletes the user row (F18).
- `set_default_timezone.go` — `SetDefaultTimezoneUseCase` (#169): set-if-empty for the `timezone` preference. Normalise and validate (`ErrInvalidPreferenceValue`, same message shape as the PATCH), `InsertIfAbsent`, read back; a stored value that no longer validates is replaced via `ReplaceValueIf` (compare-and-set). Always returns the stored, valid zone, so a later browser never overwrites an explicit choice (an explicit `UTC` included).

### [sharing/](sharing/)

Calendar and address book sharing:

- `create_calendar_share.go`, `list_calendar_shares.go`, `update_calendar_share.go`, `revoke_calendar_share.go` — Calendar sharing CRUD.
- `create_addressbook_share.go`, `list_addressbook_shares.go`, `update_addressbook_share.go`, `revoke_addressbook_share.go` — Address book sharing CRUD.

### [importexport/](importexport/)

Data import and export:

- `calendar_import.go` — Import iCalendar (.ics) files. Components are grouped **by UID** into one
  calendar object each (a recurring master and its `RECURRENCE-ID` overrides share a UID and must
  live in one resource, RFC 4791 §4.1). `total`/`imported`/`skipped`/`failed` count those objects,
  not components: a series with its overrides is one; a component without a UID is one failed item.
  A second master or a repeated `RECURRENCE-ID` for a UID already in the file starts another object,
  which then goes through the duplicate handling (skip/replace/duplicate) like a re-import would.
  An item whose component the target calendar does not store (`calendar.RequireComponent`, #170) is counted as `skipped` and listed in `errors` with the reason, before the duplicate handling.
- `calendar_import_timezone.go` — Copies the file's `VTIMEZONE`s into each imported object that references their TZID (RFC 5545 §3.2.19), sorted so the encoding is stable.
- `contact_import.go` — Import vCard (.vcf) files.
- `import_path.go` — Helpers shared by both imports (F23): a missing DTSTAMP is added (import time, UTC) instead of failing the event, and the DAV resource name is `<uid>.ics`/`.vcf` only for a short URL-safe UID on a free path, otherwise `<uuid>.ics` — a UID may contain `/` or `..`. Duplicates are matched by UID, never by path; `replace` keeps the replaced object's path when it is still a usable segment.
  Every card is decoded first and fails (`failed`, with the reason in `errors`) without a VERSION,
  with a VERSION other than 3.0/4.0 (what the CardDAV collections advertise), or when it does not
  decode. A missing FN is derived (`addressbook.EnsureFormattedName`); only a card with nothing to
  derive it from fails. Stored as the go-vcard encoding of the parsed card, like REST and DAV PUT (F27).
- `backup_export.go` — Full user data backup export.
- `types.go` — Import/export type definitions.

### [mcptoken/](mcptoken/)

MCP access tokens (story 104) — the long-lived bearer credential an MCP client is configured with:

- `token.go` — `Generate` / `HashToken`, plus the create, list, revoke and authenticate use cases.

Why its own credential rather than a JWT or an app password: an access token lives ten minutes, which is useless for a client configured once with a static header; and app passwords are scoped to CalDAV/CardDAV, so widening them would silently grant every existing DAV credential a full read/write tool surface. The secret is 256 bits of randomness stored as a **unique-indexed SHA-256** — not bcrypt — because an MCP client presents only the opaque string, so the token must be findable from that alone, and a full-entropy secret needs no slow KDF.

### [subscription/](subscription/)

Remote calendar subscriptions (story 100) — mirroring third-party iCalendar feeds into read-only calendars:

- `fetch.go` — the SSRF-guarded HTTP client and URL normalization (`webcal://` → `https://`). **Read this before touching anything that fetches a user-supplied URL.** The address check lives in the dialer's `Control` hook, which sees the already-resolved `ip:port` immediately before connect, so DNS rebinding — the standard bypass for a pre-flight lookup — is caught, and every redirect hop and round-robin address is covered for free. `FeedError` marks a failure as the remote feed's fault (a 4xx at the HTTP boundary) rather than ours (a 5xx); its message is written to be shown to the user and never repeats the feed URL, which routinely carries a secret token.
- `parse.go` — feed → `CalendarObject`s, grouped **by UID** (a recurring event and its `RECURRENCE-ID` overrides share a UID and must live in one resource, RFC 4791 §4.1) with only the `VTIMEZONE`s each object references. Output must be BYTE-STABLE across runs — a missing `sort` or a `time.Now()` DTSTAMP would make every refresh report every event as modified and wake every DAV client.
- `sync.go` — one refresh: conditional fetch, parse, `ReplaceFeedObjects`, then persist success or failure with backoff. Shared by the worker, the manual-refresh endpoint and the initial sync on creation, so all three record status identically.
- `create.go`, `manage.go` — the CRUD use cases. Create validates the feed BEFORE writing anything and rolls the calendar back if the subscription cannot be stored. The mirror calendar stores `VEVENT,VTODO` on purpose (#170): the parser keeps a feed's VTODOs and `ReplaceFeedObjects` bypasses the component-set check, so a narrower set would hold objects it does not advertise. A view's `event_count` counts events only — create counts the feed's VEVENT objects, every later view uses `GetEventCount` — so a feed's tasks are not reported as events. Delete is a single `CalendarRepository.Delete` of the mirror calendar: the collection cascade removes the subscription row with it, in one transaction.
- `worker.go` — the ticker that refreshes due subscriptions. A poller over a `NextSyncAt` column rather than a timer per subscription, so the schedule survives a restart with no recovery step.

### [about/](about/)

Project metadata (story 101):

- `list_open_source.go` — Serves the open-source attribution list. The data is the committed, `//go:embed`-ed `open_source_go.json`, generated by `go run ./tools/genlicenses` from the server directory — NOT hand-edited, and never fetched at runtime. This use case has no repository: the list is a build artefact, not application state.

## Key Principles

1. **No External Dependencies**: This layer remains ignorant of whether it's being called by an HTTP request, a CLI command, or a background worker.
2. **Interface Driven**: It interacts with the outside world (database, email, tokens) exclusively via interfaces defined in the `domain` layer.
3. **Pure Logic**: It should ideally contain no technical "leaks" from infrastructure frameworks like GORM or Fiber.
