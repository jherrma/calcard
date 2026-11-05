// @vitest-environment nuxt
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { mount, type VueWrapper } from '@vue/test-utils';
import { nextTick } from 'vue';
import { createTestingPinia } from '@pinia/testing';
import DashboardPage from './index.vue';
import QuickStatsCard from '~/components/dashboard/QuickStatsCard.vue';
import MiniCalendarCard from '~/components/dashboard/MiniCalendarCard.vue';
import EventCreateDialog from '~/components/calendar/EventCreateDialog.vue';
import { useCalendarStore } from '~/stores/calendars';
import { useDashboardStore } from '~/stores/dashboard';
import { useTasksStore } from '~/stores/tasks';
import type { Calendar } from '~/types/calendar';
import type { TasksSummary } from '~/types/tasks';

// SCOPE (#170): the dashboard's calendar figures. GET /calendars returns task
// lists (`kind: 'tasks'`) next to the calendars, and the store keeps them in
// `calendars` for the tasks section. The "Calendars" stat and the mini
// calendar's "N calendars" line count event calendars only — a task list is not
// a calendar to the user (the Open tasks figure is story 28's) — and the "New
// Event" quick action needs a writable EVENT calendar.

interface PageVm {
  canCreateEvent: boolean;
}

function calendar(id: number, extra: Partial<Calendar> = {}): Calendar {
  return {
    id: String(id),
    uuid: `cal-uuid-${id}`,
    path: `${id}.ics`,
    name: `Cal ${id}`,
    color: '#123456',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...extra,
  };
}

async function mountPage(calendars: Calendar[]) {
  // stubActions: true — mount calls dashboardStore.refresh(), which would
  // otherwise refetch the calendar list over the one seeded here.
  const pinia = createTestingPinia({ stubActions: true });
  useCalendarStore().calendars = calendars;
  const wrapper = mount(DashboardPage, { shallow: true, global: { plugins: [pinia] } });
  await nextTick();
  return { wrapper, vm: wrapper.vm as unknown as PageVm };
}

let mounted: Awaited<ReturnType<typeof mountPage>>['wrapper'] | null = null;

beforeEach(() => {
  vi.stubGlobal('$fetch', vi.fn());
});

afterEach(() => {
  // The page arms a clock and a refresh interval on mount.
  mounted?.unmount();
  mounted = null;
  vi.unstubAllGlobals();
});

describe('dashboard calendar figures never count a task list (#170)', () => {
  it('counts event, mixed and shared calendars but not task lists', async () => {
    const { wrapper } = await mountPage([
      calendar(1, { name: 'Personal', kind: 'events' }),
      calendar(2, { name: 'Tasks', kind: 'tasks' }),
      calendar(3, { name: 'Old mixed', kind: 'both' }),
      calendar(4, { name: 'Team tasks', kind: 'tasks', shared: true, permission: 'read-write' }),
      calendar(5, { name: 'Team', kind: 'events', shared: true, permission: 'read-only' }),
    ]);
    mounted = wrapper;

    // REVERT PROOF: `calendars.length` (the pre-#170 binding) gives 5 for both.
    expect(wrapper.findComponent(QuickStatsCard).props('calendars')).toBe(3);
    expect(wrapper.findComponent(MiniCalendarCard).props('calendarCount')).toBe(3);
  });

  it('shows 0 calendars and no New Event for a user who only has task lists', async () => {
    const { wrapper, vm } = await mountPage([
      calendar(1, { name: 'Tasks', kind: 'tasks' }),
      calendar(2, { name: 'Shopping', kind: 'tasks' }),
    ]);
    mounted = wrapper;

    expect(wrapper.findComponent(QuickStatsCard).props('calendars')).toBe(0);
    expect(wrapper.findComponent(MiniCalendarCard).props('calendarCount')).toBe(0);
    // Nothing to create an event in: the quick action is off and its dialog
    // (whose calendar select reads writableCalendars) is not mounted.
    expect(vm.canCreateEvent).toBe(false);
    expect(wrapper.findComponent(EventCreateDialog).exists()).toBe(false);
  });

  it('keeps New Event for an event calendar next to a task list', async () => {
    const { wrapper, vm } = await mountPage([
      calendar(1, { name: 'Tasks', kind: 'tasks' }),
      calendar(2, { name: 'Personal', kind: 'events' }),
    ]);
    mounted = wrapper;

    expect(vm.canCreateEvent).toBe(true);
    expect(wrapper.findComponent(EventCreateDialog).exists()).toBe(true);
    expect(wrapper.findComponent(QuickStatsCard).props('calendars')).toBe(1);
  });
});

