# Infrastructure Layer

This directory contains the technical implementations and external library integrations. It follows the principles of **Clean Architecture** by providing concrete implementations for the interfaces required by the application's upper layers.

## Subdirectories

### [database/](database/)

- **Purpose**: Manages the persistence layer.
- **Key Components**:
  - `database.go` — Unified database initialization based on configuration (auto-selects SQLite or PostgreSQL).
  - `sqlite.go` — SQLite driver setup using GORM.
  - `postgres.go` — PostgreSQL driver setup using GORM.
  - `migrations.go` — Automatic database schema updates using GORM's `AutoMigrate`. Registers all domain models (User, Calendar, Event, AddressBook, Contact, Sharing, etc.).

### [server/](server/)

- **Purpose**: Manages the HTTP server lifecycle and request pipeline.
- **Key Components**:
  - `server.go` — Configures the Fiber application instance, including custom WebDAV HTTP methods (PROPFIND, PROPPATCH, MKCOL, REPORT, MKCALENDAR, etc.).
  - `routes.go` — Registers all API endpoints and injects handler dependencies. Initializes OAuth/SAML providers. This is the dependency injection root of the application.
  - `middleware.go` — Configures global HTTP middleware (CORS, Recovery, Request ID logging, security headers, rate limiting, TLS).

### [email/](email/)

- **Purpose**: Handles external communication services.
- **Key Components**:
  - `smtp.go` — SMTP email sender implementation for verification emails, password resets, etc. Satisfies the email service interface used by auth use cases. When SMTP is not configured (`cfg.SMTP.Host == ""`), users are auto-activated on registration.

### [logging/](logging/)

- **Purpose**: Security audit logging.
- **Key Components**:
  - `security_logger.go` — Logs security-relevant events (authentication attempts, password changes, etc.).

## Design Philosophy

The infrastructure layer is the "outermost" layer. It depends on internal layers (like `config` or `adapter`) but the internal business logic (in `domain` and `usecase`) never depends on code in this directory. Instead, they use interfaces which are fulfilled by the implementations found here.
