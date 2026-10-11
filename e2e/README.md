# CalCard end-to-end suite

Black-box tests for a **deployed** CalCard server, written in bash with curl and
jq. They talk to the server over HTTP only — no database or process access — so
they run against anything from a local `go run` to the production container
(`herrj/calcard`).

```bash
cd e2e
cp .env.example .env        # set CALCARD_URL
./run.sh                    # all suites
./run.sh dav_caldav sharing # selected suites, in that order
./run.sh --list             # what exists
./55_dav_caldav.sh          # any suite file also runs on its own
```

Every run registers fresh, uniquely named test users, works only with data it
created, and deletes it again at the end — also when a test fails or the run is
interrupted. Exit code: `0` all good, `1` at least one test FAILed, `2` the run
could not start (bad config, server unreachable, registration disabled), `130`
interrupted.

## Prerequisites

| Tool | Needed for |
|---|---|
| bash ≥ 4.4 | everything (macOS: `brew install bash`) |
| curl, jq | everything |
| coreutils (`mktemp`, `base64`, `od`, `sed`, `grep`, …) | everything |
| xmllint (libxml2) | XPath checks on WebDAV responses. **Optional:** without it those checks are skipped and only the substring checks around them run (a warning says so) |
| python3 | **optional:** serving a local iCalendar feed for the subscription tests, and inspecting the backup ZIP (`unzip` works for the latter too) |

The tools are checked at start; a missing one stops the run with a clear message.

### What the target server must allow

- **Self-registration enabled** (`registration_enabled: true`, i.e.
  `CALDAV_REGISTRATION_DISABLED` unset/false). The suite cannot create users any
  other way — the API has no admin user management, so no admin credentials are
  needed (or used).
- **No SMTP / e-mail verification** (`smtp_enabled: false`). With SMTP configured
  new accounts stay inactive until the mailbox link is clicked, which the suite
  cannot do. Point it at a staging instance of the same image instead.

Both are checked up front via `GET /api/v1/system/settings`, *before* any user
is created, and the run aborts with instructions if they do not hold.

- Rate limiting may stay on: on HTTP 429 the suite waits `Retry-After` and
  retries (the summary shows how many times). With the default 100 requests per
  minute per IP a full run takes several minutes longer; for a quick run raise
  `CALDAV_RATE_LIMIT_REQUESTS` on the target or turn the limiter off.
- MCP (`CALDAV_MCP_ENABLED`) and subscriptions (`CALDAV_SUBSCRIPTIONS_ENABLED`)
  are detected; when disabled their suites report SKIP.

## Configuration

`e2e/.env` (git-ignored; copy `.env.example`, which documents every key) holds
`KEY=VALUE` lines. **Variables set in the shell win over the file**, so
`CALCARD_URL=https://staging.example.com ./run.sh` needs no file at all.
`E2E_ENV_FILE=/path/to/file` selects another file.

| Key | Default | Meaning |
|---|---|---|
| `CALCARD_URL` | — | Base URL, e.g. `https://cal.example.com` or `http://192.168.1.10:8080` |
| `CALCARD_HOST` / `CALCARD_PORT` / `CALCARD_SCHEME` | — / — / http (https on 443) | Alternative to `CALCARD_URL`; the host may also be `host:port` or a full URL |
| `E2E_INSECURE` | `false` | `curl -k` for self-signed TLS |
| `E2E_TIMEOUT` / `E2E_CONNECT_TIMEOUT` | `30` / `10` | per-request limits (s) |
| `E2E_HEALTH_WAIT` | `60` | wait this long for `GET /health` before giving up (s) |
| `E2E_USER_PREFIX` / `E2E_EMAIL_DOMAIN` | `e2e` / `example.test` | test users are `<prefix>-<run id>-<key>@<domain>` |
| `E2E_PASSWORD` | random per run | password of the test users |
| `E2E_SUITES` / `E2E_SKIP_SUITES` | all / none | suite selection (space or comma separated); CLI arguments override `E2E_SUITES` |
| `E2E_VERBOSE` | `0` | `1` logs every request, `2` also bodies (same as `-v` / `-vv`) |
| `E2E_BODY_PREVIEW` | `1200` | body characters printed for a failing assertion |
| `E2E_STRICT` | `false` | count known server bugs (KFAIL) as failures (`--strict`) |
| `E2E_KEEP_DATA` | `false` | skip cleanup, for debugging (`--keep-data`) |
| `E2E_RATE_LIMIT_RETRIES` / `E2E_RATE_LIMIT_MAX_WAIT` | `6` / `90` | 429 handling |
| `E2E_RATE_LIMIT_BURST` | `false` | opt-in login-limiter test (see below) |
| `E2E_FEED` | `auto` | `off` skips the subscription fetch tests |
| `E2E_FEED_HOST` / `E2E_FEED_BIND` / `E2E_FEED_PORT` | auto | where the local feed server listens and how the server reaches it |
| `NO_COLOR` | — | disable colours (also off when stdout is not a terminal) |

