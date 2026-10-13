// @vitest-environment nuxt
import { describe, it, expect } from 'vitest';
import { mount } from '@vue/test-utils';
import MiniCalendarCard from './MiniCalendarCard.vue';

// SCOPE: the mini calendar's week start. The grid and the weekday header row are
// built separately, so the invariant worth pinning is that they agree — a header
// row that still read Sunday-first over a Monday-first grid would mislabel every
// column — and that both follow the `firstDay` the dashboard passes in (the
// user's first_day_of_week, also used by the calendar page).

interface CardVm {
  grid: { key: string; date: Date }[];
  weekdayNames: string[];
}

function narrowWeekday(d: Date): string {
  return d.toLocaleDateString(undefined, { weekday: 'narrow' });
}

function mountCard(firstDay: number) {
  const wrapper = mount(MiniCalendarCard, {
    props: {
      month: new Date(2026, 6, 1), // July 2026 starts on a Wednesday
      eventDayKeys: new Set<string>(),
      now: new Date(2026, 6, 15, 12).getTime(),
      calendarCount: 1,
      eventCount: 0,
      firstDay,
    },
    shallow: true, // stub DashboardWidgetCard, but render the grid in its slot
    global: { renderStubDefaultSlot: true },
  });
  return { wrapper, vm: wrapper.vm as unknown as CardVm };
}

describe.each([
  [0, '2026-06-28'],
  [1, '2026-06-29'],
  [6, '2026-06-27'],
])('MiniCalendarCard with firstDay %i', (firstDay, firstCellKey) => {
  it('starts the grid on that weekday', () => {
    const { vm } = mountCard(firstDay);

    expect(vm.grid[0]!.key).toBe(firstCellKey);
    expect(vm.grid[0]!.date.getDay()).toBe(firstDay);
  });

  it('labels every column with the weekday of the cells beneath it', () => {
    const { wrapper, vm } = mountCard(firstDay);

    const rendered = wrapper.findAll('span.uppercase').map((s) => s.text());
    expect(rendered).toEqual(vm.weekdayNames);
    expect(rendered).toHaveLength(7);
    for (let column = 0; column < 7; column++) {
      expect(rendered[column]).toBe(narrowWeekday(vm.grid[column]!.date));
    }
  });
});
