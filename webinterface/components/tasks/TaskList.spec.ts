// @vitest-environment nuxt
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils';
import { createTestingPinia } from '@pinia/testing';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import { defineComponent, h, nextTick } from 'vue';
import TaskList from './TaskList.vue';
import { useCalendarStore } from '~/stores/calendars';
import { useTasksStore } from '~/stores/tasks';
import type { Calendar } from '~/types/calendar';
import type { Task, TaskStatusFilter, TasksSummary } from '~/types/tasks';

// SCOPE (#170 story 25, spec §7.3/§7.4/§7.5/§8): the grouped task list.
// Groups by status in a fixed order with empty ones hidden, Completed and
// Cancelled collapsible (collapsed by default; Completed open under Done),
// sort within groups, "Show N completed" under Open, "Showing N of total" when
// truncated, the loading / no-lists / empty / no-match / all-done / error
// states, inline completion (600 ms in place, then gone or moved; "Task
// completed" toast with Undo) and the keyboard (roving tabindex, Space,
// Enter, Delete). The stores run for real against a mocked useApi; the rows
// (TaskRow + TaskCheck) are mounted for real; PrimeVue's Menu and Button are
// stubs.

const { apiMock, toastMock, confirmMock } = vi.hoisted(() => ({
  apiMock: vi.fn(),
  toastMock: { success: vi.fn(), error: vi.fn(), warn: vi.fn(), info: vi.fn(), action: vi.fn() },
  confirmMock: { require: vi.fn(), close: vi.fn() },
}));
mockNuxtImport('useApi', () => () => apiMock);
mockNuxtImport('useAppToast', () => () => toastMock);
mockNuxtImport('useConfirm', () => () => confirmMock);

enableAutoUnmount(afterEach);

// Due labels and overdue follow the browser's zone: pin one (see utils/tasks.spec.ts).
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

const MenuStub = defineComponent({
  name: 'Menu',
  props: { model: { type: Array, default: () => [] }, id: { type: String, default: undefined }, popup: Boolean },
  setup(props, { expose }) {
    expose({ toggle: vi.fn() });
    return () => h('div', { 'data-test': 'menu-stub', id: props.id });
  },
});

const ButtonStub = defineComponent({
  name: 'Button',
  props: { label: { type: String, default: '' } },
  emits: ['click'],
  setup(props, { emit }) {
    return () => h('button', { type: 'button', onClick: (e: Event) => emit('click', e) }, props.label);
  },
});

interface ApiOptions {
  method?: string;
  query?: Record<string, string>;
  body?: Record<string, unknown>;
}
type Handler = (options: ApiOptions) => unknown;

function calendar(uuid: string, extra: Partial<Calendar> = {}): Calendar {
  return {
    id: uuid,
    uuid,
    path: `${uuid}.ics`,
    name: uuid,
    color: '#123456',
    kind: 'tasks',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...extra,
  };
}

const LISTS: Calendar[] = [
  calendar('personal', { name: 'Personal', kind: 'events' }),
  calendar('errands', { name: 'Errands', color: '#ff0000' }),
  calendar('home', { name: 'Home', color: '#00aa00', kind: 'both' }),
  calendar('team', { name: 'Team tasks', shared: true, permission: 'read-only' }),
];

const SUMMARY: TasksSummary = {
  open: 6,
  overdue: 1,
  due_today: 1,
  completed: 5,
  by_calendar: {
    errands: { open: 4, overdue: 1, completed: 4 },
    home: { open: 1, overdue: 0, completed: 1 },
    team: { open: 1, overdue: 0, completed: 0 },
  },
};

function task(id: string, overrides: Partial<Task> = {}): Task {
  return {
    id,
    calendar_id: 1,
    calendar_uuid: 'errands',
    uid: `${id}@calcard.io`,
    summary: id,
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
    etag: `etag-${id}`,
    created_at: '2026-11-01T10:00:00Z',
    updated_at: '2026-11-01T10:00:00Z',
    calendar_name: 'Errands',
    calendar_color: '#ff0000',
    ...overrides,
  };
}

const DONE: Partial<Task> = { status: 'completed', completed: true, completed_at: '2026-11-02T10:00:00Z', percent_complete: 100 };

/** One of each: two open (one overdue, one due today 17:00), one in progress, one completed, one cancelled. */
function mixedTasks(): Task[] {
  return [
    task('Send invoice', { due: '2026-11-03T16:00:00Z' }),
    task('Renew passport', { due_date: '2026-11-02', due_all_day: true }),
    task('Write tax return', { status: 'in-process', percent_complete: 40 }),
    task('Book dentist', DONE),
    task('Old idea', { status: 'cancelled', calendar_uuid: 'home', calendar_name: 'Home' }),
  ];
}

/** The server's answer to …/complete or …/uncomplete: the row flipped, without the list fields. */
function flipped(row: Task): Task {
  const { calendar_name: _n, calendar_color: _c, ...rest } = row;
  return row.completed
    ? { ...rest, status: 'needs-action', completed: false, completed_at: null, percent_complete: null }
    : { ...rest, ...DONE };
}

/** Answers by `METHOD url`; task writes answer the flipped/removed row; anything else fails loudly. */
function serve(handlers: Record<string, Handler> = {}) {
  apiMock.mockImplementation(async (url: string, options: ApiOptions = {}) => {
    const key = `${options.method ?? 'GET'} ${url}`;
    const handler = handlers[key];
    if (handler) return handler(options);
    if (key === 'GET /api/v1/tasks/summary') return SUMMARY;
    if (key === 'GET /api/v1/tasks') return { tasks: [], count: 0, total: 0 };
    // The undo of a tick is a PATCH of the status and percent the row had.
    const patch = /^PATCH \/api\/v1\/calendars\/[^/]+\/tasks\/([^/]+)$/.exec(key);
    if (patch) {
      const row = useTasksStore().tasks.find((t) => t.id === decodeURIComponent(patch[1]!));
      const status = options.body?.status as Task['status'];
      if (row) {
        return { ...row, status, completed: status === 'completed', completed_at: null, percent_complete: options.body?.percent_complete };
      }
    }
    const write = /^(POST|DELETE) \/api\/v1\/calendars\/[^/]+\/tasks\/([^/]+)(?:\/(complete|uncomplete))?$/.exec(key);
    if (write) {
      const row = useTasksStore().tasks.find((t) => t.id === decodeURIComponent(write[2]!));
      if (write[1] === 'DELETE') return null;
      if (row) return flipped({ ...row, completed: write[3] === 'uncomplete' });
    }
    throw new Error(`unexpected request ${key}`);
  });
}

const requests = () => apiMock.mock.calls.map(([url, options]) => `${(options as ApiOptions | undefined)?.method ?? 'GET'} ${url}`);
const count = (key: string) => requests().filter((r) => r === key).length;
const taskQueries = () =>
  apiMock.mock.calls
    .filter(([url, options]) => url === '/api/v1/tasks' && !(options as ApiOptions | undefined)?.method)
    .map(([, options]) => (options as ApiOptions).query);

interface MountOptions {
  tasks?: Task[];
  filter?: TaskStatusFilter;
  listId?: string | null;
  search?: string;
  summary?: TasksSummary | null;
  total?: number;
  calendars?: Calendar[];
  error?: string | null;
  loading?: boolean;
  listsLoading?: boolean;
  listsFailed?: boolean;
}

