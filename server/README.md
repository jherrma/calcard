# CalDAV/CardDAV Server Backend

This is the backend for the CalCard project, a modern, self-hostable CalDAV/CardDAV server written in Go.

## Setup

1.  Ensure you have Go 1.22+ installed.
2.  Navigate to this directory: `cd server`
3.  Install dependencies: `go mod tidy`
4.  Run the server: `go run ./cmd/server`

## Configuration

### WebDAV HTTP Methods

The server uses Fiber v3, which by default only allows standard HTTP methods. To support CalDAV/CardDAV, the following methods have been explicitly enabled in `internal/infrastructure/server/server.go`:

- `PROPFIND`
- `PROPPATCH`
- `MKCOL`
- `COPY`
- `MOVE`
- `LOCK`
- `UNLOCK`
- `REPORT`
- `MKCALENDAR` (standard CalDAV method)

If you add new WebDAV-based features, ensure the required methods are added to the `fiber.Config.RequestMethods` slice.

### OAuth2/OpenID Connect Setup

The server supports logging in via OAuth2/OIDC providers (Google, Microsoft, etc.).

#### 1. Configure Providers in `config.yaml`

Create a `config.yaml` file in the `server` directory if it doesn't exist. Add your provider credentials:

```yaml
oauth:
  google:
    client_id: "YOUR_GOOGLE_CLIENT_ID"
    client_secret: "YOUR_GOOGLE_CLIENT_SECRET"
    issuer: "https://accounts.google.com"
  microsoft:
    client_id: "YOUR_MICROSOFT_CLIENT_ID"
    client_secret: "YOUR_MICROSOFT_CLIENT_SECRET"
    issuer: "https://login.microsoftonline.com/common/v2.0"
```

#### 2. Register Applications

You must register an application with each provider to obtain the Client ID and Secret.

**Redirect URLs:**
The server listens on `http://localhost:8080` by default. You must whitelist the callback URL for each provider:

- **Google**: `http://localhost:8080/api/v1/auth/oauth/google/callback`
- **Microsoft**: `http://localhost:8080/api/v1/auth/oauth/microsoft/callback`

_(Replace `http://localhost:8080` with your actual public base URL if deployed)_

**Google Setup:**

1.  Go to the [Google Cloud Console](https://console.cloud.google.com/).
2.  Create a new project or select an existing one.
3.  Navigate to **APIs & Services > Credentials**.
4.  Create **OAuth client ID** credentials.
5.  Select **Web application**.
6.  Add the Redirect URI mentioned above.
7.  Copy the Client ID and Client Secret to your `config.yaml`.

**Microsoft Setup:**

1.  Go to the [Azure Portal](https://portal.azure.com/).
2.  Search for **App registrations** and create a new registration.
3.  Set the Redirect URI (Web) to the URL mentioned above.
4.  Once created, note the **Application (client) ID**.
5.  Under **Certificates & secrets**, create a new client secret and copy the value.
6.  Update your `config.yaml`.

**Custom OIDC Provider Setup:**

You can configure a generic OpenID Connect provider (e.g., Keycloak, Auth0, Zitadel).

1.  Add the `custom` section to your `config.yaml`:

```yaml
oauth:
  custom:
    client_id: "YOUR_CUSTOM_CLIENT_ID"
    client_secret: "YOUR_CUSTOM_CLIENT_SECRET"
    issuer: "https://your-oidc-provider.com/realms/your-realm"
```

2.  Ensure your provider supports OpenID Connect discovery (`/.well-known/openid-configuration`).
3.  Whitelist the callback URL: `http://localhost:8080/api/v1/auth/oauth/custom/callback`.

## Building for Release

### Prerequisites

- Go 1.25.6+
- GCC and SQLite dev libraries (for CGO/SQLite support)
- Node.js 18+ and npm (for the web interface)
- [swag](https://github.com/swaggo/swag) (`go install github.com/swaggo/swag/cmd/swag@latest`)

### Backend

1.  Generate Swagger docs:

    ```bash
    cd server
    swag init -g cmd/server/main.go --parseDependency --parseInternal
    ```

2.  Build a statically linked binary:

    ```bash
    CGO_ENABLED=1 go build -a -ldflags '-linkmode external -extldflags "-static"' -o server ./cmd/server
    ```

    The resulting `server` binary is self-contained and can be deployed to any compatible Linux host.

### Frontend

1.  Install dependencies and build the SPA:

    ```bash
    cd webinterface
    npm install
    npm run build
    ```

2.  The build output is in `webinterface/.output/public/`. Serve it with any static file server or reverse proxy (e.g., Nginx, Caddy) and point the `NUXT_PUBLIC_API_BASE_URL` environment variable at the backend.

### Docker

The simplest way to build and run the server is with Docker Compose:

```bash
# SQLite (default)
cd server && docker-compose up --build

# PostgreSQL
cd server && docker-compose -f docker-compose.postgres.yml up --build
```

The Dockerfile produces a minimal Alpine-based image with a statically linked binary. Key environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `CALDAV_SERVER_HOST` | `0.0.0.0` | Listen address |
| `CALDAV_SERVER_PORT` | `8080` | Listen port |
| `CALDAV_JWT_SECRET` | — | **Required.** Secret for signing JWTs |
| `CALDAV_BASE_URL` | `http://localhost:8080` | Public-facing URL |
| `CALDAV_DATA_DIR` | `/data` | Data directory (SQLite DB, uploads) |
| `CALDAV_DB_HOST` | — | PostgreSQL host (omit for SQLite) |
| `CALDAV_DB_PORT` | `5432` | PostgreSQL port |
| `CALDAV_DB_USER` | — | PostgreSQL user |
| `CALDAV_DB_PASSWORD` | — | PostgreSQL password |
| `CALDAV_DB_NAME` | — | PostgreSQL database name |

See `configs/config.yaml.example` for the full configuration reference.

## Architecture

The project follows Clean Architecture principles:

- `internal/domain`: Core entities and business rules.
- `internal/usecase`: Application-specific business logic.
- `internal/adapter`: Interface adapters (HTTP handlers, database repositories).
- `internal/infrastructure`: External tools and frameworks (database setup, configuration).
