# E2E findings

Server bugs found by the e2e suite. **None are open**: F01–F31 are all fixed on
master and their tests are ordinary regression tests (a run against master:
PASS 287, FAIL 0, KFAIL 0). F02–F19 were found against master `9334b84` on
2026-10-11; F20–F31 turned up while fixing them. A new finding is listed here and
marked `@known("F<nn>")` in its test (`t.known("F<nn>")` where it only applies
under a runtime condition) until it is fixed: the test asserts the CORRECT
behaviour, its pinning checks (`t.bug…`) name the documented wrong response, and
only that response produces a KFAIL (see README.md, "Known bugs in a test").

## Fixed on master, guarded by the suite

| ID | Fixed in | What was wrong |
| --- | --- | --- |
| F02 | #195 | Basic auth and sharing by mixed-case username |
| F03 | #196 | MKCOL/MKCALENDAR on an existing collection created a duplicate (now 405) |
| F04 | #197 | `DTSTAMP;TZID=Local` on server-written events and vCard REV |
| F05 | #198 | not-found on contacts / foreign address books answered 500 |
| F06 | #199 | empty-body PROPFIND on `/dav/` and the principal |
| F07 | #200 | collection properties differed between PROPFIND depths |
| F08 | #201 | PROPPATCH was not atomic |
| F09 | #202 | event URLs ignored their calendar segment |
| F10 | #203 | password change with new == current answered 401 |
| F11 | #204 | PROPFIND on a foreign home answered 207 |
| F12 | #205 | calendar PATCH/DELETE/import answered 400 instead of 404 |
| F13 | #206 | events without start/end were accepted |
| F14 | #207 | calendar import dropped recurrence overrides |
| F15 | #208 | imported events lost their VTIMEZONE |
| F16 | #209 | CalDAV PUT of an empty VCALENDAR was a 500 |
| F17 | #210 | sync-collection on a missing collection was a 500 |
| F18 | #211 | account deletion left shares and public links alive |
| F19 | #212 | a deleted account's e-mail could never register again |
| F20 | #225 | REST/MCP events with a TZID carried no VTIMEZONE |
| F21 | #213 | owner's Depth 1 PROPFIND on a subscription answered 404 |
| F22 | #214 | REST times with a UTC offset and no timezone were stored with an empty TZID |
| F23 | #215 | calendar import: events without DTSTAMP failed, unsafe UIDs made unreachable paths |
| F24 | #216 | a revoked DAV credential's username could never be reused |
| F25 | #217 | MKCALENDAR properties (colour, description, timezone, …) were ignored |
| F26 | #220 | search was case-sensitive on PostgreSQL and ASCII-only on SQLite |
| F27 | #221 | VJOURNAL/VFREEBUSY PUT, .vcf import without VERSION/FN |
| F28 | #222 | calendar errors: storage failures answered 400/404 instead of 500 |
| F29 | #223 | DAV path spellings (`%63alendars`, `//`, no trailing slash) bypassed scope checks |
| F30 | #218 | doc drift (handler comments, SMTP settings in CONFIGURATION.md); a test now guards it |
| F31 | #224 | credential names/scopes/expiry were not validated |

Also fixed earlier: F01 (password hash leak via shared address books, #181), P02
(access tokens kept working after logout / password change, #189), P03 (owners'
numeric user ids in collection JSON, #190), the two-UID CalDAV PUT 500 (#194).

## Observations without a failing test

- PROPFIND on a path directly under the principal that is no collection home
  answers 404 since F11/F29; `/dav/{other}/` still answers with the caller's own
  principal on purpose (the name is never looked up).
- DAV Basic auth bcrypt-checks every one of the user's app passwords (~250 ms each):
  login cost grows with their number, and anyone knowing an e-mail can trigger it.
- An unknown `timezone` value on REST/MCP event writes is silently ignored, not a 400.
- `DELETE /carddav-credentials/<unknown uuid>` returns a 500 (nil pointer); revoking a
  CardDAV credential twice answers 204 (CalDAV: 404).
- CalDAV PUT does not add missing VTIMEZONEs and CalDAV PUT stores a card with VERSION
  2.1; `.vcf`/calendar import silently ignores VJOURNAL; `create_contact.go` still uses
  `uid + ".vcf"` for client-supplied UIDs.
- Calendar DELETE is not atomic (the calendar is deleted before shares are revoked);
  the address-book share handlers still map every error to 400; the last-calendar guard
  answers 400 (409 would fit better).
- CORS `allow_credentials` is documented as default `true`; the code default is `false`.
- Data backfills in `data_migrations.go` use `LIKE` and match case differently per driver.
