// @vitest-environment nuxt
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils';
import { createTestingPinia } from '@pinia/testing';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import { defineComponent, h } from 'vue';
import TaskListSettingsDialog from './TaskListSettingsDialog.vue';
import CalendarSettingsDialog from '~/components/calendar/CalendarSettingsDialog.vue';
import { useCalendarStore } from '~/stores/calendars';
import type { Calendar } from '~/types/calendar';

// SCOPE (#170 story 27, spec §7.3): the wrapper. It is the calendar settings
// dialog with the list's kind, so it opens with task-list copy; it relays
// `updated` and `deleted` (with the list); and its `confirmDelete(list)` is
// the sidebar's "Delete task list": the same question and delete as the
// dialog's own button. Rename/recolour go through for real (mocked useApi).

const { apiMock, toastMock, confirmRequire } = vi.hoisted(() => ({
  apiMock: vi.fn(),
  toastMock: { success: vi.fn(), error: vi.fn(), warn: vi.fn(), info: vi.fn(), action: vi.fn() },
  confirmRequire: vi.fn(),
}));
mockNuxtImport('useApi', () => () => apiMock);
mockNuxtImport('useAppToast', () => () => toastMock);
vi.mock('primevue/useconfirm', () => ({ useConfirm: () => ({ require: confirmRequire }) }));

enableAutoUnmount(afterEach);

const DialogStub = defineComponent({
  name: 'Dialog',
  props: { visible: Boolean, header: { type: String, default: '' } },
  setup(props, { slots }) {
    return () => (props.visible ? h('div', [h('h2', props.header), slots.default?.(), slots.footer?.()]) : null);
  },
});

const slot = (name: string) => defineComponent({
  name,
  setup(_props, { slots }) {
    return () => h('div', slots.default?.());
  },
});
const TAB_STUBS = Object.fromEntries(['Tabs', 'TabList', 'Tab', 'TabPanels', 'TabPanel'].map((n) => [n, slot(n)]));

const ButtonStub = defineComponent({
  name: 'Button',
  props: { label: { type: String, default: '' }, disabled: Boolean },
  emits: ['click'],
  setup(props, { emit }) {
    return () => h('button', { type: 'button', disabled: props.disabled, onClick: (e: Event) => emit('click', e) }, props.label);
  },
});