/** Seeds the stores as a page load would leave them and mounts the list into the document. */
function mountList(options: MountOptions = {}) {
  const pinia = createTestingPinia({ stubActions: false });
  useCalendarStore().calendars = options.calendars ?? LISTS;
  const store = useTasksStore();
  store.tasks = options.tasks ?? mixedTasks();
  store.total = options.total ?? store.tasks.length;
  store.statusFilter = options.filter ?? 'all';
  store.selectedListId = options.listId ?? null;
  store.searchQuery = options.search ?? '';
  store.summary = options.summary === undefined ? SUMMARY : options.summary;
  store.error = options.error ?? null;
  store.isLoading = options.loading ?? false;
  const wrapper = mount(TaskList, {
    props: { listsLoading: options.listsLoading ?? false, listsFailed: options.listsFailed ?? false },
    attachTo: document.body,
    global: { plugins: [pinia], stubs: { Menu: MenuStub, Button: ButtonStub } },
  });
  return { wrapper, store };
}

type Wrapper = ReturnType<typeof mountList>['wrapper'];

const groupKeys = (w: Wrapper) => w.findAll('[data-test="task-group"]').map((g) => g.attributes('data-group'));
const group = (w: Wrapper, key: string) => w.get(`[data-test="task-group"][data-group="${key}"]`);
const header = (w: Wrapper, key: string) => group(w, key).get('[data-test="group-header"]');
const rowIds = (w: Wrapper, key?: string) =>
  (key ? group(w, key) : w).findAll('li[data-task-id]').map((li) => li.attributes('data-task-id'));
const row = (w: Wrapper, id: string) => w.get(`li[data-task-id="${id}"]`);
const hasRow = (w: Wrapper, id: string) => w.find(`li[data-task-id="${id}"]`).exists();
const emptyState = (w: Wrapper) => w.find('[data-test="empty-state"]');

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((res) => {
    resolve = res;
  });
  return { promise, resolve };
}

beforeEach(() => {
  apiMock.mockReset();
  serve();
  Object.values(toastMock).forEach((fn) => fn.mockReset());
  confirmMock.require.mockReset();
});

afterEach(() => {
  vi.useRealTimers();
});

describe('TaskList — groups', () => {
  it('renders Needs action, In progress, Completed, Cancelled in that order', () => {
    const { wrapper } = mountList();

    expect(groupKeys(wrapper)).toEqual(['needs-action', 'in-process', 'completed', 'cancelled']);
    expect(header(wrapper, 'needs-action').text()).toBe('Needs action, 2 tasks');
    expect(header(wrapper, 'in-process').text()).toBe('In progress, 1 task');
    expect(header(wrapper, 'completed').text()).toBe('Completed, 1 task');
    expect(header(wrapper, 'cancelled').text()).toBe('Cancelled, 1 task');
  });

  it('hides empty groups', () => {
    const { wrapper } = mountList({ tasks: [task('a'), task('b', DONE)] });

    expect(groupKeys(wrapper)).toEqual(['needs-action', 'completed']);
  });

  it('sorts within each group: due ascending (overdue first), reversed by the direction, then by priority', async () => {
    const { wrapper, store } = mountList({
      tasks: [
        task('later', { due_date: '2026-11-20', due_all_day: true }),
        task('undated'),
        task('overdue', { due_date: '2026-11-01', due_all_day: true }),
        task('high', { priority: 1 }),
        task('progress', { status: 'in-process', due_date: '2026-11-04', due_all_day: true }),
      ],
    });

    expect(rowIds(wrapper, 'needs-action')).toEqual(['overdue', 'later', 'high', 'undated']);
    store.setSort('due', 'desc');
    await nextTick();
    expect(rowIds(wrapper, 'needs-action')).toEqual(['later', 'overdue', 'high', 'undated']);
    store.setSort('priority', 'asc');
    await nextTick();
    expect(rowIds(wrapper, 'needs-action')[0]).toBe('high');
    // Sorting never mixes groups.
    expect(rowIds(wrapper, 'in-process')).toEqual(['progress']);
  });

  it('starts Completed and Cancelled collapsed: a disclosure button, the rows not rendered', async () => {
    const { wrapper } = mountList();
    const toggle = header(wrapper, 'completed').get('button[data-test="group-toggle"]');
    const list = group(wrapper, 'completed').get('ul');

    expect(toggle.attributes('aria-expanded')).toBe('false');
    expect(toggle.attributes('aria-controls')).toBe(list.attributes('id'));
    expect(list.attributes('hidden')).toBeDefined();
    expect(rowIds(wrapper, 'completed')).toEqual([]);
    expect(header(wrapper, 'cancelled').get('button').attributes('aria-expanded')).toBe('false');
    expect(rowIds(wrapper, 'cancelled')).toEqual([]);

    await toggle.trigger('click');
    expect(toggle.attributes('aria-expanded')).toBe('true');
    expect(list.attributes('hidden')).toBeUndefined();
    expect(rowIds(wrapper, 'completed')).toEqual(['Book dentist']);

    await toggle.trigger('click');
    expect(rowIds(wrapper, 'completed')).toEqual([]);
  });

  it('the open groups are plain headings, always expanded', () => {
    const { wrapper } = mountList();

    expect(header(wrapper, 'needs-action').find('button').exists()).toBe(false);
    expect(header(wrapper, 'in-process').find('button').exists()).toBe(false);
    expect(rowIds(wrapper, 'needs-action')).toHaveLength(2);
  });

  it('mutes the Cancelled heading only', () => {
    const { wrapper } = mountList();

    expect(header(wrapper, 'cancelled').classes()).toEqual(expect.arrayContaining(['text-surface-500', 'dark:text-surface-400']));
    expect(header(wrapper, 'completed').classes()).toEqual(expect.arrayContaining(['text-surface-900', 'dark:text-surface-100']));
    expect(header(wrapper, 'needs-action').classes()).not.toContain('text-surface-500');
  });

  it('under Done, Completed starts open (it is the view) and Cancelled stays collapsed', async () => {
    const { wrapper, store } = mountList({ filter: 'completed', tasks: [task('done', DONE), task('dropped', { status: 'cancelled' })] });

    expect(groupKeys(wrapper)).toEqual(['completed', 'cancelled']);
    expect(rowIds(wrapper, 'completed')).toEqual(['done']);
    expect(rowIds(wrapper, 'cancelled')).toEqual([]);

    // Back to All: the defaults of that view (Completed collapsed again).
    store.statusFilter = 'all';
    await nextTick();
    expect(rowIds(wrapper, 'completed')).toEqual([]);
  });

  it('under Open there is no Completed group, even with completed rows still loaded', () => {
    const { wrapper } = mountList({ filter: 'open' });

    expect(groupKeys(wrapper)).toEqual(['needs-action', 'in-process']);
  });
});

describe('TaskList — semantics', () => {
  it('each group is an h2 naming the group and its count, over a <ul role="list"> of <li> labelled by it', () => {
    const { wrapper } = mountList();
    const h2 = header(wrapper, 'needs-action');
    const list = group(wrapper, 'needs-action').get('ul');

    expect(h2.element.tagName).toBe('H2');
    expect(h2.text()).toBe('Needs action, 2 tasks');
    // The visible count is the number alone.
    expect(h2.get('[data-test="group-count"]').text()).toBe('2');
    expect(list.attributes('role')).toBe('list');
    expect(list.attributes('aria-labelledby')).toBe(h2.attributes('id'));
    expect(list.findAll(':scope > li').length).toBe(2);
    // The collapsible heading's name comes from its button.
    expect(header(wrapper, 'completed').get('button').text()).toBe('Completed, 1 task');
  });
});

