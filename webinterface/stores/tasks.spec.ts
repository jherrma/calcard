// @vitest-environment nuxt
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import { createTestingPinia } from '@pinia/testing';
import { flushPromises } from '@vue/test-utils';
import { toRaw } from 'vue';
import { TASK_SORT_STORAGE_KEY, useTasksStore } from './tasks';
import { useCalendarStore } from './calendars';
import type { Calendar } from '~/types/calendar';
import type { Task, TaskInput, TasksSummary } from '~/types/tasks';
import { installMemoryStorage, installThrowingStorage } from '~/test/support/storage';

// useApi() returns this function (the store does `const api = useApi(); await api(url, opts)`).
// The toast and the confirmation service are PrimeVue inject()-based; the store
// reaches them through the app context, and these mocks stand in for them.
const { apiMock, toastMock, confirmMock } = vi.hoisted(() => ({
  apiMock: vi.fn(),
  toastMock: { success: vi.fn(), error: vi.fn(), warn: vi.fn(), info: vi.fn() },
  confirmMock: { require: vi.fn(), close: vi.fn() },
}));
mockNuxtImport('useApi', () => () => apiMock);
mockNuxtImport('useAppToast', () => () => toastMock);
mockNuxtImport('useConfirm', () => () => confirmMock);
// The reminder refresh hook (#169, story 16) is a lazy import of the alarms store: stand in for it.
const { refreshMock } = vi.hoisted(() => ({ refreshMock: vi.fn() }));
mockNuxtImport('refreshAlarmsSoon', () => refreshMock);

interface ApiOptions {
  method?: string;
  query?: Record<string, string>;
  body?: unknown;
}
type Handler = (options: ApiOptions) => unknown;

const SUMMARY: TasksSummary = {
  open: 4,
  overdue: 2,
  due_today: 1,
  completed: 3,
  by_calendar: { 'list-a': { open: 3, overdue: 2, completed: 2 }, 'list-b': { open: 1, overdue: 0, completed: 1 } },
};

/**
 * Answers every request by `METHOD url`; an unexpected request fails the call
 * loudly. The summary route answers SUMMARY unless a test overrides it, because
 * every mutation refetches it.
 */
function serve(handlers: Record<string, Handler>) {
  const all: Record<string, Handler> = { 'GET /api/v1/tasks/summary': () => SUMMARY, ...handlers };
  apiMock.mockImplementation(async (url: string, options: ApiOptions = {}) => {
    const key = `${options.method ?? 'GET'} ${url}`;
    const handler = all[key];
    if (!handler) throw new Error(`unexpected request ${key}`);
    return handler(options);
  });
}

/** The calls made as `METHOD url`, in order. */
function requests(): string[] {
  return apiMock.mock.calls.map(([url, options]) => `${(options as ApiOptions | undefined)?.method ?? 'GET'} ${url}`);
}

const count = (key: string) => requests().filter((r) => r === key).length;

/** The options of the n-th call of `METHOD url`. */
function optionsOf(key: string, n = 0): ApiOptions {
  const calls = apiMock.mock.calls.filter(
    ([url, options]) => `${(options as ApiOptions | undefined)?.method ?? 'GET'} ${url}` === key,
  );
  return (calls[n]?.[1] ?? {}) as ApiOptions;
}

