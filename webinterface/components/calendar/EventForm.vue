<template>
  <form class="flex flex-col gap-4" @submit.prevent="handleSubmit">
    <!-- Title -->
    <div class="flex flex-col gap-1">
      <label for="event-title" class="text-sm font-medium text-surface-700 dark:text-surface-300">Title</label>
      <InputText
        id="event-title"
        v-model="form.summary"
        placeholder="Add title"
        :invalid="!!errors.summary"
      />
      <small v-if="errors.summary" class="text-red-500">{{ errors.summary }}</small>
    </div>

    <!-- Calendar selector -->
    <div class="flex flex-col gap-1">
      <label for="event-calendar" class="text-sm font-medium text-surface-700 dark:text-surface-300">Calendar</label>
      <Select
        id="event-calendar"
        v-model="form.calendar_id"
        :options="calendarStore.writableCalendars"
        option-label="name"
        option-value="id"
        placeholder="Select calendar"
        :invalid="!!errors.calendar_id"
      >
        <template #option="{ option }">
          <div class="flex items-center gap-2">
            <span class="w-3 h-3 rounded-full flex-shrink-0" :style="{ backgroundColor: option.color }" />
            <span>{{ option.name }}</span>
          </div>
        </template>
        <template #value="{ value }">
          <div v-if="value" class="flex items-center gap-2">
            <span
              class="w-3 h-3 rounded-full flex-shrink-0"
              :style="{ backgroundColor: getCalendarColor(value) }"
            />
            <span>{{ getCalendarName(value) }}</span>
          </div>
          <span v-else class="text-surface-400">Select calendar</span>
        </template>
      </Select>
      <small v-if="errors.calendar_id" class="text-red-500">{{ errors.calendar_id }}</small>
    </div>

    <!-- All day toggle -->
    <div class="flex items-center gap-3">
      <InputSwitch v-model="form.all_day" input-id="event-allday" />
      <label for="event-allday" class="text-sm text-surface-700 dark:text-surface-300 cursor-pointer">All day</label>
    </div>

    <!-- Start date/time -->
    <div class="flex flex-col gap-1">
      <label for="event-start" class="text-sm font-medium text-surface-700 dark:text-surface-300">Start</label>
      <DatePicker
        id="event-start"
        v-model="form.start"
        :show-time="!form.all_day"
        :show-seconds="false"
        hour-format="24"
        date-format="yy-mm-dd"
        :invalid="!!errors.start"
      />
      <small v-if="errors.start" class="text-red-500">{{ errors.start }}</small>
    </div>

    <!-- End date/time -->
    <div class="flex flex-col gap-1">
      <label for="event-end" class="text-sm font-medium text-surface-700 dark:text-surface-300">End</label>
      <DatePicker
        id="event-end"
        v-model="form.end"
        :show-time="!form.all_day"
        :show-seconds="false"
        hour-format="24"
        date-format="yy-mm-dd"
        :invalid="!!errors.end"
      />
      <small v-if="errors.end" class="text-red-500">{{ errors.end }}</small>
    </div>

    <!-- Timezone -->
    <div class="flex flex-col gap-1">
      <label for="event-timezone" class="text-sm font-medium text-surface-700 dark:text-surface-300">Timezone</label>
      <Select
        id="event-timezone"
        v-model="form.timezone"
        :options="timezones"
        filter
        placeholder="Select timezone"
      />
    </div>

    <!-- Location -->
    <div class="flex flex-col gap-1">
      <label for="event-location" class="text-sm font-medium text-surface-700 dark:text-surface-300">Location</label>
      <InputText
        id="event-location"
        v-model="form.location"
        placeholder="Add location"
      />
    </div>

    <!-- Description -->
    <div class="flex flex-col gap-1">
      <label for="event-description" class="text-sm font-medium text-surface-700 dark:text-surface-300">Description</label>
      <Textarea
        id="event-description"
        v-model="form.description"
        rows="3"
        placeholder="Add description"
        auto-resize
      />
    </div>

    <!-- Recurrence -->
    <div class="flex flex-col gap-3">
      <div class="flex items-center gap-3">
        <InputSwitch v-model="enableRecurrence" input-id="event-recurrence" />
        <label for="event-recurrence" class="text-sm text-surface-700 dark:text-surface-300 cursor-pointer">Repeat</label>
      </div>

      <div v-if="enableRecurrence" class="flex flex-col gap-3 pl-4 border-l-2 border-surface-200 dark:border-surface-700">
        <!-- Frequency -->
        <div class="flex flex-col gap-1">
          <label class="text-sm font-medium text-surface-700 dark:text-surface-300">Frequency</label>
          <Select
            v-model="recurrence.frequency"
            :options="frequencyOptions"
            option-label="label"
            option-value="value"
          />
        </div>

        <!-- Interval -->
        <div class="flex items-center gap-2">
          <label class="text-sm text-surface-700 dark:text-surface-300">Every</label>
          <InputNumber v-model="recurrence.interval" :min="1" :max="99" class="w-20" />
          <span class="text-sm text-surface-600 dark:text-surface-400">{{ intervalLabel }}</span>
        </div>

        <!-- Weekly day selection -->
        <div v-if="recurrence.frequency === 'WEEKLY'" class="flex flex-col gap-1">
          <label class="text-sm font-medium text-surface-700 dark:text-surface-300">On days</label>
          <div class="flex gap-1 flex-wrap">
            <ToggleButton
              v-for="day in weekDays"
              :key="day.value"
              :model-value="recurrence.by_day.includes(day.value)"
              :on-label="day.label"
              :off-label="day.label"
              class="!min-w-[3rem]"
              @update:model-value="toggleDay(day.value)"
            />
          </div>
        </div>

        <!-- End condition -->
        <div class="flex flex-col gap-2">
          <label class="text-sm font-medium text-surface-700 dark:text-surface-300">Ends</label>
          <div class="flex flex-col gap-2">
            <div class="flex items-center gap-2">
              <RadioButton v-model="recurrenceEnd" input-id="rec-never" value="never" />
              <label for="rec-never" class="text-sm text-surface-700 dark:text-surface-300 cursor-pointer">Never</label>
            </div>
            <div class="flex items-center gap-2">
              <RadioButton v-model="recurrenceEnd" input-id="rec-count" value="count" />
              <label for="rec-count" class="text-sm text-surface-700 dark:text-surface-300 cursor-pointer">After</label>
              <InputNumber
                v-model="recurrence.count"
                :min="1"
                :max="999"
                :disabled="recurrenceEnd !== 'count'"
                class="w-24"
              />
              <span class="text-sm text-surface-600 dark:text-surface-400">occurrences</span>
            </div>
            <div class="flex items-center gap-2">
              <RadioButton v-model="recurrenceEnd" input-id="rec-until" value="until" />
              <label for="rec-until" class="text-sm text-surface-700 dark:text-surface-300 cursor-pointer">On</label>
              <DatePicker
                v-model="recurrenceUntilDate"
                :disabled="recurrenceEnd !== 'until'"
                date-format="yy-mm-dd"
                class="w-40"
              />
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Actions -->
    <div class="flex justify-end gap-2 pt-2">
      <Button label="Cancel" text @click="$emit('cancel')" />
      <Button type="submit" label="Save" :loading="isSubmitting" />
    </div>
  </form>
