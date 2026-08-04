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
│   ├── index.vue            # Dashboard: landing page after login (story 042) — NOT a redirect
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
│   └── settings/            # Profile, password, credentials, connections, import/export, admin, danger
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
│   ├── dashboard/                 # Dashboard widgets (story 042) → <DashboardWidgetCard> etc.
│   │   ├── WidgetCard.vue         # Shared card shell (title, icon, badge, actions, footer)
│   │   ├── TodayAgendaCard.vue    # Day timeline with lane-split overlapping events
│   │   ├── UpcomingEventsCard.vue # Next 5-7 events with Today/Tomorrow labels
│   │   ├── MiniCalendarCard.vue   # 6x7 month grid with event dots + month navigation
│   │   ├── RecentContactsCard.vue # Most recently updated contacts
│   │   ├── RecentContactRow.vue   # Single row of the above
│   │   └── QuickStatsCard.vue     # Calendars / events this month / address books / contacts
│   ├── sharing/                   # Resource-agnostic sharing UI (story 043)
│   │   ├── SharePanel.vue         # → <SharingSharePanel>  list/invite/change/revoke, BOTH resource kinds
│   │   ├── PublicLinkPanel.vue    # → <SharingPublicLinkPanel>  calendar public link (calendars only)
│   │   └── ShareDialog.vue        # → <SharingShareDialog>  modal composing both panels
│   └── common/
│       ├── AppHeader.vue          # Top bar with hamburger toggle + global search trigger
│       ├── AppSidebar.vue         # Main navigation sidebar (Calendar, Contacts, Settings)
│       ├── GlobalSearch.vue       # Cmd/Ctrl+K search palette (trigger + dialog)
│       ├── SearchSectionHeader.vue # Category header + count for a search result group
│       ├── SearchViewAll.vue      # "View all N …" link into /search
│       ├── HighlightText.vue      # Search term highlighter using <mark> tags
│       ├── LoadingSpinner.vue     # Centered spinner
│       └── SkeletonList.vue       # Loading placeholder rows
├── stores/                  # Pinia stores (state + getters + actions)
│   ├── auth.ts              # Auth state, login/register/logout/refresh, token scheduling
│   ├── calendars.ts         # Calendar + event CRUD, visibility toggling, FullCalendar integration
│   ├── contacts.ts          # Address book + contact state, search, sorting, letter grouping
│   ├── dashboard.ts         # Dashboard event window (loadedMonths), recent contacts, clock
│   ├── preferences.ts       # User preferences (default event duration, all-day, 12h/24h time format)
│   ├── search.ts            # Global search fan-out (events/contacts/collections), cache, recents
│   └── sharing.ts           # Share CRUD for calendars AND address books, calendar public link,
│                            #   `sharedWithMe` derived from the two list endpoints (story 043)
├── composables/
│   ├── useApi.ts            # $fetch wrapper with JWT auth + response unwrapping
│   ├── useAppToast.ts       # Toast notification helpers (success/error/warn/info)
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
│   └── vcardDate.ts         # vCard date parsing/formatting
├── plugins/
│   └── primevue-services.ts # Registers ConfirmationService (for useConfirm)
├── nuxt.config.ts           # Nuxt configuration, PrimeVue theme preset, module registration
├── tailwind.config.ts       # Tailwind CSS configuration
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
   - `GET /api/v1/contacts/search?q=...` → `{ "contacts": [...], "query", "count" }` (lowercase `c`)
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

### Sharing (story 043)

`stores/sharing.ts` is the single entry point; the three `components/sharing/*` components are thin
views over it. Constraints that shaped it — all imposed by the API:

- **One store, both resource kinds.** `/api/v1/calendars/:uuid/shares` and
  `/api/v1/addressbooks/:uuid/shares` are byte-for-byte identical in request and response, so
  `shareCollectionUrl(type, uuid)` is the only thing that differs. Both are keyed on the resource
  **UUID** (#52) — a numeric id gives a 404.
- **These endpoints return raw JSON**, not the `{ status, data }` envelope: `GET` → `{ shares: [...] }`,
  `POST`/`PATCH` → the bare share object, `DELETE` → 204.
- **Errors carry `{ "error": "..." }`, not `{ "message": ... }`** like the auth handlers. Use
  `shareErrorMessage()`; reading `Error.message` yields ofetch's useless `[POST] …: 400 Bad Request`.
- **There is no user-search endpoint.** Invite by `user_identifier`, which the backend resolves as an
  email *or* a username. Nothing to autocomplete against.
- **There is no `/shared-with-me` endpoint.** `GET /calendars` and `GET /addressbooks` already return
  shared resources carrying `shared` / `permission` / `owner` (#53); `sharedWithMe` derives from those.
- **Share management is owner-only** — a sharee gets 404 from every share endpoint. Never render it
  for a resource with `shared === true`; pass `:can-manage="!resource.shared"`.
- **Public links are calendars-only and have no DELETE route.** `POST …/public` with
  `{ enabled: false }` is how you disable (it also clears the token, so re-enabling mints a new URL).
  `GET …/public` is the *only* source of the public URL — the token is `json:"-"` on the domain model.
- **A failed read must never render as a fact.** Read actions swallow their failure into
  `sharesError` / `publicError` (two fields, because both panels are mounted at once). Any view of
  `shares` / `publicAccess` MUST render those errors, because an empty list after a failed load is
  indistinguishable from "nothing is shared" — and "this calendar is private" is then a false
  statement the owner will act on. Same rule on `pages/settings/sharing.vue`, which has to read
  `calendarStore.error` / `contactsStore.error` back after the load, since neither list action rejects.
- **The store holds exactly ONE resource at a time**, so `fetchShares` / `fetchPublicAccess` carry a
  sequence token and discard superseded responses; mutations bump it too. `useApi`'s
  retry-once-on-401 spends a whole refresh round-trip before re-issuing, so responses really do
  arrive out of order.
- **Refetching a list must not reset the user's view.** `changed` fires on every share mutation and
  the pages refetch on it, so `fetchCalendars` / `fetchAddressBooks` preserve `visibleCalendarIds` /
  `selectedAddressBookIds` for already-known resources (first load still selects everything).
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
- **Dark mode**: Toggled via `.dark-mode` class on the root element.
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

The `NUXT_PUBLIC_API_BASE_URL` environment variable must point to the running backend (default: `http://localhost:8080`).

## Request Flow

1. User navigates to a route → Nuxt middleware (`auth.ts` or `guest.ts`) checks auth state.
2. Page component mounts → calls store actions to fetch data.
3. Store action → uses `useApi()` → `$fetch` with auth headers → backend API.
4. Response flows back through `useApi`'s `onResponse` (unwraps `SuccessResponse` if applicable) → store state updates → Vue reactivity updates the UI.
5. User interactions → store actions for mutations → API calls → toast notifications on success/error.
