import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import {
  contactMetaLine,
  eventRelativeLabel,
  formatEventWhen,
  isPastEvent,
  searchAvatarColor,
  searchInitials,
  toLocalDateParam,
} from './searchFormat';
import type { CalendarEvent } from '~/types/calendar';
import type { ContactHit } from '~/types/search';

// SCOPE: the row labels shared by the global-search palette and the full results
// page (story 044). They are pure, so they are tested here rather than through two
// component trees.

function event(overrides: Partial<CalendarEvent> = {}): CalendarEvent {
  return {
    id: 'ev-1',
    calendar_id: 7,
    uid: 'uid-1',
    summary: 'Team Standup',
    // Local parts, so the assertions don't depend on the runner's timezone.
    start: new Date(2026, 5, 16, 9, 0, 0).toISOString(),
    end: new Date(2026, 5, 16, 9, 30, 0).toISOString(),
    all_day: false,
    is_recurring: false,
    ...overrides,
  };
}

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date(2026, 5, 16, 12, 0, 0));
});

afterEach(() => {
  vi.useRealTimers();
});

describe('formatEventWhen', () => {
  it('includes the time for a timed event and omits it for an all-day one', () => {
    expect(formatEventWhen(event())).toMatch(/,/); // day + time, comma separated
    const allDay = formatEventWhen(event({ all_day: true }));
    expect(allDay).not.toMatch(/\d{2}:\d{2}/);
  });

  it('returns an empty string for an unparseable start', () => {
    expect(formatEventWhen(event({ start: 'not a date' }))).toBe('');
  });
});

describe('eventRelativeLabel', () => {
  it('labels the local day, not a 24h delta', () => {
    // 23:30 today is still "Today" even though it is more than 12h away.
    expect(eventRelativeLabel(event({ start: new Date(2026, 5, 16, 23, 30).toISOString() }))).toBe('Today');
    expect(eventRelativeLabel(event({ start: new Date(2026, 5, 17, 1, 0).toISOString() }))).toBe('Tomorrow');
    expect(eventRelativeLabel(event({ start: new Date(2026, 5, 20, 9, 0).toISOString() }))).not.toBe('Tomorrow');
  });
});

describe('isPastEvent', () => {
  it('measures against the END of the event, so a running event is not past', () => {
    const running = event({
      start: new Date(2026, 5, 16, 11, 30).toISOString(),
      end: new Date(2026, 5, 16, 12, 30).toISOString(),
    });
    expect(isPastEvent(running)).toBe(false);
    expect(isPastEvent(event({ start: new Date(2026, 5, 15, 9, 0).toISOString(), end: new Date(2026, 5, 15, 10, 0).toISOString() }))).toBe(true);
  });

  it('falls back to the start when there is no end', () => {
    expect(isPastEvent(event({ start: new Date(2026, 5, 15, 9, 0).toISOString(), end: undefined }))).toBe(true);
  });
});

describe('searchInitials', () => {
  it.each([
    ['Alice Adams', 'AA'],
    ['alice van der Berg', 'AB'], // first + LAST word
    ['Cher', 'C'],
    ['', '?'],
    [undefined, '?'],
  ])('%s → %s', (input, expected) => {
    expect(searchInitials(input)).toBe(expected);
  });
});

describe('searchAvatarColor', () => {
  it('is stable per seed and inside the palette', () => {
    const a = searchAvatarColor('Alice Adams');
    expect(a).toBe(searchAvatarColor('Alice Adams'));
    expect(a).toMatch(/^#[0-9a-f]{6}$/);
  });
});

describe('toLocalDateParam', () => {
  it('uses local date parts (an evening event keeps its own day)', () => {
    expect(toLocalDateParam(new Date(2026, 5, 16, 23, 30))).toBe('2026-06-16');
    expect(toLocalDateParam(new Date(2026, 0, 1, 0, 5))).toBe('2026-01-01');
  });
});

describe('contactMetaLine', () => {
  const hit = (overrides: Partial<ContactHit['contact']> = {}, addressBookName = 'Work'): ContactHit => ({
    contact: {
      id: 'ct-1',
      addressbook_id: '2',
      uid: 'uid-ct-1',
      formatted_name: 'Alice Adams',
      created_at: '',
      updated_at: '',
      ...overrides,
    },
    addressBookId: '2',
    addressBookName,
  });

  it('prefers the primary email and drops empty parts', () => {
    expect(
      contactMetaLine(
        hit({
          organization: 'Acme',
          emails: [
            { type: 'work', value: 'secondary@example.com' },
            { type: 'home', value: 'alice@example.com', primary: true },
          ],
        })
      )
    ).toBe('Acme · alice@example.com · Work');

    expect(contactMetaLine(hit({}, ''))).toBe('');
  });

  it('falls back to the first email when none is primary', () => {
    expect(contactMetaLine(hit({ emails: [{ type: 'work', value: 'first@example.com' }] }, ''))).toBe(
      'first@example.com'
    );
  });
});
