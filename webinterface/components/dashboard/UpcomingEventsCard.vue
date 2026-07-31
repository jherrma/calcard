<template>
  <DashboardWidgetCard
    title="Upcoming Events"
    icon="pi pi-clock"
    :badge="showSkeleton ? null : events.length"
  >
    <template #actions>
      <button
        type="button"
        class="text-xs font-medium text-primary-600 dark:text-primary-400 hover:underline"
        @click="$emit('view-all')"
      >
        View all
      </button>
    </template>

    <CommonSkeletonList v-if="showSkeleton" :count="4" />

    <div v-else-if="events.length === 0" class="flex flex-col items-center gap-2 py-8 text-center">
      <i class="pi pi-calendar text-3xl text-surface-300 dark:text-surface-600" />
      <p class="text-sm text-surface-600 dark:text-surface-400">No upcoming events</p>
      <p class="text-xs text-surface-400 dark:text-surface-500">
        Nothing scheduled for the rest of {{ horizonLabel }}.
      </p>
    </div>

    <ul v-else class="space-y-0.5">
      <li v-for="item in items" :key="item.key">
        <button
          type="button"
          class="w-full flex items-center gap-3 px-2 py-2 rounded-xl text-left hover:bg-surface-100 dark:hover:bg-surface-800 transition-colors"
          @click="$emit('select', item.event)"
        >
          <span
            class="w-1.5 h-9 rounded-full flex-shrink-0"
            :style="{ backgroundColor: item.color }"
          />
          <span class="flex-1 min-w-0">
            <span class="block text-sm font-medium text-surface-900 dark:text-surface-100 truncate">
              {{ item.title }}
            </span>
            <span class="block text-xs text-surface-500 dark:text-surface-400 truncate">
              {{ item.when }}
            </span>
          </span>
          <span
            class="text-xs font-medium whitespace-nowrap flex-shrink-0"
            :class="item.isNear
              ? 'text-primary-600 dark:text-primary-400'
              : 'text-surface-400 dark:text-surface-500'"
          >
            {{ item.relative }}
          </span>
        </button>
      </li>
    </ul>
  </DashboardWidgetCard>
</template>

<script setup lang="ts">
import type { CalendarEvent } from '~/types/calendar';
import { useCalendarStore } from '~/stores/calendars';

const props = defineProps<{
  events: CalendarEvent[];
  /** Dashboard clock in epoch ms — drives the Today/Tomorrow labels. */
  now: number;
  loading?: boolean;
}>();

defineEmits<{
  select: [event: CalendarEvent];
  'view-all': [];
}>();

const calendarStore = useCalendarStore();

// Only skeleton on a first load; a background refresh keeps the current list
// visible instead of flashing placeholders every five minutes.
const showSkeleton = computed(() => props.loading && props.events.length === 0);

// How far ahead the dashboard actually looked, so the empty state doesn't claim
// "nothing ever" when it only fetched the current and next month.
const horizonLabel = computed(() =>
  addMonths(new Date(props.now), 1).toLocaleDateString(undefined, { month: 'long' })
);

const items = computed(() =>
  props.events.map((event) => {
    const start = new Date(event.start);
    const relative = relativeDayLabel(start, new Date(props.now));
    return {
      // Recurring occurrences share the backend id, so include the occurrence
      // marker in the v-for key (same reason as the calendar page's composite id).
      key: `${event.id}|${event.recurrence_id ?? ''}|${event.start}`,
      event,
      title: event.summary || '(No title)',
      color: calendarStore.calendars.find((c) => String(c.id) === String(event.calendar_id))?.color || '#3b82f6',
      relative,
      isNear: relative === 'Today' || relative === 'Tomorrow',
      when: describeWhen(event, start),
    };
  })
);

function describeWhen(event: CalendarEvent, start: Date): string {
  const date = start.toLocaleDateString(undefined, { weekday: 'short', month: 'short', day: 'numeric' });
  if (event.all_day) return `${date} · All day`;
  const end = new Date(event.end);
  const times = Number.isNaN(end.getTime()) || end.getTime() <= start.getTime()
    ? formatTimeOfDay(start)
    : `${formatTimeOfDay(start)} – ${formatTimeOfDay(end)}`;
  return `${date} · ${times}`;
}
</script>
