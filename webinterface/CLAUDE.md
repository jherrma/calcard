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
│   ├── index.vue            # Dashboard: landing page after login (story 042) — NOT a redirect; asks for the task
│   │                        #   summary itself on mount, Refresh and the 5-minute timer ("Open tasks", #170)
│   ├── auth/                # Authentication flows
│   │   ├── login.vue        # Email/password + OAuth login
│   │   ├── register.vue     # New account registration
│   │   ├── setup.vue        # First admin setup (shown when no admin exists)
│   │   ├── forgot-password.vue
│   │   ├── reset-password.vue
│   │   ├── verify.vue       # Email verification: the activation mail links here; GETs /api/v1/auth/verify?token= (no guest middleware)
│   │   └── oauth/
│   │       └── callback.vue # OAuth provider callback handler
│   ├── calendar/
│   │   └── index.vue        # FullCalendar view with event CRUD dialogs
│   ├── contacts/
│   │   └── index.vue        # Contact list with search, grouping, detail panel
│   ├── tasks/
│   │   └── index.vue        # Tasks section (#170): list sidebar + toolbar + quick-add + TaskList + the task and task-list dialogs;
│   │                        #   ?list=<calendar uuid> selects a list and stays in the URL (a new list is selected, a deleted one leaves it)
│   ├── search.vue           # Full global-search results page (?q=…), uncapped; a task-list hit opens /tasks?list=<uuid>,
│   │                        #   other calendar hits /calendar?cal=<id> (#170)
│   └── settings/            # Profile, password, credentials, MCP access, subscriptions, connections, import/export, admin, danger
│       ├── about-app.vue    # About CalCard: server version (from GET /system/settings) + link to /settings/about
│       ├── about.vue        # Open Source Attribution (story 101)
│       └── appearance.vue   # Theme + accent colour picker (story 046)
├── components/              # Auto-imported by Nuxt (directory prefix = component name)
│   ├── about/
│   │   └── OpenSourceList.vue     # → <AboutOpenSourceList>: filtered, incrementally rendered package list
│   ├── auth/
│   │   └── PasswordStrength.vue   # → <AuthPasswordStrength>; scored by passwordPolicyChecks, so "Strong" = the policy accepts it
│   ├── calendar/
│   │   ├── CalendarSidebar.vue    # Calendar list with visibility toggles
│   │   ├── CalendarToolbar.vue    # View switcher (month/week/day), navigation
│   │   ├── EventForm.vue          # Shared form for create/edit (summary, dates, recurrence)
│   │   ├── EventCreateDialog.vue  # Dialog wrapping EventForm for new events
│   │   ├── EventEditDialog.vue    # Dialog wrapping EventForm for editing
│   │   ├── EventDetailDialog.vue  # Read-only event details
│   │   ├── CalendarSettingsDialog.vue # General / Sharing / Public Access / Integration + delete. `kind="tasks"` (#170) = task-list copy,
│   │   │                          #   no Timezone, no Public Access, changed fields only; without it unchanged; exposes confirmDelete(cal)
│   │   └── RecurrenceScopeDialog.vue  # "This event / All events / This and future"
│   ├── contacts/
│   │   ├── ContactsSidebar.vue    # Address book list with checkboxes
│   │   ├── ContactListItem.vue    # Single contact row (avatar, name, email, actions)
│   │   └── AlphabetNavigation.vue # A-Z letter strip for quick scrolling
│   ├── tasks/                     # Tasks section (#170); imported explicitly by pages/tasks/index.vue
│   │   ├── TasksSidebar.vue       # All tasks + own/shared task lists (colour dot, open count, lock = read only), "+"; an owned
│   │   │                          #   kind=tasks list has a menu: List settings, Share list, Delete task list (emits edit/share/delete-list)
│   │   ├── TaskToolbar.vue        # View title + "N open", search, Open/Done/All, sort + direction, phone list picker
│   │   ├── TaskQuickAdd.vue       # "Add a task" line above the list: Enter creates in tasksStore.newTaskListId (absent for a
│   │   │                          #   read-only list), clears on success, keeps the text on failure; pen = "Add with details"
│   │   ├── TaskList.vue           # Status groups (sorted within), states (skeleton, empty, error banner), "Show N completed",
│   │   │                          #   "Showing N of total"; owns the tick (600 ms in place, Undo toast), the clock and the keyboard;
│   │   │                          #   emits open (row focused first) + empty-state, exposes focusTask(id); delete -> "Task deleted";
│   │   │                          #   polite live region: "Task marked as not done" (the toasts say the rest)
│   │   ├── TaskGroupHeader.vue    # Sticky h2 "Needs action, 9 tasks"; Completed/Cancelled are <button aria-expanded aria-controls>
│   │   ├── TaskEmptyState.vue     # no-lists / empty / no-match / all-done (spec §7.5 copy)
│   │   ├── TaskRow.vue            # One task <li> (props task/now/showList/readOnly/pending/tabbable; emits toggle/delete/open): overdue
│   │   │                          #   bar + label, priority mark, <time>, strike, Cancelled tag, repeat icon, menu (Complete, Edit task,
│   │   │                          #   Delete task); the title is a tabindex=-1 button that opens the task; spinner after 300 ms pending
│   │   ├── TaskCheck.vue          # The tick: <button role="checkbox">, Space/Enter, aria-disabled + reason, 180 ms check draw
│   │   ├── TaskForm.vue           # Create/edit form (Vuelidate: title, due >= start); list select create-only over writable lists;
│   │   │                          #   repeating task: rule as text, status + progress disabled, note on a due change; readOnly = no Save
│   │   ├── TaskCreateDialog.vue   # "Add a task": TaskForm -> buildTaskInput -> createTask; stays open on failure
│   │   ├── TaskEditDialog.vue     # "Edit task" / "Task details" (read only): PATCH of the changed keys only (none = no request),
│   │   │                          #   "Delete task" asks (permanent, no Undo) then "Task deleted"
│   │   ├── AddTaskListDialog.vue  # "Create task list": name + colour (first colour no task list has) -> createTaskList (kind tasks);
│   │   │                          #   "Task list created", stays open on failure. AddCalendarDialog is NOT used for task lists
│   │   └── TaskListSettingsDialog.vue # CalendarSettingsDialog with kind="tasks": rename, recolour, share, "Delete task list" (permanent);
│   │                              #   confirmDelete(list) is the sidebar's delete; relays updated / deleted(list)
│   ├── dashboard/                 # Dashboard widgets (story 042) → <DashboardWidgetCard> etc.
│   │   ├── WidgetCard.vue         # Shared card shell (title, icon, badge, actions, footer)
│   │   ├── TodayAgendaCard.vue    # Day timeline with lane-split overlapping events
│   │   ├── UpcomingEventsCard.vue # Next 5-7 events with Today/Tomorrow labels
│   │   ├── MiniCalendarCard.vue   # 6x7 month grid with event dots + month navigation
│   │   ├── RecentContactsCard.vue # Most recently updated contacts
│   │   ├── RecentContactRow.vue   # Single row of the above
│   │   └── QuickStatsCard.vue     # Calendars / events this month / open tasks / address books / contacts; open tasks =
│   │                              #   summary.open (#170), null = pulse while asked for, else "—" (never 0)
│   ├── settings/
│   │   ├── DavCredentialManager.vue # → <SettingsDavCredentialManager>  CalDAV / CardDAV credential pages
│   │   └── DavUrlBox.vue          # → <SettingsDavUrlBox>  copyable DAV URLs + which login to use
│   ├── sharing/                   # Resource-agnostic sharing UI (story 043)
│   │   ├── SharePanel.vue         # → <SharingSharePanel>  list/invite/change/revoke, BOTH resource kinds;
│   │   │                          #   resourceNoun overrides the copy's noun ("task list", #170)
│   │   ├── PublicLinkPanel.vue    # → <SharingPublicLinkPanel>  calendar public link (calendars only)
│   │   └── ShareDialog.vue        # → <SharingShareDialog>  modal composing both panels
│   └── common/
│       ├── ActionToast.vue        # Host of useAppToast().action() toasts ("Task completed" + Undo), bottom centre; default layout only
│       ├── ReminderField.vue      # #169: reminder chips + "Add reminder" (presets, Custom…), v-model = Reminder[]; props allDay, readOnly, target ('event'|'task'), disabled, disabledHint; used by EventForm and TaskForm
│       ├── ReminderToast.vue      # #169: sticky reminder toasts (group app-reminder) for the alarms store's queue; default + settings layouts
│       ├── AppHeader.vue          # Top bar with hamburger toggle + global search trigger
│       ├── AppSidebar.vue         # Main navigation sidebar (Dashboard, Calendar, Tasks, Contacts, Settings); the Tasks badge is
│       │                          #   tasksStore.overdueCount, summary fetched on mount, at each local midnight, at the next timed due instant of a loaded task, and when the tab becomes visible again (#170)
│       ├── GlobalSearch.vue       # Cmd/Ctrl+K search palette (trigger + dialog); a task-list hit opens /tasks?list=<uuid> (#170)
│       ├── SearchSectionHeader.vue # Category header + count for a search result group
│       ├── SearchViewAll.vue      # "View all N …" link into /search
│       ├── HighlightText.vue      # Search term highlighter using <mark> tags
│       ├── ThemeToggle.vue        # Light/Dark/System switcher (story 046)
│       ├── LoadingSpinner.vue     # Centered spinner
│       └── SkeletonList.vue       # Loading placeholder rows
├── stores/                  # Pinia stores (state + getters + actions)
│   ├── alarms.ts            # #169 reminders: polling of /alarms/upcoming, one timer per alarm key, display queue, acknowledgement + localStorage mirror
│   ├── auth.ts              # Auth state, login/register/logout/refresh, token scheduling
│   ├── calendars.ts         # Calendar + event CRUD, visibility toggling, FullCalendar integration
│   ├── contacts.ts          # Address book + contact state, search, sorting, letter grouping
│   ├── dashboard.ts         # Dashboard event window (loadedMonths), recent contacts, clock
│   ├── preferences.ts       # User preferences (event duration, all-day, time format, accent colour, first day of week)
│   ├── search.ts            # Global search fan-out (events/contacts/collections), cache, recents
│   ├── sharing.ts           # Share CRUD for calendars AND address books, calendar public link,
│   │                        #   `sharedWithMe` derived from the two list endpoints (story 043)
│   └── tasks.ts             # Tasks (#170): task lists (kind tasks/both, read from the calendars store), one GET /tasks,
│                            #   summary (badge), status groups, optimistic toggleComplete; mutations toast their own errors;
│                            #   newTaskListId = where a new task goes (selected writable list / first writable / null);
│                            #   forgetTaskList / applyListInfo after a list is deleted / renamed or recoloured
├── composables/
│   ├── useApi.ts            # $fetch wrapper: JWT auth, response unwrapping, HTTP 429 retry/notice
│   ├── useAppToast.ts       # Toast notification helpers (success/error/warn/info, action = one button such as Undo, 5 s)
│   ├── useTheme.ts          # Light/Dark/System state — SINGLETON, see Theming below
│   ├── useAccentColor.ts    # Accent colour — SINGLETON, server-backed, see Theming below
│   ├── useDatePickerFirstDay.ts # Mirrors first_day_of_week into PrimeVue's global locale (DatePickers)
│   ├── useOpenSourceAttribution.ts  # Loads both attribution manifests + filterOpenSourcePackages()
│   └── useCopyToClipboard.ts  # Copy + toast, with the plain-http fallback (no navigator.clipboard)
├── middleware/
│   ├── auth.ts              # Requires authentication (redirects to /auth/login)
│   └── guest.ts             # Requires unauthenticated (redirects away from auth pages)
├── layouts/
│   ├── default.vue          # App shell: sidebar + header + Toast + ActionToast + ReminderToast + ConfirmDialog + loading overlay
│   └── auth.vue             # Centered card with CalCard branding
├── types/
│   ├── alarms.ts            # #169: UpcomingAlarm, UpcomingAlarmsResponse, AcknowledgeAlarmsBody
│   ├── auth.ts              # User, LoginResponse, RefreshResponse, SystemSettings, AuthMethod
│   ├── calendar.ts          # Calendar, CalendarEvent, RecurrenceRule, EventFormData
│   ├── contacts.ts          # AddressBook, Contact, ContactEmail/Phone/Address/URL
│   ├── about.ts             # OpenSourcePackage, OpenSourceManifest, UNKNOWN_LICENSE
│   ├── api.ts               # ApiResponse, ApiError, ValidationError, PaginatedResponse
│   ├── search.ts            # EventHit, ContactHit, SearchResults (global search)
│   └── tasks.ts             # Task, TaskInput, TaskPatch, TasksSummary, TaskStatus, TaskGroupKey: the task API's raw JSON (#170);
│                            #   TaskFormData (what TaskForm edits)
├── utils/                   # Auto-imported pure helpers (no Nuxt context needed)
│   ├── apiErrors.ts         # RETRY_HINT, httpErrorStatus, clientErrorMessage (the server's sentence for a 4xx): task and task-list writes (#170)
│   ├── agendaLayout.ts      # Lane assignment for overlapping blocks on the day timeline
│   ├── contactAvatar.ts     # Contact initials + deterministic avatar colour
│   ├── dashboardDates.ts    # Local-date primitives, month-range merging, week-start-aware month grid, CLOCK_INTERVAL_MS (the minute clock)
│   ├── dialogs.ts           # DIALOG_BREAKPOINTS / TOAST_BREAKPOINTS: the one 640px rule of the task dialogs and the undo toast (the event dialogs have no breakpoint; ShareDialog uses 95vw)
│   ├── davUrls.ts           # davOrigin() / davHomeUrl(): the URLs DAV clients are configured with
│   ├── rateLimit.ts         # Retry-After parsing + friendly 429 text for useApi
│   ├── sanitizeUrl.ts       # Allowlists URL schemes before binding to an href (#27)
│   ├── searchFormat.ts      # Shared row labels for the search palette + results page; taskListRoute (#170)
│   ├── alarmRefresh.ts      # #169: refreshAlarmsSoon() (lazy import of the alarms store's refreshSoon), called after event/task mutations and a time-zone change
│   ├── alarmToast.ts        # #169: toast wording (alarmDetail, summaryLines, MAX_SINGLE_TOASTS)
│   ├── reminders.ts         # #169: presets, all-day offset formulas, describeReminder, remindersChanged, toReminderInputs, mapReminderMode; task presets/wording via ReminderTarget
│   ├── reminderChannel.ts   # #169: BroadcastChannel('calcard-reminders') claim between tabs (§10.7)
│   ├── alarmSchedule.ts     # #169 pure helpers: delayMs (cap 2^31-1, skew), mergePoll (§10.3), alarmLink (Open targets)
│   ├── accent.ts            # Accent palette generation + hex parsing (story 046)
│   ├── davCredentials.ts    # Generated DAV credential username/password + the username rule
│   ├── passwordPolicy.ts    # Client copy of the server's password rules (Unicode classes, 8–72 bytes); checks the NEW-password field of
│   │                        #   register, setup, reset-password and settings/password (never a login/current password) and scores AuthPasswordStrength
│   ├── tasks.ts             # Task helpers (#170): isOverdue/dueBucket (the server's overdue rule: all-day by local date,
│   │                        #   timed by instant), priorityLevel, formatDue, compareTasks (the in-group sort)
│   ├── taskForm.ts          # Task form <-> API (#170): taskToFormData, buildTaskInput, buildTaskPatch (changed keys only; never
│   │                        #   status/progress of a repeating task), priority 1/5/9, local-date all-day values, describeRecurrence
│   ├── taskLists.ts         # Task lists (#170): TASK_LIST_COLORS + suggestTaskListColor (a new list's colour), taskListDeleteMessage, COLOR_FORMAT_ERROR
│   ├── theme.ts             # Theme constants + pure helpers (also imported by nuxt.config)
│   └── vcardDate.ts         # vCard date parsing/formatting
├── plugins/
│   ├── alarms.client.ts     # #169: isAuthenticated (immediate) -> alarms.start() / alarms.stop()
│   ├── first-day-of-week.client.ts # Starts useDatePickerFirstDay() app-wide at boot
│   ├── primevue-services.ts # Registers ConfirmationService (for useConfirm) and the v-tooltip directive (#170)
│   ├── theme.client.ts      # Starts useTheme() app-wide at boot (story 046)
│   └── timezone.client.ts   # #169: one set-if-empty PUT /users/me/preferences/timezone-default per tab and signed-in period
│                            #   (watches auth.isAuthenticated, immediate); the answer goes into preferencesStore.setTimezone (no PATCH);
│                            #   errors only console.warn; `useNuxtApp().$timezoneReady()` = the always-resolving pending promise
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
7. A 429 (rate limit) is NOT a failed refresh: the session and the refresh cookie are kept (see Rate limiting below).

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

