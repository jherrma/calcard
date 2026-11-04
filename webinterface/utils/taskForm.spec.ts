// @vitest-environment nuxt
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import type { Task, TaskFormData, TaskInput, TaskPatch } from '~/types/tasks';
import {
  TASK_PRIORITY_VALUES,
  buildTaskInput,
  buildTaskPatch,
  describeRecurrence,
  encodeTaskTime,
  isDueBeforeStart,
  isEmptyTaskPatch,
  newTaskFormData,
  summaryLength,
  taskToFormData,
} from './taskForm';

// SCOPE (#170 story 26): the task form's conversions. A task into form
// values and back into a POST body or a PATCH of the changed keys only (an
// unchanged form = an empty patch = no request); text is plain and goes back
// byte for byte; all-day values are the LOCAL date, timed ones RFC 3339 with
// the local offset plus the browser's zone; priority levels save as 1/5/9
// (DECISIONS 3); a repeating task never sends status or progress (409);
// the repeat rule as read-only words.

// Local dates and the browser zone matter: pin Europe/Berlin and prove it took.
let previousTz: string | undefined;
beforeAll(() => {
  previousTz = process.env.TZ;
  process.env.TZ = 'Europe/Berlin';
});
afterAll(() => {
  if (previousTz === undefined) delete process.env.TZ;
  else process.env.TZ = previousTz;
});

/** Local wall-clock time; `month` is 1-based. Built inside tests, after the zone is set. */
function local(year: number, month: number, day: number, hour = 0, minute = 0, second = 0): Date {
  return new Date(year, month - 1, day, hour, minute, second);
}

function task(overrides: Partial<Task> = {}): Task {
  return {
    id: 'task-1',
    calendar_id: 1,
    calendar_uuid: 'errands',
    uid: 'task-1@calcard.io',
    summary: 'Renew passport',
    description: '',
    status: 'needs-action',
    completed: false,
    completed_at: null,
    percent_complete: null,
    priority: null,
    due: null,
    due_date: null,
    due_all_day: false,
    start: null,
    start_date: null,
    is_recurring: false,
    rrule: null,
    etag: '"1"',
    created_at: '2026-10-01T08:00:00Z',
    updated_at: '2026-10-01T08:00:00Z',
    ...overrides,
  };
}

/** Text the server answers unescaped (decision 9): commas, semicolons, backslashes, newlines. */
const TRICKY = 'Milk, eggs; bread \\ butter\nsecond line\\n not a newline';

it('runs in Europe/Berlin (the zone the dates below assume)', () => {
  expect(local(2026, 11, 5).getTimezoneOffset()).toBe(-60);
  expect(local(2026, 7, 5).getTimezoneOffset()).toBe(-120);
  expect(Intl.DateTimeFormat().resolvedOptions().timeZone).toBe('Europe/Berlin');
});

describe('taskToFormData', () => {
  it('reads a timed task: the due instant, the start instant, level, progress, status, list', () => {
    const data = taskToFormData(task({
      due: '2026-11-05T16:00:00Z',
      start: '2026-11-04T08:30:00Z',
      priority: 3,
      percent_complete: 40,
      status: 'in-process',
      description: TRICKY,
    }));

    expect(data).toEqual({
      summary: 'Renew passport',
      description: TRICKY,
      listId: 'errands',
      allDay: false,
      due: new Date('2026-11-05T16:00:00Z'),
      start: new Date('2026-11-04T08:30:00Z'),
      priority: 'high',
      percent: 40,
      status: 'in-process',
    });
  });

  it('reads an all-day task\'s dates as LOCAL midnight of that date (never UTC midnight)', () => {
    const data = taskToFormData(task({ due_all_day: true, due_date: '2026-11-05', start_date: '2026-11-01' }));

    expect(data.allDay).toBe(true);
    expect(data.due).toEqual(local(2026, 11, 5));
    expect(data.start).toEqual(local(2026, 11, 1));
  });

  it.each([
    [1, 'high'], [4, 'high'], [5, 'medium'], [6, 'low'], [9, 'low'], [0, 'none'], [null, 'none'],
  ] as const)('shows priority %s as %s', (priority, level) => {
    expect(taskToFormData(task({ priority })).priority).toBe(level);
  });

  it('a task without dates has none', () => {
    expect(taskToFormData(task())).toMatchObject({ due: null, start: null, allDay: false, percent: null });
  });
});

