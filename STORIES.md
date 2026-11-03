# Story Implementation Tracker

Status vocabulary: **Done** — acceptance criteria met. **Partial** — substantially built, usually as
a side effect of a neighbouring story; the story file's `## Implementation status` section says
exactly what is ticked and what is left. **Pending** — no code exists.

A story is never the whole picture: features filed straight onto the issue tracker never got a story
file. The consolidated list at the bottom merges both.

## Backend Stories (001–029)

All backend stories are implemented.

| #   | Story                                      | Status |
| --- | ------------------------------------------ | ------ |
| 001 | Project Initialization                     | Done   |
| 002 | Configuration Management                   | Done   |
| 003 | Database Connection Layer                  | Done   |
| 004 | Fiber v3 HTTP Server Setup                 | Done   |
| 005 | Database Migration System                  | Done   |
| 006 | User Registration                          | Done   |
| 007 | User Authentication (Local Login)          | Done   |
| 008 | Password Management                        | Done   |
| 009 | User Profile Management                    | Done   |
| 010 | App Passwords for DAV Clients              | Done   |
| 011 | OAuth2/OIDC Authentication                 | Done   |
| 013 | Calendar Management                        | Done   |
| 014 | CalDAV Protocol Implementation             | Done   |
| 015 | WebDAV-Sync for Calendars                  | Done   |
| 016 | Event Management REST API                  | Done   |
| 017 | Address Book Management                    | Done   |
| 018 | Contact Management REST API                | Done   |
| 019 | CardDAV Protocol Implementation            | Done   |
| 020 | WebDAV-Sync for Address Books              | Done   |
| 021 | CalDAV Access Credentials                  | Done   |
| 022 | CardDAV Access Credentials                 | Done   |
| 023 | Calendar Sharing                           | Done   |
| 024 | Address Book Sharing                       | Done   |
| 025 | Public Calendar URLs (iCal Feed)           | Done   |
| 026 | Docker Deployment                          | Done   |
| 027 | Security Configuration                     | Done   |
| 028 | Server Setup Instructions                  | Done   |
| 029 | Import/Export Functionality                 | Done   |

## Frontend Stories (031–050)

| #   | Story                                      | Status  |
| --- | ------------------------------------------ | ------- |
| 031 | Frontend Project Setup                     | Done    |
| 032 | Authentication UI                          | Done    |
| 033 | Layout and Navigation                      | Done    |
| 034 | Calendar Views                             | Done    |
| 035 | Event Management UI                        | Done    |
| 036 | Contact List and Search UI                 | Done    |
| 037 | Contact Management UI                      | Done    |
| 038 | Settings Pages                             | Done    |
| 039 | Calendar and Address Book Settings         | Done    |
| 040 | Client Setup Instructions Page             | Done    |
| 041 | Import/Export UI                           | Done    |
| 044 | Global Search                              | Done    |
| 045 | Error Handling & Loading States            | Partial |
| 046 | Dark Mode & Theming                        | Done    |
| 047 | Accessibility (a11y)                       | Partial |
| 049 | Responsive Design & Mobile Optimization    | Partial |
| 050 | PWA & Push Notifications                   | Pending |

## Continuation Stories (042–043, 100+)

| #   | Story                                      | Status  |
| --- | ------------------------------------------ | ------- |
| 042 | Dashboard Home Page                        | Done    |
| 043 | Sharing Management UI                      | Done    |
| 100 | Remote Calendar Subscriptions              | Done    |
| 101 | Open Source Attribution                    | Done    |
| 103 | Event Default Settings                     | Done    |
| 104 | MCP Server Integration                     | Done    |
| 170 | Tasks (VTODO) and Task Lists               | Done    |

## Summary

- **Done**: 48 / 52
- **Partial**: 3 / 52 — 045 Error Handling, 047 Accessibility, 049 Responsive
- **Pending**: 1 / 52 — 050 PWA & Push

<!-- Counts are of the rows in the three tables above (28 backend + 17 frontend + 7
     continuation = 52; 170 added 2026-11-03). The denominator used to read 57, which never matched the
     tables — it kept counting stories that are not listed here (there is no 012,
     and 048/102 were dropped). It read 52 until story 030 (API Documentation)
     was removed. Recount from the tables when you change a status. -->

## #170 real-client interop (Phase 4 matrix, 2026-11-03)

