<template>
  <DashboardWidgetCard :title="monthLabel" icon="pi pi-calendar">
    <template #actions>
      <button
        type="button"
        class="p-1.5 rounded-full text-surface-500 hover:bg-surface-100 dark:hover:bg-surface-800"
        title="Previous month"
        @click="shiftMonth(-1)"
      >
        <i class="pi pi-chevron-left text-xs" />
      </button>
      <button
        type="button"
        class="px-2 py-1 rounded-full text-xs font-medium text-surface-600 dark:text-surface-300 hover:bg-surface-100 dark:hover:bg-surface-800"
        title="Back to the current month"
        @click="goToCurrentMonth"
      >
        Today
      </button>
      <button
        type="button"
        class="p-1.5 rounded-full text-surface-500 hover:bg-surface-100 dark:hover:bg-surface-800"
        title="Next month"
        @click="shiftMonth(1)"
      >
        <i class="pi pi-chevron-right text-xs" />
      </button>
    </template>

    <div class="grid grid-cols-7 gap-y-1 text-center">
      <span
        v-for="(name, i) in weekdayNames"
        :key="i"
        class="text-[10px] font-semibold uppercase text-surface-400 dark:text-surface-500 py-1"
      >
        {{ name }}
      </span>

      <button
        v-for="cell in grid"
        :key="cell.key"
        type="button"
        class="relative mx-auto w-8 h-8 rounded-full text-xs flex items-center justify-center transition-colors"
        :class="[
          cell.key === todayKey
            ? 'bg-primary-600 text-white font-semibold hover:bg-primary-700'
            : cell.otherMonth
              ? 'text-surface-300 dark:text-surface-600 hover:bg-surface-100 dark:hover:bg-surface-800'
              : 'text-surface-700 dark:text-surface-200 hover:bg-surface-100 dark:hover:bg-surface-800',
        ]"
        :title="cell.date.toLocaleDateString(undefined, { weekday: 'long', month: 'long', day: 'numeric' })"
        @click="$emit('select-date', cell.date)"
      >
        {{ cell.day }}
        <!-- Event marker. Kept as a dot rather than a background tint so it stays
             visible on the highlighted "today" cell. -->
        <span
          v-if="eventDayKeys.has(cell.key)"
          class="absolute bottom-0.5 w-1 h-1 rounded-full"
          :class="cell.key === todayKey ? 'bg-white' : 'bg-primary-500'"
        />
      </button>
    </div>

    <template #footer>
      <span v-if="loading">Loading events…</span>
      <span v-else>
        {{ calendarCount }} {{ calendarCount === 1 ? 'calendar' : 'calendars' }} ·
        {{ eventCount }} {{ eventCount === 1 ? 'event' : 'events' }} in {{ shortMonthLabel }}
      </span>
    </template>
  </DashboardWidgetCard>
</template>

<script setup lang="ts">
const props = defineProps<{
  /** The displayed month (local midnight on the 1st). Owned by the page so it
      can make sure that month's events are fetched. */
  month: Date;
  /** Local `YYYY-MM-DD` keys that have at least one event. */
  eventDayKeys: Set<string>;
  /** Dashboard clock in epoch ms — decides which cell is "today". */
  now: number;
  calendarCount: number;
  /** Events overlapping the displayed month. */
  eventCount: number;
  loading?: boolean;
}>();

const emit = defineEmits<{
  'update:month': [month: Date];
  'select-date': [date: Date];
}>();

const monthLabel = computed(() =>
  props.month.toLocaleDateString(undefined, { month: 'long', year: 'numeric' })
);
const shortMonthLabel = computed(() => props.month.toLocaleDateString(undefined, { month: 'short' }));
const todayKey = computed(() => dayKey(new Date(props.now)));
const grid = computed(() => buildMonthGrid(props.month.getFullYear(), props.month.getMonth()));

// Localized weekday initials, Sunday first — buildMonthGrid lays the grid out
// Sunday-first to match FullCalendar's default on the calendar page. 2024-09-01
// is a Sunday, so it anchors the sequence without hardcoding English letters.
const weekdayNames = computed(() =>
  Array.from({ length: 7 }, (_, i) =>
    new Date(2024, 8, 1 + i).toLocaleDateString(undefined, { weekday: 'narrow' })
  )
);

const shiftMonth = (delta: number) => emit('update:month', addMonths(props.month, delta));
const goToCurrentMonth = () => emit('update:month', startOfMonth(new Date(props.now)));
</script>
