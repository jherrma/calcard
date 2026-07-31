// @vitest-environment nuxt
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { mount } from '@vue/test-utils';
import type { VueWrapper } from '@vue/test-utils';
import { nextTick } from 'vue';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import { createTestingPinia } from '@pinia/testing';
import type { TestingPinia } from '@pinia/testing';
import GlobalSearch from './GlobalSearch.vue';
import { useSearchStore } from '~/stores/search';
import type { ContactHit, EventHit, SearchResults } from '~/types/search';
import type { Calendar } from '~/types/calendar';
import type { AddressBook } from '~/types/contacts';

// Two mount modes:
//  * mountShallow() — the Dialog and every row are stubbed; used for the wiring we
//    own (debounce, close/reset cycle, the route each result type navigates to).
//  * mountFull() — the Dialog is replaced by a pass-through stub so the palette's
//    CONTENT renders and the state branches (loading / recents / results / error /
//    no results), the 5-per-category cap and keyboard navigation are asserted
//    against the real DOM.

const { navigateToMock } = vi.hoisted(() => ({ navigateToMock: vi.fn() }));
mockNuxtImport('navigateTo', () => navigateToMock);

interface SearchVm {
  query: string;
  visible: boolean;
  activeIndex: number;
  focusInput: () => void;
  selectEvent: (hit: EventHit) => void;
  selectContact: (hit: ContactHit) => void;
  selectCalendar: () => void;
  selectAddressBook: () => void;
}

let pinia: TestingPinia;
const wrappers: VueWrapper[] = [];

/** Renders slot content only while `visible` — enough to exercise every branch. */
const DialogStub = {
  props: ['visible'],
  template: '<div v-if="visible" class="dialog-stub"><slot /></div>',
};

function mountShallow() {
  const wrapper = mount(GlobalSearch, { shallow: true, global: { plugins: [pinia] } });
  wrappers.push(wrapper);
  return { wrapper, vm: wrapper.vm as unknown as SearchVm };
}

function mountFull() {
  const wrapper = mount(GlobalSearch, {
    // Attached to the document so focus assertions can read document.activeElement.
    attachTo: document.body,
    global: {
      plugins: [pinia],
      stubs: { Dialog: DialogStub, ProgressSpinner: true, Tag: true },
    },
  });
  wrappers.push(wrapper);
  return { wrapper, vm: wrapper.vm as unknown as SearchVm };
}

function eventHit(overrides: Partial<EventHit['event']> = {}, key = 'ev-1::'): EventHit {
  // Built from local date parts so the yyyy-MM-dd link param is timezone-stable.
  const start = new Date(2026, 5, 16, 9, 0, 0);
  return {
    key,
    event: {
      id: 'ev-1',
      calendar_id: 7,
      uid: 'uid-1',
      summary: 'Team Standup',
      start: start.toISOString(),
      end: new Date(2026, 5, 16, 9, 30, 0).toISOString(),
      all_day: false,
      is_recurring: false,
      ...overrides,
    },
    calendarId: '7',
    calendarName: 'Work',
    calendarColor: '#ff0000',
  };
}

function contactHit(id = 'ct-1', name = 'Alice Adams'): ContactHit {
  return {
    contact: {
      id,
      addressbook_id: '2',
      uid: `uid-${id}`,
      formatted_name: name,
      created_at: '',
      updated_at: '',
    },
    addressBookId: '2',
    addressBookName: 'Work',
  };
}

function results(partial: Partial<SearchResults> = {}): SearchResults {
  return { events: [], contacts: [], calendars: [], addressBooks: [], ...partial };
}

/** Puts the store in the "results for <query> are on screen" state. */
function seed(query: string, payload: Partial<SearchResults>) {
  const store = useSearchStore();
  store.query = query;
  store.results = results(payload);
  store.isLoading = false;
  store.error = null;
  return store;
}

async function open(vm: SearchVm, query = '') {
  vm.visible = true;
  if (query) vm.query = query;
  await nextTick();
  await nextTick();
}

beforeEach(() => {
  // stubActions (the @pinia/testing default) keeps these specs about the component:
  // the store's own behaviour is covered in stores/search.spec.ts.
  pinia = createTestingPinia();
  navigateToMock.mockReset();
  vi.useFakeTimers();
});

afterEach(() => {
  // Unmount so the window-level Cmd/Ctrl+K listener of one test can't open the
  // palette of another.
  while (wrappers.length > 0) wrappers.pop()!.unmount();
  vi.useRealTimers();
});

