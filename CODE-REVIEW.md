# Code Review Findings — 2026-06-12

Scope: full backend (`server/`) plus a focused frontend pass (`webinterface/`). Six parallel reviewers covered auth, the WebDAV protocol layer, REST handlers, usecases/domain, infrastructure/persistence, and the frontend. The highest-impact findings were re-verified directly against the source; two data-layer claims were additionally verified with throwaway GORM tests.

Severity legend: **Critical** = core feature broken or remote code execution; **High** = security flaw or data loss/corruption on a normal path; **Medium** = real bug with bounded impact; **Low** = hardening gap or edge case.

---

## Critical

### C1. Recurring events vanish from the REST event listing after their first occurrence
`server/internal/adapter/repository/calendar_repo.go:183` — `ListEvents` filters with `start_time < ? AND end_time > ?`, but `StartTime`/`EndTime` are denormalized to the **first occurrence only** (`usecase/event/create.go:60`, and the CalDAV PUT path does the same from DTSTART/DTEND). A weekly event created in January is never returned for a June query, so `ListEventsUseCase` never receives the object to expand its RRULE. There is no RRULE-aware handling (e.g., NULL/far-future `end_time` for recurring masters) anywhere in the query path. Recurring events silently disappear from the calendar UI outside their first occurrence's window.

### C2. Imported events are stored in a format no other code path can parse
`server/internal/usecase/importexport/calendar_import.go:133` — import strips the `BEGIN:VCALENDAR` wrapper via `extractComponentBlock` before persisting `ICalData`, but every consumer decodes with `ical.NewDecoder(...).Decode()` (`domain/calendar/event.go:73`, `adapter/webdav/caldav_backend.go:483`), which hard-fails on a non-VCALENDAR top level. Every other write path (REST create, DAV PUT) stores a full VCALENDAR. Result: `GET /events?expand=true` (the handler default) returns 500 for any window containing an imported event, and DAV GET/REPORT of imported objects breaks. The comment claiming the import "mirrors event.CreateEventUseCase" is wrong — the formats differ.

---

## High — security

### H1. App-password revocation has no ownership check (IDOR)
`server/internal/usecase/apppassword/revoke.go:22` — `Execute` receives `userUUID` but never uses it; there is no `ap.UserID == requester` check, the handler doesn't check either, and `GetByUUID` doesn't filter by user. Any authenticated user who learns another user's app-password UUID can revoke it. Contrast `RevokeCalDAVCredentialUseCase`, which does check ownership.

### H2. OAuth login auto-links to an existing account by email without verifying email ownership
`server/internal/usecase/auth/oauth_callback.go:106-114` — when no OAuth connection exists, a local user matching `userInfo.Email` is linked and logged in immediately, with no check of `userInfo.EmailVerified` (it is captured at `oauth.go:88` but only copied to newly created users). With a configurable OIDC provider or any IdP that doesn't verify emails, an attacker asserting a victim's email takes over the account, bypassing the password. Classic OAuth account takeover via unverified email.

### H3. App-password scopes (caldav/carddav) are never enforced
`server/internal/adapter/webdav/handler.go:86-97` — the real DAV auth path never calls `ap.HasScope(...)`. The only scope check lives in `adapter/auth/basic_auth.go:65`, inside `BasicAuthMiddleware`, which is **wired nowhere** (its only references are its own definition). An app password created with only `["caldav"]` scope grants full read/write CardDAV access and vice-versa; the scope restriction shown in the UI is decorative.

### H4. Stored XSS via contact data in search highlighting (frontend)
`webinterface/components/common/HighlightText.vue:2,16` — `v-html="highlighted"` renders `props.text` without HTML-escaping it (only the highlight term is regex-escaped). Used in `ContactListItem.vue` with `formatted_name`, email, and organization — all attacker-influenceable via CardDAV sync, .vcf import, or shared address books. A contact named `<img src=x onerror=...>` executes script in the victim's session as soon as they type any search query.

---

## High — data loss / broken features

### H5. Contact photos are stripped from every CardDAV read; a sync round-trip then deletes them permanently
`server/internal/adapter/repository/addressbook_repository.go` — PHOTO is extracted to a side table on write, but re-injection happens only in `GetObjectByUUID`. All DAV read paths (`ListObjects`, `QueryObjects`, `GetObjectByPath` via `carddav_backend.go:171,203,431,505`) serve the stripped vCard. When a client (Apple Contacts, DAVx5) edits that photo-less card and PUTs it back, `UpdateObject`'s else-branch deletes the `ContactPhoto` row — permanent photo loss through normal sync. Additionally, `PutAddressObject` sets `ContentLength` from the pre-strip vCard while serving the smaller stripped body, so GETs of photo contacts send a too-large Content-Length and clients hang/abort.

