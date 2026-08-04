import type {
  ContactHit,
  EventHit,
  SearchApiContactItem,
  SearchApiEventItem,
  SearchApiResponse,
  SearchResults,
} from '~/types/search';

/** Below this length we don't search at all (story 044: minimum 2 characters). */
export const MIN_QUERY_LENGTH = 2;

/**
 * Items requested per category from `GET /api/v1/search` (#156). The server caps
 * `limit` at 100 and echoes the cap as `max_limit`, so asking for exactly the cap
 * keeps the request honest: whatever comes back is everything the server will
 * give for this query, and `has_more` says whether it truncated.
 *
 * Before #156 this store fanned out one events request per calendar over a
 * rolling ±6-month window and filtered in the browser, which made any event
 * outside that window impossible to find. There is no client-side date window
 * any more — the endpoint searches every calendar the user can read, unbounded.
 */
export const SEARCH_LIMIT = 100;

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
   * Monotonic request counter. Every call to `search()` claims the next number
   * and only commits its response if it still holds the latest one. Without this
   * a slow early query (say "a") can resolve after a fast later one ("alice")
   * and overwrite the newer results.
   */
  requestSeq: number;
}

function emptyResults(): SearchResults {
  return {
    events: [],
    contacts: [],
    calendars: [],
    addressBooks: [],
    hasMore: { events: false, contacts: false, calendars: false, addressBooks: false },
  };
}

/**
 * Maps the endpoint's grouped payload onto the row shapes the palette and the
 * results page render. Ranking is NOT re-applied here: the server returns events
 * upcoming-first then past, contacts by name, and re-sorting a truncated page
 * client-side would only reorder an arbitrary slice.
 */
export function toSearchResults(response: SearchApiResponse): SearchResults {
  const events: EventHit[] = (response.events?.items || []).map((item: SearchApiEventItem) => ({
    key: `${item.event.id}::${item.event.recurrence_id || ''}`,
    event: item.event,
    // Numeric id as a string — the deep link into the calendar view uses it.
    calendarId: String(item.event.calendar_id),
    calendarName: item.calendar_name,
    calendarColor: item.calendar_color || '#3b82f6',
  }));

  const contacts: ContactHit[] = (response.contacts?.items || []).map((item: SearchApiContactItem) => ({
    contact: item.contact,
    // Contact.addressbook_id is the NUMERIC id (matches AddressBook.ID, never
    // its UUID) — see the note in CLAUDE.md.
    addressBookId: item.contact.addressbook_id,
    addressBookName: item.addressbook_name,
  }));

  return {
    events,
    contacts,
    calendars: response.calendars?.items || [],
    addressBooks: response.addressbooks?.items || [],
    hasMore: {
      events: !!response.events?.has_more,
      contacts: !!response.contacts?.has_more,
      calendars: !!response.calendars?.has_more,
      addressBooks: !!response.addressbooks?.has_more,
    },
  };
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
     * One request to `GET /api/v1/search`. Safe to call on every keystroke (the
     * caller debounces): late responses from superseded queries are dropped by
     * the requestSeq guard, which is still needed even with a single request —
     * `useApi` retries once on a 401 and spends a whole token refresh before
     * re-issuing, so responses really do arrive out of order.
     */
    async search(rawQuery: string) {
      const query = rawQuery.trim();
      const needle = query.toLowerCase();
      this.query = query;

      // Claim a request number even for a too-short query, so clearing the input
      // also invalidates whatever request is still in flight.
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
        const api = useApi();
        // Raw JSON, NOT the { status, data } envelope (see webinterface/CLAUDE.md).
        // Every hit arrives self-contained — calendar name/colour, address book
        // name — so nothing has to be joined against the calendar/contacts
        // stores, and the palette works on a page that never loaded them.
        const response = await api<SearchApiResponse>(
          `/api/v1/search?q=${encodeURIComponent(query)}&limit=${SEARCH_LIMIT}`
        );

        // Stale: a newer search claimed the counter while we were awaiting. Drop
        // everything — including isLoading, which the newer request now owns.
        if (token !== this.requestSeq) return;

        const results = toSearchResults(response);
        this.results = results;
        this.cache.set(needle, { at: Date.now(), results });
      } catch (e: unknown) {
        if (token !== this.requestSeq) return;
        // A failed search is reported, never rendered as "no results": an empty
        // Contacts section after a 500 reads as "this person isn't in my address
        // book", which is worse than an error.
        this.error = (e as Error)?.message || 'Search failed';
        this.results = emptyResults();
      } finally {
        // Only the newest request may clear the spinner.
        if (token === this.requestSeq) this.isLoading = false;
      }
    },

    reset() {
      // Invalidate the in-flight request too: closing the dialog must not let a
      // late response repopulate the results.
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
