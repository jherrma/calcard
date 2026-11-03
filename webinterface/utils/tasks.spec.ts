// @vitest-environment nuxt
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import {
  TASK_GROUP_ORDER,
  type DueBucket,
  type Task,
  type TaskInput,
  type TaskListResponse,
  type TaskPatch,
  type TaskStatus,
  type TasksSummary,
} from '~/types/tasks';
import { compareTasks, dueBucket, formatDue, isOverdue, isTaskDone, priorityLevel } from './tasks';

// The helpers work in the browser's LOCAL timezone, so the specs pin one: each
// describe below sets `process.env.TZ` (Node re-reads it on assignment) and
// asserts the zone really took effect, so a runner that ignored it fails here
// instead of silently testing nothing. Local dates are therefore built INSIDE
// the tests (`local()`), after the zone is set; due instants are UTC strings.

function useTimeZone(zone: string) {
  let previous: string | undefined;
  beforeAll(() => {
    previous = process.env.TZ;
    process.env.TZ = zone;
  });
  afterAll(() => {
    if (previous === undefined) delete process.env.TZ;
    else process.env.TZ = previous;
  });
}

/** Local wall-clock time; `month` is 1-based. */
function local(year: number, month: number, day: number, hour = 0, minute = 0, second = 0, ms = 0): Date {
  return new Date(year, month - 1, day, hour, minute, second, ms);
}

let seq = 0;
function task(overrides: Partial<Task> = {}): Task {
  seq += 1;
  return {
    id: `task-${String(seq).padStart(4, '0')}`,
    calendar_id: 1,
    calendar_uuid: 'list-uuid',
    uid: `task-${seq}@calcard.io`,
    summary: `Task ${seq}`,
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
    etag: `etag-${seq}`,
    created_at: '2026-11-01T10:00:00Z',
    updated_at: '2026-11-01T10:00:00Z',
    ...overrides,
  };
}

const timed = (due: string, overrides: Partial<Task> = {}) => task({ due, ...overrides });
const allDay = (dueDate: string, overrides: Partial<Task> = {}) =>
  task({ due_date: dueDate, due_all_day: true, ...overrides });

const sortTitles = (tasks: Task[], ...args: Parameters<typeof compareTasks>) =>
  [...tasks].sort(compareTasks(...args)).map((t) => t.summary);

describe('wire shapes', () => {
  // Copied from the Go DTO tests (server/internal/adapter/http/dto/task_dto_test.go,
  // TestNewTaskResponse "timed task" and TestNewTaskSummaryResponse). `satisfies`
  // makes vue-tsc refuse a key the type lacks and a required key the wire lacks,
  // so `pnpm nuxt typecheck` keeps the types in step with the JSON.
  it('types the server JSON exactly', () => {
    const timedTask = {
      id: '6f1c-uuid', calendar_id: 12, calendar_uuid: '9a0e-uuid', uid: '6f1c@calcard.io',
      summary: 'Renew passport', description: 'Bring two photos',
      status: 'in-process', completed: false, completed_at: null,
      percent_complete: 40, priority: 3,
      due: '2026-11-05T16:00:00Z', due_date: null, due_all_day: false,
      start: '2026-11-04T07:00:00Z', start_date: null,
      is_recurring: false, rrule: null, etag: '1735-ab12',
      created_at: '2026-11-03T08:30:00Z', updated_at: '2026-11-03T08:30:00Z',
    } satisfies Task;

    // GET /api/v1/tasks items: the same plus the list's name and colour.
    const page = {
      tasks: [{ ...timedTask, calendar_name: 'Errands', calendar_color: '#28a745' }],
      count: 1,
      total: 7,
    } satisfies TaskListResponse;

    const summary = {
      open: 14, overdue: 3, due_today: 2, completed: 41,
      by_calendar: {
        '9a0e0000-0000-4000-8000-000000000001': { open: 5, overdue: 1 },
        '9a0e0000-0000-4000-8000-000000000003': { open: 0, overdue: 0 },
      },
    } satisfies TasksSummary;

    // The spec's POST body and a PATCH that clears two fields.
    const input = {
      summary: 'Renew passport', description: '', due: '2026-11-05T17:00:00+01:00',
      due_all_day: false, start: null, priority: 3, percent_complete: 0,
      status: 'needs-action', timezone: 'Europe/Berlin',
    } satisfies TaskInput;
    const patch = { description: null, priority: null, status: 'completed' } satisfies TaskPatch;

    expect(Object.keys(timedTask)).toHaveLength(21);
    expect(page.tasks[0]!.calendar_name).toBe('Errands');
    expect(summary.by_calendar['9a0e0000-0000-4000-8000-000000000003']).toEqual({ open: 0, overdue: 0 });
    expect(input.summary).toBe('Renew passport');
    expect(patch.status).toBe('completed');
  });

  it('has one group per status, in display order', () => {
    // A Record over TaskStatus fails to compile if a status is added without a group.
    const covered: Record<TaskStatus, true> = {
      'needs-action': true, 'in-process': true, completed: true, cancelled: true,
    };
    expect(TASK_GROUP_ORDER).toEqual(['needs-action', 'in-process', 'completed', 'cancelled']);
    expect([...TASK_GROUP_ORDER].sort()).toEqual(Object.keys(covered).sort());
  });
});