### H6. Revoking a share permanently blocks re-sharing with that user
`server/internal/adapter/repository/calendar_share_repo.go:55` (same in `addressbook_share_repo.go`) — shares use soft delete plus a composite unique index on (calendar_id, shared_with_id). The soft-deleted row stays in the unique index; re-creating the share fails with a UNIQUE constraint violation. **Verified empirically** (create → revoke → re-create fails). Fix: `Unscoped().Delete` or a partial unique index.

### H7. Sharing is non-functional end to end
- **CardDAV**: `carddav_backend.go:66-80` lists shared address books in the home set, but `resolveAddressBook` (`:398`) only searches owned books — every access to a shared book 404s; clients show a phantom collection.
- **REST/web UI**: `ListCalendarsUseCase` returns shared calendars (including read-write), but every event endpoint gates on `ownsCalendar` (`event_handler.go:47`), so sharees get 404 on list/create/edit. The repo's `GetUserPermission` (used by the DAV backend) is never called from REST.
- Deleting a collection leaves share rows dangling; sharees then see ghost zero-value entries (`usecase/calendar/delete.go:53` — the "cascade delete" comment is wrong, GORM soft delete cascades nothing).

### H8. Sync-token bookkeeping is inconsistent → forced full-resync loops and missed changes
`GetChangesSinceToken` validates an incoming token by looking it up as a changelog row and returns 403 `valid-sync-token` when absent (`sync.go:19`). But several paths mint or overwrite tokens **without** a changelog row:
- Collection create (`usecase/calendar/create.go:75`), calendar/address-book rename (`usecase/calendar/update.go:71`, `usecase/addressbook/update.go:42`), contact move (`usecase/contact/move.go:63`). A client syncing a fresh or renamed collection gets a token that the next sync rejects — an endless 403→full-resync loop.
- Import re-`Save`s a collection struct loaded before the loop, reverting the sync token advanced by `recordChange` during import (`calendar_import.go:184`, `contact_import.go:126`).
- `usecase/event/move.go` records the change only under the **target** calendar; the source calendar gets no "deleted" entry, so DAV clients keep the stale copy forever. `contact/move.go` has the same missing-delete problem.

### H9. CardDAV PUT ignores If-Match/If-None-Match preconditions
`server/internal/adapter/webdav/carddav_backend.go:260` — `PutAddressObject` never reads `opts`, while the CalDAV twin honors them (`caldav_backend.go:278-292`). Conditional updates with a stale ETag silently overwrite the other client's change (lost update); `If-None-Match: *` creates clobber existing contacts instead of returning 412. Related: `carddav_backend.go:319-325` matches existing contacts by UID **across paths** — a PUT to `/new.vcf` with an existing UID silently overwrites `/old.vcf` and returns the wrong path (RFC 6352 requires 409 `no-uid-conflict`).

### H10. REST event create/update never set or bump the ETag
`usecase/event/create.go:51-64`, `update.go:325-340` — REST-created events get `ETag: ""` and REST edits keep the old ETag. DAV clients comparing `getetag` never re-download REST-edited events; all REST-created events share the identical empty ETag. The DAV PUT path regenerates ETags correctly.

### H11. Contact import duplicate detection compares vCard UID against the internal DB UUID
`usecase/importexport/contact_import.go:67-91` — `GetObjectByUUID(ctx, uid)` queries the internal `uuid.New()` column with the vCard `UID`, which never matches. `skip` mode re-imports duplicates; `replace` mode's delete silently no-ops and creates a second copy. Re-importing a backup doubles every contact.

### H12. Single-calendar export produces invalid .ics (nested VCALENDAR)
`usecase/calendar/export.go:49-56` — appends `obj.ICalData` (a full VCALENDAR for all REST/DAV-created events) inside its own `BEGIN:VCALENDAR` wrapper. `backup_export.go` was fixed with `stripVCalendarWrapper`; this sibling path was not.

### H13. SQLite PRAGMAs applied to one pooled connection only
`server/internal/infrastructure/database/sqlite.go:45-56` — `busy_timeout`, `foreign_keys`, `synchronous` are per-connection settings, but are set via a single `Exec` on the pool (no `SetMaxOpenConns` cap). Under concurrency, other connections run with busy_timeout=0 (instant "database is locked" errors) and FK enforcement off. Put the pragmas in the DSN or cap the pool at 1.

### H14. JWT secret regenerates on every restart; persistence logic is dead code
`config.go:230-238` unconditionally generates a random secret when none is configured, so `EnsureSecret`'s persist-to-DB path (`jwt.go:97`) always short-circuits on its `Secret != ""` guard. Without `CALDAV_JWT_SECRET`, every restart invalidates all sessions, and multiple replicas mint different secrets (random 401s behind a load balancer). The `EnsureSecret` error is also only printed, not fatal (`routes.go:51`).

