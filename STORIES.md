# Story Implementation Tracker

Status vocabulary: **Done** — acceptance criteria met. **Partial** — substantially built, usually as
a side effect of a neighbouring story; the story file's `## Implementation status` section says
exactly what is ticked and what is left. **Pending** — no code exists.

A story is never the whole picture: features filed straight onto the issue tracker never got a story
file. The consolidated list at the bottom merges both.

## Backend Stories (001–030)

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
| 030 | API Documentation                          | Done   |

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
| 046 | Dark Mode & Theming                        | Partial |
| 047 | Accessibility (a11y)                       | Partial |
| 049 | Responsive Design & Mobile Optimization    | Partial |
| 050 | PWA & Push Notifications                   | Pending |

## Continuation Stories (042–043, 100+)

| #   | Story                                      | Status  |
| --- | ------------------------------------------ | ------- |
| 042 | Dashboard Home Page                        | Done    |
| 043 | Sharing Management UI                      | Done    |
| 100 | Remote Calendar Subscriptions              | Pending |
| 101 | Open Source Attribution                    | Done    |
| 103 | Event Default Settings                     | Done    |
| 104 | MCP Server Integration                     | Pending |

## Summary

- **Done**: 45 / 52
- **Partial**: 4 / 52 — 045 Error Handling, 046 Dark Mode, 047 Accessibility, 049 Responsive
- **Pending**: 3 / 52 — 050 PWA & Push, 100 Remote Calendar Subscriptions, 104 MCP Server

<!-- Counts are of the rows in the three tables above (29 backend + 17 frontend + 6
     continuation = 52). The denominator used to read 57, which never matched the
     tables — it kept counting stories that are not listed here (there is no 012,
     and 048/102 were dropped). Recount from the tables when you change a status. -->

## What remains

Everything open, stories and issues together, audited 2026-08-04. Ordered by effort within each
group; the issue numbers are GitHub issues, which are the more current source — four features were
filed straight onto the tracker and never got a story file.

### Small, self-contained

| Item | What is actually left |
| --- | --- |
| **046 Dark Mode** (toggle only) | The 551 `dark:` styles already exist but nothing ever sets `.dark-mode`, so dark mode is unreachable today. Needs a `useTheme()` composable, a toggle, a persisted preference and flash prevention. The accent-colour half of the story is separate, larger, and optional. |
| **047 Accessibility** (sweep) | Live regions (none exist), a skip link, `aria-label` on the ~1 in 2 icon-only buttons that lack one, `aria-hidden` on decorative icons, and `for`/`id` pairing in `ContactForm.vue`. Contrast / zoom / tab order need an axe pass before assuming they fail. |
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
| **#68** DAV interop pass | Real clients (iOS/macOS, DAVx⁵, Thunderbird). Blocked on physical devices — cannot be done in CI. |

### Large

| Item | What is actually left |
| --- | --- |
| **100 Remote Calendar Subscriptions** | Whole feature. Needs read-only calendars in the domain model, a refresh scheduler, and failure surfacing. |
| **050 PWA & Push Notifications** | Whole feature, and **not frontend-only**: reminders need a server-side scheduler plus a delivery channel (web push subscriptions, or SMTP which is optional here). `VALARM` round-trips today but nothing fires it. Its offline half overlaps 045 — one story should own the action queue. |
| **104 MCP Server** | Whole feature, but the tool surface maps onto existing use cases; the work is the JSON-RPC transport and per-session auth scoping. |
| **#113** Pluggable persistence | Refactor behind the repository interfaces: GORM plus a direct Turso driver, opening the door to MongoDB. Broadest blast radius of anything open. |

### Known documentation debt

- `stories/**` are pre-code design sketches. They contain invented endpoints and occasional
  React/Vite pseudo-code, and story 046's sketch uses `.dark` where the app is wired for
  `.dark-mode`. Read the code first; the `## Implementation status` sections added on 2026-08-04
  record what was actually verified.
- `server/docs/swagger.*` is committed but stale. Regenerate with
  `swag init -g cmd/server/main.go --parseDependency --parseInternal` from `server/`.