describe('TaskList — rows', () => {
  it('shows the list on each row only in All tasks', async () => {
    const { wrapper, store } = mountList({ filter: 'open' });
    expect(row(wrapper, 'Send invoice').find('[data-test="task-list"]').exists()).toBe(true);

    store.selectedListId = 'errands';
    await nextTick();
    expect(row(wrapper, 'Send invoice').find('[data-test="task-list"]').exists()).toBe(false);
  });

  it('marks rows of a read-only list read-only (no menu, the check disabled)', () => {
    const { wrapper } = mountList({ tasks: [task('mine'), task('theirs', { calendar_uuid: 'team' })] });

    expect(row(wrapper, 'mine').find('[data-test="row-menu"]').exists()).toBe(true);
    expect(row(wrapper, 'theirs').find('[data-test="row-menu"]').exists()).toBe(false);
    expect(row(wrapper, 'theirs').get('[data-test="task-check"]').attributes('aria-disabled')).toBe('true');
  });

  it('passes the saving state to the row', async () => {
    const { wrapper, store } = mountList({ tasks: [task('a')] });
    store.isSaving.add('a');
    await nextTick();

    expect(row(wrapper, 'a').attributes('aria-busy')).toBe('true');
  });

  it('ticks the clock on the minute: a task due 17:00 turns overdue at 17:00', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'Date'] });
    vi.setSystemTime(local(2026, 11, 3, 16, 59, 30));
    const { wrapper } = mountList({ tasks: [task('invoice', { due: '2026-11-03T16:00:00Z' })] });
    expect(row(wrapper, 'invoice').find('[data-test="overdue-bar"]').exists()).toBe(false);

    vi.advanceTimersByTime(29_999);
    await nextTick();
    expect(row(wrapper, 'invoice').find('[data-test="overdue-bar"]').exists()).toBe(false);

    // The tick at 17:00:00 is not overdue yet (due < now is strict); the one
    // at 17:01:00 is, and none comes in between (ticks sit on the minute).
    vi.advanceTimersByTime(60_000);
    await nextTick();
    expect(row(wrapper, 'invoice').find('[data-test="overdue-bar"]').exists()).toBe(false);
    vi.advanceTimersByTime(1);
    await nextTick();
    expect(row(wrapper, 'invoice').find('[data-test="overdue-bar"]').exists()).toBe(true);
  });
});

describe('TaskList — "Show N completed" and truncation', () => {
  it('under Open in All tasks offers "Show N completed" from summary.completed, which switches to Done', async () => {
    const { wrapper, store } = mountList({ filter: 'open' });
    const link = wrapper.get('[data-test="show-completed"]');

    expect(link.element.tagName).toBe('BUTTON');
    expect(link.text()).toBe('Show 5 completed');

    await link.trigger('click');
    await flushPromises();
    expect(store.statusFilter).toBe('completed');
    expect(taskQueries()).toEqual([{ status: 'completed' }]);
  });

  it('counts the selected list\'s done tasks in a single list, and drops the number for a search (the summary ignores it)', async () => {
    const { wrapper, store } = mountList({ filter: 'open', listId: 'errands' });
    expect(wrapper.get('[data-test="show-completed"]').text()).toBe('Show 4 completed');

    store.selectedListId = null;
    store.searchQuery = 'invoice';
    await nextTick();
    expect(wrapper.get('[data-test="show-completed"]').text()).toBe('Show completed');
  });

  it('offers no link in a list that has nothing completed, although other lists do (MN13)', () => {
    const { wrapper } = mountList({ filter: 'open', listId: 'team' });

    expect(wrapper.find('[data-test="show-completed"]').exists()).toBe(false);
  });

  it('offers no link when nothing is completed, before the summary, or outside Open', async () => {
    const none = mountList({ filter: 'open', summary: { ...SUMMARY, completed: 0 } }).wrapper;
    expect(none.find('[data-test="show-completed"]').exists()).toBe(false);

    const noSummary = mountList({ filter: 'open', summary: null }).wrapper;
    expect(noSummary.find('[data-test="show-completed"]').exists()).toBe(false);

    const all = mountList({ filter: 'all' }).wrapper;
    expect(all.find('[data-test="show-completed"]').exists()).toBe(false);
  });

  it('shows "Showing {count} of {total}" when the server has more than were loaded', () => {
    const { wrapper } = mountList({ total: 750 });

    expect(wrapper.get('[data-test="truncated"]').text()).toBe('Showing 5 of 750');
  });

  it('shows no such note when everything is loaded', () => {
    const { wrapper } = mountList();

    expect(wrapper.find('[data-test="truncated"]').exists()).toBe(false);
    expect(wrapper.find('[data-test="list-footer"]').exists()).toBe(false);
  });
});

