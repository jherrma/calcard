// @vitest-environment nuxt
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils';
import { createTestingPinia } from '@pinia/testing';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import { defineComponent, h } from 'vue';
import TaskCreateDialog from './TaskCreateDialog.vue';
import TaskEditDialog from './TaskEditDialog.vue';
import TaskForm from './TaskForm.vue';
import { useCalendarStore } from '~/stores/calendars';
import { useTasksStore } from '~/stores/tasks';
import type { Calendar } from '~/types/calendar';
import type { Task, TaskFormData } from '~/types/tasks';
import { taskToFormData } from '~/utils/taskForm';
import PrimeVue from 'primevue/config';

// SCOPE (#170 story 26, spec §7.3/§7.5/§8): "Add a task" and the task editor.
// Both wrap the REAL TaskForm (its PrimeVue inputs are shallow stubs) in a
// Dialog stub that renders its content. The tasks store runs for real
// against a mocked useApi, so the specs assert the REQUESTS: create posts the
// form to the chosen list; a save PATCHes only what changed, and an
// unchanged save sends nothing; a repeating task never sends status or
// progress; failures keep the dialog open; "Delete task" asks, deletes for
// good and says "Task deleted"; a read-only list has no Save.

const { apiMock, toastMock, confirmMock } = vi.hoisted(() => ({
  apiMock: vi.fn(),
  toastMock: { success: vi.fn(), error: vi.fn(), warn: vi.fn(), info: vi.fn(), action: vi.fn() },
  confirmMock: { require: vi.fn(), close: vi.fn() },
}));
mockNuxtImport('useApi', () => () => apiMock);
mockNuxtImport('useAppToast', () => () => toastMock);
mockNuxtImport('useConfirm', () => () => confirmMock);

enableAutoUnmount(afterEach);

let previousTz: string | undefined;
beforeAll(() => {
  previousTz = process.env.TZ;
  process.env.TZ = 'Europe/Berlin';
});
afterAll(() => {
  if (previousTz === undefined) delete process.env.TZ;
  else process.env.TZ = previousTz;
});

function local(year: number, month: number, day: number, hour = 0, minute = 0): Date {
  return new Date(year, month - 1, day, hour, minute);
}

const DialogStub = defineComponent({
  name: 'Dialog',
  props: {
    visible: Boolean,
    header: { type: String, default: '' },
    breakpoints: { type: Object, default: undefined },
    modal: Boolean,
  },
  emits: ['update:visible', 'after-hide'],
  setup(props, { slots }) {
    return () => (props.visible ? h('div', { 'data-test': 'dialog' }, slots.default?.()) : null);
  },
});

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
  calendar('errands', { name: 'Errands' }),
  calendar('home', { name: 'Home', kind: 'both' }),
  calendar('team', { name: 'Team tasks', shared: true, permission: 'read-only' }),
];

function task(overrides: Partial<Task> = {}): Task {
  return {
    id: 'task-1',
    calendar_id: 1,
    calendar_uuid: 'errands',
    uid: 'task-1@calcard.io',
    summary: 'Milk, eggs; bread',
    description: 'Line one\nC:\\shopping, aisle 3; left',
    status: 'in-process',
    completed: false,
    completed_at: null,
    percent_complete: 40,
    priority: 3,
    due: '2026-11-05T16:00:00Z',
    due_date: null,
    due_all_day: false,
    start: null,
    start_date: null,
    is_recurring: false,
    rrule: null,
    etag: '"1"',
    created_at: '2026-10-01T08:00:00Z',
    updated_at: '2026-10-01T08:00:00Z',
    calendar_name: 'Errands',
    calendar_color: '#123456',
    ...overrides,
  };
}

interface ApiOptions {
  method?: string;
  body?: Record<string, unknown>;
}
type Handler = (options: ApiOptions) => unknown;

function serve(handlers: Record<string, Handler> = {}) {
  apiMock.mockImplementation(async (url: string, options: ApiOptions = {}) => {
    const key = `${options.method ?? 'GET'} ${url}`;
    if (handlers[key]) return handlers[key]!(options);
    if (key === 'GET /api/v1/tasks/summary') return { open: 0, overdue: 0, due_today: 0, completed: 0, by_calendar: {} };
    throw new Error(`unexpected request ${key}`);
  });
}

/** Every request but the summary refetch each mutation makes. */
const writes = () =>
  apiMock.mock.calls
    .filter(([url]) => url !== '/api/v1/tasks/summary')
    .map(([url, options]) => ({ key: `${(options as ApiOptions).method} ${url}`, body: (options as ApiOptions).body }));

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((res) => {
    resolve = res;
  });
  return { promise, resolve };
}

