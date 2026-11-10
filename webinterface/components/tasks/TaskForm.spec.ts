// @vitest-environment nuxt
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import { enableAutoUnmount, mount } from '@vue/test-utils';
import type { VueWrapper } from '@vue/test-utils';
import { defineComponent, h, nextTick } from 'vue';
import { createTestingPinia } from '@pinia/testing';
import type { TestingPinia } from '@pinia/testing';
import TaskForm from './TaskForm.vue';
import { useCalendarStore } from '~/stores/calendars';
import { usePreferencesStore, PREF_TIME_FORMAT } from '~/stores/preferences';
import type { Calendar, Reminder } from '~/types/calendar';
import type { Task, TaskFormData } from '~/types/tasks';
import { buildTaskPatch, taskToFormData } from '~/utils/taskForm';

// SCOPE (#170 story 26, spec §7.3/§7.6, pattern EventForm.spec.ts): the task
// form. Title required; due before start refused; the all-day switch turns
// the due (and start) control between date and date-time; the list select
// offers only writable task lists and is create-only (disabled when
// editing); a repeating task shows its rule as read-only text, has status and
// progress disabled and a note when its due changes; a read-only list opens
// without Save. Shallow mount: the PrimeVue inputs are stubs whose bound
// values are read from `vm.$attrs` (keyed as written in the template), and the
// form's own state is driven through the exposed `form`.

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

/** Local wall-clock time; `month` is 1-based. Built inside tests, after the zone is set. */
function local(year: number, month: number, day: number, hour = 0, minute = 0): Date {
  return new Date(year, month - 1, day, hour, minute);
}

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
  calendar('personal', { name: 'Personal', kind: 'events' }),
  calendar('team', { name: 'Team tasks', color: '#0000ff', shared: true, permission: 'read-only' }),
  calendar('feed', { name: 'Holidays feed', kind: 'both', subscribed: true }),
  calendar('errands', { name: 'Errands', color: '#ff0000' }),
  calendar('shared-rw', { name: 'Family', shared: true, permission: 'read-write' }),
  calendar('home', { name: 'Home', color: '#00aa00', kind: 'both' }),
];

function task(overrides: Partial<Task> = {}): Task {
  return {
    id: 'task-1',
    calendar_id: 1,
    calendar_uuid: 'errands',
    uid: 'task-1@calcard.io',
    summary: 'Renew passport',
    description: 'Bring two photos, the old one; and C:\\forms',
    status: 'needs-action',
    completed: false,
    completed_at: null,
    percent_complete: 40,
    priority: 5,
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
    ...overrides,
  };
}

const RECURRING: Partial<Task> = { is_recurring: true, rrule: 'FREQ=WEEKLY;BYDAY=MO' };

interface FormVm {
  form: TaskFormData;
  initial: TaskFormData;
  handleSubmit: () => Promise<void>;
}

let pinia: TestingPinia;

beforeEach(() => {
  pinia = createTestingPinia({ stubActions: false });
  useCalendarStore().calendars = LISTS;
});

function mountForm(props: Record<string, unknown> = {}) {
  const wrapper = mount(TaskForm, { props, shallow: true, global: { plugins: [pinia] } });
  return { wrapper, vm: wrapper.vm as unknown as FormVm };
}

type Wrapper = ReturnType<typeof mountForm>['wrapper'];

/** The stubbed PrimeVue control with that data-test. */
const control = (w: Wrapper, test: string) => w.findComponent(`[data-test="${test}"]`) as VueWrapper;
/** A value bound on a stubbed control, keyed as written in the template. */
const bound = (w: Wrapper, test: string, name: string): unknown => control(w, test).vm.$attrs[name];
const has = (w: Wrapper, test: string) => w.find(`[data-test="${test}"]`).exists();