describe('TaskList — inline completion', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'Date'] });
    vi.setSystemTime(local(2026, 11, 3, 9, 30));
  });

  it('under Open a ticked row stays in place, ticked, for 600 ms and then leaves', async () => {
    const { wrapper } = mountList({ filter: 'open' });
    const before = row(wrapper, 'Send invoice').element;

    await row(wrapper, 'Send invoice').get('[data-test="task-check"]').trigger('click');
    await flushPromises();

    // The SAME element, now ticked and struck: the check and strike transitions play.
    expect(row(wrapper, 'Send invoice').element).toBe(before);
    expect(row(wrapper, 'Send invoice').get('[data-test="task-check"]').attributes('aria-checked')).toBe('true');
    expect(row(wrapper, 'Send invoice').get('[data-test="task-title-text"]').attributes('data-struck')).toBe('true');
    expect(rowIds(wrapper, 'needs-action')).toEqual(['Renew passport', 'Send invoice']);
    expect(count('POST /api/v1/calendars/errands/tasks/Send invoice/complete')).toBe(1);

    vi.advanceTimersByTime(599);
    await nextTick();
    expect(hasRow(wrapper, 'Send invoice')).toBe(true);

    vi.advanceTimersByTime(1);
    await nextTick();
    expect(hasRow(wrapper, 'Send invoice')).toBe(false);
    expect(header(wrapper, 'needs-action').text()).toBe('Needs action, 1 task');
  });

  it('offers "Task completed" with Undo (5 s, the toast default), and Undo brings the row back', async () => {
    const { wrapper } = mountList({ filter: 'open' });

    await row(wrapper, 'Send invoice').get('[data-test="task-check"]').trigger('click');
    await flushPromises();
    vi.advanceTimersByTime(600);
    await nextTick();

    expect(toastMock.action).toHaveBeenCalledTimes(1);
    const [summary, label, undo, life] = toastMock.action.mock.calls[0]!;
    expect([summary, label, life]).toEqual(['Task completed', 'Undo', undefined]);

    (undo as () => void)();
    await flushPromises();
    expect(count('PATCH /api/v1/calendars/errands/tasks/Send invoice')).toBe(1);
    expect(count('POST /api/v1/calendars/errands/tasks/Send invoice/uncomplete')).toBe(0);
    expect(hasRow(wrapper, 'Send invoice')).toBe(true);
    expect(row(wrapper, 'Send invoice').get('[data-test="task-check"]').attributes('aria-checked')).toBe('false');
  });

  it.each([
    ['an in-progress task at 40 %', { status: 'in-process', percent_complete: 40 }, { status: 'in-process', percent_complete: 40 }],
    ['a needs-action task without a percentage', {}, { status: 'needs-action', percent_complete: null }],
  ] as const)('Undo restores the status and percent %s had before the tick (MN9)', async (_name, before, patch) => {
    const { wrapper } = mountList({ filter: 'open', tasks: [task('Send invoice', before as Partial<Task>)] });
    await row(wrapper, 'Send invoice').get('[data-test="task-check"]').trigger('click');
    await flushPromises();
    const undo = toastMock.action.mock.calls[0]![2] as () => void;
    apiMock.mockClear();

    undo();
    await flushPromises();

    const calls = apiMock.mock.calls.filter(([, o]) => (o as ApiOptions | undefined)?.method === 'PATCH');
    expect(calls).toHaveLength(1);
    expect(calls[0]![0]).toBe('/api/v1/calendars/errands/tasks/Send invoice');
    expect((calls[0]![1] as ApiOptions).body).toEqual(patch);
    expect(requests().some((r) => r.endsWith('/uncomplete'))).toBe(false);
  });

  it('Undo puts a cancelled task back as cancelled (MN9)', async () => {
    const { wrapper } = mountList({ filter: 'completed', tasks: [task('Send invoice', { status: 'cancelled' })] });
    await header(wrapper, 'cancelled').get('button').trigger('click');
    await row(wrapper, 'Send invoice').get('[data-test="task-check"]').trigger('click');
    await flushPromises();
    const undo = toastMock.action.mock.calls[0]![2] as () => void;
    apiMock.mockClear();

    undo();
    await flushPromises();

    const calls = apiMock.mock.calls.filter(([, o]) => (o as ApiOptions | undefined)?.method === 'PATCH');
    expect(calls.map(([, o]) => (o as ApiOptions).body)).toEqual([{ status: 'cancelled', percent_complete: null }]);
  });

  it('Undo does nothing when the task was already unticked another way', async () => {
    const { wrapper, store } = mountList({ filter: 'all' });

    await row(wrapper, 'Send invoice').get('[data-test="task-check"]').trigger('click');
    await flushPromises();
    const undo = toastMock.action.mock.calls[0]![2] as () => void;
    await store.toggleComplete(store.tasks.find((t) => t.id === 'Send invoice')!);
    apiMock.mockClear();

    undo();
    await flushPromises();
    expect(requests().filter((r) => r.startsWith('POST'))).toEqual([]);
  });

  it('under All the ticked row moves into Completed after 600 ms', async () => {
    const { wrapper } = mountList({ filter: 'all' });

    await row(wrapper, 'Send invoice').get('[data-test="task-check"]').trigger('click');
    await flushPromises();
    expect(rowIds(wrapper, 'needs-action')).toContain('Send invoice');
    expect(header(wrapper, 'completed').text()).toBe('Completed, 1 task');

    vi.advanceTimersByTime(600);
    await nextTick();
    expect(rowIds(wrapper, 'needs-action')).not.toContain('Send invoice');
    expect(header(wrapper, 'completed').text()).toBe('Completed, 2 tasks');
    await header(wrapper, 'completed').get('button').trigger('click');
    expect(rowIds(wrapper, 'completed')).toContain('Send invoice');
  });

  it('under Done a ticked cancelled task moves from Cancelled into the open Completed group', async () => {
    const { wrapper } = mountList({ filter: 'completed', tasks: [task('done', DONE), task('dropped', { status: 'cancelled' })] });
    await header(wrapper, 'cancelled').get('button').trigger('click');

    await row(wrapper, 'dropped').get('[data-test="task-check"]').trigger('click');
    await flushPromises();
    expect(rowIds(wrapper, 'cancelled')).toEqual(['dropped']);

    vi.advanceTimersByTime(600);
    await nextTick();
    expect(rowIds(wrapper, 'completed')).toEqual(['done', 'dropped']);
    expect(groupKeys(wrapper)).toEqual(['completed']);
  });

  it('a held row is let go when the view changes (it belongs to the old list)', async () => {
    const { wrapper, store } = mountList({ filter: 'open' });

    await row(wrapper, 'Send invoice').get('[data-test="task-check"]').trigger('click');
    await flushPromises();
    store.selectedListId = 'home';
    await nextTick();

    expect(hasRow(wrapper, 'Send invoice')).toBe(false);
  });

  it('unticking shows no toast', async () => {
    const { wrapper } = mountList({ filter: 'completed', tasks: [task('done', DONE)] });

    await row(wrapper, 'done').get('[data-test="task-check"]').trigger('click');
    await flushPromises();

    expect(count('POST /api/v1/calendars/errands/tasks/done/uncomplete')).toBe(1);
    expect(toastMock.action).not.toHaveBeenCalled();
  });

  it('a failed tick shows no Undo and the row stays', async () => {
    serve({
      'POST /api/v1/calendars/errands/tasks/Send invoice/complete': () => {
        throw Object.assign(new Error('500'), { status: 500, statusCode: 500 });
      },
    });
    const { wrapper } = mountList({ filter: 'open' });

    await row(wrapper, 'Send invoice').get('[data-test="task-check"]').trigger('click');
    await flushPromises();
    vi.advanceTimersByTime(600);
    await nextTick();

    expect(toastMock.action).not.toHaveBeenCalled();
    expect(hasRow(wrapper, 'Send invoice')).toBe(true);
    expect(row(wrapper, 'Send invoice').get('[data-test="task-check"]').attributes('aria-checked')).toBe('false');
  });

  it('the menu\'s Delete task asks first (the store\'s confirmation)', async () => {
    const { wrapper } = mountList({ filter: 'open' });
    const items = row(wrapper, 'Send invoice').getComponent(MenuStub).props('model') as { label: string; command: () => void }[];

    items.find((i) => i.label === 'Delete task')!.command();
    await flushPromises();

    expect(confirmMock.require).toHaveBeenCalledTimes(1);
    expect(count('DELETE /api/v1/calendars/errands/tasks/Send invoice')).toBe(0);
  });
});

