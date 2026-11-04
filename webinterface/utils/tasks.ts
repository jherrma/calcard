/**
 * Pure task helpers (#170): overdue, due-date bucket, priority level, the due
 * label and the sort order. No Vue and no store, so the store, every task
 * component and the specs share ONE definition of each.
 *
 * Due dates come in two shapes (see `Task` in `types/tasks.ts`):
 *   - all-day: `due_date` = `YYYY-MM-DD`, a CALENDAR DATE. It is read into
 *     local midnight from its parts and compared by date in the user's (the
 *     browser's) timezone, never through `new Date('YYYY-MM-DD')`, which is UTC
 *     midnight and lands on the previous day west of Greenwich.
 *   - timed: `due` = an RFC 3339 instant, compared as an instant. R3: a floating
 *     DUE arrives as UTC wall time; a known server approximation.
 *
 * These rules match the server's `GET /tasks/summary`, so a row styled overdue
 * here is a row the sidebar badge counts: an open task is overdue when its
 * timed due instant is before now, or its due date is before today's date. A
 * done task (completed or cancelled) is never overdue.
 *
 * Day arithmetic goes through `dashboardDates.ts` (`setDate`, local calendar
 * days), so a 23- or 25-hour DST day never moves a task onto the wrong date.
 *
 * Auto-imported by Nuxt (files under `utils/`).
 */

import type { TimeFormat } from '~/types/settings';
import type { DueBucket, PriorityLevel, Task, TaskSortBy, TaskSortDir } from '~/types/tasks';
import { addDays, dayDiff, dayKey } from './dashboardDates';

/** A task's due date, read from the wire pair. `date` is local midnight for an all-day one. */
interface TaskDue {
  allDay: boolean;
  date: Date;
}

const DATE_ONLY = /^(\d{4})-(\d{2})-(\d{2})$/;

/**
 * `YYYY-MM-DD` as local midnight of that calendar date, or null when it is not
 * a real date (`2026-02-30` included: it would roll into March).
 */
export function parseDateOnly(value: string): Date | null {
  const match = DATE_ONLY.exec(value);
  if (!match) return null;
  const year = Number(match[1]);
  const month = Number(match[2]) - 1;
  const day = Number(match[3]);
  const date = new Date(2000, 0, 1);
  date.setFullYear(year, month, day); // not the constructor: it maps years 0-99 to 19xx
  date.setHours(0, 0, 0, 0);
  if (date.getFullYear() !== year || date.getMonth() !== month || date.getDate() !== day) return null;
  return date;
}

/** The due date of a task, or null when it has none (or an unreadable one). `due_date` wins. */
function taskDue(task: Pick<Task, 'due' | 'due_date'>): TaskDue | null {
  if (task.due_date) {
    const date = parseDateOnly(task.due_date);
    return date ? { allDay: true, date } : null;
  }
  if (task.due) {
    const ms = Date.parse(task.due);
    return Number.isNaN(ms) ? null : { allDay: false, date: new Date(ms) };
  }
  return null;
}

/**
 * Done = completed OR cancelled: the one cancelled rule of the API (`status=completed`
 * lists both, `summary.completed` counts both, neither is ever overdue).
 */
export function isTaskDone(task: Pick<Task, 'status'>): boolean {
  return task.status === 'completed' || task.status === 'cancelled';
}

/**
 * Whether the task is overdue at `now`: open, and its timed due instant is
 * before `now` (at the instant itself it is not yet), or its all-day due date
 * is before `now`'s local date (an all-day task due today is never overdue
 * that day, not even at 23:59).
 */
export function isOverdue(task: Pick<Task, 'status' | 'due' | 'due_date'>, now: Date): boolean {
  if (isTaskDone(task)) return false;
  const due = taskDue(task);
  if (!due) return false;
  if (due.allDay) return dayKey(due.date) < dayKey(now);
  return due.date.getTime() < now.getTime();
}

/**
 * The earliest instant, as epoch ms, after `now` at which an open task with a
 * timed due date turns overdue: the soonest timed due instant still ahead,
 * or null when none is. All-day tasks turn overdue at local midnight, which
 * the caller knows on its own.
 */
