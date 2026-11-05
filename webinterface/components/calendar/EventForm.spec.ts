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
  PREF_FIRST_DAY_OF_WEEK,
  PREF_TIME_FORMAT,
} from '~/stores/preferences';
import type { CalendarEvent, EventFormData, RecurrenceRule, Reminder } from '~/types/calendar';
import { offsetReminder } from '~/utils/reminders';

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
  weekDays: { label: string; value: string }[];
  handleSubmit: () => void;
}

let pinia: TestingPinia;

// Seed the writable calendar every mount needs, plus the preference values the
// form reads at setup time (story 103).
function seedStores(prefs: Record<string, string> = {}) {
  const store = useCalendarStore();
  store.calendars = [
    { id: 1, uuid: 'u1', path: '/c/1', name: 'Work', color: '#3b82f6', shared: false, created_at: '', updated_at: '' } as never,
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

// SCOPE: the weekly-recurrence day chips follow the first day of the week, like
// the calendar grid, instead of a hardcoded order. Display only — what gets
// submitted is still the set of picked BYDAY codes in the order they were picked.
describe('EventForm weekday chips follow first_day_of_week', () => {
  it.each([
    ['0', ['SU', 'MO', 'TU', 'WE', 'TH', 'FR', 'SA']],
    ['1', ['MO', 'TU', 'WE', 'TH', 'FR', 'SA', 'SU']],
    ['6', ['SA', 'SU', 'MO', 'TU', 'WE', 'TH', 'FR']],
  ])('lays the chips out from weekday %s', async (pref, expected) => {
    const { vm } = await mountCreateForm({ [PREF_FIRST_DAY_OF_WEEK]: pref });

    expect(vm.weekDays.map((d) => d.value)).toEqual(expected);
  });

  it('keeps the picked BYDAY order regardless of the chip order', async () => {
    const { wrapper, vm } = await mountForm(baseEvent());
    const preferences = usePreferencesStore();
    preferences.preferences = { ...preferences.preferences, [PREF_FIRST_DAY_OF_WEEK]: '6' };
    vm.enableRecurrence = true;
    vm.recurrence.frequency = 'WEEKLY';
    vm.recurrence.by_day = ['MO', 'SA'];
    await nextTick();

    vm.handleSubmit();

    expect(submittedRecurrence(wrapper)?.by_day).toEqual(['MO', 'SA']);
  });
});

// SCOPE (#170): task lists (`kind: 'tasks'`) arrive in the calendar store next
// to the calendars. The calendar select reads writableCalendars, which is built
// on eventCalendars, so it never offers a task list — and the default target of
// a new event is never one (the server would answer 400 "This calendar does not
// store events").
describe('EventForm calendar select never offers a task list', () => {
  function mountWithCalendars(calendars: Array<Record<string, unknown>>) {
    const store = useCalendarStore();
    store.calendars = calendars.map((c) => ({
      path: `/c/${c.id}`,
      color: '#3b82f6',
      created_at: '',
      updated_at: '',
      ...c,
    })) as never;
    const wrapper = mount(EventForm, { props: {}, shallow: true, global: { plugins: [pinia] } });
    return wrapper.vm as unknown as FormVm & { calendarSelectOptions: { id: string; name: string }[] };
  }

  it('lists event and mixed calendars only and defaults to the first of them', () => {
    const vm = mountWithCalendars([
      { id: 1, uuid: 'u1', name: 'Tasks', kind: 'tasks' }, // first in the list
      { id: 2, uuid: 'u2', name: 'Personal', kind: 'events' },
      { id: 3, uuid: 'u3', name: 'Team tasks', kind: 'tasks', shared: true, permission: 'read-write' },
      { id: 4, uuid: 'u4', name: 'Old mixed', kind: 'both' },
    ]);

    // REVERT PROOF: without the kind filter "Tasks" is listed AND preselected.
    expect(vm.calendarSelectOptions.map((c) => c.name)).toEqual(['Personal', 'Old mixed']);
    expect(vm.form.calendar_id).toBe('2');
  });

  it('offers nothing (and preselects nothing) when the user only has task lists', () => {
    const vm = mountWithCalendars([
      { id: 1, uuid: 'u1', name: 'Tasks', kind: 'tasks' },
    ]);

    expect(vm.calendarSelectOptions).toEqual([]);
    expect(vm.form.calendar_id).toBe('');
  });
});

// ---- Reminders (#169 story 17, spec §10.9) ---------------------------------------------------

describe('EventForm reminders', () => {
  type ReminderVm = FormVm & { reminders: Reminder[] };
  const stored = (over: Partial<Reminder>): Reminder => ({
    index: 0, action: 'display', related: 'start', offset_seconds: -900, at: null, editable: true, ...over,
  });
  const submitted = (wrapper: ReturnType<typeof mount>) => wrapper.emitted('submit')![0]![0] as EventFormData;

  it('puts the block between Description and Repeat', async () => {
    const { wrapper } = await mountCreateForm();
    const html = wrapper.html();
    const desc = html.indexOf('event-description');
    const block = html.indexOf('reminders-block');
    const repeat = html.indexOf('event-recurrence');
    expect(desc).toBeGreaterThan(-1);
    expect(block).toBeGreaterThan(desc);
    expect(repeat).toBeGreaterThan(block);
  });

  it('a new event starts with no reminder and sends no `reminders` key', async () => {
    const { wrapper, vm } = await mountCreateForm();
    expect((vm as ReminderVm).reminders).toEqual([]);
    vm.form.summary = 'x';
    vm.handleSubmit();
    expect(submitted(wrapper)).not.toHaveProperty('reminders');
  });

  it('shows the event\'s own reminders as the initial value', async () => {
    const { wrapper, vm } = await mountForm(baseEvent({ reminders: [stored({ offset_seconds: -900 })] }));
    expect(wrapper.find('[data-test="reminders-block"]').html()).toContain('reminder-field');
    expect((vm as ReminderVm).reminders).toEqual([stored({ offset_seconds: -900 })]);
  });

  it('editing only the title sends no `reminders`, even with foreign ones present', async () => {
    const { wrapper, vm } = await mountForm(baseEvent({
      reminders: [stored({}), stored({ index: 1, action: 'email', editable: false, offset_seconds: -60 })],
    }));
    vm.form.summary = 'Renamed';
    vm.handleSubmit();
    expect(submitted(wrapper)).not.toHaveProperty('reminders');
  });

  it('a date-only change sends no `reminders`', async () => {
    const { wrapper, vm } = await mountForm(baseEvent({ reminders: [stored({})] }));
    vm.form.start = new Date(2026, 1, 12, 11, 0);
    vm.form.end = new Date(2026, 1, 12, 12, 0);
    vm.handleSubmit();
    expect(submitted(wrapper)).not.toHaveProperty('reminders');
  });

  it('removing one chip sends the remaining editable list and not the read-only ones', async () => {
    const at = '2026-11-04T08:00:00Z';
    const { wrapper, vm } = await mountForm(baseEvent({
      reminders: [
        stored({ offset_seconds: -900 }),
        stored({ index: 1, offset_seconds: -3600 }),
        stored({ index: 2, offset_seconds: null, at }),
        stored({ index: 3, action: 'email', editable: false, offset_seconds: -60 }),
      ],
    }));
    const r = (vm as ReminderVm);
    r.reminders = r.reminders.filter((x) => x.offset_seconds !== -3600);
    vm.handleSubmit();
    // The absolute one goes back as the `at` the server returned.
    expect(submitted(wrapper).reminders).toEqual([{ offset_seconds: -900 }, { at }]);
  });

  it('adding a reminder to a new event sends it', async () => {
    const { wrapper, vm } = await mountCreateForm();
    (vm as ReminderVm).reminders = [offsetReminder(-900)];
    vm.form.summary = 'x';
    vm.handleSubmit();
    expect(submitted(wrapper).reminders).toEqual([{ offset_seconds: -900 }]);
  });

  it('switching all-day <-> timed maps the editable ones and sends the result', async () => {
    const { wrapper, vm } = await mountForm(baseEvent({
      all_day: true,
      reminders: [stored({ offset_seconds: -54000 }), stored({ index: 1, offset_seconds: 32400 })],
    }));
    vm.form.all_day = false;
    await nextTick();
    expect((vm as ReminderVm).reminders.map((r) => r.offset_seconds)).toEqual([-86400, 0]);
    vm.form.all_day = true;
    await nextTick();
    expect((vm as ReminderVm).reminders.map((r) => r.offset_seconds)).toEqual([-54000, 32400]);
    // Back to where it started: nothing changed, nothing sent.
    vm.handleSubmit();
    expect(submitted(wrapper)).not.toHaveProperty('reminders');
  });

  it('is absent for a calendar shared with the user (read and read-write) and present for an owned one', async () => {
    const { wrapper, vm } = await mountCreateForm();
    const store = useCalendarStore();
    store.calendars = [
      { id: 1, uuid: 'u1', path: '/c/1', name: 'Work', color: '#3b82f6', shared: false, created_at: '', updated_at: '' },
      { id: 2, uuid: 'u2', path: '/c/2', name: 'Team', color: '#3b82f6', shared: true, permission: 'read-write', created_at: '', updated_at: '' },
      { id: 3, uuid: 'u3', path: '/c/3', name: 'Look', color: '#3b82f6', shared: true, permission: 'read', created_at: '', updated_at: '' },
    ] as never;
    vm.form.calendar_id = '1';
    await nextTick();
    expect(wrapper.find('[data-test="reminders-block"]').exists()).toBe(true);
    vm.form.calendar_id = '2';
    await nextTick();
    expect(wrapper.find('[data-test="reminders-block"]').exists()).toBe(false);
    vm.form.calendar_id = '3';
    await nextTick();
    expect(wrapper.find('[data-test="reminders-block"]').exists()).toBe(false);
  });

  it('a shared calendar\'s form never sends `reminders`', async () => {
    const { wrapper, vm } = await mountForm(baseEvent({ calendar_id: 2, reminders: [stored({})] }));
    useCalendarStore().calendars = [
      { id: 2, uuid: 'u2', path: '/c/2', name: 'Team', color: '#3b82f6', shared: true, permission: 'read-write', created_at: '', updated_at: '' },
    ] as never;
    await nextTick();
    (vm as ReminderVm).reminders = [];
    vm.handleSubmit();
    expect(submitted(wrapper)).not.toHaveProperty('reminders');
  });
});
