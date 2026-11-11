// @vitest-environment nuxt
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { mount } from '@vue/test-utils';
import { defineComponent, h } from 'vue';
import type { MenuItem } from 'primevue/menuitem';
import TasksSidebar from './TasksSidebar.vue';
import type { Calendar } from '~/types/calendar';
import type { TasksSummary } from '~/types/tasks';

// SCOPE (#170 story 22): the Tasks section's list picker. Single-select rows
// (aria-current, never checkboxes), own lists then "Shared with me", colour
// dot and open count from summary.by_calendar, the read-only lock with its
// "Read only" tooltip AND its screen-reader text, "+" for a new list, and the
// placeholder lines while the lists load. Mounted for real: the rows are plain
// buttons, and v-tooltip is the real PrimeVue directive.

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

const LISTS: Calendar[] = [
  list('errands', { name: 'Errands', color: '#ff0000' }),
  list('team', { name: 'Team tasks', color: '#00aa00', shared: true, permission: 'read-only', owner: { id: 'o1', display_name: 'Alex' } }),
  list('home', { name: 'Home', color: '#0000ff' }),
  list('feed', { name: 'Feed', color: '#999999', kind: 'both', subscribed: true }),
  list('family', { name: 'Family', color: '#aa00aa', shared: true, permission: 'read-write', owner: { id: 'o2', display_name: 'Sam' } }),
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

function mountSidebar(props: Partial<InstanceType<typeof TasksSidebar>['$props']> = {}) {
  return mount(TasksSidebar, {
    props: {
      lists: LISTS,
      selectedId: null,
      // Writable: own lists that are not subscriptions, and the read-write share.
      writableIds: ['errands', 'home', 'family'],
      summary: SUMMARY,
      ...props,
    },
  });
}

const row = (wrapper: ReturnType<typeof mountSidebar>, uuid: string) => wrapper.get(`button[data-list-id="${uuid}"]`);
const allRow = (wrapper: ReturnType<typeof mountSidebar>) => wrapper.get('button[data-list-id=""]');
const rowNames = (wrapper: ReturnType<typeof mountSidebar>, selector: string) =>
  wrapper.findAll(`${selector} button[data-list-id]`).map((b) => b.attributes('data-list-id'));

describe('TasksSidebar — rows', () => {
  it('lists All tasks, then own lists (subscriptions included), then the shared ones under their own heading', () => {
    const wrapper = mountSidebar();

    expect(allRow(wrapper).text()).toContain('All tasks');
    const own = wrapper.findAll('ul')[0]!;
    expect(own.findAll('button[data-list-id]').map((b) => b.attributes('data-list-id'))).toEqual(['errands', 'home', 'feed']);
    expect(rowNames(wrapper, '[data-test="shared-lists"]')).toEqual(['team', 'family']);
    const headings = wrapper.findAll('h2').map((h) => h.text());
    expect(headings).toEqual(['My lists', 'Shared with me']);
    // Each list is labelled by its heading.
    expect(own.attributes('aria-labelledby')).toBe(wrapper.findAll('h2')[0]!.attributes('id'));
  });

  it('keeps its scroll area out of the Tab order (Firefox would make an overflowing one a stop of its own)', () => {
    const wrapper = mountSidebar();
    const scroller = wrapper.get('[data-test="lists-scroller"]');

    expect(scroller.classes()).toContain('overflow-y-auto');
    expect(scroller.attributes('tabindex')).toBe('-1');
    expect(scroller.element.contains(allRow(wrapper).element)).toBe(true);
  });

  it('leaves the Shared heading out when nothing is shared', () => {
    const wrapper = mountSidebar({ lists: LISTS.filter((l) => !l.shared) });

    expect(wrapper.findAll('h2').map((h) => h.text())).toEqual(['My lists']);
    expect(wrapper.find('[data-test="shared-lists"]').exists()).toBe(false);
  });

  it("paints each list's own colour as its dot", () => {
    const wrapper = mountSidebar();

    expect(row(wrapper, 'errands').get('[data-test="list-dot"]').attributes('style')).toContain('background-color: #ff0000');
    expect(row(wrapper, 'team').get('[data-test="list-dot"]').attributes('style')).toContain('background-color: #00aa00');
  });

  it('shows the open counts from the summary, All tasks from `open`, and leaves zero blank', () => {
    const wrapper = mountSidebar();

    // Visible number plus " open" for screen readers.
    expect(allRow(wrapper).text()).toMatch(/All tasks\s*14 open$/);
    expect(row(wrapper, 'errands').text()).toMatch(/Errands\s*9 open$/);
    expect(row(wrapper, 'team').text()).toContain('4 open');
    expect(row(wrapper, 'family').text()).toContain('1 open');
    // REVERT PROOF: rendering `0` puts "0 open" into these rows.
    expect(row(wrapper, 'home').text()).not.toContain('open');
    expect(row(wrapper, 'feed').text()).not.toContain('open');
  });

  it('shows no counts before the summary has loaded', () => {
    const wrapper = mountSidebar({ summary: null });

    expect(wrapper.text()).not.toContain('open');
    expect(wrapper.findAll('button[data-list-id]')).toHaveLength(LISTS.length + 1);
  });

  it("names a shared list's owner", () => {
    const wrapper = mountSidebar();

    expect(row(wrapper, 'team').text()).toContain('Shared by Alex');
    expect(row(wrapper, 'family').text()).toContain('Shared by Sam');
  });
});

describe('TasksSidebar — read-only lists', () => {
  it('locks read-only shares and subscriptions, with a "Read only" tooltip and the same words for screen readers', () => {
    const wrapper = mountSidebar();

    for (const uuid of ['team', 'feed']) {
      const lock = row(wrapper, uuid).find('[data-test="read-only"]');
      expect(lock.exists(), uuid).toBe(true);
      expect(lock.classes()).toContain('pi-lock');
      expect(lock.attributes('aria-hidden')).toBe('true');
      // The real PrimeVue directive keeps its text on the element.
      expect((lock.element as HTMLElement & { $_ptooltipValue?: string }).$_ptooltipValue).toBe('Read only');
      expect(row(wrapper, uuid).text()).toContain('Read only');
    }
  });

  it('does not lock writable lists (own lists, read-write shares)', () => {
    const wrapper = mountSidebar();

    for (const uuid of ['errands', 'home', 'family']) {
      expect(row(wrapper, uuid).find('[data-test="read-only"]').exists(), uuid).toBe(false);
      expect(row(wrapper, uuid).text(), uuid).not.toContain('Read only');
    }
  });
});

describe('TasksSidebar — selection', () => {
  it('marks All tasks as current when nothing is selected', () => {
    const wrapper = mountSidebar();

    expect(allRow(wrapper).attributes('aria-current')).toBe('true');
    expect(wrapper.findAll('[aria-current]')).toHaveLength(1);
  });

  it('marks exactly the selected list as current (single-select)', () => {
    const wrapper = mountSidebar({ selectedId: 'team' });

    expect(row(wrapper, 'team').attributes('aria-current')).toBe('true');
    expect(row(wrapper, 'team').classes()).toContain('font-semibold');
    expect(allRow(wrapper).attributes('aria-current')).toBeUndefined();
    expect(wrapper.findAll('[aria-current]')).toHaveLength(1);
  });

  it('emits the list UUID, or null for All tasks', async () => {
    const wrapper = mountSidebar({ selectedId: 'errands' });

    await row(wrapper, 'family').trigger('click');
    await allRow(wrapper).trigger('click');

    expect(wrapper.emitted('select')).toEqual([['family'], [null]]);
  });

  it('asks for a new list from "+", labelled for screen readers and with a tooltip', async () => {
    const wrapper = mountSidebar();
    const add = wrapper.get('[data-test="add-list"]');

    expect(add.attributes('aria-label')).toBe('Create task list');
    expect((add.element as HTMLElement & { $_ptooltipValue?: string }).$_ptooltipValue).toBe('Create task list');
    await add.trigger('click');

    expect(wrapper.emitted('add-list')).toHaveLength(1);
    expect(wrapper.emitted('select')).toBeUndefined();
  });
});

describe('TasksSidebar — loading', () => {
  it('shows placeholder lines instead of the lists, and keeps All tasks and "+" usable', async () => {
    const wrapper = mountSidebar({ lists: [], loading: true });

    const skeleton = wrapper.get('[data-test="lists-skeleton"]');
    expect(skeleton.attributes('aria-busy')).toBe('true');
    expect(skeleton.text()).toBe('Loading task lists');
    expect(wrapper.find('ul').exists()).toBe(false);

    await allRow(wrapper).trigger('click');
    await wrapper.get('[data-test="add-list"]').trigger('click');
    expect(wrapper.emitted('select')).toEqual([[null]]);
    expect(wrapper.emitted('add-list')).toHaveLength(1);
  });

  it('replaces the placeholder lines with the lists once loaded', async () => {
    const wrapper = mountSidebar({ lists: [], loading: true });

    await wrapper.setProps({ lists: LISTS, loading: false });

    expect(wrapper.find('[data-test="lists-skeleton"]').exists()).toBe(false);
    expect(wrapper.findAll('button[data-list-id]')).toHaveLength(LISTS.length + 1);
  });
});

// --- Story 27: the owner's list menu --------------------------------------

const { menuToggle } = vi.hoisted(() => ({ menuToggle: vi.fn() }));

/** PrimeVue's popup Menu, reduced to its items as buttons and a `toggle` spy. */
const MenuStub = defineComponent({
  name: 'Menu',
  props: { model: { type: Array, default: () => [] }, id: { type: String, default: undefined } },
  emits: ['hide'],
  setup(props, { expose }) {
    expose({ toggle: menuToggle });
    return () => h('div', { 'data-test': 'menu-stub', id: props.id }, (props.model as MenuItem[]).map((item) => h('button', {
      type: 'button',
      'data-item': item.label,
      'data-icon': item.icon,
      onClick: () => (item.command as (e: unknown) => void)({}),
    }, item.label as string)));
  },
});

function mountWithMenu(props: Partial<InstanceType<typeof TasksSidebar>['$props']> = {}) {
  return mount(TasksSidebar, {
    props: { lists: LISTS, selectedId: null, writableIds: ['errands', 'home', 'family'], summary: SUMMARY, ...props },
    global: { stubs: { Menu: MenuStub } },
  });
}

const menuButton = (wrapper: ReturnType<typeof mountWithMenu>, uuid: string) =>
  wrapper.get(`button[data-list-id="${uuid}"]`).element.parentElement!.querySelector('[data-test="list-menu"]') as HTMLButtonElement | null;

describe('TasksSidebar — list menu (owner only)', () => {
  beforeEach(() => menuToggle.mockReset());

  it('is on the task lists the user owns, not on shares, subscriptions or mixed calendars', () => {
    // A subscription is always mixed today; one stored as a task list must still get no menu.
    const lists = [...LISTS, list('mixed', { name: 'Old mixed', kind: 'both' }), list('feed2', { name: 'Feed 2', subscribed: true })];
    const wrapper = mountWithMenu({ lists });

    const withMenu = lists.filter((l) => menuButton(wrapper, l.uuid) !== null).map((l) => l.uuid);
    expect(withMenu).toEqual(['errands', 'home']);
    expect(wrapper.findAll('[data-test="list-menu"]')).toHaveLength(2);
  });

  it('is a labelled popup button next to the row, not inside it', () => {
    const wrapper = mountWithMenu();
    const button = menuButton(wrapper, 'errands')!;

    expect(button.getAttribute('type')).toBe('button');
    expect(button.getAttribute('aria-label')).toBe('List actions: Errands');
    expect(button.getAttribute('aria-haspopup')).toBe('true');
    expect(button.getAttribute('aria-controls')).toBe(wrapper.get('[data-test="list-menu-items"]').attributes('id'));
    expect(wrapper.get('button[data-list-id="errands"]').find('[data-test="list-menu"]').exists()).toBe(false);
    expect(button.querySelector('i')!.getAttribute('aria-hidden')).toBe('true');
  });

  it('opens with List settings, Share list and Delete task list, each emitting the list (and never selecting it)', async () => {
    const wrapper = mountWithMenu();

    menuButton(wrapper, 'home')!.click();
    await wrapper.vm.$nextTick();
    expect(menuToggle).toHaveBeenCalledTimes(1);
    const items = wrapper.findAll('[data-item]');
    expect(items.map((i) => [i.attributes('data-item'), i.attributes('data-icon')])).toEqual([
      ['List settings', 'pi pi-cog'],
      ['Share list', 'pi pi-share-alt'],
      ['Delete task list', 'pi pi-trash'],
    ]);
    const home = LISTS.find((l) => l.uuid === 'home');
    const emitted = () => ['edit-list', 'share-list', 'delete-list'].filter((e) => wrapper.emitted(e));

    await items[0]!.trigger('click');
    expect(emitted()).toEqual(['edit-list']);
    await items[1]!.trigger('click');
    expect(emitted()).toEqual(['edit-list', 'share-list']);
    await items[2]!.trigger('click');
    expect(emitted()).toEqual(['edit-list', 'share-list', 'delete-list']);
    expect(wrapper.emitted('edit-list')).toEqual([[home]]);
    expect(wrapper.emitted('share-list')).toEqual([[home]]);
    expect(wrapper.emitted('delete-list')).toEqual([[home]]);
    expect(wrapper.emitted('select')).toBeUndefined();
  });

  it('shows on hover, keyboard focus and touch; always on the selected list and while its menu is open', async () => {
    const wrapper = mountWithMenu({ selectedId: 'home' });
    const hidden = ['opacity-0', 'group-hover:opacity-100', 'group-focus-within:opacity-100', '[@media(hover:none)]:opacity-100'];

    expect([...menuButton(wrapper, 'errands')!.classList]).toEqual(expect.arrayContaining(hidden));
    expect(wrapper.get('button[data-list-id="errands"]').element.parentElement!.classList).toContain('group');
    expect(menuButton(wrapper, 'home')!.classList).toContain('opacity-100');
    expect(menuButton(wrapper, 'home')!.classList).not.toContain('opacity-0');

    menuButton(wrapper, 'errands')!.click();
    await wrapper.vm.$nextTick();
    expect(menuButton(wrapper, 'errands')!.classList).toContain('opacity-100');
    expect(menuButton(wrapper, 'errands')!.classList).not.toContain('opacity-0');

    wrapper.getComponent(MenuStub).vm.$emit('hide');
    await wrapper.vm.$nextTick();
    expect(menuButton(wrapper, 'errands')!.classList).toContain('opacity-0');
  });

  it('keeps room for the menu at the end of every row, so the counts line up', () => {
    const wrapper = mountWithMenu();

    for (const button of wrapper.findAll('button[data-list-id]')) expect(button.classes()).toContain('pr-10');
  });
});
