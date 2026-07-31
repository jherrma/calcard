import type { Calendar, CalendarEvent } from '~/types/calendar';
import type { AddressBook, Contact } from '~/types/contacts';
// Type-only: the FUNCTIONS in utils/ are auto-imported, exported interfaces are not.
import type { MonthRange } from '~/utils/dashboardDates';
import { useCalendarStore } from '~/stores/calendars';
import { useContactsStore } from '~/stores/contacts';

/**
 * Dashboard state (story 042).
 *
 * There is deliberately NO backend aggregation endpoint: the dashboard composes
 * what the existing per-resource endpoints already return. It keeps its OWN
 * event list rather than reusing `stores/calendars.ts`'s, because that one is
 * bound to whatever range FullCalendar currently displays — sharing it would
 * make the two screens fight over the fetched window. Calendars and address
 * books themselves (colours, permissions, counts) still come from the existing
 * stores so there is one source of truth for them.
 */

/** Upcoming events shown at once — story asks for "next 5-7". */
export const UPCOMING_EVENTS_LIMIT = 7;

/** Recent contacts shown at once — story asks for "5-6". */
export const RECENT_CONTACTS_LIMIT = 6;

/** Months of events pulled on first load: the current one plus the next. */
export const INITIAL_MONTHS = 2;

/**
 * Months a refresh keeps alive.
 *
 * The widgets themselves only read the current month and the next (agenda,
 * upcoming horizon, "events this month"); every other month in `events` is there
 * purely because the mini calendar was pointed at it. So a refresh keeps the
 * displayed month plus its immediate neighbours — paging one step then needs no
 * fetch — and DROPS everything else from both `events` and `loadedMonths`.
 *
 * Dropping is what bounds the work: month navigation appends for as long as the
 * user clicks, and without pruning a user who paged back to 2020 would make
 * every later refresh re-request the whole span since. A pruned month leaves
 * `loadedMonths` too, so navigating back to it simply fetches it again.
 */
function retainedMonthKeys(now: number, focus: Date): Set<string> {
  const current = startOfMonth(new Date(now));
  const keys = new Set<string>();
  for (let i = 0; i < INITIAL_MONTHS; i++) keys.add(monthKey(addMonths(current, i)));
  for (let i = -1; i <= 1; i++) keys.add(monthKey(addMonths(focus, i)));
  return keys;
}

/**
 * Identity of a fetched event. Expanded occurrences of a recurring series all
 * carry the same backend `id` and differ only by `recurrence_id`/`start` (see
 * the composite FullCalendar id on the calendar page), so all three go into the
 * key — otherwise re-fetching an overlapping range would collapse a whole
 * series into one row.
 */
function eventKey(e: CalendarEvent): string {
  return `${e.id}|${e.recurrence_id ?? ''}|${e.start}`;
}

function byStartAscending(a: CalendarEvent, b: CalendarEvent): number {
  return new Date(a.start).getTime() - new Date(b.start).getTime();
}

interface DashboardState {
  /** Every event fetched so far, deduped by {@link eventKey}. */
  events: CalendarEvent[];
  /** `YYYY-MM` keys already fetched, so month navigation doesn't refetch. */
  loadedMonths: string[];
  /**
   * `YYYY-MM` the mini calendar is showing, or `''` for "whichever month `now`
   * is in". The STORE owns it (rather than the page) because a refresh has to
   * know which month is on screen: that month must never be pruned or replaced
   * with stale data behind the user's back.
   */
  miniMonthKey: string;
  recentContacts: Contact[];
  /** Sum of every address book's reported `Total` (not the fetched slice size). */
  totalContacts: number;
  /** In-flight event loads; a counter because mount and month-change can overlap. */
  eventLoads: number;
  isLoadingContacts: boolean;
  /** True when at least one calendar's events failed — the widget says so. */
  eventsIncomplete: boolean;
  /** True when at least one address book's contacts failed. */
  contactsIncomplete: boolean;
  /**
   * "Now" as epoch ms, ticked by the page once a minute. Held in state so the
   * Today/Tomorrow labels, the agenda's current-time marker and the "events this
   * month" count all re-render off one clock instead of each reading Date.now()
   * inside a computed (which would never invalidate).
   */
  now: number;
}

