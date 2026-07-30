import type { Calendar, CalendarEvent } from '~/types/calendar';
import type { AddressBook, Contact } from '~/types/contacts';
import type { ContactHit, EventHit, SearchResults } from '~/types/search';
import { useCalendarStore } from '~/stores/calendars';
import { useContactsStore } from '~/stores/contacts';

/** Below this length we don't search at all (story 044: minimum 2 characters). */
export const MIN_QUERY_LENGTH = 2;

/**
 * There is NO `/api/v1/search` endpoint and no event-search endpoint at all — the
 * only way to look at events is `GET /calendars/:uuid/events?start=&end=`. So event
 * search is a bounded client-side scan: we pull every calendar over a window of
 * ±SEARCH_WINDOW_MONTHS around today and filter summary/location/description here.
 *
 * Six months either way is the compromise: wide enough to cover "that meeting last
 * spring" and next semester's plans, narrow enough that the server-side recurrence
 * expansion (expand defaults to true) stays cheap and the fan-out is one request per
 * calendar. Anything outside the window is invisible to search — the real fix is a
 * server-side search endpoint, see the story's deferred list.
 */
export const SEARCH_WINDOW_MONTHS = 6;

/**
 * Results are cached per query for the session, but only briefly: the cache exists
 * to make backspacing/retyping instant, not to be a second source of truth. A short
 * TTL bounds how long a deleted event or renamed contact can linger, without having
 * to hook every CRUD action in the calendar/contacts stores.
 */
const CACHE_TTL_MS = 60_000;

const RECENT_SEARCHES_KEY = 'calcard:recent-searches';
const MAX_RECENT_SEARCHES = 5;

interface CacheEntry {
  at: number;
  results: SearchResults;
}

interface SearchState {
  /** The query the current `results` belong to (trimmed). */
  query: string;
  results: SearchResults;
  isLoading: boolean;
  error: string | null;
  recentSearches: string[];
  cache: Map<string, CacheEntry>;
  /**
   * Monotonic request counter. Every call to `search()` claims the next number;
   * a fan-out only commits its results if it still holds the latest one. Without
   * this a slow early query (say "a") can resolve after a fast later one ("alice")
   * and overwrite the newer results.
   */
  requestSeq: number;
}

function emptyResults(): SearchResults {
  return { events: [], contacts: [], calendars: [], addressBooks: [] };
}

function matches(haystack: string | undefined | null, needle: string): boolean {
  return !!haystack && haystack.toLowerCase().includes(needle);
}

/** The ±SEARCH_WINDOW_MONTHS window scanned for events, centred on `now`. */
export function eventSearchWindow(now: Date = new Date()): { start: Date; end: Date } {
  const start = new Date(now);
  start.setMonth(start.getMonth() - SEARCH_WINDOW_MONTHS);
  const end = new Date(now);
  end.setMonth(end.getMonth() + SEARCH_WINDOW_MONTHS);
  return { start, end };
}

/**
 * Upcoming occurrences first (soonest first), then past ones (most recent first).
 * A search for "standup" should surface the next standup, not the one in January.
 */
export function sortEventHits(hits: EventHit[], now: number = Date.now()): EventHit[] {
  const ts = (h: EventHit) => new Date(h.event.start).getTime();
  const upcoming = hits.filter((h) => ts(h) >= now).sort((a, b) => ts(a) - ts(b));
  const past = hits.filter((h) => ts(h) < now).sort((a, b) => ts(b) - ts(a));
  return [...upcoming, ...past];
}