function setupStores(tasks: Task[] = []) {
  const pinia = createTestingPinia({ stubActions: false });
  useCalendarStore().calendars = LISTS;
  const store = useTasksStore();
  store.tasks = tasks;
  store.statusFilter = 'all';
  return { pinia, store };
}

const STUBS = { Dialog: DialogStub, TaskForm: false };

interface FormVm {
  form: TaskFormData;
  handleSubmit: () => Promise<void>;
}

beforeEach(() => {
  apiMock.mockReset();
  serve();
  Object.values(toastMock).forEach((fn) => fn.mockReset());
  confirmMock.require.mockReset();
});

describe('TaskCreateDialog', () => {
  function mountCreate(props: Record<string, unknown> = {}) {
    const { pinia, store } = setupStores();
    const wrapper = mount(TaskCreateDialog, {
      props: { visible: true, defaultListId: 'home', ...props },
      shallow: true,
      global: { plugins: [pinia], stubs: STUBS },
    });
    const form = () => wrapper.findComponent(TaskForm);
    return { wrapper, store, form, formVm: () => form().vm as unknown as FormVm };
  }

  it('is "Add a task", modal, full width on small screens, with the form for a new task', () => {
    const { wrapper, form } = mountCreate({ initialSummary: 'Buy milk' });
    const dialog = wrapper.getComponent(DialogStub);

    expect(dialog.props('header')).toBe('Add a task');
    expect(dialog.props('modal')).toBe(true);
    expect(dialog.props('breakpoints')).toEqual({ '640px': 'calc(100vw - 2rem)' });
    expect(form().props()).toMatchObject({ defaultListId: 'home', initialSummary: 'Buy milk', task: undefined });
    expect((form().vm as unknown as FormVm).form.summary).toBe('Buy milk');
  });

  it('Save creates the task from the form in the chosen list, then closes and emits it', async () => {
    serve({
      'POST /api/v1/calendars/errands/tasks': (o) => task({ id: 'new', summary: o.body!.summary as string, calendar_uuid: 'errands' }),
    });
    const { wrapper, formVm } = mountCreate();
    const vm = formVm();
    vm.form.summary = '  Renew passport ';
    vm.form.listId = 'errands';
    vm.form.due = local(2026, 11, 6);
    vm.form.priority = 'medium';
    vm.form.description = 'Photos, two; C:\\x';

    await vm.handleSubmit();
    await flushPromises();

    expect(writes()).toEqual([{
      key: 'POST /api/v1/calendars/errands/tasks',
      body: { summary: 'Renew passport', due_all_day: true, due: '2026-11-06', priority: 5, description: 'Photos, two; C:\\x' },
    }]);
    expect(wrapper.emitted('update:visible')).toEqual([[false]]);
    expect(wrapper.emitted('created')).toEqual([[expect.objectContaining({ id: 'new' })]]);
  });

  it('a failed create keeps the dialog open with what was typed', async () => {
    serve({ 'POST /api/v1/calendars/home/tasks': () => { throw Object.assign(new Error('x'), { statusCode: 500 }); } });
    const { wrapper, formVm } = mountCreate({ initialSummary: 'Buy milk' });

    await formVm().handleSubmit();
    await flushPromises();

    expect(wrapper.emitted('update:visible')).toBeUndefined();
    expect(wrapper.emitted('created')).toBeUndefined();
    expect(formVm().form.summary).toBe('Buy milk');
    expect(toastMock.error).toHaveBeenCalledWith('Check your connection and try again.', "Couldn't add the task");
  });

  it('shows the save in progress on the form', async () => {
    const answer = deferred<Task>();
    serve({ 'POST /api/v1/calendars/home/tasks': () => answer.promise });
    const { form, formVm } = mountCreate({ initialSummary: 'Buy milk' });

    void formVm().handleSubmit();
    await flushPromises();
    expect(form().props('isSubmitting')).toBe(true);

    answer.resolve(task({ id: 'new', calendar_uuid: 'home' }));
    await flushPromises();
    expect(form().props('isSubmitting')).toBe(false);
  });

  it('Cancel closes without a request', () => {
    const { wrapper, form } = mountCreate();

    form().vm.$emit('cancel');

    expect(wrapper.emitted('update:visible')).toEqual([[false]]);
    expect(writes()).toEqual([]);
  });
});

