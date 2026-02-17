<template>
  <aside class="w-64 bg-surface-0 dark:bg-surface-900 border-r border-surface-200 dark:border-surface-800 flex-col hidden lg:flex">
    <div class="p-4 border-b border-surface-200 dark:border-surface-800">
      <Button
        label="New Event"
        icon="pi pi-plus"
        class="w-full"
        @click="$emit('create-event')"
      />
    </div>

    <div class="flex-1 overflow-y-auto p-4">
      <div class="flex items-center justify-between mb-3">
        <h3 class="text-sm font-semibold text-surface-700 dark:text-surface-300">My Calendars</h3>
        <button
          class="text-surface-400 hover:text-surface-600 dark:hover:text-surface-200"
          @click="$emit('add-calendar')"
        >
          <i class="pi pi-plus text-sm" />
        </button>
      </div>

      <div class="space-y-1">
        <div
          v-for="calendar in ownedCalendars"
          :key="calendar.id"
          class="flex items-center gap-2 p-2 rounded-lg hover:bg-surface-100 dark:hover:bg-surface-800 group"
        >
          <Checkbox
            :model-value="calendar.visible"
            :binary="true"
            @update:model-value="$emit('toggle-calendar', calendar.id)"
          />
          <span
            class="w-3 h-3 rounded-full flex-shrink-0"
            :style="{ backgroundColor: calendar.color }"
          />
          <span class="flex-1 text-sm truncate text-surface-700 dark:text-surface-300">{{ calendar.name }}</span>
          <button
            class="opacity-0 group-hover:opacity-100 text-surface-400 hover:text-surface-600 dark:hover:text-surface-200"
            @click.stop="showCalendarMenu($event, calendar)"
          >
            <i class="pi pi-ellipsis-v text-sm" />
          </button>
        </div>
      </div>

      <!-- Shared calendars -->
      <div v-if="sharedCalendars.length > 0" class="mt-6">
        <h3 class="text-sm font-semibold text-surface-700 dark:text-surface-300 mb-3">Shared with me</h3>
        <div class="space-y-1">
          <div
            v-for="calendar in sharedCalendars"
            :key="calendar.id"
            class="flex items-center gap-2 p-2 rounded-lg hover:bg-surface-100 dark:hover:bg-surface-800"
          >
            <Checkbox
              :model-value="calendar.visible"
              :binary="true"
              @update:model-value="$emit('toggle-calendar', calendar.id)"
            />
            <span
              class="w-3 h-3 rounded-full flex-shrink-0"
              :style="{ backgroundColor: calendar.color }"
            />
            <div class="flex-1 min-w-0">
              <span class="text-sm truncate block text-surface-700 dark:text-surface-300">{{ calendar.name }}</span>
              <span class="text-xs text-surface-500">{{ calendar.owner?.display_name }}</span>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Calendar context menu -->
    <Menu ref="calendarMenu" :model="menuItems" :popup="true" />
  </aside>
</template>

<script setup lang="ts">
import type { Calendar } from '~/types/calendar';

const props = defineProps<{
  calendars: (Calendar & { visible: boolean })[];
}>();

defineEmits<{
  'toggle-calendar': [id: string];
  'add-calendar': [];
  'create-event': [];
}>();

const calendarMenu = ref();
const selectedCalendar = ref<Calendar | null>(null);

const ownedCalendars = computed(() =>
  props.calendars.filter(c => !c.shared)
);

const sharedCalendars = computed(() =>
  props.calendars.filter(c => c.shared)
);

const menuItems = computed(() => [
  {
    label: 'Edit',
    icon: 'pi pi-pencil',
    command: () => navigateTo(`/calendar/settings/${selectedCalendar.value?.id}`),
  },
  {
    label: 'Share',
    icon: 'pi pi-share-alt',
    command: () => navigateTo(`/calendar/share/${selectedCalendar.value?.id}`),
  },
  { separator: true },
  {
    label: 'Delete',
    icon: 'pi pi-trash',
    class: 'text-red-600',
    command: () => {/* Show delete confirmation */},
  },
]);

const showCalendarMenu = (event: Event, calendar: Calendar) => {
  selectedCalendar.value = calendar;
  calendarMenu.value.toggle(event);
};
</script>