export const useDashboardStore = defineStore('dashboard', {
  state: (): DashboardState => ({
    events: [],
    loadedMonths: [],
    miniMonthKey: '',
    recentContacts: [],
    totalContacts: 0,
    eventLoads: 0,
    isLoadingContacts: false,
    eventsIncomplete: false,
    contactsIncomplete: false,
    now: Date.now(),
  }),

  getters: {
    isLoadingEvents(state: DashboardState): boolean {
      return state.eventLoads > 0;
    },

    /**
     * Month the mini calendar shows, as local midnight on the 1st. Until the
     * user navigates it follows the clock, so a dashboard left open over a month
     * boundary rolls over instead of sticking on the month it was opened in.
     */
    miniMonth(state: DashboardState): Date {
      const parsed = state.miniMonthKey ? monthFromKey(state.miniMonthKey) : null;
      return parsed ?? startOfMonth(new Date(state.now));
    },

    /** Events overlapping the current local day, earliest first. */
    todayEvents(state: DashboardState): CalendarEvent[] {
      const dayStart = startOfDay(new Date(state.now)).getTime();
      const dayEnd = addDays(new Date(dayStart), 1).getTime();
      return state.events
        .filter((e: CalendarEvent) => {
          const bounds = eventBounds(e.start, e.end);
          return bounds !== null && overlaps(bounds, dayStart, dayEnd);
        })
        .sort(byStartAscending);
    },

    todayAllDayEvents(): CalendarEvent[] {
      return this.todayEvents.filter((e: CalendarEvent) => e.all_day);
    },

    todayTimedEvents(): CalendarEvent[] {
      return this.todayEvents.filter((e: CalendarEvent) => !e.all_day);
    },

    /**
     * The next few events that haven't finished yet, nearest first. An event
     * already in progress stays listed — it is still the most relevant "next
     * thing" — and today's all-day events run until midnight, so they remain
     * visible for the whole day rather than vanishing at 00:01.
     */
    upcomingEvents(state: DashboardState): CalendarEvent[] {
      return state.events
        .filter((e: CalendarEvent) => {
          const bounds = eventBounds(e.start, e.end);
          return bounds !== null && bounds.end > state.now;
        })
        .sort(byStartAscending)
        .slice(0, UPCOMING_EVENTS_LIMIT);
    },

    /** Local `YYYY-MM-DD` keys that have at least one event — mini-calendar dots. */
    eventDayKeys(state: DashboardState): Set<string> {
      const keys = new Set<string>();
      for (const e of state.events) {
        const bounds = eventBounds(e.start, e.end);
        if (!bounds) continue;
        for (const key of dayKeysBetween(bounds.start, bounds.end)) {
          keys.add(key);
        }
      }
      return keys;
    },

    /** Events overlapping the given month — parameterized so the mini calendar
     * can label whichever month it is showing, not just the current one. */
    eventCountInMonth(state: DashboardState): (month: Date) => number {
      return (month: Date): number => {
        const from = startOfMonth(month).getTime();
        const to = startOfNextMonth(month).getTime();
        return state.events.filter((e: CalendarEvent) => {
          const bounds = eventBounds(e.start, e.end);
          return bounds !== null && overlaps(bounds, from, to);
        }).length;
      };
    },

    /** "Events this month" quick stat. */
    monthEventCount(state: DashboardState): number {
      return this.eventCountInMonth(new Date(state.now));
    },
  },

  actions: {
    /** Advance the dashboard clock (the page calls this on a 1-minute timer). */
    tick() {
      this.now = Date.now();
    },

    /** Point the mini calendar at `month` and make sure its events are loaded. */
    async setMiniMonth(month: Date) {
      this.miniMonthKey = monthKey(month);
      // Fetch on demand: the initial window only covers this month and the next,
      // so navigating further out would otherwise show a month with no markers.
      await this.ensureMonths(month, 1);
    },

    /**
     * Re-read everything on screen: calendars + address books, then events and
     * the recent-contacts preview in parallel. `fetchCalendars`/`fetchAddressBooks`
     * swallow their own failures into their store's `error`, so this never
     * rejects — a dead calendars call just leaves the widgets empty.
     *
     * Mount uses this same path (the page calls it from `onMounted`). It used to
     * have a separate `load()` that only filled in MISSING months, which meant
     * re-entering the dashboard — the Pinia store outlives client-side route
     * changes — refreshed contacts but showed a stale event snapshot from the
     * first visit: an event created on the calendar page was missing from the
     * agenda, and a deleted one still listed, until the 5-minute timer fired.
     *
     * Unlike a naive reset this does NOT clear `events` first; the fetched result
     * replaces the list in one step, so nothing blinks through an empty state.
     */
    async refresh() {
      this.eventsIncomplete = false;
      this.contactsIncomplete = false;

      const calendarStore = useCalendarStore();
      const contactsStore = useContactsStore();
      await Promise.all([calendarStore.fetchCalendars(), contactsStore.fetchAddressBooks()]);

      await Promise.all([this.reloadEvents(), this.fetchRecentContacts()]);
    },

    /**
     * Refetch the months worth keeping (see {@link retainedMonthKeys}) and
     * replace those months' events, so anything changed or deleted elsewhere
     * shows up. Contiguous months go out as one range per calendar; a month the
     * user jumped to years away is its own range rather than one giant span.
     */
    async reloadEvents() {
      const current = startOfMonth(new Date(this.now));
      const focus = this.miniMonth;
      const retained = retainedMonthKeys(this.now, focus);

      // Ask for what the widgets need whether or not it is already loaded (this
      // month, the next, and the month on screen — the clock may have rolled into
      // a month nothing ever fetched), plus every retained month we do hold.
      const targets = new Set<string>();
      for (let i = 0; i < INITIAL_MONTHS; i++) targets.add(monthKey(addMonths(current, i)));
      targets.add(monthKey(focus));
      for (const key of this.loadedMonths) {
        if (retained.has(key)) targets.add(key);
      }

      const before = new Set(this.loadedMonths);
      const ranges = mergeMonthRanges([...targets]);
      const results = await Promise.all(ranges.map((r: MonthRange) => this.fetchEventRange(r.start, r.end)));

      // Nothing usable came back — no calendars to ask, or every request failed
      // (offline, API 502). Keep what is on screen: replacing it with the empty
      // result would turn an outage into "Nothing scheduled today", a lie the
      // user cannot distinguish from an actually empty calendar. `eventsIncomplete`
      // has been set by then, so the page shows its warning banner.
      const refreshed = ranges.filter((_, i) => results[i] !== null);
      if (refreshed.length === 0) return;

      // Months that appeared WHILE we were awaiting: the user paged the mini
      // calendar and `ensureMonths` merged that month's events after our snapshot.
      // They are outside this reload's window, so they have to survive it — a
      // wholesale replace would both drop those events and unmark the month,
      // leaving a month on screen that nothing would ever refetch.
      const late = this.loadedMonths.filter((key: string) => !before.has(key) && !targets.has(key));
      const keptKeys = [...new Set([...targets, ...late])].sort();

      const fetchedSpans = refreshed.map((r: MonthRange) => ({ start: r.start.getTime(), end: r.end.getTime() }));
      const keptSpans = mergeMonthRanges(keptKeys).map((r: MonthRange) => ({
        start: r.start.getTime(),
        end: r.end.getTime(),
      }));

      const fresh: CalendarEvent[] = [];
      for (const result of results) {
        if (result) fresh.push(...result);
      }

      // An already-held event survives only OUTSIDE the refetched spans (inside
      // them the fresh result is authoritative) and only while its month is still
      // kept. A range whose request failed keeps its old events for now; the next
      // refresh recomputes the same targets and retries it.
      this.events = this.events.filter((e: CalendarEvent) => {
        const bounds = eventBounds(e.start, e.end);
        if (!bounds) return false;
        if (fetchedSpans.some((s) => overlaps(bounds, s.start, s.end))) return false;
        return keptSpans.some((s) => overlaps(bounds, s.start, s.end));
      });
      this.mergeEvents(fresh);
      this.loadedMonths = keptKeys;
    },

    /**
     * Make sure `count` months starting at `anchor` are loaded, fetching only
     * the ones that are missing. Called on mount (current + next month) and
     * whenever the mini calendar navigates to a month we haven't seen.
     */
    async ensureMonths(anchor: Date, count = 1) {
      const calendarStore = useCalendarStore();
      // Nothing to ask yet — leave the months unmarked so a later call retries
      // once the calendar list has arrived.
      if (calendarStore.calendars.length === 0) return;

      const wanted = Array.from({ length: count }, (_, i) => addMonths(anchor, i));
      const missing = wanted.filter((m: Date) => !this.loadedMonths.includes(monthKey(m)));
      if (missing.length === 0) return;

      // Mark before awaiting so two overlapping callers (mount and a month
      // change) can't fetch the same month twice.
      const claimed = missing.map((m: Date) => monthKey(m));
      this.loadedMonths.push(...claimed);

      const start = startOfMonth(missing[0]!);
      const end = startOfNextMonth(missing[missing.length - 1]!);
      const fetched = await this.fetchEventRange(start, end);
      if (fetched === null) {
        // Nothing answered. Give the claim back, or an empty month would look
        // loaded forever and never be retried when the user returns to it.
        const failed = new Set(claimed);
        this.loadedMonths = this.loadedMonths.filter((key: string) => !failed.has(key));
        return;
      }
      this.mergeEvents(fetched);
    },

    /**
     * One request per calendar for `[start, end)`, IN PARALLEL. `allSettled` (not
     * `all`) so a single failing calendar — a share revoked mid-session, say —
     * degrades to a "some events could not be loaded" note instead of blanking
     * every widget.
     *
     * Returns null for "no usable answer": either there was no calendar to ask,
     * or EVERY request failed. Callers must not treat that as an empty result —
     * an offline refresh would otherwise wipe the whole dashboard.
     */
    async fetchEventRange(start: Date, end: Date): Promise<CalendarEvent[] | null> {
      const calendarStore = useCalendarStore();
      const calendars = calendarStore.calendars;
      if (calendars.length === 0) return null;

      const api = useApi();
      this.eventLoads++;
      try {
        const results = await Promise.allSettled(
          calendars.map((c: Calendar) =>
            api<{ events: CalendarEvent[] }>(
              `/api/v1/calendars/${c.uuid}/events?start=${start.toISOString()}&end=${end.toISOString()}`
            )
          )
        );

        const events: CalendarEvent[] = [];
        let answered = 0;
        results.forEach((r, i) => {
          if (r.status === 'fulfilled') {
            answered++;
            if (r.value.events) events.push(...r.value.events);
          } else {
            this.eventsIncomplete = true;
            console.warn(`Dashboard: failed to load events for calendar ${calendars[i]?.name}`, r.reason);
          }
        });
        return answered === 0 ? null : events;
      } finally {
        this.eventLoads--;
      }
    },

    /** Append events not already held, keyed by {@link eventKey}. */
    mergeEvents(incoming: CalendarEvent[]) {
      const seen = new Set(this.events.map(eventKey));
      for (const e of incoming) {
        const key = eventKey(e);
        if (seen.has(key)) continue;
        seen.add(key);
        this.events.push(e);
      }
    },

    /**
     * Recently EDITED contacts, plus the total contact count.
     *
     * The list endpoint sorts server-side (`sort=updated_at&order=desc`), so we
     * only pull the top few from each address book and merge — never the whole
     * book. Its `Total` is the count the quick stats show, which is why this runs
     * even when the preview list itself is not interesting.
     */
    async fetchRecentContacts() {
      const contactsStore = useContactsStore();
      const books = contactsStore.addressBooks;

      this.isLoadingContacts = true;
      try {
        if (books.length === 0) {
          this.recentContacts = [];
          this.totalContacts = 0;
          return;
        }

        const api = useApi();
        const results = await Promise.allSettled(
          books.map((ab: AddressBook) =>
            api<{ Contacts: Contact[]; Total: number; Limit: number; Offset: number }>(
              `/api/v1/addressbooks/${ab.UUID}/contacts?limit=${RECENT_CONTACTS_LIMIT}&offset=0&sort=updated_at&order=desc`
            )
          )
        );

        let total = 0;
        const merged: Contact[] = [];
        results.forEach((r, i) => {
          if (r.status === 'fulfilled') {
            total += r.value.Total || 0;
            merged.push(...(r.value.Contacts || []));
          } else {
            this.contactsIncomplete = true;
            console.warn(`Dashboard: failed to load contacts for address book ${books[i]?.Name}`, r.reason);
          }
        });

        this.totalContacts = total;
        // Each book came back sorted on its own; the merge needs re-sorting
        // before the slice, or a book with older contacts could crowd out newer
        // ones purely by position.
        this.recentContacts = merged
          .sort((a: Contact, b: Contact) => new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime())
          .slice(0, RECENT_CONTACTS_LIMIT);
      } finally {
        this.isLoadingContacts = false;
      }
    },
  },
});
