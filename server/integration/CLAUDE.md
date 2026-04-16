# Integration Tests

End-to-end tests that boot the real HTTP + WebDAV stack in-process and exercise it over a real TCP socket. They complement the per-package handler tests that live alongside the code under `internal/`.

## Running

Integration tests are gated behind the `integration` build tag so `go test ./...` stays fast for day-to-day work.

```bash
# From server/ — default run, integration tests excluded:
go test ./...

# Run the integration suite explicitly:
go test -tags=integration ./integration/... -v

# With the race detector:
go test -tags=integration -race ./integration/... -v
```

Each `go test` invocation boots one server via `TestMain` on a random localhost port, runs every `TestXxx` against it, then gracefully shuts down. The SQLite database lives in `t.TempDir()` so runs are isolated.

## How the server is started

`main_test.go` builds a `config.Config` struct in Go (SQLite, `SMTP.Host == ""` so users auto-activate, `RateLimit.Enabled == false` so tests can log in repeatedly from 127.0.0.1), calls `server.New(cfg, db)` — the same constructor production uses — then `srv.Start("127.0.0.1:0")` binds a real TCP listener on a random port and serves in a goroutine. Every request the tests make goes over that real socket through the production Fiber app, real middleware, real WebDAV method dispatch, and real GORM queries. Nothing is mocked.

`TestMain` also performs the true first-boot assertion (`GET /system/settings` returns `admin_configured: false` on a pristine DB) before any subtest has a chance to register a user.

## Test files

