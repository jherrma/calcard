# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a CalDAV/CardDAV server project with a Go backend and Nuxt 3 web interface. The server implements RFC 4791 (CalDAV) and RFC 6352 (CardDAV) protocols for calendar and contact synchronization.

## Project Structure

```
/
├── Overview.md              # High-level project goals and features
├── Technical Overview.md    # Detailed technical architecture
├── Acceptance Criteria.md   # Full list of acceptance criteria
├── stories/                 # User stories for implementation
│   ├── backend/             # Backend stories (Go server)
│   ├── continuation/        # Stories after core features are implemented
│   └── frontend/            # Frontend stories (web interface)
├── server/                  # Go backend implementation
│   ├── cmd/server/          # Application entrypoint
│   ├── configs/             # Configuration examples
│   ├── internal/            # Internal packages
│   │   ├── adapter/         # HTTP handlers, repositories, WebDAV
│   │   ├── domain/          # Domain models and interfaces
│   │   ├── infrastructure/  # Database, email, server setup
│   │   └── usecase/         # Business logic use cases
│   ├── Dockerfile
│   └── docker-compose*.yml
└── webinterface/            # Nuxt 3 SPA frontend
    ├── pages/               # File-based routing
    │   ├── auth/            # Login, register, setup, forgot/reset password, OAuth callback, verify
    │   ├── calendar/        # Calendar view with FullCalendar
    │   └── contacts/        # Contact list with search, grouping, detail panel
    ├── components/
    │   ├── auth/            # PasswordStrength
    │   ├── calendar/        # CalendarSidebar, CalendarToolbar, EventForm, Event*Dialog, RecurrenceScopeDialog
    │   ├── contacts/        # ContactsSidebar, ContactListItem, ContactDetailPanel, AlphabetNavigation
    │   └── common/          # AppHeader, AppSidebar, HighlightText, LoadingSpinner, SkeletonList
    ├── stores/              # Pinia stores: auth, calendars, contacts
    ├── composables/         # useApi (fetch wrapper), useAppToast
    ├── middleware/           # auth (requires login), guest (redirects if logged in)
    ├── layouts/             # default (sidebar + header), auth (centered card)
    ├── types/               # TypeScript interfaces: auth, calendar, contacts, api
    └── plugins/             # PrimeVue service registration (ToastService, ConfirmationService)
```

## AGENT.md and CLAUDE.md Files

`AGENT.md` and `CLAUDE.md` files are placed throughout the project to provide context-specific guidance:

- `/AGENT.md` - Root level project overview
- `/server/AGENT.md` - Server directory structure, startup sequence, API surface
- `/server/internal/AGENT.md` - Backend architecture overview
- `/server/internal/adapter/AGENT.md` - Adapter layer details
- `/server/internal/domain/AGENT.md` - Domain layer details
- `/server/internal/infrastructure/AGENT.md` - Infrastructure layer details
- `/server/internal/usecase/AGENT.md` - Use case layer details
- `/server/internal/usecase/auth/AGENT.md` - Authentication use cases
- `/webinterface/AGENT.md` - Frontend architecture and conventions

- `/CLAUDE.md` - Root level project overview
- `/server/CLAUDE.md` - Server directory structure, startup sequence, API surface
- `/server/internal/CLAUDE.md` - Backend architecture overview
- `/server/internal/adapter/CLAUDE.md` - Adapter layer details
- `/server/internal/domain/CLAUDE.md` - Domain layer details
- `/server/internal/infrastructure/CLAUDE.md` - Infrastructure layer details
- `/server/internal/usecase/CLAUDE.md` - Use case layer details
- `/server/internal/usecase/auth/CLAUDE.md` - Authentication use cases
- `/webinterface/CLAUDE.md` - Frontend architecture and conventions

**Always check relevant AGENT.md files when working in a specific area of the codebase.**

## Development Commands

```bash
# Backend
cd server && go build ./...
cd server && go test ./...

# Frontend
cd webinterface && pnpm dev             # Dev server
cd webinterface && pnpm nuxt typecheck  # TypeScript check (strict mode)
cd webinterface && pnpm build           # Production build

# Docker
cd server && docker-compose up                                    # SQLite
cd server && docker-compose -f docker-compose.postgres.yml up     # PostgreSQL
```

## Key Technologies

- **Backend**: Go 1.22+, Fiber v3, GORM
- **Database**: SQLite (default), PostgreSQL (production)
- **Protocols**: CalDAV (RFC 4791), CardDAV (RFC 6352), WebDAV-Sync
- **Auth**: JWT, OAuth2/OIDC, SAML 2.0
- **Frontend**: Nuxt 3 (SPA mode), Vue 3 Composition API, TypeScript (strict)
- **UI**: PrimeVue 4.x (Material preset), Tailwind CSS 3, PrimeIcons
- **Frontend Libraries**: Pinia (state), VueUse, Vuelidate, FullCalendar

## Frontend Architecture

### Auto-imports (do NOT use explicit imports for these)

- `defineStore` — auto-imported by `@pinia/nuxt`
- `useDebounceFn`, `useMediaQuery`, etc. — auto-imported by `@vueuse/nuxt`
- Vue APIs (`ref`, `computed`, `reactive`, `onMounted`, etc.) — auto-imported by Nuxt
- Components in `components/` — auto-imported by Nuxt (use `<AuthPasswordStrength>`, not `<PasswordStrength>`)

### API Layer

- `useApi()` composable wraps `$fetch.create()` with auth headers and response unwrapping
- Backend wraps most API responses in `{ "status": "ok", "data": ... }` — the `onResponse` handler in `useApi` unwraps `.data` automatically
- **Exception**: AddressBook/Contact endpoints return raw JSON (not wrapped in `SuccessResponse`), so responses come through as-is
- Backend uses `expires_at` (Unix timestamp) for JWT expiry, NOT `expires_in`

### API Response Shapes (Contacts/AddressBooks)

- `GET /api/v1/addressbooks` → `{ "addressbooks": [...] }`
- `GET /api/v1/addressbooks/:id/contacts` → `{ "Contacts": [...], "Total", "Limit", "Offset" }` (capital C)
- `GET /api/v1/contacts/search?q=...` → `{ "contacts": [...], "query", "count" }` (lowercase)
- `DELETE` endpoints return 204 No Content

### Patterns

- **Stores**: Use `useApi()` inside actions, follow `stores/calendars.ts` and `stores/contacts.ts` as reference
- **Composables**: `useApi()`, `useAppToast()`, `useConfirm()` (from PrimeVue)
- **Styling**: Tailwind with `surface-*` colors, `dark:` variants, PrimeVue components
- **Icons**: PrimeIcons (`pi pi-*`)
- **Sidebar pattern**: `w-64`, `hidden lg:flex`, border-right, same bg as CalendarSidebar
- **PrimeVue theming**: Component design tokens require `root` nesting (e.g., `card: { root: { borderRadius: '...' } }`)
- **TypeScript strict mode** with `noUncheckedIndexedAccess` — array `[0]` returns `T | undefined`, use `.charAt(0)` for strings or `!` assertions for known-valid access

### Key Endpoints

- `GET /api/v1/system/settings` — Returns `admin_configured`, `smtp_enabled`, `registration_enabled`
- `GET /api/v1/auth/methods` — Returns available auth methods (local, OAuth providers, SAML)
- Auth pages: login, register, setup (first admin), forgot-password, reset-password, verify, OAuth callback