describe('newTaskFormData', () => {
  it('starts all day, undated, no priority or progress, needs action, in the given list', () => {
    expect(newTaskFormData('errands', 'Buy milk')).toEqual({
      summary: 'Buy milk',
      description: '',
      listId: 'errands',
      allDay: true,
      due: null,
      start: null,
      priority: 'none',
      percent: null,
      status: 'needs-action',
    });
    expect(newTaskFormData('errands').summary).toBe('');
  });
});

describe('encodeTaskTime and isDueBeforeStart', () => {
  it('an all-day value is the LOCAL date, also just after midnight when the UTC date is still yesterday', () => {
    const justAfterMidnight = local(2026, 11, 6, 0, 30); // 2026-11-05T23:30Z

    expect(encodeTaskTime(justAfterMidnight, true)).toBe('2026-11-06');
    expect(justAfterMidnight.toISOString().slice(0, 10)).toBe('2026-11-05');
  });

  it('a timed value is RFC 3339 with the local offset, winter and summer', () => {
    expect(encodeTaskTime(local(2026, 11, 5, 17, 0), false)).toBe('2026-11-05T17:00:00+01:00');
    expect(encodeTaskTime(local(2026, 7, 5, 9, 15), false)).toBe('2026-07-05T09:15:00+02:00');
  });

  it('none is null (also an invalid date)', () => {
    expect(encodeTaskTime(null, true)).toBeNull();
    expect(encodeTaskTime(new Date(Number.NaN), false)).toBeNull();
  });

  it('due before start: dates by day, times by instant; equal and missing are fine', () => {
    expect(isDueBeforeStart(local(2026, 11, 4), local(2026, 11, 5), true)).toBe(true);
    // Same day, earlier clock time: still the same date when all-day.
    expect(isDueBeforeStart(local(2026, 11, 5, 8), local(2026, 11, 5, 17), true)).toBe(false);
    expect(isDueBeforeStart(local(2026, 11, 5, 8), local(2026, 11, 5, 17), false)).toBe(true);
    expect(isDueBeforeStart(local(2026, 11, 5, 17), local(2026, 11, 5, 17), false)).toBe(false);
    expect(isDueBeforeStart(local(2026, 11, 5, 18), local(2026, 11, 5, 17), false)).toBe(false);
    expect(isDueBeforeStart(null, local(2026, 11, 5), true)).toBe(false);
    expect(isDueBeforeStart(local(2026, 11, 5), null, false)).toBe(false);
  });

  it('counts a title like the server: code points after trimming', () => {
    expect(summaryLength('  abc  ')).toBe(3);
    expect(summaryLength('🎉🎉')).toBe(2);
    expect('🎉🎉'.length).toBe(4);
  });
});

