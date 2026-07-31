// @vitest-environment nuxt
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import { createTestingPinia } from '@pinia/testing';
import { useDashboardStore } from './dashboard';
import { useCalendarStore } from './calendars';
import { useContactsStore } from './contacts';
import type { Calendar, CalendarEvent } from '~/types/calendar';
import type { AddressBook, Contact } from '~/types/contacts';
import { monthKey } from '~/utils/dashboardDates';

const { apiMock } = vi.hoisted(() => ({ apiMock: vi.fn() }));
mockNuxtImport('useApi', () => () => apiMock);

// A fixed "now": Thursday 30 July 2026, 10:00 LOCAL time. Everything is built
// from local components so the specs hold in any timezone.
const NOW = new Date(2026, 6, 30, 10, 0);

function calendar(id: number, name = `Cal ${id}`, extra: Partial<Calendar> = {}): Calendar {
  return {
    id: String(id),
    uuid: `cal-uuid-${id}`,
    path: `/cal/${id}`,
    name,
    color: '#123456',
    owner_id: '1',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...extra,
  };
}

function book(id: number, name = `Book ${id}`): AddressBook {
  return {
    ID: id,
    UUID: `ab-uuid-${id}`,
    UserID: 1,
    Path: `/ab/${id}`,
    Name: name,
    Description: '',
    CreatedAt: '2026-01-01T00:00:00Z',
    UpdatedAt: '2026-01-01T00:00:00Z',
  };
}

function event(id: string, start: Date, end: Date, extra: Partial<CalendarEvent> = {}): CalendarEvent {
  return {
    id,
    calendar_id: 1,
    uid: `uid-${id}`,
    summary: id,
    start: start.toISOString(),
    end: end.toISOString(),
    all_day: false,
    is_recurring: false,
    ...extra,
  };
}

function contact(id: string, updatedAt: Date, abId = 1): Contact {
  return {
    id,
    addressbook_id: String(abId),
    uid: `uid-${id}`,
    formatted_name: `Person ${id}`,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: updatedAt.toISOString(),
  };
}

/** URLs the api mock saw, filtered to the events endpoint. */
const eventUrls = () =>
  apiMock.mock.calls.map((c) => c[0] as string).filter((u) => u.includes('/events?'));

beforeEach(() => {
  createTestingPinia({ stubActions: false });
  apiMock.mockReset();
});

describe('refresh()', () => {
  it('hydrates calendars + address books, then one event range per calendar and one contact preview per book', async () => {
    const calendarStore = useCalendarStore();
    const contactsStore = useContactsStore();

    apiMock.mockImplementation((url: string) => {
      if (url === '/api/v1/calendars') return { calendars: [calendar(1), calendar(2)] };
      if (url === '/api/v1/addressbooks') return { addressbooks: [book(1)] };
      if (url.includes('/events?')) {
        return { events: [event(`e-${url.slice(-4)}`, new Date(2026, 6, 30, 12, 0), new Date(2026, 6, 30, 13, 0))] };
      }
      return { Contacts: [contact('c1', new Date(2026, 6, 29))], Total: 42, Limit: 6, Offset: 0 };
    });

    const store = useDashboardStore();
    store.now = NOW.getTime();
    await store.refresh();

    expect(calendarStore.calendars).toHaveLength(2);
    expect(contactsStore.addressBooks).toHaveLength(1);

    // Two calendars → two event requests. The current and next month are
    // contiguous, so they are fetched as ONE range each, not one per month.
    const urls = eventUrls();
    expect(urls).toHaveLength(2);
    expect(urls[0]).toContain(new Date(2026, 6, 1).toISOString());
    expect(urls[0]).toContain(new Date(2026, 8, 1).toISOString()); // exclusive end
    expect(store.loadedMonths).toEqual(['2026-07', '2026-08']);

    // The stats total comes from the endpoint's `Total`, not the preview length.
    expect(store.totalContacts).toBe(42);
    expect(store.recentContacts).toHaveLength(1);
    expect(store.isLoadingEvents).toBe(false);
    expect(store.isLoadingContacts).toBe(false);
    expect(store.eventsIncomplete).toBe(false);
  });

  // The store outlives client-side route changes, so re-entering the dashboard
  // has to re-read the events rather than trust the first visit's snapshot.
  it('re-reads events on every visit, even when the months are already loaded', async () => {
    apiMock.mockImplementation((url: string) => {
      if (url === '/api/v1/calendars') return { calendars: [calendar(1)] };
      if (url === '/api/v1/addressbooks') return { addressbooks: [] };
      return { events: [event('created-elsewhere', new Date(2026, 6, 30, 15, 0), new Date(2026, 6, 30, 16, 0))] };
    });

    const store = useDashboardStore();
    store.now = NOW.getTime();
    // State as a previous visit left it.
    store.loadedMonths = ['2026-07', '2026-08'];
    store.events = [event('deleted-elsewhere', new Date(2026, 6, 30, 9, 0), new Date(2026, 6, 30, 10, 0))];

    await store.refresh();

    expect(eventUrls()).toHaveLength(1);
    expect(store.events.map((e) => e.id)).toEqual(['created-elsewhere']);
  });
});