### Rate limiting (HTTP 429)

`useApi()` returns a thin wrapper around the `$fetch.create()` instance that owns every 429
(`utils/rateLimit.ts` holds the pure parts). The server's global limiter is per IP, so everyone
behind one NAT/proxy shares it, and a dashboard load fires several calls at once.

- **GET/HEAD are re-issued after waiting `Retry-After`**, at most `RATE_LIMIT_MAX_RETRIES` (2)
  times and only when the wait is `<= RATE_LIMIT_MAX_WAIT_SECONDS` (15). Writes are **never**
  repeated automatically. No usable `Retry-After` means no retry (null is not "now"). The header
  is read first, then the global limiter's body field `retry_after`: a browser hides
  `Retry-After` from a cross-origin SPA unless the server lists it in `CALDAV_CORS_EXPOSE_HEADERS`.
- **Otherwise the error is rewritten in place**: `message`, `data.message` and `data.error` all
  become "Too many requests, please try again in N seconds." So every existing
  `e.message` / `e.data?.message` / `shareErrorMessage()` reader shows that instead of
  `[GET] "…": 429 Too Many Requests` or the machine code `rate_limit_exceeded`. Status stays 429.
- **One toast per burst.** The de-duplication latch is module-level (each store action builds
  its own `useApi()`), held for `Retry-After` or at least 5s. Specs reset it with
  `resetRateLimitNotice()`. The toast is raised through `nuxtApp.vueApp.runWithContext`, because
  `useApi()` mostly runs in store actions, where PrimeVue's inject-based `useToast()` would throw.
  The auth layout has no `<Toast>`; login/register show the rewritten `data.message` inline.