</template>

<script setup lang="ts">
import type { CalendarEvent, EventFormData, RecurrenceRule } from '~/types/calendar';
import { useCalendarStore, toRFC3339 } from '~/stores/calendars';

const props = defineProps<{
  event?: CalendarEvent;
  initialStart?: Date;
  initialEnd?: Date;
  initialAllDay?: boolean;
  isSubmitting?: boolean;
}>();

const emit = defineEmits<{
  submit: [data: EventFormData];
  cancel: [];
}>();

const calendarStore = useCalendarStore();

const timezones = computed(() => {
  try {
    return Intl.supportedValuesOf('timeZone');
  } catch {
    return ['UTC'];
  }
});

const frequencyOptions = [
  { label: 'Daily', value: 'DAILY' },
  { label: 'Weekly', value: 'WEEKLY' },
  { label: 'Monthly', value: 'MONTHLY' },
  { label: 'Yearly', value: 'YEARLY' },
];

const weekDays = [
  { label: 'Mo', value: 'MO' },
  { label: 'Tu', value: 'TU' },
  { label: 'We', value: 'WE' },
  { label: 'Th', value: 'TH' },
  { label: 'Fr', value: 'FR' },
  { label: 'Sa', value: 'SA' },
  { label: 'Su', value: 'SU' },
];

const intervalLabel = computed(() => {
  const labels: Record<string, string> = {
    DAILY: 'day(s)',
    WEEKLY: 'week(s)',
    MONTHLY: 'month(s)',
    YEARLY: 'year(s)',
  };
  return labels[recurrence.frequency] || '';
});

// Form state
const HOUR_MS = 60 * 60 * 1000;

