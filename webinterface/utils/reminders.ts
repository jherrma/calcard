import type { Reminder, ReminderInput } from '~/types/calendar';

// Reminder helpers of the event (and, in story 18, task) form (#169, spec §10.9).
//
// The web app writes RELATIVE reminders only (D23). A timed one is seconds before the start; an all-day
// one is seconds from the date's midnight, so "1 day before at 09:00" is -86400 + 9 * 3600 = -54000. The server
// resolves that in the calendar owner's zone, so nothing here reads the browser's zone and the result is the
// same in every browser. Only an absolute chip ("At 4 Nov, 09:00") is shown in the browser's zone.

export const MINUTE_S = 60;
export const HOUR_S = 3600;
export const DAY_S = 86400;
export const WEEK_S = 7 * DAY_S;

/** The form holds at most this many editable reminders (the server caps an item at 10). */
export const MAX_EDITABLE_REMINDERS = 5;
/** Time of day of the all-day presets. */
export const ALL_DAY_DEFAULT_TIME_S = 9 * HOUR_S;

export interface ReminderPreset {
  label: string;
  offset: number;
}

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
const pad2 = (n: number) => n.toString().padStart(2, '0');

const plural = (n: number, unit: string) => `${n} ${unit}${n === 1 ? '' : 's'}`;

/** "09:00" for a time of day in seconds (0-86399). */
export function timeOfDayLabel(seconds: number): string {
  return `${pad2(Math.floor(seconds / HOUR_S))}:${pad2(Math.floor((seconds % HOUR_S) / MINUTE_S))}`;
}

/** All-day offset for "N days before at <time>" (N = 0 is "on the day"). `timeS` is seconds after midnight. */
export function allDayOffset(daysBefore: number, timeS: number): number {
  return -daysBefore * DAY_S + timeS;
}

/** Back to the form: whole days before (negative = after) and the time of day in seconds. */
export function splitAllDayOffset(offset: number): { daysBefore: number; timeS: number } {
  const floor = Math.floor(offset / DAY_S);
  // `0 - 0` keeps a "0 days before" from being -0.
  return { daysBefore: 0 - floor, timeS: offset - floor * DAY_S };
}

/** "1 day before at 09:00", "On the day at 09:00", "2 days after at 01:00". */
export function allDayLabel(daysBefore: number, timeS: number): string {
  const time = timeOfDayLabel(timeS);
  if (daysBefore === 0) return `On the day at ${time}`;
  const n = Math.abs(daysBefore);
  const dir = daysBefore > 0 ? 'before' : 'after';
  if (n % 7 === 0) return `${plural(n / 7, 'week')} ${dir} at ${time}`;
  return `${plural(n, 'day')} ${dir} at ${time}`;
}

/** "15 minutes", "1 hour", "2 days", "1 week"; the largest unit that divides the seconds evenly. */
export function durationLabel(seconds: number): string {
  const s = Math.abs(seconds);
  if (s % WEEK_S === 0) return plural(s / WEEK_S, 'week');
  if (s % DAY_S === 0) return plural(s / DAY_S, 'day');
  if (s % HOUR_S === 0) return plural(s / HOUR_S, 'hour');
  if (s % MINUTE_S === 0) return plural(s / MINUTE_S, 'minute');
  return plural(s, 'second');
}

const TIMED_PRESET_OFFSETS: Array<[string, number]> = [
  ['At start', 0],
  ['5 minutes before', -5 * MINUTE_S],
  ['10 minutes before', -10 * MINUTE_S],
  ['15 minutes before', -15 * MINUTE_S],
  ['30 minutes before', -30 * MINUTE_S],
  ['1 hour before', -HOUR_S],
  ['2 hours before', -2 * HOUR_S],
  ['1 day before', -DAY_S],
  ['2 days before', -2 * DAY_S],
  ['1 week before', -WEEK_S],
];

/** Presets of a timed event, in menu order. */
export const TIMED_REMINDER_PRESETS: ReminderPreset[] = TIMED_PRESET_OFFSETS.map(([label, offset]) => ({ label, offset }));

/** Presets of an all-day event (D23): relative to the date's midnight, so "On the day at 09:00" is `32400`. */
export const ALL_DAY_REMINDER_PRESETS: ReminderPreset[] = [0, 1, 2, 7].map((days) => ({
  label: allDayLabel(days, ALL_DAY_DEFAULT_TIME_S),
  offset: allDayOffset(days, ALL_DAY_DEFAULT_TIME_S),
}));

export const reminderPresets = (allDay: boolean): ReminderPreset[] =>
  allDay ? ALL_DAY_REMINDER_PRESETS : TIMED_REMINDER_PRESETS;

export type CustomUnit = 'minutes' | 'hours' | 'days' | 'weeks';
export const CUSTOM_UNITS: Array<{ value: CustomUnit; label: string; seconds: number }> = [
  { value: 'minutes', label: 'minutes', seconds: MINUTE_S },
  { value: 'hours', label: 'hours', seconds: HOUR_S },
  { value: 'days', label: 'days', seconds: DAY_S },
  { value: 'weeks', label: 'weeks', seconds: WEEK_S },
];

/** "<amount> <unit> before" of a timed event as an offset: 3 hours -> -10800. */
export function customTimedOffset(amount: number, unit: CustomUnit): number {
  const seconds = CUSTOM_UNITS.find((u) => u.value === unit)?.seconds ?? MINUTE_S;
  return -amount * seconds;
}

/** "<days> days before at <time>" of an all-day event as an offset: 2 days at 07:30 -> -145800. */
export function customAllDayOffset(daysBefore: number, hours: number, minutes: number): number {
  return allDayOffset(daysBefore, hours * HOUR_S + minutes * MINUTE_S);
}