| File | Purpose |
|---|---|
| `main_test.go` | `TestMain`, config assembly, listener lifecycle, first-boot check, `waitForReady`. |
| `client_test.go` | HTTP helpers: `restCall`, `rawCall`, `doJSON` (unwraps `{status, data}` envelope), `doJSONRaw` (raw-JSON endpoints), `davCall` (Basic-Auth WebDAV), `davURL`. |
| `flow_test.go` | `TestUserFlow` — REST journey: admin register, second-user register, login, profile, calendar/event/addressbook/contact CRUD, change password, logout. Uses `t.Run` subtests sharing a `*flowState`. |
| `backup_test.go` | `TestExportImportRoundtrip` — seeds two calendars + two address books with content, downloads the ZIP from `/users/me/export`, deletes the seeded collections, creates fresh ones, re-imports each `.ics` / `.vcf`, and asserts every original UID / summary / FN comes back. Also hosts shared helpers reused by the DAV tests (`registerAndLoginFull`, `createCalendar`, `createAddressBook`, etc.). |
| `caldav_test.go` | `TestCalDAV` — creates an app password via REST for Basic Auth, then PROPFIND principal → calendar home → collection, PUT/GET/PUT(update)/DELETE of a VEVENT, cross-check against REST `/events`, and one `REPORT sync-collection`. |
| `carddav_test.go` | `TestCardDAV` — same shape as `TestCalDAV` but for VCARD under `/dav/{username}/addressbooks/...`. |
| `sharing_test.go` | `TestCalendarSharing` + `TestAddressBookSharing` — owner creates a share, target sees it, permission transitions read↔read-write, revoke. Also spot-checks that an unprivileged user can't rename someone else's calendar before the share is in place. `TestSharedAddressBookRESTVisible` — the sharee's `GET /addressbooks` list carries the shared book with `shared=true` and an owner block (guards against the now-fixed "only owned" regression). `TestSharedCalendarCalDAVVisible` — the sharee can PROPFIND the owner's calendar under their own `/dav/{user}/calendars/…` path, a sync-collection REPORT returns hrefs inside that collection, and a read-write sharee can PUT a new event that the owner then sees through REST. |
| `public_calendar_test.go` | `TestPublicCalendar` — enable public access on a calendar, fetch the token-addressed `GET /public/calendar/:token` (and the `token.ics` variant) anonymously, verify the feed carries the seeded event, verify an unknown token 404s, regenerate the token and confirm the old one stops working, disable access and confirm the new token 404s too. Uses a small `rawGet` helper that doesn't attach the bearer. |
| `dav_queries_test.go` | `TestCalDAVTimeRangeReport` (filters VEVENTs by time-range in a `calendar-query` REPORT), `TestCalDAVSyncTokenProgression` (two successive `sync-collection` REPORTs — the second returns only the delta), `TestCalDAVEtagPreconditions` (If-Match yields 412 on stale ETag and the resource body stays at v2 after the rejected write). |
| `photo_test.go` | `TestContactPhoto` — uploads `Assets/user-icon.jpg` on a contact, fetches it back as JPEG, replaces it with `Assets/user-icon-2.jpg`, deletes it, and confirms the contact's `photo_url` is cleared. The JPEG bytes are loaded from the `Assets/` directory; the server round-trips them through base64 storage. |
| `Assets/` | Binary fixtures — currently two JPEG profile icons used by `photo_test.go`. |
| `authz_test.go` | `TestAuthorizationBoundaries` — User B cannot read/PATCH/DELETE any of User A's calendars, events, address books, or contacts via REST, and cannot PROPFIND User A's DAV home set with their own credentials. Using 404 (not 403) for the rejections is deliberate: it keeps the server from leaking existence across users. |
| `move_test.go` | `TestEventMove` + `TestContactMove` — move an event between calendars and a contact between address books via the dedicated `POST .../move` routes, verifying the UID survives and the source no longer lists the resource while the target does. |
| `malformed_input_test.go` | `TestMalformedInputs` — a table-driven sweep of bad JSON, missing required fields, unknown UUIDs, wrong auth header scheme, and a malformed iCalendar PUT over CalDAV. Every case must yield a 4xx — never a 5xx or a panic. Primarily a regression-guard against unhandled error paths. |
| `app_password_test.go` | `TestAppPasswordRevocation` — creates two app passwords, proves both authenticate DAV, revokes one, and asserts the revoked one is now rejected (401) while the other keeps working. Security-critical: without this, "revoke" is cosmetic. |
| `rate_limit_test.go` | `TestLoginRateLimiter` — spins up a **second** server instance with `RateLimit.Enabled=true` (the package-level server leaves it off so other tests can log in repeatedly from 127.0.0.1) and asserts the login limiter fires within a burst. Also exposes the `bootServerWithConfig(t, tweak)` helper that any future test needing a bespoke server configuration can reuse. |
| `refresh_test.go` | `TestRefreshTokenFlow` — /auth/refresh returns a fresh access token that actually works; invalid / garbage tokens 401; logout revokes the refresh token. Documents the "no rotation" design choice (successive refreshes with the same token are accepted until logout). |
| `dav_credentials_test.go` | `TestDedicatedCalDAVCredential` + `TestDedicatedCardDAVCredential` — covers the dedicated DAV credential flow (CRUD + auth). Read-only scope is enforced (PUT is 403), revocation is effective. Note: credential usernames must be lowercase — `GetByUsername` lowercases lookups but `Create` stores what you give it, so mixed-case would silently fail to authenticate. |
| `account_test.go` | `TestAccountDeletion` — wrong password/confirmation text → 4xx, correct credentials → 204, then old access token and login both 401. |
| `dav_collection_test.go` | `TestDAVMkcolCalendar`, `TestDAVMkcolAddressBook`, `TestDAVMkcalendar`, `TestDAVDeleteCalendar`, `TestDAVDeleteAddressBook` — MKCOL and RFC-4791 MKCALENDAR both create calendars via DAV (what real clients use for "+ new calendar"); MKCOL for address books. DAV DELETE wipes both calendar and addressbook collections from the REST list — the CalDAV path hooks into `CalDAVBackend.DeleteCalendarObject` by detecting a collection-shaped URL (no object segment), since emersion/go-webdav only exposes `DeleteCalendarObject`. MKCALENDAR bodies are stripped before handoff to emersion's MKCOL parser (the two XML root elements are incompatible); clients that want a displayname set it via a follow-up PROPPATCH. |
| `carddav_sync_test.go` | `TestCardDAVSyncTokenProgression`, `TestCardDAVAddressBookQueryReport` — CardDAV analogues of the CalDAV sync/query tests. Incremental sync returns just the delta; addressbook-query honors FN text-match filters and doesn't return unrelated contacts. |
| `calendar_export_test.go` | `TestCalendarExportEndpoint` — GET /calendars/:uuid/export returns a parseable iCalendar feed containing seeded events. `TestDeleteLastCalendarGuard`, `TestDeleteLastAddressBookGuard` — deleting the only remaining calendar / addressbook is rejected with a client error, and the collection survives. |
| `recurrence_test.go` | `TestRecurringEventDeleteThis`, `TestRecurringEventDeleteThisAndFuture`, `TestRecurringEventUpdateThis`, `TestRecurringEventUpdateThisAndFuture` — scope-aware delete and update on recurring events. Delete: `scope=this` adds an EXDATE for the one instance; `scope=this_and_future` truncates the RRULE at the split point via UNTIL. Update: `scope=this` writes an exception VEVENT with matching RECURRENCE-ID (so only that one instance carries the edit); `scope=this_and_future` splits the series — old master gets UNTIL, new master is a copy with the edit applied. The update-split usecase adjusts COUNT on the new master so the total instance count is conserved (original COUNT = pre-split instances + post-split instances). Uses the numeric recurrence-id timestamps returned by the list endpoint, so you pass them back verbatim. |
| `security_headers_test.go` | `TestSecurityHeaders` asserts helmet (X-Frame-Options, X-Content-Type-Options, Referrer-Policy, COEP/COOP, Permissions-Policy) under `Security.Enabled=true`. `TestCORSPreflightAndSimpleRequest` spins a server with `CORS.Enabled=true` and asserts preflight (204 + Access-Control-*) and actual responses carry the configured origin / credentials / expose-headers. Both use `bootServerWithConfig` because the shared test server leaves these features off. |

