<template>
  <Dialog
    :visible="visible"
    header="Event Details"
    modal
    :closable="true"
    :style="{ width: '32rem' }"
    @update:visible="$emit('update:visible', $event)"
  >
    <div v-if="event" class="flex flex-col gap-4">
      <!-- Summary -->
      <h2 class="text-xl font-semibold text-surface-900 dark:text-surface-100">{{ event.summary }}</h2>

      <!-- Date/Time -->
      <div class="flex items-start gap-3">
        <i class="pi pi-clock text-surface-500 mt-0.5" />
        <div class="text-sm text-surface-700 dark:text-surface-300">
          <div>{{ formatDateTime(event.start, event.all_day) }}</div>
          <div>to {{ formatDateTime(event.end, event.all_day) }}</div>
          <div v-if="event.is_recurring" class="text-surface-500 mt-1">
            <i class="pi pi-replay text-xs mr-1" />
            {{ formatRecurrence(event.recurrence) }}
          </div>
        </div>
      </div>

      <!-- Location -->
      <div v-if="event.location" class="flex items-start gap-3">
        <i class="pi pi-map-marker text-surface-500 mt-0.5" />
        <span class="text-sm text-surface-700 dark:text-surface-300">{{ event.location }}</span>
      </div>

      <!-- Calendar -->
      <div class="flex items-center gap-3">
        <i class="pi pi-calendar text-surface-500" />
        <div class="flex items-center gap-2">
          <span
            class="w-3 h-3 rounded-full flex-shrink-0"
            :style="{ backgroundColor: calendarColor }"
          />
          <span class="text-sm text-surface-700 dark:text-surface-300">{{ calendarName }}</span>
        </div>
      </div>

      <!-- Description -->
      <div v-if="event.description" class="flex items-start gap-3">
        <i class="pi pi-align-left text-surface-500 mt-0.5" />
        <p class="text-sm text-surface-700 dark:text-surface-300 whitespace-pre-wrap">{{ event.description }}</p>
      </div>
    </div>

    <template #footer>
      <div class="flex justify-between w-full">
        <Button
          label="Delete"
          severity="danger"
          text
          icon="pi pi-trash"
          @click="handleDelete"
        />
        <Button
          label="Edit"
          icon="pi pi-pencil"
          @click="$emit('edit', event!)"
        />
      </div>
    </template>
  </Dialog>

  <!-- Recurrence scope dialog for delete -->
  <RecurrenceScopeDialog
    :visible="showScopeDialog"
    action="delete"
    @update:visible="showScopeDialog = $event"
    @confirm="handleScopeConfirm"
    @cancel="showScopeDialog = false"
  />
</template>

<script setup lang="ts">
import type { CalendarEvent, RecurrenceRule } from '~/types/calendar';
import { useCalendarStore } from '~/stores/calendars';
import RecurrenceScopeDialog from '~/components/calendar/RecurrenceScopeDialog.vue';
import { useConfirm } from 'primevue/useconfirm';

const props = defineProps<{
  visible: boolean;
  event: CalendarEvent | null;
}>();

const emit = defineEmits<{
  'update:visible': [value: boolean];
  'edit': [event: CalendarEvent];
  'delete': [event: CalendarEvent, scope?: string];
}>();

const calendarStore = useCalendarStore();
const confirm = useConfirm();
const showScopeDialog = ref(false);

const calendarColor = computed(() => {
  if (!props.event) return '#3b82f6';
  const cal = calendarStore.calendars.find(c => c.id === String(props.event!.calendar_id));
  return cal?.color || '#3b82f6';
});

const calendarName = computed(() => {
  if (!props.event) return '';
  const cal = calendarStore.calendars.find(c => c.id === String(props.event!.calendar_id));
  return cal?.name || '';
});

const formatDateTime = (dateStr: string, allDay: boolean) => {
  const date = new Date(dateStr);
  if (allDay) {
    return date.toLocaleDateString(undefined, { weekday: 'long', year: 'numeric', month: 'long', day: 'numeric' });
  }
  return date.toLocaleString(undefined, { weekday: 'long', year: 'numeric', month: 'long', day: 'numeric', hour: '2-digit', minute: '2-digit' });
};

const formatRecurrence = (rule?: RecurrenceRule) => {
  if (!rule) return 'Recurring';
  const freq = rule.frequency?.toLowerCase();
  const interval = rule.interval || 1;
  if (interval === 1) {
    return `Repeats ${freq}`;
  }
  const units: Record<string, string> = { daily: 'days', weekly: 'weeks', monthly: 'months', yearly: 'years' };
  return `Repeats every ${interval} ${units[freq] || freq}`;
};

const handleDelete = () => {
  if (!props.event) return;

  if (props.event.is_recurring) {
    showScopeDialog.value = true;
  } else {
    confirm.require({
      message: `Are you sure you want to delete "${props.event.summary}"?`,
      header: 'Delete Event',
      icon: 'pi pi-exclamation-triangle',
      acceptClass: 'p-button-danger',
      accept: () => {
        emit('delete', props.event!);
      },
    });
  }
};

const handleScopeConfirm = (scope: string) => {
  showScopeDialog.value = false;
  emit('delete', props.event!, scope);
};
</script>
