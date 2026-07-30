<template>
  <DashboardWidgetCard title="Overview" icon="pi pi-chart-bar">
    <div class="grid grid-cols-2 gap-3">
      <div
        v-for="stat in stats"
        :key="stat.label"
        class="flex items-center gap-3 p-3 rounded-xl bg-surface-50 dark:bg-surface-800/60"
      >
        <i :class="[stat.icon, 'text-lg text-primary-600 dark:text-primary-400']" />
        <div class="min-w-0">
          <p
            v-if="loading"
            class="h-5 w-8 rounded bg-surface-200 dark:bg-surface-700 animate-pulse"
          />
          <p v-else class="text-lg font-semibold text-surface-900 dark:text-surface-100 tabular-nums leading-tight">
            {{ stat.value }}
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
  loading?: boolean;
}>();

const stats = computed(() => [
  { label: 'Calendars', value: props.calendars, icon: 'pi pi-calendar' },
  { label: 'Events this month', value: props.eventsThisMonth, icon: 'pi pi-clock' },
  { label: 'Address books', value: props.addressBooks, icon: 'pi pi-book' },
  { label: 'Contacts', value: props.contacts, icon: 'pi pi-users' },
]);
</script>