/** A reminder the form creates (not yet stored): `index` -1, display action, start-related. */
export function offsetReminder(offset: number): Reminder {
  return { index: -1, action: 'display', related: 'start', offset_seconds: offset, at: null, editable: true };
}

/** "4 Nov, 09:00" in the browser's zone. */
export function absoluteLabel(at: string): string {
  const d = new Date(at);
  if (Number.isNaN(d.getTime())) return at;
  return `${d.getDate()} ${MONTHS[d.getMonth()]}, ${pad2(d.getHours())}:${pad2(d.getMinutes())}`;
}

const ACTION_LABELS: Record<string, string> = { email: 'E-mail', audio: 'Sound', none: 'No action' };

function relativeLabel(reminder: Reminder, offset: number, allDay: boolean): string {
  if (reminder.related === 'end') {
    // Relative to the end (or the due date of a task): no all-day wording, it is a plain duration.
    if (offset === 0) return 'At end';
    return `${durationLabel(offset)} ${offset < 0 ? 'before' : 'after'} end`;
  }
  if (allDay) {
    const { daysBefore, timeS } = splitAllDayOffset(offset);
    return allDayLabel(daysBefore, timeS);
  }
  if (offset === 0) return 'At start';
  return `${durationLabel(offset)} ${offset < 0 ? 'before' : 'after start'}`;
}

/**
 * The words of one reminder chip: "15 minutes before", "1 day before at 09:00", "At 4 Nov, 09:00",
 * "15 minutes after end", "E-mail, 1 day before". `allDay` says how a start-related offset is read.
 */
export function describeReminder(reminder: Reminder, allDay: boolean): string {
  const action = ACTION_LABELS[reminder.action];
  let text: string;
  if (reminder.at) text = `At ${absoluteLabel(reminder.at)}`;
  else if (reminder.offset_seconds === null) text = 'Unreadable reminder';
  else text = relativeLabel(reminder, reminder.offset_seconds, allDay);
  return action ? `${action}, ${lowerFirst(text)}` : text;
}

const lowerFirst = (s: string) => (/^[A-Z][a-z]/.test(s) ? s.charAt(0).toLowerCase() + s.slice(1) : s);

/** Identity of a reminder for duplicate merging and change detection. */
export function reminderKey(reminder: Reminder): string {
  return reminder.at ? `a:${reminder.at}` : `o:${reminder.offset_seconds}`;
}

/** Order by trigger: relative ones by offset, then absolute ones by instant, read-only ones last (in their order). */
export function sortReminders(reminders: Reminder[]): Reminder[] {
  const rank = (r: Reminder) => (!r.editable ? 2 : r.at ? 1 : 0);
  return reminders
    .map((r, i) => ({ r, i }))
    .sort((a, b) => {
      const ra = rank(a.r);
      const rb = rank(b.r);
      if (ra !== rb) return ra - rb;
      if (ra === 0) return (a.r.offset_seconds ?? 0) - (b.r.offset_seconds ?? 0) || a.i - b.i;
      if (ra === 1) return Date.parse(a.r.at ?? '') - Date.parse(b.r.at ?? '') || a.i - b.i;
      return a.i - b.i;
    })
    .map((x) => x.r);
}

/** Merge duplicates among the editable reminders (the first stays), keep every read-only one, sort. */
export function normalizeReminders(reminders: Reminder[]): Reminder[] {
  const seen = new Set<string>();
  const kept = reminders.filter((r) => {
    if (!r.editable) return true;
    const key = reminderKey(r);
    if (seen.has(key)) return false;
    seen.add(key);
    return true;
  });
  return sortReminders(kept);
}

export const editableReminders = (reminders: Reminder[]): Reminder[] => reminders.filter((r) => r.editable);

/** The request body items: every editable reminder the form keeps, an absolute one as the `at` the server returned. */
export function toReminderInputs(reminders: Reminder[]): ReminderInput[] {
  const inputs: ReminderInput[] = [];
  for (const r of normalizeReminders(editableReminders(reminders))) {
    if (r.at) inputs.push({ at: r.at });
    else if (r.offset_seconds !== null) inputs.push({ offset_seconds: r.offset_seconds });
  }
  return inputs;
}

/**
 * Whether the editable reminders differ (as a set) from the form's starting ones. Only then is `reminders`
 * sent: a title or date edit sends none (relative reminders move with the date, the server shifts absolute
 * all-day ones itself, D23).
 */
export function remindersChanged(initial: Reminder[], current: Reminder[]): boolean {
  const a = new Set(editableReminders(initial).map(reminderKey));
  const b = new Set(editableReminders(current).map(reminderKey));
  return a.size !== b.size || [...a].some((k) => !b.has(k));
}

/**
 * The reminders after switching an event between all-day and timed: each editable relative reminder becomes the
 * other mode's preset with the same number of days before ("On the day at 09:00" <-> "At start", "N days before
 * at 09:00" <-> "N days before"). Absolute and read-only ones stay; duplicates merge.
 */
export function mapReminderMode(reminders: Reminder[], toAllDay: boolean): Reminder[] {
  const mapped = reminders.map((r) => {
    if (!r.editable || r.at || r.offset_seconds === null) return r;
    if (toAllDay) {
      // A timed offset counts whole days before the start: 15 minutes -> 0, 1 day -> 1, 1 week -> 7.
      const days = Math.max(0, Math.floor(-r.offset_seconds / DAY_S));
      return offsetReminder(allDayOffset(days, ALL_DAY_DEFAULT_TIME_S));
    }
    const { daysBefore } = splitAllDayOffset(r.offset_seconds);
    return offsetReminder(0 - Math.max(0, daysBefore) * DAY_S);
  });
  return normalizeReminders(mapped);
}
