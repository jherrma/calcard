import { describe, it, expect } from 'vitest';
import { delayMs, mergePoll, alarmLink, MAX_TIMER_MS } from './alarmSchedule';
import type { UpcomingAlarm } from '~/types/alarms';

const NOW = Date.parse('2026-11-04T09:00:00Z');
const key = (n: number) => n.toString(16).padStart(32, '0');

function alarm(n: number, triggerAt: string, over: Partial<UpcomingAlarm> = {}): UpcomingAlarm {
  return {
    key: key(n), trigger_at: triggerAt, uid: `u${n}`, recurrence_id: null, alarm_index: 0, action: 'display',
    description: null, object_type: 'event', object_id: `obj-${n}`, calendar_id: 7, calendar_uuid: 'cal-uuid',
    calendar_name: 'Personal', calendar_color: '#3788d8', summary: `Event ${n}`, location: null, all_day: false,
    start: triggerAt, start_date: null, end: null, due: null, due_date: null, ...over,
  };
}

const local = (over: Partial<Parameters<typeof mergePoll>[0]> = {}) => ({
  scheduled: [] as string[], queued: [] as string[], blocked: [] as string[], triggers: new Map<string, number>(), ...over,
});

describe('delayMs', () => {
  it('is the time left on the server clock', () => {
    expect(delayMs('2026-11-04T09:10:00Z', NOW, 0)).toBe(600_000);
  });
  it('corrects by the skew: a server ahead of the client fires earlier', () => {
    expect(delayMs('2026-11-04T09:10:00Z', NOW, 60_000)).toBe(540_000);
    expect(delayMs('2026-11-04T09:10:00Z', NOW, -60_000)).toBe(660_000);
  });
  it('never goes below 0 and fires an unparseable time at once', () => {
    expect(delayMs('2026-11-04T08:00:00Z', NOW, 0)).toBe(0);
    expect(delayMs('nonsense', NOW, 0)).toBe(0);
  });
  it('is capped at 2^31-1', () => {
    expect(delayMs('2027-11-04T09:00:00Z', NOW, 0)).toBe(MAX_TIMER_MS);
  });
  it('accepts epoch milliseconds', () => {
    expect(delayMs(NOW + 5, NOW, 0)).toBe(5);
  });
});

describe('mergePoll', () => {
  it('schedules a new future key and queues an overdue one', () => {
    const future = alarm(1, '2026-11-04T09:10:00Z');
    const overdue = alarm(2, '2026-11-04T08:00:00Z');
    const r = mergePoll(local(), { alarms: [overdue, future], truncated: false }, NOW);
    expect(r.schedule.map((a) => a.key)).toEqual([key(1)]);
    expect(r.queue.map((a) => a.key)).toEqual([key(2)]);
    expect(r.cancel).toEqual([]);
  });

  it('a trigger exactly now counts as due', () => {
    const r = mergePoll(local(), { alarms: [alarm(1, '2026-11-04T09:00:00Z')], truncated: false }, NOW);
    expect(r.queue).toHaveLength(1);
  });

  it('arms no second timer for a key that is already armed, queued or blocked', () => {
    const a = alarm(1, '2026-11-04T09:10:00Z');
    const b = alarm(2, '2026-11-04T09:20:00Z');
    const c = alarm(3, '2026-11-04T08:00:00Z');
    const r = mergePoll(
      local({ scheduled: [a.key], queued: [b.key], blocked: [c.key], triggers: new Map([[a.key, Date.parse(a.trigger_at)]]) }),
      { alarms: [a, b, c], truncated: false },
      NOW,
    );
    expect(r.schedule).toEqual([]);
    expect(r.queue).toEqual([]);
    expect(r.cancel).toEqual([]);
  });

  it('a duplicate key inside one answer yields one timer', () => {
    const a = alarm(1, '2026-11-04T09:10:00Z');
    expect(mergePoll(local(), { alarms: [a, a], truncated: false }, NOW).schedule).toHaveLength(1);
  });

  it('cancels an armed key that vanished; keeps queued keys', () => {
    const gone = alarm(1, '2026-11-04T09:10:00Z');
    const r = mergePoll(
      local({ scheduled: [gone.key], queued: [key(9)], triggers: new Map([[gone.key, Date.parse(gone.trigger_at)]]) }),
      { alarms: [], truncated: false },
      NOW,
    );
    expect(r.cancel).toEqual([gone.key]);
  });

  it('a moved alarm arrives as a new key and the old one is cancelled', () => {
    const old = alarm(1, '2026-11-04T09:10:00Z');
    const moved = alarm(2, '2026-11-04T09:40:00Z');
    const r = mergePoll(
      local({ scheduled: [old.key], triggers: new Map([[old.key, Date.parse(old.trigger_at)]]) }),
      { alarms: [moved], truncated: false },
      NOW,
    );
    expect(r.cancel).toEqual([old.key]);
    expect(r.schedule.map((a) => a.key)).toEqual([moved.key]);
  });

  it('truncated: keys after the last returned trigger keep their timers, earlier ones are cancelled', () => {
    const last = alarm(1, '2026-11-04T10:00:00Z');
    const beyond = key(2);
    const before = key(3);
    const r = mergePoll(
      local({
        scheduled: [last.key, beyond, before],
        triggers: new Map([[last.key, Date.parse(last.trigger_at)], [beyond, Date.parse('2026-11-04T11:00:00Z')], [before, Date.parse('2026-11-04T09:30:00Z')]]),
      }),
      { alarms: [last], truncated: true },
      NOW,
    );
    expect(r.cancel).toEqual([before]);
  });

  it('not truncated: a key after the last returned trigger is cancelled too', () => {
    const last = alarm(1, '2026-11-04T10:00:00Z');
    const r = mergePoll(
      local({ scheduled: [key(2)], triggers: new Map([[key(2), Date.parse('2026-11-04T11:00:00Z')]]) }),
      { alarms: [last], truncated: false },
      NOW,
    );
    expect(r.cancel).toEqual([key(2)]);
  });
});

describe('alarmLink', () => {
  it('an event opens its day with the numeric calendar id, and the recurrence when it has one', () => {
    const a = alarm(1, '2026-11-04T09:00:00Z', { start_date: '2026-11-04', all_day: true, recurrence_id: '20261104T090000Z' });
    expect(alarmLink(a)).toBe('/calendar?date=2026-11-04&event=obj-1&cal=7&recurrence=20261104T090000Z');
  });
  it('a timed event uses the local date of its start and omits recurrence when null', () => {
    const a = alarm(1, '2026-11-04T09:00:00Z');
    const d = new Date(a.start!);
    const local = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
    expect(alarmLink(a)).toBe(`/calendar?date=${local}&event=obj-1&cal=7`);
  });
  it('a task opens its list by uuid', () => {
    expect(alarmLink(alarm(1, '2026-11-04T09:00:00Z', { object_type: 'task' }))).toBe('/tasks?list=cal-uuid');
  });
});
