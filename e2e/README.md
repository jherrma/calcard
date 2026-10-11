# CalCard end-to-end suite

Black-box tests for a **deployed** CalCard server, written in Python with
pytest, httpx and lxml. They talk to the server over HTTP only — no database or
process access, no server code imported — so they run against anything from a
local `go run` to the production container (`herrj/calcard`).

```bash
cd e2e
python3 -m venv .venv && .venv/bin/pip install -r requirements.txt
cp .env.example .env         # set CALCARD_URL
.venv/bin/python run         # all suites
.venv/bin/python run dav_caldav sharing   # selected suites, in that order
.venv/bin/python run --list  # what exists
```

With [uv](https://docs.astral.sh/uv/) no venv is needed:
`uv run --with-requirements requirements.txt ./run dav_caldav`. With the venv
activated (`. .venv/bin/activate`), `./run …` works directly.

Every run registers fresh, uniquely named test users, works only with data it
created, and deletes it again at the end — also when a test fails or the run is
interrupted. Exit code: `0` all good, `1` at least one test FAILed or a known bug
no longer reproduces (XPASS), `2` the run could not start (bad config, server
unreachable, registration disabled, unknown suite), `130` interrupted (Ctrl-C /
SIGTERM).

## Prerequisites

| Requirement | Needed for |
|---|---|
| Python ≥ 3.10 | everything |
| `pytest`, `httpx` (+ `httpcore`), `lxml` — pinned in [`requirements.txt`](requirements.txt) | everything: test runner, HTTP client, XPath on WebDAV responses |

`./run` checks the packages and tells you how to install them if one is missing.
The local iCalendar feed for the subscription tests and the backup-ZIP
inspection use the standard library only.

### What the target server must allow

- **Self-registration enabled** (`registration_enabled: true`, i.e.
  `CALDAV_REGISTRATION_DISABLED` unset/false). The suite cannot create users any
  other way — the API has no admin user management, so no admin credentials are
  needed (or used).
- **No SMTP / e-mail verification** (`smtp_enabled: false`). With SMTP configured
  new accounts stay inactive until the mailbox link is clicked, which the suite
  cannot do. Point it at a staging instance of the same image instead.

Both are checked up front via `GET /api/v1/system/settings`, *before* any user
is created, and the run aborts (exit code 2) with instructions if they do not hold.

- Rate limiting may stay on: on HTTP 429 the suite waits `Retry-After` and
  retries (the summary shows how many times). With a low per-IP limit a full run
  takes several minutes longer; for a quick run raise `CALDAV_RATE_LIMIT_REQUESTS`
  on the target or turn the limiter off.
- MCP (`CALDAV_MCP_ENABLED`) and subscriptions (`CALDAV_SUBSCRIPTIONS_ENABLED`)
  are detected; when disabled every test of their suite reports SKIP.

## Running

```
./run [options] [suite...] [-- pytest-args...]

  --list            list available suites and exit
  -v, -vv           log every request (-vv also logs bodies)
  --strict          count known server bugs (KFAIL) as failures
  --keep-data       do not delete the test users / data at the end (debugging)
  --results FILE    also write one "OUTCOME<TAB>suite/test" line per test to FILE
```

Suites run in the order given (default: all, in file order). Anything after
`--` goes to pytest unchanged, e.g. `./run dav_caldav -- -k etag -x`. `./run` is
a thin wrapper: `cd e2e && pytest` (or `pytest tests/test_55_dav_caldav.py`)
runs the same tests with the same output, summary and exit codes;
`E2E_SUITES`, `E2E_STRICT`, `E2E_VERBOSE` … then come from the environment or
`.env`.

## Configuration

`e2e/.env` (git-ignored; copy `.env.example`, which documents every key) holds
`KEY=VALUE` lines. It is parsed, not executed. **Variables set in the shell win
over the file**, so `CALCARD_URL=https://staging.example.com ./run` needs no
file at all. `E2E_ENV_FILE=/path/to/file` selects another file.

| Key | Default | Meaning |
|---|---|---|
| `CALCARD_URL` | — | Base URL, e.g. `https://cal.example.com` or `http://192.168.1.10:8080` |
| `CALCARD_HOST` / `CALCARD_PORT` / `CALCARD_SCHEME` | — / — / http (https on 443) | Alternative to `CALCARD_URL`; the host may also be `host:port` or a full URL |
| `E2E_INSECURE` | `false` | accept self-signed / untrusted TLS certificates |
| `E2E_TIMEOUT` / `E2E_CONNECT_TIMEOUT` | `30` / `10` | per-request limits (s) |
| `E2E_HEALTH_WAIT` | `60` | wait this long for `GET /health` before giving up (s) |
| `E2E_USER_PREFIX` / `E2E_EMAIL_DOMAIN` | `e2e` / `example.test` | test users are `<prefix>-<run id>-<key>@<domain>` |
| `E2E_PASSWORD` | random per run | password of the test users |
| `E2E_SUITES` / `E2E_SKIP_SUITES` | all / none | suite selection (space or comma separated); `./run` arguments override `E2E_SUITES` |
| `E2E_VERBOSE` | `0` | `1` logs every request, `2` also bodies (same as `-v` / `-vv`) |
| `E2E_BODY_PREVIEW` | `1200` | body characters printed for a failing check |
| `E2E_STRICT` | `false` | count known server bugs (KFAIL) as failures (`--strict`) |
| `E2E_KEEP_DATA` | `false` | skip cleanup, for debugging (`--keep-data`) |
| `E2E_RESULTS_FILE` | — | write the per-test results there (`--results`) |
| `E2E_RATE_LIMIT_RETRIES` / `E2E_RATE_LIMIT_MAX_WAIT` | `6` / `90` | 429 handling |
| `E2E_RATE_LIMIT_BURST` | `false` | opt-in login-limiter test (see below) |
| `E2E_FEED` | `auto` | `off` skips the subscription fetch tests |
| `E2E_FEED_HOST` / `E2E_FEED_BIND` / `E2E_FEED_PORT` | auto | where the local feed server listens and how the server reaches it |
| `NO_COLOR` | — | disable colours (also off when stdout is not a terminal) |

## Output

```
== dav_caldav: MKCALENDAR, PUT/GET/DELETE + ETags, REPORTs, PROPPATCH, COPY/MOVE
  PASS  dav_caldav/PUT with If-None-Match: * creates (201 + ETag) (12ms)
  FAIL  dav_caldav/…
        ✗ expected HTTP 412, got 204
          > PUT http://…/dav/…/x.ics  [basic e2e-…-u1@example.test]
          < HTTP 204 …
  KFAIL dav_caldav/MKCALENDAR / MKCOL on an existing collection is refused (405) [known server bug F03, see e2e/FINDINGS.md]
  SKIP  subscriptions/… — server refuses private/loopback feed hosts (SSRF guard …)
…
PASS 238  FAIL 0  SKIP 6  KFAIL 23  XPASS 0  (429 retries: 0)
```

| Result | Meaning |
|---|---|
| PASS | the server behaves as documented / as the RFC requires |
| FAIL | it does not — a regression or a broken test; the run exits 1 |
| SKIP | not run, with the reason (feature disabled, external system needed, opt-in, an earlier step it builds on failed) |
| KFAIL | fails because of a **known server bug** listed in `FINDINGS.md` (a strict `xfail`); the test asserts the *correct* behaviour so the bug stays visible. Does not fail the run unless `--strict` |
| XPASS | a known bug no longer reproduces: the run **fails** (exit 1) until the `known("F<nn>")` marker is removed and `FINDINGS.md` updated |

A failed check prints the request and the response (status, key headers, body
preview) and only fails *that* test; the run continues. With `--strict` the
known-bug markers only label the tests: a bug that still reproduces is a FAIL
(tagged with its F-number), a fixed one a plain PASS.

## Suites

| File | Suite | Covers |
|---|---|---|
| `tests/test_00_system.py` | system | `/health`, `/ready`, system settings, auth methods, about/open-source, security headers, unknown routes, SPA serving |
| `tests/test_10_auth.py` | auth | registration + validation, login, refresh rotation, logout, password change, forgot/reset/verify edges, OAuth surface, account deletion |
| `tests/test_15_credentials.py` | credentials | app passwords (scopes, revocation, isolation), CalDAV / CardDAV credentials (read vs read-write, protocol confinement, expiry, duplicates) |
| `tests/test_20_profile.py` | profile | profile get/patch, preferences, full backup export (ZIP content) |
| `tests/test_30_calendars.py` | calendars | calendar CRUD, validation, isolation, export, last-calendar guard |
| `tests/test_35_events.py` | events | event CRUD, validation, If-Match, all-day, recurrence (expand, BYDAY, scope=this / this_and_future for update and delete), move |
| `tests/test_38_import_export.py` | import_export | .ics / .vcf import (raw body, JSON, multipart, duplicate handling), export round trip |
| `tests/test_40_addressbooks.py` | addressbooks | address book + contact CRUD, paging, photos, move, contact search, last-book guard |
| `tests/test_45_search.py` | search | unified search: all groups, no date window, recurring series, types, bounds, limits, isolation |
| `tests/test_50_dav_discovery.py` | dav_discovery | `/.well-known/caldav|carddav` (GET and PROPFIND), OPTIONS/DAV header, principal, home sets, collection properties at Depth 0/1 |
| `tests/test_55_dav_caldav.py` | dav_caldav | MKCALENDAR / MKCOL, PUT/GET/HEAD/DELETE with ETag, If-Match, If-None-Match, UID conflicts, VTODO, recurrence overrides, calendar-query, calendar-multiget, PROPPATCH, COPY/MOVE, REST↔DAV consistency |
| `tests/test_60_dav_carddav.py` | dav_carddav | the same for CardDAV: MKCOL, vCard 3/4, comma lists, addressbook-query / -multiget, photos over DAV |
| `tests/test_70_dav_sync.py` | dav_sync | WebDAV-Sync: initial / empty / create / modify / delete deltas, REST changes in DAV deltas, invalid tokens, `<limit>` truncation (507), ctag |
| `tests/test_75_dav_authz.py` | dav_authz | 401 shapes, malformed Authorization, accepted credential kinds, and cross-user isolation on every DAV verb (PROPFIND, REPORT, GET, PUT, DELETE, PROPPATCH, MKCALENDAR, traversal) |
| `tests/test_80_sharing.py` | sharing | calendar + address-book shares over REST and DAV (read vs read-write, revocation, cascades), public calendar links, deleted owners |
| `tests/test_90_mcp.py` | mcp | MCP tokens, `POST /mcp` handshake/sessions, all tools cross-checked over REST, resources, JSON-RPC errors, batches, isolation, revocation |
| `tests/test_95_subscriptions.py` | subscriptions | URL validation, feed mirroring, read-only enforcement (REST + DAV), refresh deltas, failing feeds, isolation |
| `tests/test_99_ratelimit.py` | ratelimit | limiter headers; opt-in login-limiter burst |

Tests inside a suite run in file order, and some build on earlier ones (create →
read → update → delete). They hand ids and ETags to each other through the
module-scoped `st` fixture; a test whose predecessor failed is SKIPPED with
"precondition not met" rather than failing a second time.

## What is skipped, and why

| Test | Reason |
|---|---|
| password-reset and e-mail-verification round trips | need an SMTP server and access to the mailbox (external system). The endpoints' edge cases (anti-enumeration, bogus tokens) *are* tested |
| OAuth/OIDC login | needs an external identity provider and a browser. The provider listing and unknown-provider handling are tested |
| web UI at `/` | when the target serves no SPA (e.g. a bare `go run`); the Docker image does |
| security headers | when `CALDAV_SECURITY_HEADERS_ENABLED=false` (or a proxy strips them) |
| subscription fetch tests | when no feed can be served from this machine (`E2E_FEED=off`, no usable local address) or the server refuses/cannot reach it: `allow_insecure_urls=false` (the local feed is plain http), `allow_private_hosts=false` (SSRF guard — a LAN/loopback feed is "private"), or no route back. URL validation always runs. To run them, start the server with `CALDAV_SUBSCRIPTIONS_ALLOW_PRIVATE_HOSTS=true` and `CALDAV_SUBSCRIPTIONS_ALLOW_INSECURE_URLS=true` and make sure `E2E_FEED_HOST` is reachable from it |
| rate-limit headers | when rate limiting is off on the target |
| login-limiter burst | opt-in (`E2E_RATE_LIMIT_BURST=true`): it sends ~11 failed logins for a throwaway, never-registered address, which also spends part of *this IP's* login allowance (20/min by default) for one window. Runs as the last suite |
| MCP / subscriptions suites | when the feature is disabled on the target (404 on its routes): every test of the suite is skipped with that reason |

COPY and MOVE are not implemented by the server (go-webdav answers 501); the
suite asserts that clean refusal and that the source stays in place.

## Data the suite leaves behind

Everything a test creates is registered on a cleanup stack and removed when the
session ends (calendars, address books, shares, public links, subscriptions,
app passwords, DAV credentials, MCP tokens), then the test accounts are deleted
through `DELETE /api/v1/users/me`. What the server's API cannot remove:

- account deletion is a **soft delete**: the user rows and each account's
  default *Personal* calendar and *Contacts* address book (emptied; the last
  calendar/book cannot be deleted) stay in the database;
- the deliberately deleted account of the account-deletion tests keeps its
  (revoked-by-deletion) app password row, and the "deleted owner" sharing test
  leaves one empty calendar of a deleted account (see F18).

None of it is reachable by anyone after the run. Use a dedicated prefix
(`E2E_USER_PREFIX`) if you want to find these rows later.

## Writing a test

A suite is a module `tests/test_NN_name.py` with a `DESCRIPTION` string; each
test is a function whose **docstring is its name in the results**. Fixtures:
`e2e` (the run: `raw`, `anon`, `req`, `uniq`, `defer`, `create_user`, `feed`),
`u1` / `u2` (shared users), `t` (checks), `st` (state shared by the suite's
tests).

```python
from harness.checks import known
from harness.data import PROPFIND_COLLECTION
from harness.xml import lp


def test_example(t, e2e, u1, u2):
    """PATCH renames, DAV shows the new name, the peer sees nothing"""
    cal = u1.mk_calendar(e2e.uniq("example"))          # cleanup registered
    r = u1.api("PATCH", f"/calendars/{cal['uuid']}", json={"name": "renamed", "color": "#00aa55"})
    if not t.status(r, 200):                            # stop this test if it failed
        return
    t.json(r, "name", "renamed")
    r = u1.dav("PROPFIND", f"{u1.home_cal}{cal['path']}/", depth=0, xml=PROPFIND_COLLECTION)
    t.status(r, 207)
    t.xpath(r, f"{lp('displayname')}[.='renamed']")
    t.status(u2.api("GET", f"/calendars/{cal['uuid']}"), 404)   # act as the second user


@known("F03")  # a documented server bug: KFAIL now, XPASS (= failure) once fixed
def test_mkcol_existing(t, u1):
    """MKCOL on an existing collection is refused (405)"""
    ...
```

- **Requests** (`harness/session.py`, `harness/http.py`): `u.api` (REST under
  `/api/v1`, the user's JWT), `u.dav` (Basic auth with the user's app password),
  `e2e.raw` / `e2e.anon` (no credentials), `e2e.req` (any URL). Options:
  `json=` (object or raw string), `xml=`, `ics=`, `vcf=`, `data=` + `ctype=`,
  `files=` (multipart), `depth=`, `headers=`, `bearer=`, `basic=(user, pw)`,
  `retry=False` (no 429 handling), `path_as_is=True` (send `..` unnormalized).
  The result is a raw `Resp`: `status`, `content` (bytes), `text`, `json()`
  (plain `json.loads`), `hdr(name)`. **Never decode responses into typed models**
  — a model would silently drop unexpected fields, and an unexpected field is
  exactly what a leak looks like (F01 was found by scanning the raw body).
- **Users**: `u1` / `u2` are shared by all suites; `e2e.create_user("KEY")`
  makes a scratch user (deleted at the end). Fields: `email`, `password`,
  `username`, `token`, `apppw`, `home_cal`, `home_ab`; `u.require_dav()` creates
  the DAV app password in a suite fixture.
- **Suite setup** (resources every test of a suite uses) lives in a
  module-scoped autouse fixture. If it fails, every test of that suite FAILs
  with the setup error and the other suites still run.
- **Factories** register their own cleanup and return the raw JSON:
  `u.mk_calendar(name)`, `u.mk_addressbook(name)`, `u.mk_event(cal_uuid, …)`,
  `u.mk_contact(ab_uuid, …)`. For anything else call
  `e2e.defer("description", e2e.cleanup_api, user, "DELETE", path)`.
- **Checks** (`harness/checks.py`) record a failure and return `False` — they do
  not stop the test, so later steps (and cleanups) still run: `t.status(r, 201, 204)`,
  `t.status_class(r, "4xx")`, `t.status_not`, `t.json(r, "data.items[0].id", value)`,
  `t.jq(r, lambda j: …, msg)`, `t.nonempty`, `t.contains` / `t.not_contains`,
  `t.match(r, regex)`, `t.header(r, name, substring)`, `t.header_eq`,
  `t.eq` / `t.ne` / `t.true`, `t.xpath(r, expr)`, `t.xpath_count`,
  `t.xml_wellformed`. Build XPath namespace-agnostically with `lp("a", "b")`
  (`//*[local-name()='a']/…`) and `el("name")`; `xstr` / `xml_text` / `xcount`
  read values.
- **Flow**: `t.need(st, "name")` skips when an earlier step did not produce a
  value; `t.skip("reason")` skips; `t.abort(msg, r)` fails and stops;
  `@known("F<nn>")` marks a known server bug (document it in `FINDINGS.md`),
  `t.known("F<nn>")` does the same from inside a test when the bug only applies
  under a runtime condition.
- Names must be unique per run: use `e2e.uniq("label")` (prefix + run id).

## Known server bugs

Details, evidence and root causes are in [`FINDINGS.md`](FINDINGS.md). Summary:

| ID | Severity | Summary |
|---|---|---|
| ~~F01~~ | ~~Critical~~ | ~~shared address books leak the owner's bcrypt password hash and e-mail to the sharee~~ — fixed in #181; the test stays as a regression guard |
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
