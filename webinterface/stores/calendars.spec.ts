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
  it('calls the move endpoint with the target calendar id', async () => {
    apiMock.mockResolvedValueOnce(undefined);
    const store = useCalendarStore();

    await store.moveEvent('1', 'ev-9', '2');

    expect(apiMock).toHaveBeenCalledTimes(1);
    const [url, opts] = apiMock.mock.calls[0]!;
    expect(url).toBe('/api/v1/calendars/1/events/ev-9/move');
    expect(opts).toMatchObject({
      method: 'POST',
      body: { target_calendar_id: '2' },
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
});