export function nextTimedDueMs(tasks: readonly Pick<Task, 'status' | 'due' | 'due_date'>[], now: Date): number | null {
  let next: number | null = null;
  for (const task of tasks) {
    const due = isTaskDone(task) ? null : taskDue(task);
    if (!due || due.allDay) continue;
    const ms = due.date.getTime();
    if (ms > now.getTime() && (next === null || ms < next)) next = ms;
  }
  return next;
}

/**
 * Where the task's due date falls at `now`, for the overdue styling:
 *   - `overdue`: exactly when `isOverdue` (open tasks only);
 *   - `today`: due on `now`'s local date and not overdue (a timed task later
 *     today, an all-day task due today, or a done task due today);
 *   - `later`: has a due date and is neither — due after today, or a DONE task
 *     whose due date has passed (a done task is never overdue);
 *   - `none`: no due date.
 * Grouping is by status, never by bucket. The due sort (`compareTasks`) orders
 * open tasks overdue, today, later, undated without needing `now`.
 */
export function dueBucket(task: Pick<Task, 'status' | 'due' | 'due_date'>, now: Date): DueBucket {
  const due = taskDue(task);
  if (!due) return 'none';
  if (isOverdue(task, now)) return 'overdue';
  return dayKey(due.date) === dayKey(now) ? 'today' : 'later';
}

/**
 * The display level of an RFC 5545 PRIORITY: 1-4 high, 5 medium, 6-9 low;
 * null, 0 (RFC "undefined") and anything outside 1..9 are none.
 */
export function priorityLevel(priority: number | null | undefined): PriorityLevel {
  if (priority == null || !Number.isInteger(priority)) return 'none';
  if (priority >= 1 && priority <= 4) return 'high';
  if (priority === 5) return 'medium';
  if (priority >= 6 && priority <= 9) return 'low';
  return 'none';
}

/** Today / Tomorrow / Yesterday for a whole-day distance, else null. English like the rest of the UI. */
function relativeDay(days: number): string | null {
  if (days === 0) return 'Today';
  if (days === 1) return 'Tomorrow';
  if (days === -1) return 'Yesterday';
  return null;
}

function dateOptions(withYear: boolean): Intl.DateTimeFormatOptions {
  return withYear
    ? { weekday: 'short', day: 'numeric', month: 'short', year: 'numeric' }
    : { weekday: 'short', day: 'numeric', month: 'short' };
}

/**
 * Hour options. With the user's preference the cycle is explicit, so it wins
 * over the locale's (story 103), and the hour width follows it: no leading
 * zero in 12-hour time, always two digits in 24-hour time. Without one, the
 * locale decides (like `formatTimeOfDay`).
 */
function timeOptions(timeFormat: TimeFormat | undefined): Intl.DateTimeFormatOptions {
  if (timeFormat === '12h') return { hour: 'numeric', minute: '2-digit', hourCycle: 'h12' };
  if (timeFormat === '24h') return { hour: '2-digit', minute: '2-digit', hourCycle: 'h23' };
  return { hour: '2-digit', minute: '2-digit' };
}

/**
 * The due label of a row, in the user's timezone:
 *   - no due date: `''`;
 *   - all-day: `Today`, `Tomorrow`, `Yesterday`, else the short date
 *     (`Wed 4 Nov` in en-GB), with the year when it is not `now`'s year;
 *   - timed: the time alone when due today (`17:00`), `Tomorrow, 17:00` /
 *     `Yesterday, 17:00`, else the short date with the time.
 * Overdue is not spelled out here: the row adds the marker (`dueBucket`).
 * `locale` defaults to the browser's; `timeFormat` is the user's 12h/24h
 * preference (`preferencesStore.timeFormat`), else the locale's clock.
 */