#170 (tasks and task lists) is Done: the REST API, the DAV component sets, the one-shot narrowing migration, the
VTODO time-range matcher and the Tasks section are covered by the unit, integration and Vitest suites. The manual
client matrix below is only partly done. Its Apple and DAVx⁵ rows stay open under **#68**. Release notes:
`docs/release-notes/170-tasks.md`.

How Thunderbird was tested: Thunderbird 156.0.1 (flatpak, build 20260922151802) ran headless on Linux against a
local CalCard built from this branch (SQLite). It was driven over Marionette through its own CalDAV provider and
UI code: the New Calendar wizard's network detection, the Tasks tab, the task tree checkbox, quick-add, the context
menu and the New Task/Event dialogs. A logging proxy recorded every DAV request. The web side was checked over the
REST API and in the built web UI. Apple Reminders and Tasks.org via DAVx⁵ were not run: there is no Apple device and
no Android device or emulator in this environment.

| Matrix row | Client and version | Result |
| --- | --- | --- |
| Apple Reminders (iOS/macOS) | — | **NOT VERIFIED** (no Apple device) |
| Tasks.org via DAVx⁵ | — | **NOT VERIFIED** (no Android device or emulator) |
| Thunderbird (CalDAV, Tasks view) | Thunderbird 156.0.1 | **Pass.** A `kind:"tasks"` list appears in the Tasks tab with its colour (`calendar-color`). The event calendar reports tasks unsupported: the New Task dialog's calendar menu leaves it out, quick-add answers "Please select a calendar that supports tasks", and it adds no rows. Thunderbird's left-hand calendar list is shared by its Calendar and Tasks tabs and always shows every calendar; that is Thunderbird's design, not a server setting. "Next Seven Days", "Overdue" and "Completed" list the CalCard tasks, but Thunderbird computes them from its local cache: it **never sends a `calendar-query`**, only PROPFIND, `sync-collection`, `calendar-multiget`, PUT and DELETE. So the story-17 VTODO time-range matcher is not exercised by Thunderbird. Complete and un-complete round-trip both ways, and so do create, priority, due date (timed and all-day), rename and delete. Text with `, ;` stays plain. A REST edit of a Thunderbird-written task keeps `RELATED-TO;RELTYPE=PARENT`, `VALARM`, `X-` properties and the escaped `DESCRIPTION` byte for byte. A Thunderbird repeating task keeps its `RRULE` after a web rename; the web refuses to complete it (409 with the message), and Thunderbird itself refuses to tick a repeating task. A delete from either side is gone from the other after one sync (hard delete). |
| Mixed legacy calendar | Thunderbird 156.0.1 | **Pass** (Apple and DAVx⁵ **NOT VERIFIED**). A `VEVENT,VTODO` calendar keeps both in Thunderbird. Events and tasks written in it from Thunderbird and from the web reach the other side, and the narrowing migration leaves it mixed. |
| Narrowed set pickup | Thunderbird 156.0.1 | **Pass.** Refresh action needed: **restart Thunderbird**. After the migration narrowed one legacy calendar to `VEVENT` and one to `VTODO`, "Synchronize" (a new sync token, ctag bumped) and disabling/re-enabling the calendar left Thunderbird on the old set. In that state Thunderbird offers the calendar for the dropped type, and the server refuses the PUT with 403 `supported-calendar-component`; Thunderbird logs it and does not keep the item. After a restart, the first sync's PROPFIND re-read the set and the dialogs offered only the right kind. Reminders and DAVx⁵ ("Refresh collection list") are **NOT VERIFIED**. |
| Extra: evolution-data-server 3.60.2 | WebDAV discovery only | **Pass.** `e_webdav_discover_sources_sync`, the discovery Evolution and GNOME Online Accounts use, classifies task lists as tasks only, event calendars as events only and mixed calendars as both, with their colours. The Evolution app is not installed and was not tested. |

Findings from the pass. No GitHub issues have been filed for them yet.

- Docs and comments claimed that Thunderbird sends a VTODO `time-range` query. The `.md` docs are corrected. The
  same claim is still in Go comments: `server/internal/adapter/webdav/caldav_vtodo_filter.go` (top comment),
  `server/internal/adapter/webdav/caldav_backend.go` (`QueryCalendarObjects`) and
  `server/integration/caldav_vtodo_report_test.go`.