## Output

```
== dav_caldav: MKCALENDAR, PUT/GET/DELETE + ETags, REPORTs, PROPPATCH, COPY/MOVE
  PASS  dav_caldav/PUT with If-None-Match: * creates (201 + ETag) (299ms)
  FAIL  dav_caldav/…
        ✗ expected HTTP 412, got 204
          > PUT http://…/dav/…/x.ics  [basic e2e-…-u1@example.test]
          < HTTP 204 …
  KFAIL dav_caldav/MKCALENDAR / MKCOL on an existing collection is refused (405) [known server bug F03, see e2e/FINDINGS.md]
  SKIP  subscriptions/… — server refuses private/loopback feed hosts (SSRF guard …)
…
PASS 237  FAIL 0  SKIP 6  KFAIL 24  XPASS 0
```

| Result | Meaning |
|---|---|
| PASS | the server behaves as documented / as the RFC requires |
| FAIL | it does not — a regression or a broken test; the run exits 1 |
| SKIP | not run, with the reason (feature disabled, external system needed, opt-in) |
| KFAIL | fails because of a **known server bug** listed in `FINDINGS.md`; the test asserts the *correct* behaviour so the bug stays visible. Does not fail the run unless `--strict` |
| XPASS | a known bug no longer reproduces — remove the `t_known` marker and update `FINDINGS.md` |

A failing assertion prints the request and the response (status, key headers,
body preview) and only fails *that* test; the run continues.

## Suites

| File | Suite | Covers |
|---|---|---|
| `00_system.sh` | system | `/health`, `/ready`, system settings, auth methods, about/open-source, security headers, unknown routes, SPA serving |
| `10_auth.sh` | auth | registration + validation, login, refresh rotation, logout, password change, forgot/reset/verify edges, OAuth surface, account deletion |
| `15_credentials.sh` | credentials | app passwords (scopes, revocation, isolation), CalDAV / CardDAV credentials (read vs read-write, protocol confinement, expiry, duplicates) |
| `20_profile.sh` | profile | profile get/patch, preferences, full backup export (ZIP content) |
| `30_calendars.sh` | calendars | calendar CRUD, validation, isolation, export, last-calendar guard |
| `35_events.sh` | events | event CRUD, validation, If-Match, all-day, recurrence (expand, BYDAY, scope=this / this_and_future for update and delete), move |
| `38_import_export.sh` | import_export | .ics / .vcf import (raw body, JSON, multipart, duplicate handling), export round trip |
| `40_addressbooks.sh` | addressbooks | address book + contact CRUD, paging, photos, move, contact search, last-book guard |
| `45_search.sh` | search | unified search: all groups, no date window, recurring series, types, bounds, limits, isolation |
| `50_dav_discovery.sh` | dav_discovery | `/.well-known/caldav|carddav` (GET and PROPFIND), OPTIONS/DAV header, principal, home sets, collection properties at Depth 0/1 |
| `55_dav_caldav.sh` | dav_caldav | MKCALENDAR / MKCOL, PUT/GET/HEAD/DELETE with ETag, If-Match, If-None-Match, UID conflicts, VTODO, recurrence overrides, calendar-query, calendar-multiget, PROPPATCH, COPY/MOVE, REST↔DAV consistency |
| `60_dav_carddav.sh` | dav_carddav | the same for CardDAV: MKCOL, vCard 3/4, comma lists, addressbook-query / -multiget, photos over DAV |
| `70_dav_sync.sh` | dav_sync | WebDAV-Sync: initial / empty / create / modify / delete deltas, REST changes in DAV deltas, invalid tokens, `<limit>` truncation (507), ctag |
| `75_dav_authz.sh` | dav_authz | 401 shapes, malformed Authorization, accepted credential kinds, and cross-user isolation on every DAV verb (PROPFIND, REPORT, GET, PUT, DELETE, PROPPATCH, MKCALENDAR, traversal) |
| `80_sharing.sh` | sharing | calendar + address-book shares over REST and DAV (read vs read-write, revocation, cascades), public calendar links, deleted owners |
| `90_mcp.sh` | mcp | MCP tokens, `POST /mcp` handshake/sessions, all tools cross-checked over REST, resources, JSON-RPC errors, batches, isolation, revocation |
| `95_subscriptions.sh` | subscriptions | URL validation, feed mirroring, read-only enforcement (REST + DAV), refresh deltas, failing feeds, isolation |
| `99_ratelimit.sh` | ratelimit | limiter headers; opt-in login-limiter burst |

