# Web Interface (Frontend)

The web interface is a **Nuxt 3 SPA** (single-page application) providing a browser-based UI for the CalDAV/CardDAV server. It uses Vue 3 Composition API with TypeScript in strict mode.

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
│   └── contacts/
│       └── index.vue        # Contact list with search, grouping, detail panel
├── components/              # Auto-imported by Nuxt (directory prefix = component name)
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
│   │   ├── ContactDetailPanel.vue # Full contact details (right panel on desktop, dialog on mobile)
│   │   └── AlphabetNavigation.vue # A-Z letter strip for quick scrolling
│   └── common/
│       ├── AppHeader.vue          # Top bar with hamburger toggle
│       ├── AppSidebar.vue         # Main navigation sidebar (Calendar, Contacts, Settings)
│       ├── HighlightText.vue      # Search term highlighter using <mark> tags
│       ├── LoadingSpinner.vue     # Centered spinner
│       └── SkeletonList.vue       # Loading placeholder rows
├── stores/                  # Pinia stores (state + getters + actions)
│   ├── auth.ts              # Auth state, login/register/logout/refresh, token scheduling
│   ├── calendars.ts         # Calendar + event CRUD, visibility toggling, FullCalendar integration
│   └── contacts.ts          # Address book + contact state, search, sorting, letter grouping
├── composables/
│   ├── useApi.ts            # $fetch wrapper with JWT auth + response unwrapping
│   └── useAppToast.ts       # Toast notification helpers (success/error/warn/info)
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
│   └── api.ts               # ApiResponse, ApiError, ValidationError, PaginatedResponse
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
- **ContactDetailPanel**: On desktop, shown as a right panel. On mobile, shown as a dialog.
- **Virtual scrolling**: Manual implementation with computed offsets and a scroll container.

### Type System Gotchas

**AddressBook vs Calendar field naming**: AddressBook types use GORM-style PascalCase (`ID`, `UUID`, `Name`, `CreatedAt`) because the backend returns raw GORM models. Calendar/Event types use snake_case (`id`, `name`, `created_at`) because they go through DTOs. This inconsistency comes from the backend — don't try to "fix" it on the frontend side.

**`noUncheckedIndexedAccess`**: TypeScript strict mode means:
- `array[0]` returns `T | undefined` — use `array[0]!` when you know it exists, or handle the undefined case.
- `string[0]` returns `string | undefined` — use `.charAt(0)` instead which always returns `string`.
- Vuelidate: `v$.field.$errors[0]?.$message` needs optional chaining.

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
pnpm dev              # Dev server (hot reload)
pnpm nuxt typecheck   # TypeScript check (must pass with zero errors)
pnpm build            # Production build
```

The `NUXT_PUBLIC_API_BASE_URL` environment variable must point to the running backend (default: `http://localhost:8080`).

## Request Flow

1. User navigates to a route → Nuxt middleware (`auth.ts` or `guest.ts`) checks auth state.
2. Page component mounts → calls store actions to fetch data.
3. Store action → uses `useApi()` → `$fetch` with auth headers → backend API.
4. Response flows back through `useApi`'s `onResponse` (unwraps `SuccessResponse` if applicable) → store state updates → Vue reactivity updates the UI.
5. User interactions → store actions for mutations → API calls → toast notifications on success/error.
