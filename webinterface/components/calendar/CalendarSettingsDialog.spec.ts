// @vitest-environment nuxt
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils';
import { createTestingPinia } from '@pinia/testing';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import { defineComponent, h } from 'vue';
import CalendarSettingsDialog from './CalendarSettingsDialog.vue';
import SharePanel from '~/components/sharing/SharePanel.vue';
import PublicLinkPanel from '~/components/sharing/PublicLinkPanel.vue';
import { useCalendarStore } from '~/stores/calendars';
import type { Calendar, CalendarKind } from '~/types/calendar';

// SCOPE (#170 story 27, spec §7.3): the calendar settings dialog and its new
// `kind` prop. Without it (the Calendar section) everything is as it was:
// header, four tabs, Timezone, "Delete Calendar" and its question, a save
// that sends all four fields. With `kind: 'tasks'` (TaskListSettingsDialog)
// the copy talks about a task list, Timezone and Public Access are gone, the
// delete question warns that it is permanent, a save sends only what changed,
// and a blank name or a junk colour cannot be saved. The calendars store runs
// for real against a mocked useApi, so the specs assert the REQUESTS.

const { apiMock, toastMock, confirmRequire } = vi.hoisted(() => ({
  apiMock: vi.fn(),
  toastMock: { success: vi.fn(), error: vi.fn(), warn: vi.fn(), info: vi.fn(), action: vi.fn() },
  confirmRequire: vi.fn(),
}));
mockNuxtImport('useApi', () => () => apiMock);
mockNuxtImport('useAppToast', () => () => toastMock);
// Explicit import in the component: a bare mount has no ConfirmationService.
vi.mock('primevue/useconfirm', () => ({ useConfirm: () => ({ require: confirmRequire }) }));

enableAutoUnmount(afterEach);

const slot = (name: string) => defineComponent({
  name,
  props: { value: { type: String, default: undefined } },
  setup(props, { slots }) {
    return () => h('div', { 'data-stub': name, 'data-value': props.value }, slots.default?.());
  },
});

const DialogStub = defineComponent({
  name: 'Dialog',
  props: {
    visible: Boolean,
    header: { type: String, default: '' },
    breakpoints: { type: Object, default: undefined },
    modal: Boolean,
  },
  emits: ['update:visible'],
  setup(props, { slots }) {
    return () => (props.visible
      ? h('div', { 'data-test': 'dialog' }, [h('h2', { 'data-test': 'header' }, props.header), slots.default?.(), slots.footer?.()])
      : null);
  },
});