export const useSearchStore = defineStore('search', {
  state: (): SearchState => ({
    query: '',
    results: emptyResults(),
    isLoading: false,
    error: null,
    recentSearches: [],
    cache: new Map<string, CacheEntry>(),
    requestSeq: 0,
  }),

  getters: {
    hasResults(state: SearchState): boolean {
      return (
        state.results.events.length > 0 ||
        state.results.contacts.length > 0 ||
        state.results.calendars.length > 0 ||
        state.results.addressBooks.length > 0
      );
    },

    totalCount(state: SearchState): number {
      return (
        state.results.events.length +
        state.results.contacts.length +
        state.results.calendars.length +
        state.results.addressBooks.length
      );
    },
  },

  actions: {
    /**
     * Fan out over the endpoints that actually exist and merge the matches.
     * Safe to call on every keystroke (the caller debounces): late responses from
     * superseded queries are dropped by the requestSeq guard.
     */
    async search(rawQuery: string) {
      const query = rawQuery.trim();
      const needle = query.toLowerCase();
      this.query = query;

      // Claim a request number even for a too-short query, so clearing the input
      // also invalidates whatever fan-out is still in flight.
      const token = ++this.requestSeq;

      if (query.length < MIN_QUERY_LENGTH) {
        this.results = emptyResults();
        this.error = null;
        this.isLoading = false;
        return;
      }

      const cached = this.cache.get(needle);
      if (cached && Date.now() - cached.at < CACHE_TTL_MS) {
        this.results = cached.results;
        this.error = null;
        this.isLoading = false;
        return;
      }

      this.isLoading = true;
      this.error = null;

      try {
        // Calendars and address books are needed both as searchable items and to
        // label/colour event and contact hits, and the header search can be opened
        // from any page (so neither list is guaranteed to be loaded yet).
        await this.ensureCollectionsLoaded();

        const [contactsSettled, eventsSettled] = await Promise.allSettled([
          this.searchContactHits(query),
          this.searchEventHits(query),
        ]);

        // Stale: a newer search claimed the counter while we were awaiting. Drop
        // everything — including isLoading, which the newer request now owns.
        if (token !== this.requestSeq) return;

        const contactHits = contactsSettled.status === 'fulfilled' ? contactsSettled.value : [];
        const eventHits = eventsSettled.status === 'fulfilled' ? eventsSettled.value : [];

        if (contactsSettled.status === 'rejected' && eventsSettled.status === 'rejected') {
          this.error = (contactsSettled.reason as Error)?.message || 'Search failed';
        } else {
          if (contactsSettled.status === 'rejected') {
            console.warn('Contact search failed', contactsSettled.reason);
          }
          if (eventsSettled.status === 'rejected') {
            console.warn('Event search failed', eventsSettled.reason);
          }
        }

        const calendarStore = useCalendarStore();
        const contactsStore = useContactsStore();

        const results: SearchResults = {
          events: eventHits,
          contacts: contactHits,
          calendars: calendarStore.calendars.filter(
            (c: Calendar) => matches(c.name, needle) || matches(c.description, needle)
          ),
          addressBooks: contactsStore.addressBooks.filter(
            (ab: AddressBook) => matches(ab.Name, needle) || matches(ab.Description, needle)
          ),
        };

        this.results = results;
        // Never cache a partial/failed result set — it would stick around as if
        // it were the truth for the whole TTL.
        if (!this.error && contactsSettled.status === 'fulfilled' && eventsSettled.status === 'fulfilled') {
          this.cache.set(needle, { at: Date.now(), results });
        }
      } catch (e: unknown) {
        if (token !== this.requestSeq) return;
        this.error = (e as Error).message || 'Search failed';
        this.results = emptyResults();
      } finally {
        // Only the newest request may clear the spinner.
        if (token === this.requestSeq) this.isLoading = false;
      }
    },

    /** Loads calendars / address books once, tolerating either one failing. */
    async ensureCollectionsLoaded() {
      const calendarStore = useCalendarStore();
      const contactsStore = useContactsStore();
      const tasks: Promise<unknown>[] = [];
      if (calendarStore.calendars.length === 0) tasks.push(calendarStore.fetchCalendars());
      if (contactsStore.addressBooks.length === 0) tasks.push(contactsStore.fetchAddressBooks());
      if (tasks.length > 0) await Promise.allSettled(tasks);
    },

    /**
     * Contacts DO have a server-side search endpoint. Deliberately calls it
     * directly instead of reusing contactsStore.searchContacts(), which replaces
     * the contacts page's list state — global search must not disturb the page
     * behind the dialog.
     */
    async searchContactHits(query: string): Promise<ContactHit[]> {
      const api = useApi();
      const contactsStore = useContactsStore();
      // Raw JSON, NOT the { status, data } envelope (see webinterface/CLAUDE.md).
      const response = await api<{ contacts: Contact[]; query: string; count: number }>(
        `/api/v1/contacts/search?q=${encodeURIComponent(query)}&limit=200`
      );
      return (response.contacts || []).map((contact: Contact) => ({
        contact,
        addressBookId: contact.addressbook_id,
        addressBookName:
          contactsStore.getAddressBookByNumericId(contact.addressbook_id)?.Name ?? '',
      }));
    },

    /** Client-side event scan over the bounded window — see SEARCH_WINDOW_MONTHS. */
    async searchEventHits(query: string): Promise<EventHit[]> {
      const api = useApi();
      const calendarStore = useCalendarStore();
      const needle = query.toLowerCase();
      const { start, end } = eventSearchWindow();
      const calendars = calendarStore.calendars;

      const settled = await Promise.allSettled(
        calendars.map((c: Calendar) =>
          api<{ events: CalendarEvent[] }>(
            `/api/v1/calendars/${c.uuid}/events?start=${start.toISOString()}&end=${end.toISOString()}`
          )
        )
      );

      const hits: EventHit[] = [];
      let failures = 0;
      settled.forEach((r, i) => {
        const calendar = calendars[i];
        if (r.status !== 'fulfilled' || !calendar) {
          failures++;
          if (r.status === 'rejected') {
            console.warn(`Search failed for calendar ${calendars[i]?.name}`, r.reason);
          }
          return;
        }
        for (const event of r.value.events || []) {
          if (
            matches(event.summary, needle) ||
            matches(event.location, needle) ||
            matches(event.description, needle)
          ) {
            hits.push({
              key: `${event.id}::${event.recurrence_id || ''}`,
              event,
              calendarId: String(event.calendar_id),
              calendarName: calendar.name,
              calendarColor: calendar.color || '#3b82f6',
            });
          }
        }
      });

      // Every calendar erroring is a real failure (auth expired, server down), not
      // "no matches" — surface it so the caller can report it instead of silently
      // showing an empty Events section.
      if (calendars.length > 0 && failures === calendars.length) {
        throw new Error('Failed to search events');
      }

      return sortEventHits(hits);
    },

    reset() {
      // Invalidate in-flight fan-outs too: closing the dialog must not let a late
      // response repopulate the results.
      this.requestSeq++;
      this.query = '';
      this.results = emptyResults();
      this.isLoading = false;
      this.error = null;
    },

    loadRecentSearches() {
      try {
        const raw = localStorage.getItem(RECENT_SEARCHES_KEY);
        if (!raw) return;
        const parsed: unknown = JSON.parse(raw);
        if (!Array.isArray(parsed)) return;
        this.recentSearches = parsed
          .filter((v: unknown): v is string => typeof v === 'string' && v.trim().length > 0)
          .slice(0, MAX_RECENT_SEARCHES);
      } catch {
        // Corrupt JSON or localStorage unavailable (private mode): recents are a
        // convenience, never a reason to break search.
      }
    },

    /**
     * Recorded when a result is CHOSEN, not on every debounced keystroke —
     * otherwise the list fills with half-typed prefixes ("al", "ali", "alic").
     */
    rememberSearch(rawQuery: string) {
      const query = rawQuery.trim();
      if (query.length < MIN_QUERY_LENGTH) return;
      const deduped = this.recentSearches.filter(
        (q: string) => q.toLowerCase() !== query.toLowerCase()
      );
      this.recentSearches = [query, ...deduped].slice(0, MAX_RECENT_SEARCHES);
      this.persistRecentSearches();
    },

    clearRecentSearches() {
      this.recentSearches = [];
      this.persistRecentSearches();
    },

    persistRecentSearches() {
      try {
        localStorage.setItem(RECENT_SEARCHES_KEY, JSON.stringify(this.recentSearches));
      } catch {
        // Storage full / unavailable — keep the in-memory list.
      }
    },

    clearCache() {
      this.cache.clear();
    },
  },
});
