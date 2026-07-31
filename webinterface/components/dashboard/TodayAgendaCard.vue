<template>
  <DashboardWidgetCard title="Today's Agenda" icon="pi pi-list" :badge="showSkeleton ? null : total">
    <template #actions>
      <button
        type="button"
        class="text-xs font-medium text-primary-600 dark:text-primary-400 hover:underline"
        @click="$emit('view-calendar')"
      >
        View calendar
      </button>
    </template>

    <CommonSkeletonList v-if="showSkeleton" :count="3" />

    <div v-else-if="total === 0" class="flex flex-col items-center gap-2 py-8 text-center">
      <i class="pi pi-check-circle text-3xl text-surface-300 dark:text-surface-600" />
      <p class="text-sm text-surface-600 dark:text-surface-400">Nothing scheduled today</p>
      <p class="text-xs text-surface-400 dark:text-surface-500">{{ todayLabel }}</p>
    </div>

    <div v-else class="space-y-3">
      <!-- All-day events sit above the timeline: they have no position on it. -->
      <div v-if="allDayEvents.length > 0" class="space-y-1">
        <p class="text-[10px] font-semibold uppercase tracking-wide text-surface-400 dark:text-surface-500">
          All day
        </p>
        <button
          v-for="item in allDayItems"
          :key="item.key"
          type="button"
          class="w-full flex items-center gap-2 px-3 py-2 rounded-lg text-left bg-surface-50 dark:bg-surface-800/60 hover:bg-surface-100 dark:hover:bg-surface-800 border-l-[3px] transition-colors"
          :style="{ borderLeftColor: item.color }"
          @click="$emit('select', item.event)"
        >
          <span class="text-sm text-surface-900 dark:text-surface-100 truncate">{{ item.title }}</span>
        </button>
      </div>

      <div v-if="timedEvents.length === 0" class="text-xs text-surface-400 dark:text-surface-500">
        No timed events today.
      </div>

      <!-- Hour-marked timeline. Scrolls rather than growing without bound: a day
           holding both an 01:00 and a 23:00 event spans the full 24 rows. -->
      <div v-else class="max-h-80 overflow-y-auto pr-1">
        <div class="relative" :style="{ height: `${timelineHeight}px` }">
          <div
            v-for="hour in hourMarks"
            :key="hour"
            class="absolute left-0 right-0 border-t border-surface-200 dark:border-surface-800"
            :style="{ top: `${offsetForHour(hour)}px` }"
          >
            <span class="absolute -top-2 left-0 w-11 text-[10px] tabular-nums text-surface-400 dark:text-surface-500">
              {{ hourLabel(hour) }}
            </span>
          </div>

          <!-- Blocks live in their own layer that starts where the hour labels
               end, so a block's lane can be expressed as a plain percentage of
               the layer's width. -->
          <div class="absolute left-12 right-0 top-0 bottom-0">
            <button
              v-for="block in blocks"
              :key="block.key"
              type="button"
              class="absolute flex flex-col justify-center overflow-hidden px-2 rounded-md text-left border-l-[3px] bg-surface-50 dark:bg-surface-800/70 hover:bg-surface-100 dark:hover:bg-surface-800 transition-colors"
              :style="{
                top: `${block.top}px`,
                height: `${block.height}px`,
                left: block.left,
                width: block.width,
                borderLeftColor: block.color,
              }"
              :title="`${block.title} · ${block.time}`"
              @click="$emit('select', block.event)"
            >
              <span class="text-xs font-medium text-surface-900 dark:text-surface-100 truncate w-full">
                {{ block.title }}
              </span>
              <span v-if="block.height >= 34" class="text-[10px] text-surface-500 dark:text-surface-400 truncate w-full">
                {{ block.time }}
              </span>
            </button>
          </div>

          <!-- Current-time indicator -->
          <div
            v-if="nowVisible"
            class="absolute left-8 right-0 flex items-center gap-1 pointer-events-none"
            :style="{ top: `${nowOffset}px` }"
          >
            <span class="w-1.5 h-1.5 rounded-full bg-red-500 flex-shrink-0" />
            <span class="flex-1 h-px bg-red-500" />
          </div>
        </div>
      </div>
    </div>
  </DashboardWidgetCard>
</template>

<script setup lang="ts">
import type { CalendarEvent } from '~/types/calendar';
import { useCalendarStore } from '~/stores/calendars';

/** Pixels per hour on the timeline. */
const HOUR_HEIGHT = 48;
/** Floor for an event block so a 10-minute meeting stays readable/clickable. */
const MIN_BLOCK_HEIGHT = 22;
/** Never show fewer than this many hours of context around the events. */
const MIN_HOURS_VISIBLE = 5;
/** Gutter in px between two side-by-side (overlapping) blocks. */
const LANE_GAP = 2;

