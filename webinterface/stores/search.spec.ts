// @vitest-environment nuxt
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import { createTestingPinia } from '@pinia/testing';
import { MIN_QUERY_LENGTH, SEARCH_LIMIT, toSearchResults, useSearchStore } from './search';
import { useCalendarStore } from './calendars';
import { useContactsStore } from './contacts';
import type { Calendar, CalendarEvent } from '~/types/calendar';
import type { AddressBook, Contact } from '~/types/contacts';
import type { SearchApiResponse } from '~/types/search';

const { apiMock } = vi.hoisted(() => ({ apiMock: vi.fn() }));
mockNuxtImport('useApi', () => () => apiMock);

// Frozen "now" so ordering assertions are deterministic.
const NOW = new Date('2026-06-15T12:00:00Z');

function calendar(id: number, name = `Calendar ${id}`, color = '#111111'): Calendar {
  return {
    id: String(id),
    uuid: `cal-uuid-${id}`,
    path: `/cal/${id}`,
    name,
    color,
    owner_id: '1',
    created_at: NOW.toISOString(),
    updated_at: NOW.toISOString(),
  };
}

function event(id: string, calendarId: number, summary: string, start: string, extra: Partial<CalendarEvent> = {}): CalendarEvent {
  return {
    id,
    calendar_id: calendarId,
    uid: `uid-${id}`,
    summary,
    start,
    end: start,
    all_day: false,
    is_recurring: false,
    ...extra,
  };
}

function book(id: number, name = `Book ${id}`, description = ''): AddressBook {
  return {
    ID: id,
    UUID: `ab-uuid-${id}`,
    UserID: 1,
    Path: `/ab/${id}`,
    Name: name,
    Description: description,
    CreatedAt: NOW.toISOString(),
    UpdatedAt: NOW.toISOString(),
  };
}

function contact(id: string, name: string, abId = 1): Contact {
  return {
    id,
    addressbook_id: String(abId),
    uid: `uid-${id}`,
    formatted_name: name,
    created_at: NOW.toISOString(),
    updated_at: NOW.toISOString(),
  };
}