describe('ensureMonths()', () => {
  it('fetches every calendar in parallel rather than one at a time', () => {
    const calendarStore = useCalendarStore();
    calendarStore.calendars = [calendar(1), calendar(2), calendar(3)];

    // Never-resolving promises: a serial loop would have exactly one request in
    // flight here.
    const resolvers: Array<(v: { events: CalendarEvent[] }) => void> = [];
    apiMock.mockImplementation(() => new Promise((res) => { resolvers.push(res); }));

    const store = useDashboardStore();
    store.now = NOW.getTime();
    const pending = store.ensureMonths(NOW, 1);

    expect(apiMock).toHaveBeenCalledTimes(3);
    expect(store.isLoadingEvents).toBe(true);

    resolvers.forEach((res) => res({ events: [] }));
    return pending.then(() => {
      expect(store.isLoadingEvents).toBe(false);
    });
  });

  it('skips months already loaded and fetches only the missing one', async () => {
    const calendarStore = useCalendarStore();
    calendarStore.calendars = [calendar(1)];
    apiMock.mockResolvedValue({ events: [] });

    const store = useDashboardStore();
    store.now = NOW.getTime();

    await store.ensureMonths(NOW, 2); // July + August
    expect(eventUrls()).toHaveLength(1);

    await store.ensureMonths(NOW, 2); // both cached → no request
    expect(eventUrls()).toHaveLength(1);

    await store.ensureMonths(new Date(2026, 8, 1), 1); // September is new
    expect(eventUrls()).toHaveLength(2);
    expect(store.loadedMonths).toEqual(['2026-07', '2026-08', '2026-09']);
  });

  it('does not mark months loaded when there is no calendar to ask yet', async () => {
    const store = useDashboardStore();
    store.now = NOW.getTime();

    await store.ensureMonths(NOW, 2);

    // Calendars had not arrived; leaving the months unmarked lets the next call
    // (after fetchCalendars) actually fetch them.
    expect(apiMock).not.toHaveBeenCalled();
    expect(store.loadedMonths).toEqual([]);
  });

  it('keeps the healthy calendars when one fails, and flags the result as incomplete', async () => {
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const calendarStore = useCalendarStore();
    calendarStore.calendars = [calendar(1, 'Broken'), calendar(2, 'Good')];

    apiMock
      .mockRejectedValueOnce(new Error('403'))
      .mockResolvedValueOnce({ events: [event('ok', new Date(2026, 6, 30, 9, 0), new Date(2026, 6, 30, 9, 30))] });

    const store = useDashboardStore();
    store.now = NOW.getTime();
    await store.ensureMonths(NOW, 1);

    expect(store.events.map((e) => e.id)).toEqual(['ok']);
    expect(store.eventsIncomplete).toBe(true);
    expect(warnSpy.mock.calls[0]![0]).toContain('Broken');
    warnSpy.mockRestore();
  });

  it('gives the month claim back when no calendar answers, so the month is retried', async () => {
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const calendarStore = useCalendarStore();
    calendarStore.calendars = [calendar(1)];
    apiMock.mockRejectedValueOnce(new Error('offline'));

    const store = useDashboardStore();
    store.now = NOW.getTime();
    await store.ensureMonths(NOW, 1);

    // Marked-before-await stops duplicate fetches, but a month that never
    // arrived must not stay marked: it would look empty forever.
    expect(store.loadedMonths).toEqual([]);
    expect(store.eventsIncomplete).toBe(true);

    apiMock.mockResolvedValue({ events: [event('retried', new Date(2026, 6, 30, 9, 0), new Date(2026, 6, 30, 10, 0))] });
    await store.ensureMonths(NOW, 1);

    expect(store.loadedMonths).toEqual(['2026-07']);
    expect(store.events.map((e) => e.id)).toEqual(['retried']);
    warnSpy.mockRestore();
  });
});

