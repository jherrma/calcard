# CalDAV/CardDAV Server Backend

This is the backend for the CalCard project, a modern, self-hostable CalDAV/CardDAV server written in Go.

## Setup

1.  Ensure you have Go 1.22+ installed.
2.  Navigate to this directory: `cd server`
3.  Install dependencies: `go mod tidy`
4.  Run the server: `go run ./cmd/server`

## Architecture

The project follows Clean Architecture principles:

- `internal/domain`: Core entities and business rules.
- `internal/usecase`: Application-specific business logic.
- `internal/adapter`: Interface adapters (HTTP handlers, database repositories).
- `internal/infrastructure`: External tools and frameworks (database setup, configuration).
