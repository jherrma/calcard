// @vitest-environment nuxt
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { enableAutoUnmount, flushPromises, mount, type DOMWrapper, type VueWrapper } from '@vue/test-utils';
import { defineComponent } from 'vue';
import { createMemoryHistory, createRouter, RouterLink, type Router, type RouteRecordRaw } from 'vue-router';
import { createTestingPinia } from '@pinia/testing';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import AppSidebar from './AppSidebar.vue';
import { useTasksStore } from '~/stores/tasks';
import type { Task, TasksSummary } from '~/types/tasks';

// SCOPE (#170 story 23): the Tasks entry in the main navigation and its overdue
// badge. The tasks store runs for real against a mocked useApi, so the specs
// assert the REQUESTS the sidebar causes (summary on mount, at local midnight)
// and the badge the store's summary produces.
//
// Highlighting: NuxtLink's `custom` slot gets isActive/isExactActive from
// vue-router's useLink, so NuxtLink is replaced by vue-router's own RouterLink
// on a memory router built from the APP's route table (useNuxtApp().$router).
// That keeps the real record matching (flat page routes) without running the
// app's auth middleware.

const { apiMock } = vi.hoisted(() => ({ apiMock: vi.fn() }));
mockNuxtImport('useApi', () => () => apiMock);
// The factory only builds the getter; `router` exists once a test mounts.
mockNuxtImport('useRoute', () => () => router!.currentRoute.value);

let router: Router | null = null;

// A sidebar left mounted would keep its midnight timer armed into the next test.
enableAutoUnmount(afterEach);

interface ApiOptions {
  method?: string;
  query?: Record<string, string>;
}
type Handler = (options: ApiOptions) => unknown;

const SUMMARY: TasksSummary = {
  open: 14,
  overdue: 3,
  due_today: 2,
  completed: 5,
  by_calendar: { errands: { open: 14, overdue: 3, completed: 0 } },
};

/** Answers by `METHOD url`; anything else fails loudly. */
function serve(handlers: Record<string, Handler> = {}) {
  const all: Record<string, Handler> = { 'GET /api/v1/tasks/summary': () => SUMMARY, ...handlers };
  apiMock.mockImplementation(async (url: string, options: ApiOptions = {}) => {
    const key = `${options.method ?? 'GET'} ${url}`;
    const handler = all[key];
    if (!handler) throw new Error(`unexpected request ${key}`);
    return handler(options);
  });
}

const requests = () =>
  apiMock.mock.calls.map(([url, options]) => `${(options as ApiOptions | undefined)?.method ?? 'GET'} ${url}`);
/** The `as_of` of every summary request, in order. */
const summaryAsOf = () =>
  apiMock.mock.calls
    .filter(([url]) => url === '/api/v1/tasks/summary')
    .map(([, options]) => (options as ApiOptions).query?.as_of);

function task(id: string): Task {
  return {
    id,
    calendar_id: 1,
    calendar_uuid: 'errands',
    uid: `${id}@calcard.io`,
    summary: `Task ${id}`,
    description: '',
    status: 'needs-action',
    completed: false,
    completed_at: null,
    percent_complete: null,
    priority: null,
    due: '2026-11-01T09:00:00Z',
    due_date: null,
    due_all_day: false,
    start: null,
    start_date: null,
    is_recurring: false,
    rrule: null,
    etag: `etag-${id}`,
    created_at: '2026-10-01T10:00:00Z',
    updated_at: '2026-10-01T10:00:00Z',
  };
}

const Empty = defineComponent({ render: () => null });

/** The app's page routes (paths, names, nesting) with inert components. */
function appRoutes(records: readonly RouteRecordRaw[] = useNuxtApp().$router.options.routes): RouteRecordRaw[] {
  return records.map(
    (r) =>
      ({
        path: r.path,
        name: r.name,
        component: Empty,
        ...(r.children ? { children: appRoutes(r.children) } : {}),
      }) as RouteRecordRaw,
  );
}

async function mountSidebar(path = '/calendar') {
  router = createRouter({ history: createMemoryHistory(), routes: appRoutes() });
  await router.push(path);
  await router.isReady();
  const pinia = createTestingPinia({ stubActions: false });
  const tasksStore = useTasksStore();
  const wrapper = mount(AppSidebar, {
    props: { open: true },
    global: { plugins: [pinia, router], stubs: { NuxtLink: RouterLink } },
  });
  await flushPromises();
  return { wrapper, tasksStore };
}

