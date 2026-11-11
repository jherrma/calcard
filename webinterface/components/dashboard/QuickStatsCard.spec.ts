// @vitest-environment nuxt
import { describe, it, expect } from 'vitest';
import { mount } from '@vue/test-utils';
import QuickStatsCard from './QuickStatsCard.vue';

// SCOPE: the dashboard "Overview" figures, and the "Open tasks" figure #170 adds
// (the task summary's `open`). The other four figures and their loading pulse must
// stay exactly as they were; "Open tasks" has its own source, so it has its own
// loading rule and never pretends to be 0 when the summary is missing.

type Props = InstanceType<typeof QuickStatsCard>['$props'];

const BASE = {
  calendars: 3,
  eventsThisMonth: 12,
  addressBooks: 2,
  contacts: 140,
  openTasks: 7,
} as const;

function mountCard(props: Partial<Props> = {}) {
  return mount(QuickStatsCard, { props: { ...BASE, ...props } });
}

function tile(wrapper: ReturnType<typeof mountCard>, key: string) {
  return wrapper.get(`[data-stat="${key}"]`);
}

/** The figure line of a tile: its first `<p>` (the pulse or the number). */
function figure(wrapper: ReturnType<typeof mountCard>, key: string) {
  return tile(wrapper, key).findAll('p')[0]!;
}

describe('QuickStatsCard', () => {
  it('shows the five figures, one row per section in the sidebar order', () => {
    const wrapper = mountCard();

    const tiles = wrapper.findAll('[data-stat]');
    expect(tiles.map((t) => t.attributes('data-stat'))).toEqual([
      'calendars',
      'events',
      'open-tasks',
      'address-books',
      'contacts',
    ]);
    expect(tiles.map((t) => t.findAll('p').map((p) => p.text()))).toEqual([
      ['3', 'Calendars'],
      ['12', 'Events this month'],
      ['7', 'Open tasks'],
      ['2', 'Address books'],
      ['140', 'Contacts'],
    ]);
    // Calendar row, Tasks row (one figure, the whole row), Contacts row.
    expect(tiles.map((t) => t.classes().includes('col-span-2'))).toEqual([false, false, true, false, false]);
    expect(wrapper.get('.grid').classes()).toContain('grid-cols-2');
  });

  it('gives Open tasks the Tasks icon and the same tile tokens as the others', () => {
    const wrapper = mountCard();

    const icon = tile(wrapper, 'open-tasks').get('i');
    expect(icon.classes()).toEqual(
      expect.arrayContaining(['pi', 'pi-check-square', 'text-primary-600', 'dark:text-primary-400'])
    );
    expect(tile(wrapper, 'open-tasks').classes()).toEqual(
      expect.arrayContaining(['rounded-xl', 'bg-surface-50', 'dark:bg-surface-800/60'])
    );
    expect(figure(wrapper, 'open-tasks').classes()).toEqual(
      expect.arrayContaining(['text-lg', 'font-semibold', 'tabular-nums'])
    );
  });

  it('shows 0 open tasks as 0', () => {
    const wrapper = mountCard({ openTasks: 0 });

    expect(figure(wrapper, 'open-tasks').text()).toBe('0');
    expect(tile(wrapper, 'open-tasks').find('.sr-only').exists()).toBe(false);
  });

  it('pulses while the summary is on its way and there is no count yet', () => {
    const wrapper = mountCard({ openTasks: null, openTasksLoading: true });

    const pulse = figure(wrapper, 'open-tasks');
    expect(pulse.classes()).toContain('animate-pulse');
    expect(pulse.text()).toBe('');
    // The label stays, so the tile does not jump when the number arrives.
    expect(tile(wrapper, 'open-tasks').text()).toBe('Open tasks');
    // The other figures are not loading.
    expect(figure(wrapper, 'calendars').text()).toBe('3');
  });

  it('says "Not available" instead of 0 when there is no summary and none is coming', () => {
    const wrapper = mountCard({ openTasks: null, openTasksLoading: false });

    const value = figure(wrapper, 'open-tasks');
    expect(value.classes()).not.toContain('animate-pulse');
    expect(value.get('[aria-hidden="true"]').text()).toBe('—');
    expect(value.get('.sr-only').text()).toBe('Not available');
    expect(value.text()).not.toContain('0');
  });

  it('keeps a known count on screen while the summary is fetched again', () => {
    const wrapper = mountCard({ openTasks: 4, openTasksLoading: true });

    expect(figure(wrapper, 'open-tasks').text()).toBe('4');
    expect(figure(wrapper, 'open-tasks').classes()).not.toContain('animate-pulse');
  });

  it('pulses the other four figures while events and contacts load, not Open tasks', () => {
    const wrapper = mountCard({ loading: true });

    for (const key of ['calendars', 'events', 'address-books', 'contacts']) {
      expect(figure(wrapper, key).classes()).toContain('animate-pulse');
    }
    expect(figure(wrapper, 'open-tasks').text()).toBe('7');
  });

  it('updates the figure when the summary changes', async () => {
    const wrapper = mountCard({ openTasks: null, openTasksLoading: true });

    await wrapper.setProps({ openTasks: 9, openTasksLoading: false });

    expect(figure(wrapper, 'open-tasks').text()).toBe('9');
  });
});
