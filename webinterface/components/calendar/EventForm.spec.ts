// @vitest-environment nuxt
import { describe, it, expect, beforeEach } from 'vitest';
import { mount } from '@vue/test-utils';
import { nextTick } from 'vue';
import { createTestingPinia } from '@pinia/testing';
import type { TestingPinia } from '@pinia/testing';
import EventForm from './EventForm.vue';
import { useCalendarStore } from '~/stores/calendars';
import {
  usePreferencesStore,
  PREF_DEFAULT_ALL_DAY,
  PREF_DEFAULT_EVENT_DURATION,
  PREF_TIME_FORMAT,
} from '~/stores/preferences';
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
  form: { summary: string; all_day: boolean; start: Date; end: Date; calendar_id: string };
  hourFormat: string;
  handleSubmit: () => void;
}

let pinia: TestingPinia;

// Seed the writable calendar every mount needs, plus the preference values the
// form reads at setup time (story 103).
function seedStores(prefs: Record<string, string> = {}) {
  const store = useCalendarStore();
  store.calendars = [
    { id: 1, uuid: 'u1', path: '/c/1', name: 'Work', color: '#3b82f6', owner_id: 'o1', shared: false, created_at: '', updated_at: '' } as never,
  ];
  const preferences = usePreferencesStore();
  preferences.preferences = { ...preferences.preferences, ...prefs };
  return { store, preferences };
}

async function mountForm(event: CalendarEvent) {
  seedStores();
  const wrapper = mount(EventForm, {
    props: { event },
    shallow: true,
    global: { plugins: [pinia] },
  });
  return { wrapper, vm: wrapper.vm as unknown as FormVm };
}

// Create mode: no `event` prop, so the form falls back to the configured defaults.
async function mountCreateForm(
  prefs: Record<string, string> = {},
  props: Record<string, unknown> = {},
) {
  seedStores(prefs);
  const wrapper = mount(EventForm, {
    props,
    shallow: true,
    global: { plugins: [pinia] },
  });
  return { wrapper, vm: wrapper.vm as unknown as FormVm };
}

const MINUTE_MS = 60 * 1000;

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

// SCOPE: new-event defaults must come from the user's preferences instead of the
// old hardcoded 1 hour / not-all-day / 24h (story 103). REVERT PROOF: restore the
// HOUR_MS-based defaults and every duration assertion here fails.
describe('EventForm new-event defaults from preferences (story 103)', () => {
  it('sizes a new event with the configured duration, not a hardcoded hour', async () => {
    const { vm } = await mountCreateForm({ [PREF_DEFAULT_EVENT_DURATION]: '30' });

    expect(vm.form.end.getTime() - vm.form.start.getTime()).toBe(30 * MINUTE_MS);
  });

  it('falls back to 60 minutes when the stored duration is not an allowed value', async () => {
    const { vm } = await mountCreateForm({ [PREF_DEFAULT_EVENT_DURATION]: '37' });

    expect(vm.form.end.getTime() - vm.form.start.getTime()).toBe(60 * MINUTE_MS);
  });

  it('starts a new event as all-day when the preference is on', async () => {
    const { vm } = await mountCreateForm({ [PREF_DEFAULT_ALL_DAY]: 'true' });

    expect(vm.form.all_day).toBe(true);
  });

  // EventCreateDialog always binds :initial-all-day, passing undefined when the
  // user opened the dialog from the toolbar rather than by dragging the grid.
  // Without the explicit `initialAllDay: undefined` default in EventForm, Vue's
  // Boolean-prop casting turns that into `false` and the preference is ignored.
  it('still honours the preference when the parent binds initialAllDay as undefined', async () => {
    const { vm } = await mountCreateForm(
      { [PREF_DEFAULT_ALL_DAY]: 'true' },
      { initialAllDay: undefined },
    );

    expect(vm.form.all_day).toBe(true);
  });

  it('lets an explicit grid selection override the all-day preference', async () => {
    // Dragging a time range passes initialAllDay: false — an explicit intent that
    // must beat the stored default.
    const { vm } = await mountCreateForm(
      { [PREF_DEFAULT_ALL_DAY]: 'true' },
      { initialAllDay: false },
    );

    expect(vm.form.all_day).toBe(false);
  });

  it('keeps the edited event\'s own all-day flag regardless of the preference', async () => {
    seedStores({ [PREF_DEFAULT_ALL_DAY]: 'true' });
    const wrapper = mount(EventForm, {
      props: { event: baseEvent({ all_day: false }) },
      shallow: true,
      global: { plugins: [pinia] },
    });

    expect((wrapper.vm as unknown as FormVm).form.all_day).toBe(false);
  });

  it('re-derives the end from the configured duration when the start changes', async () => {
    const { vm } = await mountCreateForm({ [PREF_DEFAULT_EVENT_DURATION]: '120' });

    const newStart = new Date(2026, 4, 4, 9, 0, 0, 0);
    vm.form.start = newStart;
    await nextTick();

    expect(vm.form.end.getTime() - newStart.getTime()).toBe(120 * MINUTE_MS);
  });

  it('applies the configured duration when all-day is switched back off', async () => {
    const { vm } = await mountCreateForm({
      [PREF_DEFAULT_ALL_DAY]: 'true',
      [PREF_DEFAULT_EVENT_DURATION]: '45',
    });
    expect(vm.form.all_day).toBe(true);

    vm.form.all_day = false;
    await nextTick();

    expect(vm.form.end.getTime() - vm.form.start.getTime()).toBe(45 * MINUTE_MS);
  });

  it.each([
    ['24h', '24'],
    ['12h', '12'],
  ])('maps the %s preference to DatePicker hour-format "%s"', async (pref, expected) => {
    const { vm } = await mountCreateForm({ [PREF_TIME_FORMAT]: pref });

    expect(vm.hourFormat).toBe(expected);
  });
});
