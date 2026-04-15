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

## Wrapped vs. raw JSON

Most REST endpoints return `{ "status": "ok", "data": {...} }` via `SuccessResponse()` (auth, user, system, app-password, health). Calendar, event, address book, contact, and import handlers return raw JSON. Use `doJSON` for the first category and `doJSONRaw` for the second; the helpers are otherwise interchangeable.

## Basic Auth gotcha for DAV

The WebDAV auth handler (`adapter/webdav/handler.go`) only resolves the Basic Auth principal via `userRepo.GetByEmail`. It does **not** try `GetByUsername`. So for DAV requests, the Basic Auth username must be the user's **email**, even though the URL path segment (`/dav/{username}/...`) uses the server-assigned 16-character random username. The helpers keep these straight: `registerAndLoginFull` returns the username (for URL paths), `davCall` receives the email (for Basic Auth).

## Adding new subtests

- Register a fresh user for any test that writes data — share a server, not users. Use `registerAndLoginFull(t, email, password, displayName)`.
- For anything that needs DAV creds, call `createAppPassword(t, token, name)` — it returns `(username, appPassword)`.
- Calendar paths use UUID: `/api/v1/calendars/{uuid}`, and events under `/api/v1/calendars/{calendar_id}/events` use the numeric id. Address book paths use the numeric id throughout.
- `GET /calendars/:id/events` filters on `start_time` / `end_time` — pass an explicit `?start=...&end=...` wide window when you want to list everything.
- The DAV URL for a calendar is `/dav/{username}/calendars/{calendar.Path}/` where `Path` is `{uuid}.ics` for calendars created through the server. For address books `Path` is a standalone UUID. Both come back in the list endpoints.
- Use `t.Run(...)` when you want subtests. State can be shared via a pointer-receiver struct as `flow_test.go` demonstrates.
- Prefer `require` for setup / preconditions (hard stops on failure) and `assert` for end-of-step checks (continues reporting). Follow the pattern in `flow_test.go`.
