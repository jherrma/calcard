// @vitest-environment nuxt
import { describe, it, expect, afterEach, beforeEach, vi } from 'vitest';
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils';
import { reactive } from 'vue';
import { createTestingPinia } from '@pinia/testing';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import { defineComponent, h } from 'vue';
import TasksPage from './index.vue';
import TaskCreateDialog from '~/components/tasks/TaskCreateDialog.vue';
import TaskEditDialog from '~/components/tasks/TaskEditDialog.vue';
import AddTaskListDialog from '~/components/tasks/AddTaskListDialog.vue';
import TaskList from '~/components/tasks/TaskList.vue';
import TaskListSettingsDialog from '~/components/tasks/TaskListSettingsDialog.vue';
import TaskQuickAdd from '~/components/tasks/TaskQuickAdd.vue';
import TasksSidebar from '~/components/tasks/TasksSidebar.vue';
import TaskToolbar from '~/components/tasks/TaskToolbar.vue';
import { useCalendarStore } from '~/stores/calendars';
import { useTasksStore } from '~/stores/tasks';
import type { Calendar } from '~/types/calendar';
import type { Task, TasksSummary } from '~/types/tasks';

// SCOPE (#170 story 22): the Tasks page shell. It loads the lists (only when
// the calendars store is empty), the summary and the tasks; `?list=<uuid>`
// selects a list and no `?list=` means All tasks; the sidebar, the phone picker
// and the toolbar drive the tasks store. The stores run for real against a
// mocked useApi, so the specs assert the REQUESTS the page causes; the child
// components are shallow stubs whose props are read back.

const { apiMock, navigateToMock, toastMock, confirmRequire } = vi.hoisted(() => ({
  apiMock: vi.fn(),
  navigateToMock: vi.fn(),
  toastMock: { success: vi.fn(), error: vi.fn(), warn: vi.fn(), info: vi.fn(), action: vi.fn() },
  confirmRequire: vi.fn(),
}));
// The task-list settings (story 27) wrap CalendarSettingsDialog, whose explicit
// useConfirm() import needs a ConfirmationService a bare mount lacks.
vi.mock('primevue/useconfirm', () => ({ useConfirm: () => ({ require: confirmRequire }) }));
mockNuxtImport('useApi', () => () => apiMock);
// TaskList (mounted for real in the empty-state specs) takes the toast at setup.
mockNuxtImport('useAppToast', () => () => toastMock);
mockNuxtImport('navigateTo', () => navigateToMock);
// The factory only builds the getter; `route` exists by the time the page
// calls useRoute() in setup. Reactive, so the page's watcher sees changes.
mockNuxtImport('useRoute', () => () => route);

const route = reactive({ path: '/tasks', query: {} as Record<string, string> });

// The route object is shared: a page left mounted would keep reacting to the
// next test's route changes.
enableAutoUnmount(afterEach);

interface ApiOptions {
  method?: string;
  query?: Record<string, string>;
}
type Handler = (options: ApiOptions) => unknown;

function calendar(uuid: string, extra: Partial<Calendar> = {}): Calendar {
  return {
    id: uuid,
    uuid,
    path: `${uuid}.ics`,
    name: uuid,
    color: '#123456',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...extra,
  };
}

const CALENDARS: Calendar[] = [
  calendar('personal', { name: 'Personal', kind: 'events' }),
  calendar('errands', { name: 'Errands', kind: 'tasks' }),
  calendar('mixed', { name: 'Old mixed', kind: 'both' }),
  calendar('team', { name: 'Team tasks', kind: 'tasks', shared: true, permission: 'read-only' }),
];

const SUMMARY: TasksSummary = {
  open: 14,
  overdue: 3,
  due_today: 2,
  completed: 5,
  by_calendar: { errands: { open: 9, overdue: 2, completed: 0 }, mixed: { open: 1, overdue: 0, completed: 0 }, team: { open: 4, overdue: 1, completed: 0 } },
};

function task(id: string, calendarUuid: string): Task {
  return {
    id,
    calendar_id: 1,
    calendar_uuid: calendarUuid,
    uid: `${id}@calcard.io`,
    summary: `Task ${id}`,
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
  };
}

/** Answers by `METHOD url`; anything else fails loudly. */
function serve(handlers: Record<string, Handler> = {}) {
  const all: Record<string, Handler> = {
    'GET /api/v1/calendars': () => ({ calendars: CALENDARS }),
    'GET /api/v1/tasks/summary': () => SUMMARY,
    'GET /api/v1/tasks': () => ({ tasks: [task('t1', 'errands')], count: 1, total: 1 }),
    ...handlers,
  };
  apiMock.mockImplementation(async (url: string, options: ApiOptions = {}) => {
    const key = `${options.method ?? 'GET'} ${url}`;
    const handler = all[key];
    if (!handler) throw new Error(`unexpected request ${key}`);
    return handler(options);
  });
}

const requests = () => apiMock.mock.calls.map(([url, options]) => `${(options as ApiOptions | undefined)?.method ?? 'GET'} ${url}`);
const count = (key: string) => requests().filter((r) => r === key).length;
/** The `query` of every GET /api/v1/tasks, in order. */
const taskQueries = () =>
  apiMock.mock.calls
    .filter(([url, options]) => url === '/api/v1/tasks' && !(options as ApiOptions | undefined)?.method)
    .map(([, options]) => (options as ApiOptions).query);

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((res) => {
    resolve = res;
  });
  return { promise, resolve };
}

