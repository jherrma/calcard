// @vitest-environment nuxt
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import { createTestingPinia } from '@pinia/testing';
import { useCalendarStore, toRFC3339 } from './calendars';
import type { Calendar, CalendarEvent } from '~/types/calendar';

// Single mock fetch shared by all specs. useApi() returns this function, matching
// the store's `const api = useApi(); await api(url, opts)` usage.
const { apiMock } = vi.hoisted(() => ({ apiMock: vi.fn() }));
mockNuxtImport('useApi', () => () => apiMock);
// The reminder refresh hook (#169, story 16) is a lazy import of the alarms store: stand in for it.
const { refreshMock } = vi.hoisted(() => ({ refreshMock: vi.fn() }));
mockNuxtImport('refreshAlarmsSoon', () => refreshMock);

// Minimal Calendar factory — id is a NUMBER at runtime (typed string), which is
// exactly the number-vs-string trap these specs guard.
function cal(partial: Omit<Partial<Calendar>, 'id'> & { id: number }): Calendar {
  return {
    uuid: `uuid-${partial.id}`,
    path: `/cal/${partial.id}`,
    name: `Cal ${partial.id}`,
    color: '#3b82f6',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...partial,
  } as unknown as Calendar;
}

beforeEach(() => {
  // createTestingPinia() sets the active pinia; stubActions:false keeps real
  // action logic (we assert action behavior, not that actions were called).
  createTestingPinia({ stubActions: false });
  apiMock.mockReset();
  refreshMock.mockReset();
});