describe('debounced search', () => {
  it('waits 300ms and then searches once', async () => {
    const store = useSearchStore();
    const { vm } = mountShallow();
    await open(vm);

    vm.query = 'ali';
    await nextTick();
    expect(store.search).not.toHaveBeenCalled();

    vm.query = 'alic';
    await nextTick();
    vi.advanceTimersByTime(299);
    expect(store.search).not.toHaveBeenCalled();

    vi.advanceTimersByTime(1);
    expect(store.search).toHaveBeenCalledTimes(1);
    expect(store.search).toHaveBeenCalledWith('alic');
  });

  it('clears immediately (no debounce) below the minimum length', async () => {
    const store = useSearchStore();
    const { vm } = mountShallow();
    await open(vm);

    vm.query = 'a';
    await nextTick();

    expect(store.search).toHaveBeenCalledWith('');
  });

  // REVERT PROOF for the uncancellable debounce: vueuse's useDebounceFn exposes no
  // cancel handle and only clears its timer when the wrapper is invoked again, so
  // the timer armed by "alice" survives the user deleting the query. If the
  // fire-time guard in runSearch is removed, the pending call fires here and
  // search('alice') is dispatched for text that is no longer in the input.
  it('does not fire the pending search after the query is cleared', async () => {
    const store = useSearchStore();
    const { vm } = mountShallow();
    await open(vm);

    vm.query = 'alice';
    await nextTick();
    vi.advanceTimersByTime(100);

    vm.query = '';
    await nextTick();
    vi.advanceTimersByTime(500); // the 'alice' timer would elapse in here

    expect(store.search).toHaveBeenCalledTimes(1);
    expect(store.search).toHaveBeenCalledWith('');
  });

  it('does not fire the pending search after the dialog is closed', async () => {
    const store = useSearchStore();
    const { vm } = mountShallow();
    await open(vm);

    vm.query = 'alice';
    await nextTick();

    vm.visible = false; // Escape / mask click
    await nextTick();
    vi.advanceTimersByTime(500);

    // Only the clear triggered by closing; never search('alice') for a closed dialog.
    expect(store.search).not.toHaveBeenCalledWith('alice');
    expect(store.reset).toHaveBeenCalled();
  });
});

describe('closing', () => {
  it('drops the query and resets the store', async () => {
    const store = useSearchStore();
    const { vm } = mountShallow();

    vm.visible = true;
    vm.query = 'alice';
    await nextTick();

    vm.visible = false;
    await nextTick();

    expect(vm.query).toBe('');
    expect(store.reset).toHaveBeenCalled();
  });
});

describe('result selection', () => {
  it('navigates to the calendar with a date + event deep link and records the search', async () => {
    const store = useSearchStore();
    const { vm } = mountShallow();
    await open(vm, 'standup');

    vm.selectEvent(eventHit());

    expect(store.rememberSearch).toHaveBeenCalledWith('standup');
    expect(navigateToMock).toHaveBeenCalledWith({
      path: '/calendar',
      query: { date: '2026-06-16', event: 'ev-1', cal: '7' },
    });
    expect(vm.visible).toBe(false);
  });

  it('carries recurrence_id so a single occurrence opens, not the series', async () => {
    const { vm } = mountShallow();

    vm.selectEvent(eventHit({ recurrence_id: '2026-06-16T09:00:00Z' }));

    expect(navigateToMock).toHaveBeenCalledWith({
      path: '/calendar',
      query: {
        date: '2026-06-16',
        event: 'ev-1',
        cal: '7',
        recurrence: '2026-06-16T09:00:00Z',
      },
    });
  });

  it('navigates to a contact with the NUMERIC address book id as ?ab=', async () => {
    const { vm } = mountShallow();

    vm.selectContact(contactHit());

    expect(navigateToMock).toHaveBeenCalledWith('/contacts/ct-1?ab=2');
  });

  it('navigates to the list pages for calendar and address book hits', () => {
    const { vm } = mountShallow();

    vm.selectCalendar();
    expect(navigateToMock).toHaveBeenLastCalledWith('/calendar');

    vm.selectAddressBook();
    expect(navigateToMock).toHaveBeenLastCalledWith('/contacts');
  });
});

