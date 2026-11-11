import { describe, expect, it } from 'vitest';
import { alarmDetail, alarmTitle, isMissed, summaryLines } from './alarmToast';
import type { UpcomingAlarm } from '~/types/alarms';

// SCOPE (#169 story 16, spec §10.5): the wording of the toasts. Times are local ("09:00"), so the
// specs build every instant from local components and do not depend on the runner's zone.

const at = (h: number, m = 0, day = 5) => new Date(2026, 10, day, h, m, 0).getTime();
const iso = (ms: number) => new Date(ms).toISOString();

function alarm(over: Partial<UpcomingAlarm> = {}): UpcomingAlarm {
  const start = at(9);
  return {
    key: 'k'.repeat(32), trigger_at: iso(start - 15 * 60_000), uid: 'u', recurrence_id: null, alarm_index: 0,
    action: 'display', description: null, object_type: 'event', object_id: 'o', calendar_id: 1, calendar_uuid: 'c',
    calendar_name: 'P', calendar_color: '#000', summary: 'Team sync', location: null, all_day: false,
    start: iso(start), start_date: null, end: iso(at(10)), due: null, due_date: null, ...over,
  };
}

describe('alarmTitle', () => {
  it('is the summary, or "Untitled"', () => {
    expect(alarmTitle(alarm())).toBe('Team sync');
    expect(alarmTitle(alarm({ summary: '  ' }))).toBe('Untitled');
    expect(alarmTitle(alarm({ summary: '' }))).toBe('Untitled');
  });
});

describe('alarmDetail', () => {
  const now = at(8, 45);

  it('event: "Starts 09:00 · Room 2"', () => {
    expect(alarmDetail(alarm({ location: 'Room 2' }), now)).toBe('Starts 09:00 · Room 2');
    expect(alarmDetail(alarm(), now)).toBe('Starts 09:00');
  });

  it('event on another day names the day', () => {
    expect(alarmDetail(alarm({ start: iso(at(9, 0, 6)) }), now)).toBe('Starts 6 Nov, 09:00');
  });

  it('task: "Due 10:00"', () => {
    const task = alarm({ object_type: 'task', start: null, end: null, due: iso(at(10)) });
    expect(alarmDetail(task, now)).toBe('Due 10:00');
  });

  it('all-day event: "All day, today"', () => {
    const a = alarm({ all_day: true, start: null, end: null, start_date: '2026-11-05' });
    expect(alarmDetail(a, now)).toBe('All day, today');
    expect(alarmDetail({ ...a, start_date: '2026-11-06' }, now)).toBe('All day, tomorrow');
  });

  it('all-day task: "Due today"', () => {
    const t = alarm({ object_type: 'task', all_day: true, start: null, end: null, due: null, due_date: '2026-11-05' });
    expect(alarmDetail(t, now)).toBe('Due today');
  });

  it('missed: "Missed · started 09:00", plus "(ended)" once the occurrence is over', () => {
    const later = at(9, 30);
    expect(alarmDetail(alarm(), later)).toBe('Missed · started 09:00');
    expect(alarmDetail(alarm(), at(10, 30))).toBe('Missed · started 09:00 (ended)');
  });

  it('missed before the start says "starts"', () => {
    const early = alarm({ trigger_at: iso(at(8, 0)) });
    expect(alarmDetail(early, at(8, 30))).toBe('Missed · starts 09:00');
  });

  it('missed task', () => {
    const t = alarm({ object_type: 'task', start: null, end: null, due: iso(at(10)), trigger_at: iso(at(9)) });
    expect(alarmDetail(t, at(11))).toBe('Missed · due 10:00');
  });
});

describe('isMissed', () => {
  it('is true only once the trigger lies more than a minute back', () => {
    const a = alarm();
    const trigger = Date.parse(a.trigger_at);
    expect(isMissed(a, trigger + 59_000)).toBe(false);
    expect(isMissed(a, trigger + 61_000)).toBe(true);
  });
});

describe('summaryLines', () => {
  const many = (n: number) => Array.from({ length: n }, (_, i) => alarm({ key: String(i), summary: `E${i}` }));

  it('lists up to 5 and then "and N more"', () => {
    expect(summaryLines(many(4))).toEqual(['E0 · 09:00', 'E1 · 09:00', 'E2 · 09:00', 'E3 · 09:00']);
    const lines = summaryLines(many(8));
    expect(lines).toHaveLength(6);
    expect(lines[4]).toBe('E4 · 09:00');
    expect(lines[5]).toBe('and 3 more');
  });

  it('exactly 5 has no "and more" line', () => {
    expect(summaryLines(many(5))).toHaveLength(5);
  });
});