- go-webdav v0.7.0 answers **201** to a PUT that replaces an existing object (`caldav/server.go`, its own "TODO:
  http.StatusNoContent if the resource already existed"). RFC 4918 expects 200/204. Thunderbird accepts it.
- This is a Thunderbird issue, not CalCard's: once, Thunderbird's in-memory copy of a task kept the pre-update
  priority and due date after a sync, although its `cache.sqlite` already held the server's new version. The next
  change showed correctly.
- Still open from story 17 (go-webdav, not exercised by Thunderbird): `<prop-filter name="COMPLETED"><is-not-defined/>`
  also matches completed tasks, and a VEVENT `time-range` without `start` is ignored.

## What remains

Everything open, stories and issues together, audited 2026-08-04. Ordered by effort within each
group; the issue numbers are GitHub issues, which are the more current source — four features were
filed straight onto the tracker and never got a story file.

### Small, self-contained

| Item | What is actually left |
| --- | --- |
| **047 Accessibility** (sweep) | Live regions (none exist), a skip link, `aria-label` on the ~1 in 2 icon-only buttons that lack one, `aria-hidden` on decorative icons, and `for`/`id` pairing in `ContactForm.vue`. **Colour contrast is now done** (story 046 audited and fixed every surface/text pair in both themes); zoom and tab order still need an axe pass, and it should run in both themes. |
| **#160** Recently-viewed contacts | Frontend only: a per-device list feeding the dashboard widget. |
| **#158** `GET /users/search` | Backend endpoint so share invites can autocomplete instead of requiring an exact email or username. Needs a privacy decision — it exposes an account directory. |

### Medium

| Item | What is actually left |
| --- | --- |
| **045 Error Handling** | A real `NuxtErrorBoundary`, a connectivity indicator, determinate import/export progress (needs an API decision), transient-failure retry, undo. |
| **049 Responsive** | Touch and mobile-native behaviour: bottom nav, swipe paging, pull-to-refresh, 44 px touch targets, day-view default on phones, correct input types, lazy photos. The layout itself is done. |
| **#159** `GET /events/upcoming` | Cross-calendar upcoming events with recurrence expansion — would replace the dashboard's per-calendar fan-out the way #156 replaced the search fan-out. |
| **#157** Per-sharee visibility | Persistent hide/show of a shared collection, honoured over DAV too, without letting a sharee self-leave. Touches the sharing model. |
| **#112** Dependency majors | The safe sweep landed in #149/#150; the tracked major upgrades remain. |
| **#68** DAV interop pass | Real clients (iOS/macOS, DAVx⁵, Thunderbird). **Thunderbird 156.0.1 done for #170 task lists on 2026-11-03** (all its rows pass, including mixed legacy calendars and narrowed-set pickup after a restart; see "#170 real-client interop" above). Still open: Apple Calendar/Reminders and DAVx⁵ (+ Tasks.org), including their narrowed-set refresh action. These need physical devices and cannot be done in CI. |

### Large

| Item | What is actually left |
| --- | --- |
| **050 PWA & Push Notifications** | Whole feature, and **not frontend-only**: reminders need a server-side scheduler plus a delivery channel (web push subscriptions, or SMTP which is optional here). `VALARM` round-trips today but nothing fires it. Its offline half overlaps 045 — one story should own the action queue. |
| **#113** Pluggable persistence | Refactor behind the repository interfaces: GORM plus a direct Turso driver, opening the door to MongoDB. Broadest blast radius of anything open. |

### Known documentation debt

- `stories/**` are pre-code design sketches. They contain invented endpoints and occasional
  React/Vite pseudo-code, and story 046's sketch uses `.dark` where the app is wired for
  `.dark-mode`. Read the code first; the `## Implementation status` sections added on 2026-08-04
  record what was actually verified.
- Story 046's own audit note is worth reading before trusting any "this is styled" claim in the
  other stories: two colour systems and ~440 utilities were silently compiling to nothing, which no
  test and no amount of reading templates would have revealed. Check the BUILT css, not the source.
- `webinterface/AGENT.md` has drifted from its `CLAUDE.md` twin: it predates stories 042/043, so it
  still calls `pages/index.vue` a "root redirect" and lists neither the dashboard nor the sharing
  components, and it has no Sharing section. Both files were updated for 046 on 2026-08-14, but the
  older gap was left alone rather than folded into an unrelated PR. Prefer `CLAUDE.md` where they
  disagree.