## Wrapped vs. raw JSON

Most REST endpoints return `{ "status": "ok", "data": {...} }` via `SuccessResponse()` (auth, user, system, app-password, health). Calendar, event, address book, contact, and import handlers return raw JSON. Use `doJSON` for the first category and `doJSONRaw` for the second; the helpers are otherwise interchangeable.

## Basic Auth gotcha for DAV

The WebDAV auth handler (`adapter/webdav/handler.go`) only resolves the Basic Auth principal via `userRepo.GetByEmail`. It does **not** try `GetByUsername`. So for DAV requests, the Basic Auth username must be the user's **email**, even though the URL path segment (`/dav/{username}/...`) uses the server-assigned 16-character random username. The helpers keep these straight: `registerAndLoginFull` returns the username (for URL paths), `davCall` receives the email (for Basic Auth).

## Adding new subtests

- Register a fresh user for any test that writes data — share a server, not users. Use `registerAndLoginFull(t, email, password, displayName)`.
- For anything that needs DAV creds, call `createAppPassword(t, token, name)` — it returns `(username, appPassword)`.
- Calendar paths use UUID: `/api/v1/calendars/{uuid}`, and events under `/api/v1/calendars/{calendar_id}/events` use the numeric id. Address book paths use the numeric id throughout. **Sharing and public-calendar endpoints** (`/calendars/:id/shares`, `/calendars/:id/public`, etc.) use the **numeric** calendar id too, even though the sibling CRUD routes on the same group use the UUID — a known inconsistency baked into `routes.go`.
- `GET /calendars/:id/events` filters on `start_time` / `end_time` — pass an explicit `?start=...&end=...` wide window when you want to list everything.
- The DAV URL for a calendar is `/dav/{username}/calendars/{calendar.Path}/` where `Path` is `{uuid}.ics` for calendars created through the server. For address books `Path` is a standalone UUID. Both come back in the list endpoints.
- Use `t.Run(...)` when you want subtests. State can be shared via a pointer-receiver struct as `flow_test.go` demonstrates.
- Prefer `require` for setup / preconditions (hard stops on failure) and `assert` for end-of-step checks (continues reporting). Follow the pattern in `flow_test.go`.
