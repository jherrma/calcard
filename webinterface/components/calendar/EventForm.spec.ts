// @vitest-environment nuxt
import { describe, it, expect, beforeEach } from 'vitest';
import { mount } from '@vue/test-utils';
import { nextTick } from 'vue';
import { createTestingPinia } from '@pinia/testing';
import type { TestingPinia } from '@pinia/testing';
import EventForm from './EventForm.vue';
import { useCalendarStore } from '~/stores/calendars';
import type { CalendarEvent, EventFormData, RecurrenceRule } from '~/types/calendar';

// EventForm is a heavy PrimeVue/Vuelidate-style component. Rather than driving the
// real inputs (brittle), we shallow-mount so all child inputs are stubbed, then
// exercise the extractable recurrence-payload logic in handleSubmit by setting the
// component's own reactive state and inspecting the emitted `submit` payload.

function baseEvent(overrides: Partial<CalendarEvent> = {}): CalendarEvent {
  return {
    id: 'ev-1',
    calendar_id: 1,
    uid: 'uid-1',
    summary: 'Team sync',
    start: '2026-02-09T11:00:00+01:00',
    end: '2026-02-09T12:00:00+01:00',
    all_day: false,
    is_recurring: false,
    ...overrides,
  } as CalendarEvent;
}

interface FormVm {
  enableRecurrence: boolean;
  recurrenceEnd: 'never' | 'count' | 'until';
  recurrence: { frequency: string; interval: number; by_day: string[]; count: number };
  recurrenceUntilDate: Date | null;
  handleSubmit: () => void;
}

let pinia: TestingPinia;

async function mountForm(event: CalendarEvent) {
  const store = useCalendarStore();
  // A writable calendar so calendar helpers resolve without errors.
  store.calendars = [
    { id: 1, uuid: 'u1', path: '/c/1', name: 'Work', color: '#3b82f6', owner_id: 'o1', shared: false, created_at: '', updated_at: '' } as never,
  ];
  const wrapper = mount(EventForm, {
    props: { event },
    shallow: true,
    global: { plugins: [pinia] },
  });
  return { wrapper, vm: wrapper.vm as unknown as FormVm };
}

function submittedRecurrence(wrapper: ReturnType<typeof mount>): RecurrenceRule | undefined {
  const events = wrapper.emitted('submit');
  expect(events).toBeTruthy();
  const payload = events![0]![0] as EventFormData;
  return payload.recurrence;
}

beforeEach(() => {
  pinia = createTestingPinia({ stubActions: false });
});

describe('EventForm recurrence payload', () => {
  it('emits count and NOT until when the end condition is "count"', async () => {
    const { wrapper, vm } = await mountForm(baseEvent());
    vm.enableRecurrence = true;
    vm.recurrence.frequency = 'WEEKLY';
    vm.recurrenceEnd = 'count';
    vm.recurrence.count = 5;
    await nextTick();

    vm.handleSubmit();

    const rec = submittedRecurrence(wrapper);
    expect(rec).toMatchObject({ frequency: 'WEEKLY', interval: 1, count: 5 });
    expect(rec).not.toHaveProperty('until'); // mutual exclusivity
  });

  it('emits until and NOT count when the end condition is "until"', async () => {
    const { wrapper, vm } = await mountForm(baseEvent());
    vm.enableRecurrence = true;
    vm.recurrenceEnd = 'until';
    vm.recurrenceUntilDate = new Date(2026, 11, 31, 0, 0, 0); // Dec 31 2026 local
    await nextTick();

    vm.handleSubmit();

    const rec = submittedRecurrence(wrapper);
    expect(rec).toHaveProperty('until');
    expect(rec).not.toHaveProperty('count'); // mutual exclusivity
    // until is serialized offset-preserving (RFC3339), never a bare UTC 'Z'.
    expect(rec!.until).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}[+-]\d{2}:\d{2}$/);
  });

  it('serializes the all-day until boundary as local midnight with offset (not Z)', async () => {
    const { wrapper, vm } = await mountForm(baseEvent({ all_day: true }));
    vm.enableRecurrence = true;
    vm.recurrenceEnd = 'until';
    vm.recurrenceUntilDate = new Date(2026, 11, 31, 0, 0, 0);
    await nextTick();

    vm.handleSubmit();

    const rec = submittedRecurrence(wrapper);
    // Local-midnight boundary preserved verbatim; offset suffix, no 'Z'.
    expect(rec!.until!.startsWith('2026-12-31T00:00:00')).toBe(true);
    expect(rec!.until!.endsWith('Z')).toBe(false);
  });

  it('omits recurrence entirely when repeat is disabled', async () => {
    const { wrapper, vm } = await mountForm(baseEvent());
    vm.enableRecurrence = false;
    await nextTick();

    vm.handleSubmit();

    expect(submittedRecurrence(wrapper)).toBeUndefined();
  });
});