describe('mergeEvents()', () => {
  it('dedupes on re-fetch but keeps distinct occurrences of one recurring series', () => {
    const store = useDashboardStore();
    const monday = event('series', new Date(2026, 6, 27, 9, 0), new Date(2026, 6, 27, 10, 0), {
      is_recurring: true,
      recurrence_id: '2026-07-27',
    });
    const tuesday = event('series', new Date(2026, 6, 28, 9, 0), new Date(2026, 6, 28, 10, 0), {
      is_recurring: true,
      recurrence_id: '2026-07-28',
    });

    store.mergeEvents([monday, tuesday]);
    store.mergeEvents([monday, tuesday]); // overlapping range refetch

    // Both occurrences survive (they share the series id), neither is doubled.
    expect(store.events).toHaveLength(2);
    expect(store.events.map((e) => e.recurrence_id)).toEqual(['2026-07-27', '2026-07-28']);
  });
});

describe('getters', () => {
  function seeded() {
    const store = useDashboardStore();
    store.now = NOW.getTime();
    store.events = [
      // Yesterday — finished.
      event('past', new Date(2026, 6, 29, 9, 0), new Date(2026, 6, 29, 10, 0)),
      // Today, already over.
      event('earlier-today', new Date(2026, 6, 30, 8, 0), new Date(2026, 6, 30, 9, 0)),
      // Today, in progress at 10:00.
      event('running', new Date(2026, 6, 30, 9, 30), new Date(2026, 6, 30, 11, 0)),
      // Today, later.
      event('later-today', new Date(2026, 6, 30, 15, 0), new Date(2026, 6, 30, 16, 0)),
      // Today, all-day: stored as [midnight, next midnight).
      event('all-day', new Date(2026, 6, 30), new Date(2026, 6, 31), { all_day: true }),
      // Tomorrow.
      event('tomorrow', new Date(2026, 6, 31, 9, 0), new Date(2026, 6, 31, 10, 0)),
      // Next month.
      event('next-month', new Date(2026, 7, 3, 9, 0), new Date(2026, 7, 3, 10, 0)),
    ];
    return store;
  }

  it('todayEvents picks everything overlapping the local day, sorted, and splits all-day out', () => {
    const store = seeded();

    expect(store.todayEvents.map((e) => e.id)).toEqual([
      'all-day', 'earlier-today', 'running', 'later-today',
    ]);
    expect(store.todayAllDayEvents.map((e) => e.id)).toEqual(['all-day']);
    expect(store.todayTimedEvents.map((e) => e.id)).toEqual(['earlier-today', 'running', 'later-today']);
  });

  it('upcomingEvents keeps in-progress and all-day events but drops finished ones', () => {
    const store = seeded();

    // 'earlier-today' and 'past' have ended; 'running' has not, so it leads.
    expect(store.upcomingEvents.map((e) => e.id)).toEqual([
      'all-day', 'running', 'later-today', 'tomorrow', 'next-month',
    ]);
  });

  it('upcomingEvents is capped at the widget limit', () => {
    const store = useDashboardStore();
    store.now = NOW.getTime();
    store.events = Array.from({ length: 12 }, (_, i) =>
      event(`e${i}`, new Date(2026, 6, 31, 9 + i, 0), new Date(2026, 6, 31, 10 + i, 0))
    );

    expect(store.upcomingEvents).toHaveLength(7);
    expect(store.upcomingEvents[0]!.id).toBe('e0');
  });

  it('eventDayKeys marks every day an event touches, without spilling past an all-day end', () => {
    const store = seeded();
    store.events.push(
      event('trip', new Date(2026, 6, 30, 22, 0), new Date(2026, 7, 1, 2, 0))
    );

    const keys = store.eventDayKeys;
    expect(keys.has('2026-07-29')).toBe(true);
    expect(keys.has('2026-07-30')).toBe(true);
    expect(keys.has('2026-07-31')).toBe(true);
    expect(keys.has('2026-08-01')).toBe(true); // from the overnight trip
    expect(keys.has('2026-08-02')).toBe(false);
  });

  it('counts events per month, for the current month and any other', () => {
    const store = seeded();

    // July: past, earlier-today, running, later-today, all-day, tomorrow.
    expect(store.monthEventCount).toBe(6);
    expect(store.eventCountInMonth(new Date(2026, 7, 15))).toBe(1);
    expect(store.eventCountInMonth(new Date(2026, 5, 15))).toBe(0);
  });
});