### H15. Frontend: infinite refresh-request loop when the refresh token is rejected
`webinterface/composables/useApi.ts:21-26` + `stores/auth.ts:76-95` — the 401 interceptor calls `refreshToken()`, which POSTs `/auth/refresh` through the **same** `$fetch` instance; a 401 from the refresh endpoint re-enters the interceptor recursively. Any server-side revocation/expiry mid-session triggers an unbounded loop of refresh POSTs. Also: the original request is never retried after a successful refresh, and refresh failure clears auth without redirecting to login.

### H16. Frontend: OAuth login dead-ends — callback page contract never satisfied
`webinterface/pages/auth/oauth/callback.vue:32-48` expects `access_token`/`refresh_token`/`expires_in` as query params, but the backend callback (`oauth_handler.go:121`) returns raw JSON to the browser and never redirects there. Users end on a JSON page. The page also reads `expires_in` while the backend convention is `expires_at`, and the token-in-URL design would leak tokens into history/access logs.

---

## Medium

| # | Location | Issue |
|---|----------|-------|
| M1 | `webdav/caldav_backend.go:176,251` | `parts[4]` read unconditionally — collection-level GET/PUT or a 4-segment multiget href panics (500 via recover middleware). `DeleteCalendarObject` handles it correctly. |
| M2 | `webdav/handler.go:188-197` | CardDAV principal discovery broken: PROPFIND on `/dav/` or `/dav/{user}/` always routes to the CalDAV handler, which never advertises `addressbook-home-set`. Clients can't auto-discover; manual URL required. |
| M3 | `webdav/caldav_backend.go:321` vs `sync.go:52` | ETags stored pre-quoted, then quoted again by go-webdav (`%q`) → invalid entity-tags on the wire; sync REPORT and PROPFIND emit different ETag strings for the same resource; REST writes use a third (unquoted) format. Clients re-download everything. |
| M4 | `addressbook_repository.go:528` | Address-book incremental sync filters `created_at > ?` instead of `id > ?` (the calendar repo does this right) — same-timestamp changes are permanently missed. Tokens are bare `UnixNano()` with no uniqueness guarantee. |
| M5 | `calendar_repo.go:149-170` | Calendar initial sync (empty token) replays the **entire raw changelog** — duplicate hrefs, 404 entries for resources the client never had (violates RFC 6578 §3.3), unbounded growth (changelog never pruned). The addressbook repo synthesizes current state correctly. |
| M6 | `usecase/contact/update.go:50-117` | Web-UI contact edits round-trip through the limited `Contact` model and rebuild the vCard from scratch — CATEGORIES, IMPP, X-* props, ANNIVERSARY etc. are permanently dropped, then propagated to all DAV clients via the bumped ETag. Even a photo upload triggers this. |
| M7 | `addressbook_repository.go:463,540` | Raw `Joins` bypass the soft-delete scope of the joined table: contacts of deleted address books still appear in global search and user contact counts. **Verified empirically.** |
| M8 | `domain/calendar/event.go:85-168` | Exception/EXDATE matching only works for UTC `...Z` recurrence IDs. `TZID=...` or `VALUE=DATE` forms (what real clients send) never match → modified instances appear twice, deleted occurrences reappear. |
| M9 | `usecase/event/update.go:103`, `delete.go:120` | Unparseable RECURRENCE-ID in `this_and_future` falls through to zero time → `UNTIL=00001231T235959Z` written into the master RRULE, wiping the entire series. All-day (`VALUE=DATE`) IDs hit this path. |
| M10 | `usecase/event/update.go:245-309` | A "this"-scope edit without Start/End falls back to the **series first-occurrence** times and writes DTSTART/DTEND onto the exception — a summary-only edit moves the instance. |
| M11 | `domain/calendar/event.go:160` | Recurrence expansion uses `rule.Between(start, end)` (start-in-window) while the non-recurring branch uses overlap semantics — multi-day recurring occurrences overlapping the window start are dropped. |
| M12 | `webdav/carddav_backend.go:443-449` | UUID-fallback lookup tests `abPath` instead of `objPath` (dead in normal flow), and when reachable, `GetObjectByUUID` is globally scoped — cross-tenant read/delete gated only on UUID secrecy. |
| M13 | `webdav/handler.go:100-128` | Dedicated CalDAV/CardDAV credentials are not bound to their protocol's paths — a CalDAV-only credential reads/writes address books and vice-versa. |
| M14 | `oauth_handler.go:150-181` | OAuth context cookie is plain base64 JSON, unsigned, `Secure: false` hardcoded, and carries `UserID` for the link flow — integrity-unprotected input deciding which account gets linked. |
| M15 | `config.go:190` + Fiber cors middleware | Defaults pair `AllowedOrigins: ["*"]` with `AllowCredentials: true`; Fiber v3 **panics** on this combination, so setting just `CALDAV_CORS_ENABLED=true` crashes the server at startup. |
| M16 | `docker-compose.yml:14` | Default `CALDAV_JWT_SECRET` fallback is exactly the value `Validate()` rejects → out-of-box `docker-compose up` crash-loops. Remove the fallback (the app auto-generates a secret). |
| M17 | `infrastructure/server/server.go:51-73` | `Listen` failure (port in use, bad TLS cert) only logs; `Run()` blocks on the signal channel forever — process stays alive serving nothing, supervisors never restart it. |
| M18 | `cmd/server/main.go:53` | YAML config support is dead: `config.Load("")` is the only caller and no flag/env supplies a path — `config.yaml` deployments silently run on defaults. |
| M19 | `event_handler.go:158,211,271,345` | `*obj.StartTime`/`*obj.EndTime` dereferenced unconditionally; nil for time-less imports/VTODOs (DAV PUT also hardcodes `ComponentType: "VEVENT"` for VTODOs) → 500 panics on Get/Create/Update/Move. `List` nil-checks correctly. |
| M20 | `calendar_repo.go:193` | Changelog rows can commit out of ID order under concurrent Postgres transactions — a client syncing between commits permanently misses the earlier-ID change. |

