# CalCard Server

CalCard is a high-performance CalDAV and CardDAV server written in Go. It provides synchronization for calendars (RFC 4791) and contacts (RFC 6352) across various clients like iOS, Android (via DAVx5), Thunderbird, and more.

## Features

- **Standard Protocols**: CalDAV (RFC 4791) and CardDAV (RFC 6352) synchronization, including WebDAV-Sync (RFC 6578).
- **Modern Authentication**: JWT-based auth and OAuth2/OIDC support (Google, Microsoft, 3rd Party).
- **Sharing**: Calendar and Address Book sharing between users.
- **Public Calendars**: Ability to publish calendars via unique URLs.
- **Security Focused**: Built-in TLS support, Rate Limiting, CORS, and Security Headers.
- **Flexible Storage**: Supports SQLite (default) and PostgreSQL.

> **Scope note:** CalCard is a personal **sync** server. It does not implement
> CalDAV scheduling (RFC 6638) — no attendee invitations (iTIP/iMIP), no
> `schedule-inbox`/`schedule-outbox`, and no free-busy. Events with `ATTENDEE`
> properties sync verbatim but generate no invitations. The server advertises
> only `calendar-access`, never `calendar-auto-schedule`.

## Quick Start

### Prerequisites

- Go 1.25+ or Docker
- A secure JWT secret (at least 32 characters)

### Running with Docker (single container, web UI included)

The easiest way to get started is using Docker Compose (run from the `server/`
directory — the compose files set the build context to the repo root so the
image can bundle the web UI):

```bash
cd server
docker compose up -d                                   # SQLite
# docker compose -f docker-compose.postgres.yml up -d  # PostgreSQL
```

This builds **one image** that serves everything on **one origin** (port 8080):
the REST API (`/api`), DAV (`/dav`), `/.well-known`, `/health`, **and the built
web UI**. Open `http://localhost:8080/` and the UI loads from the same
server that answers its API calls.

Because the UI and API share an origin, the browser never applies CORS, so
**no CORS configuration is required**. The CORS settings
(`CALDAV_CORS_ENABLED`, …) exist only as an escape hatch for split hosting (UI
and API on separate origins); leave them disabled for the single-container setup.

> The SPA is baked at image-build time as a static bundle. Do **not** set
> `NUXT_PUBLIC_API_BASE_URL` for the single-container image — an empty value
> makes the UI issue same-origin relative requests. Set it only when hosting the
> UI on a different origin than the API.

### Running from Source

1. Clone the repository
2. Navigate to the server directory: `cd server`
3. Copy the example configuration: `cp configs/config.yaml.example config.yaml`
4. Update `config.yaml` with your settings (don't forget the JWT secret)
5. Run the server: `go run ./cmd/server`

For frontend development, run the SPA separately with `cd webinterface && pnpm dev`
(serves on `:3000` against the API on `:8080`); in that mode the UI defaults
`apiBaseUrl` to `http://localhost:8080`.

---

## Documentation

- **Configuration Reference**: [CONFIGURATION.md](CONFIGURATION.md) - Detailed guide for all YAML and Environment parameters.
- **Example Configuration**: [server/configs/config.yaml.example](server/configs/config.yaml.example) - A complete example YAML file.
- **Architecture**: [server/CLAUDE.md](server/CLAUDE.md) and the per-layer `CLAUDE.md` / `AGENT.md`
  files under `server/internal/` - layering, request flow, and conventions. (These replaced the
  former root-level `Technical Overview.md`.)
- **Feature status**: [STORIES.md](STORIES.md) - what is implemented, what remains.

## API Documentation

There is no generated API reference. The REST surface is documented in
[server/CLAUDE.md](server/CLAUDE.md) (endpoint areas) and, per endpoint, by the
`// METHOD /api/v1/...` doc comment above each handler in
`server/internal/adapter/http/`.

---

## Web Interface

The project includes a modern web interface built with Nuxt 4.

### Prerequisites

- Node.js 22+
- pnpm (Recommended)

### Running the Frontend

1. Navigate to the web interface directory:
   ```bash
   cd webinterface
   ```
2. Install dependencies:
   ```bash
   pnpm install
   ```
3. Run in development mode:
   ```bash
   pnpm run dev
   ```
   The interface will be available at `http://localhost:3000`.

### Building and Deploying

To create a production-optimized build:

1. Build the project:
   ```bash
   pnpm run build
   ```
2. Preview the production build:
   ```bash
   pnpm run preview
   ```