const navLinks = (wrapper: VueWrapper) => wrapper.findAll('nav a');
const squash = (text: string) => text.replace(/\s+/g, ' ').trim();
/** Visible label of a nav link (its first text, without the badge). */
const labelOf = (link: DOMWrapper<Element>) => squash(link.element.childNodes[1]?.textContent ?? '');
const linkTo = (wrapper: VueWrapper, label: string) => {
  const link = navLinks(wrapper).find((a) => labelOf(a) === label);
  if (!link) throw new Error(`no nav link "${label}"`);
  return link;
};
/** The badge pills in the navigation (the existing `rounded-full` span). */
const badges = (wrapper: VueWrapper) => wrapper.findAll('nav a > span.rounded-full');
const highlighted = (wrapper: VueWrapper) =>
  navLinks(wrapper)
    .filter((a) => a.classes().includes('bg-primary-50'))
    .map(labelOf);

beforeEach(() => {
  apiMock.mockReset();
  serve();
});

describe('AppSidebar — Tasks entry', () => {
  it('lists Dashboard, Calendar, Tasks, Contacts in that order, Tasks linking to /tasks with the check-square icon', async () => {
    const { wrapper } = await mountSidebar();

    expect(navLinks(wrapper).map(labelOf)).toEqual(['Dashboard', 'Calendar', 'Tasks', 'Contacts']);
    expect(navLinks(wrapper).map((a) => a.attributes('href'))).toEqual(['/', '/calendar', '/tasks', '/contacts']);
    expect(linkTo(wrapper, 'Tasks').find('i').classes()).toEqual(['pi', 'pi-check-square', 'text-lg']);
    // Settings stays the single link of the bottom section.
    expect(wrapper.findAll('div.border-t a').map((a) => [squash(a.text()), a.attributes('href')])).toEqual([
      ['Settings', '/settings'],
    ]);
  });

  it.each(['/tasks', '/tasks?list=errands'])('highlights Tasks, and only Tasks, on %s', async (path) => {
    const { wrapper } = await mountSidebar(path);

    expect(highlighted(wrapper)).toEqual(['Tasks']);
    expect(linkTo(wrapper, 'Tasks').classes()).toEqual(
      expect.arrayContaining(['bg-primary-50', 'dark:bg-primary-900/20', 'text-primary-700', 'dark:text-primary-300']),
    );
  });

  it.each([
    ['/', 'Dashboard'],
    ['/calendar', 'Calendar'],
    ['/contacts', 'Contacts'],
  ])('leaves Tasks plain on %s, where %s is highlighted', async (path, label) => {
    const { wrapper } = await mountSidebar(path);

    expect(highlighted(wrapper)).toEqual([label]);
    expect(linkTo(wrapper, 'Tasks').classes()).toContain('hover:bg-surface-100');
  });
});

describe('AppSidebar — overdue badge', () => {
  it('shows the overdue count on the Tasks entry, and says what it counts', async () => {
    const { wrapper } = await mountSidebar();

    const tasks = linkTo(wrapper, 'Tasks');
    const pill = tasks.find('span.rounded-full');
    expect(badges(wrapper)).toHaveLength(1);
    expect(pill.exists()).toBe(true);
    // The number is what shows; the word is for screen readers ("Tasks 3 overdue") and the hover title.
    expect(pill.element.firstChild?.textContent?.trim()).toBe('3');
    expect(pill.find('.sr-only').text()).toBe('overdue');
    expect(squash(pill.element.textContent ?? '')).toBe('3 overdue');
    expect(pill.attributes('title')).toBe('3 overdue');
    expect(squash(tasks.element.textContent ?? '')).toBe('Tasks 3 overdue');
  });

  it('shows no badge when nothing is overdue', async () => {
    serve({ 'GET /api/v1/tasks/summary': () => ({ ...SUMMARY, overdue: 0 }) });
    const { wrapper } = await mountSidebar();

    expect(badges(wrapper)).toHaveLength(0);
    expect(squash(linkTo(wrapper, 'Tasks').text())).toBe('Tasks');
  });

  it('shows no badge while the summary is loading, nor when it fails', async () => {
    let fail!: (e: Error) => void;
    serve({
      'GET /api/v1/tasks/summary': () =>
        new Promise((_, reject) => {
          fail = reject;
        }),
    });
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const { wrapper, tasksStore } = await mountSidebar();

    expect(badges(wrapper)).toHaveLength(0);
    fail(new Error('offline'));
    await flushPromises();

    expect(tasksStore.summary).toBeNull();
    expect(badges(wrapper)).toHaveLength(0);
    warn.mockRestore();
  });

  it('follows the summary a task mutation refetches: down to 2, then gone at 0', async () => {
    const overdue = [3, 2, 0];
    serve({
      'GET /api/v1/tasks/summary': () => ({ ...SUMMARY, overdue: overdue.shift() ?? 0 }),
      'POST /api/v1/calendars/errands/tasks/a/complete': () => ({ ...task('a'), status: 'completed', completed: true }),
      'DELETE /api/v1/calendars/errands/tasks/b': () => undefined,
    });
    const { wrapper, tasksStore } = await mountSidebar();
    tasksStore.tasks = [task('a'), task('b')];
    expect(badges(wrapper).map((b) => b.element.firstChild?.textContent?.trim())).toEqual(['3']);

    await tasksStore.toggleComplete(task('a'));
    await flushPromises();
    expect(badges(wrapper).map((b) => b.element.firstChild?.textContent?.trim())).toEqual(['2']);

    await tasksStore.removeTask(task('b'));
    await flushPromises();
    expect(badges(wrapper)).toHaveLength(0);
    // The sidebar asked once (mount); each mutation refetched once.
    expect(requests().filter((r) => r === 'GET /api/v1/tasks/summary')).toHaveLength(3);
  });
});

