// @vitest-environment nuxt
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { enableAutoUnmount, mount } from '@vue/test-utils';
import { createTestingPinia } from '@pinia/testing';
import { defineComponent, h } from 'vue';
import TaskRow from './TaskRow.vue';
import { usePreferencesStore, PREF_TIME_FORMAT } from '~/stores/preferences';
import type { Task } from '~/types/tasks';

// SCOPE (#170 story 24, spec §7.3/§7.4/§7.5/§8): one task row. Overdue is
// text + icon + colour + the 3px start-edge bar (never colour alone);
// completed and cancelled are struck through and muted; pending dims the row
// and swaps the check for a spinner after a short delay; read-only and
// repeating tasks cannot be ticked (aria-disabled + reason) and read-only rows
// have no menu; priority is a shape with an aria-label, not a colour code;
// dates are <time datetime>; below sm the list and priority wrap under the
// title. TaskCheck is mounted for real (its own spec covers keys and motion);
// PrimeVue's Menu is a stub that records `toggle` and exposes its model.

enableAutoUnmount(afterEach);

// The row computes dates in the browser's zone: pin one (see utils/tasks.spec.ts).
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

function task(overrides: Partial<Task> = {}): Task {
  return {
    id: 'task-1',
    calendar_id: 1,
    calendar_uuid: 'errands',
    uid: 'task-1@calcard.io',
    summary: 'Renew passport',
    description: '',
    status: 'needs-action',
    completed: false,
    completed_at: null,
    percent_complete: null,
    priority: null,
    due: null,
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
    calendar_color: '#ff0000',
    ...overrides,
  };
}

const COMPLETED: Partial<Task> = { status: 'completed', completed: true, completed_at: '2026-11-02T10:00:00Z', percent_complete: 100 };

const menuToggle = vi.fn();
const MenuStub = defineComponent({
  name: 'Menu',
  props: { model: { type: Array, default: () => [] }, id: { type: String, default: undefined }, popup: Boolean },
  setup(props, { expose }) {
    expose({ toggle: menuToggle });
    return () => h('div', { 'data-test': 'menu-stub', id: props.id });
  },
});

type Props = InstanceType<typeof TaskRow>['$props'];

function mountRow(props: Partial<Props> = {}, timeFormat?: '12h' | '24h') {
  const pinia = createTestingPinia({ stubActions: false });
  if (timeFormat) usePreferencesStore(pinia).preferences[PREF_TIME_FORMAT] = timeFormat;
  return mount(TaskRow, {
    props: { task: task(), now: local(2026, 11, 3, 9, 30), ...props },
    global: { plugins: [pinia], stubs: { Menu: MenuStub } },
  });
}

type Wrapper = ReturnType<typeof mountRow>;

const check = (wrapper: Wrapper) => wrapper.get('[data-test="task-check"]');
const titleText = (wrapper: Wrapper) => wrapper.get('[data-test="task-title-text"]');
const dueLabel = (wrapper: Wrapper) => wrapper.get('[data-test="due-label"]');
/** Class list of a found element (`get` and `find` hand out different wrapper types). */
const classesOf = (el: { classes(): string[] }) => el.classes();
const menuModel = (wrapper: Wrapper) =>
  wrapper.getComponent(MenuStub).props('model') as { label: string; command: () => void }[];

interface TooltipTarget extends HTMLElement {
  $_ptooltipValue?: string;
}

