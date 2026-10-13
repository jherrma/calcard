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
| Auth | First-run setup (`POST /auth/setup`, creates the admin, 409 once any user exists; optionally guarded by `CALDAV_SETUP_TOKEN`, which also locks registration until setup is done), login, register, verify (`GET /auth/verify?token=`; the activation mail links the SPA page `/auth/verify?token=`, which calls it, as the reset mail links `/auth/reset-password`), refresh, logout, forgot/reset password. Every endpoint that sets a password (setup, register, reset, `PUT /users/me/password`) applies `user.ValidatePassword` — 8 to 72 **bytes** (bcrypt's limit), upper, lower, digit, special — and answers a violation with a 400 naming the rule; login applies none of it |
| OAuth | Initiate, callback, link/unlink providers |
| System | Settings (admin configured, SMTP enabled, registration enabled, setup token required), auth methods |
| User | Get/update profile, delete account, get/update preferences. The profile (and the login response's `user`) carries `username`, the account's `/dav/{username}/` segment, exactly as stored |
| Calendars | CRUD, public sharing, export |
| Events | CRUD, move between calendars. An event is reachable only under the calendar that holds it: a mismatched or unknown `:calendar_id` is the same 404 as an unknown event (F09) |
| Address Books | CRUD, export |
| Contacts | CRUD, search (over every readable address book, shared included — #162), move, photo. The contact must live in the URL's `:addressbook_id` (move included), else 404 |
| Search | `GET /api/v1/search` — unified search over events (no date bound), contacts, calendars and address books (#156) |
| Sharing | Calendar and address book share CRUD |
| Credentials | App passwords, CalDAV/CardDAV credentials. Creation validates (F31): the name is trimmed and required (max 100 characters, as for MCP tokens); app-password `scopes` must be a non-empty subset of `caldav`/`carddav` and default to both when omitted; a DAV credential's `expires_at` must lie in the future, its password must be 8 to 72 bytes, and its username must be 3–50 lowercase letters `a`–`z` — upper case is refused, never folded (the SPA generates 16). Each violation is a 400; a username in use is a 409. Only creation is checked: rows stored before keep working unchanged |
| Import/Export | Calendar import (.ics), contact import (.vcf), full backup export |
| About | Open-source attribution list (`GET /api/v1/about/open-source`, authenticated) |
| MCP tokens | `GET`/`POST /api/v1/mcp-tokens`, `DELETE /api/v1/mcp-tokens/:id` — the bearer credentials MCP clients use (#104) |
| Subscriptions | `GET`/`POST /api/v1/calendar-subscriptions`, `GET`/`PATCH`/`DELETE /:id`, `POST /:id/refresh` — remote iCalendar feeds mirrored into read-only calendars (story 100). Raw JSON |
| Health | `GET /health` |

**Raw JSON (no envelope)**: calendars (incl. shares, public access), events, address books (incl. shares), contacts, `/contacts/search`, `/search`, CalDAV/CardDAV credentials, import, `/mcp-tokens`, `/calendar-subscriptions`, `GET /auth/oauth/providers` and `GET /ready`. Errors are mostly `{ "status": "error", "message" }` (`ErrorResponse()`), though some raw-JSON handlers answer `{ "error": ... }`. `POST /auth/register` answers 200, not 201; that is kept for client compatibility.

### CalDAV/CardDAV (`/dav/...`)

WebDAV protocol endpoints for native calendar/contact client sync (Apple Calendar, Thunderbird, DAVx5, etc.). Uses HTTP Basic Auth with app passwords or DAV credentials. The Basic-auth username — the account's (or its e-mail), a CalDAV or a CardDAV credential's — is accepted in any case. New usernames are stored in lower case (registration generates 16 lowercase letters, setup lower-cases the admin's, a credential's must be lowercase). The CalDAV credential lookup lower-cases the input in Go and matches it exactly; account and CardDAV credential lookups try the exact spelling, then an unambiguous `LOWER()` match in SQL (F02). The `/dav/{username}/` URL segment is compared exactly.

Basic auth accepts the account's primary password, so with `rate_limit.enabled` **failed** Basic attempts are limited per client IP (`auth_ip_requests`) and per identifier — the user name or e-mail sent, trimmed and lower-cased — (`auth_email_requests`) within `rate_limit.window` (#193, `http.FailedAuthLimiter`, wired with `webdav.Handler.WithBasicAuthLimiter`). A valid credential gives its attempt back and a request without credentials never counts, so syncing clients do not use the allowance up; once a key is spent, every Basic attempt for it — a correct one included — is the auth limiters' 429 with `Retry-After` until the window ends, checked before any lookup or bcrypt. Bearer tokens are not limited.

### MCP (`/mcp`)

Model Context Protocol server (story 104) — JSON-RPC 2.0 over Streamable HTTP, so an AI
assistant can read and manage the caller's calendars and contacts. `POST /mcp` carries the
JSON-RPC messages, `GET /mcp` returns a manifest (or opens the SSE notification stream when
`Accept: text/event-stream`), `DELETE /mcp` terminates a session.

Authentication is a bearer **MCP access token** (`calcard_mcp_…`, minted per user at
`/api/v1/mcp-tokens`); a JWT access token is also accepted but expires in minutes. 19 tools
(event and contact CRUD, creating/renaming/deleting calendars and address books, unified search,
`find_free_slots`) and four resources are advertised.

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
- `internal/usecase/AGENT.md` — Business logic (auth, calendar, event, contact, sharing, import/export)
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
- **SMTP**: A new account is active (can log in) at once, SMTP or not. SMTP only decides `EmailVerified`: without SMTP it is true at once; with SMTP it stays false until the mailed link is opened (`verify.go`, which sets nothing else) or a password reset completes (which also deletes every session and credential of the account, `UserRepository.CompletePasswordReset`). Login and sharing do not wait for it (a share by e-mail resolves an unconfirmed account like any other — owner decision); only the OAuth callback refuses to auto-link a provider identity onto an account whose address is unconfirmed (`ErrLocalEmailNotVerified`). `IsActive=false` only means a deactivated account. A duplicate registration is a 409 in both modes (with SMTP the owner is also mailed); the earlier P01 trick of answering it like a new address cannot work once the account is usable before confirmation. See CONFIGURATION.md, Registration section. Forgot-password follows the same rule: an unknown address is a silent 200, but a send that really failed (mail server unreachable, storage error) is a 500 with a generic message and the cause in the log, and the SPA shows it as an error instead of the green notice.
- **Base URL**: `cfg.BaseURL` (`CALDAV_BASE_URL`) is optional and has no default. URLs returned to the caller of a request (app-password `server_url`, calendar `public_url`) go through `adapter/http.RequestBaseURL`, which falls back to the request's own origin; anything that leaves the request (e-mail links, OAuth redirect URIs) uses `cfg.BaseURL` only, and `Validate` refuses to start with SMTP or OAuth configured but no base URL.
- **Open-source attribution**: `internal/usecase/about` embeds `open_source_go.json`, generated by `go run ./tools/genlicenses` and committed. Regenerate it after any `go.mod` change; the Dockerfile re-runs it but tolerates failure because the committed file is authoritative.
- **Time zones**: `cmd/server` imports `time/tzdata`, so zone names resolve even without a system zoneinfo (the image installs `tzdata`, which takes precedence). Event `timezone`s, calendar time-zone validation and the VTIMEZONEs generated for stored events (`domain/calendar/vtimezone.go`) depend on it.
- **WebDAV methods**: Custom HTTP methods (PROPFIND, REPORT, MKCALENDAR, etc.) registered in `infrastructure/server/server.go`.
- **MCP**: `/mcp` is a facade over the existing use cases (story 104). Tools must never query or authorize on their own — reuse the use case and the `GetUserPermission` helpers in `adapter/mcp/access.go`, or the MCP surface silently becomes a way around the sharing model.
- **Subscribed calendars** (story 100): `Calendar.Subscribed` marks a read-only mirror of a remote iCalendar feed. Read-only is enforced by `calendar.EffectivePermission`, which caps the resolved permission at read — REST, CalDAV and MCP all funnel through it, so a new write path inherits the rule for free. **Never add a write that bypasses it**: the next refresh replaces the collection wholesale, so anything written there disappears silently. Collection-level ownership (rename, share, delete) is deliberately unaffected. The only permitted writer is `CalendarRepository.ReplaceFeedObjects`, used by the sync path.
- **Outbound HTTP**: subscriptions are the only feature that fetches a URL a user supplies. Anything similar must go through `usecase/subscription.Fetcher` or reproduce its dial-time address guard — a pre-flight DNS check is not equivalent (rebinding), and a failure message must never repeat the URL.
- **Server-written timestamps are UTC**: go-ical's `SetDateTime` writes the `…Z` form only for `time.UTC`; a `time.Now()` (zone `time.Local`) becomes `DTSTAMP;TZID=Local:…`, which RFC 5545 §3.8.7.2 forbids. Always pass `time.Now().UTC()`, and the same for a vCard `REV` formatted with a literal `Z` (F04).
- **Deleting a collection is permanent**: a calendar or address book deleted over REST, MCP, CalDAV/CardDAV or the subscription endpoints is hard-deleted with everything in it — events/contacts, photos, change log, shares, subscription, public link — in one transaction, by the collection cascade in `adapter/repository/collection_purge.go` (account deletion runs the same cascade for each of the account's collections). Callers just call the repository `Delete`; a new table that hangs off a collection must be added to that cascade, or deleted collections leak its rows. The boot data migration `purgeDeletedCollections` removes what older versions left behind (they soft-deleted only the collection row).
- **Dependency injection**: All wiring happens in `infrastructure/server/routes.go`.
