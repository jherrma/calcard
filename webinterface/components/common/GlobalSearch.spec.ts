// @vitest-environment nuxt
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { mount } from '@vue/test-utils';
import { nextTick } from 'vue';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import { createTestingPinia } from '@pinia/testing';
import type { TestingPinia } from '@pinia/testing';
import GlobalSearch from './GlobalSearch.vue';
import { useSearchStore } from '~/stores/search';
import type { ContactHit, EventHit } from '~/types/search';

// GlobalSearch is shallow-mounted (the PrimeVue Dialog and every result row are
// stubbed) so these specs exercise the wiring we own: the 300ms debounce, the
// close/reset cycle, and the route each result type navigates to.

const { navigateToMock } = vi.hoisted(() => ({ navigateToMock: vi.fn() }));
mockNuxtImport('navigateTo', () => navigateToMock);

interface SearchVm {
  query: string;
  visible: boolean;
  selectEvent: (hit: EventHit) => void;
  selectContact: (hit: ContactHit) => void;
  selectCalendar: () => void;
  selectAddressBook: () => void;
}

let pinia: TestingPinia;

function mountSearch() {
  const wrapper = mount(GlobalSearch, { shallow: true, global: { plugins: [pinia] } });
  return { wrapper, vm: wrapper.vm as unknown as SearchVm };
}

function eventHit(): EventHit {
  // Built from local date parts so the yyyy-MM-dd link param is timezone-stable.
  const start = new Date(2026, 5, 16, 9, 0, 0);
  return {
    key: 'ev-1::',
    event: {
      id: 'ev-1',
      calendar_id: 7,
      uid: 'uid-1',
      summary: 'Team Standup',
      start: start.toISOString(),
      end: new Date(2026, 5, 16, 9, 30, 0).toISOString(),
      all_day: false,
      is_recurring: false,
    },
    calendarId: '7',
    calendarName: 'Work',
    calendarColor: '#ff0000',
  };
}

beforeEach(() => {
  // stubActions (the @pinia/testing default) keeps these specs about delegation:
  // the store's own behaviour is covered in stores/search.spec.ts.
  pinia = createTestingPinia();
  navigateToMock.mockReset();
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

describe('debounced search', () => {
  it('waits 300ms and then searches once', async () => {
    const store = useSearchStore();
    const { vm } = mountSearch();

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
    const { vm } = mountSearch();

    vm.query = 'a';
    await nextTick();

    expect(store.search).toHaveBeenCalledWith('');
  });
});

describe('closing', () => {
  it('drops the query and resets the store', async () => {
    const store = useSearchStore();
    const { vm } = mountSearch();

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
    const { vm } = mountSearch();
    vm.query = 'standup';
    await nextTick();

    vm.selectEvent(eventHit());

    expect(store.rememberSearch).toHaveBeenCalledWith('standup');
    expect(navigateToMock).toHaveBeenCalledWith({
      path: '/calendar',
      query: { date: '2026-06-16', event: 'ev-1', cal: '7' },
    });
    expect(vm.visible).toBe(false);
  });

  it('carries recurrence_id so a single occurrence opens, not the series', async () => {
    const { vm } = mountSearch();
    const hit = eventHit();
    hit.event.recurrence_id = '2026-06-16T09:00:00Z';

    vm.selectEvent(hit);

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
    const { vm } = mountSearch();

    vm.selectContact({
      contact: {
        id: 'ct-1',
        addressbook_id: '2',
        uid: 'uid-ct-1',
        formatted_name: 'Alice Adams',
        created_at: '',
        updated_at: '',
      },
      addressBookId: '2',
      addressBookName: 'Work',
    });

    expect(navigateToMock).toHaveBeenCalledWith('/contacts/ct-1?ab=2');
  });

  it('navigates to the list pages for calendar and address book hits', () => {
    const { vm } = mountSearch();

    vm.selectCalendar();
    expect(navigateToMock).toHaveBeenLastCalledWith('/calendar');

    vm.selectAddressBook();
    expect(navigateToMock).toHaveBeenLastCalledWith('/contacts');
  });
});
