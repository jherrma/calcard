/**
 * Local-date helpers for the dashboard (story 042).
 *
 * The story sketch used date-fns, which is NOT a dependency of this project and
 * is not worth adding for a handful of calls — so these are the primitives the
 * dashboard needs, built on plain `Date`.
 *
 * Everything works in the browser's LOCAL timezone, matching how the calendar
 * page renders events, and day/month stepping goes through `setDate`/the
 * constructor rather than adding 86_400_000 ms so DST transitions (a 23h or 25h
 * day) don't drift the result onto the wrong calendar day.
 *
 * Auto-imported by Nuxt (files under `utils/`) — call these without an import.
 */

/** A half-open time interval in epoch milliseconds: `[start, end)`. */
export interface TimeInterval {
  start: number;
  end: number;
}

/** One cell of a mini-calendar month grid. */
export interface MonthGridDay {
  /** Local `YYYY-MM-DD` key, matching {@link dayKey}. */
  key: string;
  date: Date;
  /** Day of month (1–31). */
  day: number;
  /** True for the leading/trailing days that belong to the adjacent month. */
  otherMonth: boolean;
}

function pad2(n: number): string {
  return n.toString().padStart(2, '0');
}

/** Local midnight at the start of `d`'s day. */
export function startOfDay(d: Date): Date {
  const copy = new Date(d.getTime());
  copy.setHours(0, 0, 0, 0);
  return copy;
}

/** `d` shifted by whole calendar days (DST-safe). */
export function addDays(d: Date, days: number): Date {
  const copy = new Date(d.getTime());
  copy.setDate(copy.getDate() + days);
  return copy;
}

/** Local midnight on the 1st of `d`'s month. */
export function startOfMonth(d: Date): Date {
  return new Date(d.getFullYear(), d.getMonth(), 1);
}

/**
 * `d`'s month shifted by `months`, always anchored on the 1st. Anchoring matters:
 * `new Date(2026, 0, 31)` plus one month via `setMonth` overflows to March 3rd.
 */
export function addMonths(d: Date, months: number): Date {
  return new Date(d.getFullYear(), d.getMonth() + months, 1);
}

/** Local midnight on the 1st of the month AFTER `d` — an exclusive month end. */
export function startOfNextMonth(d: Date): Date {
  return addMonths(d, 1);
}

/** Local `YYYY-MM-DD`. Used as the identity of a day across the dashboard. */
export function dayKey(d: Date): string {
  return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())}`;
}

/** Local `YYYY-MM`. Identifies which months of events have been fetched. */
export function monthKey(d: Date): string {
  return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}`;
}

/** Parse a `YYYY-MM` key back into local midnight on that month's 1st. */
export function monthFromKey(key: string): Date | null {
  const match = /^(\d{4})-(\d{2})$/.exec(key);
  if (!match) return null;
  return new Date(Number(match[1]), Number(match[2]) - 1, 1);
}

/** Whole calendar days from `a` to `b` (negative when `b` is earlier). */
export function dayDiff(a: Date, b: Date): number {
  // Rounding absorbs the 23h/25h DST days that would otherwise yield ±0.96.
  return Math.round((startOfDay(b).getTime() - startOfDay(a).getTime()) / 86400000);
}

/** Minutes elapsed since local midnight — the vertical position on a day timeline. */
export function minutesIntoDay(d: Date): number {
  return d.getHours() * 60 + d.getMinutes();
}

/**
 * The interval an event occupies, or `null` when its start is unparseable.
 *
 * A stored VEVENT may carry no DTEND (a VTODO carries neither), in which case
 * the API serializes the zero time and `end <= start`. Such an event is treated
 * as a 1 ms point so overlap tests still place it on its own day instead of
 * dropping it.
 */
export function eventBounds(start: string, end: string): TimeInterval | null {
  const from = new Date(start).getTime();
  if (Number.isNaN(from)) return null;
  let to = new Date(end).getTime();
  if (Number.isNaN(to) || to <= from) to = from + 1;
  return { start: from, end: to };
}

/** Whether `interval` overlaps the half-open range `[from, to)`. */
export function overlaps(interval: TimeInterval, from: number, to: number): boolean {
  return interval.start < to && interval.end > from;
}

/**
 * Local day keys touched by the half-open range `[from, to)`. An end that lands
 * exactly on midnight does NOT include the following day — which is what makes
 * an all-day event ending at the next midnight highlight a single day.
 *
 * `maxDays` caps pathological multi-year events so a single bad row can't spin
 * the loop for thousands of iterations.
 */
export function dayKeysBetween(from: number, to: number, maxDays = 400): string[] {
  const keys: string[] = [];
  let cursor = startOfDay(new Date(from));
  const lastDay = startOfDay(new Date(Math.max(to - 1, from))).getTime();
  while (cursor.getTime() <= lastDay && keys.length < maxDays) {
    keys.push(dayKey(cursor));
    cursor = addDays(cursor, 1);
  }
  return keys;
}

/**
 * Human label for how far off a date is: `Today` / `Tomorrow` / `Yesterday`,
 * the weekday name inside the coming week, else a short date. Story 042 asks
 * specifically for the Today/Tomorrow labels on near events.
 */
export function relativeDayLabel(date: Date, now: Date): string {
  const days = dayDiff(now, date);
  if (days === 0) return 'Today';
  if (days === 1) return 'Tomorrow';
  if (days === -1) return 'Yesterday';
  if (days > 1 && days < 7) return date.toLocaleDateString(undefined, { weekday: 'long' });
  return date.toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
}

/** `HH:MM` in the user's locale/clock convention. */
export function formatTimeOfDay(d: Date): string {
  return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
}

/**
 * A fixed 6-week (42 cell) grid for the given month, so the mini calendar keeps
 * a constant height across months.
 *
 * Weeks start on SUNDAY to match the calendar page, which renders FullCalendar
 * with its default `firstDay: 0`; a Monday-first mini calendar would disagree
 * with the view it links into.
 */
export function buildMonthGrid(year: number, month: number): MonthGridDay[] {
  const first = new Date(year, month, 1);
  const gridStart = addDays(first, -first.getDay());
  const cells: MonthGridDay[] = [];
  for (let i = 0; i < 42; i++) {
    const date = addDays(gridStart, i);
    cells.push({
      key: dayKey(date),
      date,
      day: date.getDate(),
      otherMonth: date.getMonth() !== ((month % 12) + 12) % 12,
    });
  }
  return cells;
}