const props = defineProps<{
  allDayEvents: CalendarEvent[];
  timedEvents: CalendarEvent[];
  /** Dashboard clock in epoch ms — positions the current-time indicator. */
  now: number;
  loading?: boolean;
}>();

defineEmits<{
  select: [event: CalendarEvent];
  'view-calendar': [];
}>();

const calendarStore = useCalendarStore();

const total = computed(() => props.allDayEvents.length + props.timedEvents.length);
const showSkeleton = computed(() => props.loading && total.value === 0);
const todayLabel = computed(() =>
  new Date(props.now).toLocaleDateString(undefined, { weekday: 'long', month: 'long', day: 'numeric' })
);

const dayStartMs = computed(() => startOfDay(new Date(props.now)).getTime());

const colorFor = (event: CalendarEvent) =>
  calendarStore.calendars.find((c) => String(c.id) === String(event.calendar_id))?.color || '#3b82f6';

const keyFor = (event: CalendarEvent) => `${event.id}|${event.recurrence_id ?? ''}|${event.start}`;

/**
 * Fractional hours since local midnight, clamped to [0, 24]: a multi-day event
 * (or one crossing midnight) must still lay out inside today's column instead of
 * being positioned off the top or bottom of the timeline.
 */
const toDayHours = (ms: number): number => {
  const hours = (ms - dayStartMs.value) / 3600000;
  return Math.min(24, Math.max(0, hours));
};

const allDayItems = computed(() =>
  props.allDayEvents.map((event) => ({
    key: keyFor(event),
    event,
    title: event.summary || '(No title)',
    color: colorFor(event),
  }))
);

/**
 * Which hours the timeline renders: just enough to cover today's events AND the
 * current time (so the "now" line is always on screen), padded to a minimum span.
 */
const range = computed(() => {
  const marks: number[] = [toDayHours(props.now)];
  for (const event of props.timedEvents) {
    const bounds = eventBounds(event.start, event.end);
    if (!bounds) continue;
    marks.push(toDayHours(bounds.start), toDayHours(bounds.end));
  }

  let first = Math.floor(Math.min(...marks));
  let last = Math.ceil(Math.max(...marks));
  if (last - first < MIN_HOURS_VISIBLE) last = first + MIN_HOURS_VISIBLE;
  if (last > 24) {
    last = 24;
    first = Math.max(0, last - MIN_HOURS_VISIBLE);
  }
  return { first, last };
});

const offsetForHour = (hour: number) => (hour - range.value.first) * HOUR_HEIGHT;
const timelineHeight = computed(() => (range.value.last - range.value.first) * HOUR_HEIGHT);
const hourMarks = computed(() =>
  Array.from({ length: range.value.last - range.value.first }, (_, i) => range.value.first + i)
);
const hourLabel = (hour: number) => formatTimeOfDay(new Date(dayStartMs.value + hour * 3600000));

/**
 * Positioned blocks for today's timed events.
 *
 * Concurrent events are split into side-by-side lanes: with every block pinned
 * to the full width, the one drawn last covered the one underneath completely,
 * so a double-booked hour showed a single title while the badge counted two —
 * and the hidden event could not be clicked at all.
 */
const blocks = computed(() => {
  const positioned = props.timedEvents.flatMap((event) => {
    const bounds = eventBounds(event.start, event.end);
    if (!bounds) return [];
    const startHours = toDayHours(bounds.start);
    const endHours = toDayHours(bounds.end);
    const start = new Date(bounds.start);
    const end = new Date(bounds.end);
    return [{
      key: keyFor(event),
      event,
      title: event.summary || '(No title)',
      color: colorFor(event),
      top: (startHours - range.value.first) * HOUR_HEIGHT,
      height: Math.max((endHours - startHours) * HOUR_HEIGHT, MIN_BLOCK_HEIGHT),
      time: `${formatTimeOfDay(start)} – ${formatTimeOfDay(end)}`,
    }];
  });

  const lanes = assignAgendaLanes(positioned);
  return positioned.map((block, i) => {
    const { lane, lanes: count } = lanes[i]!;
    return {
      ...block,
      left: `${(lane / count) * 100}%`,
      // A gutter so adjacent lanes read as two blocks rather than one wide one.
      width: `calc(${100 / count}% - ${LANE_GAP}px)`,
    };
  });
});

const nowHours = computed(() => toDayHours(props.now));
const nowVisible = computed(() => nowHours.value >= range.value.first && nowHours.value <= range.value.last);
const nowOffset = computed(() => (nowHours.value - range.value.first) * HOUR_HEIGHT);
</script>