describe('TaskForm — fields', () => {
  it('has title, notes, list, all day, due, start, priority, progress (slider + number) and status', () => {
    const { wrapper } = mountForm({ defaultListId: 'errands' });

    for (const test of ['title', 'notes', 'list', 'all-day', 'due', 'start', 'priority', 'percent-slider', 'percent', 'status']) {
      expect(has(wrapper, test), test).toBe(true);
    }
    expect(wrapper.text()).toContain('Title');
    expect(wrapper.text()).toContain('Notes');
    expect(wrapper.text()).toContain('Progress');
    // Labels point at the controls.
    expect(wrapper.get(`label[for="${bound(wrapper, 'due', 'input-id')}"]`).text()).toBe('Due');
    expect(wrapper.get(`label[for="${bound(wrapper, 'all-day', 'input-id')}"]`).text()).toBe('All day');
    expect(wrapper.get(`label[for="${bound(wrapper, 'percent', 'input-id')}"]`).text()).toBe('Progress');
  });

  it('a new task: the default list, the quick-add title, all day, no dates, priority none, needs action', () => {
    const { vm } = mountForm({ defaultListId: 'home', initialSummary: 'Buy milk' });

    expect(vm.form).toEqual({
      summary: 'Buy milk',
      description: '',
      listId: 'home',
      allDay: true,
      due: null,
      start: null,
      priority: 'none',
      percent: null,
      status: 'needs-action',
      reminders: [],
    });
  });

  it('an existing task: its values, text exactly as stored', () => {
    const t = task();
    const { vm, wrapper } = mountForm({ task: t });

    expect(vm.form).toEqual(taskToFormData(t));
    expect(bound(wrapper, 'notes', 'modelValue')).toBe('Bring two photos, the old one; and C:\\forms');
    expect(bound(wrapper, 'title', 'modelValue')).toBe('Renew passport');
  });

  it('priority is a None / Low / Medium / High choice that cannot be emptied', () => {
    const { wrapper } = mountForm({ defaultListId: 'errands' });

    expect((bound(wrapper, 'priority', 'options') as { label: string; value: string }[]).map((o) => [o.label, o.value])).toEqual([
      ['None', 'none'], ['Low', 'low'], ['Medium', 'medium'], ['High', 'high'],
    ]);
    expect(bound(wrapper, 'priority', 'allow-empty')).toBe(false);
  });

  it('shows the priority marks from sm only, so the four choices fit a 360px phone (story 29)', () => {
    const { wrapper } = mountForm({ defaultListId: 'errands' });
    const slot = control(wrapper, 'priority').vm.$slots.option!;
    const options = bound(wrapper, 'priority', 'options') as { label: string; value: string; icon?: string }[];
    const render = (option: unknown) => mount(defineComponent({ render: () => h('div', slot({ option })) }));

    const high = render(options.find((o) => o.value === 'high'));
    const mark = high.get('[data-test="priority-icon"]');
    expect(mark.classes()).toEqual(['hidden', 'sm:inline-flex']);
    expect(mark.attributes('aria-hidden')).toBe('true');
    expect(mark.get('i').classes()).toEqual(expect.arrayContaining(['pi', 'pi-angle-double-up']));
    expect(high.text()).toBe('High');
    // None has no mark at all.
    expect(render(options.find((o) => o.value === 'none')).find('[data-test="priority-icon"]').exists()).toBe(false);
  });

  it('status offers the four statuses in the list\'s words', () => {
    const { wrapper } = mountForm({ defaultListId: 'errands' });

    expect((bound(wrapper, 'status', 'options') as { label: string; value: string }[]).map((o) => [o.label, o.value])).toEqual([
      ['Needs action', 'needs-action'], ['In progress', 'in-process'], ['Completed', 'completed'], ['Cancelled', 'cancelled'],
    ]);
  });

  it('progress: 0..100 on the slider and the number; "none" sits at 0 until moved', async () => {
    const { wrapper, vm } = mountForm({ defaultListId: 'errands' });

    expect(bound(wrapper, 'percent-slider', 'min')).toBe(0);
    expect(bound(wrapper, 'percent-slider', 'max')).toBe(100);
    expect(bound(wrapper, 'percent', 'max')).toBe(100);
    expect(bound(wrapper, 'percent-slider', 'modelValue')).toBe(0);
    expect(vm.form.percent).toBeNull();

    control(wrapper, 'percent-slider').vm.$emit('update:modelValue', 35);
    await nextTick();
    expect(vm.form.percent).toBe(35);
    expect(bound(wrapper, 'percent', 'modelValue')).toBe(35);
  });

  it('the date pickers follow the 12h/24h preference', () => {
    usePreferencesStore().preferences[PREF_TIME_FORMAT] = '12h';
    const { wrapper } = mountForm({ defaultListId: 'errands' });

    expect(bound(wrapper, 'due', 'hour-format')).toBe('12');
    expect(bound(wrapper, 'start', 'hour-format')).toBe('12');
  });
});