describe('priorityLevel', () => {
  it.each([
    [1, 'high'], [2, 'high'], [3, 'high'], [4, 'high'],
    [5, 'medium'],
    [6, 'low'], [7, 'low'], [8, 'low'], [9, 'low'],
    [0, 'none'], [null, 'none'], [undefined, 'none'],
    [10, 'none'], [-1, 'none'], [1.5, 'none'], [4.5, 'none'], [6.5, 'none'], [Number.NaN, 'none'],
  ] as const)('%s -> %s', (value, level) => {
    expect(priorityLevel(value)).toBe(level);
  });
});

describe('isTaskDone', () => {
  it('is completed or cancelled', () => {
    expect(isTaskDone({ status: 'completed' })).toBe(true);
    expect(isTaskDone({ status: 'cancelled' })).toBe(true);
    expect(isTaskDone({ status: 'needs-action' })).toBe(false);
    expect(isTaskDone({ status: 'in-process' })).toBe(false);
  });
});

describe('in Europe/Berlin (DST zone, east of UTC)', () => {
  useTimeZone('Europe/Berlin');

  it('runs in the zone it asked for', () => {
    // CET before the spring switch on 29 March 2026, CEST after it.
    expect(local(2026, 3, 28, 12).getTimezoneOffset()).toBe(-60);
    expect(local(2026, 3, 29, 12).getTimezoneOffset()).toBe(-120);
    expect(local(2026, 10, 25, 12).getTimezoneOffset()).toBe(-60);
  });

  describe('isOverdue: all-day vs timed', () => {
    it('an all-day task is overdue only once its date has passed, never during it', () => {
      const t = allDay('2026-11-05');
      expect(isOverdue(t, local(2026, 11, 5, 0, 0))).toBe(false);
      expect(isOverdue(t, local(2026, 11, 5, 23, 59, 59, 999))).toBe(false);
      expect(isOverdue(t, local(2026, 11, 6, 0, 0))).toBe(true);
      expect(isOverdue(t, local(2026, 11, 4, 12))).toBe(false);
    });

    it('a timed task is overdue one moment after its due instant, not at it', () => {
      const t = timed('2026-11-05T16:00:00Z'); // 17:00 in Berlin
      const due = Date.parse('2026-11-05T16:00:00Z');
      expect(isOverdue(t, new Date(due - 1))).toBe(false);
      expect(isOverdue(t, new Date(due))).toBe(false);
      expect(isOverdue(t, new Date(due + 1))).toBe(true);
    });

    it('compares a timed task by instant even when the due date is today', () => {
      const t = timed('2026-11-05T08:00:00Z'); // 09:00 local, today
      expect(isOverdue(t, local(2026, 11, 5, 10))).toBe(true);
      // An all-day task due the same day is not overdue at that moment.
      expect(isOverdue(allDay('2026-11-05'), local(2026, 11, 5, 10))).toBe(false);
    });

    it('reads the date in local time just after midnight, when UTC is still on yesterday', () => {
      const now = local(2026, 11, 6, 0, 10); // 2026-11-05T23:10Z
      expect(now.toISOString().slice(0, 10)).toBe('2026-11-05');
      expect(isOverdue(allDay('2026-11-05'), now)).toBe(true);
      expect(isOverdue(allDay('2026-11-06'), now)).toBe(false);
    });

    it('an open in-process task can be overdue; a done task never is', () => {
      const now = local(2026, 11, 10, 12);
      for (const shape of [allDay('2026-11-01'), timed('2026-11-01T08:00:00Z')]) {
        expect(isOverdue({ ...shape, status: 'needs-action' }, now)).toBe(true);
        expect(isOverdue({ ...shape, status: 'in-process' }, now)).toBe(true);
        const completed: Task = { ...shape, status: 'completed', completed: true };
        expect(isOverdue(completed, now)).toBe(false);
        expect(isOverdue({ ...shape, status: 'cancelled' }, now)).toBe(false);
      }
    });

    it('a task without a (readable) due date is never overdue', () => {
      const now = local(2026, 11, 10, 12);
      expect(isOverdue(task(), now)).toBe(false);
      // all-day flag from a date-only DTSTART, but no due date
      expect(isOverdue(task({ due_all_day: true, start_date: '2026-11-01' }), now)).toBe(false);
      expect(isOverdue(allDay('2026-02-30'), now)).toBe(false);
      expect(isOverdue(allDay('5 Nov 2026'), now)).toBe(false);
      expect(isOverdue(timed('not a date'), now)).toBe(false);
    });

    it('an all-day date wins over a stray due instant', () => {
      const t = task({ due_date: '2026-11-20', due_all_day: true, due: '2026-11-01T00:00:00Z' });
      expect(isOverdue(t, local(2026, 11, 10, 12))).toBe(false);
    });
  });

  describe('dueBucket around midnight', () => {
    it('one minute before midnight', () => {
      const now = local(2026, 11, 5, 23, 59);
      expect(dueBucket(timed('2026-11-05T22:58:00Z'), now)).toBe('overdue'); // 23:58 local
      expect(dueBucket(timed('2026-11-05T22:59:30Z'), now)).toBe('today'); // 23:59:30 local
      expect(dueBucket(timed('2026-11-05T23:00:00Z'), now)).toBe('later'); // 00:00 tomorrow
      expect(dueBucket(allDay('2026-11-04'), now)).toBe('overdue');
      expect(dueBucket(allDay('2026-11-05'), now)).toBe('today');
      expect(dueBucket(allDay('2026-11-06'), now)).toBe('later');
      expect(dueBucket(task(), now)).toBe('none');
    });

    it('at midnight the day rolls over', () => {
      const now = local(2026, 11, 6, 0, 0);
      expect(dueBucket(allDay('2026-11-05'), now)).toBe('overdue');
      expect(dueBucket(allDay('2026-11-06'), now)).toBe('today');
      expect(dueBucket(allDay('2026-11-07'), now)).toBe('later');
      // Due exactly now: not overdue yet, so today.
      expect(dueBucket(timed('2026-11-05T23:00:00Z'), now)).toBe('today');
      expect(dueBucket(timed('2026-11-06T22:59:59Z'), now)).toBe('today'); // 23:59:59 local
      expect(dueBucket(timed('2026-11-06T23:00:00Z'), now)).toBe('later');
    });

    it('uses the local date, not the UTC date', () => {
      const now = local(2026, 11, 6, 0, 10); // UTC: 5 Nov 23:10
      expect(dueBucket(timed('2026-11-06T08:00:00Z'), now)).toBe('today'); // UTC date 6 Nov
      expect(dueBucket(timed('2026-11-05T22:50:00Z', { status: 'completed', completed: true }), now)).toBe('later'); // 23:50 yesterday
    });

    it('a done task is never overdue: today on its date, later otherwise', () => {
      const now = local(2026, 11, 5, 12);
      const done = { status: 'completed', completed: true } as const;
      expect(dueBucket(timed('2026-11-05T08:00:00Z', done), now)).toBe('today');
      expect(dueBucket(allDay('2026-11-05', done), now)).toBe('today');
      expect(dueBucket(allDay('2026-11-01', done), now)).toBe('later');
      expect(dueBucket(timed('2026-11-01T08:00:00Z', { status: 'cancelled' }), now)).toBe('later');
      expect(dueBucket(task(done), now)).toBe('none');
    });

    it('agrees with isOverdue for every task', () => {
      const now = local(2026, 11, 5, 23, 59);
      const shapes = [
        timed('2026-11-05T22:58:00Z'), timed('2026-11-05T23:00:00Z'), allDay('2026-11-04'),
        allDay('2026-11-05'), task(), timed('2026-11-01T08:00:00Z', { status: 'cancelled' }),
      ];
      for (const t of shapes) expect(dueBucket(t, now) === 'overdue').toBe(isOverdue(t, now));
    });
  });

  describe('dueBucket across a DST change', () => {
    it('spring forward: the 23-hour day ends at local midnight, not 24 hours after the last one', () => {
      // 29 March 2026: 02:00 CET jumps to 03:00 CEST. Local midnight that night
      // is 2026-03-29T22:00Z; "previous midnight + 24h" would be 23:00Z.
      const now = local(2026, 3, 29, 23, 30); // 21:30Z
      expect(dueBucket(timed('2026-03-29T21:45:00Z'), now)).toBe('today'); // 23:45 CEST
      expect(dueBucket(timed('2026-03-29T22:00:00Z'), now)).toBe('later'); // 00:00 on the 30th
      expect(dueBucket(timed('2026-03-29T22:30:00Z'), now)).toBe('later'); // 00:30 on the 30th
      expect(dueBucket(allDay('2026-03-29'), now)).toBe('today');
      expect(dueBucket(allDay('2026-03-30'), now)).toBe('later');
    });

    it('spring forward: before and after the skipped hour are the same day', () => {
      const now = local(2026, 3, 29, 1, 59); // 00:59Z, still CET
      expect(dueBucket(timed('2026-03-29T01:00:00Z'), now)).toBe('today'); // 03:00 CEST
      expect(dueBucket(timed('2026-03-28T23:00:00Z'), now)).toBe('overdue'); // 00:00 CET today
      expect(dueBucket(allDay('2026-03-28'), now)).toBe('overdue');
    });

    it('fall back: the 25-hour day still ends at local midnight', () => {
      // 25 October 2026: 03:00 CEST falls back to 02:00 CET. Local midnight
      // that night is 2026-10-25T23:00Z; "previous midnight + 24h" would be 22:00Z.
      const now = local(2026, 10, 25, 22, 30); // 21:30Z
      expect(dueBucket(timed('2026-10-25T22:45:00Z'), now)).toBe('today'); // 23:45 CET
      expect(dueBucket(timed('2026-10-25T23:00:00Z'), now)).toBe('later'); // 00:00 on the 26th
      expect(dueBucket(timed('2026-10-25T21:00:00Z'), now)).toBe('overdue'); // 22:00 CET
    });

    it('fall back: an all-day task due that day turns overdue at the next local midnight', () => {
      const t = allDay('2026-10-25');
      expect(dueBucket(t, local(2026, 10, 25, 23, 59, 59))).toBe('today');
      expect(dueBucket(t, local(2026, 10, 26, 0, 0))).toBe('overdue');
      expect(isOverdue(allDay('2026-03-29'), local(2026, 3, 29, 23, 59))).toBe(false);
      expect(isOverdue(allDay('2026-03-29'), local(2026, 3, 30, 0, 0))).toBe(true);
    });
  });

  describe('compareTasks', () => {
    it('due: earliest first, undated last, in both directions', () => {
      const tasks = [
        task({ summary: 'undated' }),
        timed('2026-11-07T09:00:00Z', { summary: 'sat' }),
        allDay('2026-11-05', { summary: 'thu all day' }),
        timed('2026-11-04T09:00:00Z', { summary: 'wed' }),
        task({ summary: 'undated too', priority: 1 }),
      ];
      expect(sortTitles(tasks, 'due', 'asc')).toEqual(['wed', 'thu all day', 'sat', 'undated too', 'undated']);
      expect(sortTitles(tasks, 'due', 'desc')).toEqual(['sat', 'thu all day', 'wed', 'undated too', 'undated']);
      expect(sortTitles(tasks, 'due')).toEqual(sortTitles(tasks, 'due', 'asc'));
    });

    it('due: ties by priority (by level, none last), then title', () => {
      const due = '2026-11-05T09:00:00Z';
      const tasks = [
        timed(due, { summary: 'b none', priority: null }),
        timed(due, { summary: 'medium', priority: 5 }),
        timed(due, { summary: 'low', priority: 9 }),
        timed(due, { summary: 'b high', priority: 4 }),
        timed(due, { summary: 'a high', priority: 1 }),
        timed(due, { summary: 'a none', priority: 0 }),
      ];
      expect(sortTitles(tasks, 'due', 'asc')).toEqual(['a high', 'b high', 'medium', 'low', 'a none', 'b none']);
      // dir reverses the due date only, never the tie-breakers.
      expect(sortTitles(tasks, 'due', 'desc')).toEqual(['a high', 'b high', 'medium', 'low', 'a none', 'b none']);
    });

    it('due: an all-day task sorts after the timed tasks of its day and before the next day, across DST', () => {
      const tasks = [
        timed('2026-03-29T22:30:00Z', { summary: '30th 00:30' }),
        allDay('2026-03-29', { summary: '29th all day' }),
        timed('2026-03-29T21:30:00Z', { summary: '29th 23:30' }),
        allDay('2026-03-30', { summary: '30th all day' }),
        timed('2026-03-28T23:30:00Z', { summary: '29th 00:30' }),
      ];
      expect(sortTitles(tasks, 'due')).toEqual([
        '29th 00:30', '29th 23:30', '29th all day', '30th 00:30', '30th all day',
      ]);
    });

    it('due: at a midnight tie the all-day task (due by the end of its day) comes first', () => {
      // Titles would order the timed task first: the tie must be decided by the due key.
      const atMidnight = timed('2026-11-05T23:00:00Z', { summary: 'a timed at 00:00 on the 6th' });
      const allDayFifth = allDay('2026-11-05', { summary: 'b all day on the 5th' });
      expect(sortTitles([atMidnight, allDayFifth], 'due', 'asc')).toEqual([
        'b all day on the 5th', 'a timed at 00:00 on the 6th',
      ]);
      expect(sortTitles([allDayFifth, atMidnight], 'due', 'desc')).toEqual([
        'a timed at 00:00 on the 6th', 'b all day on the 5th',
      ]);
      // ...which is the bucket order on both sides of that midnight.
      expect(dueBucket(allDayFifth, local(2026, 11, 5, 23))).toBe('today');
      expect(dueBucket(atMidnight, local(2026, 11, 5, 23))).toBe('later');
      expect(dueBucket(allDayFifth, local(2026, 11, 6, 0))).toBe('overdue');
      expect(dueBucket(atMidnight, local(2026, 11, 6, 0))).toBe('today');
    });

    it('due: open tasks come out in dueBucket order (overdue, today, later, none) at any moment', () => {
      const tasks = [
        allDay('2026-11-04'), allDay('2026-11-05'), allDay('2026-11-06'),
        timed('2026-11-04T20:00:00Z'), timed('2026-11-05T05:00:00Z'), timed('2026-11-05T12:00:00Z'),
        timed('2026-11-05T22:59:00Z'), timed('2026-11-05T23:00:00Z'), timed('2026-11-06T10:00:00Z'),
        task(), task({ status: 'in-process', priority: 1 }),
        allDay('2026-03-29'), timed('2026-03-29T21:45:00Z'), timed('2026-03-29T22:00:00Z'),
      ].map((t, i) => ({ ...t, status: i % 2 ? 'in-process' : 'needs-action' }) as Task);
      const rank: Record<DueBucket, number> = { overdue: 0, today: 1, later: 2, none: 3 };
      const sorted = [...tasks].sort(compareTasks('due', 'asc'));
      for (const now of [
        local(2026, 11, 5, 0, 0), local(2026, 11, 5, 7), local(2026, 11, 5, 13), local(2026, 11, 5, 23, 59),
        local(2026, 11, 6, 0, 0), local(2026, 3, 29, 23, 30), local(2026, 3, 30, 0, 0),
      ]) {
        const ranks = sorted.map((t) => rank[dueBucket(t, now)]);
        expect(ranks, now.toString()).toEqual([...ranks].sort((a, b) => a - b));
      }
    });

    it('priority: high, medium, low, none last; ties by due date, then title', () => {
      const tasks = [
        timed('2026-11-09T09:00:00Z', { summary: 'high later', priority: 1 }),
        timed('2026-11-05T09:00:00Z', { summary: 'high sooner', priority: 4 }),
        task({ summary: 'high undated', priority: 2 }),
        timed('2026-11-01T09:00:00Z', { summary: 'none', priority: null }),
        allDay('2026-11-05', { summary: 'low', priority: 7 }),
        allDay('2026-11-05', { summary: 'medium b', priority: 5 }),
        allDay('2026-11-05', { summary: 'medium a', priority: 5 }),
      ];
      expect(sortTitles(tasks, 'priority', 'asc')).toEqual([
        'high sooner', 'high later', 'high undated', 'medium a', 'medium b', 'low', 'none',
      ]);
      expect(sortTitles(tasks, 'priority', 'desc')).toEqual([
        'low', 'medium a', 'medium b', 'high sooner', 'high later', 'high undated', 'none',
      ]);
    });

    it('title: alphabetical, case-insensitive, trimmed, numbers in order; ties by priority then due', () => {
      const tasks = [
        task({ summary: 'task 10' }),
        task({ summary: 'Banana' }),
        task({ summary: '  cherry' }), // a client's leading blanks do not sort it first
        task({ summary: 'task 2' }),
        timed('2026-11-05T09:00:00Z', { summary: 'Same', priority: 9 }),
        timed('2026-11-04T09:00:00Z', { summary: 'same', priority: 9 }),
        task({ summary: 'same', priority: 1 }),
      ].map((t, i) => ({ ...t, id: `t${i}` }));
      const order = [...tasks].sort(compareTasks('title', 'asc')).map((t) => t.id);
      expect(order).toEqual(['t1', 't2', 't6', 't5', 't4', 't3', 't0']);
      const desc = [...tasks].sort(compareTasks('title', 'desc')).map((t) => t.id);
      expect(desc).toEqual(['t0', 't3', 't6', 't5', 't4', 't2', 't1']);
    });

    it('is a total order: the input order never shows', () => {
      const tasks = [
        timed('2026-11-05T09:00:00Z', { id: 'b', summary: 'same', created_at: '2026-11-01T10:00:00Z' }),
        timed('2026-11-05T09:00:00Z', { id: 'a', summary: 'same', created_at: '2026-11-01T10:00:00Z' }),
        timed('2026-11-05T09:00:00Z', { id: 'c', summary: 'same', created_at: '2026-10-01T10:00:00Z' }),
        task({ id: 'd', summary: 'same' }),
        allDay('2026-11-05', { id: 'e', summary: 'Same' }),
      ];
      for (const sortBy of ['due', 'priority', 'title'] as const) {
        for (const dir of ['asc', 'desc'] as const) {
          const forward = [...tasks].sort(compareTasks(sortBy, dir)).map((t) => t.id);
          const backward = [...tasks].reverse().sort(compareTasks(sortBy, dir)).map((t) => t.id);
          expect(backward, `${sortBy} ${dir}`).toEqual(forward);
        }
      }
      // Equal due, priority and title: oldest first, then id.
      expect([...tasks].sort(compareTasks('due')).map((t) => t.id)).toEqual(['c', 'a', 'b', 'e', 'd']);
    });
  });

  describe('formatDue', () => {
    const now = () => local(2026, 11, 5, 12); // Thursday 5 Nov 2026

    it('is empty without a due date', () => {
      expect(formatDue(task(), now(), 'en-GB', '24h')).toBe('');
      expect(formatDue(task({ due_all_day: true, start_date: '2026-11-05' }), now(), 'en-GB')).toBe('');
      expect(formatDue(allDay('2026-13-01'), now(), 'en-GB')).toBe('');
    });

    it('labels an all-day due date by calendar day', () => {
      expect(formatDue(allDay('2026-11-05'), now(), 'en-GB')).toBe('Today');
      expect(formatDue(allDay('2026-11-06'), now(), 'en-GB')).toBe('Tomorrow');
      expect(formatDue(allDay('2026-11-04'), now(), 'en-GB')).toBe('Yesterday');
      expect(formatDue(allDay('2026-11-09'), now(), 'en-GB')).toBe('Mon 9 Nov');
      expect(formatDue(allDay('2026-10-30'), now(), 'en-GB')).toBe('Fri 30 Oct');
      expect(formatDue(allDay('2027-01-05'), now(), 'en-GB')).toBe('Tue, 5 Jan 2027');
      expect(formatDue(allDay('2025-11-05'), now(), 'en-GB')).toBe('Wed, 5 Nov 2025');
    });

    it('labels a timed due date by its local day and time', () => {
      expect(formatDue(timed('2026-11-05T16:00:00Z'), now(), 'en-GB', '24h')).toBe('17:00');
      expect(formatDue(timed('2026-11-05T06:05:00Z'), now(), 'en-GB', '24h')).toBe('07:05'); // earlier today
      expect(formatDue(timed('2026-11-06T08:30:00Z'), now(), 'en-GB', '24h')).toBe('Tomorrow, 09:30');
      expect(formatDue(timed('2026-11-04T22:15:00Z'), now(), 'en-GB', '24h')).toBe('Yesterday, 23:15');
      expect(formatDue(timed('2026-11-09T07:00:00Z'), now(), 'en-GB', '24h')).toBe('Mon 9 Nov, 08:00');
      expect(formatDue(timed('2027-01-05T07:00:00Z'), now(), 'en-GB', '24h')).toBe('Tue, 5 Jan 2027, 08:00');
    });

    it('says Tomorrow and Today across midnight and across a DST night', () => {
      // 00:30 local on the 6th is still the 5th in UTC.
      expect(formatDue(timed('2026-11-05T23:30:00Z'), local(2026, 11, 5, 23), 'en-GB', '24h')).toBe('Tomorrow, 00:30');
      expect(formatDue(allDay('2026-11-06'), local(2026, 11, 5, 23, 59), 'en-GB')).toBe('Tomorrow');
      expect(formatDue(allDay('2026-11-06'), local(2026, 11, 6, 0, 0), 'en-GB')).toBe('Today');
      const springNight = local(2026, 3, 29, 23, 30);
      expect(formatDue(timed('2026-03-29T22:00:00Z'), springNight, 'en-GB', '24h')).toBe('Tomorrow, 00:00');
      expect(formatDue(timed('2026-03-28T23:30:00Z'), springNight, 'en-GB', '24h')).toBe('00:30');
      const fallNight = local(2026, 10, 25, 23, 30);
      expect(formatDue(timed('2026-10-25T22:45:00Z'), fallNight, 'en-GB', '24h')).toBe('23:45');
      expect(formatDue(allDay('2026-10-26'), fallNight, 'en-GB')).toBe('Tomorrow');
    });

    it('follows the 12h/24h preference over the locale, and the locale without one', () => {
      const t = timed('2026-11-05T16:00:00Z'); // 17:00 local
      expect(formatDue(t, now(), 'en-GB', '12h')).toMatch(/^5:00\spm$/);
      expect(formatDue(t, now(), 'en-US', '12h')).toMatch(/^5:00\sPM$/);
      expect(formatDue(t, now(), 'en-US', '24h')).toBe('17:00');
      expect(formatDue(timed('2026-11-04T23:05:00Z'), now(), 'en-US', '24h')).toBe('00:05'); // never "24:05"
      expect(formatDue(t, now(), 'de-DE')).toBe('17:00');
      expect(formatDue(t, now(), 'en-US')).toMatch(/^05:00\sPM$/);
      expect(formatDue(timed('2026-11-06T16:00:00Z'), now(), 'en-US', '12h')).toMatch(/^Tomorrow, 5:00\sPM$/);
    });

    it('formats dates in the given locale', () => {
      const t = timed('2026-11-09T07:00:00Z');
      const date = local(2026, 11, 9, 8, 0);
      expect(formatDue(t, now(), 'de-DE', '24h')).toBe(
        new Intl.DateTimeFormat('de-DE', {
          weekday: 'short', day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit', hourCycle: 'h23',
        }).format(date),
      );
      expect(formatDue(allDay('2026-11-09'), now(), 'en-US')).toBe('Mon, Nov 9');
    });
  });
});

