# CalCard Server

CalCard is a high-performance CalDAV and CardDAV server written in Go. It provides synchronization for calendars (RFC 4791) and contacts (RFC 6352) across various clients like iOS, Android (via DAVx5), Thunderbird, and more.

## Features

- **Standard Protocols**: Full support for CalDAV and CardDAV.
- **Modern Authentication**: JWT-based auth, OAuth2/OIDC support (Google, Microsoft, 3rd Partyp), and SAML 2.0.
- **Sharing**: Calendar and Address Book sharing between users.
- **Public Calendars**: Ability to publish calendars via unique URLs.
- **Security Focused**: Built-in TLS support, Rate Limiting, CORS, and Security Headers.
- **Flexible Storage**: Supports SQLite (default) and PostgreSQL.

## Quick Start

### Prerequisites

- Go 1.25+ or Docker
- A secure JWT secret (at least 32 characters)

### Running with Docker

The easiest way to get started is using Docker Compose:

```bash
docker-compose up -d
```

This will start the server with a SQLite database on port 8080.

### Running from Source

1. Clone the repository
2. Navigate to the server directory: `cd server`
3. Copy the example configuration: `cp configs/config.yaml.example config.yaml`
4. Update `config.yaml` with your settings (don't forget the JWT secret)
5. Run the server: `go run ./cmd/server`

---

## Documentation

- **Configuration Reference**: [CONFIGURATION.md](file:///home/jherrmann/go/src/calcard/CONFIGURATION.md) - Detailed guide for all YAML and Environment parameters.
- **Example Configuration**: [server/configs/config.yaml.example](file:///home/jherrmann/go/src/calcard/server/configs/config.yaml.example) - A complete example YAML file.
- **Technical Overview**: [Technical Overview.md](file:///home/jherrmann/go/src/calcard/Technical Overview.md) - Deep dive into architecture and design choices.

## API Documentation

The server exposes an OpenAPI 3.1 specification and Swagger UI.

- **Swagger UI**: Visit `/api/docs` (e.g., `http://localhost:8080/api/docs`)
- **OpenAPI Spec**: Available at `/api/v1/openapi.json` or `/api/v1/openapi.yaml`

### Generating Documentation

If you modify the API handlers, you need to regenerate the Swagger documentation:

1. Install `swag` CLI:
   ```bash
   go install github.com/swaggo/swag/cmd/swag@latest
   ```
2. Run generator from `server` directory:
   ```bash
   cd server
   swag init -g cmd/server/main.go --parseDependency --parseInternal
   ```

> [!NOTE]
> The Docker build process automatically runs the Swagger generation step, ensuring documentation is always up-to-date in production builds.

---

## Web Interface

The project includes a modern web interface built with Nuxt 3.

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