describe('TaskForm — validation', () => {
  it('requires a title: a blank one is refused with a message and nothing is emitted', async () => {
    const { wrapper, vm } = mountForm({ defaultListId: 'errands', initialSummary: '   ' });

    await vm.handleSubmit();
    await nextTick();

    expect(wrapper.emitted('submit')).toBeUndefined();
    const error = wrapper.get('[data-test="title-error"]');
    expect(error.text()).toBe('Enter a title for the task.');
    expect(bound(wrapper, 'title', 'invalid')).toBe(true);
    expect(bound(wrapper, 'title', 'aria-describedby')).toBe(error.attributes('id'));

    vm.form.summary = 'Buy milk';
    await nextTick();
    expect(has(wrapper, 'title-error')).toBe(false);
    await vm.handleSubmit();
    expect(wrapper.emitted('submit')).toHaveLength(1);
  });

  it('refuses a title over 500 characters (counted like the server)', async () => {
    const { wrapper, vm } = mountForm({ defaultListId: 'errands', initialSummary: 'x'.repeat(501) });

    await vm.handleSubmit();
    await nextTick();
    expect(wrapper.get('[data-test="title-error"]').text()).toBe('Shorten the title to 500 characters or fewer.');

    vm.form.summary = '🎉'.repeat(500); // 1000 UTF-16 units, 500 characters
    await vm.handleSubmit();
    expect(wrapper.emitted('submit')).toHaveLength(1);
  });

  it('refuses a due date before the start, and accepts it once fixed', async () => {
    const { wrapper, vm } = mountForm({ defaultListId: 'errands', initialSummary: 'Tax return' });
    vm.form.due = local(2026, 11, 4);
    vm.form.start = local(2026, 11, 5);

    await vm.handleSubmit();
    await nextTick();

    expect(wrapper.emitted('submit')).toBeUndefined();
    const error = wrapper.get('[data-test="due-error"]');
    expect(error.text()).toBe('The due date is before the start. Pick a later due date or an earlier start.');
    expect(bound(wrapper, 'due', 'invalid')).toBe(true);
    expect(bound(wrapper, 'due', 'aria-describedby')).toBe(error.attributes('id'));

    // Moving the START also clears it (the rule reads both).
    vm.form.start = local(2026, 11, 4);
    await nextTick();
    expect(has(wrapper, 'due-error')).toBe(false);
    await vm.handleSubmit();
    expect(wrapper.emitted('submit')).toHaveLength(1);
  });

  it('all day compares dates: due earlier in the day than the start is fine; timed it is not', async () => {
    const { wrapper, vm } = mountForm({ defaultListId: 'errands', initialSummary: 'Tax return' });
    vm.form.due = local(2026, 11, 5, 8);
    vm.form.start = local(2026, 11, 5, 17);

    await vm.handleSubmit();
    expect(wrapper.emitted('submit')).toHaveLength(1);

    vm.form.allDay = false;
    await nextTick();
    vm.form.due = local(2026, 11, 5, 8);
    vm.form.start = local(2026, 11, 5, 17);
    await vm.handleSubmit();
    expect(wrapper.emitted('submit')).toHaveLength(1);
  });

  it('requires a list for a new task', async () => {
    const { wrapper, vm } = mountForm({ defaultListId: null, initialSummary: 'Buy milk' });

    await vm.handleSubmit();
    await nextTick();

    expect(wrapper.emitted('submit')).toBeUndefined();
    expect(wrapper.get('[data-test="list-error"]').text()).toBe('Choose the list to add the task to.');
  });

  it('emits the values and the values it started from', async () => {
    const t = task();
    const { wrapper, vm } = mountForm({ task: t });
    vm.form.summary = 'Renew passport now';

    await vm.handleSubmit();

    const [data, initial] = wrapper.emitted('submit')![0] as [TaskFormData, TaskFormData];
    expect(data).toEqual({ ...taskToFormData(t), summary: 'Renew passport now' });
    expect(initial).toEqual(taskToFormData(t));
  });

  it('does not submit twice while a save runs', async () => {
    const { wrapper, vm } = mountForm({ task: task(), isSubmitting: true });

    await vm.handleSubmit();
    expect(wrapper.emitted('submit')).toBeUndefined();
    expect(bound(wrapper, 'save-task', 'loading')).toBe(true);
  });
});