/** Mounts the page with fresh stores; `preload` seeds the calendars store first (another page loaded it). */
async function mountPage(options: { query?: Record<string, string>; preload?: Calendar[]; settle?: boolean } = {}) {
  route.query = { ...(options.query ?? {}) };
  const pinia = createTestingPinia({ stubActions: false });
  const calendarStore = useCalendarStore();
  if (options.preload) calendarStore.calendars = [...options.preload];
  const tasksStore = useTasksStore();
  const wrapper = mount(TasksPage, { shallow: true, global: { plugins: [pinia] } });
  if (options.settle !== false) await flushPromises();
  return {
    wrapper,
    calendarStore,
    tasksStore,
    sidebar: () => wrapper.findComponent(TasksSidebar),
    toolbar: () => wrapper.findComponent(TaskToolbar),
    list: () => wrapper.findComponent(TaskList),
  };
}

const ButtonStub = defineComponent({
  name: 'Button',
  props: { label: { type: String, default: '' } },
  emits: ['click'],
  setup(props, { emit }) {
    return () => h('button', { type: 'button', onClick: (e: Event) => emit('click', e) }, props.label);
  },
});

/** The page with the REAL TaskList (sidebar and toolbar stubbed), as a user sees the list area. */
async function mountPageWithList() {
  route.query = {};
  const pinia = createTestingPinia({ stubActions: false });
  const wrapper = mount(TasksPage, {
    global: { plugins: [pinia], stubs: { TasksSidebar: true, TaskToolbar: true, Button: ButtonStub, AddTaskListDialog: true, TaskListSettingsDialog: true } },
  });
  await flushPromises();
  return wrapper;
}

beforeEach(() => {
  apiMock.mockReset();
  serve();
  navigateToMock.mockReset();
  // A replace() navigation: the route takes the new query.
  navigateToMock.mockImplementation(async (to: { query?: Record<string, string> }) => {
    route.query = { ...(to.query ?? {}) };
  });
});

describe('tasks page — route', () => {
  it('is registered at /tasks with the default layout behind the auth middleware', () => {
    const tasksRoute = useNuxtApp().$router.getRoutes().find((r) => r.path === '/tasks');

    expect(tasksRoute?.meta).toMatchObject({ layout: 'default', middleware: 'auth' });
  });
});

describe('tasks page — mount and load', () => {
  it('mounts the sidebar and the toolbar on the tasks store, with task lists only', async () => {
    const { sidebar, toolbar, tasksStore } = await mountPage();

    expect(sidebar().exists()).toBe(true);
    expect(toolbar().exists()).toBe(true);
    // Task lists and mixed calendars; never the event calendar.
    const lists = (sidebar().props('lists') as Calendar[]).map((c) => c.name);
    expect(lists).toEqual(['Errands', 'Old mixed', 'Team tasks']);
    expect((toolbar().props('lists') as Calendar[]).map((c) => c.name)).toEqual(lists);
    // The read-only share is the one list missing from the writable ids.
    expect(sidebar().props('writableIds')).toEqual(['errands', 'mixed']);
    expect(toolbar().props('writableIds')).toEqual(['errands', 'mixed']);
    expect(sidebar().props('summary')).toEqual(SUMMARY);
    expect(toolbar().props('summary')).toEqual(SUMMARY);
    expect(sidebar().props('selectedId')).toBeNull();
    expect(toolbar().props()).toMatchObject({
      selectedListId: null,
      statusFilter: 'open',
      searchQuery: '',
      sortBy: tasksStore.sortBy,
      sortDir: tasksStore.sortDir,
    });
    expect(tasksStore.tasks.map((t) => t.id)).toEqual(['t1']);
  });

  it('loads the lists, the summary and the tasks of All tasks when the calendars store is empty', async () => {
    await mountPage();

    expect(requests().sort()).toEqual(['GET /api/v1/calendars', 'GET /api/v1/tasks', 'GET /api/v1/tasks/summary']);
    // All tasks: no list narrowing.
    expect(taskQueries()).toEqual([{ status: 'open' }]);
    // The summary carries `as_of` through ofetch's query (story 21).
    const summaryCall = apiMock.mock.calls.find(([url]) => url === '/api/v1/tasks/summary')!;
    expect((summaryCall[1] as ApiOptions).query).toHaveProperty('as_of');
    expect(navigateToMock).not.toHaveBeenCalled();
  });

  it('does not refetch the lists another page already loaded', async () => {
    const { sidebar } = await mountPage({ preload: CALENDARS });

    // REVERT PROOF: an unguarded fetchCalendars() adds 'GET /api/v1/calendars'.
    expect(requests().sort()).toEqual(['GET /api/v1/tasks', 'GET /api/v1/tasks/summary']);
    expect(sidebar().props('loading')).toBe(false);
  });

  it('shows the sidebar placeholder while the lists load, and asks for tasks only after them', async () => {
    const lists = deferred<{ calendars: Calendar[] }>();
    serve({ 'GET /api/v1/calendars': () => lists.promise });

    const { sidebar } = await mountPage({ settle: false });
    await flushPromises();
    expect(sidebar().props('loading')).toBe(true);
    expect(count('GET /api/v1/tasks')).toBe(0);
    expect(count('GET /api/v1/tasks/summary')).toBe(1);

    lists.resolve({ calendars: CALENDARS });
    await flushPromises();
    expect(sidebar().props('loading')).toBe(false);
    expect(count('GET /api/v1/tasks')).toBe(1);
  });

  it('opens on All tasks without ?list=, even when a list was selected on an earlier visit', async () => {
    const pinia = createTestingPinia({ stubActions: false });
    useTasksStore().selectedListId = 'errands';
    route.query = {};
    const wrapper = mount(TasksPage, { shallow: true, global: { plugins: [pinia] } });
    await flushPromises();

    expect(useTasksStore().selectedListId).toBeNull();
    expect(wrapper.findComponent(TasksSidebar).props('selectedId')).toBeNull();
    expect(taskQueries()).toEqual([{ status: 'open' }]);
  });
});

