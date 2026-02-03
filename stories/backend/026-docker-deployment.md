# Story 026: Docker Deployment

## Title
Implement Docker and Docker Compose Deployment

## Description
As a system administrator, I want to deploy the CalDAV/CardDAV server using Docker so that I can easily run and manage the service in any environment.

## Related Acceptance Criteria

| ID | Criterion |
|----|-----------|
| OP-8.1.1 | Server runs via docker-compose with single command |
| OP-8.1.2 | Database is automatically initialized on first run |
| OP-8.1.3 | Configuration is provided via environment variables |
| OP-8.1.4 | Configuration can be provided via config file |
| OP-8.1.5 | Data persists across container restarts |
| OP-8.1.6 | Health check endpoint is available |
| OP-8.2.1 | Server works with PostgreSQL |
| OP-8.2.2 | Server works with SQLite |
| OP-8.2.3 | Database migrations run automatically |

## Acceptance Criteria

### Dockerfile

- [ ] Multi-stage build for minimal image size
- [ ] Build stage: Go compilation with CGO for SQLite
- [ ] Runtime stage: Alpine-based or distroless
- [ ] Non-root user for security
- [ ] Expose port 8080 (configurable)
- [ ] Health check instruction included
- [ ] Labels for metadata (version, maintainer, etc.)

### Docker Compose - SQLite (Default)

- [ ] Single `docker-compose up` starts the server
- [ ] SQLite database stored in named volume
- [ ] Data persists across container restarts
- [ ] No external database dependency

### Docker Compose - PostgreSQL

- [ ] Optional PostgreSQL service
- [ ] Separate compose file or profile for PostgreSQL
- [ ] Database credentials via environment variables
- [ ] PostgreSQL data in named volume
- [ ] Wait for PostgreSQL to be ready before starting server

### Environment Variable Configuration

- [ ] All configuration via environment variables
- [ ] Sensible defaults for development
- [ ] Required variables validated on startup
- [ ] Example `.env` file provided

### Config File Support

- [ ] Optional `config.yaml` file mount
- [ ] Environment variables override config file
- [ ] Config file location configurable via `CALDAV_CONFIG_FILE`

### Automatic Initialization

- [ ] Database migrations run on first startup
- [ ] No manual intervention required
- [ ] Idempotent (safe to run multiple times)
- [ ] Migration status logged

### Health Checks

- [ ] `/health` endpoint for liveness probe
- [ ] `/health/ready` endpoint for readiness probe
- [ ] Docker health check uses these endpoints
- [ ] Kubernetes-compatible probe format

## Technical Notes

### Dockerfile
```dockerfile
# Build stage
FROM golang:1.22-alpine AS builder

RUN apk add --no-cache gcc musl-dev sqlite-dev

WORKDIR /app

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Build
COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -a -ldflags '-linkmode external -extldflags "-static"' -o server ./cmd/server

# Runtime stage
FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1000 caldav && \
    adduser -u 1000 -G caldav -s /bin/sh -D caldav

WORKDIR /app

# Copy binary
COPY --from=builder /app/server .

# Create data directory
RUN mkdir -p /data && chown caldav:caldav /data

USER caldav

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

ENTRYPOINT ["./server"]
```

### docker-compose.yml (SQLite - Default)
```yaml
version: '3.8'

services:
  caldav:
    build: .
    # Or use pre-built image:
    # image: ghcr.io/yourorg/caldav-server:latest
    ports:
      - "8080:8080"
    environment:
      - CALDAV_SERVER_HOST=0.0.0.0
      - CALDAV_SERVER_PORT=8080
      - CALDAV_DATA_DIR=/data
      - CALDAV_JWT_SECRET=${CALDAV_JWT_SECRET:-change-me-in-production}
      - CALDAV_BASE_URL=${CALDAV_BASE_URL:-http://localhost:8080}
    volumes:
      - caldav_data:/data
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "wget", "--no-verbose", "--tries=1", "--spider", "http://localhost:8080/health"]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 10s

volumes:
  caldav_data:
```

### docker-compose.postgres.yml
```yaml
version: '3.8'

services:
  caldav:
    build: .
    ports:
      - "8080:8080"
    environment:
      - CALDAV_SERVER_HOST=0.0.0.0
      - CALDAV_SERVER_PORT=8080
      - CALDAV_DB_HOST=postgres
      - CALDAV_DB_PORT=5432
      - CALDAV_DB_USER=caldav
      - CALDAV_DB_PASSWORD=${POSTGRES_PASSWORD:-caldav_secret}
      - CALDAV_DB_NAME=caldav
      - CALDAV_DB_SSLMODE=disable
      - CALDAV_JWT_SECRET=${CALDAV_JWT_SECRET:-change-me-in-production}
      - CALDAV_BASE_URL=${CALDAV_BASE_URL:-http://localhost:8080}
    depends_on:
      postgres:
        condition: service_healthy
    restart: unless-stopped

  postgres:
    image: postgres:16-alpine
    environment:
      - POSTGRES_USER=caldav
      - POSTGRES_PASSWORD=${POSTGRES_PASSWORD:-caldav_secret}
      - POSTGRES_DB=caldav
    volumes:
      - postgres_data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U caldav -d caldav"]
      interval: 10s
      timeout: 5s
      retries: 5
    restart: unless-stopped

volumes:
  postgres_data:
```

