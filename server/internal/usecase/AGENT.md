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
- `username_util.go` — Username generation for registration and OAuth sign-up: 16 random lowercase letters (older accounts keep their mixed-case names).

### [calendar/](calendar/)

Calendar management:

- `create.go`, `get.go`, `list.go`, `update.go`, `delete.go` — CRUD operations. `delete.go` (REST and MCP `delete_calendar`): owner check, `DELETE` confirmation, last-calendar guard, then one `CalendarRepository.Delete`, which permanently removes the calendar with its events, change log, shares, subscription and public link in a single transaction — no second clean-up step. Its `shareRepo` constructor parameter is unused and only kept so existing wiring compiles.
- `owned.go` — `LoadOwnedCalendar` / `LoadOwnedCalendarByID`: the one owner lookup (missing or not owned → `calendar.ErrNotFound`, a storage error stays an internal error). Reused by the calendar import and the calendar share use cases.
- `enable_public.go`, `get_public_status.go`, `regenerate_token.go` — Public calendar sharing.
- `export.go` — iCalendar export.

### [event/](event/)

Event management:

- `create.go`, `get.go`, `list.go`, `update.go`, `delete.go` — CRUD operations. An optional IANA `timezone` (REST and MCP alike) stores DTSTART/DTEND with `TZID=<zone>`; create and update then call `calendar.SyncVTimezones`, so the object carries a VTIMEZONE for every TZID it uses and none it no longer uses (F20, RFC 5545 §3.2.19). Objects written before that heal on their next REST/MCP edit. The zone name `Local` is ignored like an unknown one (it would mean the server host's zone).
- `move.go` — Move event between calendars.
- Input validation lives here, not in the DTOs (the server has no struct validator). `create.go`
  rejects a missing (zero) start or end and an end before the start with `ErrInvalidInput` (a 400 over
  REST, an error result over MCP); without that a create with no times was stored at 0001-01-01 (F13).
- `datetime.go` — `icalDateTime`: a timed start/end written without a `timezone` (an RFC 3339
  offset such as `+05:00`, or a `time.Local` value) is stored in UTC (`DTSTART:…Z`), never with an
  empty `TZID=` or `TZID=Local` (F22). A `timezone` (IANA) or the event's stored TZID is kept;
  all-day values keep their wall-clock date.

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
- `create.go`, `manage.go` — the CRUD use cases. Create validates the feed BEFORE writing anything and rolls the calendar back if the subscription cannot be stored. Delete is a single `CalendarRepository.Delete` of the mirror calendar: the collection cascade removes the subscription row with it, in one transaction.
- `worker.go` — the ticker that refreshes due subscriptions. A poller over a `NextSyncAt` column rather than a timer per subscription, so the schedule survives a restart with no recovery step.

### [about/](about/)

Project metadata (story 101):

- `list_open_source.go` — Serves the open-source attribution list. The data is the committed, `//go:embed`-ed `open_source_go.json`, generated by `go run ./tools/genlicenses` from the server directory — NOT hand-edited, and never fetched at runtime. This use case has no repository: the list is a build artefact, not application state.

## Key Principles

1. **No External Dependencies**: This layer remains ignorant of whether it's being called by an HTTP request, a CLI command, or a background worker.
2. **Interface Driven**: It interacts with the outside world (database, email, tokens) exclusively via interfaces defined in the `domain` layer.
3. **Pure Logic**: It should ideally contain no technical "leaks" from infrastructure frameworks like GORM or Fiber.
