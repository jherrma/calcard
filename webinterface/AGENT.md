# Web Interface (Frontend)

The web interface is a **Nuxt 4 SPA** (single-page application) providing a browser-based UI for the CalDAV/CardDAV server. It uses Vue 3 Composition API with TypeScript in strict mode.

## Architecture Overview

The frontend follows a layered architecture:

```
Pages (route-level views)
  └── Components (reusable UI pieces)
        └── Stores (Pinia — centralized state + API calls)
              └── Composables (useApi, useAppToast — shared utilities)
                    └── Types (TypeScript interfaces matching backend DTOs)
```

**Data flows one way**: pages mount → call store actions → stores call `useApi()` → backend responds → store state updates → Vue reactivity re-renders components.

## Directory Structure

```
webinterface/
├── pages/                   # File-based routing (each .vue → a route)
│   ├── index.vue            # Root redirect
│   ├── auth/                # Authentication flows
│   │   ├── login.vue        # Email/password + OAuth login
│   │   ├── register.vue     # New account registration
│   │   ├── setup.vue        # First admin setup (shown when no admin exists)
│   │   ├── forgot-password.vue
│   │   ├── reset-password.vue
│   │   ├── verify.vue       # Email verification
│   │   └── oauth/
│   │       └── callback.vue # OAuth provider callback handler
│   ├── calendar/
│   │   └── index.vue        # FullCalendar view with event CRUD dialogs
│   ├── contacts/
│   │   └── index.vue        # Contact list with search, grouping, detail panel
│   ├── search.vue           # Full global-search results page (?q=…), uncapped
│   └── settings/        # …
│       └── appearance.vue   # Theme + accent colour picker (story 046)            # Profile, password, credentials, connections, import/export, admin, danger
│       └── about.vue        # Open Source Attribution (story 101)
├── components/              # Auto-imported by Nuxt (directory prefix = component name)
│   ├── about/
│   │   └── OpenSourceList.vue     # → <AboutOpenSourceList>: filtered, incrementally rendered package list
│   ├── auth/
│   │   └── PasswordStrength.vue   # → <AuthPasswordStrength>
│   ├── calendar/
│   │   ├── CalendarSidebar.vue    # Calendar list with visibility toggles
│   │   ├── CalendarToolbar.vue    # View switcher (month/week/day), navigation
│   │   ├── EventForm.vue          # Shared form for create/edit (summary, dates, recurrence)
│   │   ├── EventCreateDialog.vue  # Dialog wrapping EventForm for new events
│   │   ├── EventEditDialog.vue    # Dialog wrapping EventForm for editing
│   │   ├── EventDetailDialog.vue  # Read-only event details
│   │   └── RecurrenceScopeDialog.vue  # "This event / All events / This and future"
│   ├── contacts/
│   │   ├── ContactsSidebar.vue    # Address book list with checkboxes
│   │   ├── ContactListItem.vue    # Single contact row (avatar, name, email, actions)
│   │   └── AlphabetNavigation.vue # A-Z letter strip for quick scrolling
│   └── common/
│       ├── AppHeader.vue          # Top bar with hamburger toggle + global search trigger
│       ├── AppSidebar.vue         # Main navigation sidebar (Calendar, Contacts, Settings)
│       ├── GlobalSearch.vue       # Cmd/Ctrl+K search palette (trigger + dialog)
│       ├── SearchSectionHeader.vue # Category header + count for a search result group
│       ├── SearchViewAll.vue      # "View all N …" link into /search
│       ├── HighlightText.vue      # Search term highlighter using <mark> tags
│       ├── ThemeToggle.vue        # Light/Dark/System switcher (story 046)
│       ├── LoadingSpinner.vue     # Centered spinner
│       └── SkeletonList.vue       # Loading placeholder rows
├── stores/                  # Pinia stores (state + getters + actions)
│   ├── auth.ts              # Auth state, login/register/logout/refresh, token scheduling
│   ├── calendars.ts         # Calendar + event CRUD, visibility toggling, FullCalendar integration
│   ├── contacts.ts          # Address book + contact state, search, sorting, letter grouping
│   ├── dashboard.ts         # Dashboard event window (loadedMonths), recent contacts, clock
│   ├── preferences.ts       # User preferences (event duration, all-day, time format, accent colour)
│   ├── search.ts            # Global search fan-out (events/contacts/collections), cache, recents
│   └── sharing.ts           # Share CRUD for calendars AND address books, calendar public link
├── composables/
│   ├── useApi.ts            # $fetch wrapper with JWT auth + response unwrapping
│   ├── useAppToast.ts       # Toast notification helpers (success/error/warn/info)
│   ├── useTheme.ts          # Light/Dark/System state — SINGLETON, see Theming below
│   ├── useAccentColor.ts    # Accent colour — SINGLETON, server-backed, see Theming below
│   └── useOpenSourceAttribution.ts  # Loads both attribution manifests + filterOpenSourcePackages()
├── middleware/
│   ├── auth.ts              # Requires authentication (redirects to /auth/login)
│   └── guest.ts             # Requires unauthenticated (redirects away from auth pages)
├── layouts/
│   ├── default.vue          # App shell: sidebar + header + Toast + ConfirmDialog + loading overlay
│   └── auth.vue             # Centered card with CalCard branding
├── types/
│   ├── auth.ts              # User, LoginResponse, RefreshResponse, SystemSettings, AuthMethod
│   ├── calendar.ts          # Calendar, CalendarEvent, RecurrenceRule, EventFormData
│   ├── contacts.ts          # AddressBook, Contact, ContactEmail/Phone/Address/URL
│   ├── about.ts             # OpenSourcePackage, OpenSourceManifest, UNKNOWN_LICENSE
│   ├── api.ts               # ApiResponse, ApiError, ValidationError, PaginatedResponse
│   └── search.ts            # EventHit, ContactHit, SearchResults (global search)
├── utils/                   # Auto-imported pure helpers (no Nuxt context needed)
│   ├── agendaLayout.ts      # Lane assignment for overlapping blocks on the day timeline
│   ├── contactAvatar.ts     # Contact initials + deterministic avatar colour
│   ├── dashboardDates.ts    # Local-date primitives + month-range merging for the dashboard
│   ├── sanitizeUrl.ts       # Allowlists URL schemes before binding to an href (#27)
│   ├── searchFormat.ts      # Shared row labels for the search palette + results page
│   ├── accent.ts            # Accent palette generation + hex parsing (story 046)
│   ├── theme.ts             # Theme constants + pure helpers (also imported by nuxt.config)
│   └── vcardDate.ts         # vCard date parsing/formatting
├── plugins/
│   ├── primevue-services.ts # Registers ConfirmationService (for useConfirm)
│   └── theme.client.ts      # Starts useTheme() app-wide at boot (story 046)
├── assets/css/
│   ├── tailwind.css         # Tailwind layer entry (@layer tailwind-base, primevue, …)
│   ├── theme.css            # Unlayered: theme-switch transition + pre-paint background
│   └── fullcalendar.css     # FullCalendar overrides, imported by pages/calendar/index.vue
├── test/support/
│   └── storage.ts           # In-memory localStorage for specs (see Testing)
├── nuxt.config.ts           # Nuxt configuration, PrimeVue theme preset, module registration
├── tailwind.config.ts       # Tailwind CSS configuration (darkMode is keyed to .dark-mode)
└── package.json             # Dependencies and scripts
```