export function formatDue(
  task: Pick<Task, 'due' | 'due_date'>,
  now: Date,
  locale?: string | string[],
  timeFormat?: TimeFormat,
): string {
  const due = taskDue(task);
  if (!due) return '';
  const days = dayDiff(now, due.date);
  const otherYear = due.date.getFullYear() !== now.getFullYear();

  if (due.allDay) {
    return relativeDay(days) ?? new Intl.DateTimeFormat(locale, dateOptions(otherYear)).format(due.date);
  }

  const time = new Intl.DateTimeFormat(locale, timeOptions(timeFormat)).format(due.date);
  if (days === 0) return time;
  const relative = relativeDay(days);
  if (relative) return `${relative}, ${time}`;
  return new Intl.DateTimeFormat(locale, { ...dateOptions(otherYear), ...timeOptions(timeFormat) }).format(due.date);
}

/**
 * The moment a task's due date is up, the key of the due sort (epoch ms): a
 * timed due instant as is; an all-day date at the LAST MOMENT of that local
 * date, half a millisecond before the next local midnight (it is due BY the
 * end of that date, and turns overdue at that midnight; the half millisecond
 * keeps it apart from a timed task due exactly at midnight, which no whole-ms
 * instant can tie). So an all-day task due today sorts after today's timed
 * ones and before tomorrow's, and open tasks always come out in `dueBucket`
 * order — overdue, today, later — without the comparator needing `now`.
 */
function dueSortKey(task: Pick<Task, 'due' | 'due_date'>): number | null {
  const due = taskDue(task);
  if (!due) return null;
  return due.allDay ? addDays(due.date, 1).getTime() - 0.5 : due.date.getTime();
}

const PRIORITY_RANK: Record<Exclude<PriorityLevel, 'none'>, number> = { high: 0, medium: 1, low: 2 };

/** Priority by displayed LEVEL (high, medium, low), none = null. 1 and 4 are both "high" and tie. */
function prioritySortKey(task: Pick<Task, 'priority'>): number | null {
  const level = priorityLevel(task.priority);
  return level === 'none' ? null : PRIORITY_RANK[level];
}

/** Numbers in `sign` order; a missing value (null) sorts last in either direction. */
function compareMissingLast(a: number | null, b: number | null, sign: number): number {
  if (a === null || b === null) return a === b ? 0 : a === null ? 1 : -1;
  return sign * (a - b);
}

const titleCollator = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' });

type TaskComparator = (a: Task, b: Task) => number;

const byDue = (sign: number): TaskComparator => (a, b) => compareMissingLast(dueSortKey(a), dueSortKey(b), sign);
const byPriority = (sign: number): TaskComparator => (a, b) =>
  compareMissingLast(prioritySortKey(a), prioritySortKey(b), sign);
const byTitle = (sign: number): TaskComparator => (a, b) =>
  sign * titleCollator.compare(a.summary.trim(), b.summary.trim());

/** Last resort, so equal rows never swap between renders: creation (oldest first), then id. */
const byIdentity: TaskComparator = (a, b) =>
  compareMissingLast(timestamp(a.created_at), timestamp(b.created_at), 1) || (a.id < b.id ? -1 : a.id > b.id ? 1 : 0);

function timestamp(value: string): number | null {
  const ms = Date.parse(value);
  return Number.isNaN(ms) ? null : ms;
}

/**
 * The comparator for `Array.prototype.sort` within one status group (spec §7.4):
 *   - `due`: due date (see `dueSortKey`), then priority, then title;
 *   - `priority`: high, medium, low, then due date, then title;
 *   - `title`: alphabetical (case-insensitive, numbers in numeric order), then
 *     priority, then due date.
 * `dir` reverses the first key only. Undated tasks and tasks without a priority
 * sort LAST in both directions, and the tie-breakers keep their own order
 * (high priority first, earliest due first, A to Z), ending with creation time
 * and id so the order is total.
 */
export function compareTasks(sortBy: TaskSortBy, dir: TaskSortDir = 'asc'): TaskComparator {
  const sign = dir === 'desc' ? -1 : 1;
  const keys: TaskComparator[] =
    sortBy === 'priority'
      ? [byPriority(sign), byDue(1), byTitle(1)]
      : sortBy === 'title'
        ? [byTitle(sign), byPriority(1), byDue(1)]
        : [byDue(sign), byPriority(1), byTitle(1)];
  return (a, b) => {
    for (const key of keys) {
      const order = key(a, b);
      if (order !== 0) return order;
    }
    return byIdentity(a, b);
  };
}