describe('TaskEditDialog', () => {
  function mountEdit(t: Task, props: Record<string, unknown> = {}) {
    const { pinia, store } = setupStores([t]);
    const wrapper = mount(TaskEditDialog, {
      props: { visible: true, task: t, ...props },
      shallow: true,
      global: { plugins: [pinia], stubs: STUBS },
    });
    const form = () => wrapper.findComponent(TaskForm);
    return { wrapper, store, form, formVm: () => form().vm as unknown as FormVm };
  }

  it('is "Edit task", modal, full width on small screens, with the task in the form', () => {
    const t = task();
    const { wrapper, form } = mountEdit(t);
    const dialog = wrapper.getComponent(DialogStub);

    expect(dialog.props('header')).toBe('Edit task');
    expect(dialog.props('modal')).toBe(true);
    expect(dialog.props('breakpoints')).toEqual({ '640px': 'calc(100vw - 2rem)' });
    expect(form().props()).toMatchObject({ task: t, readOnly: false });
  });

  it('a task in a read-only list opens as "Task details" with no Save, and never saves', async () => {
    const t = task({ calendar_uuid: 'team' });
    const { wrapper, form, formVm } = mountEdit(t, { readOnly: true });

    expect(wrapper.getComponent(DialogStub).props('header')).toBe('Task details');
    expect(form().props('readOnly')).toBe(true);
    expect(form().find('[data-test="save-task"]').exists()).toBe(false);
    expect(form().find('[data-test="delete-task"]').exists()).toBe(false);

    formVm().form.summary = 'Changed';
    await formVm().handleSubmit();
    // Even a changed submit reaching the dialog is not saved, and a delete is not asked.
    form().vm.$emit('submit', { ...formVm().form }, taskToFormData(t));
    form().vm.$emit('delete');
    await flushPromises();
    expect(writes()).toEqual([]);
    expect(confirmMock.require).not.toHaveBeenCalled();
    expect(wrapper.emitted('update:visible')).toBeUndefined();
  });

  it('an unchanged save sends nothing and closes (text with , ; \\ and a newline as stored)', async () => {
    const { wrapper, formVm } = mountEdit(task());

    await formVm().handleSubmit();
    await flushPromises();

    expect(apiMock).not.toHaveBeenCalled();
    expect(wrapper.emitted('update:visible')).toEqual([[false]]);
    expect(wrapper.emitted('saved')).toBeUndefined();
  });

  it('a save PATCHes only what changed, puts the answer in the list, then closes', async () => {
    serve({
      'PATCH /api/v1/calendars/errands/tasks/task-1': (o) => task({ ...(o.body as Partial<Task>), etag: '"2"' }),
    });
    const { wrapper, store, formVm } = mountEdit(task());
    formVm().form.summary = 'Milk, eggs; bread, butter';
    formVm().form.priority = 'low';

    await formVm().handleSubmit();
    await flushPromises();

    expect(writes()).toEqual([{
      key: 'PATCH /api/v1/calendars/errands/tasks/task-1',
      body: { summary: 'Milk, eggs; bread, butter', priority: 9 },
    }]);
    expect(store.tasks[0]!.summary).toBe('Milk, eggs; bread, butter');
    expect(wrapper.emitted('update:visible')).toEqual([[false]]);
    expect(wrapper.emitted('saved')).toEqual([[expect.objectContaining({ etag: '"2"' })]]);
  });

  it('a new due date goes out with the start, the zone and no all-day flag', async () => {
    serve({ 'PATCH /api/v1/calendars/errands/tasks/task-1': () => task() });
    const { formVm } = mountEdit(task());
    formVm().form.due = local(2026, 11, 6, 18);

    await formVm().handleSubmit();
    await flushPromises();

    expect(writes()[0]!.body).toEqual({ due: '2026-11-06T18:00:00+01:00', start: null, timezone: 'Europe/Berlin' });
  });

  it('a repeating task never sends status or progress (the server would answer 409)', async () => {
    serve({ 'PATCH /api/v1/calendars/errands/tasks/task-1': () => task({ is_recurring: true }) });
    const { formVm } = mountEdit(task({ is_recurring: true, rrule: 'FREQ=DAILY', status: 'completed', percent_complete: 100 }));
    formVm().form.status = 'needs-action';
    formVm().form.percent = 0;
    formVm().form.summary = 'Water plants';

    await formVm().handleSubmit();
    await flushPromises();

    expect(writes()).toEqual([{ key: 'PATCH /api/v1/calendars/errands/tasks/task-1', body: { summary: 'Water plants' } }]);
  });

  it('a failed save keeps the dialog open with the edits', async () => {
    serve({
      'PATCH /api/v1/calendars/errands/tasks/task-1': () => {
        throw Object.assign(new Error('x'), { statusCode: 400, data: { status: 'error', message: 'due must not be before start' } });
      },
    });
    const { wrapper, formVm } = mountEdit(task());
    formVm().form.summary = 'Changed';

    await formVm().handleSubmit();
    await flushPromises();

    expect(wrapper.emitted('update:visible')).toBeUndefined();
    expect(formVm().form.summary).toBe('Changed');
    expect(toastMock.error).toHaveBeenCalledWith('due must not be before start', "Couldn't save the task");
  });

  it('"Delete task" asks; on yes it deletes for good, says "Task deleted" and closes', async () => {
    serve({ 'DELETE /api/v1/calendars/errands/tasks/task-1': () => null });
    const t = task();
    const { wrapper, store, form } = mountEdit(t);

    form().vm.$emit('delete');
    await flushPromises();
    expect(confirmMock.require).toHaveBeenCalledTimes(1);
    const ask = confirmMock.require.mock.calls[0]![0] as { header: string; message: string; acceptLabel: string; accept: () => void };
    expect(ask.header).toBe('Delete task');
    expect(ask.message).toBe('Delete this task? It is removed from every device that syncs this list.');
    expect(ask.acceptLabel).toBe('Delete task');
    expect(writes()).toEqual([]);

    ask.accept();
    await flushPromises();

    expect(writes()).toEqual([{ key: 'DELETE /api/v1/calendars/errands/tasks/task-1', body: undefined }]);
    expect(store.tasks).toEqual([]);
    expect(toastMock.success).toHaveBeenCalledWith('Task deleted');
    // A deleted task is gone on the server: no Undo is offered.
    expect(toastMock.action).not.toHaveBeenCalled();
    expect(wrapper.emitted('update:visible')).toEqual([[false]]);
    expect(wrapper.emitted('deleted')).toEqual([[t]]);
  });

  it('"Delete task" answered no: nothing is deleted and the dialog stays', async () => {
    const { wrapper, form } = mountEdit(task());

    form().vm.$emit('delete');
    await flushPromises();
    (confirmMock.require.mock.calls[0]![0] as { reject: () => void }).reject();
    await flushPromises();

    expect(writes()).toEqual([]);
    expect(toastMock.success).not.toHaveBeenCalled();
    expect(wrapper.emitted('update:visible')).toBeUndefined();
  });

  it('a new task in the same dialog gets a fresh form (keyed by task)', async () => {
    const { wrapper, formVm } = mountEdit(task());
    formVm().form.summary = 'Half-typed edit';

    await wrapper.setProps({ task: task({ id: 'task-2', summary: 'Other task' }) });

    expect(formVm().form.summary).toBe('Other task');
  });

  it('passes on the end of its closing transition (the page restores the focus then)', () => {
    const { wrapper } = mountEdit(task());

    wrapper.getComponent(DialogStub).vm.$emit('after-hide');

    expect(wrapper.emitted('after-hide')).toHaveLength(1);
  });
});