describe('TaskForm — all day', () => {
  it('the switch turns the due and start controls between a date and a date and time', async () => {
    const { wrapper, vm } = mountForm({ defaultListId: 'errands' });

    expect(bound(wrapper, 'all-day', 'modelValue')).toBe(true);
    expect(bound(wrapper, 'due', 'show-time')).toBe(false);
    expect(bound(wrapper, 'start', 'show-time')).toBe(false);

    control(wrapper, 'all-day').vm.$emit('update:modelValue', false);
    await nextTick();
    expect(vm.form.allDay).toBe(false);
    expect(bound(wrapper, 'due', 'show-time')).toBe(true);
    expect(bound(wrapper, 'start', 'show-time')).toBe(true);
  });

  it('a timed task opens with date and time controls', () => {
    const { wrapper } = mountForm({ task: task() });

    expect(bound(wrapper, 'due', 'show-time')).toBe(true);
    expect(bound(wrapper, 'due', 'modelValue')).toEqual(new Date('2026-11-05T16:00:00Z'));
  });

  it('switching all day off puts a date at the current hour of that day, not at midnight', async () => {
    const { vm } = mountForm({ defaultListId: 'errands' });
    vm.form.due = local(2026, 11, 5);
    vm.form.start = local(2026, 11, 1);
    const hour = new Date().getHours();

    vm.form.allDay = false;
    await nextTick();

    expect(vm.form.due).toEqual(local(2026, 11, 5, hour));
    expect(vm.form.start).toEqual(local(2026, 11, 1, hour));
  });

  it('switching all day on keeps the dates', async () => {
    const t = task({ start: '2026-11-04T08:30:00Z' });
    const { vm } = mountForm({ task: t });

    vm.form.allDay = true;
    await nextTick();

    expect(vm.form.due).toEqual(new Date('2026-11-05T16:00:00Z'));
    expect(vm.form.start).toEqual(new Date('2026-11-04T08:30:00Z'));
  });
});

describe('TaskForm — the list', () => {
  it('offers only writable task lists (no events calendar, read-only share or subscription)', () => {
    const { wrapper } = mountForm({ defaultListId: 'errands' });

    expect((bound(wrapper, 'list', 'options') as Calendar[]).map((c) => c.uuid)).toEqual(['errands', 'shared-rw', 'home']);
    expect(bound(wrapper, 'list', 'modelValue')).toBe('errands');
  });

  it('is enabled when adding and disabled when editing (a task never moves between lists)', () => {
    expect(bound(mountForm({ defaultListId: 'errands' }).wrapper, 'list', 'disabled')).toBe(false);
    expect(bound(mountForm({ task: task() }).wrapper, 'list', 'disabled')).toBe(true);
  });

  it('names the label for the select', () => {
    const { wrapper } = mountForm({ defaultListId: 'errands' });
    const labelId = bound(wrapper, 'list', 'aria-labelledby') as string;

    expect(wrapper.get(`[id="${labelId}"]`).text()).toBe('List');
  });
});

