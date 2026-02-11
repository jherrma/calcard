# Domain Layer

This is the innermost layer of the application. It contains the core business logic and state of the application, completely independent of any external frameworks, databases, or UI.

## Responsibility

The domain layer defines:

- **Entities**: Business models representing the core concepts.
- **Interfaces**: Crucial abstractions for data persistence and external services.
- **Domain Logic**: Business rules that are intrinsic to the data itself (e.g., password hashing methods on the `User` model).

## Subdirectories

### [user/](user/)

- `user.go` — Core user entity (profile data, security status, password hashing).
- `refresh_token.go` — Opaque tokens for session persistence, linked to users and client context (User Agent, IP).
- `email_verification.go` — Email verification token model.
- `app_password.go` — Application-specific passwords for DAV client access.
- `caldav_credential.go` — CalDAV-specific access credentials.
- `carddav_credential.go` — CardDAV-specific access credentials.
- `validation.go` — User input validation logic.
- `repository.go` — Repository interfaces for user, refresh token, email verification, app password, OAuth connection, and credential persistence.

### [calendar/](calendar/)

- `calendar.go` — Calendar entity (name, color, description, public sharing token).
- `calendar_object.go` — CalDAV object (iCalendar data, ETag).
- `event.go` — Event entity (title, dates, recurrence, attendees).
- `sync_changelog.go` — WebDAV-Sync change tracking.
- `validation.go` — Calendar/event validation.
- `repository.go` — Repository interfaces for calendars, events, and sync.

### [addressbook/](addressbook/)

- `addressbook.go` — AddressBook entity.
- `address_object.go` — CardDAV address object (vCard data, ETag).
- `photo.go` — Contact photo model.
- `sync_changelog.go` — WebDAV-Sync change tracking for contacts.
- `repository.go` — Repository interfaces for address books, contacts, and sync.

### [contact/](contact/)

- `contact.go` — Contact domain model (structured name, emails, phones, addresses, URLs, birthday, notes).

### [sharing/](sharing/)

- `calendar_share.go` — Calendar sharing model (user, permission level).
- `addressbook_share.go` — AddressBook sharing model.
- `repository.go` — Repository interfaces for sharing.

### Root Level

- `system_setting.go` — Persistent system configuration (e.g., dynamically generated JWT secret).
- `repository_system.go` — System settings repository interface.

## Design Constraints

- **Zero Dependencies**: This package must not import anything from `usecase`, `adapter`, or `infrastructure`.
- **Pure Go**: Should only depend on the Go standard library (and potentially very minimal utility libraries if absolutely necessary).
- **Stability**: This is the most stable part of the codebase; changes here usually trigger changes in all other layers.