/** A FetchError as ofetch builds it: status on `status`/`statusCode`, the JSON body on `data`. */
function httpError(status: number, message: string) {
  return Object.assign(new Error(`[POST] "/api/v1/…": ${status}`), {
    status,
    statusCode: status,
    data: { status: 'error', message },
  });
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

function calendar(partial: Partial<Calendar> & { uuid: string }): Calendar {
  return {
    id: partial.uuid,
    path: `${partial.uuid}.ics`,
    name: partial.uuid,
    color: '#123456',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...partial,
  };
}

let seq = 0;
function task(overrides: Partial<Task> = {}): Task {
  seq += 1;
  return {
    id: `task-${String(seq).padStart(4, '0')}`,
    calendar_id: 1,
    calendar_uuid: 'list-a',
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

/** A row as `GET /tasks` lists it: with the list's name and colour. */
const listed = (overrides: Partial<Task> = {}) =>
  task({ calendar_name: 'Errands', calendar_color: '#ff0000', ...overrides });

/** The same task as a write route answers it: no calendar_name/calendar_color keys at all. */
function writeAnswer(row: Task, overrides: Partial<Task> = {}): Task {
  const { calendar_name: _name, calendar_color: _color, ...rest } = row;
  return { ...rest, ...overrides };
}

/** Seeds the calendars store with the lists the specs use: Errands (list-a), Home (list-b), an event calendar. */
function seedLists() {
  useCalendarStore().calendars = [
    calendar({ uuid: 'cal-personal', name: 'Personal', kind: 'events' }),
    calendar({ uuid: 'list-a', name: 'Errands', color: '#ff0000', kind: 'tasks' }),
    calendar({ uuid: 'list-b', name: 'Home', color: '#00ff00', kind: 'both' }),
  ];
}

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

let restoreStorage: (() => void) | null = null;

beforeEach(() => {
  // A working storage for every spec, so the store's state init reads a known
  // (empty) one; the sort specs install their own.
  restoreStorage = installMemoryStorage().restore;
  createTestingPinia({ stubActions: false });
  apiMock.mockReset();
  refreshMock.mockReset();
  Object.values(toastMock).forEach((fn) => fn.mockReset());
  confirmMock.require.mockReset();
  serve({});
});

afterEach(() => {
  restoreStorage?.();
  restoreStorage = null;
  vi.useRealTimers();
});

describe('task lists come from the calendars store (tasks and both only)', () => {
  const mixedBag = () => [
    calendar({ uuid: 'personal', name: 'Personal', kind: 'events' }),
    calendar({ uuid: 'tasks', name: 'Tasks', kind: 'tasks' }),
    calendar({ uuid: 'old', name: 'Old mixed', kind: 'both' }),
    calendar({ uuid: 'legacy', name: 'No kind' }),
    calendar({ uuid: 'team', name: 'Team', kind: 'events', shared: true, permission: 'read-write' }),
    calendar({ uuid: 'ro-list', name: 'Read-only list', kind: 'tasks', shared: true, permission: 'read' }),
    calendar({ uuid: 'rw-list', name: 'Shared list', kind: 'tasks', shared: true, permission: 'read-write' }),
    calendar({ uuid: 'feed', name: 'Holidays', kind: 'both', subscribed: true }),
  ];

  it('taskLists keeps tasks and both calendars and never an events or kind-less one', () => {
    useCalendarStore().calendars = mixedBag();
    const store = useTasksStore();

    expect(store.taskLists.map((c) => c.name)).toEqual([
      'Tasks',
      'Old mixed',
      'Read-only list',
      'Shared list',
      'Holidays',
    ]);
  });

  it('writableTaskLists drops read-only shares and subscriptions', () => {
    useCalendarStore().calendars = mixedBag();
    const store = useTasksStore();

    expect(store.writableTaskLists.map((c) => c.name)).toEqual(['Tasks', 'Old mixed', 'Shared list']);
  });

  it('a user with only event calendars has no task lists', () => {
    useCalendarStore().calendars = mixedBag().filter((c) => c.kind === 'events' || !c.kind);
    const store = useTasksStore();

    expect(store.taskLists).toEqual([]);
    expect(store.writableTaskLists).toEqual([]);
  });

  it('reuses the one GET /calendars and loads every list with ONE /tasks request', async () => {
    serve({
      'GET /api/v1/calendars': () => ({ calendars: mixedBag() }),
      'GET /api/v1/tasks': () => ({ tasks: [listed()], count: 1, total: 1 }),
    });
    const calendarStore = useCalendarStore();
    const store = useTasksStore();

    await calendarStore.fetchCalendars();
    expect(store.taskLists).toHaveLength(5);
    await store.fetchTasks();
    await store.fetchSummary();

    expect(requests()).toEqual(['GET /api/v1/calendars', 'GET /api/v1/tasks', 'GET /api/v1/tasks/summary']);
    // Nothing asked per calendar: no event fan-out, no per-list task route.
    expect(requests().some((r) => r.includes('/events') || /\/calendars\/[^/]+\/tasks/.test(r))).toBe(false);
  });

  // Story 26: where the quick-add and "Add a task" put a new task.
  it('newTaskListId: the selected list when writable, under All tasks the first writable one, else none', () => {
    useCalendarStore().calendars = [
      calendar({ uuid: 'ro-list', name: 'Read-only list', kind: 'tasks', shared: true, permission: 'read' }),
      calendar({ uuid: 'feed', name: 'Holidays', kind: 'both', subscribed: true }),
      ...mixedBag(),
    ];
    const store = useTasksStore();

    expect(store.newTaskListId).toBe('tasks');
    store.selectedListId = 'rw-list';
    expect(store.newTaskListId).toBe('rw-list');
    store.selectedListId = 'old';
    expect(store.newTaskListId).toBe('old');
    // A read-only share or a subscription: nowhere to add (not a fallback list).
    store.selectedListId = 'ro-list';
    expect(store.newTaskListId).toBeNull();
    store.selectedListId = 'feed';
    expect(store.newTaskListId).toBeNull();

    useCalendarStore().calendars = mixedBag().filter((c) => c.subscribed || c.permission === 'read' || c.kind === 'events');
    store.selectedListId = null;
    expect(store.newTaskListId).toBeNull();
  });

  it('reacts to a calendars refetch (a new list shows up without a request of its own)', () => {
    const calendarStore = useCalendarStore();
    calendarStore.calendars = [calendar({ uuid: 'tasks', name: 'Tasks', kind: 'tasks' })];
    const store = useTasksStore();
    expect(store.taskLists.map((c) => c.name)).toEqual(['Tasks']);

    calendarStore.calendars = [...calendarStore.calendars, calendar({ uuid: 'new', name: 'New', kind: 'tasks' })];

    expect(store.taskLists.map((c) => c.name)).toEqual(['Tasks', 'New']);
    expect(apiMock).not.toHaveBeenCalled();
  });
});

describe('fetchTasks builds the query', () => {
  it('asks GET /api/v1/tasks with status=open by default, through the query option', async () => {
    const rows = [listed(), listed()];
    serve({ 'GET /api/v1/tasks': () => ({ tasks: rows, count: 2, total: 7 }) });
    const store = useTasksStore();

    await store.fetchTasks();

    expect(apiMock).toHaveBeenCalledTimes(1);
    expect(apiMock).toHaveBeenCalledWith('/api/v1/tasks', { query: { status: 'open' } });
    expect(store.tasks).toEqual(rows);
    expect(store.total).toBe(7);
    expect(store.isLoading).toBe(false);
    expect(store.error).toBeNull();
  });

  it('sends calendar_uuid for the selected list, the status filter and the trimmed search text', async () => {
    serve({ 'GET /api/v1/tasks': () => ({ tasks: [], count: 0, total: 0 }) });
    const store = useTasksStore();
    store.selectedListId = 'list-b';
    store.statusFilter = 'completed';
    store.searchQuery = '  milk & eggs  ';

    await store.fetchTasks();

    expect(apiMock).toHaveBeenCalledWith('/api/v1/tasks', {
      query: { status: 'completed', calendar_uuid: 'list-b', q: 'milk & eggs' },
    });
  });

  it('leaves out a blank search', async () => {
    serve({ 'GET /api/v1/tasks': () => ({ tasks: [], count: 0, total: 0 }) });
    const store = useTasksStore();
    store.searchQuery = '   ';
    store.statusFilter = 'all';

    await store.fetchTasks();

    expect(optionsOf('GET /api/v1/tasks').query).toEqual({ status: 'all' });
  });

  it('is loading while the request runs', async () => {
    const pending = deferred<unknown>();
    serve({ 'GET /api/v1/tasks': () => pending.promise });
    const store = useTasksStore();

    const done = store.fetchTasks();
    expect(store.isLoading).toBe(true);
    pending.resolve({ tasks: [], count: 0, total: 0 });
    await done;

    expect(store.isLoading).toBe(false);
  });

  it('keeps the previous rows and sets error when the load fails', async () => {
    const rows = [listed()];
    serve({ 'GET /api/v1/tasks': () => ({ tasks: rows, count: 1, total: 1 }) });
    const store = useTasksStore();
    await store.fetchTasks();

    serve({
      'GET /api/v1/tasks': () => {
        throw new TypeError('Failed to fetch');
      },
    });
    await store.fetchTasks();

    expect(store.tasks).toEqual(rows);
    expect(store.error).toBe("Couldn't load tasks. Check your connection and try again.");
    expect(store.isLoading).toBe(false);
    expect(toastMock.error).not.toHaveBeenCalled();
  });

  it('carries the server message of a 4xx (a list that vanished is a 404)', async () => {
    serve({
      'GET /api/v1/tasks': () => {
        throw httpError(404, 'Calendar not found');
      },
    });
    const store = useTasksStore();
    store.selectedListId = 'gone';

    await store.fetchTasks();

    expect(store.error).toBe('Calendar not found');
  });

  it('drops a response that arrives after a newer request', async () => {
    const slow = deferred<unknown>();
    const fast = deferred<unknown>();
    serve({ 'GET /api/v1/tasks': (o) => (o.query?.calendar_uuid === 'list-a' ? slow.promise : fast.promise) });
    const store = useTasksStore();
    const listA = [listed({ calendar_uuid: 'list-a' })];
    const listB = [listed({ calendar_uuid: 'list-b' })];

    const first = store.selectList('list-a');
    const second = store.selectList('list-b');
    fast.resolve({ tasks: listB, count: 1, total: 1 });
    await second;
    expect(store.isLoading).toBe(false);
    slow.resolve({ tasks: listA, count: 1, total: 9 });
    await first;

    expect(store.tasks).toEqual(listB);
    expect(store.total).toBe(1);
    expect(store.isLoading).toBe(false);
  });
});

describe('selected list narrowing', () => {
  it('selectList narrows the request and the visible rows; selectAll widens both', async () => {
    serve({ 'GET /api/v1/tasks': () => ({ tasks: [], count: 0, total: 0 }) });
    const store = useTasksStore();
    const a = listed({ calendar_uuid: 'list-a' });
    const b = listed({ calendar_uuid: 'list-b' });

    await store.selectList('list-b');
    expect(store.selectedListId).toBe('list-b');
    expect(optionsOf('GET /api/v1/tasks', 0).query).toEqual({ status: 'open', calendar_uuid: 'list-b' });
    // Rows of another list (still loaded from "All tasks") are not shown.
    store.tasks = [a, b];
    expect(store.visibleTasks).toEqual([b]);

    await store.selectAll();
    expect(store.selectedListId).toBeNull();
    expect(optionsOf('GET /api/v1/tasks', 1).query).toEqual({ status: 'open' });
    store.tasks = [a, b];
    expect(store.visibleTasks).toEqual([a, b]);
  });

  it('setStatusFilter and setSearchQuery refetch with the new values', async () => {
    serve({ 'GET /api/v1/tasks': () => ({ tasks: [], count: 0, total: 0 }) });
    const store = useTasksStore();

    await store.setStatusFilter('completed');
    await store.setSearchQuery('dentist');

    expect(optionsOf('GET /api/v1/tasks', 0).query).toEqual({ status: 'completed' });
    expect(optionsOf('GET /api/v1/tasks', 1).query).toEqual({ status: 'completed', q: 'dentist' });
  });

  it('clearFilters goes back to Open without a search in ONE load, keeping the list and the sort', async () => {
    serve({ 'GET /api/v1/tasks': () => ({ tasks: [], count: 0, total: 0 }) });
    const store = useTasksStore();
    store.selectedListId = 'list-b';
    store.statusFilter = 'completed';
    store.searchQuery = 'dentist';
    store.setSort('priority', 'desc');

    await store.clearFilters();

    expect(store.statusFilter).toBe('open');
    expect(store.searchQuery).toBe('');
    expect(count('GET /api/v1/tasks')).toBe(1);
    expect(optionsOf('GET /api/v1/tasks', 0).query).toEqual({ status: 'open', calendar_uuid: 'list-b' });
    expect(store.selectedListId).toBe('list-b');
    expect([store.sortBy, store.sortDir]).toEqual(['priority', 'desc']);
  });

  it('visibleTasks follows the status filter: open, done (completed AND cancelled), all', () => {
    const store = useTasksStore();
    const open = task({ status: 'needs-action' });
    const inProcess = task({ status: 'in-process' });
    const done = task({ status: 'completed', completed: true });
    const cancelled = task({ status: 'cancelled' });
    store.tasks = [open, inProcess, done, cancelled];

    store.statusFilter = 'open';
    expect(store.visibleTasks).toEqual([open, inProcess]);
    store.statusFilter = 'completed';
    expect(store.visibleTasks).toEqual([done, cancelled]);
    store.statusFilter = 'all';
    expect(store.visibleTasks).toEqual([open, inProcess, done, cancelled]);
  });
});

describe('groups', () => {
  it('come in the order Needs action, In progress, Completed, Cancelled, empty ones hidden', () => {
    const store = useTasksStore();
    store.statusFilter = 'all';
    store.tasks = [
      task({ status: 'cancelled' }),
      task({ status: 'completed', completed: true }),
      task({ status: 'needs-action' }),
    ];

    expect(store.groups.map((g) => [g.key, g.label, g.tasks.length])).toEqual([
      ['needs-action', 'Needs action', 1],
      ['completed', 'Completed', 1],
      ['cancelled', 'Cancelled', 1],
    ]);
  });

  it('show all four groups when every status is present', () => {
    const store = useTasksStore();
    store.statusFilter = 'all';
    store.tasks = [
      task({ status: 'cancelled' }),
      task({ status: 'in-process' }),
      task({ status: 'completed', completed: true }),
      task({ status: 'needs-action' }),
    ];

    expect(store.groups.map((g) => g.label)).toEqual(['Needs action', 'In progress', 'Completed', 'Cancelled']);
  });

  it('under Open no Completed or Cancelled group exists, even with such rows loaded', () => {
    const store = useTasksStore();
    store.tasks = [task({ status: 'completed', completed: true }), task({ status: 'in-process' })];

    expect(store.groups.map((g) => g.key)).toEqual(['in-process']);
  });

  it('no rows, no groups', () => {
    expect(useTasksStore().groups).toEqual([]);
  });

  it('sort within each group by sortBy/sortDir, without reordering the loaded rows', () => {
    const store = useTasksStore();
    const late = task({ summary: 'Banana', due: '2026-11-09T10:00:00Z', priority: 9 });
    const early = task({ summary: 'Cherry', due: '2026-11-04T10:00:00Z', priority: 5 });
    const undated = task({ summary: 'Apple', priority: 1 });
    const busy = task({ summary: 'Damson', status: 'in-process', due: '2026-11-01T10:00:00Z' });
    store.tasks = [late, busy, undated, early];

    const titles = () => store.groups.map((g) => g.tasks.map((t) => t.summary));
    expect(titles()).toEqual([['Cherry', 'Banana', 'Apple'], ['Damson']]); // due asc, undated last
    store.sortDir = 'desc';
    expect(titles()).toEqual([['Banana', 'Cherry', 'Apple'], ['Damson']]); // undated still last
    store.sortBy = 'priority';
    store.sortDir = 'asc';
    expect(titles()).toEqual([['Apple', 'Cherry', 'Banana'], ['Damson']]);
    store.sortBy = 'title';
    expect(titles()).toEqual([['Apple', 'Banana', 'Cherry'], ['Damson']]);

    expect(store.tasks).toEqual([late, busy, undated, early]);
  });
});

describe('summary', () => {
  describe('in Europe/Berlin (+01:00 in November)', () => {
    useTimeZone('Europe/Berlin');

    it('sends as_of = local now with its offset through the query option, never in the URL', async () => {
      vi.useFakeTimers({ toFake: ['Date'] });
      vi.setSystemTime(new Date('2026-11-03T09:30:15Z'));
      expect(new Date().getTimezoneOffset()).toBe(-60); // the zone really took effect
      const store = useTasksStore();

      await store.fetchSummary();

      expect(apiMock).toHaveBeenCalledTimes(1);
      const [url, options] = apiMock.mock.calls[0]!;
      expect(url).toBe('/api/v1/tasks/summary');
      expect(url).not.toMatch(/[?+]|as_of/);
      expect(options).toEqual({ query: { as_of: '2026-11-03T10:30:15+01:00' } });
      expect(store.summary).toEqual(SUMMARY);
    });
  });

  it('overdueCount is the server count (summary.overdue), not a count of the loaded rows', () => {
    const store = useTasksStore();
    store.tasks = [task({ due: '2020-01-01T00:00:00Z' })];
    expect(store.overdueCount).toBe(0); // no summary yet

    store.summary = { ...SUMMARY, overdue: 7 };

    expect(store.overdueCount).toBe(7);
  });

  it('isOverdue(task, now) is the utils rule: all-day by date, timed by instant, done never', () => {
    const store = useTasksStore();
    const now = new Date(2026, 10, 5, 12, 0, 0);

    expect(store.isOverdue(task({ due_date: '2026-11-05', due_all_day: true }), now)).toBe(false);
    expect(store.isOverdue(task({ due_date: '2026-11-04', due_all_day: true }), now)).toBe(true);
    expect(store.isOverdue(task({ due: new Date(now.getTime() - 1000).toISOString() }), now)).toBe(true);
    expect(store.isOverdue(task({ due: '2020-01-01T00:00:00Z', status: 'cancelled' }), now)).toBe(false);
  });

  it('a failed refetch keeps the previous counts', async () => {
    const store = useTasksStore();
    await store.fetchSummary();
    serve({
      'GET /api/v1/tasks/summary': () => {
        throw new TypeError('Failed to fetch');
      },
    });
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});

    await store.fetchSummary();

    expect(store.summary).toEqual(SUMMARY);
    expect(toastMock.error).not.toHaveBeenCalled();
    warn.mockRestore();
  });

  it('drops a summary that arrives after a newer one', async () => {
    const older = deferred<TasksSummary>();
    const newer = deferred<TasksSummary>();
    const answers = [older.promise, newer.promise];
    serve({ 'GET /api/v1/tasks/summary': () => answers.shift() });
    const store = useTasksStore();

    const first = store.fetchSummary();
    const second = store.fetchSummary();
    newer.resolve({ ...SUMMARY, overdue: 1 });
    await second;
    older.resolve({ ...SUMMARY, overdue: 5 });
    await first;

    expect(store.overdueCount).toBe(1);
  });
});

describe('toggleComplete', () => {
  it('flips the row at once, marks it saving, then puts the server row in place', async () => {
    seedLists();
    const row = listed({ percent_complete: 40 });
    const answer = deferred<Task>();
    serve({ [`POST /api/v1/calendars/list-a/tasks/${row.id}/complete`]: () => answer.promise });
    const store = useTasksStore();
    store.statusFilter = 'all';
    store.tasks = [row];

    const done = store.toggleComplete(row);

    // Optimistic: before the server has answered.
    expect(store.tasks[0]).toMatchObject({ status: 'completed', completed: true, percent_complete: 100 });
    expect(store.tasks[0]!.completed_at).not.toBeNull();
    expect(store.isSaving.has(row.id)).toBe(true);
    expect(store.groups.map((g) => g.key)).toEqual(['completed']);

    const fromServer = writeAnswer(row, {
      status: 'completed',
      completed: true,
      completed_at: '2026-11-03T09:30:15Z',
      percent_complete: 100,
      etag: 'etag-new',
    });
    answer.resolve(fromServer);
    const result = await done;

    expect(optionsOf(`POST /api/v1/calendars/list-a/tasks/${row.id}/complete`)).toEqual({ method: 'POST' });
    expect(store.tasks[0]).toEqual({ ...fromServer, calendar_name: 'Errands', calendar_color: '#ff0000' });
    expect(result).toEqual(store.tasks[0]);
    expect(store.isSaving.has(row.id)).toBe(false);
    expect(toastMock.error).not.toHaveBeenCalled();
  });

  it('a completed task is unticked through /uncomplete (100% back to 0)', async () => {
    const row = listed({ status: 'completed', completed: true, completed_at: '2026-11-01T08:00:00Z', percent_complete: 100 });
    const answer = deferred<Task>();
    serve({ [`POST /api/v1/calendars/list-a/tasks/${row.id}/uncomplete`]: () => answer.promise });
    const store = useTasksStore();
    store.statusFilter = 'all';
    store.tasks = [row];

    const done = store.toggleComplete(row);
    expect(store.tasks[0]).toMatchObject({
      status: 'needs-action',
      completed: false,
      completed_at: null,
      percent_complete: 0,
    });

    answer.resolve(writeAnswer(row, { status: 'needs-action', completed: false, completed_at: null, percent_complete: 0, etag: 'e2' }));
    await done;

    expect(requests()).toContain(`POST /api/v1/calendars/list-a/tasks/${row.id}/uncomplete`);
    expect(store.tasks[0]).toMatchObject({ etag: 'e2', calendar_name: 'Errands' });
  });

  it('a cancelled task is completed (it is not "completed")', async () => {
    const row = listed({ status: 'cancelled' });
    serve({ [`POST /api/v1/calendars/list-a/tasks/${row.id}/complete`]: () => writeAnswer(row, { status: 'completed', completed: true }) });
    const store = useTasksStore();
    store.tasks = [row];

    await store.toggleComplete(row);

    expect(requests()).toContain(`POST /api/v1/calendars/list-a/tasks/${row.id}/complete`);
  });

  it('rolls the row back and toasts when the request fails', async () => {
    const row = listed();
    serve({
      [`POST /api/v1/calendars/list-a/tasks/${row.id}/complete`]: () => {
        throw httpError(500, 'Internal server error');
      },
    });
    const store = useTasksStore();
    store.tasks = [row];

    const result = await store.toggleComplete(row);

    expect(result).toBeNull();
    expect(store.tasks[0]).toEqual(row);
    expect(store.isSaving.has(row.id)).toBe(false);
    expect(toastMock.error).toHaveBeenCalledTimes(1);
    // A 5xx body says nothing useful; the toast says what failed and what to do.
    expect(toastMock.error).toHaveBeenCalledWith('Check your connection and try again.', "Couldn't complete the task");
  });

  it('a 409 (repeating task) rolls back and shows the server message', async () => {
    const row = listed({ is_recurring: true, rrule: 'FREQ=WEEKLY' });
    const sentence = 'Repeating tasks cannot be completed here yet. Complete it in your task app.';
    serve({
      [`POST /api/v1/calendars/list-a/tasks/${row.id}/complete`]: () => {
        throw httpError(409, sentence);
      },
    });
    const store = useTasksStore();
    store.tasks = [row];

    await store.toggleComplete(row);

    expect(store.tasks[0]).toEqual(row);
    expect(toastMock.error).toHaveBeenCalledWith(sentence, "Couldn't complete the task");
  });

  it('a failed untick says so', async () => {
    const row = listed({ status: 'completed', completed: true, percent_complete: 100 });
    serve({
      [`POST /api/v1/calendars/list-a/tasks/${row.id}/uncomplete`]: () => {
        throw httpError(403, 'You have read-only access to this calendar');
      },
    });
    const store = useTasksStore();
    store.statusFilter = 'completed';
    store.tasks = [row];

    await store.toggleComplete(row);

    expect(store.tasks[0]).toEqual(row);
    expect(toastMock.error).toHaveBeenCalledWith(
      'You have read-only access to this calendar',
      "Couldn't mark the task as not done",
    );
  });

  it('a second toggle of the same task while the first is in flight sends nothing', async () => {
    const row = listed();
    const answer = deferred<Task>();
    serve({ [`POST /api/v1/calendars/list-a/tasks/${row.id}/complete`]: () => answer.promise });
    const store = useTasksStore();
    store.tasks = [row];

    const first = store.toggleComplete(row);
    const second = await store.toggleComplete(store.tasks[0]!);
    answer.resolve(writeAnswer(row, { status: 'completed', completed: true }));
    await first;

    expect(second).toBeNull();
    expect(requests().filter((r) => r.startsWith('POST'))).toHaveLength(1);
  });

  it('a rollback never clobbers a row a newer load put in place meanwhile', async () => {
    const row = listed();
    const answer = deferred<Task>();
    serve({ [`POST /api/v1/calendars/list-a/tasks/${row.id}/complete`]: () => answer.promise });
    const store = useTasksStore();
    store.tasks = [row];

    const done = store.toggleComplete(row);
    const reloaded = { ...row, summary: 'Renamed on the phone', etag: 'etag-phone' };
    store.tasks = [reloaded];
    answer.reject(httpError(500, 'Internal server error'));
    await done;

    expect(store.tasks).toEqual([reloaded]);
  });

  it('a task not loaded in the list is still toggled (no row to flip)', async () => {
    const row = listed();
    serve({ [`POST /api/v1/calendars/list-a/tasks/${row.id}/complete`]: () => writeAnswer(row, { status: 'completed', completed: true }) });
    const store = useTasksStore();

    const result = await store.toggleComplete(row);

    expect(result).toMatchObject({ id: row.id, status: 'completed' });
    expect(store.tasks).toEqual([]);
  });
});

describe('undoComplete (MN9, MN10)', () => {
  /** The server's answer to the undo PATCH: the row with the patch applied. */
  const patched = (row: Task, o: ApiOptions): Task => {
    const body = o.body as { status: Task['status']; percent_complete: number | null };
    return writeAnswer(row, { status: body.status, completed: false, completed_at: null, percent_complete: body.percent_complete });
  };

  it('PATCHes the status and percent the task had before the tick, never the plain uncomplete', async () => {
    seedLists();
    const before = listed({ status: 'in-process', percent_complete: 40 });
    const ticked = listed({ ...before, status: 'completed', completed: true, percent_complete: 100 });
    serve({ [`PATCH /api/v1/calendars/list-a/tasks/${before.id}`]: (o) => patched(ticked, o) });
    const store = useTasksStore();
    store.statusFilter = 'all';
    store.tasks = [ticked];

    const result = await store.undoComplete(before);

    expect(optionsOf(`PATCH /api/v1/calendars/list-a/tasks/${before.id}`)).toEqual({
      method: 'PATCH',
      body: { status: 'in-process', percent_complete: 40 },
    });
    expect(requests().some((r) => r.endsWith('/uncomplete'))).toBe(false);
    expect(result).toMatchObject({ status: 'in-process', percent_complete: 40 });
    expect(store.tasks[0]).toMatchObject({ status: 'in-process', percent_complete: 40, calendar_name: 'Errands' });
  });

  it('sends null for a percentage the task never had, and the cancelled status of a cancelled one', async () => {
    const before = listed({ status: 'cancelled' });
    serve({ [`PATCH /api/v1/calendars/list-a/tasks/${before.id}`]: (o) => patched(before, o) });
    const store = useTasksStore();
    store.tasks = [];

    await store.undoComplete(before);

    expect(optionsOf(`PATCH /api/v1/calendars/list-a/tasks/${before.id}`).body).toEqual({
      status: 'cancelled',
      percent_complete: null,
    });
  });

  it('puts the server row back into the list when it matches the view, although the view was reloaded since the tick', async () => {
    seedLists();
    const before = listed();
    serve({ [`PATCH /api/v1/calendars/list-a/tasks/${before.id}`]: (o) => patched(before, o) });
    const store = useTasksStore();
    store.statusFilter = 'open';
    store.selectedListId = 'list-a';
    store.tasks = [listed({ summary: 'another' })];
    store.total = 1;

    await store.undoComplete(before);

    expect(store.visibleTasks.map((t) => t.id)).toContain(before.id);
    expect(store.total).toBe(2);
  });

  it('leaves the row out when it does not match the view (another list, a search that does not hit it)', async () => {
    seedLists();
    const before = listed({ summary: 'Buy milk' });
    serve({ [`PATCH /api/v1/calendars/list-a/tasks/${before.id}`]: (o) => patched(before, o) });
    const store = useTasksStore();
    store.statusFilter = 'open';

    store.selectedListId = 'list-b';
    await store.undoComplete(before);
    expect(store.tasks).toEqual([]);

    store.selectedListId = null;
    store.searchQuery = 'bread';
    await store.undoComplete(before);
    expect(store.tasks).toEqual([]);

    store.searchQuery = 'MILK';
    await store.undoComplete(before);
    expect(store.tasks.map((t) => t.id)).toEqual([before.id]);
  });

  it('does not add a row twice, and a failed PATCH changes nothing and toasts', async () => {
    seedLists();
    const before = listed();
    serve({ [`PATCH /api/v1/calendars/list-a/tasks/${before.id}`]: () => { throw httpError(409, 'No'); } });
    const store = useTasksStore();
    store.tasks = [before];

    expect(await store.undoComplete(before)).toBeNull();
    expect(store.tasks).toEqual([before]);
    expect(toastMock.error).toHaveBeenCalledTimes(1);

    store.restoreRow(before);
    expect(store.tasks).toHaveLength(1);
  });
});

describe('create, update and delete mutate the state', () => {
  const input: TaskInput = { summary: 'Buy stamps', due: '2026-11-05', due_all_day: true, priority: 1 };

  it('createTask POSTs to the list and adds the row with its list name and colour', async () => {
    seedLists();
    const created = task({ summary: 'Buy stamps', due_date: '2026-11-05', due_all_day: true, priority: 1 });
    serve({ 'POST /api/v1/calendars/list-a/tasks': () => created });
    const store = useTasksStore();
    store.total = 4;

    const result = await store.createTask('list-a', input);

    expect(optionsOf('POST /api/v1/calendars/list-a/tasks')).toEqual({ method: 'POST', body: input });
    expect(result).toEqual({ ...created, calendar_name: 'Errands', calendar_color: '#ff0000' });
    expect(store.tasks).toEqual([result]);
    expect(store.total).toBe(5);
  });

  it('createTask and updateTask pass `reminders` through unchanged', async () => {
    seedLists();
    const row = listed();
    serve({
      'POST /api/v1/calendars/list-a/tasks': () => task({ summary: 'Pay' }),
      [`PATCH /api/v1/calendars/list-a/tasks/${row.id}`]: () => writeAnswer(row, {}),
    });
    const store = useTasksStore();
    store.tasks = [row];
    const withReminders: TaskInput = { summary: 'Pay', due: '2026-11-05', due_all_day: true, reminders: [{ offset_seconds: 32400 }] };

    await store.createTask('list-a', withReminders);
    await store.updateTask(row, { reminders: [] });

    expect(optionsOf('POST /api/v1/calendars/list-a/tasks')).toEqual({ method: 'POST', body: withReminders });
    expect(optionsOf(`PATCH /api/v1/calendars/list-a/tasks/${row.id}`)).toEqual({ method: 'PATCH', body: { reminders: [] } });
  });

  it('createTask does not add a row that is not in the current view', async () => {
    seedLists();
    serve({
      'POST /api/v1/calendars/list-a/tasks': () => task({ calendar_uuid: 'list-a' }),
      'POST /api/v1/calendars/list-b/tasks': () => task({ calendar_uuid: 'list-b' }),
    });
    const store = useTasksStore();

    store.selectedListId = 'list-b';
    expect(await store.createTask('list-a', input)).not.toBeNull(); // another list
    store.selectedListId = null;
    store.statusFilter = 'completed';
    expect(await store.createTask('list-b', input)).not.toBeNull(); // an open task under Done

    expect(store.tasks).toEqual([]);
    expect(store.total).toBe(0);
  });

  it('createTask toasts the server message and resolves null on failure', async () => {
    serve({
      'POST /api/v1/calendars/list-a/tasks': () => {
        throw httpError(400, 'summary is required');
      },
    });
    const store = useTasksStore();

    const result = await store.createTask('list-a', { summary: ' ' });

    expect(result).toBeNull();
    expect(store.tasks).toEqual([]);
    expect(toastMock.error).toHaveBeenCalledWith('summary is required', "Couldn't add the task");
  });

  it('updateTask PATCHes the changes and puts the answer in place, keeping the list name and colour', async () => {
    const row = listed({ summary: 'Old title', priority: 5 });
    const other = listed();
    const answer = deferred<Task>();
    serve({ [`PATCH /api/v1/calendars/list-a/tasks/${row.id}`]: () => answer.promise });
    const store = useTasksStore();
    store.tasks = [other, row];

    const done = store.updateTask(row, { summary: 'New title', priority: null });
    expect(store.isSaving.has(row.id)).toBe(true);
    const fromServer = writeAnswer(row, { summary: 'New title', priority: null, etag: 'etag-2' });
    answer.resolve(fromServer);
    const result = await done;

    expect(optionsOf(`PATCH /api/v1/calendars/list-a/tasks/${row.id}`)).toEqual({
      method: 'PATCH',
      body: { summary: 'New title', priority: null },
    });
    expect(store.tasks).toEqual([other, { ...fromServer, calendar_name: 'Errands', calendar_color: '#ff0000' }]);
    expect(result).toEqual(store.tasks[1]);
    expect(store.isSaving.has(row.id)).toBe(false);
  });

  it('updateTask leaves the row alone and toasts on failure', async () => {
    const row = listed({ is_recurring: true });
    serve({
      [`PATCH /api/v1/calendars/list-a/tasks/${row.id}`]: () => {
        throw httpError(409, 'Repeating tasks cannot be completed here yet. Complete it in your task app.');
      },
    });
    const store = useTasksStore();
    store.tasks = [row];

    const result = await store.updateTask(row, { status: 'cancelled' });

    expect(result).toBeNull();
    expect(store.tasks).toEqual([row]);
    expect(store.isSaving.has(row.id)).toBe(false);
    expect(toastMock.error).toHaveBeenCalledWith(
      'Repeating tasks cannot be completed here yet. Complete it in your task app.',
      "Couldn't save the task",
    );
  });

  describe('deleteTask', () => {
    interface ConfirmOptions {
      header: string;
      message: string;
      acceptLabel: string;
      accept: () => void;
      reject: () => void;
      onHide: () => void;
    }
    const confirmation = () => confirmMock.require.mock.calls[0]![0] as ConfirmOptions;

    it('asks first, then DELETEs on accept and drops the row', async () => {
      const row = listed();
      const keep = listed();
      serve({ [`DELETE /api/v1/calendars/list-a/tasks/${row.id}`]: () => undefined });
      const store = useTasksStore();
      store.tasks = [row, keep];
      store.total = 2;

      const result = store.deleteTask(row);
      expect(confirmMock.require).toHaveBeenCalledTimes(1);
      expect(confirmation()).toMatchObject({
        header: 'Delete task',
        message: 'Delete this task? It is removed from every device that syncs this list. This can\'t be undone.',
        acceptLabel: 'Delete task',
      });
      expect(requests().some((r) => r.startsWith('DELETE'))).toBe(false); // not before the answer

      confirmation().accept();

      expect(await result).toBe(true);
      expect(optionsOf(`DELETE /api/v1/calendars/list-a/tasks/${row.id}`)).toEqual({ method: 'DELETE' });
      expect(store.tasks).toEqual([keep]);
      expect(store.total).toBe(1);
      expect(store.isSaving.has(row.id)).toBe(false);
    });

    it.each(['reject', 'onHide'] as const)('deletes nothing when the user cancels (%s)', async (how) => {
      const row = listed();
      const store = useTasksStore();
      store.tasks = [row];

      const result = store.deleteTask(row);
      confirmation()[how]();

      expect(await result).toBe(false);
      expect(apiMock).not.toHaveBeenCalled();
      expect(store.tasks).toEqual([row]);
    });

    it('keeps the row and toasts when the delete fails', async () => {
      const row = listed();
      serve({
        [`DELETE /api/v1/calendars/list-a/tasks/${row.id}`]: () => {
          throw httpError(403, 'You have read-only access to this calendar');
        },
      });
      const store = useTasksStore();
      store.tasks = [row];

      const result = store.deleteTask(row);
      confirmation().accept();

      expect(await result).toBe(false);
      expect(store.tasks).toEqual([row]);
      expect(toastMock.error).toHaveBeenCalledWith(
        'You have read-only access to this calendar',
        "Couldn't delete the task",
      );
    });

    it('a 404 (already deleted elsewhere) drops the row without an error', async () => {
      const row = listed();
      serve({
        [`DELETE /api/v1/calendars/list-a/tasks/${row.id}`]: () => {
          throw httpError(404, 'Task not found');
        },
      });
      const store = useTasksStore();
      store.tasks = [row];

      const result = store.deleteTask(row);
      confirmation().accept();

      expect(await result).toBe(true);
      expect(store.tasks).toEqual([]);
      expect(toastMock.error).not.toHaveBeenCalled();
    });
  });
});

describe('the summary is refetched after every mutation', () => {
  const row = () => listed({ id: 'task-x' });
  const base = '/api/v1/calendars/list-a/tasks';

  const cases: Array<[string, (store: ReturnType<typeof useTasksStore>) => Promise<unknown>]> = [
    ['createTask', (s) => s.createTask('list-a', { summary: 'New' })],
    ['updateTask', (s) => s.updateTask(row(), { summary: 'Renamed' })],
    ['toggleComplete', (s) => s.toggleComplete(row())],
    [
      'deleteTask',
      (s) => {
        const result = s.deleteTask(row());
        (confirmMock.require.mock.calls[0]![0] as { accept: () => void }).accept();
        return result;
      },
    ],
    ['createTaskList', (s) => s.createTaskList('Groceries', '#00aa00')],
  ];

  it.each(cases)('%s (success)', async (_name, act) => {
    serve({
      [`POST ${base}`]: () => row(),
      [`PATCH ${base}/task-x`]: () => row(),
      [`POST ${base}/task-x/complete`]: () => row(),
      [`DELETE ${base}/task-x`]: () => undefined,
      'POST /api/v1/calendars': () => calendar({ uuid: 'new-list', kind: 'tasks' }),
      'GET /api/v1/calendars': () => ({ calendars: [calendar({ uuid: 'new-list', kind: 'tasks' })] }),
    });
    const store = useTasksStore();
    store.tasks = [row()];

    await act(store);
    await flushPromises();

    expect(count('GET /api/v1/tasks/summary')).toBe(1);
    expect(store.summary).toEqual(SUMMARY);
  });

  it.each(cases.filter(([name]) => name !== 'createTaskList'))('%s (failure)', async (_name, act) => {
    const fail = () => {
      throw httpError(500, 'Internal server error');
    };
    serve({
      [`POST ${base}`]: fail,
      [`PATCH ${base}/task-x`]: fail,
      [`POST ${base}/task-x/complete`]: fail,
      [`DELETE ${base}/task-x`]: fail,
    });
    const store = useTasksStore();
    store.tasks = [row()];

    await act(store);
    await flushPromises();

    // The server state may still have moved (e.g. a 404 for a task deleted elsewhere).
    expect(count('GET /api/v1/tasks/summary')).toBe(1);
  });

  it('a cancelled delete is no mutation and asks nothing', async () => {
    const store = useTasksStore();
    const result = store.deleteTask(row());
    (confirmMock.require.mock.calls[0]![0] as { reject: () => void }).reject();
    await result;
    await flushPromises();

    expect(apiMock).not.toHaveBeenCalled();
  });
});

describe('createTaskList', () => {
  it("POSTs kind 'tasks', refreshes the calendars store and the new list becomes a task list", async () => {
    const created = calendar({ uuid: 'groceries', name: 'Groceries', color: '#00aa00', kind: 'tasks' });
    serve({
      'POST /api/v1/calendars': () => created,
      'GET /api/v1/calendars': () => ({
        calendars: [calendar({ uuid: 'personal', name: 'Personal', kind: 'events' }), created],
      }),
    });
    const calendarStore = useCalendarStore();
    const store = useTasksStore();

    const result = await store.createTaskList('Groceries', '#00aa00');

    expect(optionsOf('POST /api/v1/calendars')).toEqual({
      method: 'POST',
      body: { name: 'Groceries', color: '#00aa00', kind: 'tasks' },
    });
    expect(count('GET /api/v1/calendars')).toBe(1);
    expect(result).toEqual(created);
    expect(store.taskLists.map((c) => c.uuid)).toEqual(['groceries']);
    expect(store.writableTaskLists.map((c) => c.uuid)).toEqual(['groceries']);
    // Never a calendar of the Calendar section.
    expect(calendarStore.eventCalendars.map((c) => c.uuid)).toEqual(['personal']);
  });

  it('keeps the new list from the 201 body when the calendars refetch fails', async () => {
    const created = calendar({ uuid: 'groceries', name: 'Groceries', kind: 'tasks' });
    serve({
      'POST /api/v1/calendars': () => created,
      'GET /api/v1/calendars': () => {
        throw new TypeError('Failed to fetch');
      },
    });
    const store = useTasksStore();

    await store.createTaskList('Groceries', '#00aa00');

    expect(store.taskLists.map((c) => c.uuid)).toEqual(['groceries']);
  });

  it('toasts and resolves null when the list cannot be created; no refetch', async () => {
    serve({
      'POST /api/v1/calendars': () => {
        throw httpError(400, 'name cannot be empty');
      },
    });
    const store = useTasksStore();

    const result = await store.createTaskList('', '#00aa00');
    await flushPromises();

    expect(result).toBeNull();
    expect(count('GET /api/v1/calendars')).toBe(0);
    expect(toastMock.error).toHaveBeenCalledWith('name cannot be empty', "Couldn't create the task list");
  });
});

describe('a deleted or renamed list (story 27)', () => {
  it('forgetTaskList drops the rows of that list from the page and from total, and refetches the counts', async () => {
    seedLists();
    const store = useTasksStore();
    const kept = listed({ calendar_uuid: 'list-b' });
    store.tasks = [listed(), kept, listed()];
    store.total = 30;
    store.selectedListId = null;

    store.forgetTaskList('list-a');
    await flushPromises();

    expect(store.tasks).toEqual([kept]);
    expect(store.total).toBe(28);
    expect(requests()).toEqual(['GET /api/v1/tasks/summary']);
    // The selection is the page's (it owns ?list=).
    expect(store.selectedListId).toBeNull();
  });

  it('forgetTaskList of a list with no loaded rows changes nothing but the counts', async () => {
    const store = useTasksStore();
    const row = listed();
    store.tasks = [row];
    store.total = 1;

    store.forgetTaskList('gone');
    await flushPromises();

    expect(store.tasks).toEqual([row]);
    expect(store.total).toBe(1);
    expect(count('GET /api/v1/tasks/summary')).toBe(1);
  });

  it('applyListInfo gives the loaded rows their list\'s new name and colour, without a request', () => {
    seedLists();
    const store = useTasksStore();
    const other = listed({ calendar_uuid: 'list-b', calendar_name: 'Home', calendar_color: '#00ff00' });
    const unknown = listed({ calendar_uuid: 'gone' });
    store.tasks = [listed(), other, unknown];
    useCalendarStore().calendars = useCalendarStore().calendars.map((c) =>
      c.uuid === 'list-a' ? { ...c, name: 'Groceries', color: '#d6336c' } : c);

    store.applyListInfo();

    expect(store.tasks.map((t) => [t.calendar_name, t.calendar_color])).toEqual([
      ['Groceries', '#d6336c'],
      ['Home', '#00ff00'],
      ['Errands', '#ff0000'],
    ]);
    // Rows that did not change stay the same objects (an in-flight tick keeps its rollback).
    expect(store.tasks[1]).toStrictEqual(other);
    expect(toRaw(store.tasks[1])).toBe(toRaw(other));
    expect(toRaw(store.tasks[2])).toBe(toRaw(unknown));
    expect(apiMock).not.toHaveBeenCalled();
  });
});

describe('error toasts', () => {
  it('skip a 429: useApi already raised its one toast for the burst', async () => {
    const row = listed();
    serve({
      [`POST /api/v1/calendars/list-a/tasks/${row.id}/complete`]: () => {
        throw httpError(429, 'Too many requests, please try again in 30 seconds.');
      },
    });
    const store = useTasksStore();
    store.tasks = [row];

    await store.toggleComplete(row);

    expect(store.tasks[0]).toEqual(row); // still rolled back
    expect(toastMock.error).not.toHaveBeenCalled();
  });
});

describe('sort preference', () => {
  it('defaults to due date, ascending', () => {
    const store = useTasksStore();

    expect([store.sortBy, store.sortDir]).toEqual(['due', 'asc']);
  });

  it('survives a new session through localStorage (installMemoryStorage)', () => {
    restoreStorage?.();
    const storage = installMemoryStorage();
    restoreStorage = storage.restore;
    const store = useTasksStore();

    store.setSort('priority', 'desc');
    expect(JSON.parse(storage.data.get(TASK_SORT_STORAGE_KEY)!)).toEqual({ sortBy: 'priority', sortDir: 'desc' });

    // A new app instance (fresh Pinia) reads it back.
    createTestingPinia({ stubActions: false });
    const reloaded = useTasksStore();
    expect(reloaded).not.toBe(store);
    expect([reloaded.sortBy, reloaded.sortDir]).toEqual(['priority', 'desc']);

    reloaded.toggleSortDir();
    reloaded.setSort('title');
    expect(JSON.parse(storage.data.get(TASK_SORT_STORAGE_KEY)!)).toEqual({ sortBy: 'title', sortDir: 'asc' });
  });

  it('ignores a junk stored value', () => {
    restoreStorage?.();
    const storage = installMemoryStorage();
    restoreStorage = storage.restore;
    storage.data.set(TASK_SORT_STORAGE_KEY, JSON.stringify({ sortBy: 'colour', sortDir: 'up' }));
    expect([useTasksStore().sortBy, useTasksStore().sortDir]).toEqual(['due', 'asc']);

    storage.data.set(TASK_SORT_STORAGE_KEY, '{not json');
    createTestingPinia({ stubActions: false });
    expect([useTasksStore().sortBy, useTasksStore().sortDir]).toEqual(['due', 'asc']);

    storage.data.set(TASK_SORT_STORAGE_KEY, JSON.stringify({ sortBy: 'title' }));
    createTestingPinia({ stubActions: false });
    expect([useTasksStore().sortBy, useTasksStore().sortDir]).toEqual(['title', 'asc']);
  });

  it('does not throw where storage access throws (installThrowingStorage)', () => {
    restoreStorage?.();
    restoreStorage = installThrowingStorage();
    createTestingPinia({ stubActions: false });

    const store = useTasksStore();
    expect([store.sortBy, store.sortDir]).toEqual(['due', 'asc']);
    expect(() => store.setSort('title', 'desc')).not.toThrow();
    expect(() => store.toggleSortDir()).not.toThrow();

    expect([store.sortBy, store.sortDir]).toEqual(['title', 'asc']);
  });
});

describe('reset (logout)', () => {
  it('drops the per-user state, keeps the sort, and ignores a load still in flight', async () => {
    const pending = deferred<unknown>();
    serve({ 'GET /api/v1/tasks': () => pending.promise });
    const store = useTasksStore();
    store.setSort('title', 'desc');
    store.tasks = [listed()];
    store.selectedListId = 'list-a';
    store.statusFilter = 'all';
    store.searchQuery = 'milk';
    store.summary = SUMMARY;
    store.total = 3;
    store.error = 'old';
    store.isSaving.add('task-x');
    const loading = store.fetchTasks();

    store.reset();
    pending.resolve({ tasks: [listed()], count: 1, total: 1 });
    await loading;

    expect(store.tasks).toEqual([]);
    expect(store.selectedListId).toBeNull();
    expect(store.statusFilter).toBe('open');
    expect(store.searchQuery).toBe('');
    expect(store.summary).toBeNull();
    expect(store.overdueCount).toBe(0);
    expect(store.total).toBe(0);
    expect(store.error).toBeNull();
    expect(store.isLoading).toBe(false);
    expect(store.isSaving.size).toBe(0);
    expect([store.sortBy, store.sortDir]).toEqual(['title', 'desc']);
  });
});

describe('reminder refresh hook (#169)', () => {
  const row = () => listed({ id: 'task-x' });
  const base = '/api/v1/calendars/list-a/tasks';
  const ok = () => ({
    [`POST ${base}`]: () => row(),
    [`PATCH ${base}/task-x`]: () => row(),
    [`POST ${base}/task-x/complete`]: () => row(),
    [`POST ${base}/task-x/uncomplete`]: () => row(),
    [`DELETE ${base}/task-x`]: () => undefined,
  });

  it('createTask, updateTask, toggleComplete, undoComplete and removeTask each ask for a refresh', async () => {
    serve(ok());
    const store = useTasksStore();
    store.tasks = [row()];

    await store.createTask('list-a', { summary: 'New' });
    expect(refreshMock).toHaveBeenCalledTimes(1);
    await store.updateTask(row(), { summary: 'Renamed' });
    expect(refreshMock).toHaveBeenCalledTimes(2);
    await store.toggleComplete(row());
    expect(refreshMock).toHaveBeenCalledTimes(3);
    await store.undoComplete(row());
    expect(refreshMock).toHaveBeenCalledTimes(4);
    await store.removeTask(row());
    expect(refreshMock).toHaveBeenCalledTimes(5);
  });

  it('a task that is already gone (404 on delete) still refreshes; other failures do not', async () => {
    const store = useTasksStore();
    serve({ [`DELETE ${base}/task-x`]: () => { throw httpError(404, 'Task not found'); } });
    await store.removeTask(row());
    expect(refreshMock).toHaveBeenCalledTimes(1);

    refreshMock.mockReset();
    serve({
      [`DELETE ${base}/task-x`]: () => { throw httpError(500, 'boom'); },
      [`PATCH ${base}/task-x`]: () => { throw httpError(500, 'boom'); },
      [`POST ${base}`]: () => { throw httpError(500, 'boom'); },
      [`POST ${base}/task-x/complete`]: () => { throw httpError(500, 'boom'); },
    });
    await store.removeTask(row());
    await store.updateTask(row(), { summary: 'x' });
    await store.createTask('list-a', { summary: 'x' });
    await store.toggleComplete(row());
    expect(refreshMock).not.toHaveBeenCalled();
  });
});
