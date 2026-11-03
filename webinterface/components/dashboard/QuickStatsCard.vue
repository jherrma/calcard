<template>
  <DashboardWidgetCard title="Overview" icon="pi pi-chart-bar">
    <div class="grid grid-cols-2 gap-3">
      <div
        v-for="stat in stats"
        :key="stat.key"
        class="flex items-center gap-3 p-3 rounded-xl bg-surface-50 dark:bg-surface-800/60"
        :class="{ 'col-span-2': stat.wide }"
        :data-stat="stat.key"
      >
        <i :class="[stat.icon, 'text-lg text-primary-600 dark:text-primary-400']" />
        <div class="min-w-0">
          <p
            v-if="stat.loading"
            class="h-5 w-8 rounded bg-surface-200 dark:bg-surface-700 animate-pulse"
          />
          <p v-else class="text-lg font-semibold text-surface-900 dark:text-surface-100 tabular-nums leading-tight">
            <template v-if="stat.value !== null">{{ stat.value }}</template>
            <template v-else>
              <span aria-hidden="true">—</span>
              <span class="sr-only">Not available</span>
            </template>
          </p>
          <p class="text-xs text-surface-500 dark:text-surface-400 truncate">{{ stat.label }}</p>
        </div>
      </div>
    </div>
  </DashboardWidgetCard>
</template>

<script setup lang="ts">
const props = defineProps<{
  calendars: number;
  /** Events overlapping the current calendar month. */
  eventsThisMonth: number;
  addressBooks: number;
  /** Sum of every address book's reported contact total. */
  contacts: number;
  /**
   * Open tasks across every task list the user can read: `summary.open` from
   * GET /tasks/summary (#170). Null while there is no summary (not loaded yet,
   * or the request failed): shown as a pulse while `openTasksLoading`, else as
   * "—", never as 0.
   */
  openTasks: number | null;
  /** The summary request is in flight. */
  openTasksLoading?: boolean;
  /** The events and contacts behind the other four figures are loading. */
  loading?: boolean;
}>();

interface Stat {
  key: string;
  label: string;
  value: number | null;
  icon: string;
  loading: boolean;
  /** Takes the whole row. */
  wide?: boolean;
}

// One row per section, in the sidebar's order: Calendar, Tasks, Contacts. Tasks
// has one figure, so its tile spans the row rather than leaving a hole.
const stats = computed<Stat[]>(() => [
  { key: 'calendars', label: 'Calendars', value: props.calendars, icon: 'pi pi-calendar', loading: !!props.loading },
  { key: 'events', label: 'Events this month', value: props.eventsThisMonth, icon: 'pi pi-clock', loading: !!props.loading },
  {
    key: 'open-tasks',
    label: 'Open tasks',
    value: props.openTasks,
    icon: 'pi pi-check-square',
    // A known count stays on screen while it is refetched.
    loading: props.openTasks === null && !!props.openTasksLoading,
    wide: true,
  },
  { key: 'address-books', label: 'Address books', value: props.addressBooks, icon: 'pi pi-book', loading: !!props.loading },
  { key: 'contacts', label: 'Contacts', value: props.contacts, icon: 'pi pi-users', loading: !!props.loading },
]);
</script>
