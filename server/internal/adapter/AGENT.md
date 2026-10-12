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
  - **Not found = no access** (F12): calendar use cases return `calendar.ErrNotFound` for a missing calendar AND for one the caller does not own; `calendar_handler.go`'s `calendarError` (also used by the calendar import) turns it into the same 404 body GET gives, everything else stays 400. An unknown `:share_id` is `sharing.ErrCalendarShareNotFound` → 404. Never map a lookup failure to 400, and never give "not yours" a message of its own — that leaks existence.

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
  - `user_repo.go` — User persistence. `Delete` soft-deletes the user row but first hard-deletes everything the account owns or was granted, via `PurgeAccountData` (`account_purge.go`, F18). It then rewrites the e-mail and username to `deleted:<uuid>` (a tombstone), because the unique indexes cover soft-deleted rows (F19); `Count` is `Unscoped` so deleted accounts still keep first-run setup closed.
  - `account_purge.go` — `PurgeAccountData`: the account-deletion cascade (calendars with objects, change logs, subscriptions and public links; address books with contacts and photos; shares of those collections AND shares granted to the account; app passwords, DAV credentials, MCP tokens, OAuth links, preferences). Children before parents, because SQLite (`_foreign_keys=on`) and PostgreSQL enforce the GORM foreign keys. A table that gains a `user_id` must be added here, or deleted accounts leak it. The data migration `purgeDeletedAccounts` runs the same cascade for accounts deleted before it existed.
  - `username_lookup.go` — `findByUsername`, shared by the user and CardDAV-credential repos: exact spelling first, then a case-insensitive match that must be unambiguous (F02 — generated usernames are mixed-case and are also the `/dav/{username}/` segment, so stored names are never rewritten).
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
  - `handler.go` — WebDAV request dispatcher. Every error it returns goes through `serveDAVError` (`errors.go`): a go-webdav `HTTPError` from our own hand-rolled sub-handlers (sync REPORT, collection PROPFIND/PROPPATCH) is answered with its status, as emersion does internally; Fiber's default handler would make it a 500 (F17). A missing and another user's collection both answer 404. A PROPFIND one level below a principal (`/dav/{x}/{y}/`) is a 404 unless it is one of the caller's own two home sets: emersion would answer any other such path — another user's home, `/dav/{me}/{anything}/` — with an empty 207 (F11). 404, not 403, like everything below another user's home; shares live under the sharee's own home and are unaffected. Answers PROPFIND/OPTIONS on `/dav/` and the principal itself (`gowebdav.ServePrincipal`, both home sets in one response); an empty PROPFIND body there is rewritten to an explicit `allprop` first, because `ServePrincipal` would otherwise answer 400 (RFC 4918 §9.1, F06).
  - `context.go` — WebDAV request context.
  - `caldav_backend.go` — CalDAV protocol operations (calendars, events, iCalendar parsing). `ResolvePath` caps a subscribed calendar (story 100) at read-only via `calendar.EffectivePermission`, which is what makes PUT and DELETE on its objects return 403 without either handler knowing subscriptions exist. Collection-level operations that are the OWNER's prerogative — deleting the collection, PROPPATCHing its displayname — therefore compare `c.UserID` against the authenticated user instead of testing for `PermissionOwner`, or the cap would lock an owner out of their own subscription. A PUT body that parses but is not a valid calendar object (no component, no UID, two UIDs or component types, or anything the iCalendar encoder rejects) is answered with a 409 CalDAV precondition (`valid-calendar-object-resource` / `valid-calendar-data`), never a 500; validation runs before any DB lookup.
  - `carddav_backend.go` — CardDAV protocol operations (address books, contacts, vCard parsing). A vCard PUT without VERSION is a 409 `valid-address-data`, not a 500.
  - `propfind_collection.go` — the one description of a calendar / address-book collection (`collectionProps`): sync-token, getctag, supported-report-set, displayname, the CalDAV/CardDAV properties (description, supported component set / data, max size), current-user-principal and Apple `calendar-color` (the stored `#RRGGBB`). The hand-rolled Depth:0 PROPFIND answers from it, and `rewriteCollectionResponses` splices it over every collection emersion lists at Depth 1/infinity, so a collection looks the same at every Depth (F07) — add a collection property there, never to only one path. Also reports `DAV:current-user-privilege-set` — the interoperable way to tell a client a collection is read-only, for subscriptions and read-only shares alike — and `CS:source` for a subscription. The resourcetype deliberately stays `<C:calendar/>` rather than `<CS:subscribed/>`: a client that does not know the subscribed type would stop treating the collection as a calendar and hide its events entirely.
  - Collection creation (`CreateCalendar` / `CreateAddressBook`, reached by MKCOL and MKCALENDAR): the URL is resolved first, exactly like any other request (owned path, or a share's UUID / legacy path), and a URL that already maps to a collection is a **405** (RFC 4918 §9.3.1). Neither emersion nor the schema checks this — there is no unique `(user_id, path)` index, on purpose, because existing databases may already hold duplicates — so the check is the only guard.
  - `sync.go`, `sync_elements.go`, `sync_addressbook.go` — WebDAV-Sync (RFC 6578) for efficient incremental sync.
  - `proppatch.go` — the hand-rolled collection PROPPATCH (emersion's is a 501): `DAV:displayname` for both collection types, Apple `calendar-color` for calendars. All-or-nothing per RFC 4918 §9.2: any other property or any `remove` is 403, an invalid value (empty/over-long name, non-hex colour) 409, and then every other property is 424 Failed Dependency and nothing is written. A successful one goes through `UpdateMetadata`, so the rename and its sync-token bump commit together.

## Design Philosophy

Adapters are where the specific technical choices (like using JSON for API or GORM for DB) are confined. By isolating these choices here, the core business logic remains portable and easy to test. This layer depends on `domain` and `usecase`.
