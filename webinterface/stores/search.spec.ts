// @vitest-environment nuxt
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import { createTestingPinia } from '@pinia/testing';
import { eventSearchWindow, SEARCH_WINDOW_MONTHS, sortEventHits, useSearchStore } from './search';
import { useCalendarStore } from './calendars';
import { useContactsStore } from './contacts';
import type { Calendar, CalendarEvent } from '~/types/calendar';
import type { AddressBook, Contact } from '~/types/contacts';
import type { EventHit } from '~/types/search';

const { apiMock } = vi.hoisted(() => ({ apiMock: vi.fn() }));
mockNuxtImport('useApi', () => () => apiMock);

// Frozen "now" so window boundaries and past/upcoming ordering are deterministic.
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

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

/** Routes the single api mock by URL: contacts search vs per-calendar event list. */
function respond(handlers: {
  contacts?: (url: string) => unknown;
  events?: (url: string) => unknown;
}) {
  apiMock.mockImplementation((url: string) => {
    if (url.includes('/contacts/search')) {
      return Promise.resolve(handlers.contacts ? handlers.contacts(url) : { contacts: [], query: '', count: 0 });
    }
    return Promise.resolve(handlers.events ? handlers.events(url) : { events: [] });
  });
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
    respond({ contacts: () => ({ contacts: [contact('c1', 'Alice')], query: 'a', count: 1 }) });
    const store = useSearchStore();

    await store.search('a');

    expect(apiMock).not.toHaveBeenCalled();
    expect(store.results.contacts).toEqual([]);
    expect(store.isLoading).toBe(false);
    expect(store.query).toBe('a');
  });

  it('trims before measuring, so "  a " is still too short', async () => {
    const store = useSearchStore();
    await store.search('  a ');
    expect(apiMock).not.toHaveBeenCalled();
    expect(store.query).toBe('a');
  });
});

describe('stale-response guard', () => {
  it('drops a slow earlier query that resolves after a newer one', async () => {
    const slow = deferred<{ contacts: Contact[]; query: string; count: number }>();
    const fast = deferred<{ contacts: Contact[]; query: string; count: number }>();

    apiMock.mockImplementation((url: string) => {
      if (url.includes('/contacts/search')) {
        return url.includes('q=slow') ? slow.promise : fast.promise;
      }
      return Promise.resolve({ events: [] });
    });

    const store = useSearchStore();
    const slowSearch = store.search('slow');
    const fastSearch = store.search('fast');

    // Newer query lands first…
    fast.resolve({ contacts: [contact('fast-1', 'Fast Match')], query: 'fast', count: 1 });
    await fastSearch;
    expect(store.results.contacts.map((h) => h.contact.id)).toEqual(['fast-1']);

    // …then the superseded one, which must not clobber it.
    slow.resolve({ contacts: [contact('slow-1', 'Slow Match')], query: 'slow', count: 1 });
    await slowSearch;

    expect(store.query).toBe('fast');
    expect(store.results.contacts.map((h) => h.contact.id)).toEqual(['fast-1']);
    expect(store.isLoading).toBe(false);
  });

  it('a stale response does not clear the spinner owned by the newer query', async () => {
    const slow = deferred<{ contacts: Contact[]; query: string; count: number }>();
    const pending = deferred<{ contacts: Contact[]; query: string; count: number }>();

    apiMock.mockImplementation((url: string) => {
      if (url.includes('/contacts/search')) {
        return url.includes('q=slow') ? slow.promise : pending.promise;
      }
      return Promise.resolve({ events: [] });
    });

    const store = useSearchStore();
    const slowSearch = store.search('slow');
    store.search('newer'); // still in flight

    slow.resolve({ contacts: [], query: 'slow', count: 0 });
    await slowSearch;

    expect(store.isLoading).toBe(true);
  });

  it('reset() invalidates an in-flight fan-out', async () => {
    const inflight = deferred<{ contacts: Contact[]; query: string; count: number }>();
    apiMock.mockImplementation((url: string) => {
      if (url.includes('/contacts/search')) return inflight.promise;
      return Promise.resolve({ events: [] });
    });

    const store = useSearchStore();
    const search = store.search('alice');
    store.reset();

    inflight.resolve({ contacts: [contact('c1', 'Alice')], query: 'alice', count: 1 });
    await search;

    expect(store.query).toBe('');
    expect(store.results.contacts).toEqual([]);
    expect(store.isLoading).toBe(false);
  });

  it('short-query clear also invalidates an in-flight fan-out', async () => {
    const inflight = deferred<{ contacts: Contact[]; query: string; count: number }>();
    apiMock.mockImplementation((url: string) => {
      if (url.includes('/contacts/search')) return inflight.promise;
      return Promise.resolve({ events: [] });
    });

    const store = useSearchStore();
    const search = store.search('alice');
    await store.search(''); // user cleared the input

    inflight.resolve({ contacts: [contact('c1', 'Alice')], query: 'alice', count: 1 });
    await search;

    expect(store.results.contacts).toEqual([]);
  });
});