describe('tasks page — ?list= deep link', () => {
  it('selects the named list and loads only its tasks', async () => {
    const { tasksStore, sidebar, toolbar } = await mountPage({ query: { list: 'team' } });

    expect(tasksStore.selectedListId).toBe('team');
    expect(sidebar().props('selectedId')).toBe('team');
    expect(toolbar().props('selectedListId')).toBe('team');
    // REVERT PROOF: ignoring ?list= loads All tasks ({ status: 'open' }).
    expect(taskQueries()).toEqual([{ status: 'open', calendar_uuid: 'team' }]);
    expect(navigateToMock).not.toHaveBeenCalled();
  });

  it('works with a list the already-loaded store does not know yet (one refetch of the lists)', async () => {
    const newer = calendar('fresh', { name: 'Made on my phone', kind: 'tasks' });
    serve({ 'GET /api/v1/calendars': () => ({ calendars: [...CALENDARS, newer] }) });

    const { tasksStore } = await mountPage({ query: { list: 'fresh' }, preload: CALENDARS });

    expect(count('GET /api/v1/calendars')).toBe(1);
    expect(tasksStore.selectedListId).toBe('fresh');
    expect(taskQueries()).toEqual([{ status: 'open', calendar_uuid: 'fresh' }]);
  });

  it.each([
    ['an unknown list', 'gone'],
    ['an event calendar', 'personal'],
  ])('falls back to All tasks for %s and drops the param, never asking the server for it', async (_label, uuid) => {
    const { tasksStore, sidebar } = await mountPage({ query: { list: uuid, task: 'keep-me' } });

    expect(tasksStore.selectedListId).toBeNull();
    expect(sidebar().props('selectedId')).toBeNull();
    // One task load, for All tasks: no "Calendar not found" from the server.
    expect(taskQueries()).toEqual([{ status: 'open' }]);
    expect(tasksStore.error).toBeNull();
    // The lists were loaded once (the store was empty), not once more for the miss.
    expect(count('GET /api/v1/calendars')).toBe(1);
    // Other params stay.
    expect(navigateToMock).toHaveBeenCalledWith({ path: '/tasks', query: { task: 'keep-me' } }, { replace: true });
  });

  it('follows a change of ?list= while the page stays mounted (a link from search)', async () => {
    const { tasksStore } = await mountPage();
    apiMock.mockClear();

    route.query = { list: 'mixed' };
    await flushPromises();

    expect(tasksStore.selectedListId).toBe('mixed');
    expect(taskQueries()).toEqual([{ status: 'open', calendar_uuid: 'mixed' }]);

    route.query = {};
    await flushPromises();
    expect(tasksStore.selectedListId).toBeNull();
    expect(taskQueries()).toEqual([{ status: 'open', calendar_uuid: 'mixed' }, { status: 'open' }]);
  });
});

describe('tasks page — choosing a list', () => {
  it('selects a list from the sidebar, writes it into the URL and loads it once', async () => {
    const { tasksStore, sidebar } = await mountPage({ query: { task: 'keep-me' } });
    apiMock.mockClear();

    sidebar().vm.$emit('select', 'errands');
    await flushPromises();

    expect(tasksStore.selectedListId).toBe('errands');
    expect(navigateToMock).toHaveBeenCalledWith({ path: '/tasks', query: { task: 'keep-me', list: 'errands' } }, { replace: true });
    // The URL change runs the watcher too; it finds the list selected and asks nothing.
    expect(taskQueries()).toEqual([{ status: 'open', calendar_uuid: 'errands' }]);
  });

  it('goes back to All tasks from the sidebar and removes the param', async () => {
    const { tasksStore, sidebar } = await mountPage({ query: { list: 'errands' } });
    apiMock.mockClear();

    sidebar().vm.$emit('select', null);
    await flushPromises();

    expect(tasksStore.selectedListId).toBeNull();
    expect(navigateToMock).toHaveBeenCalledWith({ path: '/tasks', query: {} }, { replace: true });
    expect(taskQueries()).toEqual([{ status: 'open' }]);
  });

  it('selects a list from the phone picker in the toolbar the same way', async () => {
    const { tasksStore, toolbar } = await mountPage();
    apiMock.mockClear();

    toolbar().vm.$emit('select-list', 'team');
    await flushPromises();

    expect(tasksStore.selectedListId).toBe('team');
    expect(navigateToMock).toHaveBeenCalledWith({ path: '/tasks', query: { list: 'team' } }, { replace: true });
    expect(taskQueries()).toEqual([{ status: 'open', calendar_uuid: 'team' }]);
  });

  it('asks for a new task list from "+": the "Create task list" dialog opens', async () => {
    const { wrapper, sidebar } = await mountPage();
    expect(wrapper.getComponent(AddTaskListDialog).props('visible')).toBe(false);

    sidebar().vm.$emit('add-list');
    await flushPromises();

    expect((wrapper.vm as unknown as { showAddTaskListDialog: boolean }).showAddTaskListDialog).toBe(true);
    expect(wrapper.getComponent(AddTaskListDialog).props('visible')).toBe(true);
  });
});

