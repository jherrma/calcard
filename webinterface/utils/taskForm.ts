/**
 * The task form's conversions (#170 story 26): a task into form values, form
 * values into the POST body (`buildTaskInput`) or into a PATCH of the changed
 * keys only (`buildTaskPatch`), and the read-only text of a repeat rule.
 *
 * Text (summary, description) is plain text both ways: what the API returned
 * is shown and sent back as it is, never escaped or unescaped here. A form
 * saved without a change builds an EMPTY patch, so nothing is sent.
 *
 * Dates: an all-day value is sent as the LOCAL date (`dayKey`, never
 * `toISOString()`, which is the UTC date), a timed one as RFC 3339 with the
 * browser's offset (`toRFC3339`) plus the browser's IANA zone in `timezone`,
 * so the server writes it with that TZID.
 *
 * Auto-imported by Nuxt (files under `utils/`).
 */

import type { PriorityLevel, Task, TaskFormData, TaskInput, TaskPatch } from '~/types/tasks';
import { toRFC3339 } from '~/stores/calendars';
import { dayKey } from './dashboardDates';
import { parseDateOnly, priorityLevel } from './tasks';

/** What the form saves for each priority level (DECISIONS 3): RFC 5545 PRIORITY, none = removed. */
export const TASK_PRIORITY_VALUES: Record<PriorityLevel, number | null> = {
  none: null,
  low: 9,
  medium: 5,
  high: 1,
};

/** The server's limit on a title, in characters (Unicode code points) after trimming. */
export const TASK_SUMMARY_MAX = 500;

/** What a title longer than TASK_SUMMARY_MAX is told, in the edit form and the quick-add. */
export const TASK_SUMMARY_TOO_LONG = `Shorten the title to ${TASK_SUMMARY_MAX} characters or fewer.`;

/** A wire due/start pair (`due` + `due_date`, `start` + `start_date`) as a Date: local midnight for a date. */
function wireTime(instant: string | null, date: string | null): Date | null {
  if (date) return parseDateOnly(date);
  if (instant) {
    const ms = Date.parse(instant);
    return Number.isNaN(ms) ? null : new Date(ms);
  }
  return null;
}

/** The form values of a stored task. */
export function taskToFormData(task: Task): TaskFormData {
  return {
    summary: task.summary,
    description: task.description,
    listId: task.calendar_uuid,
    allDay: task.due_all_day,
    due: wireTime(task.due, task.due_date),
    start: wireTime(task.start, task.start_date),
    priority: priorityLevel(task.priority),
    percent: task.percent_complete,
    status: task.status,
  };
}

/**
 * The form values of a new task. All day is on: a task is usually due on a
 * day, not at a time.
 */
export function newTaskFormData(listId: string, summary = ''): TaskFormData {
  return {
    summary,
    description: '',
    listId,
    allDay: true,
    due: null,
    start: null,
    priority: 'none',
    percent: null,
    status: 'needs-action',
  };
}

/** A due/start as the API takes it: the local date when all-day, else RFC 3339 with the local offset; null = none. */
export function encodeTaskTime(value: Date | null, allDay: boolean): string | null {
  if (!value || Number.isNaN(value.getTime())) return null;
  return allDay ? dayKey(value) : toRFC3339(value);
}

/** Whether `due` falls before `start` (equal is allowed, as on the server). All-day values compare by date. */
export function isDueBeforeStart(due: Date | null, start: Date | null, allDay: boolean): boolean {
  const dueValue = encodeTaskTime(due, allDay);
  const startValue = encodeTaskTime(start, allDay);
  if (dueValue === null || startValue === null) return false;
  // Both are dates (YYYY-MM-DD sorts as text) or both instants.
  return allDay ? dueValue < startValue : due!.getTime() < start!.getTime();
}

/** Number of characters as the server counts them (code points), after trimming. */
export function summaryLength(summary: string): number {
  return Array.from(summary.trim()).length;
}

/** The browser's IANA zone, or undefined when it cannot be told (the server then writes UTC). */
function browserTimezone(): string | undefined {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || undefined;
  } catch {
    return undefined;
  }
}

/** The zone sent with the encoded due/start: the browser's when one of them is a timed value, else none. */
function timezoneForTimes(allDay: boolean, due: string | null, start: string | null): string | undefined {
  if (allDay || (due === null && start === null)) return undefined;
  return browserTimezone();
}

/**
 * The `POST …/tasks` body for the form values. Only what is set is sent:
 * no empty notes, no due/start, priority, progress or default status, so the
 * stored task carries no empty properties.
 */
export function buildTaskInput(form: TaskFormData): TaskInput {
  const input: TaskInput = { summary: form.summary.trim(), due_all_day: form.allDay };
  if (form.description !== '') input.description = form.description;
  const due = encodeTaskTime(form.due, form.allDay);
  const start = encodeTaskTime(form.start, form.allDay);
  if (due !== null) input.due = due;
  if (start !== null) input.start = start;
  const timezone = timezoneForTimes(form.allDay, due, start);
  if (timezone) input.timezone = timezone;
  const priority = TASK_PRIORITY_VALUES[form.priority];
  if (priority !== null) input.priority = priority;
  if (form.percent !== null) input.percent_complete = form.percent;
  if (form.status !== 'needs-action') input.status = form.status;
  return input;
}

