// @vitest-environment nuxt
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils';
import { createTestingPinia } from '@pinia/testing';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import { defineComponent, h } from 'vue';
import AddCalendarDialog from './AddCalendarDialog.vue';
import { useCalendarStore } from '~/stores/calendars';

// SCOPE (#170 story 27, spec §1b): the Calendar section's "Create Calendar"
// is NOT changed by task lists. It still sends no `kind` (the server makes an
// event calendar), still has the Timezone field, and still says
// "Calendar created". Task lists are created from the Tasks section.

const { apiMock, toastMock } = vi.hoisted(() => ({
  apiMock: vi.fn(),
  toastMock: { success: vi.fn(), error: vi.fn(), warn: vi.fn(), info: vi.fn(), action: vi.fn() },
}));
mockNuxtImport('useApi', () => () => apiMock);
mockNuxtImport('useAppToast', () => () => toastMock);

enableAutoUnmount(afterEach);

const DialogStub = defineComponent({
  name: 'Dialog',
  props: { visible: Boolean, header: { type: String, default: '' } },
  setup(props, { slots }) {
    return () => (props.visible ? h('div', [h('h2', props.header), slots.default?.(), slots.footer?.()]) : null);
  },
});

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
  props: { modelValue: { type: String, default: '' } },
  emits: ['update:modelValue'],
  setup(props, { emit }) {
    return () => h('input', { value: props.modelValue, onInput: (e: Event) => emit('update:modelValue', (e.target as HTMLInputElement).value) });
  },
});

function mountDialog() {
  const pinia = createTestingPinia({ stubActions: false });
  const wrapper = mount(AddCalendarDialog, {
    props: { visible: true },
    shallow: true,
    global: { plugins: [pinia], stubs: { Dialog: DialogStub, Button: ButtonStub, InputText: InputStub } },
  });
  return { wrapper, store: useCalendarStore() };
}

beforeEach(() => {
  apiMock.mockReset();
  Object.values(toastMock).forEach((fn) => fn.mockReset());
});

describe('AddCalendarDialog (unchanged by task lists)', () => {
  it('is "Create Calendar" with Name, Color, Timezone and Description', () => {
    const { wrapper } = mountDialog();

    expect(wrapper.get('h2').text()).toBe('Create Calendar');
    expect(wrapper.findAll('label').map((l) => l.text())).toEqual(['Name *', 'Color', 'Timezone', 'Description']);
    expect(wrapper.findAll('button').map((b) => b.text())).toEqual(['Cancel', 'Create']);
  });

  it('creates an event calendar: no `kind` in the body, all four fields as before', async () => {
    apiMock.mockImplementation(async (_url: string, options: { body: Record<string, unknown> }) => ({
      id: '9', uuid: 'new', path: 'new', kind: 'events', created_at: '', updated_at: '', ...options.body,
    }));
    const { wrapper, store } = mountDialog();
    await wrapper.findAll('input')[0]!.setValue('Work');

    await wrapper.findAll('button').find((b) => b.text() === 'Create')!.trigger('click');
    await flushPromises();

    expect(apiMock).toHaveBeenCalledTimes(1);
    const [url, options] = apiMock.mock.calls[0]!;
    expect(url).toBe('/api/v1/calendars');
    expect(options.method).toBe('POST');
    expect(Object.keys(options.body).sort()).toEqual(['color', 'description', 'name', 'timezone']);
    expect(options.body).toMatchObject({ name: 'Work', color: '#3788d8', description: '' });
    expect(options.body).not.toHaveProperty('kind');
    expect(store.calendars.map((c) => c.uuid)).toEqual(['new']);
    expect(toastMock.success).toHaveBeenCalledWith('Calendar created');
    expect(wrapper.emitted('created')).toEqual([[]]);
    expect(wrapper.emitted('update:visible')).toEqual([[false]]);
  });
});