describe('AppSidebar — summary on mount', () => {
  it.each(['/', '/calendar', '/contacts', '/tasks', '/search'])(
    'asks for the summary once on mount on %s, and for nothing else',
    async (path) => {
      await mountSidebar(path);

      expect(requests()).toEqual(['GET /api/v1/tasks/summary']);
      // as_of travels in ofetch's query option (story 21), not in the URL.
      expect(apiMock.mock.calls[0]![1]).toEqual({ query: { as_of: expect.any(String) } });
    },
  );

  it('asks again when it is mounted again (another layout)', async () => {
    const first = await mountSidebar('/calendar');
    first.wrapper.unmount();
    await mountSidebar('/calendar');

    expect(requests()).toEqual(['GET /api/v1/tasks/summary', 'GET /api/v1/tasks/summary']);
  });
});

describe('AppSidebar — midnight refresh (Europe/Berlin)', () => {
  let previousTz: string | undefined;
  beforeAll(() => {
    previousTz = process.env.TZ;
    process.env.TZ = 'Europe/Berlin';
  });
  afterAll(() => {
    if (previousTz === undefined) delete process.env.TZ;
    else process.env.TZ = previousTz;
  });

  beforeEach(() => {
    // setImmediate stays real: flushPromises runs on it.
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'Date'] });
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  /** A local wall-clock time in the pinned zone (built inside the test, after the zone is set). */
  const local = (month: number, day: number, h: number, m = 0, s = 0) => new Date(2026, month - 1, day, h, m, s);
  const MINUTE = 60_000;
  const HOUR = 60 * MINUTE;

  it('refetches at local midnight and arms itself for the next one', async () => {
    vi.setSystemTime(local(11, 3, 23, 59, 30));
    // The zone really took effect (+01:00 in November), or this tests nothing.
    expect(new Date().getTimezoneOffset()).toBe(-60);
    await mountSidebar();
    expect(summaryAsOf()).toEqual(['2026-11-03T23:59:30+01:00']);

    await vi.advanceTimersByTimeAsync(30_000 - 1);
    await flushPromises();
    expect(summaryAsOf()).toHaveLength(1);

    await vi.advanceTimersByTimeAsync(1);
    await flushPromises();
    expect(summaryAsOf()).toEqual(['2026-11-03T23:59:30+01:00', '2026-11-04T00:00:00+01:00']);

    await vi.advanceTimersByTimeAsync(24 * HOUR - 1);
    await flushPromises();
    expect(summaryAsOf()).toHaveLength(2);

    await vi.advanceTimersByTimeAsync(1);
    await flushPromises();
    expect(summaryAsOf()).toEqual([
      '2026-11-03T23:59:30+01:00',
      '2026-11-04T00:00:00+01:00',
      '2026-11-05T00:00:00+01:00',
    ]);
    expect(requests().every((r) => r === 'GET /api/v1/tasks/summary')).toBe(true);
  });

  it('waits the whole 25 hours of the day summer time ends', async () => {
    // 25 October 2026: clocks go back at 03:00, so that day lasts 25 hours.
    vi.setSystemTime(local(10, 24, 22));
    expect(new Date().getTimezoneOffset()).toBe(-120);
    await mountSidebar();

    await vi.advanceTimersByTimeAsync(2 * HOUR);
    await flushPromises();
    expect(summaryAsOf().slice(1)).toEqual(['2026-10-25T00:00:00+02:00']);

    // 24 hours later it is only 23:00 on the 25th.
    await vi.advanceTimersByTimeAsync(24 * HOUR);
    await flushPromises();
    expect(summaryAsOf()).toHaveLength(2);

    await vi.advanceTimersByTimeAsync(HOUR);
    await flushPromises();
    expect(summaryAsOf().slice(1)).toEqual(['2026-10-25T00:00:00+02:00', '2026-10-26T00:00:00+01:00']);
  });

  it('does not ask for yesterday again when the clock was set back: it waits for the real midnight', async () => {
    vi.setSystemTime(local(11, 3, 23));
    await mountSidebar();
    // The wall clock is corrected 10 minutes back; the armed timer still fires an hour after arming.
    vi.setSystemTime(local(11, 3, 22, 50));

    await vi.advanceTimersByTimeAsync(HOUR);
    await flushPromises();
    expect(summaryAsOf()).toHaveLength(1); // 23:50, still the 3rd

    await vi.advanceTimersByTimeAsync(10 * MINUTE);
    await flushPromises();
    expect(summaryAsOf().slice(1)).toEqual(['2026-11-04T00:00:00+01:00']);
  });

  it('stops the timer when the sidebar goes away', async () => {
    vi.setSystemTime(local(11, 3, 23));
    const { wrapper } = await mountSidebar();
    expect(vi.getTimerCount()).toBe(1);

    wrapper.unmount();
    expect(vi.getTimerCount()).toBe(0);
    await vi.advanceTimersByTimeAsync(2 * HOUR);
    await flushPromises();

    expect(summaryAsOf()).toHaveLength(1);
  });
});