/**
 * The `PATCH` body: only the keys whose value differs from `initial` (the
 * form's starting values). Empty when nothing changed: then nothing is sent.
 *
 * - A removed description, due, start, priority or progress is `null`.
 * - Due, start and the all-day flag go together: when any of them changed,
 *   due and start are both sent (an unchanged one is kept by the server, TZID
 *   included), so a stored due/start of the other value type never makes the
 *   server refuse the edit. `due_all_day` only when it changed; `timezone`
 *   with a timed value.
 * - A priority level the user did not change is not sent, so a client's 3
 *   (shown as "high") stays 3.
 * - On a repeating task status and progress are NEVER sent (the server
 *   refuses completed/cancelled and 100 % with a 409; the form disables both).
 */
export function buildTaskPatch(initial: TaskFormData, form: TaskFormData, recurring: boolean): TaskPatch {
  const patch: TaskPatch = {};
  const summary = form.summary.trim();
  if (summary !== initial.summary.trim()) patch.summary = summary;
  if (form.description !== initial.description) patch.description = form.description === '' ? null : form.description;

  const due = encodeTaskTime(form.due, form.allDay);
  const start = encodeTaskTime(form.start, form.allDay);
  const timesChanged =
    form.allDay !== initial.allDay ||
    due !== encodeTaskTime(initial.due, initial.allDay) ||
    start !== encodeTaskTime(initial.start, initial.allDay);
  if (timesChanged) {
    if (form.allDay !== initial.allDay) patch.due_all_day = form.allDay;
    patch.due = due;
    patch.start = start;
    const timezone = timezoneForTimes(form.allDay, due, start);
    if (timezone) patch.timezone = timezone;
  }

  if (form.priority !== initial.priority) patch.priority = TASK_PRIORITY_VALUES[form.priority];
  if (!recurring) {
    if (form.percent !== initial.percent) patch.percent_complete = form.percent;
    if (form.status !== initial.status) patch.status = form.status;
  }
  return patch;
}

/** Whether a patch changes nothing (then the form closes without a request). */
export function isEmptyTaskPatch(patch: TaskPatch): boolean {
  return Object.keys(patch).length === 0;
}

const FREQUENCIES: Record<string, { single: string; unit: string }> = {
  DAILY: { single: 'Daily', unit: 'day' },
  WEEKLY: { single: 'Weekly', unit: 'week' },
  MONTHLY: { single: 'Monthly', unit: 'month' },
  YEARLY: { single: 'Yearly', unit: 'year' },
};

const WEEKDAYS: Record<string, string> = {
  MO: 'Mon', TU: 'Tue', WE: 'Wed', TH: 'Thu', FR: 'Fri', SA: 'Sat', SU: 'Sun',
};

/** UNTIL (`YYYYMMDD` or `YYYYMMDDTHHMMSS[Z]`) as a date label, or null. */
function untilLabel(value: string, locale: string | undefined): string | null {
  const match = /^(\d{4})(\d{2})(\d{2})(?:T(\d{2})(\d{2})(\d{2})(Z)?)?$/.exec(value);
  if (!match) return null;
  const [, y, m, d, hh, mm, ss, z] = match;
  const date = hh === undefined
    ? parseDateOnly(`${y}-${m}-${d}`)
    : z
      ? new Date(Date.UTC(Number(y), Number(m) - 1, Number(d), Number(hh), Number(mm), Number(ss)))
      : new Date(Number(y), Number(m) - 1, Number(d), Number(hh), Number(mm), Number(ss));
  if (!date || Number.isNaN(date.getTime())) return null;
  return new Intl.DateTimeFormat(locale, { day: 'numeric', month: 'short', year: 'numeric' }).format(date);
}

/**
 * The repeat rule of a task as read-only text for the form ("Weekly on Mon,
 * Wed", "Every 2 days, 10 times", "Monthly until 31 Dec 2026"). Rules with
 * parts this does not put into words (BYMONTHDAY, BYDAY with a position such
 * as `1MO`, …) are shown as written; a task repeating by RDATE alone (no
 * rule) says "On set dates". There is no recurrence editing (spec §10).
 */
export function describeRecurrence(rrule: string | null, locale?: string): string {
  if (!rrule || !rrule.trim()) return 'On set dates';
  const raw = rrule.trim().replace(/^RRULE:/i, '');
  const parts = new Map<string, string>();
  for (const part of raw.split(';')) {
    const [key, value] = part.split('=');
    if (!key || value === undefined) return raw;
    parts.set(key.toUpperCase(), value.toUpperCase());
  }
  const freq = FREQUENCIES[parts.get('FREQ') ?? ''];
  const known = new Set(['FREQ', 'INTERVAL', 'BYDAY', 'COUNT', 'UNTIL', 'WKST']);
  if (!freq || [...parts.keys()].some((key) => !known.has(key))) return raw;

  const interval = parts.has('INTERVAL') ? Number(parts.get('INTERVAL')) : 1;
  if (!Number.isInteger(interval) || interval < 1) return raw;
  let text = interval === 1 ? freq.single : `Every ${interval} ${freq.unit}s`;

  const byDay = parts.get('BYDAY');
  if (byDay !== undefined) {
    const days = byDay.split(',').map((code) => WEEKDAYS[code]);
    if (days.some((day) => day === undefined)) return raw;
    text += ` on ${days.join(', ')}`;
  }

  const count = parts.get('COUNT');
  const until = parts.get('UNTIL');
  if (count !== undefined) {
    const times = Number(count);
    if (!Number.isInteger(times) || times < 1) return raw;
    text += times === 1 ? ', once' : `, ${times} times`;
  } else if (until !== undefined) {
    const label = untilLabel(until, locale);
    if (label === null) return raw;
    text += ` until ${label}`;
  }
  return text;
}
