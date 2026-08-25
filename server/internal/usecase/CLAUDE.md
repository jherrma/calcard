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
- `username_util.go` — Username generation utilities.

### [calendar/](calendar/)

Calendar management:

- `create.go`, `get.go`, `list.go`, `update.go`, `delete.go` — CRUD operations.
- `enable_public.go`, `get_public_status.go`, `regenerate_token.go` — Public calendar sharing.
- `export.go` — iCalendar export.

### [event/](event/)

Event management:

- `create.go`, `get.go`, `list.go`, `update.go`, `delete.go` — CRUD operations.
- `move.go` — Move event between calendars.

### [addressbook/](addressbook/)

Address book management:

- `create.go`, `get.go`, `list.go`, `update.go`, `delete.go` — CRUD operations.
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
- `caldav_credential.go`, `carddav_credential.go` — CalDAV/CardDAV-specific credential management.

### [user/](user/)

User profile management:

- `get_profile.go`, `update_profile.go` — Profile CRUD.
- `delete_account.go` — Account deletion.

### [sharing/](sharing/)

Calendar and address book sharing:

- `create_calendar_share.go`, `list_calendar_shares.go`, `update_calendar_share.go`, `revoke_calendar_share.go` — Calendar sharing CRUD.
- `create_addressbook_share.go`, `list_addressbook_shares.go`, `update_addressbook_share.go`, `revoke_addressbook_share.go` — Address book sharing CRUD.

### [importexport/](importexport/)

Data import and export:

- `calendar_import.go` — Import iCalendar (.ics) files.
- `contact_import.go` — Import vCard (.vcf) files.
- `backup_export.go` — Full user data backup export.
- `types.go` — Import/export type definitions.

### [mcptoken/](mcptoken/)

MCP access tokens (story 104) — the long-lived bearer credential an MCP client is configured with:

- `token.go` — `Generate` / `HashToken`, plus the create, list, revoke and authenticate use cases.

Why its own credential rather than a JWT or an app password: an access token lives ten minutes, which is useless for a client configured once with a static header; and app passwords are scoped to CalDAV/CardDAV, so widening them would silently grant every existing DAV credential a full read/write tool surface. The secret is 256 bits of randomness stored as a **unique-indexed SHA-256** — not bcrypt — because an MCP client presents only the opaque string, so the token must be findable from that alone, and a full-entropy secret needs no slow KDF.

### [about/](about/)

Project metadata (story 101):

- `list_open_source.go` — Serves the open-source attribution list. The data is the committed, `//go:embed`-ed `open_source_go.json`, generated by `go run ./tools/genlicenses` from the server directory — NOT hand-edited, and never fetched at runtime. This use case has no repository: the list is a build artefact, not application state.

## Key Principles

1. **No External Dependencies**: This layer remains ignorant of whether it's being called by an HTTP request, a CLI command, or a background worker.
2. **Interface Driven**: It interacts with the outside world (database, email, tokens) exclusively via interfaces defined in the `domain` layer.
3. **Pure Logic**: It should ideally contain no technical "leaks" from infrastructure frameworks like GORM or Fiber.
