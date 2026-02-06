<template>
  <Dialog
    :visible="visible"
    header="New Event"
    modal
    :closable="true"
    :style="{ width: '36rem' }"
    @update:visible="$emit('update:visible', $event)"
  >
    <EventForm
      :initial-start="initialStart"
      :initial-end="initialEnd"
      :initial-all-day="initialAllDay"
      :is-submitting="isSubmitting"
      @submit="handleSubmit"
      @cancel="$emit('update:visible', false)"
    />
  </Dialog>
</template>

<script setup lang="ts">
import type { EventFormData } from '~/types/calendar';
import { useCalendarStore } from '~/stores/calendars';
import EventForm from '~/components/calendar/EventForm.vue';

defineProps<{
  visible: boolean;
  initialStart?: Date;
  initialEnd?: Date;
  initialAllDay?: boolean;
}>();

const emit = defineEmits<{
  'update:visible': [value: boolean];
  'created': [];
}>();

const calendarStore = useCalendarStore();
const toast = useAppToast();
const isSubmitting = ref(false);

const handleSubmit = async (data: EventFormData) => {
  isSubmitting.value = true;
  try {
    await calendarStore.createEvent(data.calendar_id, data);
    toast.success('Event created');
    emit('update:visible', false);
    emit('created');
  } catch (e: unknown) {
    toast.error((e as Error).message || 'Failed to create event');
  } finally {
    isSubmitting.value = false;
  }
};
</script>