describe('buildTaskInput', () => {
  it('sends only the title (trimmed) and the all-day flag for a bare task', () => {
    expect(buildTaskInput(newTaskFormData('errands', '  Buy milk  '))).toEqual({ summary: 'Buy milk', due_all_day: true });
  });

  it('an all-day task: local dates, no timezone', () => {
    const input = buildTaskInput({
      ...newTaskFormData('errands', 'Renew passport'),
      due: local(2026, 11, 6, 0, 30),
      start: local(2026, 11, 1),
    });

    expect(input).toEqual({ summary: 'Renew passport', due_all_day: true, due: '2026-11-06', start: '2026-11-01' });
  });

  it('a timed task: RFC 3339 with the offset, plus the browser zone for the TZID', () => {
    const input = buildTaskInput({
      ...newTaskFormData('errands', 'Call'),
      allDay: false,
      due: local(2026, 11, 5, 17),
    });

    expect(input).toEqual({
      summary: 'Call',
      due_all_day: false,
      due: '2026-11-05T17:00:00+01:00',
      timezone: 'Europe/Berlin',
    } satisfies TaskInput);
  });

  it('no timezone for a timed form without any date', () => {
    expect(buildTaskInput({ ...newTaskFormData('errands', 'Call'), allDay: false })).not.toHaveProperty('timezone');
  });

  it.each([
    ['high', 1], ['medium', 5], ['low', 9],
  ] as const)('saves priority %s as %s', (priority, value) => {
    expect(buildTaskInput({ ...newTaskFormData('errands', 'x'), priority }).priority).toBe(value);
  });

  it('sends notes exactly as typed (plain text: , ; \\ and newlines untouched)', () => {
    expect(buildTaskInput({ ...newTaskFormData('errands', 'x'), description: TRICKY }).description).toBe(TRICKY);
  });

  it('sends progress 0 when set, a status other than needs-action, and leaves the defaults out', () => {
    const input = buildTaskInput({ ...newTaskFormData('errands', 'x'), percent: 0, status: 'in-process' });

    expect(input).toEqual({ summary: 'x', due_all_day: true, percent_complete: 0, status: 'in-process' });
    expect(buildTaskInput(newTaskFormData('errands', 'x'))).not.toHaveProperty('status');
    expect(buildTaskInput(newTaskFormData('errands', 'x'))).not.toHaveProperty('priority');
    expect(buildTaskInput(newTaskFormData('errands', 'x'))).not.toHaveProperty('percent_complete');
    expect(buildTaskInput(newTaskFormData('errands', 'x'))).not.toHaveProperty('description');
  });

  it('the priority table is DECISIONS 3', () => {
    expect(TASK_PRIORITY_VALUES).toEqual({ none: null, low: 9, medium: 5, high: 1 });
  });
});

describe('buildTaskPatch', () => {
  const timed = () => task({
    summary: TRICKY.split('\n')[0]!,
    description: TRICKY,
    due: '2026-11-05T16:00:00Z',
    start: '2026-11-04T08:30:45Z',
    priority: 3,
    percent_complete: 40,
    status: 'in-process',
  });

  function edit(t: Task, change: (data: TaskFormData) => void, recurring = t.is_recurring): TaskPatch {
    const initial = taskToFormData(t);
    const data = taskToFormData(t);
    change(data);
    return buildTaskPatch(initial, data, recurring);
  }

  it('an unchanged form is an empty patch: nothing is sent (text with , ; \\ and newlines included)', () => {
    const patch = edit(timed(), () => {});

    expect(patch).toEqual({});
    expect(isEmptyTaskPatch(patch)).toBe(true);
    expect(edit(task({ due_all_day: true, due_date: '2026-11-05' }), () => {})).toEqual({});
  });

  it('white space added around the title is no change (the server trims)', () => {
    expect(edit(timed(), (d) => { d.summary = `  ${d.summary} `; })).toEqual({});
  });

  it('a new title is sent trimmed', () => {
    expect(edit(timed(), (d) => { d.summary = ' Renew it '; })).toEqual({ summary: 'Renew it' });
  });

  it('new notes are sent exactly as typed; emptied notes are removed with null', () => {
    expect(edit(task(), (d) => { d.description = TRICKY; })).toEqual({ description: TRICKY });
    expect(edit(timed(), (d) => { d.description = ''; })).toEqual({ description: null });
  });

  it('a new due sends due AND start (the unchanged start as it was), with the zone, without due_all_day', () => {
    const patch = edit(timed(), (d) => { d.due = local(2026, 11, 6, 18); });

    expect(patch).toEqual({
      due: '2026-11-06T18:00:00+01:00',
      // Seconds kept: the server compares the instant and keeps its stored DTSTART.
      start: '2026-11-04T09:30:45+01:00',
      timezone: 'Europe/Berlin',
    });
  });

  it('removing the due sends null (and the start as it was)', () => {
    expect(edit(timed(), (d) => { d.due = null; })).toEqual({
      due: null,
      start: '2026-11-04T09:30:45+01:00',
      timezone: 'Europe/Berlin',
    });
  });

  it('switching to all day sends the flag and both values as local dates', () => {
    expect(edit(timed(), (d) => { d.allDay = true; })).toEqual({
      due_all_day: true,
      due: '2026-11-05',
      start: '2026-11-04',
    });
  });

  it('switching an undated task to all day sends the flag with no values', () => {
    expect(edit(task(), (d) => { d.allDay = true; })).toEqual({ due_all_day: true, due: null, start: null });
  });

  it('a new all-day due date sends dates and no zone', () => {
    const t = task({ due_all_day: true, due_date: '2026-11-05' });

    expect(edit(t, (d) => { d.due = local(2026, 11, 7); })).toEqual({ due: '2026-11-07', start: null });
  });

  it('priority: only a changed LEVEL is sent (a client\'s 3 stays 3), as 1/5/9 or null', () => {
    expect(edit(timed(), (d) => { d.priority = 'high'; })).toEqual({});
    expect(edit(timed(), (d) => { d.priority = 'low'; })).toEqual({ priority: 9 });
    expect(edit(timed(), (d) => { d.priority = 'medium'; })).toEqual({ priority: 5 });
    expect(edit(timed(), (d) => { d.priority = 'none'; })).toEqual({ priority: null });
    expect(edit(task(), (d) => { d.priority = 'high'; })).toEqual({ priority: 1 });
  });

  it('progress and status changes are sent; cleared progress is null', () => {
    expect(edit(timed(), (d) => { d.percent = 60; })).toEqual({ percent_complete: 60 });
    expect(edit(timed(), (d) => { d.percent = null; })).toEqual({ percent_complete: null });
    expect(edit(timed(), (d) => { d.status = 'completed'; })).toEqual({ status: 'completed' });
  });

  it('a repeating task never sends status or progress, but its other edits go out', () => {
    const t = task({ is_recurring: true, rrule: 'FREQ=WEEKLY', status: 'completed', percent_complete: 100 });

    // Its stored completed/100 would be a 409 even unchanged: never sent.
    expect(edit(t, () => {})).toEqual({});
    expect(edit(t, (d) => { d.status = 'needs-action'; d.percent = 10; })).toEqual({});
    expect(edit(t, (d) => {
      d.status = 'cancelled';
      d.percent = 100;
      d.summary = 'Water plants';
      d.due = local(2026, 11, 9);
      d.allDay = true;
    })).toEqual({ summary: 'Water plants', due_all_day: true, due: '2026-11-09', start: null });
  });
});

