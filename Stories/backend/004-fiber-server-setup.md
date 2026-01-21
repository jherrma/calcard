# Story 004: Fiber v3 HTTP Server Setup

## Title
Initialize Fiber v3 Web Server with Health Endpoint

## Description
As a developer, I want to set up the Fiber v3 HTTP server with basic middleware and a health endpoint, so that we have a foundation for all HTTP-based functionality.

## Acceptance Criteria

- [ ] Fiber v3 server initialized with production-ready defaults
- [ ] Server listens on configured host:port from Story 002
- [ ] Middleware configured:
  - [ ] Request ID generation
  - [ ] Structured logging (request method, path, status, duration)
  - [ ] Recovery from panics
  - [ ] CORS (configurable origins)
  - [ ] Request timeout (30s default)
- [ ] Health endpoints implemented:
  - [ ] `GET /health` - Simple liveness probe (returns 200)
  - [ ] `GET /health/ready` - Readiness probe (checks database connection)
- [ ] Graceful shutdown on SIGINT/SIGTERM:
  - [ ] Stops accepting new connections
  - [ ] Waits for in-flight requests (max 30s)
  - [ ] Closes database connection
  - [ ] Exits cleanly
- [ ] Server starts and logs startup message with version and address

## Technical Notes

Dependencies:
```go
github.com/gofiber/fiber/v3
github.com/gofiber/fiber/v3/middleware/requestid
github.com/gofiber/fiber/v3/middleware/recover
github.com/gofiber/fiber/v3/middleware/cors
github.com/gofiber/fiber/v3/middleware/logger
```

Fiber v3 configuration:
```go
app := fiber.New(fiber.Config{
    AppName:               "CalDAV Server",
    DisableStartupMessage: false,
    EnablePrintRoutes:     false,
    ReadTimeout:           30 * time.Second,
    WriteTimeout:          30 * time.Second,
    IdleTimeout:           120 * time.Second,
    ErrorHandler:          customErrorHandler,
})
```

## Health Response Format

```json
// GET /health
{
  "status": "ok"
}

// GET /health/ready
{
  "status": "ok",
  "checks": {
    "database": "ok"
  }
}

// GET /health/ready (when database is down)
{
  "status": "degraded",
  "checks": {
    "database": "failed"
  }
}
```

## Code Structure

```
internal/infrastructure/server/
├── server.go        # Server initialization and lifecycle
├── middleware.go    # Custom middleware setup
└── routes.go        # Route registration

internal/adapter/http/
├── health.go        # Health check handlers
└── response.go      # Common response helpers
```

## Definition of Done

- [ ] `go run ./cmd/server` starts server on configured port
- [ ] `curl localhost:8080/health` returns 200 with JSON
- [ ] `curl localhost:8080/health/ready` checks database and returns status
- [ ] Sending SIGTERM stops server gracefully
- [ ] Panic in handler returns 500, doesn't crash server
- [ ] Request logs show method, path, status, and duration