## What is skipped, and why

| Test | Reason |
|---|---|
| password-reset and e-mail-verification round trips | need an SMTP server and access to the mailbox (external system). The endpoints' edge cases (anti-enumeration, bogus tokens) *are* tested |
| OAuth/OIDC login | needs an external identity provider and a browser. The provider listing and unknown-provider handling are tested |
| web UI at `/` | when the target serves no SPA (e.g. a bare `go run`); the Docker image does |
| security headers | when `CALDAV_SECURITY_HEADERS_ENABLED=false` (or a proxy strips them) |
| subscription fetch tests | when no feed can be served from this machine (no python3, `E2E_FEED=off`) or the server refuses/cannot reach it: `allow_insecure_urls=false` (the local feed is plain http), `allow_private_hosts=false` (SSRF guard — a LAN/loopback feed is "private"), or no route back. URL validation always runs. To run them, start the server with `CALDAV_SUBSCRIPTIONS_ALLOW_PRIVATE_HOSTS=true` and `CALDAV_SUBSCRIPTIONS_ALLOW_INSECURE_URLS=true` and make sure `E2E_FEED_HOST` is reachable from it |
| rate-limit headers | when rate limiting is off on the target |
| login-limiter burst | opt-in (`E2E_RATE_LIMIT_BURST=true`): it sends ~11 failed logins for a throwaway, never-registered address, which also spends part of *this IP's* login allowance (20/min by default) for one window. Runs as the last suite |
| MCP / subscriptions suites | when the feature is disabled on the target (404 on its routes) |

COPY and MOVE are not implemented by the server (go-webdav answers 501); the
suite asserts that clean refusal and that the source stays in place.

## Data the suite leaves behind

Everything a test creates is registered in a cleanup stack and removed on exit
(calendars, address books, shares, public links, subscriptions, app passwords,
DAV credentials, MCP tokens), then the test accounts are deleted through
`DELETE /api/v1/users/me`. What the server's API cannot remove:

- account deletion is a **soft delete**: the user rows and each account's
  default *Personal* calendar and *Contacts* address book (emptied; the last
  calendar/book cannot be deleted) stay in the database;
- the deliberately deleted account of the account-deletion tests keeps its
  (revoked-by-deletion) app password row, and the "deleted owner" sharing test
  leaves one empty calendar of a deleted account (see F18).

None of it is reachable by anyone after the run. Use a dedicated prefix
(`E2E_USER_PREFIX`) if you want to find these rows later.

## Writing a test

A suite file `NN_name.sh` defines a description and a `suite_<name>` function
that calls `t_run "description" function` for each test, and ends with the
standalone stanza (copy it from any suite). Inside a test:

```bash
t_cal_example() {
	mk_calendar EX "$(uniq example)" || { t_fail "setup failed" with-response; return; }
	api PATCH "/calendars/$EX_UUID" --json "$(jobj name="renamed" color="#00aa55")"
	assert_status 200 || return             # stop this test if it failed
	assert_json .name renamed
	dav PROPFIND "$(dav_home_cal)$EX_PATH/" --depth 0 --xml "$PROPFIND_COLLECTION"
	assert_status 207
	assert_xpath "$(lp displayname)[.='renamed']"
	as U2 api GET "/calendars/$EX_UUID"     # act as the second user
	assert_status 404
}
```

