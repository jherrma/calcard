// @vitest-environment nuxt
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils';
import { createTestingPinia } from '@pinia/testing';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import TaskQuickAdd from './TaskQuickAdd.vue';
import { useCalendarStore } from '~/stores/calendars';
import { useTasksStore } from '~/stores/tasks';
import type { Calendar } from '~/types/calendar';
import type { Task, TaskStatusFilter } from '~/types/tasks';

// SCOPE (#170 story 26, spec §7.3/§7.5/§7.6): the one-line add. Enter with a
// non-blank title creates the task in the selected list (or, under All
// tasks, the first writable one) and clears the box; a blank title is
// ignored; on failure the text stays. Absent for a read-only list or a
// subscription. The stores run for real against a mocked useApi.

const { apiMock, toastMock } = vi.hoisted(() => ({
  apiMock: vi.fn(),
  toastMock: { success: vi.fn(), error: vi.fn(), warn: vi.fn(), info: vi.fn(), action: vi.fn() },
}));
mockNuxtImport('useApi', () => () => apiMock);
mockNuxtImport('useAppToast', () => () => toastMock);

enableAutoUnmount(afterEach);

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

// The first task lists are ones a task cannot be added to: All tasks must skip them.
const LISTS: Calendar[] = [
  calendar('personal', { name: 'Personal', kind: 'events' }),
  calendar('team', { name: 'Team tasks', shared: true, permission: 'read-only' }),
  calendar('feed', { name: 'Holidays feed', kind: 'both', subscribed: true }),
  calendar('errands', { name: 'Errands', color: '#ff0000' }),
  calendar('home', { name: 'Home', color: '#00aa00', kind: 'both' }),
];

function created(summary: string, listUuid: string): Task {
  return {
    id: `new-${summary}`,
    calendar_id: 1,
    calendar_uuid: listUuid,
    uid: 'new@calcard.io',
    summary,
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
    created_at: '2026-11-03T10:00:00Z',
    updated_at: '2026-11-03T10:00:00Z',
  };
}

