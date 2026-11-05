// @vitest-environment nuxt
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils';
import { createTestingPinia } from '@pinia/testing';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import { defineComponent, h } from 'vue';
import AddTaskListDialog from './AddTaskListDialog.vue';
import { useCalendarStore } from '~/stores/calendars';
import { useTasksStore } from '~/stores/tasks';
import type { Calendar } from '~/types/calendar';

// SCOPE (#170 story 27, spec §7.3/§7.5): "Create task list". Name + colour,
// then the tasks store's createTaskList: POST /calendars with kind 'tasks',
// then GET /calendars, so the list is in `taskLists` (the sidebar's source)
// at once; "Task list created" on success; a failure keeps the dialog open
// with what was typed (the store toasts it). The stores run for real against
// a mocked useApi, so the specs assert the REQUESTS.

const { apiMock, toastMock } = vi.hoisted(() => ({
  apiMock: vi.fn(),
  toastMock: { success: vi.fn(), error: vi.fn(), warn: vi.fn(), info: vi.fn(), action: vi.fn() },
}));
mockNuxtImport('useApi', () => () => apiMock);
mockNuxtImport('useAppToast', () => () => toastMock);

enableAutoUnmount(afterEach);

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
    return () => (props.visible ? h('div', { 'data-test': 'dialog' }, [slots.default?.(), slots.footer?.()]) : null);
  },
});

const ButtonStub = defineComponent({
  name: 'Button',
  props: { label: { type: String, default: '' }, disabled: Boolean, loading: Boolean },
  emits: ['click'],
  setup(props, { emit, attrs }) {
    return () => h('button', {
      type: 'button',
      'data-test': attrs['data-test'],
      'data-loading': String(props.loading),
      disabled: props.disabled,
      onClick: (e: Event) => emit('click', e),
    }, props.label);
  },
});

const InputStub = defineComponent({
  name: 'InputText',
  inheritAttrs: false,
  props: { modelValue: { type: String, default: '' }, invalid: Boolean },
  emits: ['update:modelValue'],
  setup(props, { emit, attrs }) {
    return () => h('input', {
      ...attrs,
      value: props.modelValue,
      'data-invalid': String(props.invalid),
      onInput: (e: Event) => emit('update:modelValue', (e.target as HTMLInputElement).value),
    });
  },
});

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

// The first palette colour is taken by a task list (another case); the second only by an
// EVENT calendar, which never shows in the Tasks section, so a new list starts on the second.
const EXISTING: Calendar[] = [
  calendar('personal', { name: 'Personal', kind: 'events', color: '#2F9E44' }),
  calendar('errands', { name: 'Errands', kind: 'tasks', color: '#3788D8' }),
];

interface ApiOptions {
  method?: string;
  body?: Record<string, unknown>;
}

let serverCalendars: Calendar[] = [];

function serve(handlers: Record<string, (o: ApiOptions) => unknown> = {}) {
  const all: Record<string, (o: ApiOptions) => unknown> = {
    'GET /api/v1/calendars': () => ({ calendars: serverCalendars }),
    'GET /api/v1/tasks/summary': () => ({ open: 0, overdue: 0, due_today: 0, completed: 0, by_calendar: {} }),
    'POST /api/v1/calendars': (o) => {
      const created = calendar('groceries', { name: o.body!.name as string, color: o.body!.color as string, kind: 'tasks', supported_components: 'VTODO' });
      serverCalendars = [...serverCalendars, created];
      return created;
    },
    ...handlers,
  };
  apiMock.mockImplementation(async (url: string, options: ApiOptions = {}) => {
    const key = `${options.method ?? 'GET'} ${url}`;
    const handler = all[key];
    if (!handler) throw new Error(`unexpected request ${key}`);
    return handler(options);
  });
}

const requests = () => apiMock.mock.calls
  .filter(([url]) => url !== '/api/v1/tasks/summary')
  .map(([url, o]) => ({ key: `${(o as ApiOptions | undefined)?.method ?? 'GET'} ${url}`, body: (o as ApiOptions | undefined)?.body }));

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((res) => {
    resolve = res;
  });
  return { promise, resolve };
}

function mountDialog(visible = true) {
  const pinia = createTestingPinia({ stubActions: false });
  const calendarStore = useCalendarStore();
  calendarStore.calendars = [...EXISTING];
  const wrapper = mount(AddTaskListDialog, {
    props: { visible },
    shallow: true,
    global: { plugins: [pinia], stubs: { Dialog: DialogStub, Button: ButtonStub, InputText: InputStub } },
  });
  return { wrapper, calendarStore, tasksStore: useTasksStore() };
}