## Key Concepts

### Authentication Flow

1. On app load, `auth` middleware calls `authStore.initAuth()` which reads the refresh token from a cookie.
2. If a refresh token exists, it calls `/api/v1/auth/refresh` to get a new access token.
3. Access tokens are stored in Pinia state (memory only — not localStorage). Refresh tokens are stored in a cookie (7-day expiry).
4. `scheduleTokenRefresh()` sets a `setTimeout` to refresh the access token 1 minute before it expires (using `expires_at` Unix timestamp from the backend).
5. The `useApi()` composable automatically attaches `Authorization: Bearer <token>` to every request.
6. On 401 response, `useApi`'s `onResponseError` attempts a token refresh. On failure, auth state is cleared and user is redirected to login.

### Store Pattern

All three stores (`auth`, `calendars`, `contacts`) follow the same pattern:

```typescript
export const useXxxStore = defineStore('xxx', {
  state: (): XxxState => ({ ... }),
  getters: { ... },  // Derived/computed data
  actions: {
    async fetchSomething() {
      const api = useApi();              // Get the configured $fetch instance
      const response = await api<Type>('/api/v1/...'); // Typed API call
      this.someState = response.data;    // Update reactive state
    }
  }
});
```

- `useApi()` must be called **inside** actions (not at store top-level) because it depends on Nuxt context.
- Error handling: wrap in try/catch, set `this.error`, use `useAppToast()` for user-facing notifications.
- Optimistic updates: after a mutation (create/update/delete), update local state immediately rather than refetching.