interface ApiOptions {
  method?: string;
  body?: { summary: string };
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

/** POSTs answer the created task; the summary refetch every mutation does answers zeros. */
function serve(post?: (url: string, options: ApiOptions) => unknown) {
  apiMock.mockImplementation(async (url: string, options: ApiOptions = {}) => {
    if (url === '/api/v1/tasks/summary') return { open: 0, overdue: 0, due_today: 0, completed: 0, by_calendar: {} };
    if (options.method === 'POST') {
      if (post) return post(url, options);
      const list = /\/calendars\/([^/]+)\/tasks$/.exec(url)![1]!;
      return created(options.body!.summary, list);
    }
    throw new Error(`unexpected request ${options.method ?? 'GET'} ${url}`);
  });
}

const posts = () =>
  apiMock.mock.calls
    .filter(([, options]) => (options as ApiOptions | undefined)?.method === 'POST')
    .map(([url, options]) => ({ url, body: (options as ApiOptions).body }));

function mountQuickAdd(options: { listId?: string | null; filter?: TaskStatusFilter; calendars?: Calendar[] } = {}) {
  const pinia = createTestingPinia({ stubActions: false });
  useCalendarStore().calendars = options.calendars ?? LISTS;
  const store = useTasksStore();
  store.selectedListId = options.listId ?? null;
  store.statusFilter = options.filter ?? 'open';
  const wrapper = mount(TaskQuickAdd, { attachTo: document.body, global: { plugins: [pinia] } });
  return { wrapper, store };
}

type Wrapper = ReturnType<typeof mountQuickAdd>['wrapper'];
const input = (w: Wrapper) => w.get<HTMLInputElement>('[data-test="quick-add-input"]');

async function typeAndEnter(w: Wrapper, text: string) {
  await input(w).setValue(text);
  await input(w).trigger('keydown', { key: 'Enter' });
}

beforeEach(() => {
  apiMock.mockReset();
  serve();
  Object.values(toastMock).forEach((fn) => fn.mockReset());
});

describe('TaskQuickAdd — where it shows', () => {
  it('is a labelled text box with the placeholder "Add a task"', () => {
    const { wrapper } = mountQuickAdd({ listId: 'errands' });
    const box = input(wrapper);

    expect(box.attributes('type')).toBe('text');
    expect(box.attributes('placeholder')).toBe('Add a task');
    expect(box.attributes('aria-label')).toBe('Add a task');
    expect(box.attributes('enterkeyhint')).toBe('done');
  });

  it.each([
    ['a read-only share', 'team'],
    ['a subscription', 'feed'],
  ])('is absent when %s is selected', (_label, listId) => {
    const { wrapper } = mountQuickAdd({ listId });

    expect(wrapper.find('[data-test="quick-add"]').exists()).toBe(false);
  });

  it('is absent under All tasks when no list is writable, and without any task list', () => {
    expect(mountQuickAdd({ calendars: [LISTS[0]!, LISTS[1]!, LISTS[2]!] }).wrapper.find('[data-test="quick-add"]').exists()).toBe(false);
    expect(mountQuickAdd({ calendars: [] }).wrapper.find('[data-test="quick-add"]').exists()).toBe(false);
  });

  it('under All tasks names the list a new task goes to (the first writable one)', () => {
    const { wrapper } = mountQuickAdd();
    const target = wrapper.get('[data-test="quick-add-list"]');

    expect(target.text()).toBe('Adds to Errands');
    expect(input(wrapper).attributes('aria-describedby')).toBe(target.attributes('id'));
  });

  it('in a single list says nothing about the list (the view title does)', () => {
    const { wrapper } = mountQuickAdd({ listId: 'home' });

    expect(wrapper.find('[data-test="quick-add-list"]').exists()).toBe(false);
    expect(input(wrapper).attributes('aria-describedby')).toBeUndefined();
  });

  it('appears when a writable list is selected after a read-only one', async () => {
    const { wrapper, store } = mountQuickAdd({ listId: 'team' });
    expect(wrapper.find('[data-test="quick-add"]').exists()).toBe(false);

    store.selectedListId = 'errands';
    await flushPromises();
    expect(wrapper.find('[data-test="quick-add"]').exists()).toBe(true);
  });
});

describe('TaskQuickAdd — Enter', () => {
  it('creates the task in the selected list, clears the box and emits it', async () => {
    const { wrapper, store } = mountQuickAdd({ listId: 'home' });

    await typeAndEnter(wrapper, 'Buy milk');
    await flushPromises();

    expect(posts()).toEqual([{ url: '/api/v1/calendars/home/tasks', body: { summary: 'Buy milk' } }]);
    expect(input(wrapper).element.value).toBe('');
    expect(wrapper.emitted('created')).toEqual([[expect.objectContaining({ id: 'new-Buy milk', calendar_uuid: 'home' })]]);
    // The store put it into the open view.
    expect(store.visibleTasks.map((t) => t.summary)).toEqual(['Buy milk']);
  });

  it('under All tasks creates in the first writable list (not the read-only share or the subscription)', async () => {
    const { wrapper } = mountQuickAdd();

    await typeAndEnter(wrapper, 'Buy milk');
    await flushPromises();

    expect(posts()).toEqual([{ url: '/api/v1/calendars/errands/tasks', body: { summary: 'Buy milk' } }]);
  });

  it('keeps a trailing shorthand as plain text in the title (no parsing in v1), trimmed', async () => {
    const { wrapper } = mountQuickAdd({ listId: 'errands' });

    await typeAndEnter(wrapper, '  Call mum tomorrow 17:00 !high, ok; \\ yes  ');
    await flushPromises();

    expect(posts()[0]!.body).toEqual({ summary: 'Call mum tomorrow 17:00 !high, ok; \\ yes' });
  });

  it.each([
    ['empty', ''],
    ['blank', '   \t '],
  ])('ignores an %s title: no request, the box keeps it', async (_label, text) => {
    const { wrapper } = mountQuickAdd({ listId: 'errands' });

    await typeAndEnter(wrapper, text);
    await flushPromises();

    expect(apiMock).not.toHaveBeenCalled();
    expect(wrapper.emitted('created')).toBeUndefined();
    expect(input(wrapper).element.value).toBe(text);
  });

  it('keeps the text when the create fails (the store toasts why) and can try again', async () => {
    let fail = true;
    serve((url, options) => {
      if (fail) throw Object.assign(new Error('boom'), { statusCode: 500 });
      return created(options.body!.summary, 'errands');
    });
    const { wrapper } = mountQuickAdd({ listId: 'errands' });

    await typeAndEnter(wrapper, 'Buy milk');
    await flushPromises();

    expect(input(wrapper).element.value).toBe('Buy milk');
    expect(wrapper.emitted('created')).toBeUndefined();
    expect(toastMock.error).toHaveBeenCalledWith('Check your connection and try again.', "Couldn't add the task");

    fail = false;
    await input(wrapper).trigger('keydown', { key: 'Enter' });
    await flushPromises();
    expect(input(wrapper).element.value).toBe('');
    expect(posts()).toHaveLength(2);
  });

  it('sends once while a create is in flight (a second Enter does nothing) and shows it is busy', async () => {
    const answer = deferred<Task>();
    serve(() => answer.promise);
    const { wrapper } = mountQuickAdd({ listId: 'errands' });

    await typeAndEnter(wrapper, 'Buy milk');
    await input(wrapper).trigger('keydown', { key: 'Enter' });
    expect(posts()).toHaveLength(1);
    expect(wrapper.get('[data-test="quick-add"]').attributes('aria-busy')).toBe('true');
    expect(wrapper.find('[data-test="quick-add-spinner"]').exists()).toBe(true);

    answer.resolve(created('Buy milk', 'errands'));
    await flushPromises();
    expect(wrapper.get('[data-test="quick-add"]').attributes('aria-busy')).toBeUndefined();
    expect(wrapper.find('[data-test="quick-add-spinner"]').exists()).toBe(false);
  });

  it('keeps what was typed while the create ran', async () => {
    const answer = deferred<Task>();
    serve(() => answer.promise);
    const { wrapper } = mountQuickAdd({ listId: 'errands' });

    await typeAndEnter(wrapper, 'Buy milk');
    await input(wrapper).setValue('Buy bread');
    answer.resolve(created('Buy milk', 'errands'));
    await flushPromises();

    expect(input(wrapper).element.value).toBe('Buy bread');
  });

  it('an Enter that confirms an IME composition is not a submit', async () => {
    const { wrapper } = mountQuickAdd({ listId: 'errands' });
    await input(wrapper).setValue('かいもの');

    await input(wrapper).trigger('keydown', { key: 'Enter', isComposing: true });
    await input(wrapper).trigger('keydown', { key: 'Enter', keyCode: 229 });
    await flushPromises();

    expect(apiMock).not.toHaveBeenCalled();
  });
});

describe('TaskQuickAdd — details and focus', () => {
  it('"Add a task with details" hands the typed text to the page', async () => {
    const { wrapper } = mountQuickAdd({ listId: 'errands' });
    const button = wrapper.get('[data-test="quick-add-details"]');
    await input(wrapper).setValue('Renew passport');

    expect(button.attributes('aria-label')).toBe('Add a task with details');
    expect(button.attributes('type')).toBe('button');
    await button.trigger('click');

    expect(wrapper.emitted('open-details')).toEqual([['Renew passport']]);
    expect(apiMock).not.toHaveBeenCalled();
  });

  it('focus() puts the cursor in the box, clear() empties it', async () => {
    const { wrapper } = mountQuickAdd({ listId: 'errands' });
    const vm = wrapper.vm as unknown as { focus: () => void; clear: () => void };
    await input(wrapper).setValue('Renew passport');

    vm.focus();
    expect(document.activeElement).toBe(input(wrapper).element);

    vm.clear();
    await flushPromises();
    expect(input(wrapper).element.value).toBe('');
  });

  it('marks the check column with a dashed ring and shows focus as the rows\' inset ring', () => {
    const { wrapper } = mountQuickAdd({ listId: 'errands' });

    expect(wrapper.get('[data-test="quick-add-mark"]').classes()).toEqual(
      expect.arrayContaining(['h-6', 'w-6', 'rounded-full', 'border-dashed']),
    );
    expect(input(wrapper).classes()).toContain('peer');
    // Story 29: the project's focus ring (ring-2 ring-primary-500), inset like a focused row's,
    // shown only while the box has focus.
    const focus = wrapper.get('[data-test="quick-add-focus"]');
    expect(focus.classes()).toEqual(
      expect.arrayContaining(['absolute', 'inset-0', 'ring-2', 'ring-inset', 'ring-primary-500', 'opacity-0', 'peer-focus:opacity-100']),
    );
    expect(focus.attributes('aria-hidden')).toBe('true');
    expect(focus.element.previousElementSibling).toBe(input(wrapper).element);
  });
});