// Story 29 (spec §8): the polite live region. "Task completed" and "Task
// deleted" are toasts (PrimeVue: role="alert"), so only the untick is said here.
describe('TaskList — announcements', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'Date'] });
    vi.setSystemTime(local(2026, 11, 3, 9, 30));
  });

  const announcer = (w: Wrapper) => w.get('[data-test="task-announcer"]');

  it('is a polite, atomic, visually hidden status region outside the busy list, empty at first', () => {
    const { wrapper } = mountList({ loading: true });
    const region = announcer(wrapper);

    expect(region.attributes()).toEqual(expect.objectContaining({ role: 'status', 'aria-live': 'polite', 'aria-atomic': 'true' }));
    expect(region.classes()).toContain('sr-only');
    expect(region.text()).toBe('');
    // aria-busy on an ancestor may hold an announcement back.
    expect(wrapper.get('[data-test="task-list-view"]').attributes('aria-busy')).toBe('true');
    expect(wrapper.get('[data-test="task-list-view"]').element.contains(region.element)).toBe(false);
    expect(region.element.closest('[aria-busy]')).toBeNull();
  });

  it.each([
    ['the skeleton', { tasks: [], loading: true }],
    ['"No task lists yet"', { tasks: [], calendars: [] }],
    ['an empty list', { tasks: [], filter: 'open' as TaskStatusFilter }],
    ['an error', { tasks: [], error: 'Couldn\'t load tasks.' }],
  ])('is there with %s (a region must exist before it speaks)', (_name, options) => {
    const { wrapper } = mountList(options);
    expect(wrapper.find('[data-test="task-announcer"][aria-live="polite"]').exists()).toBe(true);
  });

  it('an untick says "Task marked as not done" (after the region was emptied), and no toast', async () => {
    const { wrapper } = mountList({ filter: 'completed', tasks: [task('done', DONE)] });

    await row(wrapper, 'done').get('[data-test="task-check"]').trigger('click');
    await flushPromises();
    expect(count('POST /api/v1/calendars/errands/tasks/done/uncomplete')).toBe(1);

    vi.advanceTimersByTime(99);
    await nextTick();
    expect(announcer(wrapper).text()).toBe('');
    vi.advanceTimersByTime(1);
    await nextTick();
    expect(announcer(wrapper).text()).toBe('Task marked as not done');
    expect(toastMock.action).not.toHaveBeenCalled();
    expect(toastMock.success).not.toHaveBeenCalled();

    // Emptied 5 s later: no stale sentence for a reading cursor to find.
    vi.advanceTimersByTime(4999);
    await nextTick();
    expect(announcer(wrapper).text()).toBe('Task marked as not done');
    vi.advanceTimersByTime(1);
    await nextTick();
    expect(announcer(wrapper).text()).toBe('');
  });

  it('a completion is left to its toast (no second announcement)', async () => {
    const { wrapper } = mountList({ filter: 'open' });

    await row(wrapper, 'Send invoice').get('[data-test="task-check"]').trigger('click');
    await flushPromises();
    vi.advanceTimersByTime(1000);
    await nextTick();

    expect(toastMock.action).toHaveBeenCalledWith('Task completed', 'Undo', expect.any(Function));
    expect(announcer(wrapper).text()).toBe('');
  });

  it('Undo is announced like an untick', async () => {
    const { wrapper } = mountList({ filter: 'open' });

    await row(wrapper, 'Send invoice').get('[data-test="task-check"]').trigger('click');
    await flushPromises();
    (toastMock.action.mock.calls[0]![2] as () => void)();
    await flushPromises();
    vi.advanceTimersByTime(100);
    await nextTick();

    expect(count('PATCH /api/v1/calendars/errands/tasks/Send invoice')).toBe(1);
    expect(announcer(wrapper).text()).toBe('Task marked as not done');
  });

  it('a delete is left to its "Task deleted" toast', async () => {
    const { wrapper } = mountList({ filter: 'open' });
    (row(wrapper, 'Send invoice').element as HTMLElement).focus();

    await row(wrapper, 'Send invoice').trigger('keydown', { key: 'Delete' });
    (confirmMock.require.mock.calls[0]![0] as { accept: () => void }).accept();
    await flushPromises();
    vi.advanceTimersByTime(1000);
    await nextTick();

    expect(toastMock.success).toHaveBeenCalledWith('Task deleted');
    expect(announcer(wrapper).text()).toBe('');
  });

  it('the menu\'s "Mark as not done" and Space on the row announce it too', async () => {
    const { wrapper } = mountList({ filter: 'completed', tasks: [task('a', DONE), task('b', DONE)] });
    const items = row(wrapper, 'a').getComponent(MenuStub).props('model') as { label: string; command: () => void }[];

    items.find((i) => i.label === 'Mark as not done')!.command();
    await flushPromises();
    vi.advanceTimersByTime(100);
    await nextTick();
    expect(announcer(wrapper).text()).toBe('Task marked as not done');

    // The same words again: emptied first, so a screen reader hears them twice.
    (row(wrapper, 'b').element as HTMLElement).focus();
    await row(wrapper, 'b').trigger('keydown', { key: ' ' });
    await flushPromises();
    expect(announcer(wrapper).text()).toBe('');
    vi.advanceTimersByTime(100);
    await nextTick();
    expect(announcer(wrapper).text()).toBe('Task marked as not done');
    expect(count('POST /api/v1/calendars/errands/tasks/b/uncomplete')).toBe(1);
  });

  it('a failed untick announces nothing (the store toasts the error)', async () => {
    serve({
      'POST /api/v1/calendars/errands/tasks/done/uncomplete': () => {
        throw Object.assign(new Error('500'), { status: 500, statusCode: 500 });
      },
    });
    const { wrapper } = mountList({ filter: 'completed', tasks: [task('done', DONE)] });

    await row(wrapper, 'done').get('[data-test="task-check"]').trigger('click');
    await flushPromises();
    vi.advanceTimersByTime(1000);
    await nextTick();

    expect(announcer(wrapper).text()).toBe('');
    expect(row(wrapper, 'done').get('[data-test="task-check"]').attributes('aria-checked')).toBe('true');
  });
});