---

## Low

- **Login timing enumeration** — `usecase/auth/login.go:64` skips bcrypt for unknown emails (same in the DAV basic-auth path); add a dummy compare.
- **Email verification tokens stored in plaintext** (`register.go:94`) while reset tokens are SHA-256 hashed — inconsistent; hash them.
- **Refresh tokens never rotated** and no reuse detection (`refresh.go:34`).
- **Verification links/tokens printed to stdout** in the no-SMTP path (`register.go:123`); forgot-password errors via `fmt.Printf` (`auth_handler.go:226`).
- **No pagination cap** — `?limit=100000000` accepted on contact list/search; negative offset forwarded to GORM.
- **`registration_enabled` hardcoded `true`** in `system_handler.go:30`.
- **DAV docs handler** (`docs_handler.go:78`) joins a wildcard path with no containment check — protected only by fasthttp URI normalization; add a prefix check.
- **`.env.local` never loaded when `.env` is absent** — `godotenv.Load(".env", ".env.local")` stops at the first error (`config.go:148`).
- **`Security.MaxRequestSize`/`RequestTimeout` config ignored** — server hardcodes 10MB / 30s (`server.go:26`).
- **Postgres DSN built by naive Sprintf** — passwords with spaces/quotes break the DSN (`config.go:119`).
- **`user.CalDAVCredential` registered twice** in `Models()` (`migrations.go:29`).
- **LIKE wildcards unescaped** in contact search and CardDAV addressbook-query filters (parameterized, so wildcard-injection only).
- **Export filename from unsanitized address-book name** flows into Content-Disposition (`usecase/addressbook/export.go:40`); calendar exports sanitize, this doesn't.
- **X-WR-CALNAME/CALDESC written raw** — no escaping or 75-octet folding (`usecase/calendar/export.go:41`).
- **Missing `start`/`end` query params** on event list silently yield an empty 200 instead of 400 (`event_handler.go:84`).
- **No event time/RRULE validation** on REST create — `End < Start` accepted; invalid client-supplied `UNTIL` makes the event stored-but-never-rendered (`usecase/event/create.go:34`).
- **Frontend: refresh timers never cancelled** (`stores/auth.ts:97`) — stale timers across logout/login.

---

## Cross-cutting themes

1. **Sync-token/changelog discipline.** The design requires every token to exist as a changelog row, but at least five write paths violate this (C-level for fresh collections). Centralize token minting in `recordChange` and make every collection mutation go through it, including a "deleted" entry for moves.
2. **CalDAV and CardDAV halves drift apart.** Repeatedly, one side has the fix and the other doesn't: If-Match handling, initial-sync synthesis, `id` vs `created_at` ordering, export wrapper stripping, share resolution, filename sanitization. Worth a deliberate parity pass.
3. **The denormalized event columns (`start_time`, `end_time`, `component_type`, `etag`, `uid`) are maintained inconsistently** across the three write paths (REST, DAV PUT, import) — the root cause of C1, C2, H10, and several mediums.
4. **Sharing was apparently never exercised end-to-end** — listed but unreachable on both protocols and in the web UI, plus the revoke/re-share constraint bug.