const ButtonStub = defineComponent({
  name: 'Button',
  props: { label: { type: String, default: '' }, disabled: Boolean, loading: Boolean },
  emits: ['click'],
  setup(props, { emit }) {
    return () => h('button', { type: 'button', disabled: props.disabled, onClick: (e: Event) => emit('click', e) }, props.label);
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

const TextareaStub = defineComponent({
  name: 'Textarea',
  inheritAttrs: false,
  props: { modelValue: { type: String, default: '' } },
  emits: ['update:modelValue'],
  setup(props, { emit, attrs }) {
    return () => h('textarea', {
      ...attrs,
      value: props.modelValue,
      onInput: (e: Event) => emit('update:modelValue', (e.target as HTMLTextAreaElement).value),
    });
  },
});

const STUBS = {
  Dialog: DialogStub,
  Button: ButtonStub,
  InputText: InputStub,
  Textarea: TextareaStub,
  Tabs: slot('Tabs'),
  TabList: slot('TabList'),
  Tab: slot('Tab'),
  TabPanels: slot('TabPanels'),
  TabPanel: slot('TabPanel'),
};

function calendar(uuid: string, extra: Partial<Calendar> = {}): Calendar {
  return {
    id: uuid,
    uuid,
    path: `${uuid}.ics`,
    name: uuid,
    color: '#3788D8',
    timezone: 'Europe/Berlin',
    description: 'Mine, all; mine \\o/',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...extra,
  };
}

const WORK = calendar('work', { name: 'Work', kind: 'events' });
const ERRANDS = calendar('errands', { name: 'Errands', kind: 'tasks', timezone: 'UTC', description: '' });

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

const requests = () => apiMock.mock.calls.map(([url, o]) => ({ key: `${(o as ApiOptions | undefined)?.method ?? 'GET'} ${url}`, body: (o as ApiOptions | undefined)?.body }));

function mountDialog(props: { calendar?: Calendar | null; kind?: CalendarKind; initialTab?: string; visible?: boolean } = {}) {
  const pinia = createTestingPinia({ stubActions: false });
  const store = useCalendarStore();
  store.calendars = [WORK, ERRANDS];
  const wrapper = mount(CalendarSettingsDialog, {
    props: { visible: true, calendar: WORK, ...props },
    shallow: true,
    global: { plugins: [pinia], stubs: STUBS },
  });
  return { wrapper, store };
}

const header = (w: ReturnType<typeof mountDialog>['wrapper']) => w.get('[data-test="header"]').text();
const tabs = (w: ReturnType<typeof mountDialog>['wrapper']) => w.findAll('[data-stub="Tab"]').map((t) => t.text());
const panels = (w: ReturnType<typeof mountDialog>['wrapper']) => w.findAll('[data-stub="TabPanel"]').map((t) => t.attributes('data-value'));
const labels = (w: ReturnType<typeof mountDialog>['wrapper']) => w.findAll('label').map((l) => l.text());
const button = (w: ReturnType<typeof mountDialog>['wrapper'], label: string) => {
  const found = w.findAll('button').find((b) => b.text() === label);
  if (!found) throw new Error(`no button "${label}"`);
  return found;
};
const nameInput = (w: ReturnType<typeof mountDialog>['wrapper']) => w.findAll('input').find((i) => i.attributes('id') === w.findAll('label')[0]!.attributes('for'))!;
const colorInput = (w: ReturnType<typeof mountDialog>['wrapper']) => w.findAll('input').find((i) => i.attributes('id') === w.findAll('label')[1]!.attributes('for'))!;
const lastConfirm = () => confirmRequire.mock.calls.at(-1)![0] as Record<string, unknown> & { accept: () => Promise<void> };

beforeEach(() => {
  apiMock.mockReset();
  serve();
  confirmRequire.mockReset();
  Object.values(toastMock).forEach((fn) => fn.mockReset());
});

describe('CalendarSettingsDialog — a calendar (no kind): exactly as before', () => {
  it('has the old header, all four tabs, Timezone and "Delete Calendar"', () => {
    const { wrapper } = mountDialog();

    expect(header(wrapper)).toBe('Work Settings');
    expect(tabs(wrapper)).toEqual(['General', 'Sharing', 'Public Access', 'Integration']);
    expect(panels(wrapper)).toEqual(['general', 'sharing', 'public', 'integration']);
    expect(labels(wrapper)).toEqual(['Name', 'Color', 'Timezone', 'Description', 'CalDAV URL']);
    expect(wrapper.findComponent(PublicLinkPanel).exists()).toBe(true);
    expect(wrapper.text()).toContain('Delete Calendar');
    expect(wrapper.text()).not.toContain('task list');
    expect(wrapper.getComponent(DialogStub).props('breakpoints')).toBeUndefined();
  });

  it('is the same for an `events` or a mixed (`both`) kind', () => {
    for (const kind of ['events', 'both'] as const) {
      const { wrapper } = mountDialog({ kind });

      expect(header(wrapper)).toBe('Work Settings');
      expect(tabs(wrapper)).toEqual(['General', 'Sharing', 'Public Access', 'Integration']);
      expect(labels(wrapper)).toContain('Timezone');
      expect(wrapper.text()).toContain('Delete Calendar');
      wrapper.unmount();
    }
  });

  it('shares as a calendar, managed only by its owner', () => {
    const { wrapper } = mountDialog();

    expect(wrapper.getComponent(SharePanel).props()).toMatchObject({
      resourceType: 'calendar', resourceUuid: 'work', canManage: true, resourceNoun: undefined,
    });
  });

  it('Save sends all four fields, says "Calendar updated" and closes', async () => {
    serve({ 'PATCH /api/v1/calendars/work': (o) => ({ ...WORK, ...o.body }) });
    const { wrapper } = mountDialog();

    await button(wrapper, 'Save').trigger('click');
    await flushPromises();

    expect(requests()).toEqual([{
      key: 'PATCH /api/v1/calendars/work',
      body: { name: 'Work', color: '#3788D8', timezone: 'Europe/Berlin', description: 'Mine, all; mine \\o/' },
    }]);
    expect(toastMock.success).toHaveBeenCalledWith('Calendar updated');
    expect(wrapper.emitted('updated')).toEqual([[]]);
    expect(wrapper.emitted('update:visible')).toEqual([[false]]);
  });

  it('does not validate: a blank name and a junk colour are still sent, and the error is the old one', async () => {
    serve({ 'PATCH /api/v1/calendars/work': () => { throw new Error('[PATCH] "/api/v1/calendars/work": 400 Bad Request'); } });
    const { wrapper } = mountDialog();
    await nameInput(wrapper).setValue('');
    await colorInput(wrapper).setValue('nope');

    expect(button(wrapper, 'Save').attributes('disabled')).toBeUndefined();
    expect(wrapper.find('[data-test="color-error"]').exists()).toBe(false);
    await button(wrapper, 'Save').trigger('click');
    await flushPromises();

    expect(requests()[0]!.body).toMatchObject({ name: '', color: '#nope' });
    expect(toastMock.error).toHaveBeenCalledWith('[PATCH] "/api/v1/calendars/work": 400 Bad Request');
    expect(wrapper.emitted('update:visible')).toBeUndefined();
  });

  it('"Delete Calendar" asks the old question, then deletes and says "Calendar deleted"', async () => {
    serve({ 'DELETE /api/v1/calendars/work': () => undefined });
    const { wrapper, store } = mountDialog();

    await button(wrapper, 'Delete Calendar').trigger('click');
    const options = lastConfirm();
    expect(options).toEqual({
      message: 'Are you sure you want to delete this calendar? All events will be permanently deleted.',
      header: 'Delete Calendar',
      icon: 'pi pi-exclamation-triangle',
      acceptClass: 'p-button-danger',
      accept: expect.any(Function),
    });
    expect(requests()).toEqual([]);

    await options.accept();
    await flushPromises();

    expect(requests()).toEqual([{ key: 'DELETE /api/v1/calendars/work', body: { confirmation: 'DELETE' } }]);
    expect(store.calendars.map((c) => c.uuid)).toEqual(['errands']);
    expect(toastMock.success).toHaveBeenCalledWith('Calendar deleted');
    expect(wrapper.emitted('deleted')).toEqual([[WORK]]);
    expect(wrapper.emitted('update:visible')).toEqual([[false]]);
  });

  it('a mixed (`both`) calendar says its tasks go too, and refetches the task counts after the delete (MN15, MN11)', async () => {
    const MIXED = calendar('mixed', { name: 'Mixed', kind: 'both' });
    serve({
      'DELETE /api/v1/calendars/mixed': () => undefined,
      'GET /api/v1/tasks/summary': () => ({ open: 0, overdue: 0, due_today: 0, completed: 0, by_calendar: {} }),
    });
    const { wrapper, store } = mountDialog({ calendar: MIXED });
    store.calendars = [WORK, MIXED];

    await button(wrapper, 'Delete Calendar').trigger('click');
    const options = lastConfirm();
    expect(options.message).toBe('Are you sure you want to delete this calendar? All events and tasks will be permanently deleted.');
    expect(options.header).toBe('Delete Calendar');

    await options.accept();
    await flushPromises();
    expect(requests().map((r) => r.key)).toEqual(['DELETE /api/v1/calendars/mixed', 'GET /api/v1/tasks/summary']);
  });

  it('an events calendar still says only events and asks for no task counts', async () => {
    serve({ 'DELETE /api/v1/calendars/work': () => undefined });
    const { wrapper } = mountDialog();

    await button(wrapper, 'Delete Calendar').trigger('click');
    expect(lastConfirm().message).toBe('Are you sure you want to delete this calendar? All events will be permanently deleted.');
    await lastConfirm().accept();
    await flushPromises();
    expect(requests().map((r) => r.key)).toEqual(['DELETE /api/v1/calendars/work']);
  });

  it('a failed delete shows the old error and keeps the dialog', async () => {
    serve({ 'DELETE /api/v1/calendars/work': () => { throw new Error('Cannot delete your last calendar'); } });
    const { wrapper } = mountDialog();

    await button(wrapper, 'Delete Calendar').trigger('click');
    await lastConfirm().accept();
    await flushPromises();

    expect(toastMock.error).toHaveBeenCalledWith('Cannot delete your last calendar');
    expect(wrapper.emitted('deleted')).toBeUndefined();
  });

  it('opens on the tab it is asked for, Public Access included', async () => {
    const { wrapper } = mountDialog({ visible: false, initialTab: 'public' });
    await wrapper.setProps({ visible: true });

    expect(wrapper.getComponent({ name: 'Tabs' }).props('value')).toBe('public');
  });
});

describe('CalendarSettingsDialog — kind "tasks" (a task list)', () => {
  const mountList = (extra: Parameters<typeof mountDialog>[0] = {}) => mountDialog({ calendar: ERRANDS, kind: 'tasks', ...extra });

  it('talks about a task list, without Timezone and without the Public Access tab', () => {
    const { wrapper } = mountList();

    expect(header(wrapper)).toBe('Errands settings');
    expect(tabs(wrapper)).toEqual(['General', 'Sharing', 'Integration']);
    expect(panels(wrapper)).toEqual(['general', 'sharing', 'integration']);
    expect(labels(wrapper)).toEqual(['Name', 'Color', 'Description', 'CalDAV URL']);
    expect(wrapper.findComponent(PublicLinkPanel).exists()).toBe(false);
    expect(wrapper.text()).toContain('Delete task list');
    expect(wrapper.text()).not.toContain('Delete Calendar');
    expect(wrapper.text()).not.toContain('publish');
  });

  it('is full width on small screens', () => {
    const { wrapper } = mountList();

    expect(wrapper.getComponent(DialogStub).props('breakpoints')).toEqual({ '640px': 'calc(100vw - 2rem)' });
  });

  it('shares it as a "task list", managed by its owner; a shared one gets no share management', async () => {
    const { wrapper } = mountList();

    expect(wrapper.getComponent(SharePanel).props()).toMatchObject({
      resourceType: 'calendar', resourceUuid: 'errands', canManage: true, resourceNoun: 'task list',
    });
    await wrapper.setProps({ calendar: { ...ERRANDS, shared: true, permission: 'read-write' } });
    expect(wrapper.getComponent(SharePanel).props('canManage')).toBe(false);
  });

  it('never offers the public tab, even when asked for it', async () => {
    const { wrapper } = mountList({ visible: false, initialTab: 'public' });
    await wrapper.setProps({ visible: true });

    expect(wrapper.getComponent({ name: 'Tabs' }).props('value')).toBe('general');
    await wrapper.setProps({ initialTab: 'sharing' });
    expect(wrapper.getComponent({ name: 'Tabs' }).props('value')).toBe('sharing');
  });

  it('renames: only the new name (trimmed) is sent, "Task list updated", closed', async () => {
    serve({ 'PATCH /api/v1/calendars/errands': (o) => ({ ...ERRANDS, ...o.body }) });
    const { wrapper, store } = mountList();

    await nameInput(wrapper).setValue('  Groceries ');
    await button(wrapper, 'Save').trigger('click');
    await flushPromises();

    expect(requests()).toEqual([{ key: 'PATCH /api/v1/calendars/errands', body: { name: 'Groceries' } }]);
    expect(store.calendars.find((c) => c.uuid === 'errands')?.name).toBe('Groceries');
    expect(toastMock.success).toHaveBeenCalledWith('Task list updated');
    expect(wrapper.emitted('updated')).toEqual([[]]);
    expect(wrapper.emitted('update:visible')).toEqual([[false]]);
  });

  it('recolours: only the colour, as #rrggbb (a typed shorthand is expanded)', async () => {
    serve({ 'PATCH /api/v1/calendars/errands': (o) => ({ ...ERRANDS, ...o.body }) });
    const { wrapper } = mountList();

    await colorInput(wrapper).setValue('2F9E44');
    await button(wrapper, 'Save').trigger('click');
    await flushPromises();
    await colorInput(wrapper).setValue('#abc');
    await button(wrapper, 'Save').trigger('click');
    await flushPromises();

    expect(requests().map((r) => r.body)).toEqual([{ color: '#2f9e44' }, { color: '#aabbcc' }]);
  });

  it('sends the description as typed (plain text) and never a timezone', async () => {
    serve({ 'PATCH /api/v1/calendars/errands': (o) => ({ ...ERRANDS, ...o.body }) });
    const { wrapper } = mountList();

    await wrapper.get('textarea').setValue('Milk, eggs; C:\\shop\nline two');
    await button(wrapper, 'Save').trigger('click');
    await flushPromises();

    expect(requests()).toEqual([{ key: 'PATCH /api/v1/calendars/errands', body: { description: 'Milk, eggs; C:\\shop\nline two' } }]);
  });

  it('an unchanged save sends nothing and says nothing, it just closes (same colour in another case too)', async () => {
    const { wrapper } = mountList({ calendar: { ...ERRANDS, color: '#3788D8', name: 'Errands ' } });

    await colorInput(wrapper).setValue('3788d8');
    await nameInput(wrapper).setValue('Errands');
    await button(wrapper, 'Save').trigger('click');
    await flushPromises();

    expect(requests()).toEqual([]);
    expect(toastMock.success).not.toHaveBeenCalled();
    expect(wrapper.emitted('updated')).toBeUndefined();
    expect(wrapper.emitted('update:visible')).toEqual([[false]]);
  });

  it('a blank name or a junk colour cannot be saved; the colour says what to type', async () => {
    const { wrapper } = mountList();

    await nameInput(wrapper).setValue('   ');
    expect(button(wrapper, 'Save').attributes('disabled')).toBeDefined();
    await nameInput(wrapper).setValue('Errands');
    expect(button(wrapper, 'Save').attributes('disabled')).toBeUndefined();

    await colorInput(wrapper).setValue('red');
    const error = wrapper.get('[data-test="color-error"]');
    expect(error.text()).toBe('Enter the colour as a hex value, for example 3788d8.');
    expect(colorInput(wrapper).attributes('aria-describedby')).toBe(error.attributes('id'));
    expect(colorInput(wrapper).attributes('data-invalid')).toBe('true');
    expect(button(wrapper, 'Save').attributes('disabled')).toBeDefined();
    await button(wrapper, 'Save').trigger('click');
    expect(requests()).toEqual([]);
  });

  it('a refused save shows the server\'s sentence under "Couldn\'t save the task list" and stays open; a network failure says what to do', async () => {
    let failure: unknown = Object.assign(new Error('x'), { statusCode: 400, data: { status: 'error', message: 'name cannot exceed 255 characters' } });
    serve({ 'PATCH /api/v1/calendars/errands': () => { throw failure; } });
    const { wrapper } = mountList();
    await nameInput(wrapper).setValue('x'.repeat(300));

    await button(wrapper, 'Save').trigger('click');
    await flushPromises();
    failure = Object.assign(new Error('x'), { statusCode: 502 });
    await button(wrapper, 'Save').trigger('click');
    await flushPromises();

    expect(toastMock.error.mock.calls).toEqual([
      ['name cannot exceed 255 characters', "Couldn't save the task list"],
      ['Check your connection and try again.', "Couldn't save the task list"],
    ]);
    expect(wrapper.emitted('update:visible')).toBeUndefined();
  });

  it('a rate-limited save is not toasted a second time (useApi already did)', async () => {
    serve({ 'PATCH /api/v1/calendars/errands': () => { throw Object.assign(new Error('x'), { statusCode: 429, data: { message: 'Too many requests' } }); } });
    const { wrapper } = mountList();
    await nameInput(wrapper).setValue('Groceries');

    await button(wrapper, 'Save').trigger('click');
    await flushPromises();

    expect(toastMock.error).not.toHaveBeenCalled();
  });

  it('reopened after a cancelled edit, it shows what is stored again', async () => {
    const { wrapper } = mountList();
    await nameInput(wrapper).setValue('Half typed');
    await button(wrapper, 'Cancel').trigger('click');
    await wrapper.setProps({ visible: false });
    await wrapper.setProps({ visible: true });

    expect((nameInput(wrapper).element as HTMLInputElement).value).toBe('Errands');
  });

  it('"Delete task list" warns that it is permanent and deletes for good on accept', async () => {
    serve({ 'DELETE /api/v1/calendars/errands': () => undefined });
    const { wrapper, store } = mountList();

    await button(wrapper, 'Delete task list').trigger('click');
    const options = lastConfirm();
    expect(options).toMatchObject({
      header: 'Delete task list',
      message: 'Delete "Errands" and all of its tasks? Everyone it is shared with loses it too, and task apps that sync it remove it. This can\'t be undone.',
      acceptLabel: 'Delete task list',
      rejectLabel: 'Cancel',
      acceptClass: 'p-button-danger',
    });
    expect(requests()).toEqual([]);

    await options.accept();
    await flushPromises();

    expect(requests()).toEqual([{ key: 'DELETE /api/v1/calendars/errands', body: { confirmation: 'DELETE' } }]);
    expect(store.calendars.map((c) => c.uuid)).toEqual(['work']);
    expect(toastMock.success).toHaveBeenCalledWith('Task list deleted');
    expect(wrapper.emitted('deleted')).toEqual([[ERRANDS]]);
    expect(wrapper.emitted('update:visible')).toEqual([[false]]);
  });

  it('a failed delete keeps the list and says why', async () => {
    serve({ 'DELETE /api/v1/calendars/errands': () => { throw Object.assign(new Error('x'), { statusCode: 404, data: { message: 'Calendar not found' } }); } });
    const { wrapper, store } = mountList();

    await button(wrapper, 'Delete task list').trigger('click');
    await lastConfirm().accept();
    await flushPromises();

    expect(toastMock.error).toHaveBeenCalledWith('Calendar not found', "Couldn't delete the task list");
    expect(store.calendars.map((c) => c.uuid)).toEqual(['work', 'errands']);
    expect(wrapper.emitted('deleted')).toBeUndefined();
  });

  it('confirmDelete(list) asks about and deletes the list it is given, also while closed', async () => {
    const other = calendar('home', { name: 'Home', kind: 'tasks' });
    serve({ 'DELETE /api/v1/calendars/home': () => undefined });
    const { wrapper } = mountList({ visible: false, calendar: null });

    (wrapper.vm as unknown as { confirmDelete: (c: Calendar) => void }).confirmDelete(other);
    expect(lastConfirm().message).toContain('"Home"');
    await lastConfirm().accept();
    await flushPromises();

    expect(requests()).toEqual([{ key: 'DELETE /api/v1/calendars/home', body: { confirmation: 'DELETE' } }]);
    expect(wrapper.emitted('deleted')).toEqual([[other]]);
  });
});
