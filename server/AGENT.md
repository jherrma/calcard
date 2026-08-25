# Server (Go Backend)

The Go backend implements the CalDAV/CardDAV server, REST API, and protocol handlers. It follows **Clean Architecture** principles with strict dependency rules between layers.

## Directory Structure

```
server/
├── cmd/server/          # Application entrypoint (main.go)
├── configs/             # Configuration examples (config.yaml.example)
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
├── docker-compose.yml   # SQLite deployment
└── docker-compose.postgres.yml  # PostgreSQL deployment
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

JSON API for the web frontend. Most responses are wrapped in `{ "status": "ok", "data": ... }` via `SuccessResponse()`.

| Area | Endpoints |
|------|-----------|
| Auth | Login, register, verify, refresh, logout, forgot/reset password |
| OAuth | Initiate, callback, link/unlink providers |
| System | Settings (admin configured, SMTP enabled, registration enabled), auth methods |
| User | Get/update profile, delete account, get/update preferences |
| Calendars | CRUD, public sharing, export |
| Events | CRUD, move between calendars |
| Address Books | CRUD, export |
| Contacts | CRUD, search (over every readable address book, shared included — #162), move, photo |
| Search | `GET /api/v1/search` — unified search over events (no date bound), contacts, calendars and address books (#156) |
| Sharing | Calendar and address book share CRUD |
| Credentials | App passwords, CalDAV/CardDAV credentials |
| Import/Export | Calendar import (.ics), contact import (.vcf), full backup export |
| About | Open-source attribution list (`GET /api/v1/about/open-source`, authenticated) |
| MCP tokens | `GET`/`POST /api/v1/mcp-tokens`, `DELETE /api/v1/mcp-tokens/:id` — the bearer credentials MCP clients use (#104) |
| Health | `GET /health` |

**Exception**: AddressBook and Contact endpoints return raw JSON (not wrapped in `SuccessResponse`).

### CalDAV/CardDAV (`/dav/...`)

WebDAV protocol endpoints for native calendar/contact client sync (Apple Calendar, Thunderbird, DAVx5, etc.). Uses HTTP Basic Auth with app passwords or DAV credentials.

### MCP (`/mcp`)

Model Context Protocol server (story 104) — JSON-RPC 2.0 over Streamable HTTP, so an AI
assistant can read and manage the caller's calendars and contacts. `POST /mcp` carries the
JSON-RPC messages, `GET /mcp` returns a manifest (or opens the SSE notification stream when
`Accept: text/event-stream`), `DELETE /mcp` terminates a session.

Authentication is a bearer **MCP access token** (`calcard_mcp_…`, minted per user at
`/api/v1/mcp-tokens`); a JWT access token is also accepted but expires in minutes. 13 tools
(calendar + contact CRUD, unified search, `find_free_slots`) and four resources are advertised.

**It is a facade, not a second API.** Every tool delegates to the same use case the REST
handler calls and resolves permissions through the same `GetUserPermission` methods, so MCP can
never reach data REST would refuse. Preserve that when adding tools: no MCP-specific query, no
MCP-specific permission logic. Disable the whole surface with `CALDAV_MCP_ENABLED=false`.

## Context Files

Each `internal/` subdirectory has its own AGENT.md with detailed file listings:

- `internal/AGENT.md` — Architecture overview and request flow
- `internal/domain/AGENT.md` — Domain models and interfaces
- `internal/usecase/AGENT.md` — Business logic (auth, calendar, event, contact, sharing, import/export)
- `internal/usecase/auth/AGENT.md` — Authentication use cases (local, OAuth)
- `internal/adapter/AGENT.md` — HTTP handlers, repositories, auth implementations, WebDAV
- `internal/infrastructure/AGENT.md` — Database, server, email, logging

## Development

```bash
go build ./...              # Build
go test ./...               # Run all tests
go run ./cmd/server         # Run dev server
./scripts/build.sh          # Build binary to bin/server
go run ./tools/genlicenses                    # Regenerate the open-source attribution manifest (#101)
```

**There is no generated API reference.** Swagger/OpenAPI was removed along with story 030 —
no `swag`, no `docs/` package, no `/api/docs` route. Each handler carries a
`// METHOD /api/v1/...` doc comment instead; keep it in sync when a route changes, and
document endpoint areas in the API Surface table above.

## Key Conventions

- **Configuration**: Loaded from environment variables (`CALDAV_*`) and/or `config.yaml`. See `configs/config.yaml.example`.
- **Database**: SQLite by default (zero config), PostgreSQL when `CALDAV_DB_HOST` is set.
- **Migrations**: GORM `AutoMigrate` — all models registered in `infrastructure/database/migrations.go`.
- **Auth**: JWT for REST API, HTTP Basic Auth for DAV endpoints. Backend uses `expires_at` (Unix timestamp) for JWT expiry.
- **SMTP**: When `cfg.SMTP.Host == ""` (SMTP not configured), users are auto-activated on registration.
- **Open-source attribution**: `internal/usecase/about` embeds `open_source_go.json`, generated by `go run ./tools/genlicenses` and committed. Regenerate it after any `go.mod` change; the Dockerfile re-runs it but tolerates failure because the committed file is authoritative.
- **WebDAV methods**: Custom HTTP methods (PROPFIND, REPORT, MKCALENDAR, etc.) registered in `infrastructure/server/server.go`.
- **MCP**: `/mcp` is a facade over the existing use cases (story 104). Tools must never query or authorize on their own — reuse the use case and the `GetUserPermission` helpers in `adapter/mcp/access.go`, or the MCP surface silently becomes a way around the sharing model.
- **Dependency injection**: All wiring happens in `infrastructure/server/routes.go`.
