# Story 001: Project Initialization

## Title
Initialize Go Project with Module Structure

## Description
As a developer, I want to initialize the Go project with a clean module structure so that the codebase is organized and maintainable from the start.

## Acceptance Criteria

- [ ] Go module initialized with appropriate module path (e.g., `github.com/{org}/caldav-server`)
- [ ] Go version set to 1.22+ in `go.mod`
- [ ] Project directory structure created:
  ```
  caldav-server/
  ├── cmd/
  │   └── server/
  │       └── main.go
  ├── internal/
  │   ├── config/
  │   ├── domain/
  │   ├── usecase/
  │   ├── adapter/
  │   │   ├── http/
  │   │   └── repository/
  │   └── infrastructure/
  │       └── database/
  ├── migrations/
  ├── configs/
  │   └── config.yaml.example
  ├── go.mod
  ├── go.sum
  ├── .gitignore
  └── README.md
  ```
- [ ] `.gitignore` includes common Go ignores, IDE files, `.env`, and build artifacts
- [ ] `main.go` contains minimal bootstrapping code that compiles
- [ ] Project builds successfully with `go build ./...`
- [ ] README.md contains basic project description and setup instructions

## Technical Notes

- Use clean architecture principles (domain, usecase, adapter, infrastructure)
- Keep `cmd/` for entry points only - minimal code
- All business logic goes in `internal/` to prevent external imports

## Definition of Done

- [ ] `go build ./cmd/server` produces a binary
- [ ] `go mod tidy` runs without errors
- [ ] Directory structure matches specification
