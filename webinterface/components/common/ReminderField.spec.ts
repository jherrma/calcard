// @vitest-environment nuxt
import { describe, it, expect } from 'vitest';
import { mount } from '@vue/test-utils';
import { nextTick } from 'vue';
import PrimeVue from 'primevue/config';
import ReminderField from './ReminderField.vue';
import type { Reminder } from '~/types/calendar';
import { offsetReminder } from '~/utils/reminders';

// SCOPE (#169 story 17, spec §10.9, §10.10): the chips, the add menu, custom values, the cap of 5, merged
// duplicates, read-only chips and the readOnly prop. The Select is driven through the component's own handler;
// PrimeVue's overlay is not under test.

const foreign = (over: Partial<Reminder> = {}): Reminder => ({
  index: 7, action: 'email', related: 'start', offset_seconds: -86400, at: null, editable: false, ...over,
});

interface FieldVm {
  onPick: (v: number | string | null) => void;
  customOpen: boolean;
  customAmount: number;
  customUnit: string;
  customDays: number;
  customHour: number;
  customMinute: number;
  addCustom: () => void;
  presetOptions: { label: string; value: number | string }[];
  atCap: boolean;
}

function mountField(modelValue: Reminder[], props: Record<string, unknown> = {}) {
  const wrapper = mount(ReminderField, { props: { modelValue, ...props }, global: { plugins: [PrimeVue] } });
  const vm = wrapper.vm as unknown as FieldVm;
  const last = () => {
    const ev = wrapper.emitted('update:modelValue');
    return ev ? (ev[ev.length - 1]![0] as Reminder[]) : null;
  };
  return { wrapper, vm, last };
}

describe('ReminderField', () => {
  it('offers the timed presets plus Custom', () => {
    const { vm } = mountField([]);
    expect(vm.presetOptions.map((o) => o.label)).toEqual([
      'At start', '5 minutes before', '10 minutes before', '15 minutes before', '30 minutes before',
      '1 hour before', '2 hours before', '1 day before', '2 days before', '1 week before', 'Custom…',
    ]);
  });

  it('offers the all-day presets for an all-day event', () => {
    const { vm } = mountField([], { allDay: true });
    expect(vm.presetOptions.map((o) => [o.label, o.value])).toEqual([
      ['On the day at 09:00', 32400], ['1 day before at 09:00', -54000], ['2 days before at 09:00', -140400],
      ['1 week before at 09:00', -572400], ['Custom…', 'custom'],
    ]);
  });

  it('adding a preset emits a relative reminder', () => {
    const { vm, last } = mountField([]);
    vm.onPick(-900);
    expect(last()).toEqual([expect.objectContaining({ offset_seconds: -900, at: null, editable: true })]);
  });

  it('custom timed: 3 hours before is -10800', () => {
    const { vm, last } = mountField([]);
    vm.onPick('custom');
    expect(vm.customOpen).toBe(true);
    vm.customAmount = 3;
    vm.customUnit = 'hours';
    vm.addCustom();
    expect(last()!.map((r) => r.offset_seconds)).toEqual([-10800]);
    expect(vm.customOpen).toBe(false);
  });

  it('custom all-day: 2 days before at 07:30 is -145800', () => {
    const { vm, last } = mountField([], { allDay: true });
    vm.onPick('custom');
    vm.customDays = 2;
    vm.customHour = 7;
    vm.customMinute = 30;
    vm.addCustom();
    expect(last()!.map((r) => r.offset_seconds)).toEqual([-145800]);
  });

  it('merges a duplicate and keeps the list ordered by trigger', () => {
    const { vm, last } = mountField([offsetReminder(-900), offsetReminder(0)]);
    vm.onPick(-900);
    expect(last()!.map((r) => r.offset_seconds)).toEqual([-900, 0]);
    vm.onPick(-3600);
    expect(last()!.map((r) => r.offset_seconds)).toEqual([-3600, -900, 0]);
  });

  it('removes a chip; the remove button names its reminder', async () => {
    const { wrapper, last } = mountField([offsetReminder(-900), offsetReminder(0)]);
    const buttons = wrapper.findAll('[data-test="reminder-remove"]');
    expect(buttons.map((b) => b.attributes('aria-label'))).toEqual([
      'Remove reminder: At start',
      'Remove reminder: 15 minutes before',
    ].reverse());
    await buttons[0]!.trigger('click');
    expect(last()!.map((r) => r.offset_seconds)).toEqual([0]);
  });

  it('the chips form a list labelled "Reminders"', () => {
    const { wrapper } = mountField([offsetReminder(-900)]);
    const list = wrapper.find('ul');
    expect(list.attributes('aria-label')).toBe('Reminders');
    expect(list.findAll('li')).toHaveLength(1);
  });

  it('stops at 5 editable reminders (read-only ones do not count)', async () => {
    const five = [-300, -600, -900, -1800, -3600].map(offsetReminder);
    const { wrapper, vm, last } = mountField([...five, foreign()]);
    expect(vm.atCap).toBe(true);
    expect(wrapper.find('[data-test="reminder-add"]').exists()).toBe(false);
    expect(wrapper.find('[data-test="reminder-cap"]').exists()).toBe(true);
    vm.onPick(-7200);
    expect(last()).toBeNull();

    const four = mountField([...five.slice(0, 4), foreign(), foreign({ index: 8, action: 'audio', related: 'end' })]);
    expect(four.vm.atCap).toBe(false);
    expect(four.wrapper.find('[data-test="reminder-add"]').exists()).toBe(true);
  });

  it('shows a read-only reminder as "Set in another app" without a remove button, and never drops it', () => {
    const ro = foreign();
    const { wrapper, vm, last } = mountField([ro]);
    const chip = wrapper.find('[data-test="reminder-chip-readonly"]');
    expect(chip.text()).toContain('E-mail, 1 day before');
    expect(chip.text()).toContain('Set in another app');
    expect(chip.find('button').exists()).toBe(false);
    vm.onPick(-900);
    expect(last()).toContainEqual(ro);
  });

  it('an absolute reminder is a removable chip', async () => {
    const at = new Date(2026, 10, 4, 9, 0, 0).toISOString();
    const abs: Reminder = { index: 1, action: 'display', related: null, offset_seconds: null, at, editable: true };
    const { wrapper, last } = mountField([abs]);
    expect(wrapper.find('[data-test="reminder-chip"]').text()).toContain('At 4 Nov, 09:00');
    await wrapper.find('[data-test="reminder-remove"]').trigger('click');
    expect(last()).toEqual([]);
  });

  it('readOnly renders chips without any control', async () => {
    const { wrapper } = mountField([offsetReminder(-900), foreign()], { readOnly: true });
    await nextTick();
    expect(wrapper.findAll('[data-test="reminder-chip"]')).toHaveLength(1);
    expect(wrapper.find('button').exists()).toBe(false);
    expect(wrapper.find('[data-test="reminder-add"]').exists()).toBe(false);
  });

  it('readOnly with nothing says so', () => {
    const { wrapper } = mountField([], { readOnly: true });
    expect(wrapper.find('[data-test="reminder-none"]').exists()).toBe(true);
  });
});