// SCOPE: the palette's rendered states. Everything below drives the real DOM, so
// deleting a template branch fails a test instead of passing silently.
describe('rendered states', () => {
  it('autofocuses the input when the dialog is shown', async () => {
    const { wrapper, vm } = mountFull();
    await open(vm);

    vm.focusInput();
    await nextTick();

    expect(document.activeElement).toBe(wrapper.find('input').element);
  });

  it('shows the recent searches before anything is typed', async () => {
    const store = useSearchStore();
    store.recentSearches = ['alice', 'standup'];
    const { wrapper, vm } = mountFull();
    await open(vm);

    expect(wrapper.text()).toContain('Recent searches');
    expect(wrapper.text()).toContain('standup');
    // Picking a recent search fills the input (and therefore triggers the search).
    await wrapper.findAll('button').find((b) => b.text() === 'alice')!.trigger('click');
    expect(vm.query).toBe('alice');
  });

  it('shows the keyboard hints in the empty state', async () => {
    const { wrapper, vm } = mountFull();
    await open(vm);

    expect(wrapper.text()).toContain('Start typing to search');
    expect(wrapper.text()).toContain('Switch category');
  });

  it('shows the spinner while a search is in flight', async () => {
    const store = useSearchStore();
    store.isLoading = true;
    const { wrapper, vm } = mountFull();
    await open(vm, 'alice');

    expect(wrapper.find('[data-testid="search-loading"]').exists()).toBe(true);
  });

  it('renders event and contact rows with their metadata and highlighting', async () => {
    seed('standup', { events: [eventHit()], contacts: [contactHit()] });
    const { wrapper, vm } = mountFull();
    await open(vm, 'standup');

    const eventRow = wrapper.find('[data-testid="event-row"]');
    expect(eventRow.exists()).toBe(true);
    expect(eventRow.text()).toContain('Work'); // calendar name
    expect(eventRow.find('mark').text()).toBe('Standup'); // matching text highlighted
    expect(wrapper.find('[data-testid="contact-row"]').text()).toContain('AA'); // initials
  });

  it('marks a past event and dates an upcoming one', async () => {
    const past = eventHit({ start: '2020-01-01T09:00:00Z', end: '2020-01-01T09:30:00Z' }, 'past');
    const tomorrow = new Date(Date.now() + 86_400_000);
    const upcoming = eventHit(
      { start: tomorrow.toISOString(), end: new Date(tomorrow.getTime() + 1_800_000).toISOString() },
      'soon'
    );
    seed('standup', { events: [past, upcoming] });
    const { wrapper, vm } = mountFull();
    await open(vm, 'standup');

    const rows = wrapper.findAll('[data-testid="event-row"]');
    expect(rows[0]!.find('tag-stub').exists()).toBe(true); // "Past" Tag
    expect(rows[1]!.text()).toContain('Tomorrow');
  });

  it('caps each category at 5 rows and hands "View all" to the full results page', async () => {
    const many = Array.from({ length: 7 }, (_, i) => eventHit({ summary: `Standup ${i}` }, `ev-${i}`));
    seed('standup', { events: many });
    const { wrapper, vm } = mountFull();
    await open(vm, 'standup');

    expect(wrapper.findAll('[data-testid="event-row"]')).toHaveLength(5);
    const viewAll = wrapper.findAll('button').find((b) => b.text() === 'View all 7 events');
    expect(viewAll).toBeDefined();

    await viewAll!.trigger('click');
    expect(navigateToMock).toHaveBeenCalledWith({ path: '/search', query: { q: 'standup' } });
  });

  it('shows the no-results state for a query with no matches', async () => {
    seed('zzz', {});
    const { wrapper, vm } = mountFull();
    await open(vm, 'zzz');

    expect(wrapper.find('[data-testid="search-no-results"]').text()).toContain('No results for');
  });

  it('reports a partial failure as a banner ABOVE the results it did find', async () => {
    const store = seed('family', { calendars: [{ id: '1', uuid: 'u1', path: '/c/1', name: 'Family', color: '#000', owner_id: '1', created_at: '', updated_at: '' } as Calendar] });
    store.error = 'Contacts could not be searched — those results are missing.';
    const { wrapper, vm } = mountFull();
    await open(vm, 'family');

    // REVERT PROOF: with the error branch back in front of the results branch, the
    // Family calendar hit disappears behind the message.
    expect(wrapper.find('[data-testid="search-partial-error"]').exists()).toBe(true);
    expect(wrapper.text()).toContain('Family');
  });

  it('shows the error state when nothing at all could be searched', async () => {
    const store = seed('alice', {});
    store.error = 'server down';
    const { wrapper, vm } = mountFull();
    await open(vm, 'alice');

    expect(wrapper.find('[data-testid="search-error"]').text()).toContain('server down');
    expect(wrapper.find('[data-testid="search-no-results"]').exists()).toBe(false);
  });
});

