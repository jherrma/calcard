# Adapter Layer

The adapter layer is responsible for translating data between the application's internal layers (domain and use case) and the outside world. It follows the **Clean Architecture** pattern by implementing the "Port" interfaces defined in the domain layer.

## Subdirectories

### [http/](http/)

- **Purpose**: Handles HTTP/REST communication using the Fiber framework.
- **Key Components**:
  - **Handlers**: One handler per domain area — `auth_handler.go`, `oauth_handler.go`, `user_handler.go`, `system_handler.go`, `calendar_handler.go`, `event_handler.go`, `addressbook_handler.go`, `contact_handler.go`, `calendar_share_handler.go`, `addressbook_share_handler.go`, `calendar_public_handler.go`, `public_calendar_handler.go`, `app_password_handler.go`, `caldav_credential_handler.go`, `carddav_credential_handler.go`, `import_handler.go`, `backup_handler.go`, `mcp_token_handler.go` (MCP access tokens, #104), `subscription_handler.go` (remote calendar subscriptions, story 100), `health.go`, `about_handler.go` (open-source attribution), `search_handler.go` (unified search, #156).
  - **DTOs** (`dto/`): Data Transfer Objects for auth, user, contact, addressbook, event, and credentials.
  - **Middleware**: `auth_middleware.go` (JWT verification, then the per-user revocation check `User.AcceptsAccessTokenIssuedAt` — P02), `rate_limiter.go`.
  - **Base URL**: `base_url.go` — `RequestBaseURL(c, cfg)` returns `cfg.BaseURL` or, when it is unset, the origin the request came in on (Host header; `X-Forwarded-Proto`/`-Host` only with `CALDAV_TRUST_FORWARDED_HEADERS`). Use it **only** for URLs returned to the caller of the same request (app-password `server_url`, calendar `public_url`). E-mail links and OAuth redirect URIs must keep using `cfg.BaseURL` — config validation requires it whenever SMTP or OAuth is configured — or a forged Host header poisons a password-reset link.
  - **Responses**: `response.go` — `SuccessResponse()` wraps a payload in `{ "status": "ok", "data": ... }`; `ErrorResponse()` writes `{ "status": "error", "message" }`. Only the auth, user, system, app-password, about and health handlers use the success envelope. Calendar, event, address book, contact, sharing, public-calendar, CalDAV/CardDAV-credential, import, MCP-token, search, subscription and OAuth-provider-list handlers return raw JSON. Some of the raw ones answer errors as `{ "error": ... }`. Match the existing shape of an area; changing one breaks the SPA and API clients.

### [mcp/](mcp/)

- **Purpose**: Model Context Protocol server (story 104) — a JSON-RPC 2.0 tool/resource surface over Streamable HTTP at `/mcp`, so an AI assistant can read and manage the caller's calendars and contacts.
- **Key Components**:
  - `protocol.go` — JSON-RPC and MCP message types, protocol-version negotiation, the handshake `instructions` handed to the model.
  - `errors.go` — JSON-RPC error codes and response constructors.
  - `server.go` — `Deps` (the use cases the tools delegate to), the tool registry, and the method dispatcher.
  - `handler.go` — Fiber transport: bearer authentication (MCP token or JWT; a JWT gets the same revocation check as the REST middleware), `POST` (JSON-RPC, single or batch), `GET` (manifest or SSE stream), `DELETE` (terminate session).
  - `session.go` — In-memory `Mcp-Session-Id` store. Sessions are bound to a user, so a leaked id cannot be used by another account.
  - `access.go` — `callContext` plus the permission helpers, mirroring the HTTP handlers exactly.
  - `tools_calendar.go`, `tools_contact.go`, `tools_scheduling.go` — the 13 advertised tools.
  - `resources.go` — `calendars://list`, `contacts://list`, `calendars://{id}/events`, `contacts://{id}`.
- **Invariant**: this package owns **no** persistence, queries or permission logic. Every tool calls the same use case the REST handler does and resolves access through the same repository methods, so MCP can never reach data REST would refuse. A tool that reaches past a use case is a bug, not an optimization.

### [repository/](repository/)

- **Purpose**: Implements persistence logic for domain entities using GORM.
- **Key Components**:
  - `user_repo.go` — User persistence.
  - `refresh_token_repo.go` — Refresh token storage.
  - `password_reset_repo.go` — Password reset token storage.
  - `calendar_repo.go` — Calendar persistence. `ReplaceFeedObjects` makes a subscribed calendar's contents match a feed in one transaction (story 100) — one sync token per changed resource but a single calendar-row update, and an unchanged feed touches neither, so a static `.ics` republished hourly does not wake every DAV client. `Delete` cascades to the calendar's subscription row, because a calendar can be deleted from three places and only one of them knows subscriptions exist.
  - `addressbook_repository.go` — AddressBook and contact persistence (with pagination and search).
  - `app_password_repo.go` — App password storage.
  - `caldav_credential_repo.go`, `carddav_credential_repo.go` — DAV credential storage.
  - `mcp_token_repo.go` — MCP access-token storage, looked up by SHA-256 of the presented secret (#104).
  - `subscription_repo.go` — Calendar-subscription storage, incl. `FindDue` (the worker's only query, story 100).
  - `calendar_share_repo.go`, `addressbook_share_repo.go` — Sharing persistence.
  - `oauth_connection_repo.go` — OAuth provider link storage.
  - `system_setting_repo.go` — System settings persistence.

### [auth/](auth/)

- **Purpose**: Implements authentication-related services.
- **Key Components**:
  - `jwt.go` — JWT token generation and validation. Access tokens carry `iat_ns` (issue time in nanoseconds) next to the standard `iat`; `ValidateAccessToken` returns it as `AccessTokenClaims.IssuedAt` (falling back to the whole-second `iat` for older tokens). Validation proves only signature and expiry — the caller must still check the user's revocation marker.
  - `basic_auth.go` — HTTP Basic Auth for CalDAV/CardDAV client access (app passwords and DAV credentials).
  - `oauth.go` — OIDC/OAuth2 provider management using `go-oidc` and `golang.org/x/oauth2`.

### [middleware/](middleware/)

- **Purpose**: Reusable HTTP middleware.
- **Key Components**:
  - `cors.go` — CORS configuration.
  - `rate_limit.go` — Rate limiting.
  - `security_headers.go` — Security headers (HSTS, CSP, etc.).

### [webdav/](webdav/)

- **Purpose**: Implements the CalDAV (RFC 4791) and CardDAV (RFC 6352) protocol backends.
- **Key Components**:
  - `handler.go` — WebDAV request dispatcher.
  - `context.go` — WebDAV request context.
  - `caldav_backend.go` — CalDAV protocol operations (calendars, events, iCalendar parsing). `ResolvePath` caps a subscribed calendar (story 100) at read-only via `calendar.EffectivePermission`, which is what makes PUT and DELETE on its objects return 403 without either handler knowing subscriptions exist. Collection-level operations that are the OWNER's prerogative — deleting the collection, PROPPATCHing its displayname — therefore compare `c.UserID` against the authenticated user instead of testing for `PermissionOwner`, or the cap would lock an owner out of their own subscription.
  - `carddav_backend.go` — CardDAV protocol operations (address books, contacts, vCard parsing).
  - `propfind_collection.go` — the hand-rolled Depth:0 collection PROPFIND (sync-token, getctag, supported-report-set, displayname). Also reports `DAV:current-user-privilege-set` — the interoperable way to tell a client a collection is read-only, for subscriptions and read-only shares alike — and `CS:source` for a subscription. The resourcetype deliberately stays `<C:calendar/>` rather than `<CS:subscribed/>`: a client that does not know the subscribed type would stop treating the collection as a calendar and hide its events entirely.
  - `sync.go`, `sync_elements.go`, `sync_addressbook.go` — WebDAV-Sync (RFC 6578) for efficient incremental sync.
  - `caldav_component.go` — `calendarComponentSet` (VEVENT, VTODO): the one list `mapCalendar` advertises as `supported-calendar-component-set` and `PutCalendarObject` enforces. A PUT holding any other component (VJOURNAL, VFREEBUSY, …; VTIMEZONE is always fine) is a **403** with `CALDAV:supported-calendar-component` (RFC 4791 §5.3.2.1), wrapped as `webdav.NewHTTPError(403, caldav.NewPreconditionError(…))` because the library's precondition helper is always 409 (F27).

## Design Philosophy

Adapters are where the specific technical choices (like using JSON for API or GORM for DB) are confined. By isolating these choices here, the core business logic remains portable and easy to test. This layer depends on `domain` and `usecase`.
