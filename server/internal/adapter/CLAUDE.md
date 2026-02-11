# Adapter Layer

The adapter layer is responsible for translating data between the application's internal layers (domain and use case) and the outside world. It follows the **Clean Architecture** pattern by implementing the "Port" interfaces defined in the domain layer.

## Subdirectories

### [http/](http/)

- **Purpose**: Handles HTTP/REST communication using the Fiber framework.
- **Key Components**:
  - **Handlers**: One handler per domain area — `auth_handler.go`, `oauth_handler.go`, `user_handler.go`, `system_handler.go`, `calendar_handler.go`, `event_handler.go`, `addressbook_handler.go`, `contact_handler.go`, `calendar_share_handler.go`, `addressbook_share_handler.go`, `calendar_public_handler.go`, `public_calendar_handler.go`, `app_password_handler.go`, `caldav_credential_handler.go`, `carddav_credential_handler.go`, `import_handler.go`, `backup_handler.go`, `docs_handler.go`, `health.go`.
  - **DTOs** (`dto/`): Data Transfer Objects for auth, user, contact, addressbook, event, and credentials.
  - **Middleware**: `auth_middleware.go` (JWT verification), `rate_limiter.go`.
  - **Responses**: `response.go` — `SuccessResponse()` wraps most responses in `{ "status": "ok", "data": ... }`. **Exception**: AddressBook and Contact handlers return raw JSON.
  - **SAML** (`auth/saml_handler.go`): SAML SSO callback handler.
  - **Swagger**: `swagger_types.go` for API documentation type definitions.

### [repository/](repository/)

- **Purpose**: Implements persistence logic for domain entities using GORM.
- **Key Components**:
  - `user_repo.go` — User persistence.
  - `refresh_token_repo.go` — Refresh token storage.
  - `password_reset_repo.go` — Password reset token storage.
  - `calendar_repo.go` — Calendar persistence.
  - `addressbook_repository.go` — AddressBook and contact persistence (with pagination and search).
  - `app_password_repo.go` — App password storage.
  - `caldav_credential_repo.go`, `carddav_credential_repo.go` — DAV credential storage.
  - `calendar_share_repo.go`, `addressbook_share_repo.go` — Sharing persistence.
  - `oauth_connection_repo.go` — OAuth provider link storage.
  - `saml_session_repo.go` — SAML session storage.
  - `system_setting_repo.go` — System settings persistence.

### [auth/](auth/)

- **Purpose**: Implements authentication-related services.
- **Key Components**:
  - `jwt.go` — JWT token generation and validation.
  - `basic_auth.go` — HTTP Basic Auth for CalDAV/CardDAV client access (app passwords and DAV credentials).
  - `oauth.go` — OIDC/OAuth2 provider management using `go-oidc` and `golang.org/x/oauth2`.
  - `saml.go` — SAML 2.0 service provider implementation.

### [middleware/](middleware/)

- **Purpose**: Reusable HTTP middleware.
- **Key Components**:
  - `cors.go` — CORS configuration.
  - `rate_limit.go` — Rate limiting.
  - `security_headers.go` — Security headers (HSTS, CSP, etc.).

### [webdav/](webdav/)

- **Purpose**: Implements the CalDAV (RFC 4791) and CardDAV (RFC 6352) protocol backends.
- **Key Components**:
  - `handler.go` — WebDAV request dispatcher.
  - `context.go` — WebDAV request context.
  - `caldav_backend.go` — CalDAV protocol operations (calendars, events, iCalendar parsing).
  - `carddav_backend.go` — CardDAV protocol operations (address books, contacts, vCard parsing).
  - `sync.go`, `sync_elements.go`, `sync_addressbook.go` — WebDAV-Sync (RFC 6578) for efficient incremental sync.

## Design Philosophy

Adapters are where the specific technical choices (like using JSON for API or GORM for DB) are confined. By isolating these choices here, the core business logic remains portable and easy to test. This layer depends on `domain` and `usecase`.