// Story 26, spec §8 "Dialogs trap focus and return it to the originating row
// (PrimeVue default; verify)": the REAL PrimeVue Dialog, with real transitions.
// PrimeVue keeps `document.activeElement` when the dialog enters and focuses it
// again when it leaves; the list focuses the row before it opens the editor.
describe('TaskEditDialog — focus with the real PrimeVue Dialog', () => {
  const FormStub = defineComponent({
    name: 'TaskForm',
    emits: ['submit', 'cancel', 'delete'],
    setup(_props, { emit }) {
      return () => h('form', [
        h('input', { 'data-test': 'first-field' }),
        h('button', { type: 'button', 'data-test': 'stub-cancel', onClick: () => emit('cancel') }, 'Cancel'),
      ]);
    },
  });
  const frames = () => new Promise((resolve) => setTimeout(resolve, 50));

  it('moves the focus into the dialog, keeps Tab inside it, and gives it back to the row that opened it', async () => {
    const row = document.createElement('li');
    row.tabIndex = 0;
    row.dataset.test = 'origin-row';
    document.body.appendChild(row);
    row.focus();
    const { pinia } = setupStores([task()]);
    const wrapper = mount(TaskEditDialog, {
      props: { visible: false, task: task() },
      attachTo: document.body,
      global: { plugins: [pinia, PrimeVue], stubs: { TaskForm: FormStub, transition: false } },
    });

    await wrapper.setProps({ visible: true });
    await flushPromises();
    await frames();
    const dialog = document.querySelector<HTMLElement>('.p-dialog')!;
    expect(dialog).not.toBeNull();
    expect(dialog.getAttribute('role')).toBe('dialog');
    expect(dialog.getAttribute('aria-modal')).toBe('true');
    expect(dialog.contains(document.activeElement)).toBe(true);
    // The focus trap: PrimeVue's FocusTrap directive puts hidden sentinels at both ends.
    expect(dialog.querySelectorAll('[data-pc-section="firstfocusableelement"], [data-pc-section="lastfocusableelement"]').length).toBe(2);

    document.querySelector<HTMLButtonElement>('[data-test="stub-cancel"]')!.click();
    await flushPromises();
    await frames();

    expect(wrapper.emitted('update:visible')).toEqual([[false]]);
    await wrapper.setProps({ visible: false });
    await flushPromises();
    await frames();
    expect(document.activeElement).toBe(row);
    expect(wrapper.emitted('after-hide')).toHaveLength(1);
    row.remove();
  });
});
