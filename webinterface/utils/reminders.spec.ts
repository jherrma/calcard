import { describe, it, expect } from 'vitest';
import type { Reminder } from '~/types/calendar';
import {
  ALL_DAY_REMINDER_PRESETS,
  TIMED_REMINDER_PRESETS,
  allDayOffset,
  customAllDayOffset,
  customTimedOffset,
  describeReminder,
  mapReminderMode,
  normalizeReminders,
  offsetReminder,
  remindersChanged,
  splitAllDayOffset,
  toReminderInputs,
} from './reminders';

// SCOPE (#169 story 17, spec §10.9): presets <-> offsets, the all-day formulas (no browser zone involved),
// absolute chips, the mode switch, the change rule and the wording of read-only reminders.

function rem(over: Partial<Reminder> = {}): Reminder {
  return { index: 0, action: 'display', related: 'start', offset_seconds: -900, at: null, editable: true, ...over };
}

describe('presets', () => {
  it('timed presets map to the offsets of the spec', () => {
    expect(TIMED_REMINDER_PRESETS.map((p) => [p.label, p.offset])).toEqual([
      ['At start', 0],
      ['5 minutes before', -300],
      ['10 minutes before', -600],
      ['15 minutes before', -900],
      ['30 minutes before', -1800],
      ['1 hour before', -3600],
      ['2 hours before', -7200],
      ['1 day before', -86400],
      ['2 days before', -172800],
      ['1 week before', -604800],
    ]);
  });

  it('all-day presets are relative to the midnight of the date (D23)', () => {
    expect(ALL_DAY_REMINDER_PRESETS.map((p) => [p.label, p.offset])).toEqual([
      ['On the day at 09:00', 32400],
      ['1 day before at 09:00', -54000],
      ['2 days before at 09:00', -140400],
      ['1 week before at 09:00', -572400],
    ]);
  });

  it('custom values', () => {
    expect(customTimedOffset(3, 'hours')).toBe(-10800);
    expect(customTimedOffset(2, 'weeks')).toBe(-1209600);
    expect(customAllDayOffset(2, 7, 30)).toBe(-145800);
    expect(customAllDayOffset(0, 9, 0)).toBe(32400);
  });
});

describe('all-day formulas', () => {
  it('round trip, with no -0', () => {
    expect(allDayOffset(1, 9 * 3600)).toBe(-54000);
    expect(splitAllDayOffset(-54000)).toEqual({ daysBefore: 1, timeS: 32400 });
    expect(splitAllDayOffset(32400)).toEqual({ daysBefore: 0, timeS: 32400 });
    expect(Object.is(splitAllDayOffset(32400).daysBefore, -0)).toBe(false);
    expect(splitAllDayOffset(-145800)).toEqual({ daysBefore: 2, timeS: 27000 });
    expect(splitAllDayOffset(90000)).toEqual({ daysBefore: -1, timeS: 3600 });
  });

  it.each(['Europe/Berlin', 'America/New_York', 'UTC'])('does not depend on process.env.TZ (%s)', (tz) => {
    const before = process.env.TZ;
    process.env.TZ = tz;
    try {
      expect(ALL_DAY_REMINDER_PRESETS.map((p) => p.offset)).toEqual([32400, -54000, -140400, -572400]);
      expect(customAllDayOffset(2, 7, 30)).toBe(-145800);
      expect(describeReminder(rem({ offset_seconds: -54000 }), true)).toBe('1 day before at 09:00');
    } finally {
      process.env.TZ = before;
    }
  });
});

