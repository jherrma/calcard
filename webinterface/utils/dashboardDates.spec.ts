// @vitest-environment nuxt
import { describe, it, expect } from 'vitest';
import {
  addDays,
  addMonths,
  buildMonthGrid,
  dayDiff,
  dayKey,
  dayKeysBetween,
  eventBounds,
  mergeMonthRanges,
  minutesIntoDay,
  monthFromKey,
  monthKey,
  overlaps,
  relativeDayLabel,
  startOfDay,
  startOfMonth,
  startOfNextMonth,
} from './dashboardDates';

// Every date here is built from LOCAL components, so the assertions hold in any
// timezone the test runner happens to use.

describe('day/month keys', () => {
  it('formats local day and month keys zero-padded', () => {
    const d = new Date(2026, 6, 5, 23, 30); // 5 Jul 2026, local
    expect(dayKey(d)).toBe('2026-07-05');
    expect(monthKey(d)).toBe('2026-07');
  });

  it('merges contiguous month keys and keeps distant ones apart', () => {
    const ranges = mergeMonthRanges(['2026-08', '2026-07', '2020-01']);

    // Sorted, July+August collapsed into one half-open range, January 2020 on
    // its own — a single min..max span would have covered six years.
    expect(ranges).toHaveLength(2);
    expect(ranges[0]!.start.getTime()).toBe(new Date(2020, 0, 1).getTime());
    expect(ranges[0]!.end.getTime()).toBe(new Date(2020, 1, 1).getTime());
    expect(ranges[1]!.start.getTime()).toBe(new Date(2026, 6, 1).getTime());
    expect(ranges[1]!.end.getTime()).toBe(new Date(2026, 8, 1).getTime());
  });

  it('bridges a year boundary, collapses duplicates and drops junk keys', () => {
    const ranges = mergeMonthRanges(['2026-12', '2027-01', '2026-12', 'nope']);

    expect(ranges).toHaveLength(1);
    expect(ranges[0]!.start.getTime()).toBe(new Date(2026, 11, 1).getTime());
    expect(ranges[0]!.end.getTime()).toBe(new Date(2027, 1, 1).getTime());
    expect(mergeMonthRanges([])).toEqual([]);
  });

  it('round-trips a month key back to local midnight on the 1st', () => {
    const parsed = monthFromKey('2026-02');
    expect(parsed).not.toBeNull();
    expect(parsed!.getFullYear()).toBe(2026);
    expect(parsed!.getMonth()).toBe(1);
    expect(parsed!.getDate()).toBe(1);
    expect(parsed!.getHours()).toBe(0);
    expect(monthFromKey('nonsense')).toBeNull();
  });
});

describe('month arithmetic', () => {
  it('addMonths anchors on the 1st so a 31-day month cannot overflow', () => {
    // The bug this guards: new Date(2026, 0, 31).setMonth(+1) rolls to March 3rd.
    const jan31 = new Date(2026, 0, 31);
    const next = addMonths(jan31, 1);
    expect(next.getMonth()).toBe(1); // February, not March
    expect(next.getDate()).toBe(1);
  });

  it('startOfMonth / startOfNextMonth bracket the month half-open', () => {
    const mid = new Date(2026, 11, 17, 8, 45);
    expect(dayKey(startOfMonth(mid))).toBe('2026-12-01');
    expect(dayKey(startOfNextMonth(mid))).toBe('2027-01-01');
  });
});

describe('day helpers', () => {
  it('startOfDay/addDays/minutesIntoDay work on local time', () => {
    const d = new Date(2026, 2, 10, 14, 30);
    expect(minutesIntoDay(d)).toBe(14 * 60 + 30);
    expect(startOfDay(d).getHours()).toBe(0);
    expect(dayKey(addDays(d, 25))).toBe('2026-04-04');
  });

  it('dayDiff counts whole calendar days in both directions', () => {
    const base = new Date(2026, 4, 10, 9, 0);
    expect(dayDiff(base, new Date(2026, 4, 10, 23, 59))).toBe(0);
    expect(dayDiff(base, new Date(2026, 4, 11, 0, 1))).toBe(1);
    expect(dayDiff(base, new Date(2026, 4, 9, 23, 59))).toBe(-1);
  });
});

