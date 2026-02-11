# Internal Server Architecture

This directory contains the core business logic and implementation details of the CalDAV/CardDAV server, following a **Clean Architecture** (Hexagonal Architecture) pattern.

## Directory Structure

### [domain/](domain/)

- **Purpose**: Contains the core business entities and the logic that is intrinsic to the domain.
- **Contents**:
  - `user/` — User, RefreshToken, EmailVerification, AppPassword, CalDAV/CardDAV credentials, repository interfaces.
  - `calendar/` — Calendar, CalendarObject, Event, SyncChangelog, repository interfaces, validation.
  - `addressbook/` — AddressBook, AddressObject, Photo, SyncChangelog, repository interfaces.
  - `contact/` — Contact domain model.
  - `sharing/` — CalendarShare, AddressBookShare, sharing repository interfaces.
  - `system_setting.go`, `repository_system.go` — System-level settings and repository.
- **Dependencies**: None. This is the heart of the application.

### [usecase/](usecase/)

- **Purpose**: Implements the application-specific business rules.
- **Subdirectories**:
  - `auth/` — Login, register, verify, refresh, logout, password management, OAuth (initiate/callback/link/unlink), SAML login/metadata.
  - `calendar/` — CRUD, public sharing (enable, get status, regenerate token), export.
  - `event/` — CRUD, move between calendars.
  - `addressbook/` — CRUD, create contact, export.
  - `contact/` — CRUD, search, move, photo handling, DTO mapping.
  - `apppassword/` — CRUD, CalDAV/CardDAV credential management.
  - `user/` — Get/update profile, delete account.
  - `sharing/` — Calendar and address book share create/list/update/revoke.
  - `importexport/` — Calendar import, contact import, backup export.
- **Dependencies**: Only depends on `domain`.

### [adapter/](adapter/)

- **Purpose**: Translates data between the internal layers and the external world.
- **Contents**:
  - `http/` — REST handlers (auth, user, system, calendar, event, addressbook, contact, sharing, app password, credentials, import/export, docs, health), DTOs, middleware (auth, rate limiter), SAML handler.
  - `repository/` — GORM implementations for all domain repository interfaces (~16 repos).
  - `auth/` — JWT, Basic Auth (for DAV clients), OAuth (OIDC), SAML.
  - `middleware/` — CORS, rate limiting, security headers.
  - `webdav/` — CalDAV/CardDAV protocol backends, WebDAV-Sync, handler dispatcher.
- **Dependencies**: Depends on `domain` and `usecase`.

### [infrastructure/](infrastructure/)

- **Purpose**: Contains the purely technical implementations and external libraries.
- **Contents**:
  - `database/` — SQLite and PostgreSQL drivers, connection management, GORM auto-migrations.
  - `server/` — Fiber server setup, route registration, global middleware configuration.
  - `email/` — SMTP email sender.
  - `logging/` — Security event logger.
- **Dependencies**: Can depend on any other layer, but mostly provides implementation details for `adapter`.

### [config/](config/)

- **Purpose**: Manages application configuration.
- **Contents**:
  - Loading settings from environment variables and YAML files.
  - Default configuration values.
  - Configuration validation.
- **Dependencies**: None.

## Request Flow

### REST API

1. **Infrastructure**: Server (Fiber) accepts a request and passes it through middleware to an **HTTP Adapter** (Handler).
2. **Adapter (HTTP)**: Validates input, converts it to a **Usecase** request object.
3. **Usecase**: Executes business logic, interacts with **Domain** entities and calls **Repository** interfaces.
4. **Adapter (Repository)**: The concrete implementation of the repository interface handles the actual database interaction.
5. **Usecase**: Returns a result back to the **Adapter (HTTP)**.
6. **Adapter (HTTP)**: Formats the result into an HTTP response (JSON). Most responses are wrapped in `{ "status": "ok", "data": ... }` via `SuccessResponse()`, except AddressBook/Contact endpoints which return raw JSON.

### CalDAV/CardDAV Protocol

1. DAV client sends a PROPFIND/REPORT/PUT/DELETE request.
2. **Adapter (auth/basic_auth)**: Validates Basic Auth credentials (app password or CalDAV/CardDAV credential).
3. **Adapter (webdav/handler)**: Routes to CalDAV or CardDAV backend.
4. **Adapter (webdav/caldav_backend or carddav_backend)**: Translates WebDAV operations to domain/repository calls.
5. Response is formatted as WebDAV XML.