describe('in America/Los_Angeles (west of UTC)', () => {
  useTimeZone('America/Los_Angeles');

  it('runs in the zone it asked for', () => {
    expect(local(2026, 1, 15, 12).getTimezoneOffset()).toBe(480);
    expect(local(2026, 7, 15, 12).getTimezoneOffset()).toBe(420);
  });

  it('never shifts an all-day due date to the previous day', () => {
    // new Date('2026-11-05') is 4 Nov 16:00 here; the date must stay the 5th.
    const now = local(2026, 11, 5, 9);
    const t = allDay('2026-11-05');
    expect(isOverdue(t, now)).toBe(false);
    expect(dueBucket(t, now)).toBe('today');
    expect(formatDue(t, now, 'en-GB')).toBe('Today');
    expect(dueBucket(t, local(2026, 11, 5, 23, 59))).toBe('today');
    expect(dueBucket(t, local(2026, 11, 6, 0, 0))).toBe('overdue');
  });

  it('puts a timed task on its local day although its UTC date is tomorrow', () => {
    const now = local(2026, 11, 5, 9); // 17:00Z
    const t = timed('2026-11-06T05:00:00Z'); // 21:00 on the 5th here
    expect(dueBucket(t, now)).toBe('today');
    expect(formatDue(t, now, 'en-GB', '24h')).toBe('21:00');
  });

  it('fall back (1 November 2026): the long day ends at local midnight', () => {
    const now = local(2026, 11, 1, 23, 30); // PST, 2026-11-02T07:30Z
    expect(dueBucket(timed('2026-11-02T07:45:00Z'), now)).toBe('today'); // 23:45 PST
    expect(dueBucket(timed('2026-11-02T08:00:00Z'), now)).toBe('later'); // 00:00 on the 2nd
    expect(dueBucket(allDay('2026-11-01'), now)).toBe('today');
  });
});

describe('in Pacific/Kiritimati (UTC+14)', () => {
  useTimeZone('Pacific/Kiritimati');

  it('runs in the zone it asked for', () => {
    expect(local(2026, 11, 5, 12).getTimezoneOffset()).toBe(-840);
  });

  it('reads dates in local time when UTC is a day behind', () => {
    const now = local(2026, 11, 6, 9); // 2026-11-05T19:00Z
    expect(dueBucket(allDay('2026-11-05'), now)).toBe('overdue');
    expect(dueBucket(allDay('2026-11-06'), now)).toBe('today');
    expect(dueBucket(timed('2026-11-05T20:00:00Z'), now)).toBe('today'); // 10:00 on the 6th here
    expect(dueBucket(timed('2026-11-05T09:00:00Z'), now)).toBe('overdue'); // 23:00 on the 5th
    expect(formatDue(timed('2026-11-05T09:00:00Z'), now, 'en-GB', '24h')).toBe('Yesterday, 23:00');
  });
});
