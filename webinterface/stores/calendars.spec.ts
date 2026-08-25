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

// Minimal Calendar factory — id is a NUMBER at runtime (typed string), which is
// exactly the number-vs-string trap these specs guard.
function cal(partial: Omit<Partial<Calendar>, 'id'> & { id: number }): Calendar {
  return {
    uuid: `uuid-${partial.id}`,
    path: `/cal/${partial.id}`,
    name: `Cal ${partial.id}`,
    color: '#3b82f6',
    owner_id: 'owner-1',
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