### API Response Handling

The backend has two different response patterns:

1. **Standard endpoints** (auth, calendars, events, system): Responses wrapped in `{ "status": "ok", "data": ... }`. The `useApi` composable's `onResponse` handler automatically unwraps `.data`, so store code receives the inner payload directly.

2. **AddressBook/Contact endpoints**: Return raw JSON (NOT wrapped). Specific shapes:
   - `GET /api/v1/addressbooks` → `{ "addressbooks": [...] }`
   - `GET /api/v1/addressbooks/:id/contacts` → `{ "Contacts": [...], "Total", "Limit", "Offset" }` (note capital `C` in `Contacts`)
   - `GET /api/v1/contacts/search?q=...` → `{ "contacts": [...], "query", "count" }` (lowercase `c`).
     The corpus is every address book the user can read, **shared ones included** (#162) — so the
     contacts page and the global palette agree about whether a contact exists. `contacts` is
     always an array, never `null`. Optional `addressbook_id` narrows to one book by UUID and
     **404s** for a book the user can't read, rather than silently searching all of them.
   - `DELETE` endpoints → 204 No Content (no response body)

### Calendar Page Architecture

The calendar page (`pages/calendar/index.vue`) integrates FullCalendar:

- **CalendarSidebar**: Lists calendars with colored checkboxes. Toggling visibility updates `visibleCalendarIds` Set in the store.
- **CalendarToolbar**: Month/Week/Day view switcher, prev/next/today navigation. Controls FullCalendar via template ref.
- **Event dialogs**: Create, edit, and detail dialogs are separate components. For recurring events, a `RecurrenceScopeDialog` asks the user whether to modify "this event", "this and future", or "all events".
- **Drag-and-drop**: FullCalendar's `@eventDrop` and `@eventResize` call `calendarStore.updateEventTime()` for direct time changes.
- **Date formatting**: `toRFC3339()` utility in `stores/calendars.ts` preserves local timezone offset (not UTC) so the backend can attach the correct IANA timezone.

### Contacts Page Architecture

The contacts page (`pages/contacts/index.vue`) uses a custom list layout:

- **ContactsSidebar**: Address book checkboxes filter which contacts are shown.
- **Search**: 300ms debounced input triggers `contactsStore.searchContacts()` which calls the backend search API.
- **Grouping**: Contacts are grouped by first letter of `formatted_name` via the `groupedContacts` getter (returns `Map<string, Contact[]>`).
- **AlphabetNavigation**: A-Z letter strip. Only letters with contacts are clickable. Clicking scrolls to that section.
- **Contact detail**: Full contact details live on the route page `pages/contacts/[id]/index.vue` (navigated to when a contact is selected).
- **Virtual scrolling**: Manual implementation with computed offsets and a scroll container.

### Global Search (story 044)

`components/common/GlobalSearch.vue` lives in `AppHeader` and owns both the trigger and
the palette dialog. `pages/search.vue` is the uncapped results page it links to.

- **`GET /api/v1/search` is the single source of results (#156).** `stores/search.ts` issues ONE
  request per query — `?q=&limit=100` — covering events, contacts, calendars and address books
  across every collection the user can read, owned and shared. **There is no date window**: an
  event two years out or two years back is findable. The old ±6-month per-calendar fan-out and
  its `SEARCH_WINDOW_MONTHS` constant are gone.
- **Hits are self-contained.** Each event item carries `calendar_uuid` / `calendar_name` /
  `calendar_color` and each contact item its `addressbook_name`, so the palette needs neither the
  calendar nor the contacts store loaded. Raw JSON, no `{ status, data }` envelope.
- **A recurring series is ONE hit**, represented by the occurrence the server resolved (next one,
  or the last one for a finished series) and carrying that occurrence's `recurrence_id`.
- **Each group is capped and says so.** `limit` applies per category, the server clamps it to
  `max_limit` (100), and each group reports `has_more` — surfaced as `results.hasMore.<category>`
  so counts render as "N+" instead of claiming a total the server never promised.
- **`requestSeq`** in the store drops responses from superseded queries; `reset()` bumps it
  so closing the dialog invalidates in-flight work. Still needed with one request: `useApi`
  retries once on a 401 and spends a whole token refresh before re-issuing.
- **The debounce cannot be cancelled.** vueuse's `useDebounceFn` exposes no cancel handle and
  only re-arms when invoked again, so both the palette and the results page re-read the live
  query inside the debounced callback and bail if it is no longer searchable. Never close over
  the value that scheduled the timer.
- **A failure is an error, not "no matches".** `store.error` is set and rendered as a banner;
  results are cleared. An empty Contacts section after a 500 would read as "this person isn't in
  my address book", which is worse than an error.
- **Keyboard**: Cmd/Ctrl+K opens it from anywhere (window listener), Up/Down walk one flat
  listbox across all categories, Enter opens the highlighted row, Tab/Shift+Tab jump between
  categories. Rows are `role="option"` with `aria-activedescendant` on the input.
- **Deep links**: choosing an event navigates to
  `/calendar?date=&event=&cal=<numeric id>[&recurrence=<recurrence_id>]`. The calendar page
  resolves a recurring occurrence with `calendarStore.fetchEventOccurrence()` — `GET
  /events/:id` returns the series MASTER and does no recurrence expansion, so it must never be
  used to open an occurrence. Contacts use the NUMERIC `?ab=` id (`Contact.addressbook_id`).

### Theming (story 046)

Light / Dark / System, switchable from the header on every shell (including the logged-out auth
pages) and from `pages/settings/appearance.vue`.

- **One class drives everything: `.dark-mode` on `<html>`.** Four places have to agree on it and
  `utils/theme.spec.ts` asserts that they do: `tailwind.config.ts`
  (`darkMode: ['selector', '.dark-mode']`), `nuxt.config.ts` (PrimeVue's `darkModeSelector`),
  `assets/css/fullcalendar.css`, and `utils/theme.ts` itself.
- **This was previously broken in two ways** — worth knowing, because the symptoms look like
  ordinary styling bugs. `tailwind.config.ts` had no `darkMode` key, so Tailwind defaulted to
  `media`: its ~550 `dark:` utilities followed the OS while PrimeVue followed a class nothing set,
  and an OS-dark user got dark Tailwind surfaces behind light PrimeVue components. Separately,
  `fullcalendar.css` targeted a bare `.dark`, so the dark calendar grid had never rendered at all.
- **`useTheme()` is a module-level SINGLETON**, unlike every other composable here — the theme
  belongs to the document, not to a component, and the header, the settings page and the boot
  plugin all read and write the same state. It self-initializes on first call and is idempotent
  after that. Specs therefore need `vi.resetModules()` + a dynamic `import()` per test, or state
  leaks between them.
- **`themeMode` is a writable computed whose setter is the only way in**, so persistence cannot be
  forgotten by a caller assigning through `v-model`. `resolvedTheme` / `isDark` collapse `system`
  against the live OS preference.
- **The preference is device-local (localStorage), not a user preference on the server.** That is
  deliberate: the theme has to apply on the login page before any token exists, and it has to
  apply before the first paint, neither of which an API round-trip can do. It also means each
  device keeps its own — which is usually what people want from a theme.
- **No flash of the wrong theme.** An inline script in `app.head.script` (built in `nuxt.config.ts`)
  sets the class synchronously before the stylesheet or the bundle load. It **imports its constants
  from `utils/theme.ts`** so the storage key cannot drift, but its *logic* is necessarily duplicated
  — keep it in step with `readStoredThemeMode` + `resolveTheme`. Anything that is not exactly
  `'dark'` or `'light'` (junk, missing key) counts as `'system'` in both.
- **Transitions are opt-in per switch**, not global: `useTheme()` adds `.theme-switching` for 200ms
  around a change and `assets/css/theme.css` hangs the transition off that, under a
  `prefers-reduced-motion` guard. It is deliberately NOT applied during startup — animating there
  would mean transitioning *from* the wrong colours, i.e. reintroducing the flash.
- **`color-scheme` is set alongside the class**, which is what makes scrollbars, native form
  controls and the canvas behind the page follow the theme. There is no scrollbar CSS.
- **`surface-*` was dead for the entire life of the repo.** It is used ~440 times but was never a
  defined Tailwind colour, so every one of those utilities compiled to NOTHING — three quarters of
  the app's `dark:` styling had no effect, and the surfaces you saw came from PrimeVue's component
  CSS alone. It is now defined against CSS variables in `theme.css` carrying **PrimeVue's own
  Material surface palette** (slate in light, zinc in dark, exactly as the preset defines it), so
  the two systems cannot disagree. A missing colour produces no error, just no CSS, which is why
  `utils/theme.spec.ts` asserts the scale exists and that every variable it references is defined.
- **Muted text is `text-surface-500 dark:text-surface-400`.** Both halves of that pair matter:
  surface-400 alone is 2.6:1 on a light card and surface-500 alone is 3.7:1 on a dark one, so
  either on its own fails AA in one theme. 141 sites were missing the dark half and 11 had the pair
  inverted (the worst of both). Contrast below AA now survives only where WCAG exempts it —
  disabled controls and decorative empty-state icons (`text-surface-300 dark:text-surface-600`).

### Accent colour (story 046)

The second half of theming, and the one that persists DIFFERENTLY from the theme.

- **The accent is a SERVER preference (`accent_color`), the theme is device-local.** The theme has
  to apply on the login page before a token exists and before the first paint; the accent has
  neither constraint and no reason to differ between a laptop and a phone. `utils/accent.ts` +
  `composables/useAccentColor.ts`; the value lives in `stores/preferences.ts` like any other key.
- **The server validates it by PATTERN, not by a closed set** — the first preference key that does.
  See `patternPreferences` in `server/internal/domain/user/preference.go`. It is normalized
  (trimmed, lower-cased) before validation and before storage, so `"#8B5CF6"` reads back as
  `"#8b5cf6"` and the swatch row's equality check cannot be defeated by casing. Three-digit
  shorthand is expanded CLIENT-side (`normalizeAccentColor`); the server rejects it.
- **One hex drives two colour systems.** Tailwind `primary-*` reads `--accent-<shade>` CSS
  variables holding `R G B` CHANNEL TRIPLETS — the triplet form is load-bearing, because the config
  wraps them as `rgb(var(--accent-500) / <alpha-value>)` and the app really does use
  `bg-primary-900/20`. PrimeVue gets the same ramp through `updatePrimaryPalette()`.
- **The default ramp is Tailwind's blue spelled out, not generated.** `palette()` produces a good
  scale but not an identical one (blue-700 comes out `#295bac` against Tailwind's `#1d4ed8`), so
  generating the default would restyle every screen for users who never touch the setting.
  `accentPalette()` special-cases it; custom colours are generated.
- **There is a device-local CACHE (`calcard-accent`) that is a paint hint, never the truth.** It is
  written only from a loaded server value, dropped on logout so the next account on a shared
  machine does not inherit a colour, and deliberately NOT dropped at boot — `isAuthenticated` is
  still false then because `initAuth()` has not run, and treating that as a logout wiped the cache
  on every page load.
- **A save applies optimistically and rolls back** if the server refuses, so the screen never shows
  a colour the account does not have.

### Type System Gotchas

**AddressBook vs Calendar field naming**: AddressBook types use GORM-style PascalCase (`ID`, `UUID`, `Name`, `CreatedAt`) because the backend returns raw GORM models. Calendar/Event types use snake_case (`id`, `name`, `created_at`) because they go through DTOs. This inconsistency comes from the backend — don't try to "fix" it on the frontend side.

**`noUncheckedIndexedAccess`**: TypeScript strict mode means:
- `array[0]` returns `T | undefined` — use `array[0]!` when you know it exists, or handle the undefined case.
- `string[0]` returns `string | undefined` — use `.charAt(0)` instead which always returns `string`.
- Vuelidate: `v$.field.$errors[0]?.$message` needs optional chaining.

### Open Source Attribution (story 101)

`pages/settings/about.vue` shows two lists, both shaped like `OpenSourceManifest`:

- **Backend** — `GET /api/v1/about/open-source` (authenticated, envelope-wrapped, so `useApi` unwraps it). Generated by `go run ./tools/genlicenses` in `server/` and embedded in the Go binary.
- **Frontend** — `public/open-source.json`, generated by `pnpm gen:licenses` (`scripts/gen-licenses.mjs`, zero dependencies, reads the installed `node_modules` metadata). It is fetched with NATIVE `fetch`, not `useApi`/`$fetch`: it is a static asset of this SPA, not an API call, and Nuxt's `$fetch` is injected at transform time so specs cannot stub it.

Both manifests are COMMITTED — the page works without ever running a generator. Re-run both after a dependency change; their output is deterministic (sorted, no timestamps), so an unchanged dependency set yields an empty diff. `server/Dockerfile` re-runs both but tolerates failure, since the committed files are authoritative.

A license of `"unknown"` means it could not be detected/was not declared — never render it as "unlicensed". The generators' `note` field carries that caveat and the page prints it.

## Nuxt Configuration

Key settings in `nuxt.config.ts`:

- **SPA mode**: `ssr: false` — no server-side rendering.
- **Modules**: `@pinia/nuxt`, `@primevue/nuxt-module`, `@nuxtjs/tailwindcss`, `@vueuse/nuxt`.
- **PrimeVue**: Material preset with blue primary, custom border radius scale, CSS layer ordering with Tailwind.
- **API base URL**: `NUXT_PUBLIC_API_BASE_URL` env var (defaults to `http://localhost:8080`).
- **TypeScript**: Strict mode enabled.
- **PrimeVue components**: Explicitly listed in `include` array (tree-shaking).

## Auto-imports (Critical)

Nuxt auto-imports the following — **do NOT add explicit imports** for these:

| What | Provided by | Wrong import |
|------|-------------|--------------|
| `defineStore` | `@pinia/nuxt` | ~~`import { defineStore } from 'pinia'`~~ |
| `useDebounceFn`, `useMediaQuery`, etc. | `@vueuse/nuxt` | ~~`import { ... } from '@vueuse/core'`~~ |
| `ref`, `computed`, `reactive`, `watch`, `onMounted`, etc. | Nuxt (Vue) | ~~`import { ref } from 'vue'`~~ |
| Components in `components/` | Nuxt | ~~`import X from '~/components/...'`~~ |
| Composables in `composables/` | Nuxt | ~~`import { useApi } from '~/composables/...'`~~ |
| `useRoute`, `useRouter`, `navigateTo`, `useCookie` | Nuxt | ~~`import { ... } from 'vue-router'`~~ |
| `useToast`, `useConfirm` | `@primevue/nuxt-module` | (these are OK to use directly) |

Explicit imports from `@vueuse/core` or `pinia` will cause "module not found" errors because only the Nuxt wrappers are installed.

## Styling Guide

- **Framework**: Tailwind CSS 3 + PrimeVue 4.x Material preset.
- **Colors**: Use `surface-*` scale (`surface-0` through `surface-950`). Always include `dark:` variants.
- **Primary color**: Blue (`primary-50` through `primary-950`).
- **Dark mode**: `.dark-mode` on `<html>`, driven by `useTheme()`. Tailwind and PrimeVue are both
  keyed to that one class — see the **Theming (story 046)** section before touching either config.
- **Icons**: PrimeIcons via `pi pi-*` classes (e.g., `pi pi-calendar`, `pi pi-users`, `pi pi-search`).
- **Sidebar width**: `w-64` (256px). `hidden lg:flex` for responsive behavior.
- **Border style**: `border-surface-200 dark:border-surface-800` for dividers.
- **Rounded elements**: Buttons are pill-shaped (`borderRadius: '2rem'`), cards are `xl` rounded, inputs are `md` rounded.
- **PrimeVue design tokens**: Component overrides require `root` nesting: `card: { root: { borderRadius: '...' } }`.

## Common Patterns

### Page Setup
```vue
<script setup lang="ts">
definePageMeta({ layout: 'default', middleware: 'auth' });
// Auth pages use: layout: 'auth', middleware: 'guest'
</script>
```

### Form Validation (Vuelidate)
```vue
<script setup lang="ts">
import { useVuelidate } from '@vuelidate/core';
import { required, email } from '@vuelidate/validators';

const form = reactive({ email: '', password: '' });
const rules = { email: { required, email }, password: { required } };
const v$ = useVuelidate(rules, form);

async function handleSubmit() {
  const valid = await v$.value.$validate();
  if (!valid) return;
  // proceed...
}
</script>
```

### Toast Notifications
```typescript
const toast = useAppToast();
toast.success('Contact saved');
toast.error('Failed to delete calendar');
```

### Confirmation Dialogs
```typescript
const confirm = useConfirm();
confirm.require({
  message: 'Are you sure?',
  header: 'Delete Contact',
  icon: 'pi pi-exclamation-triangle',
  acceptClass: 'p-button-danger',
  accept: () => { /* delete logic */ },
});
```

## Development

```bash
pnpm dev                        # Dev server (hot reload)
pnpm nuxt typecheck && pnpm test # Verification gate (typecheck must pass, then all Vitest specs)
pnpm test                       # Vitest specs (run once) — co-located *.spec.ts
pnpm test:watch                 # Vitest specs (watch mode)
pnpm build                      # Production build
pnpm gen:licenses               # Regenerate public/open-source.json (story 101)
```

### Testing

- **Runner**: Vitest with the `@nuxt/test-utils` Nuxt environment (happy-dom). Config in `vitest.config.ts` via `defineVitestConfig` — this wires Nuxt aliases (`~/…`) and auto-imports into specs.
- **Specs are co-located** next to sources as `*.spec.ts`.
- **Mock auto-imports in tests** with `mockNuxtImport('useApi', () => …)` from `@nuxt/test-utils/runtime`. `$fetch` is a Nuxt global (not an unimport auto-import) — stub it with `vi.stubGlobal('$fetch', …)`.
- **Store setup**: use `createTestingPinia({ stubActions: false })` from `@pinia/testing` (it sets the active pinia and keeps real action logic so you assert behavior, not that an action was called). Prefer importing this over `pinia` directly, which does not resolve under `vue-tsc`.
- Keep specs hermetic: no network (mock `useApi`/`$fetch`), and shallow-mount heavy PrimeVue components (assert logic, not PrimeVue internals).
- **`window.localStorage` does not work in this environment.** It is a plain empty object with no
  `getItem`/`setItem`/`clear` — Node 22's built-in Web Storage shadows happy-dom's and stays inert
  without `--localstorage-file` (hence the warning printed on every run). Use
  `installMemoryStorage()` / `installThrowingStorage()` from `test/support/storage.ts`. The
  throwing variant models Safari private mode and blocked cookies, where the property *access*
  itself throws rather than returning null.

The `NUXT_PUBLIC_API_BASE_URL` environment variable must point to the running backend (default: `http://localhost:8080`).

## Request Flow

1. User navigates to a route → Nuxt middleware (`auth.ts` or `guest.ts`) checks auth state.
2. Page component mounts → calls store actions to fetch data.
3. Store action → uses `useApi()` → `$fetch` with auth headers → backend API.
4. Response flows back through `useApi`'s `onResponse` (unwraps `SuccessResponse` if applicable) → store state updates → Vue reactivity updates the UI.
5. User interactions → store actions for mutations → API calls → toast notifications on success/error.