- **Requests** (`lib/http.sh`): `api` (REST under `/api/v1`, JWT of the acting
  user), `dav` (Basic auth with the acting user's app password), `raw` / `api_anon`
  (no credentials), `req` (anything). Options: `--json`, `--xml`, `--ics`,
  `--vcf`, `--data`, `--file`, `--form`, `--ctype`, `--depth`, `-H`, `--bearer`,
  `--basic`, `--no-retry`. Results land in `RESP_STATUS`, `RESP_HEADERS`,
  `RESP_BODY` (`hdr NAME`, `jget FILTER` read them). **Never call request or
  factory helpers inside `$(...)`** — they set globals (and refresh tokens) that a
  subshell would lose.
- **Users** (`lib/fixtures.sh`): `ensure_user U1` / `U2` are shared by all suites;
  `user_create KEY` makes a scratch user (deleted at the end). Fields:
  `uget KEY EMAIL|PASSWORD|USERNAME|TOKEN|APPPW…`; `as KEY cmd…` acts as KEY.
- **Factories** register their own cleanup: `mk_calendar PREFIX name` (sets
  `PREFIX_UUID/_ID/_PATH`), `mk_addressbook`, `mk_event OUTVAR …`,
  `mk_contact OUTVAR …`. For anything else call `defer "description" command…`.
- **Assertions** (`lib/assert.sh`): `assert_status 201 204 ["msg"]`,
  `assert_status_class 4xx`, `assert_json FILTER VALUE`, `assert_jq EXPR`,
  `assert_contains` / `assert_not_contains`, `assert_match REGEX`,
  `assert_header NAME [substring]`, `assert_header_eq`, `assert_eq` / `assert_ne`,
  `assert_xpath EXPR`, `assert_xpath_count`, `assert_xml_wellformed`. Build XPath
  namespace-agnostically with `lp a b c` (`//*[local-name()='a']/…`) and `el name`.
- **Flow**: `need VAR…` skips the test when an earlier step did not produce a
  value; `t_skip "reason"` skips explicitly; `t_known F<nn>` marks a known server
  bug (document it in `FINDINGS.md`).
- Names must be unique per run: use `uniq label` (prefix + run id).
- Run `shellcheck -x run.sh *.sh lib/*.sh` (settings in `.shellcheckrc`).

## Known server bugs

Details, evidence and root causes are in [`FINDINGS.md`](FINDINGS.md). Summary:

| ID | Severity | Summary |
|---|---|---|
| F01 | Critical | shared address books leak the owner's bcrypt password hash and e-mail to the sharee (`GET /addressbooks`, `GET /search`) |
| F02 | High | Basic auth and sharing by *username* fail (mixed-case usernames are lower-cased on lookup); mixed-case CardDAV credentials never authenticate |
| F03 | High | MKCOL / MKCALENDAR on an existing collection answer 201 and create a duplicate at the same path |
| F18 | High | deleting an account leaves its calendars shared and its public links serving |
| F04 | Medium | server-written events carry `DTSTAMP;TZID=Local:…` (must be UTC) |
| F05 | Medium | not-found on contacts / foreign address books answers 500 |
| F06 | Medium | empty-body PROPFIND on `/dav/` and the principal is a 400 (must be allprop) |
| F13 | Medium | events without start/end are accepted and stored in year 1 |
| F14 | Medium | calendar import drops recurrence exceptions (RECURRENCE-ID) |
| F15 | Medium | imported events lose the VTIMEZONE their TZID refers to |
| F16 | Medium | CalDAV PUT of a VCALENDAR without components is a 500 |
| F17 | Medium | sync-collection on a missing / foreign collection is a 500 |
| F19 | Medium | a deleted account's e-mail can never register again |
| F07 | Low | collection properties differ between Depth 0 and 1; `calendar-color` is write-only |
| F08 | Low | PROPPATCH is not atomic |
| F09 | Low | event URLs ignore their calendar segment |
| F10 | Low | password change with new == current answers 401 instead of 400 |
| F11 | Low | PROPFIND on another user's home answers 207 with an empty body |
| F12 | Low | calendar PATCH/DELETE/import answer 400 instead of 404 for unknown/foreign calendars |