/** Builds a `GET /api/v1/search` payload, defaulting every group to empty. */
function payload(partial: {
  events?: { event: CalendarEvent; calendar_name?: string; calendar_color?: string; calendar_uuid?: string }[];
  contacts?: { contact: Contact; addressbook_name?: string; addressbook_uuid?: string }[];
  calendars?: Calendar[];
  addressbooks?: AddressBook[];
  hasMore?: Partial<Record<'events' | 'contacts' | 'calendars' | 'addressbooks', boolean>>;
} = {}): SearchApiResponse {
  const group = <T>(items: T[], has_more = false) => ({
    items,
    count: items.length,
    has_more,
    searched: true,
  });
  return {
    query: 'q',
    types: ['events', 'contacts', 'calendars', 'addressbooks'],
    limit: SEARCH_LIMIT,
    offset: 0,
    max_limit: 100,
    events: group(
      (partial.events || []).map((e) => ({
        event: e.event,
        calendar_uuid: e.calendar_uuid ?? 'cal-uuid-1',
        calendar_name: e.calendar_name ?? 'Work',
        calendar_color: e.calendar_color ?? '#ff0000',
      })),
      partial.hasMore?.events
    ),
    contacts: group(
      (partial.contacts || []).map((c) => ({
        contact: c.contact,
        addressbook_uuid: c.addressbook_uuid ?? 'ab-uuid-1',
        addressbook_name: c.addressbook_name ?? 'My Contacts',
      })),
      partial.hasMore?.contacts
    ),
    calendars: group(partial.calendars || [], partial.hasMore?.calendars),
    addressbooks: group(partial.addressbooks || [], partial.hasMore?.addressbooks),
  };
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

/**
 * The nuxt/happy-dom test environment exposes a `localStorage` OBJECT WITH NO
 * METHODS (getItem/setItem are undefined), so recent-search persistence can't be
 * exercised against it — stub a minimal in-memory Storage instead. The store
 * guards every access in try/catch precisely because environments like that (and
 * Safari private mode) exist; see the "tolerates a broken localStorage" test.
 */
function stubStorage() {
  const backing = new Map<string, string>();
  vi.stubGlobal('localStorage', {
    getItem: (k: string) => backing.get(k) ?? null,
    setItem: (k: string, v: string) => void backing.set(k, String(v)),
    removeItem: (k: string) => void backing.delete(k),
    clear: () => backing.clear(),
  });
  return backing;
}

beforeEach(() => {
  createTestingPinia({ stubActions: false });
  apiMock.mockReset();
  stubStorage();
  vi.useFakeTimers();
  vi.setSystemTime(NOW);
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe('minimum query length', () => {
  it('clears results and issues no request below 2 characters', async () => {
    apiMock.mockResolvedValue(payload({ contacts: [{ contact: contact('c1', 'Alice') }] }));
    const store = useSearchStore();

    await store.search('a');

    expect(apiMock).not.toHaveBeenCalled();
    expect(store.results.contacts).toEqual([]);
    expect(store.isLoading).toBe(false);
    expect(store.query).toBe('a');
    expect(MIN_QUERY_LENGTH).toBe(2);
  });

  it('trims before measuring, so "  a " is still too short', async () => {
    const store = useSearchStore();
    await store.search('  a ');
    expect(apiMock).not.toHaveBeenCalled();
    expect(store.query).toBe('a');
  });
});

describe('the single search request', () => {
  it('hits /api/v1/search once with the query and the per-group limit', async () => {
    apiMock.mockResolvedValue(payload());
    const store = useSearchStore();

    await store.search('standup');

    expect(apiMock).toHaveBeenCalledTimes(1);
    const url = apiMock.mock.calls[0]![0] as string;
    expect(url).toContain('/api/v1/search');
    expect(url).toContain('q=standup');
    expect(url).toContain(`limit=${SEARCH_LIMIT}`);
  });

  it('percent-encodes the query', async () => {
    apiMock.mockResolvedValue(payload());
    await useSearchStore().search('a & b');
    expect(apiMock.mock.calls[0]![0] as string).toContain('q=a%20%26%20b');
  });

  // The whole point of #156: no ±6-month window is imposed, so the store must
  // not send date bounds of its own.
  it('sends no start/end bounds', async () => {
    apiMock.mockResolvedValue(payload());
    await useSearchStore().search('standup');
    const url = apiMock.mock.calls[0]![0] as string;
    expect(url).not.toContain('start=');
    expect(url).not.toContain('end=');
  });

  // Every hit is self-contained, so the palette works on a page that never
  // loaded the calendar or contacts lists.
  it('does not fetch the calendar or address book lists', async () => {
    apiMock.mockResolvedValue(payload({ calendars: [calendar(1, 'Family')] }));
    const store = useSearchStore();

    await store.search('family');

    expect(apiMock.mock.calls.map((c) => c[0] as string)).toEqual([
      expect.stringContaining('/api/v1/search'),
    ]);
    expect(store.results.calendars.map((c) => c.name)).toEqual(['Family']);
  });

  it('leaves the contacts page state untouched', async () => {
    apiMock.mockResolvedValue(payload({ contacts: [{ contact: contact('c1', 'Alice') }] }));

    const contactsStore = useContactsStore();
    contactsStore.contacts = [contact('page-1', 'Someone Else')];
    useCalendarStore().calendars = [calendar(1)];

    await useSearchStore().search('ali');

    expect(contactsStore.searchQuery).toBe('');
    expect(contactsStore.contacts.map((c) => c.id)).toEqual(['page-1']);
  });
});

describe('mapping the response', () => {
  it('carries calendar metadata from the server onto event hits', async () => {
    apiMock.mockResolvedValue(
      payload({
        events: [
          {
            event: event('e1', 7, 'Team Standup', '2026-06-16T09:00:00Z'),
            calendar_name: 'Shared Family',
            calendar_color: '#00ff00',
            calendar_uuid: 'cal-uuid-9',
          },
        ],
      })
    );

    const store = useSearchStore();
    await store.search('standup');

    const hit = store.results.events[0]!;
    expect(hit.calendarName).toBe('Shared Family');
    expect(hit.calendarColor).toBe('#00ff00');
    // The deep link uses the NUMERIC calendar id carried on the event itself.
    expect(hit.calendarId).toBe('7');
  });

  it('falls back to a default colour when the calendar has none', () => {
    const mapped = toSearchResults(
      payload({ events: [{ event: event('e1', 1, 'X', '2026-06-16T09:00:00Z'), calendar_color: '' }] })
    );
    expect(mapped.events[0]!.calendarColor).toBe('#3b82f6');
  });

  it('gives recurring occurrences distinct keys', () => {
    const mapped = toSearchResults(
      payload({
        events: [
          { event: event('series', 1, 'Standup', '2026-06-16T09:00:00Z', { recurrence_id: '2026-06-16T09:00:00Z' }) },
          { event: event('series', 1, 'Standup', '2026-06-17T09:00:00Z', { recurrence_id: '2026-06-17T09:00:00Z' }) },
        ],
      })
    );
    expect(new Set(mapped.events.map((h) => h.key)).size).toBe(2);
  });

  it('labels contacts with the address book name the server supplied', async () => {
    apiMock.mockResolvedValue(
      payload({ contacts: [{ contact: contact('c1', 'Alice Adams', 2), addressbook_name: 'Work' }] })
    );

    const store = useSearchStore();
    await store.search('ali');

    // addressbook_id is a NUMERIC STRING matching AddressBook.ID, never the UUID.
    expect(store.results.contacts[0]!.addressBookId).toBe('2');
    expect(store.results.contacts[0]!.addressBookName).toBe('Work');
  });

  it('preserves the order the server ranked results in', async () => {
    apiMock.mockResolvedValue(
      payload({
        events: [
          { event: event('near', 1, 'Review soon', '2026-06-16T09:00:00Z') },
          { event: event('far', 1, 'Review later', '2027-01-04T09:00:00Z') },
          { event: event('past', 1, 'Review past', '2026-06-01T09:00:00Z') },
        ],
      })
    );

    const store = useSearchStore();
    await store.search('review');

    expect(store.results.events.map((h) => h.event.id)).toEqual(['near', 'far', 'past']);
  });

  it('tolerates missing groups in the payload', () => {
    const mapped = toSearchResults({} as SearchApiResponse);
    expect(mapped.events).toEqual([]);
    expect(mapped.contacts).toEqual([]);
    expect(mapped.calendars).toEqual([]);
    expect(mapped.addressBooks).toEqual([]);
    expect(mapped.hasMore).toEqual({ events: false, contacts: false, calendars: false, addressBooks: false });
  });

  it('exposes per-category truncation so a capped count can be shown as "N+"', async () => {
    apiMock.mockResolvedValue(
      payload({
        events: [{ event: event('e1', 1, 'Sprint', '2026-06-16T09:00:00Z') }],
        hasMore: { events: true },
      })
    );

    const store = useSearchStore();
    await store.search('sprint');

    expect(store.results.hasMore.events).toBe(true);
    expect(store.results.hasMore.contacts).toBe(false);
  });

  it('counts every category in totalCount and hasResults', async () => {
    apiMock.mockResolvedValue(
      payload({
        events: [{ event: event('e1', 1, 'Family dinner', '2026-06-16T09:00:00Z') }],
        calendars: [calendar(1, 'Family')],
        addressbooks: [book(1, 'Family & Friends')],
      })
    );

    const store = useSearchStore();
    await store.search('family');

    expect(store.hasResults).toBe(true);
    expect(store.totalCount).toBe(3);
  });
});

// The guard is still required with one request: useApi retries once on a 401 and
// spends a whole token refresh before re-issuing, so responses do arrive out of
// order.
describe('stale-response guard', () => {
  it('drops a slow earlier query that resolves after a newer one', async () => {
    const slow = deferred<SearchApiResponse>();
    const fast = deferred<SearchApiResponse>();
    apiMock.mockImplementation((url: string) =>
      url.includes('q=slow') ? slow.promise : fast.promise
    );

    const store = useSearchStore();
    const slowSearch = store.search('slow');
    const fastSearch = store.search('fast');

    fast.resolve(payload({ contacts: [{ contact: contact('fast-1', 'Fast Match') }] }));
    await fastSearch;
    expect(store.results.contacts.map((h) => h.contact.id)).toEqual(['fast-1']);

    slow.resolve(payload({ contacts: [{ contact: contact('slow-1', 'Slow Match') }] }));
    await slowSearch;

    expect(store.query).toBe('fast');
    expect(store.results.contacts.map((h) => h.contact.id)).toEqual(['fast-1']);
    expect(store.isLoading).toBe(false);
  });

  it('a stale response does not clear the spinner owned by the newer query', async () => {
    const slow = deferred<SearchApiResponse>();
    const pending = deferred<SearchApiResponse>();
    apiMock.mockImplementation((url: string) =>
      url.includes('q=slow') ? slow.promise : pending.promise
    );

    const store = useSearchStore();
    const slowSearch = store.search('slow');
    store.search('newer'); // still in flight

    slow.resolve(payload());
    await slowSearch;

    expect(store.isLoading).toBe(true);
  });

  it('a stale FAILURE does not overwrite the newer query results', async () => {
    const slow = deferred<SearchApiResponse>();
    apiMock.mockImplementation((url: string) =>
      url.includes('q=slow') ? slow.promise : Promise.resolve(payload({ contacts: [{ contact: contact('c1', 'Alice') }] }))
    );

    const store = useSearchStore();
    const slowSearch = store.search('slow');
    await store.search('alice');

    slow.reject(new Error('boom'));
    await slowSearch;

    expect(store.error).toBeNull();
    expect(store.results.contacts).toHaveLength(1);
  });

  it('reset() invalidates an in-flight request', async () => {
    const inflight = deferred<SearchApiResponse>();
    apiMock.mockImplementation(() => inflight.promise);

    const store = useSearchStore();
    const search = store.search('alice');
    store.reset();

    inflight.resolve(payload({ contacts: [{ contact: contact('c1', 'Alice') }] }));
    await search;

    expect(store.query).toBe('');
    expect(store.results.contacts).toEqual([]);
    expect(store.isLoading).toBe(false);
  });

  it('short-query clear also invalidates an in-flight request', async () => {
    const inflight = deferred<SearchApiResponse>();
    apiMock.mockImplementation(() => inflight.promise);

    const store = useSearchStore();
    const search = store.search('alice');
    await store.search(''); // user cleared the input

    inflight.resolve(payload({ contacts: [{ contact: contact('c1', 'Alice') }] }));
    await search;

    expect(store.results.contacts).toEqual([]);
  });
});

// A failed search must never look like "nothing matched" — an empty Contacts
// section after a 500 reads as "this person isn't in my address book".
describe('failure reporting', () => {
  it('surfaces the error and shows no results', async () => {
    apiMock.mockRejectedValue(new Error('500 Internal Server Error'));

    const store = useSearchStore();
    await store.search('alice');

    expect(store.error).toBe('500 Internal Server Error');
    expect(store.hasResults).toBe(false);
    expect(store.isLoading).toBe(false);
  });

  it('falls back to a generic message when the error carries none', async () => {
    apiMock.mockRejectedValue(new Error(''));
    const store = useSearchStore();
    await store.search('alice');
    expect(store.error).toBe('Search failed');
  });

  it('does not cache a failed search', async () => {
    apiMock.mockRejectedValue(new Error('nope'));
    const store = useSearchStore();

    await store.search('ali');
    await store.search('ali');

    expect(apiMock.mock.calls.length).toBe(2);
  });
});

describe('session cache', () => {
  it('serves a repeated query from cache without a new request', async () => {
    apiMock.mockResolvedValue(payload({ contacts: [{ contact: contact('c1', 'Alice') }] }));
    const store = useSearchStore();

    await store.search('ali');
    await store.search('ALI'); // case-insensitive cache key

    expect(apiMock.mock.calls.length).toBe(1);
    expect(store.results.contacts).toHaveLength(1);
  });

  it('re-queries once the entry has expired', async () => {
    apiMock.mockResolvedValue(payload({ contacts: [{ contact: contact('c1', 'Alice') }] }));
    const store = useSearchStore();

    await store.search('ali');
    vi.setSystemTime(new Date(NOW.getTime() + 120_000));
    await store.search('ali');

    expect(apiMock.mock.calls.length).toBe(2);
  });

  it('clearCache forces the next identical query to re-request', async () => {
    apiMock.mockResolvedValue(payload());
    const store = useSearchStore();

    await store.search('ali');
    store.clearCache();
    await store.search('ali');

    expect(apiMock.mock.calls.length).toBe(2);
  });
});

describe('recent searches', () => {
  it('records chosen queries newest-first, de-duplicated case-insensitively, capped at 5', () => {
    const store = useSearchStore();

    store.rememberSearch('alice');
    store.rememberSearch('bob');
    store.rememberSearch('ALICE');
    expect(store.recentSearches).toEqual(['ALICE', 'bob']);

    ['c1', 'c2', 'c3', 'c4'].forEach((q) => store.rememberSearch(q));
    expect(store.recentSearches).toEqual(['c4', 'c3', 'c2', 'c1', 'ALICE']);
  });

  it('ignores queries below the minimum length', () => {
    const store = useSearchStore();
    store.rememberSearch('a');
    expect(store.recentSearches).toEqual([]);
  });

  it('round-trips through localStorage and survives corrupt data', () => {
    const store = useSearchStore();
    store.rememberSearch('alice');

    store.recentSearches = [];
    store.loadRecentSearches();
    expect(store.recentSearches).toEqual(['alice']);

    localStorage.setItem('calcard:recent-searches', 'not json');
    store.recentSearches = [];
    store.loadRecentSearches();
    expect(store.recentSearches).toEqual([]);

    localStorage.setItem('calcard:recent-searches', JSON.stringify(['ok', 42, '']));
    store.loadRecentSearches();
    expect(store.recentSearches).toEqual(['ok']);
  });

  it('tolerates a broken localStorage (no methods / throwing accessor)', () => {
    vi.stubGlobal('localStorage', {
      getItem: () => {
        throw new Error('denied');
      },
      setItem: () => {
        throw new Error('denied');
      },
    });

    const store = useSearchStore();
    expect(() => store.loadRecentSearches()).not.toThrow();
    expect(() => store.rememberSearch('alice')).not.toThrow();
    // The in-memory list still works even when persistence doesn't.
    expect(store.recentSearches).toEqual(['alice']);
  });

  it('clearRecentSearches empties the persisted list', () => {
    const store = useSearchStore();
    store.rememberSearch('alice');
    store.clearRecentSearches();

    expect(store.recentSearches).toEqual([]);
    store.loadRecentSearches();
    expect(store.recentSearches).toEqual([]);
  });
});
