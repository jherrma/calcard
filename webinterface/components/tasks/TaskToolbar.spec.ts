// @vitest-environment nuxt
import { describe, it, expect, afterEach, vi } from 'vitest';
import { mount } from '@vue/test-utils';
import type { VueWrapper } from '@vue/test-utils';
import TaskToolbar from './TaskToolbar.vue';
import type { Calendar } from '~/types/calendar';
import type { TasksSummary } from '~/types/tasks';

// SCOPE (#170 story 22): the Tasks page header. It is presentational: every
// choice goes out as an event and the page drives the store, so these specs
// assert what is shown and what is emitted. PrimeVue controls are shallow
// stubs; their bound values sit in the stub's $attrs, keyed as written in the
// template (PrimeVue declares its props on a Base* parent the stub does not
// copy). The search box is a native input and is driven for real.

function list(uuid: string, extra: Partial<Calendar> = {}): Calendar {
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

// Server order mixes own and shared lists on purpose.
const LISTS: Calendar[] = [
  list('errands', { name: 'Errands', color: '#ff0000' }),
  list('team', { name: 'Team tasks', shared: true, permission: 'read-only' }),
  list('home', { name: 'Home' }),
  list('feed', { name: 'Feed', kind: 'both', subscribed: true }),
  list('family', { name: 'Family', shared: true, permission: 'read-write' }),
];

const SUMMARY: TasksSummary = {
  open: 14,
  overdue: 3,
  due_today: 2,
  completed: 5,
  by_calendar: {
    errands: { open: 9, overdue: 2, completed: 0 },
    team: { open: 4, overdue: 1, completed: 0 },
    home: { open: 0, overdue: 0, completed: 0 },
    feed: { open: 0, overdue: 0, completed: 0 },
    family: { open: 1, overdue: 0, completed: 0 },
  },
};

type Props = InstanceType<typeof TaskToolbar>['$props'];

function mountToolbar(props: Partial<Props> = {}) {
  return mount(TaskToolbar, {
    props: {
      lists: LISTS,
      selectedListId: null,
      writableIds: ['errands', 'home', 'family'],
      summary: SUMMARY,
      statusFilter: 'open',
      searchQuery: '',
      sortBy: 'due',
      sortDir: 'asc',
      ...props,
    },
    shallow: true,
  });
}

type Wrapper = ReturnType<typeof mountToolbar>;

/** A PrimeVue stub by its data-test hook. */
const control = (wrapper: Wrapper, name: string) => wrapper.findComponent(`[data-test="${name}"]`) as VueWrapper;
/** A value bound on a stub (see SCOPE). */
const bound = (wrapper: Wrapper, name: string, key: string) => control(wrapper, name).vm.$attrs[key];
const labelsOf = (options: unknown) => (options as { label: string }[]).map((o) => o.label);
const search = (wrapper: Wrapper) => wrapper.get<HTMLInputElement>('input[data-test="search"]');

afterEach(() => {
  vi.useRealTimers();
});

describe('TaskToolbar — title and count', () => {
  it('says "All tasks" and the open count of every list when nothing is selected', () => {
    const wrapper = mountToolbar();

    expect(wrapper.get('[data-test="view-title"]').text()).toBe('All tasks');
    expect(wrapper.get('[data-test="open-count"]').text()).toBe('14 open');
  });

  it("names the selected list and shows that list's own open count", () => {
    const wrapper = mountToolbar({ selectedListId: 'team' });

    expect(wrapper.get('[data-test="view-title"]').text()).toBe('Team tasks');
    // REVERT PROOF: summary.open here would say 14.
    expect(wrapper.get('[data-test="open-count"]').text()).toBe('4 open');
  });

  it('says "0 open" for a list with nothing open, and nothing until the summary loads', async () => {
    const wrapper = mountToolbar({ selectedListId: 'home' });
    expect(wrapper.get('[data-test="open-count"]').text()).toBe('0 open');

    await wrapper.setProps({ summary: null });
    expect(wrapper.find('[data-test="open-count"]').exists()).toBe(false);
  });

  it('keeps the heading for screen readers on phones, where the list picker shows the name', () => {
    const wrapper = mountToolbar();
    const heading = wrapper.get('[data-test="view-title"]');

    expect(heading.element.tagName).toBe('H2');
    expect(heading.classes()).toEqual(expect.arrayContaining(['sr-only', 'lg:not-sr-only']));
    expect(control(wrapper, 'list-select').classes()).toContain('lg:hidden');
  });
});

describe('TaskToolbar — phone list picker', () => {
  it('offers All tasks, then own lists, then shared ones, marking read-only lists', () => {
    const wrapper = mountToolbar();
    const options = bound(wrapper, 'list-select', 'options') as { label: string; value: string; readOnly: boolean }[];

    expect(labelsOf(options)).toEqual(['All tasks', 'Errands', 'Home', 'Feed', 'Team tasks', 'Family']);
    expect(options.filter((o) => o.readOnly).map((o) => o.label)).toEqual(['Feed', 'Team tasks']);
    expect(bound(wrapper, 'list-select', 'aria-label')).toBe('Task list');
  });

  it('shows the selection, with All tasks as its own value', async () => {
    const wrapper = mountToolbar();
    const allValue = bound(wrapper, 'list-select', 'model-value');
    expect(allValue).toBe((bound(wrapper, 'list-select', 'options') as { value: string }[])[0]!.value);

    await wrapper.setProps({ selectedListId: 'home' });
    expect(bound(wrapper, 'list-select', 'model-value')).toBe('home');
  });

  it('emits the chosen list, null for All tasks, and nothing for the current one or no value', () => {
    const wrapper = mountToolbar({ selectedListId: 'errands' });
    const picker = control(wrapper, 'list-select');
    const allValue = (bound(wrapper, 'list-select', 'options') as { value: string }[])[0]!.value;

    picker.vm.$emit('update:modelValue', 'team');
    picker.vm.$emit('update:modelValue', allValue);
    picker.vm.$emit('update:modelValue', 'errands');
    picker.vm.$emit('update:modelValue', null);

    expect(wrapper.emitted('select-list')).toEqual([['team'], [null]]);
  });
});

describe('TaskToolbar — status filter', () => {
  it('is an Open / Done / All segmented control that cannot be emptied, labelled "Show tasks"', () => {
    const wrapper = mountToolbar({ statusFilter: 'completed' });
    const options = bound(wrapper, 'status-filter', 'options') as { label: string; value: string }[];

    expect(options).toEqual([
      { label: 'Open', value: 'open' },
      { label: 'Done', value: 'completed' },
      { label: 'All', value: 'all' },
    ]);
    expect(bound(wrapper, 'status-filter', 'model-value')).toBe('completed');
    expect(bound(wrapper, 'status-filter', 'allow-empty')).toBe(false);
    const labelId = bound(wrapper, 'status-filter', 'aria-labelledby') as string;
    expect(wrapper.get(`[id="${labelId}"]`).text()).toBe('Show tasks');
  });

  it('emits a new filter, and ignores a deselect (null), junk and the current one', () => {
    const wrapper = mountToolbar();
    const filter = control(wrapper, 'status-filter');

    filter.vm.$emit('update:modelValue', 'completed');
    filter.vm.$emit('update:modelValue', null);
    filter.vm.$emit('update:modelValue', 'overdue');
    filter.vm.$emit('update:modelValue', 'open');
    filter.vm.$emit('update:modelValue', 'all');

    expect(wrapper.emitted('filter-change')).toEqual([['completed'], ['all']]);
  });
});

describe('TaskToolbar — sort', () => {
  it('offers Due date, Priority and Title, showing the current one', () => {
    const wrapper = mountToolbar({ sortBy: 'priority' });

    expect(labelsOf(bound(wrapper, 'sort-by', 'options'))).toEqual(['Due date', 'Priority', 'Title']);
    expect(bound(wrapper, 'sort-by', 'model-value')).toBe('priority');
    expect(bound(wrapper, 'sort-by', 'aria-label')).toBe('Sort by');
  });

  it('emits a new sort and ignores junk and the current one', () => {
    const wrapper = mountToolbar();
    const sort = control(wrapper, 'sort-by');

    sort.vm.$emit('update:modelValue', 'title');
    sort.vm.$emit('update:modelValue', 'due');
    sort.vm.$emit('update:modelValue', null);

    expect(wrapper.emitted('sort-change')).toEqual([['title']]);
  });

  it.each([
    ['due', 'asc', 'false', 'Earliest due first', 'pi pi-sort-amount-up-alt'],
    ['due', 'desc', 'true', 'Latest due first', 'pi pi-sort-amount-down'],
    ['priority', 'asc', 'false', 'Highest priority first', 'pi pi-sort-amount-up-alt'],
    ['priority', 'desc', 'true', 'Lowest priority first', 'pi pi-sort-amount-down'],
    ['title', 'asc', 'false', 'A to Z', 'pi pi-sort-amount-up-alt'],
    ['title', 'desc', 'true', 'Z to A', 'pi pi-sort-amount-down'],
  ] as const)('direction button for %s %s: aria-pressed %s, described as "%s"', (sortBy, sortDir, pressed, words, icon) => {
    const wrapper = mountToolbar({ sortBy, sortDir });
    const button = control(wrapper, 'sort-dir');

    // The name stays the same; the pressed state carries the direction.
    expect(button.vm.$attrs['aria-label']).toBe('Reverse order');
    expect(button.vm.$attrs['aria-pressed']).toBe(pressed);
    expect(button.vm.$attrs.icon).toBe(icon);
    const describedBy = button.vm.$attrs['aria-describedby'] as string;
    expect(wrapper.get(`[id="${describedBy}"]`).text()).toBe(words);
  });

  it('asks to reverse the order on click', async () => {
    const wrapper = mountToolbar();

    await control(wrapper, 'sort-dir').trigger('click');

    expect(wrapper.emitted('toggle-sort-dir')).toHaveLength(1);
  });
});

describe('TaskToolbar — search', () => {
  it('starts from the search the store holds', () => {
    const wrapper = mountToolbar({ searchQuery: 'invoice' });

    expect(search(wrapper).element.value).toBe('invoice');
    expect(search(wrapper).attributes('aria-label')).toBe('Search tasks');
    expect(search(wrapper).attributes('type')).toBe('search');
  });

  it('sends the text once, 300 ms after the last keystroke', async () => {
    vi.useFakeTimers();
    const wrapper = mountToolbar();

    await search(wrapper).setValue('r');
    await search(wrapper).setValue('re');
    await search(wrapper).setValue('rent');
    vi.advanceTimersByTime(299);
    expect(wrapper.emitted('search')).toBeUndefined();

    vi.advanceTimersByTime(1);
    expect(wrapper.emitted('search')).toEqual([['rent']]);
  });

  it('sends at once on Enter, and the pending debounce does not send it again', async () => {
    vi.useFakeTimers();
    const wrapper = mountToolbar();

    await search(wrapper).setValue('rent');
    await search(wrapper).trigger('keydown', { key: 'Enter' });
    expect(wrapper.emitted('search')).toEqual([['rent']]);

    vi.advanceTimersByTime(300);
    expect(wrapper.emitted('search')).toEqual([['rent']]);
  });

  it('does not resend a change of surrounding blanks, but does send a cleared box', async () => {
    vi.useFakeTimers();
    const wrapper = mountToolbar({ searchQuery: 'rent' });

    await search(wrapper).setValue('rent ');
    vi.advanceTimersByTime(300);
    expect(wrapper.emitted('search')).toBeUndefined();

    await search(wrapper).setValue('');
    vi.advanceTimersByTime(300);
    expect(wrapper.emitted('search')).toEqual([['']]);
  });

  it('takes a search cleared from outside, but never overwrites typing with the echo of its own search', async () => {
    vi.useFakeTimers();
    const wrapper = mountToolbar();

    await search(wrapper).setValue('ren');
    vi.advanceTimersByTime(300);
    // The user keeps typing while the store takes 'ren'.
    await search(wrapper).setValue('rent');
    await wrapper.setProps({ searchQuery: 'ren' });
    expect(search(wrapper).element.value).toBe('rent');

    // Something else (e.g. "Clear filters") resets the store's search.
    await wrapper.setProps({ searchQuery: '' });
    expect(search(wrapper).element.value).toBe('');
  });
});