describe('TaskList — states', () => {
  it('loading with nothing to show: the six-row skeleton, no empty state', () => {
    const { wrapper } = mountList({ tasks: [], loading: true });
    const skeleton = wrapper.get('[data-test="list-skeleton"]');

    expect(skeleton.findAll('.animate-pulse')).toHaveLength(6);
    expect(skeleton.attributes('role')).toBe('status');
    expect(skeleton.text()).toBe('Loading tasks');
    expect(emptyState(wrapper).exists()).toBe(false);
  });

  it('loading with rows already shown keeps the rows (no flicker on each search keystroke)', () => {
    const { wrapper } = mountList({ loading: true });

    expect(wrapper.find('[data-test="list-skeleton"]').exists()).toBe(false);
    expect(rowIds(wrapper)).not.toEqual([]);
    expect(wrapper.get('[data-test="task-list-view"]').attributes('aria-busy')).toBe('true');
  });

  it('while the page loads the lists: the skeleton, never "No task lists yet"', () => {
    const { wrapper } = mountList({ tasks: [], calendars: [], listsLoading: true });

    expect(wrapper.find('[data-test="list-skeleton"]').exists()).toBe(true);
    expect(emptyState(wrapper).exists()).toBe(false);
  });

  it('no task lists: the no-lists state with its copy and "Create task list"', async () => {
    const { wrapper } = mountList({ tasks: [], calendars: [calendar('personal', { kind: 'events' })] });
    const state = emptyState(wrapper);

    expect(state.attributes('data-variant')).toBe('no-lists');
    expect(state.get('[data-test="empty-heading"]').text()).toBe('No task lists yet');
    expect(state.get('[data-test="empty-text"]').text()).toBe(
      'Create a list to start adding tasks. Task apps on your phone will find it too.',
    );
    expect(state.get('[data-test="empty-action"]').text()).toBe('Create task list');

    await state.get('[data-test="empty-action"]').trigger('click');
    expect(wrapper.emitted('add-list')).toHaveLength(1);
  });

  it('a mixed legacy calendar counts as a task list: no no-lists state', () => {
    const { wrapper } = mountList({ tasks: [], filter: 'open', calendars: [calendar('old', { kind: 'both' })], summary: { ...SUMMARY, completed: 0 } });

    expect(emptyState(wrapper).attributes('data-variant')).toBe('empty');
  });

  it('lists that failed to load: an error banner, not "No task lists yet"', async () => {
    const { wrapper } = mountList({ tasks: [], calendars: [], listsFailed: true });

    expect(emptyState(wrapper).exists()).toBe(false);
    expect(wrapper.get('[data-test="load-error-text"]').text()).toBe(
      "Couldn't load your task lists. Check your connection and try again.",
    );
    await wrapper.get('[data-test="retry"]').trigger('click');
    expect(wrapper.emitted('retry')).toHaveLength(1);
  });

  it('an empty list with nothing completed: "Nothing to do here", and no "Show completed" (MN13)', () => {
    const { wrapper } = mountList({ tasks: [], filter: 'open', listId: 'team' });
    const state = emptyState(wrapper);

    expect(state.attributes('data-variant')).toBe('empty');
    expect(state.get('[data-test="empty-heading"]').text()).toBe('Nothing to do here');
    expect(state.find('[data-test="empty-action"]').exists()).toBe(false);
  });

  it('a single list with only completed tasks: "All caught up" and "Show completed" (spec §7.5, MN13)', async () => {
    const { wrapper, store } = mountList({ tasks: [], filter: 'open', listId: 'errands' });
    const state = emptyState(wrapper);

    expect(state.attributes('data-variant')).toBe('all-done');
    expect(state.get('[data-test="empty-heading"]').text()).toBe('All caught up');
    await state.get('[data-test="empty-action"]').trigger('click');
    await flushPromises();
    expect(store.statusFilter).toBe('completed');
    expect(taskQueries()).toEqual([{ status: 'completed', calendar_uuid: 'errands' }]);
  });

  it('All with no rows and no search is an empty list too', () => {
    const { wrapper } = mountList({ tasks: [], filter: 'all' });

    expect(emptyState(wrapper).get('[data-test="empty-heading"]').text()).toBe('Nothing to do here');
  });

  it('a search that matches nothing: "No tasks match" and "Clear filters" (one load back to Open)', async () => {
    const { wrapper, store } = mountList({ tasks: [], filter: 'all', search: 'zebra', listId: 'errands' });
    const state = emptyState(wrapper);

    expect(state.attributes('data-variant')).toBe('no-match');
    expect(state.get('[data-test="empty-heading"]').text()).toBe('No tasks match');
    expect(state.get('[data-test="empty-action"]').text()).toBe('Clear filters');

    await state.get('[data-test="empty-action"]').trigger('click');
    await flushPromises();
    expect(store.searchQuery).toBe('');
    expect(store.statusFilter).toBe('open');
    expect(taskQueries()).toEqual([{ status: 'open', calendar_uuid: 'errands' }]);
  });

  it('the Done filter with nothing done is "No tasks match"', () => {
    const { wrapper } = mountList({ tasks: [], filter: 'completed' });

    expect(emptyState(wrapper).attributes('data-variant')).toBe('no-match');
  });

  it('All tasks, Open, nothing open but tasks completed: "All caught up" and "Show completed"', async () => {
    const { wrapper, store } = mountList({ tasks: [], filter: 'open' });
    const state = emptyState(wrapper);

    expect(state.attributes('data-variant')).toBe('all-done');
    expect(state.get('[data-test="empty-heading"]').text()).toBe('All caught up');
    const link = state.get('[data-test="empty-action"]');
    expect(link.element.tagName).toBe('BUTTON');
    expect(link.text()).toBe('Show completed');

    await link.trigger('click');
    await flushPromises();
    expect(store.statusFilter).toBe('completed');
    expect(taskQueries()).toEqual([{ status: 'completed' }]);
  });

  it('a load error: the banner with "Try again" on top, the previous rows still below', async () => {
    const { wrapper } = mountList({ error: "Couldn't load tasks. Check your connection and try again." });
    const banner = wrapper.get('[data-test="load-error"]');

    expect(banner.attributes('role')).toBe('alert');
    expect(wrapper.get('[data-test="load-error-text"]').text()).toBe("Couldn't load tasks. Check your connection and try again.");
    expect(banner.get('[data-test="retry"]').text()).toBe('Try again');
    expect(rowIds(wrapper)).toContain('Send invoice');
    // The banner sits above the groups.
    const html = wrapper.html();
    expect(html.indexOf('data-test="load-error"')).toBeLessThan(html.indexOf('data-test="task-groups"'));

    await banner.get('[data-test="retry"]').trigger('click');
    expect(wrapper.emitted('retry')).toHaveLength(1);
  });

  it('a load error with nothing loaded is never shown as an empty list', () => {
    const { wrapper } = mountList({ tasks: [], filter: 'open', error: "Couldn't load tasks. Check your connection and try again." });

    expect(wrapper.find('[data-test="load-error"]').exists()).toBe(true);
    expect(emptyState(wrapper).exists()).toBe(false);
  });
});