const nameInput = (w: ReturnType<typeof mountDialog>['wrapper']) => w.get('[data-test="list-name"]');
const colorInput = (w: ReturnType<typeof mountDialog>['wrapper']) => w.get('[data-test="list-color"]');
const createButton = (w: ReturnType<typeof mountDialog>['wrapper']) => w.get('[data-test="create-list"]');

beforeEach(() => {
  apiMock.mockReset();
  serverCalendars = [...EXISTING];
  serve();
  Object.values(toastMock).forEach((fn) => fn.mockReset());
});

describe('AddTaskListDialog — the form', () => {
  it('is "Create task list", modal, full width on small screens, with a labelled name and colour', () => {
    const { wrapper } = mountDialog();
    const dialog = wrapper.getComponent(DialogStub);

    expect(dialog.props('header')).toBe('Create task list');
    expect(dialog.props('modal')).toBe(true);
    expect(dialog.props('breakpoints')).toEqual({ '640px': 'calc(100vw - 2rem)' });
    const labels = wrapper.findAll('label');
    expect(labels.map((l) => l.text())).toEqual(['Name', 'Color']);
    expect(labels[0]!.attributes('for')).toBe(nameInput(wrapper).attributes('id'));
    expect(labels[1]!.attributes('for')).toBe(colorInput(wrapper).attributes('id'));
    // PrimeVue's Dialog focuses the [autofocus] element when it opens.
    expect(nameInput(wrapper).attributes('autofocus')).toBeDefined();
    expect(wrapper.findAll('button').map((b) => b.text())).toEqual(['Cancel', 'Create task list']);
  });

  it('starts empty, on the first colour no task list has yet (compared without case; event calendars do not count)', () => {
    const { wrapper } = mountDialog();

    expect((nameInput(wrapper).element as HTMLInputElement).value).toBe('');
    expect((colorInput(wrapper).element as HTMLInputElement).value).toBe('2f9e44');
  });

  it('a blank name cannot be created: the button is disabled and Enter sends nothing', async () => {
    const { wrapper } = mountDialog();

    expect(createButton(wrapper).attributes('disabled')).toBeDefined();
    await nameInput(wrapper).setValue('   ');
    await nameInput(wrapper).trigger('keydown', { key: 'Enter' });
    await flushPromises();

    expect(createButton(wrapper).attributes('disabled')).toBeDefined();
    expect(requests()).toEqual([]);
  });

  it('a colour that is not hex says what to type and cannot be created', async () => {
    const { wrapper } = mountDialog();
    await nameInput(wrapper).setValue('Groceries');
    expect(wrapper.find('[data-test="color-error"]').exists()).toBe(false);

    await colorInput(wrapper).setValue('blue');

    const error = wrapper.get('[data-test="color-error"]');
    expect(error.text()).toBe('Enter the colour as a hex value, for example 3788d8.');
    expect(error.classes()).toEqual(expect.arrayContaining(['text-red-600', 'dark:text-red-400']));
    expect(colorInput(wrapper).attributes('aria-describedby')).toBe(error.attributes('id'));
    expect(colorInput(wrapper).attributes('data-invalid')).toBe('true');
    expect(createButton(wrapper).attributes('disabled')).toBeDefined();
    await colorInput(wrapper).trigger('keydown', { key: 'Enter' });
    await flushPromises();
    expect(requests()).toEqual([]);
  });

  it('Cancel closes without a request', async () => {
    const { wrapper } = mountDialog();
    await nameInput(wrapper).setValue('Groceries');

    await wrapper.findAll('button').find((b) => b.text() === 'Cancel')!.trigger('click');

    expect(wrapper.emitted('update:visible')).toEqual([[false]]);
    expect(requests()).toEqual([]);
  });

  it('every opening starts again from an empty name and a free colour', async () => {
    const { wrapper } = mountDialog();
    await nameInput(wrapper).setValue('Half typed');
    await colorInput(wrapper).setValue('nope');

    await wrapper.setProps({ visible: false });
    await wrapper.setProps({ visible: true });

    expect((nameInput(wrapper).element as HTMLInputElement).value).toBe('');
    expect((colorInput(wrapper).element as HTMLInputElement).value).toBe('2f9e44');
  });

  it('forwards after-hide', async () => {
    const { wrapper } = mountDialog();

    wrapper.getComponent(DialogStub).vm.$emit('after-hide');

    expect(wrapper.emitted('after-hide')).toEqual([[]]);
  });
});