// SCOPE: story 044's keyboard criteria — Cmd/Ctrl+K, Up/Down, Enter, Tab. Without
// these the palette is mouse-only.
describe('keyboard', () => {
  it('opens on Cmd/Ctrl+K from anywhere in the app', async () => {
    const store = useSearchStore();
    const { vm } = mountFull();
    expect(vm.visible).toBe(false);

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'k', ctrlKey: true }));
    await nextTick();

    expect(vm.visible).toBe(true);
    expect(store.loadRecentSearches).toHaveBeenCalled();
  });

  it('ignores a bare "k" (typing must not toggle the palette)', async () => {
    const { vm } = mountFull();

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'k' }));
    await nextTick();

    expect(vm.visible).toBe(false);
  });

  it('Up/Down move the highlight across category boundaries and wrap', async () => {
    seed('a', { events: [eventHit({}, 'e1'), eventHit({}, 'e2')], contacts: [contactHit('ct-1')] });
    const { wrapper, vm } = mountFull();
    await open(vm, 'alice');

    const input = wrapper.find('input');
    const selected = () => wrapper.findAll('[role="option"]').findIndex((o) => o.attributes('aria-selected') === 'true');

    expect(selected()).toBe(0);
    await input.trigger('keydown', { key: 'ArrowDown' });
    expect(selected()).toBe(1);
    await input.trigger('keydown', { key: 'ArrowDown' });
    expect(selected()).toBe(2); // into the Contacts section
    await input.trigger('keydown', { key: 'ArrowDown' });
    expect(selected()).toBe(0); // wraps
    await input.trigger('keydown', { key: 'ArrowUp' });
    expect(selected()).toBe(2);
  });

  it('Enter opens the highlighted result', async () => {
    seed('a', { events: [eventHit({}, 'e1'), eventHit({ id: 'ev-2' }, 'e2')] });
    const { wrapper, vm } = mountFull();
    await open(vm, 'alice');

    const input = wrapper.find('input');
    await input.trigger('keydown', { key: 'ArrowDown' });
    await input.trigger('keydown', { key: 'Enter' });

    expect(navigateToMock).toHaveBeenCalledWith(
      expect.objectContaining({ path: '/calendar', query: expect.objectContaining({ event: 'ev-2' }) })
    );
  });

  it('Tab jumps to the next category and Shift+Tab back', async () => {
    seed('a', { events: [eventHit({}, 'e1'), eventHit({}, 'e2')], contacts: [contactHit('ct-1')] });
    const { wrapper, vm } = mountFull();
    await open(vm, 'alice');

    const input = wrapper.find('input');
    await input.trigger('keydown', { key: 'Tab' });
    // First row of the Contacts section, not merely the next row.
    expect(vm.activeIndex).toBe(2);

    await input.trigger('keydown', { key: 'Tab', shiftKey: true });
    expect(vm.activeIndex).toBe(0);
  });

  it('Escape closes the palette', async () => {
    const { wrapper, vm } = mountFull();
    await open(vm, 'alice');

    await wrapper.find('input').trigger('keydown', { key: 'Escape' });

    expect(vm.visible).toBe(false);
  });

  it('Enter is inert when there are no rows', async () => {
    seed('zzz', {});
    const { wrapper, vm } = mountFull();
    await open(vm, 'zzz');

    await wrapper.find('input').trigger('keydown', { key: 'Enter' });

    expect(navigateToMock).not.toHaveBeenCalled();
  });

  it('address book rows are reachable by keyboard too', async () => {
    seed('a', {
      addressBooks: [
        { ID: 2, UUID: 'ab-2', UserID: 1, Path: '/ab/2', Name: 'Work', Description: '', CreatedAt: '', UpdatedAt: '' } as AddressBook,
      ],
    });
    const { wrapper, vm } = mountFull();
    await open(vm, 'alice');

    await wrapper.find('input').trigger('keydown', { key: 'Enter' });

    expect(navigateToMock).toHaveBeenCalledWith('/contacts');
  });
});
