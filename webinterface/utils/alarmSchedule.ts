import type { UpcomingAlarm } from '~/types/alarms';

/** `setTimeout` fires at once for a delay above this (a 32-bit signed int). */
export const MAX_TIMER_MS = 2 ** 31 - 1;

/**
 * Milliseconds until `triggerAt` on the server's clock: `max(0, trigger - (now + skew))`,
 * capped at 2^31-1. `triggerAt` is an ISO string or epoch ms; an unparseable one fires at once.
 */
export function delayMs(triggerAt: string | number, nowMs: number, skewMs: number): number {
  const trigger = typeof triggerAt === 'number' ? triggerAt : Date.parse(triggerAt);
  if (Number.isNaN(trigger)) return 0;
  return Math.min(MAX_TIMER_MS, Math.max(0, trigger - (nowMs + skewMs)));
}

export interface MergeLocal {
  /** Keys that have a timer armed. */
  scheduled: Iterable<string>;
  /** Keys already in the display queue (kept whatever the response says). */
  queued: Iterable<string>;
  /** `shown` and `pendingAck` keys: never armed or queued again. */
  blocked: Iterable<string>;
  /** trigger_at (epoch ms) of every armed key, for the truncated-tail rule. */
  triggers: ReadonlyMap<string, number>;
}

export interface MergeResult {
  /** New keys whose trigger is still ahead: arm exactly one timer each. */
  schedule: UpcomingAlarm[];
  /** New keys already due at poll time (overdue, missed): queue directly. */
  queue: UpcomingAlarm[];
  /** Armed keys the server no longer returns: cancel their timers. */
  cancel: string[];
}

/**
 * Merges one poll answer into the local state, by key (§10.3).
 *  - a returned key that is blocked, queued or already armed changes nothing (one timer per key);
 *  - a new key is scheduled, or queued when `trigger_at <= now`;
 *  - an armed key missing from the answer is cancelled (deleted, moved, acknowledged elsewhere,
 *    share revoked, out of the window), except when `truncated` and its trigger lies after the
 *    last returned one: the cut-off list simply did not reach it.
 * `nowMs` already includes the skew.
 */
export function mergePoll(
  local: MergeLocal,
  response: { alarms: UpcomingAlarm[]; truncated: boolean },
  nowMs: number,
): MergeResult {
  const scheduled = new Set(local.scheduled);
  const queued = new Set(local.queued);
  const blocked = new Set(local.blocked);
  const returned = new Set<string>();
  const result: MergeResult = { schedule: [], queue: [], cancel: [] };
  let lastTrigger = -Infinity;

  for (const alarm of response.alarms) {
    returned.add(alarm.key);
    const trigger = Date.parse(alarm.trigger_at);
    if (!Number.isNaN(trigger) && trigger > lastTrigger) lastTrigger = trigger;
    if (blocked.has(alarm.key) || queued.has(alarm.key) || scheduled.has(alarm.key)) continue;
    // Count it so a duplicate key inside one answer cannot arm a second timer.
    scheduled.add(alarm.key);
    if (Number.isNaN(trigger) || trigger <= nowMs) result.queue.push(alarm);
    else result.schedule.push(alarm);
  }

  for (const key of local.scheduled) {
    if (returned.has(key)) continue;
    const trigger = local.triggers.get(key);
    if (response.truncated && trigger !== undefined && trigger > lastTrigger) continue;
    result.cancel.push(key);
  }
  return result;
}

const pad2 = (n: number) => n.toString().padStart(2, '0');

/**
 * Where "Open" on a reminder goes (§10.5): an event to its calendar day, a task to its list.
 * `/calendar?date=<YYYY-MM-DD>&event=<object_id>&cal=<numeric calendar id>[&recurrence=<recurrence_id>]`
 * or `/tasks?list=<calendar uuid>`. The date is the occurrence's start, in the browser's zone
 * unless the occurrence is all-day (then its own date).
 */
export function alarmLink(alarm: UpcomingAlarm): string {
  if (alarm.object_type === 'task') {
    return `/tasks?list=${encodeURIComponent(alarm.calendar_uuid)}`;
  }
  let date = alarm.start_date ?? '';
  if (!date) {
    const start = new Date(alarm.start ?? alarm.trigger_at);
    date = `${start.getFullYear()}-${pad2(start.getMonth() + 1)}-${pad2(start.getDate())}`;
  }
  const params = new URLSearchParams({ date, event: alarm.object_id, cal: String(alarm.calendar_id) });
  if (alarm.recurrence_id) params.set('recurrence', alarm.recurrence_id);
  return `/calendar?${params.toString()}`;
}