describe('contact hits', () => {
  it('calls the contacts search endpoint with limit=200 and labels the address book', async () => {
    respond({ contacts: () => ({ contacts: [contact('c1', 'Alice Adams', 2)], query: 'ali', count: 1 }) });

    const store = useSearchStore();
    useContactsStore().addressBooks = [book(1), book(2, 'Work')];

    await store.search('ali');

    const url = apiMock.mock.calls.map((c) => c[0] as string).find((u) => u.includes('/contacts/search'));
    expect(url).toContain('q=ali');
    expect(url).toContain('limit=200');
    expect(store.results.contacts).toHaveLength(1);
    // addressbook_id is a NUMERIC STRING matching AddressBook.ID, never the UUID.
    expect(store.results.contacts[0]!.addressBookId).toBe('2');
    expect(store.results.contacts[0]!.addressBookName).toBe('Work');
  });

  it('does not touch the contacts page state (searchQuery / contacts list)', async () => {
    respond({ contacts: () => ({ contacts: [contact('c1', 'Alice')], query: 'ali', count: 1 }) });

    const contactsStore = useContactsStore();
    contactsStore.addressBooks = [book(1)];
    contactsStore.contacts = [contact('page-1', 'Someone Else')];

    await useSearchStore().search('ali');

    expect(contactsStore.searchQuery).toBe('');
    expect(contactsStore.contacts.map((c) => c.id)).toEqual(['page-1']);
  });
});

describe('event hits', () => {
  it('scans every calendar over the bounded window', async () => {
    respond({ events: () => ({ events: [] }) });

    const store = useSearchStore();
    useCalendarStore().calendars = [calendar(1), calendar(2)];

    await store.search('anything');

    const urls = apiMock.mock.calls.map((c) => c[0] as string).filter((u) => u.includes('/events'));
    expect(urls).toHaveLength(2);
    // One request per calendar, keyed on the calendar UUID (#52), not its numeric id.
    expect(urls[0]).toContain('/api/v1/calendars/cal-uuid-1/events');
    expect(urls[1]).toContain('/api/v1/calendars/cal-uuid-2/events');

    const { start, end } = eventSearchWindow(NOW);
    expect(urls[0]).toContain(`start=${start.toISOString()}`);
    expect(urls[0]).toContain(`end=${end.toISOString()}`);
  });

  it('eventSearchWindow spans SEARCH_WINDOW_MONTHS either side of now', () => {
    // Local-constructed date so the month arithmetic is timezone independent.
    const { start, end } = eventSearchWindow(new Date(2026, 5, 15, 12, 0, 0));
    expect(SEARCH_WINDOW_MONTHS).toBe(6);
    expect([start.getFullYear(), start.getMonth()]).toEqual([2025, 11]);
    expect([end.getFullYear(), end.getMonth()]).toEqual([2026, 11]);
  });

  it('matches summary, location and description but not unrelated events', async () => {
    respond({
      events: () => ({
        events: [
          event('e1', 1, 'Team Standup', '2026-06-16T09:00:00Z'),
          event('e2', 1, 'Lunch', '2026-06-17T12:00:00Z', { location: 'Standup Cafe' }),
          event('e3', 1, 'Retro', '2026-06-18T09:00:00Z', { description: 'replaces the standup' }),
          event('e4', 1, 'Unrelated', '2026-06-19T09:00:00Z'),
        ],
      }),
    });

    const store = useSearchStore();
    useCalendarStore().calendars = [calendar(1, 'Work', '#ff0000')];

    await store.search('standup');

    expect(store.results.events.map((h) => h.event.id)).toEqual(['e1', 'e2', 'e3']);
    expect(store.results.events[0]!.calendarName).toBe('Work');
    expect(store.results.events[0]!.calendarColor).toBe('#ff0000');
    expect(store.results.events[0]!.calendarId).toBe('1');
  });

  it('gives recurring occurrences distinct keys', async () => {
    respond({
      events: () => ({
        events: [
          event('series', 1, 'Standup', '2026-06-16T09:00:00Z', { recurrence_id: '2026-06-16T09:00:00Z' }),
          event('series', 1, 'Standup', '2026-06-17T09:00:00Z', { recurrence_id: '2026-06-17T09:00:00Z' }),
        ],
      }),
    });

    const store = useSearchStore();
    useCalendarStore().calendars = [calendar(1)];

    await store.search('standup');

    const keys = store.results.events.map((h) => h.key);
    expect(new Set(keys).size).toBe(2);
  });

  it('keeps results from the calendars that succeeded when one fails', async () => {
    apiMock.mockImplementation((url: string) => {
      if (url.includes('/contacts/search')) return Promise.resolve({ contacts: [], query: '', count: 0 });
      if (url.includes('cal-uuid-1')) return Promise.reject(new Error('boom'));
      return Promise.resolve({ events: [event('e2', 2, 'Standup', '2026-06-16T09:00:00Z')] });
    });

    const store = useSearchStore();
    useCalendarStore().calendars = [calendar(1), calendar(2)];

    await store.search('standup');

    expect(store.error).toBeNull();
    expect(store.results.events.map((h) => h.event.id)).toEqual(['e2']);
  });

  it('reports an error when every calendar AND the contact search fail', async () => {
    apiMock.mockImplementation(() => Promise.reject(new Error('server down')));

    const store = useSearchStore();
    useCalendarStore().calendars = [calendar(1)];

    await store.search('standup');

    expect(store.error).toBe('server down');
    expect(store.results.events).toEqual([]);
  });
});

