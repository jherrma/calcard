# Domain Layer

This is the innermost layer of the application. It contains the core business logic and state of the application, completely independent of any external frameworks, databases, or UI.

## Responsibility

The domain layer defines:

- **Entities**: Business models representing the core concepts.
- **Interfaces**: Crucial abstractions for data persistence and external services.
- **Domain Logic**: Business rules that are intrinsic to the data itself (e.g., password hashing methods on the `User` model).

## Subdirectories

### [user/](user/)

- `user.go` — Core user entity (profile data, security status, password hashing). `TokensValidAfter` (`json:"-"`, nil = never revoked) invalidates every access token issued before it; `AcceptsAccessTokenIssuedAt` is the single rule (a token issued at exactly the marker is accepted, so the replacement token change-password mints right after setting it survives). `DeletedIdentityPrefix` (`deleted:`) builds the tombstone a deleted account's e-mail and username are replaced with.
- `refresh_token.go` — Opaque tokens for session persistence, linked to users and client context (User Agent, IP).
- `email_verification.go` — Email verification token model.
- `app_password.go` — Application-specific passwords for DAV client access. The only scopes are `ScopeCalDAV` / `ScopeCardDAV` (`AppPasswordScopes()`, also the default for a create without scopes).
- `caldav_credential.go` — CalDAV-specific access credentials.
- `carddav_credential.go` — CardDAV-specific access credentials.
- `mcp_token.go` — Long-lived bearer credential for the `/mcp` endpoint (story 104). Unlike the DAV credentials it stores a **unique-indexed SHA-256** of a 256-bit random secret rather than a bcrypt hash: an MCP client presents only the opaque token, so it must be findable from that string alone, which a per-row salted hash cannot do. `TokenPrefix` is the only cleartext fragment, kept so a shown-once token stays recognizable in the settings list.
- `validation.go` — User input validation logic, incl. `ValidatePassword` — the policy for a NEW account password: 8 to `MaxPasswordBytes` (72) **bytes**, an upper-case letter, a lower-case letter, a digit and a punctuation/symbol character, by Unicode class. The maximum is bcrypt's (x/crypto refuses longer input with `bcrypt.ErrPasswordTooLong` rather than truncating), so every site that hashes a password a person chose must refuse it first with `ErrPasswordTooLong`, the DAV credentials included; login never checks it. The SPA mirrors the rules in `webinterface/utils/passwordPolicy.ts`. Also `NormalizeCredentialName` (trimmed, 1–100 characters) shared by app passwords, DAV credentials and MCP tokens, and `ValidateDAVCredentialUsername` (3–50 lowercase letters `a`–`z`, nothing else; checked when a CalDAV/CardDAV credential is created; the CalDAV credential lookup relies on it and lower-cases its input).
- `repository.go` — Repository interfaces for user, refresh token, email verification, app password, OAuth connection, MCP token, and credential persistence.

### [calendar/](calendar/)

- `calendar.go` — Calendar entity (name, color, description, public sharing token). Carries `Subscribed` (story 100) and `EffectivePermission`, which caps a subscribed calendar at read-only. That helper is the ONE place the "a subscription is read-only" rule is expressed; REST, CalDAV and MCP all funnel their permission resolution through it, so a write path that already refuses `PermissionRead` needs no new code and cannot be forgotten. It caps object access only — ownership of the collection (rename, recolour, share, delete) is unaffected, and those sites compare `cal.UserID` directly. `UserID` (the owner's primary key) is `json:"-"` (P03): responses expose the owner only through the list's `owner` block (UUID + display name), never as a numeric id.
- `calendar_object.go` — CalDAV object (iCalendar data, ETag).
- `vtimezone.go` — `SyncVTimezones` (one VTIMEZONE per TZID an object references, unused ones dropped, a client's own definition kept) and `GenerateVTimezone`, which builds one from Go's tz data: a fixed window from 1970 (earlier for older events) to 2100, yearly transitions compressed into `RRULE`s such as `BYMONTH=3;BYDAY=-1SU`, open-ended past 2100. The window is fixed so every object of a zone carries the identical block (the calendar export keeps one per TZID). `LoadEventLocation` refuses `Local`.
- `event.go` — Event entity (title, dates, recurrence, attendees).
- `subscription.go` — `CalendarSubscription` (story 100): the remote iCalendar feed behind a subscribed calendar, plus the status derivation, the allowed refresh-interval set, and the success/failure state machine (`RecordSuccess` / `RecordFailure`, exponential backoff anchored to the subscription's own interval, capped at 24h, auto-disable after N failures). A sidecar to `Calendar` rather than columns on it: every field here is meaningless for an ordinary calendar.
- `sync_changelog.go` — WebDAV-Sync change tracking.
- `validation.go` — Calendar/event validation.
- `errors.go` — `ErrNotFound`: a missing calendar and one the caller may not access are the same error (and the same 404), so no endpoint leaks existence (F12). `ErrInvalidInput` / `Invalidf` mark a request the caller can correct (400, message kept verbatim); every other calendar error is a server failure (500, F28). `validation.go` returns `Invalidf` errors.
- `repository.go` — Repository interfaces for calendars, events, sync, and calendar subscriptions. `CalendarRepository.ReplaceFeedObjects` is the transactional feed diff the subscription sync uses — and the only writer permitted on a subscribed calendar.

### [addressbook/](addressbook/)

- `addressbook.go` — AddressBook entity. Serialised raw (PascalCase) by the REST handlers, so `UserID`, `DeletedAt` and the preloaded owner row `User` are all `json:"-"` (P03 / F01) — a sharee must learn neither the owner's numeric id nor their account.
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