const InputStub = defineComponent({
  name: 'InputText',
  inheritAttrs: false,
  props: { modelValue: { type: String, default: '' } },
  emits: ['update:modelValue'],
  setup(props, { emit, attrs }) {
    return () => h('input', {
      ...attrs,
      value: props.modelValue,
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
    color: '#3788d8',
    kind: 'tasks',
    description: '',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...extra,
  };
}

const ERRANDS = calendar('errands', { name: 'Errands' });
const HOME = calendar('home', { name: 'Home' });

interface ApiOptions {
  method?: string;
  body?: Record<string, unknown>;
}

function serve(handlers: Record<string, (o: ApiOptions) => unknown> = {}) {
  apiMock.mockImplementation(async (url: string, options: ApiOptions = {}) => {
    const key = `${options.method ?? 'GET'} ${url}`;
    const handler = handlers[key];
    if (!handler) throw new Error(`unexpected request ${key}`);
    return handler(options);
  });
}

function mountWrapper(props: Record<string, unknown> = {}) {
  const pinia = createTestingPinia({ stubActions: false });
  const store = useCalendarStore();
  store.calendars = [ERRANDS, HOME];
  const wrapper = mount(TaskListSettingsDialog, {
    props: { visible: true, list: ERRANDS, initialTab: 'general', ...props },
    shallow: true,
    global: {
      plugins: [pinia],
      stubs: { CalendarSettingsDialog: false, Dialog: DialogStub, Button: ButtonStub, InputText: InputStub, ...TAB_STUBS },
    },
  });
  return { wrapper, store, inner: () => wrapper.getComponent(CalendarSettingsDialog) };
}

const button = (w: ReturnType<typeof mountWrapper>['wrapper'], label: string) => w.findAll('button').find((b) => b.text() === label)!;
const lastConfirm = () => confirmRequire.mock.calls.at(-1)![0] as { message: string; header: string; accept: () => Promise<void> };

beforeEach(() => {
  apiMock.mockReset();
  serve();
  confirmRequire.mockReset();
  Object.values(toastMock).forEach((fn) => fn.mockReset());
});

describe('TaskListSettingsDialog', () => {
  it('is the calendar settings dialog for the list, with kind "tasks" and the asked-for tab', () => {
    const { wrapper, inner } = mountWrapper({ initialTab: 'sharing' });

    expect(inner().props()).toMatchObject({ visible: true, calendar: ERRANDS, kind: 'tasks', initialTab: 'sharing' });
    expect(wrapper.get('h2').text()).toBe('Errands settings');
    expect(wrapper.text()).toContain('Delete task list');
    expect(wrapper.findAll('label').map((l) => l.text())).toEqual(['Name', 'Color', 'Description', 'CalDAV URL']);
  });

  it('passes the list\'s own kind (a task list is "tasks"; nothing chosen yet counts as one)', () => {
    const { inner, wrapper } = mountWrapper({ list: null, visible: false });

    expect(inner().props('kind')).toBe('tasks');
    return wrapper.setProps({ list: { ...ERRANDS, kind: 'both' } }).then(() => {
      expect(inner().props('kind')).toBe('both');
    });
  });

  it('renames and recolours the list, and relays "updated"; closing is relayed too', async () => {
    serve({ 'PATCH /api/v1/calendars/errands': (o) => ({ ...ERRANDS, ...o.body }) });
    const { wrapper, store } = mountWrapper();
    const [name, color] = wrapper.findAll('input');

    await name!.setValue('Groceries');
    await color!.setValue('d6336c');
    await button(wrapper, 'Save').trigger('click');
    await flushPromises();

    expect(apiMock).toHaveBeenCalledWith('/api/v1/calendars/errands', { method: 'PATCH', body: { name: 'Groceries', color: '#d6336c' } });
    expect(store.calendars.find((c) => c.uuid === 'errands')).toMatchObject({ name: 'Groceries', color: '#d6336c', kind: 'tasks' });
    expect(wrapper.emitted('updated')).toEqual([[]]);
    expect(wrapper.emitted('update:visible')).toEqual([[false]]);
  });

  it('relays "deleted" with the list after the dialog\'s own "Delete task list"', async () => {
    serve({ 'DELETE /api/v1/calendars/errands': () => undefined });
    const { wrapper } = mountWrapper();

    await button(wrapper, 'Delete task list').trigger('click');
    await lastConfirm().accept();
    await flushPromises();

    expect(wrapper.emitted('deleted')).toEqual([[ERRANDS]]);
  });

  it('confirmDelete(list) asks the same question about that list, closed, and deletes it on accept', async () => {
    serve({ 'DELETE /api/v1/calendars/home': () => undefined });
    const { wrapper, store } = mountWrapper({ visible: false });

    (wrapper.vm as unknown as { confirmDelete: (l: Calendar) => void }).confirmDelete(HOME);

    expect(lastConfirm()).toMatchObject({ header: 'Delete task list', message: taskListDeleteMessage('Home') });
    expect(apiMock).not.toHaveBeenCalled();
    await lastConfirm().accept();
    await flushPromises();

    expect(apiMock).toHaveBeenCalledWith('/api/v1/calendars/home', { method: 'DELETE', body: { confirmation: 'DELETE' } });
    expect(store.calendars.map((c) => c.uuid)).toEqual(['errands']);
    expect(toastMock.success).toHaveBeenCalledWith('Task list deleted');
    expect(wrapper.emitted('deleted')).toEqual([[HOME]]);
  });
});
