/**
 * Column ("lane") assignment for the dashboard's day timeline (story 042).
 *
 * Absolutely positioned event blocks that share vertical space would paint on
 * top of each other, hiding the one drawn first and making it unclickable — a
 * double-booked hour would silently lose an event even though the widget's badge
 * counts it. Splitting each overlap cluster into side-by-side columns is what
 * FullCalendar's timeGrid does; this is the same idea, kept as a pure function
 * so it can be tested without mounting the card.
 *
 * Overlap is decided on the RENDERED rectangles, not on the event times: a
 * 10-minute meeting is floored to a minimum height, so two events that barely
 * touch in time can still cover each other on screen.
 *
 * Auto-imported by Nuxt (files under `utils/`) — call this without an import.
 */

/** A block's vertical extent in pixels: `[top, top + height)`. */
export interface AgendaSlot {
  top: number;
  height: number;
}

/** Where a block sits horizontally within its overlap cluster. */
export interface AgendaLane {
  /** 0-based column index. */
  lane: number;
  /** Columns the cluster was split into — the divisor for the block's width. */
  lanes: number;
}

/**
 * Lane assignment for each slot, returned in the SAME order as the input so the
 * caller can zip it back onto its own block objects by index.
 *
 * Blocks are walked top-down (longest first on a tie, so an event that contains
 * others takes the leftmost column) and greedily placed in the first column that
 * is free at that height. A cluster ends as soon as a block starts at or below
 * every rectangle seen so far, which resets the columns — so an empty afternoon
 * doesn't inherit the morning's column count.
 */
export function assignAgendaLanes(slots: AgendaSlot[]): AgendaLane[] {
  const result: AgendaLane[] = slots.map(() => ({ lane: 0, lanes: 1 }));

  const order = slots
    .map((_, i) => i)
    .sort((a, b) => {
      const first = slots[a]!;
      const second = slots[b]!;
      if (first.top !== second.top) return first.top - second.top;
      return second.height - first.height;
    });

  // Bottom edge currently occupied by each column of the open cluster.
  let laneEnds: number[] = [];
  let cluster: number[] = [];
  let clusterEnd = Number.NEGATIVE_INFINITY;

  const closeCluster = () => {
    const width = Math.max(laneEnds.length, 1);
    for (const i of cluster) result[i]!.lanes = width;
    laneEnds = [];
    cluster = [];
    clusterEnd = Number.NEGATIVE_INFINITY;
  };

  for (const i of order) {
    const slot = slots[i]!;
    if (slot.top >= clusterEnd) closeCluster();

    let lane = laneEnds.findIndex((end) => end <= slot.top);
    if (lane === -1) {
      lane = laneEnds.length;
      laneEnds.push(0);
    }
    laneEnds[lane] = slot.top + slot.height;

    result[i]!.lane = lane;
    cluster.push(i);
    clusterEnd = Math.max(clusterEnd, slot.top + slot.height);
  }
  closeCluster();

  return result;
}