describe('AddTaskListDialog — creating', () => {
  it('creates a kind "tasks" calendar with the trimmed name and the #rrggbb colour, refreshes the lists, says "Task list created"', async () => {
    const { wrapper, calendarStore, tasksStore } = mountDialog();
    await nameInput(wrapper).setValue('  Groceries ');
    await colorInput(wrapper).setValue('AE3EC9');

    await createButton(wrapper).trigger('click');
    await flushPromises();

    expect(requests()).toEqual([
      { key: 'POST /api/v1/calendars', body: { name: 'Groceries', color: '#ae3ec9', kind: 'tasks' } },
      { key: 'GET /api/v1/calendars', body: undefined },
    ]);
    expect(calendarStore.calendars.map((c) => c.uuid)).toEqual(['personal', 'errands', 'groceries']);
    expect(tasksStore.taskLists.map((l) => l.name)).toEqual(['Errands', 'Groceries']);
    expect(tasksStore.writableTaskLists.map((l) => l.uuid)).toContain('groceries');
    // A task list never shows in the Calendar section.
    expect(calendarStore.eventCalendars.map((c) => c.uuid)).toEqual(['personal']);
    expect(toastMock.success).toHaveBeenCalledWith('Task list created');
    expect(wrapper.emitted('created')).toEqual([[expect.objectContaining({ uuid: 'groceries', name: 'Groceries', kind: 'tasks' })]]);
    expect(wrapper.emitted('update:visible')).toEqual([[false]]);
  });

  it('Enter in the name creates too; a three-digit colour is expanded', async () => {
    const { wrapper } = mountDialog();
    await nameInput(wrapper).setValue('Groceries');
    await colorInput(wrapper).setValue('#0c8');

    await nameInput(wrapper).trigger('keydown', { key: 'Enter' });
    await flushPromises();

    expect(requests()[0]).toEqual({ key: 'POST /api/v1/calendars', body: { name: 'Groceries', color: '#00cc88', kind: 'tasks' } });
    expect(wrapper.emitted('created')).toHaveLength(1);
  });

  it('shows the create in progress and sends it once however often it is asked', async () => {
    const answer = deferred<Calendar>();
    serve({ 'POST /api/v1/calendars': () => answer.promise });
    const { wrapper } = mountDialog();
    await nameInput(wrapper).setValue('Groceries');

    await createButton(wrapper).trigger('click');
    await nameInput(wrapper).trigger('keydown', { key: 'Enter' });
    await createButton(wrapper).trigger('click');

    expect(createButton(wrapper).attributes('data-loading')).toBe('true');
    expect(createButton(wrapper).attributes('disabled')).toBeDefined();
    expect(requests().filter((r) => r.key === 'POST /api/v1/calendars')).toHaveLength(1);

    serverCalendars = [...EXISTING, calendar('groceries', { name: 'Groceries', kind: 'tasks' })];
    answer.resolve(serverCalendars[2]!);
    await flushPromises();
    expect(createButton(wrapper).attributes('data-loading')).toBe('false');
  });

  it('a failed create keeps the dialog open with what was typed; the store says why', async () => {
    serve({ 'POST /api/v1/calendars': () => { throw Object.assign(new Error('x'), { statusCode: 400, data: { message: 'name cannot exceed 255 characters' } }); } });
    const { wrapper, tasksStore } = mountDialog();
    await nameInput(wrapper).setValue('Groceries');

    await createButton(wrapper).trigger('click');
    await flushPromises();

    expect(toastMock.error).toHaveBeenCalledWith('name cannot exceed 255 characters', "Couldn't create the task list");
    expect(toastMock.success).not.toHaveBeenCalled();
    expect(wrapper.emitted('created')).toBeUndefined();
    expect(wrapper.emitted('update:visible')).toBeUndefined();
    expect((nameInput(wrapper).element as HTMLInputElement).value).toBe('Groceries');
    expect(createButton(wrapper).attributes('disabled')).toBeUndefined();
    expect(tasksStore.taskLists.map((l) => l.uuid)).toEqual(['errands']);
  });
});