// SCOPE (#170, story 28): the "Open tasks" figure is the task summary's `open`.
// AppSidebar asks for the summary only when IT mounts, and going from /calendar to
// / does not remount it, so the dashboard asks on its own mount, and again with
// every refresh (the Refresh button and the 5-minute timer).
describe('dashboard "Open tasks" figure (#170)', () => {
  function summary(open: number): TasksSummary {
    return { open, overdue: 2, due_today: 1, completed: 30, by_calendar: {} };
  }

  async function mountWithTasks(seed: TasksSummary | null, fetchSummary?: () => Promise<void>) {
    const pinia = createTestingPinia({ stubActions: true });
    useCalendarStore().calendars = [calendar(1, { kind: 'events' }), calendar(2, { kind: 'tasks' })];
    const tasks = useTasksStore();
    tasks.summary = seed;
    if (fetchSummary) vi.mocked(tasks.fetchSummary).mockImplementation(fetchSummary);
    const wrapper = mount(DashboardPage, { shallow: true, global: { plugins: [pinia] } });
    mounted = wrapper;
    await nextTick();
    return { wrapper, tasks, dashboard: useDashboardStore() };
  }

  function statsProps(wrapper: Awaited<ReturnType<typeof mountWithTasks>>['wrapper']) {
    return wrapper.findComponent(QuickStatsCard).props();
  }

  it('passes summary.open to QuickStatsCard and follows the summary', async () => {
    const { wrapper, tasks } = await mountWithTasks(summary(7));

    // REVERT PROOF: binding overdue (2), due_today (1) or completed (30) fails here.
    expect(statsProps(wrapper).openTasks).toBe(7);

    // A task mutation elsewhere refetches the summary (story 21); the figure follows.
    tasks.summary = summary(6);
    await nextTick();
    expect(statsProps(wrapper).openTasks).toBe(6);

    tasks.summary = summary(0);
    await nextTick();
    expect(statsProps(wrapper).openTasks).toBe(0);
  });

  it('asks for the summary on its own mount', async () => {
    const { tasks, dashboard } = await mountWithTasks(summary(7));

    expect(tasks.fetchSummary).toHaveBeenCalledTimes(1);
    expect(dashboard.refresh).toHaveBeenCalledTimes(1);
  });

  it('passes null (never 0) while there is no summary, with the loading flag while it is asked for', async () => {
    let answer!: () => void;
    const { wrapper, tasks } = await mountWithTasks(
      null,
      () => new Promise<void>((resolve) => { answer = resolve; })
    );

    expect(statsProps(wrapper).openTasks).toBeNull();
    expect(statsProps(wrapper).openTasksLoading).toBe(true);

    tasks.summary = summary(3);
    answer();
    await flush();

    expect(statsProps(wrapper).openTasks).toBe(3);
    expect(statsProps(wrapper).openTasksLoading).toBe(false);
  });

  it('drops the loading flag when the summary request fails (the store keeps no count)', async () => {
    let answer!: () => void;
    const { wrapper } = await mountWithTasks(
      null,
      // fetchSummary never rejects; a failure resolves with the summary untouched.
      () => new Promise<void>((resolve) => { answer = resolve; })
    );

    answer();
    await flush();

    expect(statsProps(wrapper).openTasks).toBeNull();
    expect(statsProps(wrapper).openTasksLoading).toBe(false);
  });

  it('keeps the loading flag until the last of two overlapping requests answers', async () => {
    const answers: Array<() => void> = [];
    const { wrapper } = await mountWithTasks(
      null,
      () => new Promise<void>((resolve) => { answers.push(resolve); })
    );
    await refreshButton(wrapper).vm.$emit('click');
    expect(answers).toHaveLength(2);

    answers[0]!();
    await flush();
    expect(statsProps(wrapper).openTasksLoading).toBe(true);

    answers[1]!();
    await flush();
    expect(statsProps(wrapper).openTasksLoading).toBe(false);
  });

  it('the Refresh button re-reads the summary along with the dashboard', async () => {
    const { wrapper, tasks, dashboard } = await mountWithTasks(summary(7));

    await refreshButton(wrapper).vm.$emit('click');

    expect(tasks.fetchSummary).toHaveBeenCalledTimes(2);
    expect(dashboard.refresh).toHaveBeenCalledTimes(2);
  });

  it('the 5-minute refresh re-reads the summary too', async () => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] });
    try {
      const { tasks, dashboard } = await mountWithTasks(summary(7));

      vi.advanceTimersByTime(5 * 60 * 1000 - 1);
      expect(tasks.fetchSummary).toHaveBeenCalledTimes(1);

      vi.advanceTimersByTime(1);
      expect(tasks.fetchSummary).toHaveBeenCalledTimes(2);
      expect(dashboard.refresh).toHaveBeenCalledTimes(2);
    } finally {
      vi.useRealTimers();
    }
  });

  it('creating an event refreshes the dashboard only, not the task summary', async () => {
    const { wrapper, tasks, dashboard } = await mountWithTasks(summary(7));

    wrapper.findComponent(EventCreateDialog).vm.$emit('created');
    await nextTick();

    expect(dashboard.refresh).toHaveBeenCalledTimes(2);
    expect(tasks.fetchSummary).toHaveBeenCalledTimes(1);
  });

  it('leaves the other figures as they were', async () => {
    const { wrapper } = await mountWithTasks(summary(7));

    const props = statsProps(wrapper);
    // Event calendars only (story 18); the task list is counted by Open tasks.
    expect(props.calendars).toBe(1);
    expect(Object.keys(props).sort()).toEqual(
      ['addressBooks', 'calendars', 'contacts', 'eventsThisMonth', 'loading', 'openTasks', 'openTasksLoading'].sort()
    );
  });
});

function refreshButton(wrapper: VueWrapper) {
  const button = wrapper
    .findAllComponents({ name: 'Button' })
    .find((b) => b.attributes('label') === 'Refresh');
  if (!button) throw new Error('Refresh button not found');
  return button;
}

async function flush() {
  for (let i = 0; i < 5; i++) await Promise.resolve();
  await nextTick();
}