describe('tasks page — toolbar drives the store', () => {
  it('refetches with the Done filter', async () => {
    const { tasksStore, toolbar } = await mountPage({ query: { list: 'errands' } });
    apiMock.mockClear();

    toolbar().vm.$emit('filter-change', 'completed');
    await flushPromises();

    expect(tasksStore.statusFilter).toBe('completed');
    expect(taskQueries()).toEqual([{ status: 'completed', calendar_uuid: 'errands' }]);
    expect(toolbar().props('statusFilter')).toBe('completed');
  });

  it('refetches with the search text', async () => {
    const { tasksStore, toolbar } = await mountPage();
    apiMock.mockClear();

    toolbar().vm.$emit('search', ' rent ');
    await flushPromises();

    expect(tasksStore.searchQuery).toBe(' rent ');
    expect(taskQueries()).toEqual([{ status: 'open', q: 'rent' }]);
  });

  it('re-sorts locally: a new sort or direction asks the server nothing', async () => {
    const { tasksStore, toolbar } = await mountPage();
    tasksStore.setSort('due', 'asc');
    apiMock.mockClear();

    toolbar().vm.$emit('sort-change', 'priority');
    toolbar().vm.$emit('toggle-sort-dir');
    await flushPromises();

    expect(tasksStore.sortBy).toBe('priority');
    expect(tasksStore.sortDir).toBe('desc');
    expect(toolbar().props()).toMatchObject({ sortBy: 'priority', sortDir: 'desc' });
    expect(apiMock).not.toHaveBeenCalled();
  });
});

describe('tasks page — the list area', () => {
  it('is the scroll container, out of the Tab order (story 29: no unnamed Firefox stop before the rows)', async () => {
    const wrapper = await mountPageWithList();
    const area = wrapper.get('[data-test="task-list-area"]');

    expect(area.classes()).toEqual(expect.arrayContaining(['min-h-0', 'flex-1', 'overflow-y-auto']));
    expect(area.attributes('tabindex')).toBe('-1');
    expect(area.find('[data-test="task-list-view"]').exists()).toBe(true);
  });

  it('shows the no-lists empty state to a user without any task list, and "Create task list" asks for one', async () => {
    serve({ 'GET /api/v1/calendars': () => ({ calendars: [calendar('personal', { name: 'Personal', kind: 'events' })] }) });

    const wrapper = await mountPageWithList();
    const area = wrapper.get('[data-test="task-list-area"]');
    const state = area.get('[data-test="empty-state"]');

    expect(state.attributes('data-variant')).toBe('no-lists');
    expect(state.get('h2').text()).toBe('No task lists yet');
    expect(state.text()).toContain('Create a list to start adding tasks. Task apps on your phone will find it too.');

    await state.get('[data-test="empty-action"]').trigger('click');
    expect((wrapper.vm as unknown as { showAddTaskListDialog: boolean }).showAddTaskListDialog).toBe(true);
  });

  it('shows the no-lists state to an account with no calendar at all (OAuth-provisioned)', async () => {
    serve({ 'GET /api/v1/calendars': () => ({ calendars: [] }) });

    const wrapper = await mountPageWithList();

    expect(wrapper.get('[data-test="empty-state"]').attributes('data-variant')).toBe('no-lists');
  });

  it('renders the loaded tasks in their groups', async () => {
    const wrapper = await mountPageWithList();

    expect(wrapper.findAll('[data-test="task-group"]').map((g) => g.attributes('data-group'))).toEqual(['needs-action']);
    expect(wrapper.findAll('li[data-task-id]').map((li) => li.attributes('data-task-id'))).toEqual(['t1']);
    expect(wrapper.find('[data-test="empty-state"]').exists()).toBe(false);
  });

  it('tells the list while the lists load (it shows the skeleton, not "No task lists yet")', async () => {
    const lists = deferred<{ calendars: Calendar[] }>();
    serve({ 'GET /api/v1/calendars': () => lists.promise });

    const { list } = await mountPage({ settle: false });
    await flushPromises();
    expect(list().props()).toMatchObject({ listsLoading: true, listsFailed: false });

    lists.resolve({ calendars: CALENDARS });
    await flushPromises();
    expect(list().props()).toMatchObject({ listsLoading: false, listsFailed: false });
  });

  it('a failed lists load is an error for the list; "Try again" reloads the lists and then the tasks', async () => {
    let fail = true;
    serve({
      'GET /api/v1/calendars': () => {
        if (fail) throw new Error('offline');
        return { calendars: CALENDARS };
      },
    });
    const { list, calendarStore } = await mountPage();
    expect(list().props('listsFailed')).toBe(true);

    fail = false;
    apiMock.mockClear();
    list().vm.$emit('retry');
    await flushPromises();

    expect(requests()).toEqual(['GET /api/v1/calendars', 'GET /api/v1/tasks']);
    expect(list().props('listsFailed')).toBe(false);
    expect(calendarStore.error).toBeNull();
  });

  it('"Try again" after a failed task load reloads only the tasks', async () => {
    const { list } = await mountPage();
    apiMock.mockClear();

    list().vm.$emit('retry');
    await flushPromises();

    expect(requests()).toEqual(['GET /api/v1/tasks']);
    expect(taskQueries()).toEqual([{ status: 'open' }]);
  });

  it('a stale calendars error from another page does not count as a failed lists load', async () => {
    const pinia = createTestingPinia({ stubActions: false });
    const calendarStore = useCalendarStore();
    calendarStore.error = 'Failed to load events';
    route.query = {};
    const wrapper = mount(TasksPage, { shallow: true, global: { plugins: [pinia] } });
    await flushPromises();

    expect(wrapper.findComponent(TaskList).props('listsFailed')).toBe(false);
  });

  it('Enter on a row hands the task to the page (the editor is story 26)', async () => {
    const { list, wrapper } = await mountPage();
    const t = task('t1', 'errands');

    list().vm.$emit('open', t);

    expect((wrapper.vm as unknown as { openedTask: Task | null }).openedTask).toEqual(t);
  });
});