### .env.example
```bash
# Server Configuration
CALDAV_SERVER_HOST=0.0.0.0
CALDAV_SERVER_PORT=8080
CALDAV_BASE_URL=https://caldav.example.com

# Security (REQUIRED in production)
CALDAV_JWT_SECRET=your-secure-random-string-at-least-32-chars

# Database (PostgreSQL - optional, defaults to SQLite)
# CALDAV_DB_HOST=postgres
# CALDAV_DB_PORT=5432
# CALDAV_DB_USER=caldav
# CALDAV_DB_PASSWORD=secure-password
# CALDAV_DB_NAME=caldav
# CALDAV_DB_SSLMODE=require

# OAuth (optional)
# CALDAV_OAUTH_GOOGLE_CLIENT_ID=
# CALDAV_OAUTH_GOOGLE_CLIENT_SECRET=
# CALDAV_OAUTH_MICROSOFT_CLIENT_ID=
# CALDAV_OAUTH_MICROSOFT_CLIENT_SECRET=

# SAML (optional)
# CALDAV_SAML_ENABLED=false
# CALDAV_SAML_IDP_METADATA_URL=

# Email (optional - required for password reset)
# CALDAV_SMTP_HOST=smtp.example.com
# CALDAV_SMTP_PORT=587
# CALDAV_SMTP_USER=
# CALDAV_SMTP_PASSWORD=
# CALDAV_SMTP_FROM=noreply@example.com

# Logging
CALDAV_LOG_LEVEL=info
```

### config.yaml.example
```yaml
server:
  host: 0.0.0.0
  port: 8080

base_url: https://caldav.example.com

# Database configuration
# Omit this section entirely to use SQLite (default)
database:
  host: postgres
  port: 5432
  user: caldav
  password: ${CALDAV_DB_PASSWORD}  # Environment variable substitution
  name: caldav
  ssl_mode: require

jwt:
  secret: ${CALDAV_JWT_SECRET}
  access_expiry: 15m
  refresh_expiry: 168h  # 7 days

oauth:
  google:
    client_id: ${CALDAV_OAUTH_GOOGLE_CLIENT_ID}
    client_secret: ${CALDAV_OAUTH_GOOGLE_CLIENT_SECRET}

logging:
  level: info
  format: json  # or "text"
```

### Startup Validation
```go
func (c *Config) Validate() error {
    var errs []string

    if c.JWT.Secret == "" || c.JWT.Secret == "change-me-in-production" {
        errs = append(errs, "CALDAV_JWT_SECRET must be set to a secure value")
    }
    if len(c.JWT.Secret) < 32 {
        errs = append(errs, "CALDAV_JWT_SECRET must be at least 32 characters")
    }
    if c.BaseURL == "" {
        errs = append(errs, "CALDAV_BASE_URL must be set")
    }

    if len(errs) > 0 {
        return fmt.Errorf("configuration errors:\n  - %s", strings.Join(errs, "\n  - "))
    }
    return nil
}
```

### File Structure
```
/
├── Dockerfile
├── docker-compose.yml              # SQLite (default)
├── docker-compose.postgres.yml     # PostgreSQL
├── .env.example
├── configs/
│   └── config.yaml.example
└── scripts/
    └── docker-entrypoint.sh        # Optional entrypoint script
```

## Deployment Commands

### Quick Start (SQLite)
```bash
# Clone and start
git clone https://github.com/yourorg/caldav-server
cd caldav-server
cp .env.example .env
# Edit .env with your settings
docker-compose up -d
```

### Production (PostgreSQL)
```bash
cp .env.example .env
# Edit .env with production settings
docker-compose -f docker-compose.postgres.yml up -d
```

### View Logs
```bash
docker-compose logs -f caldav
```

### Backup SQLite
```bash
docker-compose exec caldav sqlite3 /data/caldav.db ".backup '/data/backup.db'"
docker cp caldav-caldav-1:/data/backup.db ./backup.db
```

### Backup PostgreSQL
```bash
docker-compose -f docker-compose.postgres.yml exec postgres \
    pg_dump -U caldav caldav > backup.sql
```

## Definition of Done

- [ ] Dockerfile builds successfully with multi-stage build
- [ ] `docker-compose up` starts server with SQLite
- [ ] `docker-compose -f docker-compose.postgres.yml up` starts with PostgreSQL
- [ ] Data persists in named volumes across restarts
- [ ] Environment variables configure all settings
- [ ] Config file optionally overrides defaults
- [ ] Migrations run automatically on startup
- [ ] Health check endpoints work in Docker
- [ ] Non-root user in container
- [ ] Image size under 50MB (excluding data)
- [ ] Documentation for deployment commands