describe('TaskList — keyboard', () => {
  /** The rows of the default mix under Open, in screen order. */
  const OPEN_ORDER = ['Renew passport', 'Send invoice', 'Write tax return'];

  const focused = () => (document.activeElement as HTMLElement | null)?.dataset?.taskId ?? document.activeElement?.tagName;

  it('one row is in the Tab order (the first), with only its own check and menu', () => {
    const { wrapper } = mountList({ filter: 'open' });

    expect(rowIds(wrapper)).toEqual(OPEN_ORDER);
    expect(OPEN_ORDER.map((id) => row(wrapper, id).attributes('tabindex'))).toEqual(['0', '-1', '-1']);
    expect(row(wrapper, OPEN_ORDER[0]!).get('[data-test="task-check"]').attributes('tabindex')).toBeUndefined();
    expect(row(wrapper, OPEN_ORDER[0]!).get('[data-test="row-menu"]').attributes('tabindex')).toBeUndefined();
    expect(row(wrapper, OPEN_ORDER[1]!).get('[data-test="task-check"]').attributes('tabindex')).toBe('-1');
    expect(row(wrapper, OPEN_ORDER[1]!).get('[data-test="row-menu"]').attributes('tabindex')).toBe('-1');
    // A visible focus ring on the row itself.
    expect(row(wrapper, OPEN_ORDER[0]!).classes()).toEqual(expect.arrayContaining(['focus-visible:ring-2', 'focus-visible:ring-primary-500']));
  });

  it('ArrowDown/ArrowUp move between rows across groups and carry the Tab stop along', async () => {
    const { wrapper } = mountList({ filter: 'open' });
    (row(wrapper, 'Renew passport').element as HTMLElement).focus();

    await row(wrapper, 'Renew passport').trigger('keydown', { key: 'ArrowDown' });
    await nextTick();
    expect(focused()).toBe('Send invoice');
    // Into the next group.
    await row(wrapper, 'Send invoice').trigger('keydown', { key: 'ArrowDown' });
    await nextTick();
    expect(focused()).toBe('Write tax return');
    expect(row(wrapper, 'Write tax return').attributes('tabindex')).toBe('0');
    expect(row(wrapper, 'Renew passport').attributes('tabindex')).toBe('-1');
    // The last row: nowhere to go.
    await row(wrapper, 'Write tax return').trigger('keydown', { key: 'ArrowDown' });
    await nextTick();
    expect(focused()).toBe('Write tax return');

    await row(wrapper, 'Write tax return').trigger('keydown', { key: 'ArrowUp' });
    await nextTick();
    expect(focused()).toBe('Send invoice');
  });

  it('skips the rows of a collapsed group', async () => {
    const { wrapper } = mountList({ filter: 'all' });
    (row(wrapper, 'Write tax return').element as HTMLElement).focus();

    await row(wrapper, 'Write tax return').trigger('keydown', { key: 'ArrowDown' });
    await nextTick();
    expect(focused()).toBe('Write tax return');
    // The Tab stop stays on a rendered row (never on a hidden one).
    expect(row(wrapper, 'Write tax return').attributes('tabindex')).toBe('0');

    await header(wrapper, 'completed').get('button').trigger('click');
    await row(wrapper, 'Write tax return').trigger('keydown', { key: 'ArrowDown' });
    await nextTick();
    expect(focused()).toBe('Book dentist');
  });

  it('the arrows also work from the check inside a row', async () => {
    const { wrapper } = mountList({ filter: 'open' });
    const check = row(wrapper, 'Renew passport').get('[data-test="task-check"]');
    (check.element as HTMLElement).focus();

    await check.trigger('keydown', { key: 'ArrowDown' });
    await nextTick();
    expect(focused()).toBe('Send invoice');
  });

  it('Space on a row ticks it, once', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'Date'] });
    vi.setSystemTime(local(2026, 11, 3, 9, 30));
    const { wrapper } = mountList({ filter: 'open' });
    (row(wrapper, 'Send invoice').element as HTMLElement).focus();

    await row(wrapper, 'Send invoice').trigger('keydown', { key: ' ' });
    await flushPromises();
    // A held key repeats; the answer is already in, so only the repeat guard stops an untick.
    await row(wrapper, 'Send invoice').trigger('keydown', { key: ' ', repeat: true });
    await flushPromises();

    expect(count('POST /api/v1/calendars/errands/tasks/Send invoice/complete')).toBe(1);
    expect(count('POST /api/v1/calendars/errands/tasks/Send invoice/uncomplete')).toBe(0);
    expect(toastMock.action).toHaveBeenCalledTimes(1);
  });

  it('Space on the check toggles once (the check handles it, the list does not again)', async () => {
    const { wrapper } = mountList({ filter: 'open' });
    const check = row(wrapper, 'Send invoice').get('[data-test="task-check"]');
    (check.element as HTMLElement).focus();

    await check.trigger('keydown', { key: ' ' });
    await flushPromises();

    expect(count('POST /api/v1/calendars/errands/tasks/Send invoice/complete')).toBe(1);
  });

  it('Space does nothing on a read-only or a repeating task', async () => {
    const { wrapper } = mountList({
      filter: 'open',
      tasks: [task('theirs', { calendar_uuid: 'team' }), task('weekly', { is_recurring: true, rrule: 'FREQ=WEEKLY' })],
    });

    await row(wrapper, 'theirs').trigger('keydown', { key: ' ' });
    await row(wrapper, 'weekly').trigger('keydown', { key: ' ' });
    await flushPromises();

    expect(requests().filter((r) => r.startsWith('POST'))).toEqual([]);
  });

  it('Enter on a row asks to open the task', async () => {
    const { wrapper, store } = mountList({ filter: 'open' });

    await row(wrapper, 'Send invoice').trigger('keydown', { key: 'Enter' });

    expect(wrapper.emitted('open')).toEqual([[store.tasks.find((t) => t.id === 'Send invoice')]]);
    expect(requests().filter((r) => r.startsWith('POST'))).toEqual([]);
  });

  it('Enter on the check ticks it and opens nothing', async () => {
    const { wrapper } = mountList({ filter: 'open' });

    await row(wrapper, 'Send invoice').get('[data-test="task-check"]').trigger('keydown', { key: 'Enter' });
    await flushPromises();

    expect(wrapper.emitted('open')).toBeUndefined();
    expect(count('POST /api/v1/calendars/errands/tasks/Send invoice/complete')).toBe(1);
  });

  it('Delete (and Backspace) ask first, delete on accept, and the next row takes the focus', async () => {
    const { wrapper } = mountList({ filter: 'open' });
    (row(wrapper, 'Send invoice').element as HTMLElement).focus();

    await row(wrapper, 'Send invoice').trigger('keydown', { key: 'Delete' });
    expect(confirmMock.require).toHaveBeenCalledTimes(1);
    expect(count('DELETE /api/v1/calendars/errands/tasks/Send invoice')).toBe(0);

    (confirmMock.require.mock.calls[0]![0] as { accept: () => void }).accept();
    await flushPromises();
    expect(count('DELETE /api/v1/calendars/errands/tasks/Send invoice')).toBe(1);
    expect(hasRow(wrapper, 'Send invoice')).toBe(false);
    expect(focused()).toBe('Write tax return');

    await row(wrapper, 'Write tax return').trigger('keydown', { key: 'Backspace' });
    expect(confirmMock.require).toHaveBeenCalledTimes(2);
  });

  it('Delete on a read-only row asks nothing', async () => {
    const { wrapper } = mountList({ tasks: [task('theirs', { calendar_uuid: 'team' })] });

    await row(wrapper, 'theirs').trigger('keydown', { key: 'Delete' });

    expect(confirmMock.require).not.toHaveBeenCalled();
  });

  it('a row ticked with Space under Open hands the focus to the next row when it leaves', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'Date'] });
    vi.setSystemTime(local(2026, 11, 3, 9, 30));
    const { wrapper } = mountList({ filter: 'open' });
    (row(wrapper, 'Renew passport').element as HTMLElement).focus();

    await row(wrapper, 'Renew passport').trigger('keydown', { key: ' ' });
    await flushPromises();
    expect(focused()).toBe('Renew passport');

    vi.advanceTimersByTime(600);
    await nextTick();
    await nextTick();
    expect(hasRow(wrapper, 'Renew passport')).toBe(false);
    expect(focused()).toBe('Send invoice');
    expect(row(wrapper, 'Send invoice').attributes('tabindex')).toBe('0');
  });

  // Story 29: found in a real browser. Whether removing the focused element
  // fires blur/focusout (and whether the element is still connected then)
  // differs between browsers, and a browser without focus events fires none.
  // The hand-over reads document.activeElement before the rows change instead.
  it('hands the focus over although the browser said focusout (still connected, to nowhere) as the row went', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'Date'] });
    vi.setSystemTime(local(2026, 11, 3, 9, 30));
    const { wrapper } = mountList({ filter: 'open' });
    const li = row(wrapper, 'Renew passport').element as HTMLElement;
    li.focus();
    await row(wrapper, 'Renew passport').trigger('keydown', { key: ' ' });
    await flushPromises();

    li.dispatchEvent(new FocusEvent('focusout', { bubbles: true, relatedTarget: null }));
    vi.advanceTimersByTime(600);
    await nextTick();
    await nextTick();

    expect(hasRow(wrapper, 'Renew passport')).toBe(false);
    expect(focused()).toBe('Send invoice');
  });

  it('hands the focus over after a confirmed delete with no focus events at all', async () => {
    const { wrapper } = mountList({ filter: 'open' });
    const li = row(wrapper, 'Send invoice').element as HTMLElement;
    // Focus without the focusin the list would normally hear (as in a browser window that is not focused).
    const stop = (e: Event) => e.stopImmediatePropagation();
    document.addEventListener('focusin', stop, true);
    try {
      li.focus();
      await row(wrapper, 'Send invoice').trigger('keydown', { key: 'Delete' });
      (confirmMock.require.mock.calls[0]![0] as { accept: () => void }).accept();
      await flushPromises();
    } finally {
      document.removeEventListener('focusin', stop, true);
    }

    expect(hasRow(wrapper, 'Send invoice')).toBe(false);
    expect(focused()).toBe('Write tax return');
  });

  it('a mouse tick with nothing focused does not pull the focus into the list when the row leaves', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'Date'] });
    vi.setSystemTime(local(2026, 11, 3, 9, 30));
    const { wrapper } = mountList({ filter: 'open' });
    // The list was used from the keyboard before (the row holds the Tab stop),
    // then a click on blank space left nothing focused.
    (row(wrapper, 'Renew passport').element as HTMLElement).focus();
    (document.activeElement as HTMLElement | null)?.blur();
    expect(document.activeElement).toBe(document.body);
    expect(row(wrapper, 'Renew passport').attributes('tabindex')).toBe('0');

    await row(wrapper, 'Renew passport').get('[data-test="task-check"]').trigger('click');
    await flushPromises();
    vi.advanceTimersByTime(600);
    await nextTick();
    await nextTick();

    expect(hasRow(wrapper, 'Renew passport')).toBe(false);
    expect(document.activeElement).toBe(document.body);
  });

  it('a row that leaves after the focus went elsewhere does not pull the focus back', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'Date'] });
    vi.setSystemTime(local(2026, 11, 3, 9, 30));
    const outside = document.createElement('input');
    document.body.appendChild(outside);
    const { wrapper } = mountList({ filter: 'open' });
    (row(wrapper, 'Renew passport').element as HTMLElement).focus();
    await row(wrapper, 'Renew passport').trigger('keydown', { key: ' ' });
    await flushPromises();

    outside.focus();
    vi.advanceTimersByTime(600);
    await nextTick();
    await nextTick();

    expect(document.activeElement).toBe(outside);
    outside.remove();
  });
});

