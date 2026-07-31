// @vitest-environment nuxt
import { describe, it, expect } from 'vitest';
import { assignAgendaLanes } from './agendaLayout';

// Slots are rendered rectangles in px: `top` and `height` as the agenda card
// computes them (48px per hour, floored to a 22px minimum).
const HOUR = 48;

describe('assignAgendaLanes', () => {
  it('leaves a non-overlapping day in a single full-width lane', () => {
    const lanes = assignAgendaLanes([
      { top: 0, height: HOUR },
      { top: HOUR, height: HOUR },
      { top: 3 * HOUR, height: HOUR / 2 },
    ]);

    expect(lanes).toEqual([
      { lane: 0, lanes: 1 },
      { lane: 0, lanes: 1 },
      { lane: 0, lanes: 1 },
    ]);
  });

  it('splits two events at the same time into side-by-side lanes', () => {
    // 10:00–11:00 "Standup" and 10:00–11:00 "1:1": identical rectangles used to
    // paint on top of each other, hiding one event completely.
    const lanes = assignAgendaLanes([
      { top: 2 * HOUR, height: HOUR },
      { top: 2 * HOUR, height: HOUR },
    ]);

    expect(lanes.map((l) => l.lanes)).toEqual([2, 2]);
    expect(lanes.map((l) => l.lane).sort()).toEqual([0, 1]);
  });

  it('gives the containing event the first lane and keeps the short one visible', () => {
    // 09:00–17:00 conference listed AFTER a 10:00–10:30 call: the short block was
    // drawn first and buried.
    const call = { top: HOUR, height: HOUR / 2 };
    const conference = { top: 0, height: 8 * HOUR };
    const lanes = assignAgendaLanes([call, conference]);

    expect(lanes[1]).toEqual({ lane: 0, lanes: 2 }); // conference, leftmost
    expect(lanes[0]).toEqual({ lane: 1, lanes: 2 }); // call, beside it
  });

  it('counts a cluster by its widest point, so a chain of three shares three lanes', () => {
    const lanes = assignAgendaLanes([
      { top: 0, height: 3 * HOUR },
      { top: HOUR, height: 3 * HOUR },
      { top: 2 * HOUR, height: HOUR },
    ]);

    expect(lanes.map((l) => l.lane)).toEqual([0, 1, 2]);
    expect(lanes.map((l) => l.lanes)).toEqual([3, 3, 3]);
  });

  it('starts a fresh cluster once the timeline clears, and reuses a freed lane', () => {
    const lanes = assignAgendaLanes([
      { top: 0, height: HOUR }, // 1st cluster, lane 0
      { top: 0, height: HOUR }, // 1st cluster, lane 1
      { top: 4 * HOUR, height: HOUR }, // 2nd cluster: alone, full width again
    ]);

    expect(lanes[0]!.lanes).toBe(2);
    expect(lanes[1]!.lanes).toBe(2);
    expect(lanes[2]).toEqual({ lane: 0, lanes: 1 });
  });

  it('treats a block that starts exactly where another ends as not overlapping', () => {
    const lanes = assignAgendaLanes([
      { top: 0, height: HOUR },
      { top: HOUR, height: HOUR },
    ]);

    expect(lanes.every((l) => l.lanes === 1 && l.lane === 0)).toBe(true);
  });

  it('returns an empty result for an empty day', () => {
    expect(assignAgendaLanes([])).toEqual([]);
  });
});
