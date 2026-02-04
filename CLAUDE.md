# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a CalDAV/CardDAV server project with a Go backend and web interface. The server implements RFC 4791 (CalDAV) and RFC 6352 (CardDAV) protocols for calendar and contact synchronization.

## Project Structure

```
/
├── Overview.md              # High-level project goals and features
├── Technical Overview.md    # Detailed technical architecture
├── Acceptance Criteria.md   # Full list of acceptance criteria
├── stories/                  # User stories for implementation
│   ├── backend/              # Backend stories (Go server)
│   └── frontend/             # Frontend stories (web interface)
├── server/                   # Go backend implementation
│   ├── cmd/server/           # Application entrypoint
│   ├── configs/              # Configuration examples
│   ├── internal/             # Internal packages
│   │   ├── adapter/          # HTTP handlers, repositories, WebDAV
│   │   ├── domain/           # Domain models and interfaces
│   │   ├── infrastructure/   # Database, email, server setup
│   │   └── usecase/          # Business logic use cases
│   ├── Dockerfile            # Docker build configuration
│   └── docker-compose*.yml   # Docker Compose configurations
└── webinterface/             # Frontend (web interface)
```

## AGENT.md Files

`AGENT.md` files are placed throughout the project to provide context-specific guidance:

- `/AGENT.md` - Root level project overview
- `/server/internal/AGENT.md` - Backend architecture overview
- `/server/internal/adapter/AGENT.md` - Adapter layer details
- `/server/internal/domain/AGENT.md` - Domain layer details
- `/server/internal/infrastructure/AGENT.md` - Infrastructure layer details
- `/server/internal/usecase/AGENT.md` - Use case layer details
- `/server/internal/usecase/auth/AGENT.md` - Authentication use cases

**Always check relevant AGENT.md files when working in a specific area of the codebase.**

## Development Commands

```bash
# Build the server
cd server && go build ./...

# Run tests
cd server && go test ./...

# Run with Docker (SQLite)
cd server && docker-compose up

# Run with Docker (PostgreSQL)
cd server && docker-compose -f docker-compose.postgres.yml up
```

## Key Technologies

- **Backend**: Go 1.22+, Fiber v3, GORM
- **Database**: SQLite (default), PostgreSQL (production)
- **Protocols**: CalDAV (RFC 4791), CardDAV (RFC 6352), WebDAV-Sync
- **Auth**: JWT, OAuth2/OIDC, SAML 2.0