describe('fetchRecentContacts()', () => {
  it('asks each book for its newest few, re-sorts the merged result and sums the totals', async () => {
    const contactsStore = useContactsStore();
    contactsStore.addressBooks = [book(1), book(2)];

    apiMock.mockImplementation((url: string) => {
      if (url.includes('ab-uuid-1')) {
        return {
          Contacts: [contact('old-a', new Date(2026, 0, 1), 1), contact('old-b', new Date(2025, 11, 1), 1)],
          Total: 10,
          Limit: 6,
          Offset: 0,
        };
      }
      return {
        Contacts: [contact('newest', new Date(2026, 6, 29), 2)],
        Total: 5,
        Limit: 6,
        Offset: 0,
      };
    });

    const store = useDashboardStore();
    await store.fetchRecentContacts();

    // Server-side sorting is per book, so the merge must re-sort: book 2's single
    // contact is newer than everything in book 1 and has to come first.
    expect(store.recentContacts.map((c) => c.id)).toEqual(['newest', 'old-a', 'old-b']);
    expect(store.totalContacts).toBe(15);

    const urls = apiMock.mock.calls.map((c) => c[0] as string);
    expect(urls.every((u) => u.includes('sort=updated_at') && u.includes('order=desc'))).toBe(true);
    expect(urls.every((u) => u.includes('limit=6'))).toBe(true);
  });

  it('caps the merged list at the widget limit', async () => {
    const contactsStore = useContactsStore();
    contactsStore.addressBooks = [book(1), book(2)];
    apiMock.mockImplementation(() => ({
      Contacts: Array.from({ length: 6 }, (_, i) => contact(`c${i}`, new Date(2026, 6, 20 + i))),
      Total: 6,
      Limit: 6,
      Offset: 0,
    }));

    const store = useDashboardStore();
    await store.fetchRecentContacts();

    expect(store.recentContacts).toHaveLength(6);
    expect(store.totalContacts).toBe(12);
  });

  it('flags an incomplete result when a book fails but keeps the others', async () => {
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const contactsStore = useContactsStore();
    contactsStore.addressBooks = [book(1, 'Broken'), book(2, 'Good')];

    apiMock
      .mockRejectedValueOnce(new Error('500'))
      .mockResolvedValueOnce({ Contacts: [contact('ok', new Date(2026, 6, 1), 2)], Total: 3, Limit: 6, Offset: 0 });

    const store = useDashboardStore();
    await store.fetchRecentContacts();

    expect(store.recentContacts.map((c) => c.id)).toEqual(['ok']);
    expect(store.totalContacts).toBe(3);
    expect(store.contactsIncomplete).toBe(true);
    expect(warnSpy.mock.calls[0]![0]).toContain('Broken');
    warnSpy.mockRestore();
  });

  it('clears the preview and total when the user has no address books', async () => {
    const store = useDashboardStore();
    store.recentContacts = [contact('stale', new Date(2026, 1, 1))];
    store.totalContacts = 7;

    await store.fetchRecentContacts();

    expect(store.recentContacts).toEqual([]);
    expect(store.totalContacts).toBe(0);
    expect(apiMock).not.toHaveBeenCalled();
  });
});