describe('eventBounds / overlaps', () => {
  it('treats a missing or inverted end as a point in time instead of dropping it', () => {
    const start = new Date(2026, 4, 10, 9, 0).toISOString();
    // The API serializes the zero time when a stored object carries no DTEND.
    const bounds = eventBounds(start, '0001-01-01T00:00:00Z');
    expect(bounds).not.toBeNull();
    expect(bounds!.end).toBe(bounds!.start + 1);
  });

  it('returns null for an unparseable start', () => {
    expect(eventBounds('not-a-date', 'also-not')).toBeNull();
  });

  it('overlaps is half-open: touching endpoints do not count', () => {
    const from = new Date(2026, 4, 10).getTime();
    const to = new Date(2026, 4, 11).getTime();
    expect(overlaps({ start: from - 1000, end: from + 1000 }, from, to)).toBe(true);
    expect(overlaps({ start: to, end: to + 1000 }, from, to)).toBe(false);
    expect(overlaps({ start: from - 2000, end: from }, from, to)).toBe(false);
  });
});

describe('dayKeysBetween', () => {
  it('covers each day a multi-day event touches', () => {
    const from = new Date(2026, 4, 10, 22, 0).getTime();
    const to = new Date(2026, 4, 12, 3, 0).getTime();
    expect(dayKeysBetween(from, to)).toEqual(['2026-05-10', '2026-05-11', '2026-05-12']);
  });

  it('does not spill onto the next day when the range ends exactly at midnight', () => {
    // An all-day event is stored as [midnight, next midnight) — one day only.
    const from = new Date(2026, 4, 10).getTime();
    const to = new Date(2026, 4, 11).getTime();
    expect(dayKeysBetween(from, to)).toEqual(['2026-05-10']);
  });

  it('always yields the starting day and honours the iteration cap', () => {
    const from = new Date(2026, 4, 10, 9, 0).getTime();
    expect(dayKeysBetween(from, from + 1)).toEqual(['2026-05-10']);
    expect(dayKeysBetween(from, new Date(2030, 0, 1).getTime(), 5)).toHaveLength(5);
  });
});

describe('relativeDayLabel', () => {
  const now = new Date(2026, 6, 30, 10, 0); // Thursday 30 Jul 2026

  it('labels the near days by name', () => {
    expect(relativeDayLabel(new Date(2026, 6, 30, 23, 0), now)).toBe('Today');
    expect(relativeDayLabel(new Date(2026, 6, 31, 1, 0), now)).toBe('Tomorrow');
    expect(relativeDayLabel(new Date(2026, 6, 29, 23, 0), now)).toBe('Yesterday');
  });

  it('uses the weekday inside the coming week and a short date beyond it', () => {
    const inThreeDays = new Date(2026, 7, 2, 12, 0);
    expect(relativeDayLabel(inThreeDays, now)).toBe(
      inThreeDays.toLocaleDateString(undefined, { weekday: 'long' })
    );

    const inThreeWeeks = new Date(2026, 7, 20, 12, 0);
    expect(relativeDayLabel(inThreeWeeks, now)).toBe(
      inThreeWeeks.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
    );
  });
});

describe('buildMonthGrid', () => {
  it('returns a fixed 6-week Sunday-first grid around the month', () => {
    // July 2026 starts on a Wednesday, so the grid leads in with Jun 28–30.
    const grid = buildMonthGrid(2026, 6);
    expect(grid).toHaveLength(42);
    expect(grid[0]!.key).toBe('2026-06-28');
    expect(grid[0]!.otherMonth).toBe(true);
    expect(grid[0]!.date.getDay()).toBe(0); // Sunday

    const first = grid.find((c) => c.key === '2026-07-01');
    expect(first).toBeDefined();
    expect(first!.otherMonth).toBe(false);
    expect(first!.day).toBe(1);

    // 31 in-month days, the rest belong to the neighbours.
    expect(grid.filter((c) => !c.otherMonth)).toHaveLength(31);
    expect(grid[41]!.key).toBe('2026-08-08');
  });

  it('keeps day keys unique and consecutive', () => {
    const grid = buildMonthGrid(2027, 1); // February 2027
    expect(new Set(grid.map((c) => c.key)).size).toBe(42);
    for (let i = 1; i < grid.length; i++) {
      expect(dayDiff(grid[i - 1]!.date, grid[i]!.date)).toBe(1);
    }
  });
});
