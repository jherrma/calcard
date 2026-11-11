// @vitest-environment nuxt
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { mount } from '@vue/test-utils';
import { defineComponent, nextTick } from 'vue';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import { createTestingPinia } from '@pinia/testing';
import SearchPage from './search.vue';
import { useSearchStore } from '~/stores/search';
import type { Calendar } from '~/types/calendar';
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

// SCOPE (#170, story 28): the calendars group lists every calendar, task lists
// included (each hit carries `kind`). A task list opens in the Tasks section as
// /tasks?list=<uuid>; event (`events`) and mixed (`both`) calendars open the
// Calendar section as /calendar?cal=<numeric id>.
describe('calendars group routing (#170)', () => {
  // Keeps the `to` prop readable (the default stub above drops it).
  const NuxtLinkStub = defineComponent({
    name: 'NuxtLinkStub',
    props: { to: { type: [String, Object], required: true } },
    template: '<a><slot /></a>',
  });

  function cal(id: number | string, uuid: string, name: string, kind?: Calendar['kind'], extra: Partial<Calendar> = {}): Calendar {
    return {
      id: id as string,
      uuid,
      path: `/c/${id}`,
      name,
      color: '#123456',
      created_at: '',
      updated_at: '',
      kind,
      ...extra,
    };
  }

  function mountWithCalendars(calendars: Calendar[]) {
    routeStub.query = { q: 'team' };
    const wrapper = mount(SearchPage, {
      global: {
        plugins: [createTestingPinia()],
        stubs: { ProgressSpinner: true, Tag: true, NuxtLink: NuxtLinkStub },
      },
    });
    const store = useSearchStore();
    store.query = 'team';
    store.results = results({ calendars });
    return wrapper;
  }

  async function rows(calendars: Calendar[]) {
    const wrapper = mountWithCalendars(calendars);
    await nextTick();
    const links = wrapper.findAllComponents(NuxtLinkStub);
    expect(links.map((l) => l.attributes('data-testid'))).toEqual(calendars.map(() => 'page-calendar-row'));
    return links;
  }

  it('links a task list to /tasks?list=<uuid> and every other calendar to /calendar?cal=<id>', async () => {
    const links = await rows([
      cal(5, 'cal-team', 'Team', 'events'),
      cal(6, 'list-team', 'Team chores', 'tasks'),
      cal(7, 'cal-mixed', 'Team (old)', 'both'),
      cal(8, 'cal-legacy', 'Team legacy'),
    ]);

    expect(links.map((l) => l.props('to'))).toEqual([
      { path: '/calendar', query: { cal: '5' } },
      { path: '/tasks', query: { list: 'list-team' } },
      { path: '/calendar', query: { cal: '7' } },
      { path: '/calendar', query: { cal: '8' } },
    ]);
  });

  it('uses the UUID for a task list and the numeric id (as sent on the wire) for a calendar', async () => {
    // GET /search sends `id` as a JSON number.
    const links = await rows([
      cal(42 as unknown as string, 'cal-uuid-42', 'Team', 'events'),
      cal(43 as unknown as string, 'list-uuid-43', 'Team chores', 'tasks'),
    ]);

    expect(links[0]!.props('to')).toEqual({ path: '/calendar', query: { cal: '42' } });
    expect(links[1]!.props('to')).toEqual({ path: '/tasks', query: { list: 'list-uuid-43' } });
  });

  it('links a task list shared with the user to the Tasks section too', async () => {
    const links = await rows([
      cal(9, 'list-shared', 'Team chores', 'tasks', { shared: true, permission: 'read-only' }),
    ]);

    expect(links[0]!.props('to')).toEqual({ path: '/tasks', query: { list: 'list-shared' } });
  });

  it('says "Task list" on a task-list row only', async () => {
    const links = await rows([
      cal(5, 'cal-team', 'Team', 'events'),
      cal(6, 'list-team', 'Team chores', 'tasks'),
      cal(7, 'cal-mixed', 'Team (old)', 'both'),
    ]);

    const hints = links.map((l) => l.find('[data-testid="task-list-hint"]'));
    expect(hints.map((h) => h.exists())).toEqual([false, true, false]);
    expect(hints[1]!.text()).toBe('Task list');
    expect(hints[1]!.classes()).toEqual(
      expect.arrayContaining(['text-xs', 'text-surface-500', 'dark:text-surface-400'])
    );
  });
});