describe('describeReminder', () => {
  it('timed and all-day wording of a start-related offset', () => {
    expect(describeReminder(rem({ offset_seconds: -900 }), false)).toBe('15 minutes before');
    expect(describeReminder(rem({ offset_seconds: 0 }), false)).toBe('At start');
    expect(describeReminder(rem({ offset_seconds: -10800 }), false)).toBe('3 hours before');
    expect(describeReminder(rem({ offset_seconds: -604800 }), false)).toBe('1 week before');
    expect(describeReminder(rem({ offset_seconds: 600 }), false)).toBe('10 minutes after start');
    expect(describeReminder(rem({ offset_seconds: -54000 }), true)).toBe('1 day before at 09:00');
    expect(describeReminder(rem({ offset_seconds: 32400 }), true)).toBe('On the day at 09:00');
    expect(describeReminder(rem({ offset_seconds: -145800 }), true)).toBe('2 days before at 07:30');
    expect(describeReminder(rem({ offset_seconds: 90000 }), true)).toBe('1 day after at 01:00');
  });

  it('an absolute reminder reads "At 4 Nov, 09:00" in the browser zone', () => {
    const at = new Date(2026, 10, 4, 9, 0, 0).toISOString();
    expect(describeReminder(rem({ offset_seconds: null, at }), false)).toBe('At 4 Nov, 09:00');
  });

  it('read-only kinds', () => {
    expect(describeReminder(rem({ related: 'end', offset_seconds: 900, editable: false }), false)).toBe('15 minutes after end');
    expect(describeReminder(rem({ related: 'end', offset_seconds: -900, editable: false }), true)).toBe('15 minutes before end');
    expect(describeReminder(rem({ action: 'email', offset_seconds: -86400, editable: false }), false)).toBe('E-mail, 1 day before');
    expect(describeReminder(rem({ offset_seconds: null, at: null, editable: false }), false)).toBe('Unreadable reminder');
  });
});

describe('toReminderInputs', () => {
  it('sends relative as offset_seconds and absolute as the server-returned `at`, never read-only ones', () => {
    const at = '2026-11-04T08:00:00Z';
    const list = [
      rem({ offset_seconds: -900 }),
      rem({ offset_seconds: null, at }),
      rem({ action: 'email', offset_seconds: -86400, editable: false }),
      rem({ related: 'end', offset_seconds: 60, editable: false }),
    ];
    expect(toReminderInputs(list)).toEqual([{ offset_seconds: -900 }, { at }]);
  });

  it('merges duplicates and orders by trigger', () => {
    const list = [rem({ offset_seconds: 0 }), rem({ offset_seconds: -3600 }), rem({ offset_seconds: 0, index: 3 }), rem({ offset_seconds: -300 })];
    expect(toReminderInputs(list)).toEqual([{ offset_seconds: -3600 }, { offset_seconds: -300 }, { offset_seconds: 0 }]);
    expect(normalizeReminders(list)).toHaveLength(3);
  });
});

describe('remindersChanged', () => {
  const initial = [rem({ offset_seconds: -900 }), rem({ action: 'email', editable: false, offset_seconds: -60 })];

  it('is false for the same set in another order or with an extra copy of a read-only one', () => {
    expect(remindersChanged(initial, [...initial].reverse())).toBe(false);
    expect(remindersChanged([], [])).toBe(false);
  });

  it('is true for an added, removed or replaced editable reminder', () => {
    expect(remindersChanged(initial, [...initial, offsetReminder(-300)])).toBe(true);
    expect(remindersChanged(initial, [initial[1]!])).toBe(true);
    expect(remindersChanged(initial, [offsetReminder(-600), initial[1]!])).toBe(true);
  });

  it('only looks at reminders, so a date-only change cannot make it true', () => {
    // The helper takes no dates: an unchanged list stays unchanged whatever the event's date does.
    expect(remindersChanged(initial, initial.map((r) => ({ ...r })))).toBe(false);
  });
});

describe('mapReminderMode', () => {
  it('timed -> all-day keeps the days before', () => {
    const out = mapReminderMode([offsetReminder(0), offsetReminder(-86400), offsetReminder(-900), offsetReminder(-604800)], true);
    expect(out.map((r) => r.offset_seconds)).toEqual([-572400, -54000, 32400]);
  });

  it('all-day -> timed keeps the days before', () => {
    const out = mapReminderMode([offsetReminder(32400), offsetReminder(-54000), offsetReminder(-140400)], false);
    expect(out.map((r) => r.offset_seconds)).toEqual([-172800, -86400, 0]);
  });

  it('there and back again', () => {
    const start = [offsetReminder(-86400), offsetReminder(0)];
    const back = mapReminderMode(mapReminderMode(start, true), false);
    expect(back.map((r) => r.offset_seconds)).toEqual([-86400, 0]);
  });

  it('absolute and read-only reminders stay as they are', () => {
    const abs = rem({ offset_seconds: null, at: '2026-11-04T08:00:00Z' });
    const ro = rem({ action: 'email', editable: false, offset_seconds: -60 });
    const out = mapReminderMode([abs, ro, offsetReminder(-86400)], true);
    expect(out).toContain(abs);
    expect(out).toContain(ro);
    expect(out.filter((r) => r.editable && !r.at).map((r) => r.offset_seconds)).toEqual([-54000]);
  });
});
