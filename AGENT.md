# AGENT.md

This file provides guidance to AI coding assistants when working with this repository.

## Project Overview

This is a **CalDAV/CardDAV server** implementing RFC 4791 (CalDAV) and RFC 6352 (CardDAV) for calendar and contact synchronization. The project includes a Go backend server and a web interface frontend.

## Project Structure

```
/
├── Overview.md              # High-level project goals and features
├── Technical Overview.md    # Detailed technical architecture
├── Acceptance Criteria.md   # Full list of acceptance criteria
├── Stories/                  # User stories for implementation
│   ├── backend/              # Backend stories (Go server)
│   └── frontend/             # Frontend stories (web interface)
├── server/                   # Go backend implementation
│   ├── cmd/server/           # Application entrypoint
│   ├── configs/              # Configuration examples
│   └── internal/             # Internal packages (adapters, domain, infrastructure, usecases)
└── webinterface/             # Frontend (web interface)
```

## Context Files

`AGENT.md` files are placed throughout the project to provide context-specific guidance. Check them when working in specific areas:

- `/server/internal/AGENT.md` - Backend architecture
- `/server/internal/adapter/AGENT.md` - Adapter layer (HTTP, repositories, WebDAV)
- `/server/internal/domain/AGENT.md` - Domain models and interfaces
- `/server/internal/infrastructure/AGENT.md` - Infrastructure (database, email, server)
- `/server/internal/usecase/AGENT.md` - Business logic use cases

## Development Instructions

1. **Follow the stories**: Implementation details are in `stories/backend/` and `stories/frontend/`.
2. **Propose alternatives**: If you encounter issues or have better solutions than proposed in the story, propose a new implementation approach.
3. **Write tests**: Always write unit and integration tests to verify your implementation works correctly.
4. **Check existing patterns**: Review existing code in similar areas before implementing new features.