const defaultStart = () => {
  if (props.initialStart) return new Date(props.initialStart);
  if (props.event) return new Date(props.event.start);
  const d = new Date();
  d.setMinutes(0, 0, 0);
  return new Date(d.getTime() + HOUR_MS);
};

const defaultEnd = () => {
  if (props.initialEnd) return new Date(props.initialEnd);
  if (props.event) return new Date(props.event.end);
  return new Date(defaultStart().getTime() + HOUR_MS);
};

const defaultCalendarId = () => {
  if (props.event) return String(props.event.calendar_id);
  const first = calendarStore.writableCalendars[0];
  return first ? first.id : '';
};

const form = reactive({
  summary: props.event?.summary || '',
  description: props.event?.description || '',
  location: props.event?.location || '',
  calendar_id: defaultCalendarId(),
  all_day: props.event?.all_day ?? props.initialAllDay ?? false,
  start: defaultStart(),
  end: defaultEnd(),
  timezone: props.event ? '' : Intl.DateTimeFormat().resolvedOptions().timeZone,
});

// When toggling all-day off, set start to the last full hour
watch(() => form.all_day, (newVal, oldVal) => {
  if (oldVal && !newVal) {
    const now = new Date();
    const start = new Date(form.start);
    start.setHours(now.getHours(), 0, 0, 0);
    form.start = start;
    form.end = new Date(start.getTime() + HOUR_MS);
  }
});

// When start time changes, set end to start + 1 hour
// Watch the timestamp value to detect in-place Date mutations from DatePicker
const isEditing = !!props.event;
watch(() => form.start?.getTime(), (newTime, oldTime) => {
  if (!isEditing && newTime && newTime !== oldTime && !form.all_day) {
    form.end = new Date(newTime + HOUR_MS);
  }
});

// Recurrence state
const enableRecurrence = ref(!!props.event?.recurrence);
const recurrence = reactive({
  frequency: props.event?.recurrence?.frequency || 'WEEKLY',
  interval: props.event?.recurrence?.interval || 1,
  by_day: [...(props.event?.recurrence?.by_day || [])],
  count: props.event?.recurrence?.count || 10,
});
const recurrenceEnd = ref<'never' | 'count' | 'until'>(
  props.event?.recurrence?.count ? 'count' : props.event?.recurrence?.until ? 'until' : 'never'
);
const recurrenceUntilDate = ref<Date | null>(
  props.event?.recurrence?.until ? new Date(props.event.recurrence.until) : null
);

const toggleDay = (day: string) => {
  const idx = recurrence.by_day.indexOf(day);
  if (idx === -1) {
    recurrence.by_day.push(day);
  } else {
    recurrence.by_day.splice(idx, 1);
  }
};

// Validation
const errors = reactive<Record<string, string>>({});

const validate = (): boolean => {
  // Clear previous errors
  Object.keys(errors).forEach(k => delete errors[k]);

  if (!form.summary.trim()) {
    errors.summary = 'Title is required';
  }
  if (!form.calendar_id) {
    errors.calendar_id = 'Please select a calendar';
  }
  if (!form.start) {
    errors.start = 'Start date is required';
  }
  if (!form.end) {
    errors.end = 'End date is required';
  }
  if (form.start && form.end && form.start >= form.end) {
    errors.end = 'End must be after start';
  }

  return Object.keys(errors).length === 0;
};

const handleSubmit = () => {
  if (!validate()) return;

  let recurrenceData: RecurrenceRule | undefined;
  if (enableRecurrence.value) {
    recurrenceData = {
      frequency: recurrence.frequency,
      interval: recurrence.interval,
    };
    if (recurrence.frequency === 'WEEKLY' && recurrence.by_day.length > 0) {
      recurrenceData.by_day = [...recurrence.by_day];
    }
    if (recurrenceEnd.value === 'count') {
      recurrenceData.count = recurrence.count;
    } else if (recurrenceEnd.value === 'until' && recurrenceUntilDate.value) {
      recurrenceData.until = toRFC3339(recurrenceUntilDate.value);
    }
  }

  emit('submit', {
    summary: form.summary.trim(),
    description: form.description,
    location: form.location,
    calendar_id: form.calendar_id,
    all_day: form.all_day,
    start: form.start,
    end: form.end,
    timezone: form.timezone,
    recurrence: recurrenceData,
  });
};

// Helper functions
const getCalendarColor = (id: string) => {
  const cal = calendarStore.calendars.find(c => c.id === id);
  return cal?.color || '#3b82f6';
};

const getCalendarName = (id: string) => {
  const cal = calendarStore.calendars.find(c => c.id === id);
  return cal?.name || '';
};
</script>