describe('TaskForm — a repeating task', () => {
  it('shows the rule as read-only text, with no control to change it', () => {
    const { wrapper } = mountForm({ task: task(RECURRING) });

    expect(wrapper.get('[data-test="recurrence-text"]').text()).toBe('Weekly on Mon');
    expect(wrapper.get('[data-test="recurrence"]').findAll('input, select, textarea, button')).toHaveLength(0);
    expect(wrapper.get('[data-test="recurrence-hint"]').text()).toContain('Repeating tasks can be completed in your task app.');
  });

  it('disables status and progress (the server would refuse completing it)', () => {
    const { wrapper } = mountForm({ task: task(RECURRING) });

    expect(bound(wrapper, 'status', 'disabled')).toBe(true);
    expect(bound(wrapper, 'percent-slider', 'disabled')).toBe(true);
    expect(bound(wrapper, 'percent', 'disabled')).toBe(true);
    // Everything else stays editable.
    expect(bound(wrapper, 'due', 'disabled')).toBe(false);
    expect(bound(wrapper, 'priority', 'disabled')).toBe(false);
    expect(bound(wrapper, 'title', 'readonly')).toBe(false);
  });

  it('a one-off task has status and progress enabled and no rule', () => {
    const { wrapper } = mountForm({ task: task() });

    expect(bound(wrapper, 'status', 'disabled')).toBe(false);
    expect(bound(wrapper, 'percent-slider', 'disabled')).toBe(false);
    expect(bound(wrapper, 'percent', 'disabled')).toBe(false);
    expect(has(wrapper, 'recurrence')).toBe(false);
  });

  it('notes that a changed due moves the whole series; nothing while the due is as stored', async () => {
    const { wrapper, vm } = mountForm({ task: task(RECURRING) });
    expect(has(wrapper, 'recurring-due-note')).toBe(false);

    vm.form.due = local(2026, 11, 12, 17);
    await nextTick();
    expect(wrapper.get('[data-test="recurring-due-note"]').text()).toBe(
      'This task repeats. A new due date moves the whole series, not just the next one.',
    );

    vm.form.due = new Date('2026-11-05T16:00:00Z');
    await nextTick();
    expect(has(wrapper, 'recurring-due-note')).toBe(false);
  });

  it('a one-off task gets no note when its due changes', async () => {
    const { wrapper, vm } = mountForm({ task: task() });

    vm.form.due = local(2026, 11, 12, 17);
    await nextTick();
    expect(has(wrapper, 'recurring-due-note')).toBe(false);
  });

  it('a task repeating by dates alone says so', () => {
    const { wrapper } = mountForm({ task: task({ is_recurring: true, rrule: null }) });

    expect(wrapper.get('[data-test="recurrence-text"]').text()).toBe('On set dates');
  });
});

describe('TaskForm — actions and read only', () => {
  it('a new task: Cancel and "Save task", no Delete', async () => {
    const { wrapper } = mountForm({ defaultListId: 'errands' });

    expect(bound(wrapper, 'save-task', 'label')).toBe('Save task');
    expect(bound(wrapper, 'save-task', 'type')).toBe('submit');
    expect(bound(wrapper, 'cancel', 'label')).toBe('Cancel');
    expect(has(wrapper, 'delete-task')).toBe(false);

    control(wrapper, 'cancel').vm.$emit('click');
    expect(wrapper.emitted('cancel')).toHaveLength(1);
  });

  it('editing: "Delete task" asks the dialog to delete', () => {
    const { wrapper } = mountForm({ task: task() });

    expect(bound(wrapper, 'delete-task', 'label')).toBe('Delete task');
    control(wrapper, 'delete-task').vm.$emit('click');
    expect(wrapper.emitted('delete')).toHaveLength(1);
  });

  it('a read-only list: everything shown, nothing changeable, Close instead of Cancel, no Save or Delete', async () => {
    const { wrapper, vm } = mountForm({ task: task(), readOnly: true });

    expect(wrapper.get('[data-test="read-only-note"]').text()).toBe("This list is read only, so the task can't be changed here.");
    expect(bound(wrapper, 'title', 'readonly')).toBe(true);
    expect(bound(wrapper, 'notes', 'readonly')).toBe(true);
    for (const test of ['list', 'all-day', 'due', 'start', 'priority', 'percent-slider', 'percent', 'status']) {
      expect(bound(wrapper, test, 'disabled'), test).toBe(true);
    }
    expect(has(wrapper, 'save-task')).toBe(false);
    expect(has(wrapper, 'delete-task')).toBe(false);
    expect(bound(wrapper, 'cancel', 'label')).toBe('Close');

    await vm.handleSubmit();
    expect(wrapper.emitted('submit')).toBeUndefined();
  });

  it('a writable task has no read-only note', () => {
    expect(has(mountForm({ task: task() }).wrapper, 'read-only-note')).toBe(false);
  });
});

// ---- Reminders (#169 story 18, spec §10.9, US-6) --------------------------------------------------------------

