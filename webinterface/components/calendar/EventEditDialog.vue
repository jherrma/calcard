<template>
  <Dialog
    :visible="visible"
    header="Edit Event"
    modal
    :closable="true"
    :style="{ width: '36rem' }"
    @update:visible="$emit('update:visible', $event)"
  >
    <EventForm
      v-if="event"
      :event="event"
      :is-submitting="isSubmitting"
      @submit="handleSubmit"
      @cancel="$emit('update:visible', false)"
    />
  </Dialog>

  <!-- Recurrence scope dialog -->
  <RecurrenceScopeDialog
    :visible="showScopeDialog"
    action="edit"
    @update:visible="showScopeDialog = $event"
    @confirm="handleScopeConfirm"
    @cancel="showScopeDialog = false"
  />
</template>

<script setup lang="ts">
import type { CalendarEvent, EventFormData } from '~/types/calendar';
import { useCalendarStore } from '~/stores/calendars';
import EventForm from '~/components/calendar/EventForm.vue';
import RecurrenceScopeDialog from '~/components/calendar/RecurrenceScopeDialog.vue';

const props = defineProps<{
  visible: boolean;
  event: CalendarEvent | null;
}>();

const emit = defineEmits<{
  'update:visible': [value: boolean];
  'updated': [];
}>();

const calendarStore = useCalendarStore();
const toast = useAppToast();
const isSubmitting = ref(false);
const showScopeDialog = ref(false);
const pendingFormData = ref<EventFormData | null>(null);

const handleSubmit = (data: EventFormData) => {
  if (props.event?.is_recurring) {
    pendingFormData.value = data;
    showScopeDialog.value = true;
  } else {
    saveEvent(data);
  }
};

const handleScopeConfirm = (scope: string) => {
  showScopeDialog.value = false;
  if (pendingFormData.value) {
    saveEvent(pendingFormData.value, scope);
  }
};

const saveEvent = async (data: EventFormData, scope?: string) => {
  if (!props.event) return;

  isSubmitting.value = true;
  try {
    await calendarStore.updateEvent(
      String(props.event.calendar_id),
      props.event.id,
      data,
      scope,
      props.event.recurrence_id
    );
    toast.success('Event updated');
    emit('update:visible', false);
    emit('updated');
  } catch (e: unknown) {
    toast.error((e as Error).message || 'Failed to update event');
  } finally {
    isSubmitting.value = false;
  }
};
</script>
