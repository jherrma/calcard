# Usecase Layer

This directory contains the application-specific business rules. It orchestrates the flow of data to and from the domain entities, and directs those domain entities to use their critical business rules to achieve the goals of the use case.

## Responsibility

The usecase layer is responsible for:

- Implementing the user stories (e.g., "User Login", "Account Registration").
- Coordinating interactions between domain models and data access (repositories).
- Validation of business constraints that cross multiple entities.
- Ensuring the data is in the correct format for the domain layer.

## Subdirectories

### [auth/](auth/)

Authentication and authorization logic. See [auth/AGENT.md](auth/AGENT.md) for details.

- `login.go`, `register.go`, `verify.go`, `refresh.go`, `logout.go` — Standard email/password auth flows.
- `change_password.go`, `forgot_password.go`, `reset_password.go` — Password management.
- `oauth_initiate.go`, `oauth_callback.go`, `oauth_link.go`, `oauth_providers.go` — OAuth2/OIDC flows.
- `saml_login.go`, `saml_metadata.go` — SAML 2.0 SSO.
- `email_service.go` — Email service interface for auth-related emails.
- `username_util.go` — Username generation utilities.

### [calendar/](calendar/)

Calendar management:

- `create.go`, `get.go`, `list.go`, `update.go`, `delete.go` — CRUD operations.
- `enable_public.go`, `get_public_status.go`, `regenerate_token.go` — Public calendar sharing.
- `export.go` — iCalendar export.

### [event/](event/)

Event management:

- `create.go`, `get.go`, `list.go`, `update.go`, `delete.go` — CRUD operations.
- `move.go` — Move event between calendars.

### [addressbook/](addressbook/)

Address book management:

- `create.go`, `get.go`, `list.go`, `update.go`, `delete.go` — CRUD operations.
- `create_contact.go` — Create contact within an address book.
- `export.go` — vCard export.

### [contact/](contact/)

Contact management:

- `create.go`, `get.go`, `list.go`, `update.go`, `delete.go` — CRUD operations.
- `search.go` — Full-text contact search.
- `move.go` — Move contact between address books.
- `photo.go` — Contact photo handling.
- `mapper.go` — Contact-to-DTO mapping utilities.

### [apppassword/](apppassword/)

Application password management (for DAV client access):

- `create.go`, `list.go`, `revoke.go` — App password CRUD.
- `caldav_credential.go`, `carddav_credential.go` — CalDAV/CardDAV-specific credential management.

### [user/](user/)

User profile management:

- `get_profile.go`, `update_profile.go` — Profile CRUD.
- `delete_account.go` — Account deletion.

### [sharing/](sharing/)

Calendar and address book sharing:

- `create_calendar_share.go`, `list_calendar_shares.go`, `update_calendar_share.go`, `revoke_calendar_share.go` — Calendar sharing CRUD.
- `create_addressbook_share.go`, `list_addressbook_shares.go`, `update_addressbook_share.go`, `revoke_addressbook_share.go` — Address book sharing CRUD.

### [importexport/](importexport/)

Data import and export:

- `calendar_import.go` — Import iCalendar (.ics) files.
- `contact_import.go` — Import vCard (.vcf) files.
- `backup_export.go` — Full user data backup export.
- `types.go` — Import/export type definitions.

## Key Principles

1. **No External Dependencies**: This layer remains ignorant of whether it's being called by an HTTP request, a CLI command, or a background worker.
2. **Interface Driven**: It interacts with the outside world (database, email, tokens) exclusively via interfaces defined in the `domain` layer.
3. **Pure Logic**: It should ideally contain no technical "leaks" from infrastructure frameworks like GORM or Fiber.