describe('TaskForm — reminders', () => {
  const stored = (over: Partial<Reminder> = {}): Reminder => ({
    index: 0, action: 'display', related: 'end', offset_seconds: -1800, at: null, editable: true, ...over,
  });
  const field = (w: Wrapper) => w.findComponent({ name: 'CommonReminderField' });
  const fieldProps = (w: Wrapper) => field(w).props() as Record<string, unknown>;

  it('sits below Due and Start, above Priority', () => {
    const { wrapper } = mountForm({ defaultListId: 'errands' });
    const html = wrapper.html();
    const block = html.indexOf('reminders-block');
    expect(block).toBeGreaterThan(html.indexOf('data-test="start"'));
    expect(block).toBeLessThan(html.indexOf('data-test="priority"'));
    expect(fieldProps(wrapper).target).toBe('task');
  });

  it('is disabled with the hint while there is no due date, and enabled once one is set', async () => {
    const { wrapper, vm } = mountForm({ defaultListId: 'errands' });
    expect(fieldProps(wrapper).disabled).toBe(true);
    expect(fieldProps(wrapper).disabledHint).toBe('Set a due date to add a reminder');

    vm.form.due = local(2026, 11, 6);
    await nextTick();
    expect(fieldProps(wrapper).disabled).toBe(false);
  });

  it('a new task starts with none; an existing one shows its own', () => {
    expect(mountForm({ defaultListId: 'errands' }).vm.form.reminders).toEqual([]);
    const { wrapper } = mountForm({ task: task({ reminders: [stored()] }) });
    expect(fieldProps(wrapper).modelValue).toEqual([stored()]);
  });

  it('passes the all-day mode to the field', async () => {
    const { wrapper, vm } = mountForm({ defaultListId: 'errands' });
    expect(fieldProps(wrapper).allDay).toBe(true);
    vm.form.allDay = false;
    await nextTick();
    expect(fieldProps(wrapper).allDay).toBe(false);
  });

  it('a read-only owned list shows the chips without controls', () => {
    const { wrapper } = mountForm({ task: task({ reminders: [stored()] }), readOnly: true });
    expect(fieldProps(wrapper).readOnly).toBe(true);
  });

  it.each(['team', 'shared-rw', 'feed'])('hides the field on the list %s the user does not own', (uuid) => {
    const { wrapper } = mountForm({ task: task({ calendar_uuid: uuid, reminders: [stored()] }) });
    expect(has(wrapper, 'reminders-block')).toBe(false);
  });

  it('all day <-> timed maps the editable reminders when there is a due date, keeping them end-related', async () => {
    const t = task({ due: null, due_date: '2026-11-05', due_all_day: true, reminders: [stored({ offset_seconds: 32400 })] });
    const { vm } = mountForm({ task: t });
    vm.form.allDay = false;
    await nextTick();
    expect(vm.form.reminders.map((r) => [r.offset_seconds, r.related])).toEqual([[0, 'end']]);
    vm.form.allDay = true;
    await nextTick();
    expect(vm.form.reminders.map((r) => [r.offset_seconds, r.related])).toEqual([[32400, 'end']]);
  });

  it('the mode switch leaves stored reminders alone while there is no due date', async () => {
    const t = task({ due: null, due_date: null, reminders: [stored({ offset_seconds: -1800 })] });
    const { vm } = mountForm({ task: t });
    vm.form.allDay = true;
    await nextTick();
    expect(vm.form.reminders).toEqual([stored({ offset_seconds: -1800 })]);
  });

  it('clearing the due date takes the reminders back to the stored ones', async () => {
    const t = task({ reminders: [stored()] });
    const { vm } = mountForm({ task: t });
    vm.form.reminders = [...vm.form.reminders, { ...stored(), index: -1, offset_seconds: -3600 }];
    vm.form.due = null;
    await nextTick();
    expect(vm.form.reminders).toEqual([stored()]);
  });

  it('a foreign reminder is carried in the form values but never sent', async () => {
    const foreign = stored({ index: 1, action: 'email', editable: false, offset_seconds: -60 });
    const { wrapper, vm } = mountForm({ task: task({ reminders: [stored(), foreign] }) });
    await vm.handleSubmit();
    const [form, initial] = wrapper.emitted('submit')![0] as [TaskFormData, TaskFormData];
    expect(form.reminders).toContainEqual(foreign);
    expect(buildTaskPatch(initial, form, false)).toEqual({});
  });

  it('a recurring task keeps the field editable', () => {
    const { wrapper } = mountForm({ task: task({ ...RECURRING, reminders: [stored()] }) });
    expect(has(wrapper, 'reminders-block')).toBe(true);
    expect(fieldProps(wrapper).disabled).toBe(false);
    expect(fieldProps(wrapper).readOnly).toBe(false);
  });
});
