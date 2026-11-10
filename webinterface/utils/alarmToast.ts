import type { UpcomingAlarm } from '~/types/alarms';

/** More than this many due alarms at once become one summary toast (§10.5). */
export const MAX_SINGLE_TOASTS = 3;
/** The summary toast lists this many reminders, then "and N more". */
export const SUMMARY_LIST_MAX = 5;
/** An alarm shown this long after its trigger counts as missed. */
export const MISSED_AFTER_MS = 60_000;

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
const pad2 = (n: number) => n.toString().padStart(2, '0');

const clock = (d: Date) => `${pad2(d.getHours())}:${pad2(d.getMinutes())}`;
const sameDay = (a: Date, b: Date) =>
  a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate();
const dayLabel = (d: Date) => `${d.getDate()} ${MONTHS[d.getMonth()]}`;

/** "09:00" for today, "6 Nov, 09:00" for another day (browser time zone). */
function when(d: Date, now: Date): string {
  return sameDay(d, now) ? clock(d) : `${dayLabel(d)}, ${clock(d)}`;
}

/** "today", "tomorrow" or "6 Nov" for a `YYYY-MM-DD` date. */
function dateWord(ymd: string, now: Date): string {
  const [y, m, d] = ymd.split('-').map(Number);
  if (!y || !m || !d) return ymd;
  const date = new Date(y, m - 1, d);
  if (sameDay(date, now)) return 'today';
  if (sameDay(date, new Date(now.getFullYear(), now.getMonth(), now.getDate() + 1))) return 'tomorrow';
  return dayLabel(date);
}

export const alarmTitle = (alarm: UpcomingAlarm): string => alarm.summary?.trim() || 'Untitled';

/** True when the alarm fired noticeably later than its trigger (after a sleep, a hidden tab, a reload). */
export function isMissed(alarm: UpcomingAlarm, nowMs: number): boolean {
  const trigger = Date.parse(alarm.trigger_at);
  return !Number.isNaN(trigger) && nowMs - trigger > MISSED_AFTER_MS;
}

/**
 * The detail line of one toast (§10.5): "Starts 09:00 · Room 2", "Due 10:00", "All day, today";
 * a missed one "Missed · started 09:00" plus " (ended)" when the occurrence is over.
 */
export function alarmDetail(alarm: UpcomingAlarm, nowMs: number): string {
  const now = new Date(nowMs);
  const missed = isMissed(alarm, nowMs);

  if (alarm.object_type === 'task') {
    if (alarm.due) {
      const due = new Date(alarm.due);
      return `${missed ? 'Missed · due' : 'Due'} ${when(due, now)}`;
    }
    const word = alarm.due_date ? dateWord(alarm.due_date, now) : 'today';
    return missed ? `Missed · due ${word}` : `Due ${word}`;
  }

  const parts: string[] = [];
  if (alarm.all_day) {
    const word = alarm.start_date ? dateWord(alarm.start_date, now) : 'today';
    parts.push(missed ? 'Missed · all day' : `All day, ${word}`);
  } else if (alarm.start) {
    const start = new Date(alarm.start);
    if (missed) {
      const ended = alarm.end !== null && Date.parse(alarm.end) <= nowMs;
      const verb = start.getTime() <= nowMs ? 'started' : 'starts';
      parts.push(`Missed · ${verb} ${when(start, now)}${ended ? ' (ended)' : ''}`);
    } else {
      parts.push(`Starts ${when(start, now)}`);
    }
  }
  if (!missed && alarm.location) parts.push(alarm.location);
  return parts.join(' · ');
}

/** The time of an alarm's object ("09:00"), "" for an all-day one. For the summary list. */
export function alarmClock(alarm: UpcomingAlarm): string {
  const at = alarm.object_type === 'task' ? alarm.due : alarm.start;
  return at ? clock(new Date(at)) : '';
}

/** Lines of the summary toast: up to 5 reminders, then "and N more". */
export function summaryLines(alarms: UpcomingAlarm[]): string[] {
  const lines = alarms.slice(0, SUMMARY_LIST_MAX).map((a) => {
    const c = alarmClock(a);
    return c ? `${alarmTitle(a)} · ${c}` : alarmTitle(a);
  });
  if (alarms.length > SUMMARY_LIST_MAX) lines.push(`and ${alarms.length - SUMMARY_LIST_MAX} more`);
  return lines;
}