// Story 26: the quick-add sits between the toolbar and the scrolling list;
// a row opens the editor (read only for a read-only list); "Add with
// details" opens "Add a task" with the typed title; "Nothing to do here"
// focuses the quick-add; a new task the view does not show gets a toast; the
// focus goes back to the row when the editor closed and the row was redrawn.
describe('tasks page — adding and editing (story 26)', () => {
  const quickAddApi = { focus: vi.fn(), clear: vi.fn() };
  const listApi = { focusTask: vi.fn() };
  const QuickAddStub = defineComponent({
    name: 'TaskQuickAdd',
    emits: ['created', 'open-details'],
    setup(_props, { expose }) {
      expose(quickAddApi);
      return () => h('div', { 'data-test': 'quick-add-stub' });
    },
  });
  const TaskListStub = defineComponent({
    name: 'TaskList',
    props: { listsLoading: Boolean, listsFailed: Boolean },
    emits: ['add-list', 'retry', 'open', 'empty-state'],
    setup(_props, { expose }) {
      expose(listApi);
      return () => h('div', { 'data-test': 'task-list-stub' });
    },
  });

  async function mountWithStubs(options: { filter?: 'open' | 'completed' } = {}) {
    route.query = {};
    const pinia = createTestingPinia({ stubActions: false });
    const tasksStore = useTasksStore();
    const wrapper = mount(TasksPage, {
      shallow: true,
      global: { plugins: [pinia], stubs: { TaskQuickAdd: QuickAddStub, TaskList: TaskListStub } },
    });
    await flushPromises();
    if (options.filter) tasksStore.statusFilter = options.filter;
    return {
      wrapper,
      tasksStore,
      quickAdd: () => wrapper.getComponent(QuickAddStub),
      list: () => wrapper.getComponent(TaskListStub),
      createDialog: () => wrapper.getComponent(TaskCreateDialog),
      editDialog: () => wrapper.getComponent(TaskEditDialog),
    };
  }

  beforeEach(() => {
    Object.values(quickAddApi).forEach((fn) => fn.mockReset());
    listApi.focusTask.mockReset();
    Object.values(toastMock).forEach((fn) => fn.mockReset());
  });

  it('puts the quick-add between the toolbar and the scrolling list area (Tab order: toolbar, quick-add, rows)', async () => {
    const { wrapper } = await mountPage();
    const column = wrapper.findComponent(TaskToolbar).element.parentElement!;
    const children = Array.from(column.children);
    const quickAdd = wrapper.findComponent(TaskQuickAdd).element;
    const area = wrapper.get('[data-test="task-list-area"]').element;

    expect(children.indexOf(quickAdd)).toBe(children.indexOf(wrapper.findComponent(TaskToolbar).element) + 1);
    expect(children.indexOf(area)).toBe(children.indexOf(quickAdd) + 1);
    expect(area.contains(quickAdd)).toBe(false);
  });

  it('a row\'s task opens in the editor; a writable list\'s task is editable', async () => {
    const { list, editDialog } = await mountWithStubs();
    const t = task('t1', 'errands');
    expect(editDialog().props('visible')).toBe(false);

    list().vm.$emit('open', t);
    await flushPromises();

    expect(editDialog().props()).toMatchObject({ visible: true, task: t, readOnly: false });
  });

  it('a task of a read-only share opens read only', async () => {
    const { list, editDialog } = await mountWithStubs();

    list().vm.$emit('open', task('theirs', 'team'));
    await flushPromises();

    expect(editDialog().props()).toMatchObject({ visible: true, readOnly: true });
  });

  it('closing the editor hides it but keeps the task for its closing transition', async () => {
    const { list, editDialog } = await mountWithStubs();
    const t = task('t1', 'errands');
    list().vm.$emit('open', t);
    await flushPromises();

    editDialog().vm.$emit('update:visible', false);
    await flushPromises();

    expect(editDialog().props()).toMatchObject({ visible: false, task: t });
  });

  it('after the editor closed with the focus lost, the task\'s row takes it; otherwise nothing moves', async () => {
    const { list, editDialog } = await mountWithStubs();
    list().vm.$emit('open', task('t1', 'errands'));
    await flushPromises();

    (document.activeElement as HTMLElement | null)?.blur();
    editDialog().vm.$emit('after-hide');
    expect(listApi.focusTask).toHaveBeenCalledWith('t1');

    listApi.focusTask.mockReset();
    const outside = document.createElement('button');
    document.body.appendChild(outside);
    outside.focus();
    editDialog().vm.$emit('after-hide');
    expect(listApi.focusTask).not.toHaveBeenCalled();
    outside.remove();
  });

  it('"Add with details" opens "Add a task" with the typed title and the view\'s list', async () => {
    const { quickAdd, createDialog, tasksStore } = await mountWithStubs();
    expect(createDialog().props('visible')).toBe(false);

    quickAdd().vm.$emit('open-details', 'Renew passport');
    await flushPromises();

    expect(createDialog().props()).toMatchObject({ visible: true, initialSummary: 'Renew passport', defaultListId: 'errands' });
    expect(tasksStore.newTaskListId).toBe('errands');
  });

  it('a task added through the dialog empties the quick-add', async () => {
    const { quickAdd, createDialog, tasksStore } = await mountWithStubs();
    quickAdd().vm.$emit('open-details', 'Renew passport');
    const created = task('new', 'errands');
    tasksStore.tasks.push(created);

    createDialog().vm.$emit('created', created);

    expect(quickAddApi.clear).toHaveBeenCalledTimes(1);
    expect(toastMock.success).not.toHaveBeenCalled();
  });

  it('a new task the view shows needs no word; one it hides says where it went', async () => {
    const { quickAdd, createDialog, tasksStore } = await mountWithStubs();
    const shown = task('shown', 'errands');
    tasksStore.tasks.push(shown);

    quickAdd().vm.$emit('created', shown);
    expect(toastMock.success).not.toHaveBeenCalled();

    // Under Done a new open task is not in the view.
    tasksStore.statusFilter = 'completed';
    quickAdd().vm.$emit('created', task('hidden', 'errands'));
    expect(toastMock.success).toHaveBeenCalledWith('Task added to Errands');

    // The dialog put it into another list than the one shown.
    tasksStore.statusFilter = 'open';
    tasksStore.selectedListId = 'errands';
    createDialog().vm.$emit('created', task('elsewhere', 'mixed'));
    expect(toastMock.success).toHaveBeenLastCalledWith('Task added to Old mixed');
  });

  it('"Nothing to do here" focuses the quick-add; other states and an open dialog do not', async () => {
    const { list, editDialog } = await mountWithStubs();

    list().vm.$emit('empty-state', 'empty');
    await flushPromises();
    expect(quickAddApi.focus).toHaveBeenCalledTimes(1);

    for (const variant of ['no-match', 'all-done', 'no-lists', null]) list().vm.$emit('empty-state', variant);
    await flushPromises();
    expect(quickAddApi.focus).toHaveBeenCalledTimes(1);

    list().vm.$emit('open', task('t1', 'errands'));
    await flushPromises();
    expect(editDialog().props('visible')).toBe(true);
    list().vm.$emit('empty-state', 'empty');
    await flushPromises();
    expect(quickAddApi.focus).toHaveBeenCalledTimes(1);
  });

  it('with the real list: an empty list focuses the real quick-add', async () => {
    serve({ 'GET /api/v1/tasks': () => ({ tasks: [], count: 0, total: 0 }) });
    route.query = { list: 'errands' };
    const pinia = createTestingPinia({ stubActions: false });
    const wrapper = mount(TasksPage, {
      attachTo: document.body,
      global: { plugins: [pinia], stubs: { TasksSidebar: true, TaskToolbar: true, Button: ButtonStub, TaskCreateDialog: true, TaskEditDialog: true, AddTaskListDialog: true, TaskListSettingsDialog: true } },
    });
    await flushPromises();

    expect(wrapper.get('[data-test="empty-state"]').attributes('data-variant')).toBe('empty');
    expect(document.activeElement).toBe(wrapper.get('[data-test="quick-add-input"]').element);
  });
});

