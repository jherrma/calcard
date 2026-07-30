import type { Calendar, CalendarEvent } from '~/types/calendar';
import type { AddressBook, Contact } from '~/types/contacts';
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

    /**
     * First load: calendars + address books, then the event window and the
     * recent-contacts preview in parallel. `fetchCalendars`/`fetchAddressBooks`
     * swallow their own failures into their store's `error`, so this never
     * rejects — a dead calendars call just leaves the widgets empty.
     */
    async load() {
      const calendarStore = useCalendarStore();
      const contactsStore = useContactsStore();

      await Promise.all([calendarStore.fetchCalendars(), contactsStore.fetchAddressBooks()]);

      await Promise.all([
        this.ensureMonths(new Date(this.now), INITIAL_MONTHS),
        this.fetchRecentContacts(),
      ]);
    },

    /**
     * Re-read everything currently on screen. Unlike a naive reset this does NOT
     * clear `events` first: the fetched result REPLACES the list in one step, so
     * deletions made elsewhere disappear without the widgets blinking through an
     * empty state (this runs on a background timer as well as the refresh button).
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
     * Refetch every month already loaded and replace the event list. Loaded
     * months may be non-contiguous (the user can jump around in the mini
     * calendar); refetching the whole min..max span in one request per calendar
     * is cheaper than one request per gap, and fills the gaps as a side effect.
     */
    async reloadEvents() {
      const anchors = this.loadedMonths
        .map((key: string) => monthFromKey(key))
        .filter((d): d is Date => d !== null)
        .sort((a: Date, b: Date) => a.getTime() - b.getTime());
      if (anchors.length === 0) {
        await this.ensureMonths(new Date(this.now), INITIAL_MONTHS);
        return;
      }

      const start = startOfMonth(anchors[0]!);
      const end = startOfNextMonth(anchors[anchors.length - 1]!);
      const fresh = await this.fetchEventRange(start, end);
      if (fresh === null) return; // no calendars to ask; keep what we have

      this.events = fresh;
      // Everything between the ends is loaded now, including any gap months.
      const keys: string[] = [];
      for (let cursor = start; cursor.getTime() < end.getTime(); cursor = startOfNextMonth(cursor)) {
        keys.push(monthKey(cursor));
      }
      this.loadedMonths = keys;
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
      this.loadedMonths.push(...missing.map((m: Date) => monthKey(m)));

      const start = startOfMonth(missing[0]!);
      const end = startOfNextMonth(missing[missing.length - 1]!);
      const fetched = await this.fetchEventRange(start, end);
      if (fetched) this.mergeEvents(fetched);
    },

    /**
     * One request per calendar for `[start, end)`, IN PARALLEL. `allSettled` (not
     * `all`) so a single failing calendar — a share revoked mid-session, say —
     * degrades to a "some events could not be loaded" note instead of blanking
     * every widget. Returns null when there are no calendars to query.
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
        results.forEach((r, i) => {
          if (r.status === 'fulfilled') {
            if (r.value.events) events.push(...r.value.events);
          } else {
            this.eventsIncomplete = true;
            console.warn(`Dashboard: failed to load events for calendar ${calendars[i]?.name}`, r.reason);
          }
        });
        return events;
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