- **A 429 is never a logout.** The limiter rejects before the handler runs, so a rate-limited
  `/auth/refresh` neither checked nor rotated the refresh token. `stores/auth.ts` keeps the
  session (a live one re-schedules the refresh after `Retry-After`), and `fetchUser()` does not
  `clearAuth()` on a 429. Before this, a busy IP logged the user out and wiped the cookie.
- 429 is deliberately NOT in ofetch's `retryStatusCodes` (that stays `[401]` for the token
  refresh): ofetch would retry POSTs too and ignore `Retry-After`.

### API Response Handling

The backend has two different response patterns:

1. **Envelope endpoints**: auth, `/system/*`, `/users/me` (+ password, preferences), `/app-passwords`, `/about/open-source`. Responses are wrapped in `{ "status": "ok", "data": ... }`. The `useApi` composable's `onResponse` handler automatically unwraps `.data`, so store code receives the inner payload directly. (`POST /auth/register` answers 200, not 201.)

2. **Raw JSON endpoints, NOT wrapped**: everything else. That includes **calendars and events** (e.g. `GET /api/v1/calendars` → `{ "calendars": [...] }`), sharing, public access, DAV credentials, import, MCP tokens, search, subscriptions, and the address book and contact endpoints. Address-book and contact shapes:
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
- **Task lists are not calendars (#170)**: `GET /calendars` also returns task lists (`kind: 'tasks'`, VTODO only). `calendarStore.calendars` keeps them (one request; the tasks section reuses it), so anything that lists calendars for EVENTS reads `eventCalendars` (`kind !== 'tasks'`) or a getter built on it (`calendarOptions`, `ownedCalendars`, `sharedCalendars`, `writableCalendars`, `subscribedCalendars`): the sidebar, the event form's calendar select, `fetchEvents`, the dashboard's `fetchEventRange` and calendar counts, the import/export selectors. Mixed (`both`) calendars and subscriptions stay. A lookup by id (an event's calendar colour or name) may still read `calendars`.
- **CalendarToolbar**: Month/Week/Day view switcher, prev/next/today navigation. Controls FullCalendar via template ref.
- **Event dialogs**: Create, edit, and detail dialogs are separate components. For recurring events, a `RecurrenceScopeDialog` asks the user whether to modify "this event", "this and future", or "all events".
- **Drag-and-drop**: FullCalendar's `@eventDrop` and `@eventResize` call `calendarStore.updateEventTime()` for direct time changes.
- **Date formatting**: `toRFC3339()` utility in `stores/calendars.ts` preserves local timezone offset (not UTC) so the backend can attach the correct IANA timezone.
- **First day of the week**: one server preference, `first_day_of_week` (`"0"` Sunday — the
  default — `"1"` Monday, `"6"` Saturday; `Date.getDay()` numbering), read through
  `preferencesStore.firstDayOfWeek`, lays out every week in the app: FullCalendar's `firstDay`
  here, the dashboard mini calendar (`buildMonthGrid(year, month, firstDay)`, where the parameter
  is required, plus `orderedWeekdays()` for its header row), the weekly-recurrence day chips in
  `EventForm`, and PrimeVue's DatePicker popups. The DatePicker has no prop for it — it reads the
  GLOBAL `config.locale.firstDayOfWeek`, which `useDatePickerFirstDay()` (started by
  `plugins/first-day-of-week.client.ts`) keeps in step. A new week-shaped view must read the getter
  too; never offset by a bare `getDay()` or hardcode a weekday array. Display only: no RRULE `WKST`
  is written.

### Contacts Page Architecture

The contacts page (`pages/contacts/index.vue`) uses a custom list layout:

- **ContactsSidebar**: Address book checkboxes filter which contacts are shown.
- **Search**: 300ms debounced input triggers `contactsStore.searchContacts()` which calls the backend search API.
- **Grouping**: Contacts are grouped by first letter of `formatted_name` via the `groupedContacts` getter (returns `Map<string, Contact[]>`).
- **AlphabetNavigation**: A-Z letter strip. Only letters with contacts are clickable. Clicking scrolls to that section.
- **Contact detail**: Full contact details live on the route page `pages/contacts/[id]/index.vue` (navigated to when a contact is selected).
- **Virtual scrolling**: Manual implementation with computed offsets and a scroll container.

### Tasks Section (#170)

`pages/tasks/index.vue` is the third section of the main navigation (Dashboard, Calendar, **Tasks**,
Contacts; the Tasks entry's pill is the overdue count). A task list is a calendar with
`kind: 'tasks'` (VTODO only); a mixed `both` calendar holds tasks too. The page composes the
`components/tasks/*` pieces (imported explicitly) over `stores/tasks.ts`:

- **The selection lives in the URL**: `?list=<calendar uuid>`, none = All tasks. The sidebar and the
  phone picker write it with `replace`, a watcher applies it, and an unknown uuid gets one calendars
  refetch before the page falls back to All tasks.
- **One store, one request per view.** `taskLists` / `writableTaskLists` derive from the calendars
  store (no second `GET /calendars`); `fetchTasks()` is one `GET /tasks` (`calendar_uuid` narrows it);
  `fetchSummary()` passes `as_of` through ofetch's `query`. Mutations toast their own errors and
  refetch the summary (badge and counts). Open / Done / All is a server query; the sort runs in the
  browser, within the status groups.
- **Who owns what.** TaskToolbar, TasksSidebar, TaskRow and TaskCheck only emit. TaskList owns the
  tick (optimistic, 600 ms in place, then the "Task completed" action toast with Undo, which PATCHes the
  status and percent the row had before the tick via `tasksStore.undoComplete`, not the server's uncomplete, and re-inserts the row when it matches the view), the
  minute clock, the keyboard and the focus. The page owns the dialogs (TaskCreateDialog and
  TaskEditDialog over TaskForm, AddTaskListDialog, TaskListSettingsDialog = CalendarSettingsDialog
  with `kind="tasks"`).
- **Deletes are permanent** (a task is hard-deleted; a list goes with everything in it), so nothing
  offers Undo after a delete: the confirm dialog is the safety net. It says so ("This can't be undone.").
- **Ticked rows.** Under Open a ticked row stays in `tasks` for the Undo but is neither shown nor counted in
  "Showing N of total" once it has left the view. Under All it stays where it was ticked (not in the collapsed
  Completed group) until the list is loaded again, the view changes or that group is opened. A completed
  repeating task can be reopened (uncomplete is allowed); an open one cannot be completed. `summary` / `description` are
  plain text: shown and sent back as they are.
- **Logout empties the store.** `clearAuth()` resets the tasks store (rows, badge counts, filters)
  through a lazy `import('~/stores/tasks')`: a static import closes the cycle
  useApi -> auth -> tasks -> calendars -> useApi and breaks `mockNuxtImport('useApi')` in specs.
- **Summary freshness.** The sidebar refetches the summary on mount, at local midnight, at the next
  timed due instant of a loaded task, on `visibilitychange`; the calendar delete (mixed calendars) and
  a calendar import into a task-bearing calendar refetch it too. `by_calendar[uuid].completed` is the
  list's done count (the empty state's "All caught up" and the "Show N completed" link read it).

Accessibility and phones (spec §8, story 29):

- **Tab order**: toolbar, quick-add (input, pen), then the list's ONE Tab stop (roving tabindex: the
  active row `<li>` with its own check and menu button), the Completed / Cancelled disclosure
  buttons, "Show N completed". ArrowUp / ArrowDown move between the rows of open groups; Space
  ticks, Enter opens the editor, Delete (or Backspace) asks to delete. The scroll containers (the
  list area, the sidebar's) carry `tabindex="-1"`: Firefox makes an overflowing scroller an unnamed
  Tab stop of its own.
- **Focus never falls to the page.** When the focused row leaves (ticked under Open, deleted, moved
  to another group), its neighbour takes the focus. TaskList reads `document.activeElement` in a
  pre-flush watcher, before Vue removes the row; it does not track focusout, because whether and
  when removing the focused element fires blur/focusout differs between browsers. Dialogs: PrimeVue
  traps the focus and returns it to the opener (the row is focused before a dialog opens); after
  the editor closes, the page refocuses the row when it was re-rendered meanwhile.
- **Announcements.** PrimeVue renders every toast as `role="alert"`, so "Task completed", "Task
  deleted" and "Task list created / updated / deleted" are announced by their toasts. The one other
  announcement is TaskList's polite live region (`data-test="task-announcer"`, outside the
  `aria-busy` list): "Task marked as not done" after an untick or an Undo, emptied again after 5 s.
  Do not add a second region for something a toast already says.
- **Focus ring**: the section's own controls use `focus-visible:ring-2 focus-visible:ring-primary-500`
  (inset on a row and on the quick-add line); PrimeVue controls draw the Material preset's 2px
  primary outline.
- **Overdue** is text (sr-only "Overdue, due …" plus the visible date), an icon and colour. Danger
  text is `text-red-600 dark:text-red-400`: 4.83:1 on surface-0 and 6.40:1 on the dark surface-900,
  measured in the built app, so the red-700 fallback is not needed.
- **Reduced motion**: the tick's check draw, ring fill and strike are `motion-reduce:transition-none`.
- **Phones (below `lg`)**: the sidebar is hidden and the toolbar's list `Select` replaces it as the
  list picker; the toolbar wraps (search full width, filter and sort below); rows drop the list and
  priority to a second line; the task form drops its decorative priority marks so the four choices
  fit. List settings, delete and "+" have no phone entry, the same as the
  Calendar section ("Create task list" shows only in the no-lists state). Dialogs use
  `:breakpoints="{ '640px': 'calc(100vw - 2rem)' }"`.

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
- **Calendar hits and task lists (#170)**: the calendars group lists task lists too (each hit
  carries `kind`). A `kind: 'tasks'` hit opens `/tasks?list=<uuid>` in both the palette and the
  results page (`taskListRoute()` in `utils/searchFormat.ts`), never the Calendar section, which
  does not show task lists; its row says "Task list". Every other hit (`events`, mixed `both`)
  opens the Calendar section as before: `/calendar` from the palette, `/calendar?cal=<numeric id>`
  from the results page. The calendar page consumes and strips a lone `?cal=` (nothing to jump to
  or open). There is no tasks group in the results (deferred).

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

### User time zone (#169)

`plugins/timezone.client.ts` fills the user's `timezone` preference once per tab and signed-in period: it watches
`auth.isAuthenticated` (immediate, so a session restored by `initAuth`, a password sign-in and the OAuth callback
all count) and sends `PUT /api/v1/users/me/preferences/timezone-default` with the browser's zone. The server only
stores it if none is stored ("set-if-empty"), so the *answer* is written into the store via
`preferencesStore.setTimezone()` (no PATCH), also when it differs from the browser's zone. Errors (400, network)
only `console.warn`: no toast, no retry. The flag re-arms when `isAuthenticated` turns false.
`useNuxtApp().$timezoneReady()` returns the pending promise (never rejects) for callers that must wait for it.
`PREF_TIMEZONE` (`timezone`, default `UTC`) and the `timezone` getter live in `stores/preferences.ts`;
Settings -> Calendar has a filterable "Time zone" Select (`UTC` + `Intl.supportedValuesOf('timeZone')`) saved
through the preferences PATCH, a 400 shows the server's text.

### Reminders (#169)

`stores/alarms.ts` is the data layer of the web app's reminders; `components/common/ReminderToast.vue` renders its
`queue` as toasts (see "Toasts, several tabs and refresh hooks" below). Spec §10.

- **Lifecycle.** `plugins/alarms.client.ts` watches `auth.isAuthenticated` (immediate): `true` -> `start()`,
  `false` -> `stop()`. `start()` waits for `$timezoneReady()` (at most 3 s, so the first poll does not hit the UTC
  fallback), loads the persisted keys, polls, then installs the 5-minute interval, the 30 s heartbeat, the
  `visibilitychange` and `online` listeners. `stop()` clears every timer and listener, bumps the sequence tokens (a
  late answer is dropped) and keeps state; `reset()` (called by `clearAuth` through a lazy import, like
  `resetTasksStore`) also empties the state and removes that user's `localStorage` entry. Both are idempotent.
  Every cross-store access (`toRFC3339` from `stores/calendars.ts`) is a lazy `import()`.
- **Poll.** `GET /api/v1/alarms/upcoming` (raw JSON, no envelope) with `query: { until: toRFC3339(now + 24 h) }`:
  ofetch encodes the `+` of the offset; no zone parameter. One request in flight (a call meanwhile schedules ONE
  follow-up). Triggers: `start()`, every 5 minutes (also hidden tabs), `visibilitychange` to visible when the last
  poll is older than 60 s, `online`, heartbeat gap > 90 s (sleep: `checkDue()` + `refresh()`), `refreshSoon()`
  (2 s debounce, for the refresh hooks below).
- **Skew.** `skewMs = Date.parse(server_time) - (requestStart + requestEnd) / 2`; every "now" is `Date.now() + skewMs`.
- **Timers.** `mergePoll` (pure, `utils/alarmSchedule.ts`): a new key gets exactly one `setTimeout` (delay
  `delayMs`, capped at 2^31-1), or goes straight into `queue` when `trigger_at <= now` (missed, up to 24 h); a key
  absent from the answer is cancelled, except when `truncated` and its trigger is after the last returned one;
  queued, shown and pending keys are never armed again. A firing timer re-arms when `now + skew < trigger_at`
  (clock set back). `queue` is oldest first and holds the data the toast needs.
- **Acknowledgement.** `ReminderToast` calls `markShown(keys)` once toasts are rendered in a visible document. The keys
  move to `pendingAck`, then a 1 s debounced `POST /api/v1/alarms/acknowledge` sends `{ keys }` (no times) in batches
  of at most 500; 204 moves them to `shown`. A failure keeps them pending and retries after 5 s, 30 s, 2 min, then
  with every poll (`useApi` never retries writes). `shown` and `pendingAck` are mirrored to
  `localStorage['calcard-alarms:<user id>']` as `{ shown, pending }` (key -> trigger instant, dropped 25 h after
  it); after a reload a pending key is neither shown again nor forgotten (its POST is retried). Storage access is
  always in `try/catch`.
- **Failures.**

  | Situation | Behaviour |
  |---|---|
  | Poll fails (network, 5xx) | timers kept, `console.warn`, next trigger retries |
  | 401 | `useApi` refreshes the token; a failed refresh logs out -> `stop()` |
  | 404 on the poll or the acknowledgement | `disabled`: polling and timers stop until the next sign-in (the server runs with `alarms.enabled: false`) |
  | 400 on the poll | a bug: `console.error` once, polling stops for the session, armed timers keep running |
  | Acknowledge fails | retry schedule above; the key is never queued again while pending |

#### Toasts, several tabs and refresh hooks (story 16)

- **Group `app-reminder`** (`REMINDER_TOAST_GROUP` in `composables/useAppToast.ts`, next to `ACTION_TOAST_GROUP`):
  `components/common/ReminderToast.vue` hosts it, top right, and is mounted by the **default and settings layouts**
  (not the auth layout), so a reminder shows on every authenticated page. It cannot reuse `useAppToast().action()`
  (one at a time, 5 s). The toasts are sticky `info` (no `life`) with "Open" and "Dismiss"; PrimeVue renders them as
  `role="alert"`, the component never moves focus, and the buttons are named ("Open Team sync", "Dismiss reminder for
  Team sync"). Wording lives in `utils/alarmToast.ts`.
- **Display rules.** Nothing is claimed or shown while `document.visibilityState !== 'visible'`; the queue waits and is
  worked off at `visibilitychange`. A toast rendered in a visible document calls `markShown(keys)` (the
  acknowledgement), so call `markShown` only then. More than 3 at once become ONE summary toast ("You missed N
  reminders", up to 5 lines plus "and N more") that acknowledges every key. "Open" goes to `alarmLink(alarm)`.
- **Several tabs (§10.7).** `utils/reminderChannel.ts`, `BroadcastChannel('calcard-reminders')`: a visible tab posts
  `{type:'claim', key, tab, at}`, waits 300 ms, the smallest `(at, tab)` shows the toast and posts
  `{type:'shown', key}`; every tab receiving `shown` calls `alarms.drop([key])` (cancels the timer, leaves the queue,
  never queues it again, acknowledges nothing). A tab that lost yields the key and claims it again after 5 s if no
  `shown` came. Hidden tabs never claim. Without `BroadcastChannel` (absent or throwing) every tab shows its own.
  `navigator.locks` is not used (secure contexts only; the default compose deployment is plain http).
- **Refresh hooks (§10.3).** `refreshAlarmsSoon()` (`utils/alarmRefresh.ts`, a lazy import of the alarms store's
  debounced `refreshSoon()`) runs after a SUCCESSFUL `calendars.createEvent` / `updateEvent` / `updateEventTime` /
  `moveEvent` / `deleteEvent`, `tasks.createTask` / `updateTask` / `toggleComplete` / `undoComplete` (through
  `updateTask`) / `removeTask`, and after Settings > Calendar saved a changed time zone. Any new code that creates,
  changes, moves or deletes an event or task over the API (the reminder fields of the forms, stories 17 and 18) goes
  through those store actions and needs no call of its own; a new direct `api()` write must call
  `refreshAlarmsSoon()` itself.

#### Reminder field in the event form (story 17)

- **`components/common/ReminderField.vue`** (`<CommonReminderField v-model allDay readOnly>`) + **`utils/reminders.ts`**. The v-model is
  the item's full `Reminder[]` as the API answers it; `EventForm` holds it as `reminders` and sends
  `EventFormData.reminders` (`ReminderInput[]`: `{ offset_seconds }` or `{ at }`) ONLY when `remindersChanged(initial, current)`
  says the editable set changed. A title or date change alone sends no `reminders` key (relative ones move with the date, the server
  shifts absolute all-day ones); `updateEventTime` (drag and drop) never sends them. A PATCH `reminders` list REPLACES the editable
  ones, so the form sends every editable reminder it shows (absolute ones as the `at` the server returned); read-only ones
  (`editable: false`: END-related, EMAIL, REPEAT, ...) show as "Set in another app" and are never sent or removed.
- **All-day reminders are relative too (D23)**: "On the day at 09:00" = `32400`, "N days before at hh:mm" = `-N*86400 + hh*3600 + mm*60`
  (`-54000`, `-140400`, `-572400`; custom 2 days before at 07:30 = `-145800`). No browser zone is involved; only an absolute chip
  ("At 4 Nov, 09:00") is shown in the browser's zone. Back: `daysBefore = -floor(offset/86400)`.
- **Switching all-day <-> timed** maps editable relative reminders to the other mode's preset with the same days before
  (`mapReminderMode`); absolute and read-only ones stay. At most 5 editable reminders, duplicates merge, ordered by trigger.
- **Ownership (Q9)**: the block is hidden (and nothing sent) unless the selected calendar is in `calendarStore.ownedCalendars`: a shared
  event's VALARMs alert the owner and every sharee. New events start with none (Q13).
- Story 18 (task form) reuses the field and helpers with `target="task"` (below).

#### Reminder field in the task form (story 18)

- `TaskForm` renders `<CommonReminderField target="task" :disabled="!form.due" disabled-hint="Set a due date to add a reminder">` below
  the Due/Start row. `target="task"` (`ReminderTarget`) switches the presets and wording to the due date: timed = At due time, 5/15/30 minutes,
  1 hour, 1 day before due; all-day = "On the due date at 09:00" (`32400`) and "1 day before at 09:00" (`-54000`); `offsetReminder(offset, 'task')`
  creates `related: 'end'` (the server writes `TRIGGER;RELATED=END`). `describeReminder(r, allDay, 'task')` words an end-related one
  ("30 minutes before due"); a start-related one on a task is read only. `mapReminderMode(..., 'task')` keeps them end-related.
- `TaskFormData.reminders` (full `Reminder[]`): `taskToFormData` reads `task.reminders`, `newTaskFormData` starts with none.
  `buildTaskInput` sends `reminders` only with a due date (the server answers 400 `A reminder needs a due date` otherwise); `buildTaskPatch`
  sends the full editable list ONLY when `remindersChanged` (a due-only change sends none), so `isEmptyTaskPatch` covers an untouched form.
  `stores/tasks.ts` `createTask`/`updateTask` pass the bodies through. Quick-add has no reminder control.
- Disabled without a due date (chips of stored reminders stay and can be removed). Clearing the due resets the field to the stored list;
  the all-day switch maps reminders only while a due date is set. Hidden when the task's list is not owned (`shared` or `subscribed`, Q9);
  `readOnly` shows chips without controls. A repeating task's field stays editable (a reminders-only PATCH edits the master, no 409).

### Type System Gotchas

**AddressBook vs Calendar field naming**: AddressBook types use GORM-style PascalCase (`ID`, `UUID`, `Name`, `CreatedAt`) because the backend returns raw GORM models. Calendar/Event types use snake_case (`id`, `name`, `created_at`) because they go through DTOs. This inconsistency comes from the backend — don't try to "fix" it on the frontend side.

Neither type carries the **owner's numeric user id** or soft-delete state: `Calendar.UserID` and `AddressBook.UserID`/`DeletedAt` are `json:"-"` on the backend (P03 — a sharee must not learn the owner's primary key). Identify the owner of a shared collection by the `owner` block (`owner.id` is the owner's UUID). The collection's own numeric `id`/`ID` and an event's `calendar_id` are still sent and still used as handles.

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

### DAV connection URLs

`components/settings/DavUrlBox.vue` (→ `<SettingsDavUrlBox>`) shows the URLs a CalDAV/CardDAV
client is configured with, with copy buttons, at the top of the CalDAV and CardDAV credential pages
(rendered by `DavCredentialManager`, keyed on its `apiEndpoint`) and the app-password page. The URL
strings come from `utils/davUrls.ts`.

- **The URL carries the ACCOUNT username, never a credential's.** The server's tree is
  `/dav/{username}/calendars/` and `/dav/{username}/addressbooks/`, and the DAV handler compares that
  segment to the authenticated account exactly: a DAV credential's own username in the URL is a
  404, and so is a lower-cased mixed-case account name. The credential's username is only the
  Basic-auth login name; an app password logs in with the account username (or e-mail).
- **The username comes from the auth store**, which the login response and `GET /users/me` fill.
  `/users/me` had no `username` field until this was added, so the profile page showed an empty
  box and `components/setup/ProtocolSection.vue` built `/dav//…` URLs. With no username the box
  shows a warning instead of a URL a client would accept and then fail on.
- **Origin = `apiBaseUrl || window.location.origin`**, the same rule as the MCP page.
  `/.well-known/caldav` and `/.well-known/carddav` redirect to `/dav/`, so auto-discovering clients
  (DAVx5, Apple) need only that origin; the box says so in one line.
- **Copying goes through `useCopyToClipboard()`**, which checks for `navigator.clipboard` first: over
  plain http (docker-compose's default) the API does not exist at all, and the user is told to copy
  by hand.

### DAV credentials (CalDAV / CardDAV settings pages)

`pages/settings/caldav-credentials.vue` and `carddav-credentials.vue` are thin wrappers around
`components/settings/DavCredentialManager.vue` (raw JSON, no `{status,data}` envelope).

- **Username and password are generated in the browser** every time the create dialog opens
  (`utils/davCredentials.ts`): a username of 16 lowercase letters `a`–`z`, and a 24-character
  password from an alphabet without look-alikes (no `0 O o 1 l I`) holding an upper-case letter, a
  lower-case letter, a digit and one of `- _ .` (URL-unreserved). The refresh button draws both
  again; the password has a show/hide toggle, both fields a copy button (`useCopyToClipboard()`).
  Randomness is `crypto.getRandomValues` with **rejection sampling** — never `Math.random`, never a
  plain `byte % n`, which favours the first `256 % n` characters. Specs inject the byte source.
  `getRandomValues` exists in insecure contexts too (unlike `crypto.subtle`/`randomUUID` and the
  clipboard), so generation works over plain http.
- **The username rule mirrors the server** (`ValidateDAVCredentialUsername`): a NEW credential's
  username is 3–50 lowercase letters `a`–`z`. Upper case is an inline error, never lower-cased
  silently: the name has to reach the DAV client exactly as it is stored. Every stored name is
  therefore lowercase, which the server's CalDAV credential lookup relies on.
- **The password is shown once**, in the "Credential Created" panel after the POST: the server
  keeps a bcrypt hash and no list returns it. Closing the dialog clears it from memory.
- **409 = username taken** (unique across all accounts, `user.ErrCredentialUsernameTaken`). A
  generated name is replaced and the user asked to press Create again — no automatic retry, they
  may already have copied it; the password is kept. A typed name is kept, with an inline message.

### MCP Access (story 104)

`pages/settings/mcp.vue` mints and revokes the bearer tokens an AI assistant uses against the
server's `/mcp` endpoint. It has no Pinia store — three `useApi()` calls are the whole surface:

- `GET /api/v1/mcp-tokens` → `{ tokens: MCPToken[] }` (raw JSON, no `{status,data}` envelope).
- `POST /api/v1/mcp-tokens` → `MCPTokenCreateResponse`. **The `token` field is the only time the
  secret exists**: the server stores a SHA-256 and cannot reproduce it. The post-create panel is
  therefore not a nicety — losing it strands the user with an unusable token.
- `DELETE /api/v1/mcp-tokens/:id` → 204.

Three things that look cosmetic but are not:

- **The intro says what a token may do, deletions included.** Since the collection tools, an
  assistant can create, rename and delete the user's own calendars and address books (everything
  in them goes too). The sentence saying so is the only place the user learns it before minting a
  token, so `mcp.spec.ts` pins it.
- **The endpoint shown to the user is `${apiBaseUrl || window.location.origin}/mcp`** — NOT under
  `/api/v1`. MCP addresses one URL that clients are configured with directly. A wrong URL here
  makes every client fail with no diagnosis, which is why `mcp.spec.ts` pins it.
- **A failed load renders an error with a Retry, never the empty state.** "No MCP tokens yet" on a
  failed request is a false statement about the account and hides the very token the user may have
  come to revoke — the same rule `pages/settings/sharing.vue` follows.

### Calendar Subscriptions (story 100)

`pages/settings/subscriptions.vue` manages remote iCalendar feeds mirrored into read-only
calendars. Like the MCP page it has no Pinia store of its own — `useApi()` against
`/api/v1/calendar-subscriptions` (raw JSON, no `{status,data}` envelope) is the whole surface.

Three things that look cosmetic but are not:

- **A subscribed calendar is read-only, and the UI must not offer a write.** `Calendar.subscribed`
  drives that. `stores/calendars.ts` excludes subscribed calendars from `writableCalendars` — the
  single getter the event form and the dashboard quick-add read — and `pages/calendar/index.vue`
  sets `editable` **per event** rather than globally, so one read-only calendar among several does
  not freeze the whole grid. (That per-event flag also fixed the same gap for read-only *shares*,
  which were draggable and failed server-side.) `CalendarSidebar.vue` lists them in their own
  section without the edit/share/delete menu.
- **A failed refresh answers HTTP 200.** The request succeeded; the third-party feed is what
  failed, and the response carries the reason on the subscription. The page keys its toast on
  `result.synced`, not on the request resolving — treating 200 as success would show "refreshed"
  over a row that is about to render an error.
- **Every mutation refreshes the calendar store too.** A subscription IS a calendar: adding,
  renaming, recolouring, refreshing or removing one changes `GET /calendars`, and without the
  extra call the sidebar keeps showing the old state until a full page reload.

A failed *load* renders an error with a Retry rather than the empty state, for the same reason as
the MCP page.

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
toast.action('Task completed', 'Undo', () => undo()); // one button, 5 s, bottom centre
```

`action()` toasts render in `components/common/ActionToast.vue` (group `ACTION_TOAST_GROUP`), which only
the **default** layout mounts, so they do not show on settings or auth pages. Reminder toasts (#169) use their own
group `REMINDER_TOAST_GROUP` and host `ReminderToast.vue` (default and settings layouts); see "Reminders (#169)". One at a time: a new one
replaces the previous. The button runs the callback once and closes the toast. The 5 s countdown pauses while the pointer is over the toast or the focus is inside it (PrimeVue pauses on hover only when `onMouseEnter` is passed; the focus pause raises the same synthetic mouseenter/mouseleave).

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
- **Store setup**: use `createTestingPinia({ stubActions: false })` from `@pinia/testing` (it sets the active pinia and keeps real action logic so you assert behavior, not that an action was called). Prefer it over `createPinia()` from `pinia`. (`pinia` is a direct dependency since the Pinia 4 bump, so importing from it resolves; before that it was only a transitive peer and did not resolve under `vue-tsc`.)
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