// Story 27: task lists are created and managed from the Tasks section. "+"
// (and "Create task list") opens AddTaskListDialog, and the new list is
// selected; the sidebar's owner-only list menu opens TaskListSettingsDialog
// on General or Sharing, or asks to delete the list; a renamed list renames
// its rows; a deleted list leaves the page (and the URL when it was selected),
// down to the "No task lists yet" state after the last one.
describe('tasks page — task lists (story 27)', () => {
  const settingsDialog = (wrapper: Awaited<ReturnType<typeof mountPage>>['wrapper']) => wrapper.getComponent(TaskListSettingsDialog);

  beforeEach(() => {
    confirmRequire.mockReset();
    Object.values(toastMock).forEach((fn) => fn.mockReset());
  });

  it('a created list is selected: it goes into the URL and its (empty) tasks are loaded', async () => {
    const { wrapper, calendarStore, tasksStore, sidebar } = await mountPage();
    const created = calendar('groceries', { name: 'Groceries', kind: 'tasks' });
    // createTaskList has refreshed the calendars store before the dialog emits.
    calendarStore.calendars = [...calendarStore.calendars, created];
    apiMock.mockClear();
    navigateToMock.mockClear();

    wrapper.getComponent(AddTaskListDialog).vm.$emit('created', created);
    await flushPromises();

    expect(tasksStore.selectedListId).toBe('groceries');
    expect(navigateToMock).toHaveBeenCalledWith({ path: '/tasks', query: { list: 'groceries' } }, { replace: true });
    expect(taskQueries()).toEqual([{ status: 'open', calendar_uuid: 'groceries' }]);
    expect(sidebar().props('selectedId')).toBe('groceries');
    expect(sidebar().props('lists').map((l: Calendar) => l.uuid)).toContain('groceries');
  });

  it('"List settings" opens the settings of that list on General, "Share list" on Sharing', async () => {
    const { wrapper, sidebar } = await mountPage();
    const errands = CALENDARS.find((c) => c.uuid === 'errands')!;
    expect(settingsDialog(wrapper).props('visible')).toBe(false);

    sidebar().vm.$emit('edit-list', errands);
    await flushPromises();
    expect(settingsDialog(wrapper).props()).toMatchObject({ visible: true, list: errands, initialTab: 'general' });

    settingsDialog(wrapper).vm.$emit('update:visible', false);
    await flushPromises();
    expect(settingsDialog(wrapper).props('visible')).toBe(false);

    sidebar().vm.$emit('share-list', errands);
    await flushPromises();
    expect(settingsDialog(wrapper).props()).toMatchObject({ visible: true, list: errands, initialTab: 'sharing' });
  });

  it('a renamed or recoloured list renames its loaded rows, without a request', async () => {
    const { wrapper, calendarStore, tasksStore } = await mountPage();
    calendarStore.calendars = calendarStore.calendars.map((c) => (c.uuid === 'errands' ? { ...c, name: 'Groceries', color: '#d6336c' } : c));
    apiMock.mockClear();

    settingsDialog(wrapper).vm.$emit('updated');
    await flushPromises();

    expect(tasksStore.tasks.map((t) => [t.id, t.calendar_name, t.calendar_color])).toEqual([['t1', 'Groceries', '#d6336c']]);
    expect(apiMock).not.toHaveBeenCalled();
  });

  it('deleting the selected list goes back to All tasks: the param leaves the URL, All tasks loads, the counts refresh', async () => {
    const { wrapper, calendarStore, tasksStore } = await mountPage({ query: { list: 'errands', task: 'keep-me' } });
    const errands = calendarStore.calendars.find((c) => c.uuid === 'errands')!;
    calendarStore.calendars = calendarStore.calendars.filter((c) => c.uuid !== 'errands');
    apiMock.mockClear();
    navigateToMock.mockClear();

    settingsDialog(wrapper).vm.$emit('deleted', errands);
    await flushPromises();

    expect(tasksStore.selectedListId).toBeNull();
    expect(navigateToMock).toHaveBeenCalledWith({ path: '/tasks', query: { task: 'keep-me' } }, { replace: true });
    expect(taskQueries()).toEqual([{ status: 'open' }]);
    expect(count('GET /api/v1/tasks/summary')).toBe(1);
  });

  it('deleting another list drops its rows from All tasks, without a reload or a navigation', async () => {
    serve({ 'GET /api/v1/tasks': () => ({ tasks: [task('t1', 'errands'), task('t2', 'mixed')], count: 2, total: 2 }) });
    const { wrapper, calendarStore, tasksStore } = await mountPage();
    const errands = calendarStore.calendars.find((c) => c.uuid === 'errands')!;
    apiMock.mockClear();
    navigateToMock.mockClear();

    settingsDialog(wrapper).vm.$emit('deleted', errands);
    await flushPromises();

    expect(tasksStore.tasks.map((t) => t.id)).toEqual(['t2']);
    expect(tasksStore.total).toBe(1);
    expect(navigateToMock).not.toHaveBeenCalled();
    expect(taskQueries()).toEqual([]);
    expect(count('GET /api/v1/tasks/summary')).toBe(1);
  });

  it('deleting another list while one is selected keeps the selection and the URL', async () => {
    const { wrapper, calendarStore, tasksStore } = await mountPage({ query: { list: 'errands' } });
    const other = calendar('groceries', { name: 'Groceries', kind: 'tasks' });
    apiMock.mockClear();
    navigateToMock.mockClear();

    settingsDialog(wrapper).vm.$emit('deleted', other);
    await flushPromises();

    expect(tasksStore.selectedListId).toBe('errands');
    expect(route.query).toEqual({ list: 'errands' });
    expect(navigateToMock).not.toHaveBeenCalled();
    expect(taskQueries()).toEqual([]);
    expect(tasksStore.tasks.map((t) => t.id)).toEqual(['t1']);
    expect(calendarStore.calendars.some((c) => c.uuid === 'errands')).toBe(true);
  });

  it('"Nothing to do here" does not take the focus while a list dialog is open', async () => {
    const focus = vi.fn();
    const QuickAddStub = defineComponent({
      name: 'TaskQuickAdd',
      setup(_props, { expose }) {
        expose({ focus, clear: vi.fn() });
        return () => h('div');
      },
    });
    route.query = {};
    const pinia = createTestingPinia({ stubActions: false });
    const wrapper = mount(TasksPage, { shallow: true, global: { plugins: [pinia], stubs: { TaskQuickAdd: QuickAddStub } } });
    await flushPromises();
    const list = () => wrapper.getComponent(TaskList);

    wrapper.getComponent(TasksSidebar).vm.$emit('add-list');
    list().vm.$emit('empty-state', 'empty');
    await flushPromises();
    expect(focus).not.toHaveBeenCalled();

    wrapper.getComponent(AddTaskListDialog).vm.$emit('update:visible', false);
    wrapper.getComponent(TasksSidebar).vm.$emit('edit-list', CALENDARS[1]);
    list().vm.$emit('empty-state', 'empty');
    await flushPromises();
    expect(focus).not.toHaveBeenCalled();

    settingsDialog(wrapper).vm.$emit('update:visible', false);
    list().vm.$emit('empty-state', 'empty');
    await flushPromises();
    expect(focus).toHaveBeenCalledTimes(1);
  });

  // --- The whole page: real sidebar, real dialogs, real list ---------------

  const DialogStub = defineComponent({
    name: 'Dialog',
    props: { visible: Boolean, header: { type: String, default: '' } },
    emits: ['update:visible', 'after-hide'],
    setup(props, { slots }) {
      return () => (props.visible ? h('div', { 'data-test': 'dialog', 'data-header': props.header }, [slots.default?.(), slots.footer?.()]) : null);
    },
  });
  const InputStub = defineComponent({
    name: 'InputText',
    inheritAttrs: false,
    props: { modelValue: { type: String, default: '' } },
    emits: ['update:modelValue'],
    setup(props, { emit, attrs }) {
      return () => h('input', { ...attrs, value: props.modelValue, onInput: (e: Event) => emit('update:modelValue', (e.target as HTMLInputElement).value) });
    },
  });
  const MenuStub = defineComponent({
    name: 'Menu',
    props: { model: { type: Array, default: () => [] } },
    setup(props, { expose }) {
      expose({ toggle: vi.fn() });
      return () => h('div', (props.model as { label: string; command: (e: unknown) => void }[]).map((item) =>
        h('button', { type: 'button', 'data-item': item.label, onClick: () => item.command({}) }, item.label)));
    },
  });
  const slotStub = (name: string) => defineComponent({ name, setup: (_p, { slots }) => () => h('div', slots.default?.()) });

  /** Server state the handlers below read and write. */
  let serverCalendars: Calendar[] = [];
  let serverTasks: Task[] = [];

  async function mountWholePage(query: Record<string, string> = {}) {
    serve({
      'GET /api/v1/calendars': () => ({ calendars: serverCalendars }),
      'GET /api/v1/tasks': (o) => {
        const rows = serverTasks.filter((t) => !o.query?.calendar_uuid || t.calendar_uuid === o.query.calendar_uuid);
        return { tasks: rows, count: rows.length, total: rows.length };
      },
      'POST /api/v1/calendars': (o) => {
        const body = (o as ApiOptions & { body: { name: string; color: string; kind: string } }).body;
        const created = calendar('groceries', { name: body.name, color: body.color, kind: body.kind as Calendar['kind'] });
        serverCalendars = [...serverCalendars, created];
        return created;
      },
      'DELETE /api/v1/calendars/errands': () => {
        serverCalendars = serverCalendars.filter((c) => c.uuid !== 'errands');
        serverTasks = serverTasks.filter((t) => t.calendar_uuid !== 'errands');
        return undefined;
      },
    });
    route.query = { ...query };
    const pinia = createTestingPinia({ stubActions: false });
    const wrapper = mount(TasksPage, {
      global: {
        plugins: [pinia],
        stubs: {
          TaskToolbar: true,
          TaskCreateDialog: true,
          TaskEditDialog: true,
          Dialog: DialogStub,
          Button: ButtonStub,
          InputText: InputStub,
          Menu: MenuStub,
          ColorPicker: true,
          Textarea: true,
          Select: true,
          ...Object.fromEntries(['Tabs', 'TabList', 'Tab', 'TabPanels', 'TabPanel'].map((n) => [n, slotStub(n)])),
        },
      },
    });
    await flushPromises();
    return wrapper;
  }

  const sidebarRowNames = (wrapper: Awaited<ReturnType<typeof mountWholePage>>) =>
    wrapper.getComponent(TasksSidebar).findAll('button[data-list-id]').map((b) => b.attributes('data-list-id'));

  it('creates a list from "+": it appears in the sidebar, selected, and "Task list created" is said', async () => {
    serverCalendars = [calendar('personal', { name: 'Personal', kind: 'events' }), calendar('errands', { name: 'Errands', kind: 'tasks' })];
    serverTasks = [task('t1', 'errands')];
    const wrapper = await mountWholePage();
    expect(sidebarRowNames(wrapper)).toEqual(['', 'errands']);

    await wrapper.get('[data-test="add-list"]').trigger('click');
    const dialog = wrapper.get('[data-header="Create task list"]');
    await dialog.get('[data-test="list-name"]').setValue('Groceries');
    await dialog.get('[data-test="create-list"]').trigger('click');
    await flushPromises();

    expect(apiMock).toHaveBeenCalledWith('/api/v1/calendars', { method: 'POST', body: { name: 'Groceries', color: '#3788d8', kind: 'tasks' } });
    expect(sidebarRowNames(wrapper)).toEqual(['', 'errands', 'groceries']);
    expect(wrapper.get('button[data-list-id="groceries"]').text()).toContain('Groceries');
    expect(wrapper.get('button[data-list-id="groceries"]').attributes('aria-current')).toBe('true');
    expect(toastMock.success).toHaveBeenCalledWith('Task list created');
    expect(route.query).toEqual({ list: 'groceries' });
    expect(wrapper.find('[data-header="Create task list"]').exists()).toBe(false);
    // The new list is empty: "Nothing to do here", not "No task lists yet".
    expect(wrapper.get('[data-test="empty-state"]').attributes('data-variant')).toBe('empty');
  });

  it('creates the first list from "No task lists yet"', async () => {
    serverCalendars = [calendar('personal', { name: 'Personal', kind: 'events' })];
    serverTasks = [];
    const wrapper = await mountWholePage();
    expect(wrapper.get('[data-test="empty-state"]').attributes('data-variant')).toBe('no-lists');

    await wrapper.get('[data-test="empty-action"]').trigger('click');
    await wrapper.get('[data-test="list-name"]').setValue('Errands');
    await wrapper.get('[data-test="create-list"]').trigger('click');
    await flushPromises();

    expect(sidebarRowNames(wrapper)).toEqual(['', 'groceries']);
    expect(wrapper.get('[data-test="empty-state"]').attributes('data-variant')).toBe('empty');
  });

  it('deletes the last task list from its menu: asked first, then gone, and "No task lists yet" shows', async () => {
    serverCalendars = [calendar('personal', { name: 'Personal', kind: 'events' }), calendar('errands', { name: 'Errands', kind: 'tasks' })];
    serverTasks = [task('t1', 'errands')];
    const wrapper = await mountWholePage({ list: 'errands' });
    expect(wrapper.findAll('[data-test="task-row"]')).toHaveLength(1);

    await wrapper.get('[data-test="list-menu"]').trigger('click');
    await wrapper.getComponent(TasksSidebar).get('[data-item="Delete task list"]').trigger('click');

    expect(confirmRequire).toHaveBeenCalledTimes(1);
    const question = confirmRequire.mock.calls[0]![0] as { header: string; message: string; acceptLabel: string; accept: () => Promise<void> };
    expect(question).toMatchObject({ header: 'Delete task list', acceptLabel: 'Delete task list', message: taskListDeleteMessage('Errands') });
    expect(apiMock).not.toHaveBeenCalledWith('/api/v1/calendars/errands', expect.anything());

    await question.accept();
    await flushPromises();

    expect(apiMock).toHaveBeenCalledWith('/api/v1/calendars/errands', { method: 'DELETE', body: { confirmation: 'DELETE' } });
    expect(toastMock.success).toHaveBeenCalledWith('Task list deleted');
    expect(sidebarRowNames(wrapper)).toEqual(['']);
    expect(route.query).toEqual({});
    expect(wrapper.findAll('[data-test="task-row"]')).toHaveLength(0);
    expect(wrapper.get('[data-test="empty-state"]').attributes('data-variant')).toBe('no-lists');
    // The settings dialog itself never opened.
    expect(wrapper.find('[data-test="dialog"]').exists()).toBe(false);
  });

  it('a list shared with the user has no menu at all', async () => {
    serverCalendars = [calendar('team', { name: 'Team tasks', kind: 'tasks', shared: true, permission: 'read-write' })];
    serverTasks = [];
    const wrapper = await mountWholePage();

    expect(sidebarRowNames(wrapper)).toEqual(['', 'team']);
    expect(wrapper.find('[data-test="list-menu"]').exists()).toBe(false);
  });
});