describe('sortEventHits', () => {
  it('puts upcoming events first (soonest first), then past ones (most recent first)', () => {
    const hit = (id: string, start: string): EventHit => ({
      key: id,
      event: event(id, 1, id, start),
      calendarId: '1',
      calendarName: 'Work',
      calendarColor: '#000',
    });

    const sorted = sortEventHits(
      [
        hit('past-old', '2026-01-01T09:00:00Z'),
        hit('future-far', '2026-09-01T09:00:00Z'),
        hit('past-recent', '2026-06-01T09:00:00Z'),
        hit('future-near', '2026-06-16T09:00:00Z'),
      ],
      NOW.getTime()
    );

    expect(sorted.map((h) => h.event.id)).toEqual(['future-near', 'future-far', 'past-recent', 'past-old']);
  });
});

describe('calendars and address books', () => {
  it('matches calendar and address book names/descriptions client-side', async () => {
    respond({});

    const store = useSearchStore();
    useCalendarStore().calendars = [calendar(1, 'Family'), calendar(2, 'Work')];
    useContactsStore().addressBooks = [book(1, 'Family & Friends'), book(2, 'Vendors', 'family businesses')];

    await store.search('family');

    expect(store.results.calendars.map((c) => c.name)).toEqual(['Family']);
    expect(store.results.addressBooks.map((ab) => ab.Name)).toEqual(['Family & Friends', 'Vendors']);
    expect(store.hasResults).toBe(true);
    expect(store.totalCount).toBe(3);
  });

  it('loads calendars and address books when they are not in state yet', async () => {
    apiMock.mockImplementation((url: string) => {
      if (url === '/api/v1/calendars') return Promise.resolve({ calendars: [calendar(1, 'Family')] });
      if (url === '/api/v1/addressbooks') return Promise.resolve({ addressbooks: [book(1, 'Family')] });
      if (url.includes('/contacts/search')) return Promise.resolve({ contacts: [], query: '', count: 0 });
      return Promise.resolve({ events: [] });
    });

    const store = useSearchStore();
    await store.search('family');

    expect(store.results.calendars).toHaveLength(1);
    expect(store.results.addressBooks).toHaveLength(1);
  });
});

describe('session cache', () => {
  it('serves a repeated query from cache without new requests', async () => {
    respond({ contacts: () => ({ contacts: [contact('c1', 'Alice')], query: 'ali', count: 1 }) });

    const store = useSearchStore();
    useCalendarStore().calendars = [calendar(1)];
    useContactsStore().addressBooks = [book(1)];

    await store.search('ali');
    const callsAfterFirst = apiMock.mock.calls.length;

    await store.search('ALI'); // case-insensitive cache key
    expect(apiMock.mock.calls.length).toBe(callsAfterFirst);
    expect(store.results.contacts).toHaveLength(1);
  });

  it('re-queries once the entry has expired', async () => {
    respond({ contacts: () => ({ contacts: [contact('c1', 'Alice')], query: 'ali', count: 1 }) });

    const store = useSearchStore();
    useContactsStore().addressBooks = [book(1)];

    await store.search('ali');
    const callsAfterFirst = apiMock.mock.calls.length;

    vi.setSystemTime(new Date(NOW.getTime() + 120_000));
    await store.search('ali');

    expect(apiMock.mock.calls.length).toBeGreaterThan(callsAfterFirst);
  });

  it('does not cache a partially failed result set', async () => {
    apiMock.mockImplementation((url: string) => {
      if (url.includes('/contacts/search')) return Promise.reject(new Error('nope'));
      return Promise.resolve({ events: [] });
    });

    const store = useSearchStore();
    useCalendarStore().calendars = [calendar(1)];

    await store.search('ali');
    const callsAfterFirst = apiMock.mock.calls.length;

    await store.search('ali');
    expect(apiMock.mock.calls.length).toBeGreaterThan(callsAfterFirst);
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