describe('toRFC3339', () => {
  it('preserves the local UTC offset instead of converting to Z', () => {
    // A local wall-clock time. Components below are read back via local getters,
    // so the assertion is deterministic regardless of the runner timezone.
    const d = new Date(2026, 1, 9, 11, 0, 0); // Feb 9 2026, 11:00 local
    const out = toRFC3339(d);

    // Shape: full offset suffix, never the UTC 'Z' designator.
    expect(out).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}[+-]\d{2}:\d{2}$/);
    expect(out.endsWith('Z')).toBe(false);

    // Uses LOCAL time components (not UTC) — this is the offset-preservation fix.
    const pad = (n: number) => n.toString().padStart(2, '0');
    const expectedLocal =
      `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}` +
      `T${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
    expect(out.slice(0, 19)).toBe(expectedLocal);

    // Offset digits match the machine's actual offset for that instant.
    const offset = -d.getTimezoneOffset();
    const sign = offset >= 0 ? '+' : '-';
    const abs = Math.abs(offset);
    const expectedOffset = `${sign}${pad(Math.floor(abs / 60))}:${pad(abs % 60)}`;
    expect(out.slice(19)).toBe(expectedOffset);
  });
});

describe('stringification invariants (guards the number-vs-string Set bug #11/#13)', () => {
  it('fetchCalendars populates visibleCalendarIds with String(id) from numeric ids', async () => {
    apiMock.mockResolvedValueOnce({ calendars: [cal({ id: 1 }), cal({ id: 2 })] });
    const store = useCalendarStore();

    await store.fetchCalendars();

    // Numeric ids are stringified into the Set.
    expect(store.visibleCalendarIds.has('1')).toBe(true);
    expect(store.visibleCalendarIds.has('2')).toBe(true);
    // Every member is a string (not a raw number).
    for (const id of store.visibleCalendarIds) {
      expect(typeof id).toBe('string');
    }
  });

  it('visibleEvents getter matches numeric event.calendar_id against the String Set', () => {
    const store = useCalendarStore();
    store.events = [
      { id: 'ev1', calendar_id: 1, summary: 'A' } as unknown as CalendarEvent,
      { id: 'ev2', calendar_id: 2, summary: 'B' } as unknown as CalendarEvent,
    ];
    // Set holds a STRING id; the numeric calendar_id must still resolve.
    store.visibleCalendarIds = new Set(['1']);

    const visible = store.visibleEvents;
    expect(visible.map((e) => e.id)).toEqual(['ev1']);
  });

  it('calendarOptions getter compares String(cal.id) against the String Set', () => {
    const store = useCalendarStore();
    store.calendars = [cal({ id: 1 }), cal({ id: 2 })];
    store.visibleCalendarIds = new Set(['1']);

    const opts = store.calendarOptions;
    // Calendar.id is typed string but numeric at runtime — compare via String().
    expect(opts.find((o) => String(o.id) === '1')?.visible).toBe(true);
    expect(opts.find((o) => String(o.id) === '2')?.visible).toBe(false);
  });

  it('createCalendar adds String(response.id) to visibleCalendarIds', async () => {
    apiMock.mockResolvedValueOnce(cal({ id: 42 }));
    const store = useCalendarStore();

    await store.createCalendar({ name: 'New', color: '#fff', timezone: 'UTC', description: '' });

    expect(store.visibleCalendarIds.has('42')).toBe(true);
    expect(store.calendars.some((c) => String(c.id) === '42')).toBe(true);
  });
});

describe('moveEvent', () => {
  it('maps numeric calendar ids to UUIDs in the path and target body (#52)', async () => {
    apiMock.mockResolvedValueOnce(undefined);
    const store = useCalendarStore();
    // Callers still pass numeric ids; the store resolves them to UUIDs via the
    // loaded calendar list, matching the API's UUID-only /calendars/:id contract.
    store.calendars = [cal({ id: 1 }), cal({ id: 2 })]; // uuids uuid-1 / uuid-2

    await store.moveEvent('1', 'ev-9', '2');

    expect(apiMock).toHaveBeenCalledTimes(1);
    const [url, opts] = apiMock.mock.calls[0]!;
    expect(url).toBe('/api/v1/calendars/uuid-1/events/ev-9/move');
    expect(opts).toMatchObject({
      method: 'POST',
      body: { target_calendar_id: 'uuid-2' },
    });
  });
});

describe('writableCalendars', () => {
  it('includes owned + read-write shares, excludes read-only shares (permission field)', () => {
    const store = useCalendarStore();
    store.calendars = [
      cal({ id: 1, shared: false }), // owned
      cal({ id: 2, shared: true, permission: 'read-only' }), // shared RO
      cal({ id: 3, shared: true, permission: 'read-write' }), // shared RW
    ];

    const writable = store.writableCalendars.map((c) => String(c.id));
    expect(writable).toContain('1');
    expect(writable).toContain('3');
    expect(writable).not.toContain('2');
  });

  it('excludes a subscribed calendar even though the user owns it', () => {
    const store = useCalendarStore();
    store.calendars = [
      cal({ id: 1, shared: false }),
      cal({ id: 2, shared: false, subscribed: true }),
    ];

    // REVERT PROOF: this getter is what the event form and the dashboard's
    // quick-add offer as targets. A subscribed calendar mirrors a remote feed,
    // so the server refuses the write — and had it not, the next refresh would
    // replace the collection and silently discard the event.
    const writable = store.writableCalendars.map((c) => String(c.id));
    expect(writable).toEqual(['1']);

    expect(store.ownedCalendars.map((c) => String(c.id))).toEqual(['1']);
    expect(store.subscribedCalendars.map((c) => String(c.id))).toEqual(['2']);
  });
});

describe('fetchEvents parallel loading (#22)', () => {
  it('dispatches all calendar requests concurrently, not one at a time', async () => {
    const store = useCalendarStore();
    store.calendars = [cal({ id: 1 }), cal({ id: 2 }), cal({ id: 3 })];

    // Never-resolving promises: a serial `for … await` would issue the first
    // request and stall on it, so only ONE call would be in flight at the assert
    // below. Promise.allSettled fires all three up front. (Revert to serial and
    // this drops to 1 — the test proves the parallelism.)
    const resolvers: Array<(v: { events: CalendarEvent[] }) => void> = [];
    apiMock.mockImplementation(() => new Promise((res) => { resolvers.push(res); }));

    const p = store.fetchEvents(new Date('2026-02-01T00:00:00Z'), new Date('2026-03-01T00:00:00Z'));
    expect(apiMock).toHaveBeenCalledTimes(3);

    resolvers.forEach((res, i) =>
      res({ events: [{ id: `e${i + 1}`, calendar_id: i + 1 } as unknown as CalendarEvent] }),
    );
    await p;
    expect(store.events).toHaveLength(3);
  });

  it('continues past a failing calendar (allSettled) and warns, without a store error', async () => {
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const store = useCalendarStore();
    store.calendars = [cal({ id: 1 }), cal({ id: 2 }), cal({ id: 3 })];

    // Dispatched in calendar order, so the mock queue lines up 1 → 2 → 3.
    apiMock
      .mockResolvedValueOnce({ events: [{ id: 'e1', calendar_id: 1 } as unknown as CalendarEvent] })
      .mockRejectedValueOnce(new Error('boom'))
      .mockResolvedValueOnce({ events: [{ id: 'e3', calendar_id: 3 } as unknown as CalendarEvent] });

    await store.fetchEvents(new Date('2026-02-01T00:00:00Z'), new Date('2026-03-01T00:00:00Z'));

    // None dropped by the failure; only the healthy calendars' events survive.
    expect(apiMock).toHaveBeenCalledTimes(3);
    expect(store.events.map((e) => e.id).sort()).toEqual(['e1', 'e3']);
    expect(warnSpy).toHaveBeenCalledTimes(1);
    // A per-calendar failure is swallowed, so the store-level error stays clear.
    expect(store.error).toBeNull();
    warnSpy.mockRestore();
  });
});

// SCOPE: the sidebar filter must survive a refetch (story 043 review). The share
// dialog calls fetchCalendars() after EVERY share mutation, so the old
// "select all" reset re-checked calendars the user had hidden and flooded their
// events back into the view mid-dialog.
describe('fetchCalendars visibility preservation', () => {
  it('shows everything on the FIRST load', async () => {
    apiMock.mockResolvedValueOnce({ calendars: [cal({ id: 1 }), cal({ id: 2 })] });
    const store = useCalendarStore();

    await store.fetchCalendars();

    expect([...store.visibleCalendarIds].sort()).toEqual(['1', '2']);
  });

  it('keeps calendars the user hid when the list is refetched', async () => {
    apiMock.mockResolvedValueOnce({ calendars: [cal({ id: 1 }), cal({ id: 2 }), cal({ id: 3 })] });
    const store = useCalendarStore();
    await store.fetchCalendars();

    store.toggleCalendarVisibility('2'); // user unchecks "Cal 2"

    apiMock.mockResolvedValueOnce({ calendars: [cal({ id: 1 }), cal({ id: 2 }), cal({ id: 3 })] });
    await store.fetchCalendars();

    // REVERT PROOF: the old unconditional "select all" put '2' back.
    expect([...store.visibleCalendarIds].sort()).toEqual(['1', '3']);
  });

  it('defaults a calendar it has never seen (e.g. one just shared with you) to visible', async () => {
    apiMock.mockResolvedValueOnce({ calendars: [cal({ id: 1 })] });
    const store = useCalendarStore();
    await store.fetchCalendars();
    store.toggleCalendarVisibility('1');

    apiMock.mockResolvedValueOnce({ calendars: [cal({ id: 1 }), cal({ id: 7 })] });
    await store.fetchCalendars();

    expect(store.visibleCalendarIds.has('1')).toBe(false);
    expect(store.visibleCalendarIds.has('7')).toBe(true);
  });

  it('forgets calendars that disappeared from the list', async () => {
    apiMock.mockResolvedValueOnce({ calendars: [cal({ id: 1 }), cal({ id: 2 })] });
    const store = useCalendarStore();
    await store.fetchCalendars();

    apiMock.mockResolvedValueOnce({ calendars: [cal({ id: 1 })] });
    await store.fetchCalendars();

    expect([...store.visibleCalendarIds]).toEqual(['1']);
  });
});

// SCOPE (#170): a task list is a calendars row with `kind: 'tasks'`. GET
// /calendars returns it next to the calendars, and the store keeps it in
// `calendars` (the tasks section reuses that one request), so every reader that
// lists calendars for EVENTS must go through `eventCalendars`. Each test names
// the revert it catches.
describe('task lists stay out of the event side (#170)', () => {
  type Extra = Omit<Partial<Calendar>, 'id'>;
  const events = (id: number, extra: Extra = {}) => cal({ id, kind: 'events', ...extra });
  const tasks = (id: number, extra: Extra = {}) => cal({ id, kind: 'tasks', ...extra });
  const both = (id: number, extra: Extra = {}) => cal({ id, kind: 'both', ...extra });
  const ids = (list: Calendar[]) => list.map((c) => String(c.id));

  it('eventCalendars keeps events, mixed and kind-less calendars and drops task lists', () => {
    const store = useCalendarStore();
    store.calendars = [events(1), tasks(2), both(3), cal({ id: 4 }), tasks(5, { shared: true })];

    expect(ids(store.eventCalendars)).toEqual(['1', '3', '4']);
    // `calendars` is still the whole list: nothing is thrown away.
    expect(ids(store.calendars)).toEqual(['1', '2', '3', '4', '5']);
  });

  it('calendarOptions (the Calendar sidebar) never lists a task list', () => {
    const store = useCalendarStore();
    store.calendars = [
      events(1),
      tasks(2),
      both(3),
      tasks(4, { shared: true, permission: 'read-write' }),
      both(5, { subscribed: true }),
    ];
    store.visibleCalendarIds = new Set(['1', '2', '3', '4', '5']);

    // REVERT PROOF: built on `calendars` again, '2' and '4' come back.
    expect(store.calendarOptions.map((o) => String(o.id))).toEqual(['1', '3', '5']);
    expect(store.calendarOptions.every((o) => o.visible)).toBe(true);
  });

  it('writableCalendars (the event form and dashboard quick-add) never offers a task list', () => {
    const store = useCalendarStore();
    store.calendars = [
      tasks(1), // the user's own task list, listed FIRST
      events(2),
      tasks(3, { shared: true, permission: 'read-write' }),
      both(4, { shared: true, permission: 'read-write' }),
      events(5, { shared: true, permission: 'read-only' }),
    ];

    // REVERT PROOF: without the kind filter '1' would be first, and the event
    // form's default target, earning a 400 "This calendar does not store events".
    expect(ids(store.writableCalendars)).toEqual(['2', '4']);
  });

  it('a user with only task lists has no event calendar at all', () => {
    const store = useCalendarStore();
    store.calendars = [tasks(1), tasks(2, { shared: true, permission: 'read-write' })];

    expect(store.eventCalendars).toEqual([]);
    expect(store.calendarOptions).toEqual([]);
    expect(store.writableCalendars).toEqual([]);
    expect(store.ownedCalendars).toEqual([]);
    expect(store.sharedCalendars).toEqual([]);
  });

  it('the sidebar sections (owned, shared, subscribed) are built on eventCalendars', () => {
    const store = useCalendarStore();
    store.calendars = [
      events(1),
      tasks(2),
      tasks(3, { shared: true, permission: 'read-only' }),
      both(4, { shared: true, permission: 'read-only' }),
      both(5, { subscribed: true }),
    ];

    expect(ids(store.ownedCalendars)).toEqual(['1']);
    expect(ids(store.sharedCalendars)).toEqual(['4']);
    expect(ids(store.subscribedCalendars)).toEqual(['5']);
  });

  it('fetchEvents asks event and mixed calendars only, never a task list', async () => {
    const store = useCalendarStore();
    store.calendars = [tasks(1), events(2), both(3), tasks(4)];
    apiMock.mockImplementation((url: string) =>
      Promise.resolve({ events: [{ id: `ev-${url.split('/')[4]}`, calendar_id: 0 } as unknown as CalendarEvent] }),
    );

    await store.fetchEvents(new Date('2026-02-01T00:00:00Z'), new Date('2026-03-01T00:00:00Z'));

    // REVERT PROOF: mapping over `calendars` fires four requests, two of them
    // at task lists (one wasted request each against the per-IP limiter).
    const urls = apiMock.mock.calls.map((c) => c[0] as string);
    expect(urls).toHaveLength(2);
    expect(urls.every((u) => u.includes('/events?'))).toBe(true);
    expect(urls.map((u) => u.split('/')[4])).toEqual(['uuid-2', 'uuid-3']);
    expect(store.events.map((e) => e.id)).toEqual(['ev-uuid-2', 'ev-uuid-3']);
  });

  it('fetchEvents names the calendar that failed, not the task list at the same index', async () => {
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const store = useCalendarStore();
    store.calendars = [tasks(1), events(2), both(3)];
    apiMock
      .mockRejectedValueOnce(new Error('boom')) // calendar 2, the first request
      .mockResolvedValueOnce({ events: [] });

    await store.fetchEvents(new Date('2026-02-01T00:00:00Z'), new Date('2026-03-01T00:00:00Z'));

    // The warning indexes the filtered list it mapped over; indexing
    // `calendars` would blame task list 1.
    expect(warnSpy).toHaveBeenCalledTimes(1);
    expect(warnSpy.mock.calls[0]![0]).toBe('Failed to load events for calendar 2');
    warnSpy.mockRestore();
  });

  it('fetchEvents with only task lists sends nothing and leaves no events', async () => {
    const store = useCalendarStore();
    store.calendars = [tasks(1), tasks(2)];
    store.events = [{ id: 'stale', calendar_id: 9 } as unknown as CalendarEvent];

    await store.fetchEvents(new Date('2026-02-01T00:00:00Z'), new Date('2026-03-01T00:00:00Z'));

    expect(apiMock).not.toHaveBeenCalled();
    expect(store.events).toEqual([]);
    expect(store.error).toBeNull();
  });

  it('fetchCalendars is still exactly one request and keeps the task lists in `calendars`', async () => {
    apiMock.mockResolvedValueOnce({ calendars: [events(1), tasks(2), both(3)] });
    const store = useCalendarStore();

    await store.fetchCalendars();

    expect(apiMock).toHaveBeenCalledTimes(1);
    expect(apiMock).toHaveBeenCalledWith('/api/v1/calendars');
    // The tasks section reads its lists from this same result (story 21).
    expect(ids(store.calendars)).toEqual(['1', '2', '3']);
    expect(store.calendars[1]!.kind).toBe('tasks');
    expect(ids(store.eventCalendars)).toEqual(['1', '3']);
  });
});

describe('reminder refresh hook (#169)', () => {
  const formData = {
    summary: 'Sync',
    description: '',
    location: '',
    start: new Date(2026, 10, 5, 9, 0),
    end: new Date(2026, 10, 5, 10, 0),
    timezone: 'UTC',
    all_day: false,
  } as unknown as Parameters<ReturnType<typeof useCalendarStore>['createEvent']>[1];

  const store = () => {
    const s = useCalendarStore();
    s.calendars = [cal({ id: 1 })];
    return s;
  };

  it.each([
    ['createEvent', (s: ReturnType<typeof useCalendarStore>) => s.createEvent('1', formData)],
    ['updateEvent', (s: ReturnType<typeof useCalendarStore>) => s.updateEvent('1', 'ev', formData)],
    ['updateEventTime', (s: ReturnType<typeof useCalendarStore>) => s.updateEventTime('ev', '1', new Date(), new Date())],
    ['moveEvent', (s: ReturnType<typeof useCalendarStore>) => s.moveEvent('1', 'ev', '1')],
    ['deleteEvent', (s: ReturnType<typeof useCalendarStore>) => s.deleteEvent('1', 'ev')],
  ])('%s asks for one refresh after the request succeeded', async (_name, act) => {
    apiMock.mockResolvedValue({ id: 'ev' });
    await act(store());
    expect(refreshMock).toHaveBeenCalledTimes(1);
  });

  it('a failed mutation does not ask for a refresh', async () => {
    apiMock.mockRejectedValue(new Error('boom'));
    await expect(store().deleteEvent('1', 'ev')).rejects.toThrow('boom');
    await expect(store().createEvent('1', formData)).rejects.toThrow('boom');
    expect(refreshMock).not.toHaveBeenCalled();
  });
});