describe('reloadEvents()', () => {
  it('replaces the events of the refetched window so deletions made elsewhere disappear', async () => {
    const calendarStore = useCalendarStore();
    calendarStore.calendars = [calendar(1)];

    const store = useDashboardStore();
    store.now = NOW.getTime();
    store.events = [
      event('deleted-since', new Date(2026, 6, 30, 9, 0), new Date(2026, 6, 30, 10, 0)),
    ];
    store.loadedMonths = ['2026-07', '2026-08'];

    apiMock.mockResolvedValue({
      events: [event('still-there', new Date(2026, 6, 31, 9, 0), new Date(2026, 6, 31, 10, 0))],
    });

    await store.reloadEvents();

    expect(store.events.map((e) => e.id)).toEqual(['still-there']);
    // July and August are contiguous → ONE request, ending exclusively on 1 Sep.
    const urls = eventUrls();
    expect(urls).toHaveLength(1);
    expect(urls[0]).toContain(new Date(2026, 6, 1).toISOString());
    expect(urls[0]).toContain(new Date(2026, 8, 1).toISOString());
    expect(store.loadedMonths).toEqual(['2026-07', '2026-08']);
  });

  it('fetches the initial window when nothing has been loaded yet', async () => {
    const calendarStore = useCalendarStore();
    calendarStore.calendars = [calendar(1)];
    apiMock.mockResolvedValue({ events: [] });

    const store = useDashboardStore();
    store.now = NOW.getTime();
    await store.reloadEvents();

    expect(store.loadedMonths).toEqual(['2026-07', '2026-08']);
  });

  it('keeps what is on screen when every request fails instead of blanking the widgets', async () => {
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const calendarStore = useCalendarStore();
    calendarStore.calendars = [calendar(1), calendar(2)];

    const store = useDashboardStore();
    store.now = NOW.getTime();
    store.loadedMonths = ['2026-07', '2026-08'];
    store.events = [event('real', new Date(2026, 6, 30, 9, 0), new Date(2026, 6, 30, 10, 0))];

    apiMock.mockRejectedValue(new Error('502'));
    await store.reloadEvents();

    // An outage must not read as "Nothing scheduled today".
    expect(store.events.map((e) => e.id)).toEqual(['real']);
    expect(store.loadedMonths).toEqual(['2026-07', '2026-08']);
    expect(store.eventsIncomplete).toBe(true);
    warnSpy.mockRestore();
  });

  it('keeps a month that was loaded while the reload was in flight', async () => {
    const calendarStore = useCalendarStore();
    calendarStore.calendars = [calendar(1)];

    const store = useDashboardStore();
    store.now = NOW.getTime();
    store.loadedMonths = ['2026-07', '2026-08'];

    const october = event('october', new Date(2026, 9, 5, 9, 0), new Date(2026, 9, 5, 10, 0));
    const octoberStart = new Date(2026, 9, 1).toISOString();
    const held: Array<(value: { events: CalendarEvent[] }) => void> = [];
    apiMock.mockImplementation((url: string) => {
      // The mini calendar's own fetch answers at once; the reload hangs until we
      // release it, which is the race this test exists for.
      if (url.includes(octoberStart)) return { events: [october] };
      return new Promise((res) => { held.push(res); });
    });

    const pending = store.reloadEvents();
    // User pages the mini calendar to October while the reload is awaiting.
    await store.setMiniMonth(new Date(2026, 9, 1));
    expect(store.events.map((e) => e.id)).toEqual(['october']);

    held[0]!({ events: [event('july', new Date(2026, 6, 30, 9, 0), new Date(2026, 6, 30, 10, 0))] });
    await pending;

    // October is outside the reload's window: its events and its loaded-month
    // entry must both survive, or the month on screen would empty out with
    // nothing left to trigger a refetch.
    expect(store.events.map((e) => e.id).sort()).toEqual(['july', 'october']);
    expect(store.loadedMonths).toEqual(['2026-07', '2026-08', '2026-10']);
  });

  it('splits far-apart months into separate ranges and prunes them once they leave the screen', async () => {
    const calendarStore = useCalendarStore();
    calendarStore.calendars = [calendar(1)];
    apiMock.mockResolvedValue({ events: [] });

    const store = useDashboardStore();
    store.now = NOW.getTime();
    store.loadedMonths = ['2020-01', '2026-07', '2026-08'];
    store.miniMonthKey = '2020-01'; // the user paged years back
    store.events = [
      event('ancient', new Date(2020, 0, 15, 9, 0), new Date(2020, 0, 15, 10, 0)),
    ];

    await store.reloadEvents();

    // Two ranges, NOT one 79-month span from 2020 to 2026.
    const urls = eventUrls();
    expect(urls).toHaveLength(2);
    expect(urls.some((u) => u.includes(new Date(2020, 0, 1).toISOString()) && u.includes(new Date(2020, 1, 1).toISOString()))).toBe(true);
    expect(urls.some((u) => u.includes(new Date(2026, 6, 1).toISOString()) && u.includes(new Date(2026, 8, 1).toISOString()))).toBe(true);
    expect(store.loadedMonths).toEqual(['2020-01', '2026-07', '2026-08']);

    // Back to the current month: 2020 is no longer worth keeping, so it is
    // dropped from both lists and would simply be refetched if visited again.
    apiMock.mockClear();
    store.miniMonthKey = '2026-07';
    store.events = [event('ancient', new Date(2020, 0, 15, 9, 0), new Date(2020, 0, 15, 10, 0))];
    await store.reloadEvents();

    expect(eventUrls()).toHaveLength(1);
    expect(store.loadedMonths).toEqual(['2026-07', '2026-08']);
    expect(store.events).toEqual([]);
  });
});

describe('setMiniMonth()', () => {
  it('follows the clock until the user navigates, then sticks', async () => {
    const store = useDashboardStore();
    store.now = NOW.getTime();

    expect(monthKey(store.miniMonth)).toBe('2026-07');

    // No calendars yet: the month still moves, there is just nothing to fetch.
    await store.setMiniMonth(new Date(2026, 10, 1));
    expect(monthKey(store.miniMonth)).toBe('2026-11');

    store.now = new Date(2026, 7, 1, 10, 0).getTime();
    expect(monthKey(store.miniMonth)).toBe('2026-11');
  });
});