describe('describeRecurrence', () => {
  it.each([
    ['FREQ=DAILY', 'Daily'],
    ['FREQ=WEEKLY', 'Weekly'],
    ['FREQ=MONTHLY', 'Monthly'],
    ['FREQ=YEARLY', 'Yearly'],
    ['RRULE:FREQ=YEARLY', 'Yearly'],
    ['FREQ=WEEKLY;BYDAY=MO,WE', 'Weekly on Mon, Wed'],
    ['FREQ=WEEKLY;INTERVAL=2;BYDAY=FR;WKST=MO', 'Every 2 weeks on Fri'],
    ['FREQ=DAILY;INTERVAL=3', 'Every 3 days'],
    ['FREQ=DAILY;COUNT=10', 'Daily, 10 times'],
    ['FREQ=DAILY;COUNT=1', 'Daily, once'],
    ['freq=weekly;byday=su', 'Weekly on Sun'],
  ])('%s -> %s', (rule, text) => {
    expect(describeRecurrence(rule)).toBe(text);
  });

  it('names the end date (a date, a UTC date-time)', () => {
    expect(describeRecurrence('FREQ=MONTHLY;UNTIL=20261231', 'en-GB')).toBe('Monthly until 31 Dec 2026');
    expect(describeRecurrence('FREQ=WEEKLY;UNTIL=20270105T225959Z', 'en-GB')).toBe('Weekly until 5 Jan 2027');
  });

  it('shows a rule it cannot put into words as written', () => {
    for (const rule of [
      'FREQ=MONTHLY;BYDAY=1MO',
      'FREQ=MONTHLY;BYMONTHDAY=15',
      'FREQ=HOURLY',
      'FREQ=DAILY;INTERVAL=0',
      'FREQ=DAILY;COUNT=x',
      'FREQ=DAILY;UNTIL=soon',
      'nonsense',
    ]) {
      expect(describeRecurrence(rule)).toBe(rule);
    }
  });

  it('a task repeating by RDATE alone (no rule) says so', () => {
    expect(describeRecurrence(null)).toBe('On set dates');
    expect(describeRecurrence('  ')).toBe('On set dates');
  });
});