describe('AppSidebar — refresh when a loaded task turns overdue, and when the tab returns (MN11, Europe/Berlin)', () => {
  let previousTz: string | undefined;
  beforeAll(() => {
    previousTz = process.env.TZ;
    process.env.TZ = 'Europe/Berlin';
  });
  afterAll(() => {
    if (previousTz === undefined) delete process.env.TZ;
    else process.env.TZ = previousTz;
  });

  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'Date'] });
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  const local = (month: number, day: number, h: number, m = 0, s = 0) => new Date(2026, month - 1, day, h, m, s);
  const MINUTE = 60_000;

  /** A timed task due at `iso` (UTC). */
  const dueAt = (id: string, iso: string): Task => ({ ...task(id), due: iso });

  it('refetches the summary one second after the next timed due instant of a loaded task', async () => {
    vi.setSystemTime(local(11, 3, 16, 0)); // 15:00Z
    const { tasksStore } = await mountSidebar();
    expect(summaryAsOf()).toHaveLength(1);

    tasksStore.tasks = [dueAt('a', '2026-11-03T16:00:00Z'), dueAt('later', '2026-11-03T18:00:00Z')]; // 17:00 and 19:00 local
    await flushPromises();
    await vi.advanceTimersByTimeAsync(60 * MINUTE + 999);
    await flushPromises();
    expect(summaryAsOf()).toHaveLength(1);

    await vi.advanceTimersByTimeAsync(1);
    await flushPromises();
    expect(summaryAsOf()).toEqual(['2026-11-03T16:00:00+01:00', '2026-11-03T17:00:01+01:00']);

    // Armed again for the next one.
    await vi.advanceTimersByTimeAsync(2 * 60 * MINUTE);
    await flushPromises();
    expect(summaryAsOf().at(-1)).toBe('2026-11-03T19:00:01+01:00');
  });

  it('re-arms when a task that is due sooner is added later', async () => {
    vi.setSystemTime(local(11, 3, 9, 0));
    const { tasksStore } = await mountSidebar();
    tasksStore.tasks = [dueAt('far', '2026-11-03T20:00:00Z')];
    await flushPromises();
    tasksStore.tasks = [...tasksStore.tasks, dueAt('soon', '2026-11-03T08:30:00Z')]; // 09:30 local

    await vi.advanceTimersByTimeAsync(30 * MINUTE + 1000);
    await flushPromises();
    expect(summaryAsOf().at(-1)).toBe('2026-11-03T09:30:01+01:00');
  });

  it('refetches when the tab becomes visible again, and not while it is hidden', async () => {
    vi.setSystemTime(local(11, 3, 12, 0));
    await mountSidebar();
    expect(summaryAsOf()).toHaveLength(1);

    const visibility = vi.spyOn(document, 'visibilityState', 'get');
    visibility.mockReturnValue('hidden');
    document.dispatchEvent(new Event('visibilitychange'));
    await flushPromises();
    expect(summaryAsOf()).toHaveLength(1);

    visibility.mockReturnValue('visible');
    document.dispatchEvent(new Event('visibilitychange'));
    await flushPromises();
    expect(summaryAsOf()).toHaveLength(2);
    visibility.mockRestore();
  });

  it('stops listening and clears its timer when the sidebar goes away', async () => {
    vi.setSystemTime(local(11, 3, 12, 0));
    const { wrapper } = await mountSidebar();
    wrapper.unmount();

    expect(vi.getTimerCount()).toBe(0);
    document.dispatchEvent(new Event('visibilitychange'));
    await flushPromises();
    expect(summaryAsOf()).toHaveLength(1);
  });
});
