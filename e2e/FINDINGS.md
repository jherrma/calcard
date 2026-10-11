# E2E findings

Server bugs found by the e2e suite against master `9334b84` on 2026-10-11. Each
is marked `t_known F<nn>` in its test: the test asserts the CORRECT behaviour and
reports KFAIL, so the bug shows in every run without failing the exit code
(`--strict` turns KFAIL into a failure). When a bug is fixed its test reports
XPASS — remove the `t_known` marker then.

The server was not changed to fix any of these.

| ID  | Severity | Summary |
| --- | -------- | ------- |
| F01 | Critical | Sharee sees the owner's bcrypt hash and e-mail |
| F02 | High     | DAV login with the server-issued username fails |
| F03 | High     | MKCOL/MKCALENDAR on an existing path creates a duplicate |
| F18 | High     | Account deletion leaves shares and public links alive |
| F04–F05, F13–F17, F19 | Medium | see below |
| F06–F12 | Low | see below |

## Critical / High

### F01 — Critical: password hash leak via shared address books
- **Symptom:** when A shares an address book with B, B's `GET /api/v1/addressbooks`
  and `GET /api/v1/search` include `"User":{"Email":…,"Username":…,"PasswordHash":"$2a$12$…"}`.
- **Cause:** `adapter/repository/addressbook_share_repo.go` does
  `Preload("AddressBook.User")`; `usecase/addressbook/list.go` embeds
  `*AddressBook`; `domain/addressbook/addressbook.go` `User` and
  `domain/user/user.go` `PasswordHash` have no `json:"-"`. Calendars are safe
  because their `Owner` is `json:"-"`.
- Related: the address-book JSON also exposes `UserID`, `DeletedAt` and an empty
  `User` object to its owner.

### F02 — High: username login/share lookup is case-broken
- **Cause:** usernames are random mixed-case (`usecase/auth/username_util.go`),
  but `user_repo.go` `GetByUsername` lower-cases before an exact `=` match.
- **Symptom:** Basic auth with `credentials.username` from `POST /app-passwords`
  gets 401 (only the e-mail works); sharing with `user_identifier=<username>`
  gets 400 "user not found". `carddav_credential_repo.go` lower-cases too, so a
  mixed-case CardDAV credential is created (201) but every login gets 401.

### F03 — High: MKCOL/MKCALENDAR on an existing collection
- **Symptom:** returns 201 (should be 405 per RFC 4918 §9.3.1 / RFC 4791
  `resource-must-be-null`); the REST list then shows 2–3 collections at the same path.
- **Cause:** no existence check in `CreateCalendar`/`CreateAddressBook`, no unique
  `(user_id, path)` constraint.

### F18 — High: account deletion leaves data reachable
- **Symptom:** `DELETE /users/me` only soft-deletes the user row and removes its
  tokens. Afterwards a sharee still lists and reads the calendar (owner shown
  empty) and `/public/calendar/<token>` still returns 200.

## Medium

- **F04:** REST-created/updated events and DAV PUTs without DTSTAMP are stored with
  `DTSTAMP;TZID=Local:…`; RFC 5545 §3.8.7.2 requires UTC. Cause:
  `SetDateTime(..., time.Now())` in `event/create.go`, `event/update.go`,
  `caldav_backend.go`. Fix: `time.Now().UTC()`.
- **F05:** not-found returns 500: `DELETE` of a missing contact (`{"error":"contact not
  found"}`), photo upload to a missing contact, `PATCH` of another user's address
  book; `DELETE`/export of it return 400. `GET` correctly returns 404.
- **F13:** `POST /events {"summary":"x"}` with no start/end returns 201 and stores
  `0001-01-01T00:00:00Z`. The DTO has `validate:"required"` but nothing runs the validator.
- **F14:** importing a master event plus a `RECURRENCE-ID` override returns
  `{"total":2,"imported":1,"skipped":1}`; the override is lost. A DAV PUT of the
  same data keeps it. `calendar_import.go` handles components individually
  instead of grouping by UID.
- **F15:** import re-wraps each VEVENT alone, so objects keep
  `DTSTART;TZID=Europe/Berlin` but lose the VTIMEZONE (RFC 5545 §3.2.19).
- **F16:** DAV PUT of a VCALENDAR with no components returns 500 "ical: failed to
  encode VCALENDAR: calendar is empty"; should be 4xx.
- **F17:** sync-collection REPORT on a missing/foreign collection returns 500 with
  body "404 Not Found": `handleSyncReport`/`handleAddressBookSyncReport` hand
  go-webdav's `HTTPError` to Fiber as a plain error.
- **F19:** after account deletion, registering the same e-mail always returns 400
  "registration failed" because the soft-deleted row keeps the unique e-mail.
- **F06:** PROPFIND with no body on `/dav/` and the principal returns 400
  "webdav: expected application/xml request"; RFC 4918 §9.1 says treat as allprop
  (collections already do).

## Low

- **F07:** collection properties vary with PROPFIND Depth: Depth 0 lacks
  `calendar-description` and `supported-calendar-component-set`; Depth 1 lacks
  `sync-token` and `getctag`. `calendar-color` can be set via PROPPATCH but is
  never returned.
- **F08:** PROPPATCH is not atomic (RFC 4918 §9.2): `displayname` plus an unsupported
  property applies the displayname (200 + 403) instead of 424 and no change.
- **F09:** `GET/PATCH/DELETE /calendars/{cal}/events/{id}` ignore `{cal}` (works
  under another calendar's UUID or an all-zero UUID). Permissions still hold.
- **F10:** password change with new == current returns 401; should be 400
  (`user_handler.go` maps `ErrSamePassword` to 401).
- **F11:** PROPFIND on another user's calendar/address-book home returns 207 with an
  empty `<multistatus/>` instead of 403/404. No data leaks.
- **F12:** calendar `PATCH`/`DELETE` on an unknown UUID return 400 "calendar not
  found", import into a foreign calendar returns 400 "access denied", while `GET`
  returns 404 (`calendar_handler.go` maps every error to 400).

## Observations without a failing test

- DAV Basic auth bcrypt-checks every one of the user's app passwords (~250 ms each):
  login cost grows with their number, and anyone knowing an e-mail can trigger it.
- Handler doc comments say `PUT` for events/contacts (route is `PATCH`) and
  `/api/v1/public/calendar` (route is `/public/calendar`).
- `CONFIGURATION.md` doesn't document `CALDAV_REGISTRATION_DISABLED`, the
  `CALDAV_SMTP_*` keys or the auth rate-limit keys.
- App passwords accept an empty name and any scope string; DAV credentials accept
  an expiry date in the past.