describe('TaskList — loads it causes', () => {
  it('renders from the store without a request of its own', async () => {
    mountList();
    await flushPromises();

    expect(apiMock).not.toHaveBeenCalled();
  });

  it('a deferred tick keeps the row pending until the server answers', async () => {
    const answer = deferred<Task>();
    serve({ 'POST /api/v1/calendars/errands/tasks/Send invoice/complete': () => answer.promise });
    const { wrapper, store } = mountList({ filter: 'all' });

    await row(wrapper, 'Send invoice').get('[data-test="task-check"]').trigger('click');
    expect(row(wrapper, 'Send invoice').attributes('aria-busy')).toBe('true');
    expect(toastMock.action).not.toHaveBeenCalled();

    answer.resolve(flipped({ ...store.tasks.find((t) => t.id === 'Send invoice')!, completed: false }));
    await flushPromises();
    expect(row(wrapper, 'Send invoice').attributes('aria-busy')).toBeUndefined();
    expect(toastMock.action).toHaveBeenCalledTimes(1);
  });
});

// Story 26: the editor. A row opens from its title, "Edit task" or Enter; the
// row takes the focus first so the dialog returns it there. A delete says
// "Task deleted" (no Undo: it is permanent). The list tells the page which
// empty state it shows (the page focuses the quick-add for "Nothing to do
// here") and can put the focus back on a row after the editor closed.
describe('TaskList — opening, deleting, empty state, focus (story 26)', () => {
  const focusedId = () => (document.activeElement as HTMLElement | null)?.dataset?.taskId ?? document.activeElement?.tagName;
  const menuItems = (wrapper: Wrapper, id: string) =>
    row(wrapper, id).getComponent(MenuStub).props('model') as { label: string; command: () => void }[];

  it('a click on a title opens the task and puts the focus on its row', async () => {
    const { wrapper, store } = mountList({ filter: 'open' });

    await row(wrapper, 'Send invoice').get('[data-test="task-title"]').trigger('click');

    expect(wrapper.emitted('open')).toEqual([[store.tasks.find((t) => t.id === 'Send invoice')]]);
    expect(focusedId()).toBe('Send invoice');
    // The row now holds the list's Tab stop.
    expect(row(wrapper, 'Send invoice').attributes('tabindex')).toBe('0');
  });

  it('"Edit task" in a row\'s menu opens the task and focuses its row', async () => {
    const { wrapper, store } = mountList({ filter: 'open' });

    menuItems(wrapper, 'Write tax return').find((i) => i.label === 'Edit task')!.command();
    await nextTick();

    expect(wrapper.emitted('open')).toEqual([[store.tasks.find((t) => t.id === 'Write tax return')]]);
    expect(focusedId()).toBe('Write tax return');
  });

  it('a read-only row (no menu) still opens from its title', async () => {
    const { wrapper } = mountList({ tasks: [task('theirs', { calendar_uuid: 'team' })] });

    await row(wrapper, 'theirs').get('[data-test="task-title"]').trigger('click');

    expect(wrapper.emitted('open')).toHaveLength(1);
  });

  it('a delete says "Task deleted" once it is done; a cancelled one says nothing', async () => {
    serve();
    const { wrapper } = mountList({ filter: 'open' });

    menuItems(wrapper, 'Send invoice').find((i) => i.label === 'Delete task')!.command();
    await flushPromises();
    (confirmMock.require.mock.calls[0]![0] as { reject: () => void }).reject();
    await flushPromises();
    expect(toastMock.success).not.toHaveBeenCalled();

    menuItems(wrapper, 'Send invoice').find((i) => i.label === 'Delete task')!.command();
    await flushPromises();
    (confirmMock.require.mock.calls[1]![0] as { accept: () => void }).accept();
    await flushPromises();

    expect(count('DELETE /api/v1/calendars/errands/tasks/Send invoice')).toBe(1);
    expect(toastMock.success).toHaveBeenCalledWith('Task deleted');
    expect(toastMock.action).not.toHaveBeenCalled();
  });

  it('a failed delete says nothing more than the store\'s error', async () => {
    serve({ 'DELETE /api/v1/calendars/errands/tasks/Send invoice': () => { throw Object.assign(new Error('x'), { statusCode: 500 }); } });
    const { wrapper } = mountList({ filter: 'open' });

    menuItems(wrapper, 'Send invoice').find((i) => i.label === 'Delete task')!.command();
    await flushPromises();
    (confirmMock.require.mock.calls[0]![0] as { accept: () => void }).accept();
    await flushPromises();

    expect(toastMock.success).not.toHaveBeenCalled();
    expect(toastMock.error).toHaveBeenCalledTimes(1);
  });

  it('tells the page which empty state it shows', async () => {
    const { wrapper, store } = mountList({ tasks: [], filter: 'open', listId: 'team' });
    expect(wrapper.emitted('empty-state')).toEqual([['empty']]);

    store.tasks = [task('New one', { calendar_uuid: 'team' })];
    await nextTick();
    expect(wrapper.emitted('empty-state')!.at(-1)).toEqual([null]);

    store.searchQuery = 'zzz';
    store.tasks = [];
    await nextTick();
    expect(wrapper.emitted('empty-state')!.at(-1)).toEqual(['no-match']);
  });

  it('focusTask puts the focus on that row, or on the row holding the Tab stop when it is gone', async () => {
    const { wrapper } = mountList({ filter: 'open' });
    const vm = wrapper.vm as unknown as { focusTask: (id: string) => void };

    vm.focusTask('Write tax return');
    await nextTick();
    expect(focusedId()).toBe('Write tax return');
    expect(row(wrapper, 'Write tax return').attributes('tabindex')).toBe('0');

    // Gone (deleted in the editor): the row holding the Tab stop takes it.
    (document.activeElement as HTMLElement).blur();
    expect(focusedId()).toBe('BODY');
    vm.focusTask('deleted meanwhile');
    expect(focusedId()).toBe('Write tax return');
  });
});