describe('TaskRow — layout, type and tokens', () => {
  it('is a list item on the ground colour with a hairline rule, at least 48px tall', () => {
    const wrapper = mountRow();
    const row = wrapper.get('[data-test="task-row"]');

    expect(row.element.tagName).toBe('LI');
    expect(row.attributes('data-task-id')).toBe('task-1');
    expect(classesOf(row)).toEqual(
      expect.arrayContaining([
        'grid',
        'min-h-12',
        'border-b',
        'last:border-b-0',
        'border-surface-200',
        'dark:border-surface-800',
        'bg-surface-0',
        'dark:bg-surface-900',
      ]),
    );
    // No card styling: no rounding, no shadow, no hover lift.
    expect(row.attributes('class')).not.toMatch(/rounded|shadow|hover:/);
  });

  it('lays out [check 24] [title, grows] [list] [priority] [due] [menu] from sm, and drops list + priority under the title below sm', () => {
    const wrapper = mountRow({ task: task({ priority: 1, due_date: '2026-11-09' }), showList: true });
    const row = wrapper.get('[data-test="task-row"]');

    expect(classesOf(row)).toContain('grid-cols-[1.5rem_minmax(0,1fr)_auto_2rem]');
    expect(classesOf(row)).toContain('sm:grid-cols-[1.5rem_minmax(0,1fr)_auto_1rem_minmax(5.5rem,auto)_2rem]');
    const meta = wrapper.get('[data-test="task-meta"]');
    expect(classesOf(meta)).toEqual(expect.arrayContaining(['col-start-2', 'row-start-2', 'flex', 'sm:contents']));
    expect(classesOf(wrapper.get('[data-test="task-list"]'))).toEqual(
      expect.arrayContaining(['sm:col-start-3', 'sm:row-start-1']),
    );
    expect(wrapper.get('[data-test="priority-mark"]').element.parentElement!.className).toMatch(
      /sm:col-start-4 sm:row-start-1/,
    );
    expect(dueLabel(wrapper).element.parentElement!.className).toMatch(/col-start-3 .*sm:col-start-5/);
    expect(wrapper.get('[data-test="row-menu"]').element.parentElement!.className).toMatch(/col-start-4 .*sm:col-start-6/);
  });

  it('sets the title in 15px/500 ink, truncated, and the notes in one muted 13px line', () => {
    const wrapper = mountRow({ task: task({ description: 'Photos at the\n  booth,\tform B' }) });
    const title = wrapper.get('[data-test="task-title"]');
    const notes = wrapper.get('[data-test="task-notes"]');

    expect(titleText(wrapper).text()).toBe('Renew passport');
    expect(classesOf(title)).toEqual(
      expect.arrayContaining(['text-[15px]', 'font-medium', 'text-surface-900', 'dark:text-surface-100', 'min-w-0', 'truncate']),
    );
    expect(notes.text()).toBe('Photos at the booth, form B');
    expect(classesOf(notes)).toEqual(
      expect.arrayContaining(['text-[13px]', 'truncate', 'text-surface-500', 'dark:text-surface-400']),
    );
  });

  it('has no notes line without a description, and names an untitled task', () => {
    const wrapper = mountRow({ task: task({ summary: '   ', description: '  \n ' }) });

    expect(wrapper.find('[data-test="task-notes"]').exists()).toBe(false);
    expect(titleText(wrapper).text()).toBe('(No title)');
    expect(check(wrapper).attributes('aria-label')).toBe('Complete: (No title)');
  });

  // The API sends summary and description as plain text (#170 decision 9): the
  // iCalendar escaping (\, \; \\ \n) exists only in the stored object. The row
  // shows them exactly as sent — never escapes or unescapes them itself.
  it('shows a title and notes with , ; and \\ exactly as the API sent them (plain text)', () => {
    const summary = 'Buy milk, eggs; bread \\ x';
    const description = 'Milk, 2 l; eggs \\ bread\nfrom the shop, corner; C:\\temp';
    const wrapper = mountRow({ task: task({ summary, description }) });

    expect(titleText(wrapper).element.textContent).toBe(summary);
    expect(check(wrapper).attributes('aria-label')).toBe(`Complete: ${summary}`);
    expect(wrapper.get('[data-test="row-menu"]').attributes('aria-label')).toBe(`Task actions: ${summary}`);
    // One line: the newline becomes a space, nothing else changes.
    expect(wrapper.get('[data-test="task-notes"]').text()).toBe('Milk, 2 l; eggs \\ bread from the shop, corner; C:\\temp');
    const html = wrapper.html();
    for (const escaped of ['\\,', '\\;', '\\\\', '\\n']) expect(html).not.toContain(escaped);
  });

  it('sets the due label in 13px tabular figures that never wrap', () => {
    const wrapper = mountRow({ task: task({ due_date: '2026-11-09' }) });

    expect(classesOf(dueLabel(wrapper))).toEqual(
      expect.arrayContaining(['text-[13px]', 'tabular-nums', 'whitespace-nowrap']),
    );
    // Right-aligned in its cell.
    expect(dueLabel(wrapper).element.parentElement!.className).toContain('justify-end');
  });

  it('shows the list dot in the list colour with its name only in All tasks', () => {
    const inList = mountRow();
    expect(inList.find('[data-test="task-list"]').exists()).toBe(false);
    expect(inList.find('[data-test="task-meta"]').exists()).toBe(false);
    // One list with a priority: the second line holds the mark, not the list.
    const inListWithPriority = mountRow({ task: task({ priority: 1 }) });
    expect(inListWithPriority.find('[data-test="task-meta"]').exists()).toBe(true);
    expect(inListWithPriority.find('[data-test="task-list"]').exists()).toBe(false);
    expect(inListWithPriority.find('[data-test="list-dot"]').exists()).toBe(false);

    const inAll = mountRow({ showList: true });
    const dot = inAll.get('[data-test="list-dot"]');
    expect((dot.element as HTMLElement).style.backgroundColor).toMatch(/^(#ff0000|rgb\(255, 0, 0\))$/);
    expect(classesOf(dot)).toEqual(expect.arrayContaining(['h-2', 'w-2', 'rounded-full']));
    expect(dot.attributes('aria-hidden')).toBe('true');
    // Not colour alone: the name is there, and says what it is to a screen reader.
    expect(inAll.get('[data-test="task-list"]').text()).toBe('List: Errands');
    expect(inAll.get('[data-test="task-list"] .sr-only').text()).toBe('List:');
  });

  it('falls back to a neutral dot when the task carries no list colour', () => {
    const wrapper = mountRow({ task: task({ calendar_color: undefined }), showList: true });
    const dot = wrapper.get('[data-test="list-dot"]');

    expect((dot.element as HTMLElement).style.backgroundColor).toBe('');
    expect(classesOf(dot)).toContain('bg-surface-400');
  });
});

describe('TaskRow — overdue', () => {
  it('marks an all-day task due yesterday: red label, 3px start-edge bar, icon and hidden "Overdue"', () => {
    const wrapper = mountRow({ task: task({ due_date: '2026-11-02', due_all_day: true }) });

    const bar = wrapper.get('[data-test="overdue-bar"]');
    expect(classesOf(bar)).toEqual(
      expect.arrayContaining(['absolute', 'inset-y-0', 'start-0', 'w-[3px]', 'bg-red-600', 'dark:bg-red-400']),
    );
    expect(bar.attributes('aria-hidden')).toBe('true');
    expect(classesOf(dueLabel(wrapper))).toEqual(expect.arrayContaining(['text-red-600', 'dark:text-red-400']));
    const icon = wrapper.get('[data-test="overdue-icon"]');
    expect(classesOf(icon)).toEqual(expect.arrayContaining(['pi', 'pi-exclamation-circle']));
    expect(icon.attributes('aria-hidden')).toBe('true');
    const hidden = dueLabel(wrapper).get('.sr-only');
    expect(hidden.text()).toContain('Overdue');
    expect(dueLabel(wrapper).text()).toBe('Overdue, due Yesterday');
  });

  it('marks a timed task an instant after its due time, and not before', async () => {
    const wrapper = mountRow({
      task: task({ due: '2026-11-03T08:00:00Z' }), // 09:00 in Berlin
      now: local(2026, 11, 3, 8, 59),
    });
    expect(wrapper.find('[data-test="overdue-bar"]').exists()).toBe(false);
    expect(dueLabel(wrapper).text()).toBe('Due 09:00');

    await wrapper.setProps({ now: local(2026, 11, 3, 9, 1) });
    expect(wrapper.find('[data-test="overdue-bar"]').exists()).toBe(true);
    expect(dueLabel(wrapper).text()).toBe('Overdue, due 09:00');
  });

  it('turns an all-day task overdue when the clock it is given passes midnight', async () => {
    const wrapper = mountRow({ task: task({ due_date: '2026-11-03' }), now: local(2026, 11, 3, 23, 59) });
    expect(wrapper.find('[data-test="overdue-bar"]').exists()).toBe(false);
    expect(dueLabel(wrapper).text()).toBe('Due Today');

    await wrapper.setProps({ now: local(2026, 11, 4, 0, 0) });
    expect(wrapper.find('[data-test="overdue-bar"]').exists()).toBe(true);
    expect(dueLabel(wrapper).text()).toBe('Overdue, due Yesterday');
  });

  it.each([
    ['completed', COMPLETED],
    ['cancelled', { status: 'cancelled' as const }],
  ])('never marks a %s task overdue', (_name, overrides) => {
    const wrapper = mountRow({ task: task({ due_date: '2026-10-20', ...overrides }) });

    expect(wrapper.find('[data-test="overdue-bar"]').exists()).toBe(false);
    expect(wrapper.find('[data-test="overdue-icon"]').exists()).toBe(false);
    expect(wrapper.text()).not.toContain('Overdue');
    expect(classesOf(dueLabel(wrapper))).not.toContain('text-red-600');
    expect(classesOf(dueLabel(wrapper))).toEqual(expect.arrayContaining(['text-surface-500', 'dark:text-surface-400']));
  });

  it('keeps red and the bar off rows that are not overdue: today in ink, later quiet', () => {
    const today = mountRow({ task: task({ due_date: '2026-11-03' }) });
    expect(today.find('[data-test="overdue-bar"]').exists()).toBe(false);
    expect(classesOf(dueLabel(today))).toEqual(expect.arrayContaining(['text-surface-900', 'dark:text-surface-100']));

    const later = mountRow({ task: task({ due_date: '2026-11-09' }) });
    expect(later.find('[data-test="overdue-bar"]').exists()).toBe(false);
    expect(classesOf(dueLabel(later))).toEqual(expect.arrayContaining(['text-surface-500', 'dark:text-surface-400']));
    expect(later.html()).not.toContain('red-');
  });
});

describe('TaskRow — dates', () => {
  it('renders an all-day due date as <time datetime="YYYY-MM-DD">', () => {
    const wrapper = mountRow({ task: task({ due_date: '2026-11-09', due_all_day: true }) });
    const time = dueLabel(wrapper).get('time');

    expect(time.attributes('datetime')).toBe('2026-11-09');
    // The browser's locale (formatDue's default), e.g. "Mon 9 Nov" in en-GB.
    const expected = new Intl.DateTimeFormat(undefined, { weekday: 'short', day: 'numeric', month: 'short' }).format(
      local(2026, 11, 9),
    );
    expect(time.text()).toBe(expected);
  });

  it('renders a timed due date as <time> with the UTC instant, labelled in local time', () => {
    const wrapper = mountRow({ task: task({ due: '2026-11-04T16:00:00Z' }) });
    const time = dueLabel(wrapper).get('time');

    expect(time.attributes('datetime')).toBe('2026-11-04T16:00:00Z');
    expect(time.text()).toBe('Tomorrow, 17:00');
  });

  it("follows the user's 12-hour preference", () => {
    const wrapper = mountRow({ task: task({ due: '2026-11-04T16:00:00Z' }) }, '12h');

    expect(dueLabel(wrapper).get('time').text()).toMatch(/^Tomorrow, 5:00\sPM$/);
  });

  it('shows no due label (and no <time>) without a due date', () => {
    const wrapper = mountRow({ task: task({ due_all_day: true, start_date: '2026-11-01' }) });

    expect(wrapper.find('[data-test="due-label"]').exists()).toBe(false);
    expect(wrapper.find('time').exists()).toBe(false);
  });
});

describe('TaskRow — priority', () => {
  it.each([
    [1, 'High priority', 'pi-angle-double-up'],
    [4, 'High priority', 'pi-angle-double-up'],
    [5, 'Medium priority', 'pi-angle-up'],
    [6, 'Low priority', 'pi-angle-down'],
    [9, 'Low priority', 'pi-angle-down'],
  ])('priority %i shows %s as %s with an aria-label and the same tooltip', (priority, label, icon) => {
    const wrapper = mountRow({ task: task({ priority }) });
    const mark = wrapper.get('[data-test="priority-mark"]');

    expect(mark.attributes('role')).toBe('img');
    expect(mark.attributes('aria-label')).toBe(label);
    expect(classesOf(mark)).toEqual(expect.arrayContaining(['pi', icon]));
    expect((mark.element as TooltipTarget).$_ptooltipValue).toBe(label);
  });

  it.each([null, 0])('shows no mark for priority %s', (priority) => {
    const wrapper = mountRow({ task: task({ priority }) });

    expect(wrapper.find('[data-test="priority-mark"]').exists()).toBe(false);
    expect(wrapper.find('[data-test="task-meta"]').exists()).toBe(false);
  });

  it('tells the levels apart by shape, not colour: medium and low share the quiet tone, high is the accent', () => {
    const tone = (priority: number) => classesOf(mountRow({ task: task({ priority }) }).get('[data-test="priority-mark"]'));

    expect(tone(5)).toEqual(expect.arrayContaining(['text-surface-500', 'dark:text-surface-400']));
    expect(tone(9)).toEqual(expect.arrayContaining(['text-surface-500', 'dark:text-surface-400']));
    expect(tone(1)).toEqual(expect.arrayContaining(['text-primary-700', 'dark:text-primary-300']));
    for (const p of [1, 5, 9]) {
      expect(tone(p).join(' ')).not.toMatch(/red|green|amber|yellow|orange/);
    }
  });
});

describe('TaskRow — states', () => {
  it('an open task: unchecked, not struck, the strike ready to draw left to right over 200ms', () => {
    const wrapper = mountRow();

    expect(check(wrapper).attributes('aria-checked')).toBe('false');
    expect(check(wrapper).attributes('aria-label')).toBe('Complete: Renew passport');
    expect(titleText(wrapper).attributes('data-struck')).toBeUndefined();
    expect(classesOf(titleText(wrapper))).toEqual(
      expect.arrayContaining([
        'bg-[length:0%_1px]',
        'bg-[linear-gradient(currentColor,currentColor)]',
        'bg-no-repeat',
        'transition-[background-size]',
        'duration-200',
        'motion-reduce:transition-none',
      ]),
    );
  });

  it('a completed task: checked, title struck through and muted at weight 400', () => {
    const wrapper = mountRow({ task: task(COMPLETED) });

    expect(check(wrapper).attributes('aria-checked')).toBe('true');
    expect(check(wrapper).attributes('aria-label')).toBe('Mark as not done: Renew passport');
    expect(titleText(wrapper).attributes('data-struck')).toBe('true');
    expect(classesOf(titleText(wrapper))).toEqual(
      expect.arrayContaining(['bg-[length:100%_1px]', 'forced-colors:line-through']),
    );
    expect(classesOf(wrapper.get('[data-test="task-title"]'))).toEqual(
      expect.arrayContaining(['font-normal', 'text-surface-500', 'dark:text-surface-400']),
    );
    expect(classesOf(wrapper.get('[data-test="task-title"]'))).not.toContain('font-medium');
  });

  it('strikes the same title element when the task is ticked, so the strike animates instead of appearing', async () => {
    const wrapper = mountRow();
    const before = titleText(wrapper).element;

    await wrapper.setProps({ task: task(COMPLETED) });

    expect(titleText(wrapper).element).toBe(before);
    expect(titleText(wrapper).attributes('data-struck')).toBe('true');
  });

  it('a cancelled task: struck through, muted, a "Cancelled" tag, and still tickable', () => {
    const wrapper = mountRow({ task: task({ status: 'cancelled' }) });

    expect(titleText(wrapper).attributes('data-struck')).toBe('true');
    expect(classesOf(wrapper.get('[data-test="task-title"]'))).toContain('text-surface-500');
    expect(wrapper.get('[data-test="cancelled-tag"]').text()).toBe('Cancelled');
    expect(check(wrapper).attributes('aria-checked')).toBe('false');
    expect(check(wrapper).attributes('aria-disabled')).toBeUndefined();
  });

  it('has no Cancelled tag on other statuses', () => {
    for (const status of ['needs-action', 'in-process', 'completed'] as const) {
      expect(mountRow({ task: task({ status }) }).find('[data-test="cancelled-tag"]').exists()).toBe(false);
    }
  });

  describe('pending write', () => {
    afterEach(() => {
      vi.useRealTimers();
    });

    it('is aria-busy at once, and dims to 60% with a spinner for the check only after 300ms', async () => {
      vi.useFakeTimers();
      const wrapper = mountRow({ task: task(COMPLETED) });
      const row = () => wrapper.get('[data-test="task-row"]');

      await wrapper.setProps({ pending: true });
      expect(row().attributes('aria-busy')).toBe('true');
      expect(classesOf(row())).not.toContain('opacity-60');
      expect(wrapper.find('[data-test="check-spinner"]').exists()).toBe(false);

      await vi.advanceTimersByTimeAsync(299);
      expect(wrapper.find('[data-test="check-spinner"]').exists()).toBe(false);

      await vi.advanceTimersByTimeAsync(1);
      expect(classesOf(row())).toContain('opacity-60');
      expect(wrapper.find('[data-test="check-spinner"]').exists()).toBe(true);
      expect(wrapper.find('[data-test="check-ring"]').exists()).toBe(false);

      await wrapper.setProps({ pending: false });
      expect(row().attributes('aria-busy')).toBeUndefined();
      expect(classesOf(row())).not.toContain('opacity-60');
      expect(wrapper.find('[data-test="check-ring"]').exists()).toBe(true);
    });

    it('shows nothing when the write ends within the delay, and leaves no timer behind', async () => {
      vi.useFakeTimers();
      const wrapper = mountRow();

      await wrapper.setProps({ pending: true });
      await vi.advanceTimersByTimeAsync(150);
      await wrapper.setProps({ pending: false });
      await vi.advanceTimersByTimeAsync(1000);

      expect(classesOf(wrapper.get('[data-test="task-row"]'))).not.toContain('opacity-60');
      expect(wrapper.find('[data-test="check-spinner"]').exists()).toBe(false);
      expect(vi.getTimerCount()).toBe(0);
    });

    it('ignores the check while a write is in flight, before the spinner shows too', async () => {
      const wrapper = mountRow({ pending: true });

      await check(wrapper).trigger('click');
      expect(wrapper.emitted('toggle')).toBeUndefined();
    });
  });

  it('read-only list: the check is disabled with the reason and there is no menu', async () => {
    const wrapper = mountRow({ readOnly: true });

    expect(check(wrapper).attributes('aria-disabled')).toBe('true');
    const reason = wrapper.get(`[id="${check(wrapper).attributes('aria-describedby')}"]`);
    expect(reason.text()).toBe('This list is read only');
    expect((check(wrapper).element as TooltipTarget).$_ptooltipValue).toBe('This list is read only');
    expect(wrapper.find('[data-test="row-menu"]').exists()).toBe(false);
    expect(wrapper.findComponent(MenuStub).exists()).toBe(false);

    await check(wrapper).trigger('click');
    expect(wrapper.emitted('toggle')).toBeUndefined();
  });

  it('recurring task: the repeat icon, and the check disabled with "Repeating tasks can be completed in your task app"', async () => {
    const wrapper = mountRow({ task: task({ is_recurring: true, rrule: 'FREQ=WEEKLY' }) });

    const repeat = wrapper.get('[data-test="repeat-icon"]');
    expect(classesOf(repeat)).toEqual(expect.arrayContaining(['pi', 'pi-replay']));
    expect(repeat.attributes('role')).toBe('img');
    expect(repeat.attributes('aria-label')).toBe('Repeats');
    expect((repeat.element as TooltipTarget).$_ptooltipValue).toBe('Repeats');

    expect(check(wrapper).attributes('aria-disabled')).toBe('true');
    const reason = wrapper.get(`[id="${check(wrapper).attributes('aria-describedby')}"]`);
    expect(reason.text()).toBe('Repeating tasks can be completed in your task app');
    expect((check(wrapper).element as TooltipTarget).$_ptooltipValue).toBe(
      'Repeating tasks can be completed in your task app',
    );

    await check(wrapper).trigger('click');
    expect(wrapper.emitted('toggle')).toBeUndefined();
    // Its menu cannot complete it either; it can still be edited and deleted.
    expect(menuModel(wrapper).map((i) => i.label)).toEqual(['Edit task', 'Delete task']);
  });

  it('a completed repeating task can be reopened: the check is enabled and the menu offers "Mark as not done"', async () => {
    const wrapper = mountRow({ task: task({ ...COMPLETED, is_recurring: true, rrule: 'FREQ=WEEKLY' }) });

    expect(check(wrapper).attributes('aria-disabled')).toBeUndefined();
    expect(wrapper.find('[data-test="check-reason"]').exists()).toBe(false);
    expect(menuModel(wrapper).map((i) => i.label)).toEqual(['Mark as not done', 'Edit task', 'Delete task']);

    await check(wrapper).trigger('click');
    expect(wrapper.emitted('toggle')).toHaveLength(1);
  });

  it('has no repeat icon on a one-off task', () => {
    expect(mountRow().find('[data-test="repeat-icon"]').exists()).toBe(false);
  });

  it('a read-only repeating task gives the read-only reason (it cannot be changed at all)', () => {
    const wrapper = mountRow({ task: task({ is_recurring: true }), readOnly: true });

    expect(wrapper.get(`[id="${check(wrapper).attributes('aria-describedby')}"]`).text()).toBe('This list is read only');
  });
});

describe('TaskRow — actions', () => {
  afterEach(() => {
    menuToggle.mockClear();
  });

  it('emits toggle with the task when the check is ticked', async () => {
    const t = task();
    const wrapper = mountRow({ task: t });

    await check(wrapper).trigger('click');

    expect(wrapper.emitted('toggle')).toEqual([[t]]);
  });

  it('opens its menu from a labelled button that points at the menu', async () => {
    const wrapper = mountRow();
    const button = wrapper.get('[data-test="row-menu"]');

    expect(button.element.tagName).toBe('BUTTON');
    expect(button.attributes('aria-label')).toBe('Task actions: Renew passport');
    expect(button.attributes('aria-haspopup')).toBe('true');
    expect(button.attributes('aria-controls')).toBe(wrapper.getComponent(MenuStub).props('id'));
    expect(wrapper.getComponent(MenuStub).props('popup')).toBe(true);

    await button.trigger('click');
    expect(menuToggle).toHaveBeenCalledTimes(1);
    expect(menuToggle.mock.calls[0]![0]).toBeInstanceOf(Event);
  });

  it('offers "Complete", "Edit task" and "Delete task" for an open task, and each emits with the task', () => {
    const t = task();
    const wrapper = mountRow({ task: t });
    const items = menuModel(wrapper);

    expect(items.map((i) => i.label)).toEqual(['Complete', 'Edit task', 'Delete task']);
    items[0]!.command();
    items[1]!.command();
    items[2]!.command();
    expect(wrapper.emitted('toggle')).toEqual([[t]]);
    expect(wrapper.emitted('open')).toEqual([[t]]);
    expect(wrapper.emitted('delete')).toEqual([[t]]);
  });

  it('offers "Mark as not done" for a completed task', () => {
    const wrapper = mountRow({ task: task(COMPLETED) });

    expect(menuModel(wrapper).map((i) => i.label)).toEqual(['Mark as not done', 'Edit task', 'Delete task']);
  });

  // Story 26: the title opens the task. It is a real button (a screen reader
  // can activate it) but out of the Tab order: the keyboard opens a task with
  // Enter on the row or "Edit task", so the row keeps two Tab stops.
  it('opens the task from a click on its title, a button outside the Tab order', async () => {
    const t = task();
    const wrapper = mountRow({ task: t });
    const title = wrapper.get('[data-test="task-title"]');

    expect(title.element.tagName).toBe('BUTTON');
    expect(title.attributes('type')).toBe('button');
    expect(title.attributes('tabindex')).toBe('-1');
    expect(title.text()).toBe('Renew passport');

    await title.trigger('click');
    expect(wrapper.emitted('open')).toEqual([[t]]);
    expect(wrapper.emitted('toggle')).toBeUndefined();
  });

  it('a read-only row has no menu, but its title still opens the task (read only)', async () => {
    const t = task();
    const wrapper = mountRow({ task: t, readOnly: true });

    await wrapper.get('[data-test="task-title"]').trigger('click');
    expect(wrapper.emitted('open')).toEqual([[t]]);
  });

  it('a pending row still opens', async () => {
    const wrapper = mountRow({ pending: true });

    await wrapper.get('[data-test="task-title"]').trigger('click');
    expect(wrapper.emitted('open')).toHaveLength(1);
  });

  it('does not toggle from the menu while a write is in flight', () => {
    const wrapper = mountRow({ pending: true });

    menuModel(wrapper)[0]!.command();
    expect(wrapper.emitted('toggle')).toBeUndefined();
  });
});

describe('TaskRow — roving tabindex (story 25)', () => {
  it('keeps the check and the menu button in the Tab order by default', () => {
    const wrapper = mountRow();

    expect(check(wrapper).attributes('tabindex')).toBeUndefined();
    expect(wrapper.get('[data-test="row-menu"]').attributes('tabindex')).toBeUndefined();
  });

  it('takes both out of the Tab order when not tabbable (the list gives the stop to one row)', () => {
    const wrapper = mountRow({ tabbable: false });

    expect(check(wrapper).attributes('tabindex')).toBe('-1');
    expect(wrapper.get('[data-test="row-menu"]').attributes('tabindex')).toBe('-1');
  });

  it('lets the list put its tabindex and focus ring on the <li>', () => {
    const wrapper = mount(TaskRow, {
      props: { task: task(), now: local(2026, 11, 3, 9, 30) },
      attrs: { tabindex: '0', class: 'focus-visible:ring-2' },
      global: { plugins: [createTestingPinia({ stubActions: false })], stubs: { Menu: MenuStub } },
    });
    const row = wrapper.get('[data-test="task-row"]');

    expect(row.element.tagName).toBe('LI');
    expect(row.attributes('tabindex')).toBe('0');
    expect(row.classes()).toContain('focus-visible:ring-2');
  });
});
