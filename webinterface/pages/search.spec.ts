// @vitest-environment nuxt
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { mount } from '@vue/test-utils';
import { nextTick } from 'vue';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import { createTestingPinia } from '@pinia/testing';
import SearchPage from './search.vue';
import { useSearchStore } from '~/stores/search';
import type { EventHit, SearchResults } from '~/types/search';

// SCOPE: the full results page (story 044). The header palette caps every category
// at 5 rows and links here with ?q=…; this page must search that query on arrival
// and render EVERY match. REVERT PROOF: reintroduce a slice(0, 5) and the
// "renders every match" expectation of 7 rows fails.

const { routeStub, navigateToMock } = vi.hoisted(() => ({
  routeStub: { query: {} as Record<string, string> },
  navigateToMock: vi.fn(),
}));
mockNuxtImport('useRoute', () => () => routeStub as never);
mockNuxtImport('navigateTo', () => navigateToMock);

function eventHit(key: string, summary: string): EventHit {
  return {
    key,
    event: {
      id: key,
      calendar_id: 7,
      uid: `uid-${key}`,
      summary,
      start: new Date(2026, 5, 16, 9, 0, 0).toISOString(),
      end: new Date(2026, 5, 16, 9, 30, 0).toISOString(),
      all_day: false,
      is_recurring: false,
    },
    calendarId: '7',
    calendarName: 'Work',
    calendarColor: '#ff0000',
  };
}

function results(partial: Partial<SearchResults> = {}): SearchResults {
  return {
    events: [],
    contacts: [],
    calendars: [],
    addressBooks: [],
    hasMore: { events: false, contacts: false, calendars: false, addressBooks: false },
    ...partial,
  };
}

function mountPage() {
  return mount(SearchPage, {
    global: {
      plugins: [createTestingPinia()],
      stubs: {
        ProgressSpinner: true,
        Tag: true,
        NuxtLink: { template: '<a><slot /></a>' },
      },
    },
  });
}

beforeEach(() => {
  routeStub.query = {};
  navigateToMock.mockReset();
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

describe('search results page', () => {
  it('searches the query from ?q= on arrival', async () => {
    routeStub.query = { q: 'standup' };
    mountPage();
    await nextTick();

    expect(useSearchStore().search).toHaveBeenCalledWith('standup');
  });

  it('does not search a too-short ?q=', async () => {
    routeStub.query = { q: 'a' };
    mountPage();
    await nextTick();

    expect(useSearchStore().search).not.toHaveBeenCalled();
  });

  it('renders every match — no per-category cap', async () => {
    routeStub.query = { q: 'standup' };
    const wrapper = mountPage();
    const store = useSearchStore();
    store.query = 'standup';
    store.results = results({
      events: Array.from({ length: 7 }, (_, i) => eventHit(`ev-${i}`, `Standup ${i}`)),
    });
    await nextTick();

    expect(wrapper.findAll('[data-testid="page-event-row"]')).toHaveLength(7);
    expect(wrapper.text()).toContain('7 results');
  });

  it('mirrors typing into ?q= and debounces the search', async () => {
    const wrapper = mountPage();
    const store = useSearchStore();

    await wrapper.find('input').setValue('alice');
    expect(navigateToMock).toHaveBeenCalledWith(
      { path: '/search', query: { q: 'alice' } },
      { replace: true }
    );
    expect(store.search).not.toHaveBeenCalled();

    vi.advanceTimersByTime(300);
    expect(store.search).toHaveBeenCalledWith('alice');
  });

  it('reports a partial failure without hiding the results it has', async () => {
    routeStub.query = { q: 'standup' };
    const wrapper = mountPage();
    const store = useSearchStore();
    store.query = 'standup';
    store.results = results({ events: [eventHit('ev-1', 'Standup')] });
    store.error = 'Contacts could not be searched — those results are missing.';
    await nextTick();

    expect(wrapper.find('[data-testid="search-page-error"]').exists()).toBe(true);
    expect(wrapper.findAll('[data-testid="page-event-row"]')).toHaveLength(1);
  });

  it('shows a no-results state for a query with no matches', async () => {
    routeStub.query = { q: 'zzz' };
    const wrapper = mountPage();
    const store = useSearchStore();
    store.query = 'zzz';
    await nextTick();

    expect(wrapper.find('[data-testid="page-no-results"]').text()).toContain('No results for');
  });

  it('deep-links an event row into the calendar view', async () => {
    routeStub.query = { q: 'standup' };
    const wrapper = mountPage();
    const store = useSearchStore();
    store.query = 'standup';
    store.results = results({ events: [eventHit('ev-1', 'Standup')] });
    await nextTick();

    await wrapper.find('[data-testid="page-event-row"]').trigger('click');

    expect(navigateToMock).toHaveBeenCalledWith({
      path: '/calendar',
      query: { date: '2026-06-16', event: 'ev-1', cal: '7' },
    });
  });
});
