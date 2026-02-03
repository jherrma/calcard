# Story 035: Event Management UI

## Title
Implement Event Create, Edit, and Delete UI

## Description
As a user, I want to create, edit, and delete calendar events through the web interface so that I can manage my schedule.

## Related Acceptance Criteria

| ID | Criterion |
|----|-----------|
| CD-3.2.4 | Users can create events with title, start time, end time |
| CD-3.2.5 | Users can create all-day events |
| CD-3.2.6 | Users can add event description |
| CD-3.2.7 | Users can add event location |
| CD-3.2.8 | Users can edit existing events |
| CD-3.2.9 | Users can delete events |
| CD-3.2.12 | Users can create recurring events |
| CD-3.2.13 | Users can edit single instance of recurring event |
| CD-3.2.14 | Users can edit all instances of recurring event |
| CD-3.2.15 | Users can delete single instance of recurring event |

## Acceptance Criteria

### Event Detail Dialog

- [ ] Opens when clicking on event in calendar
- [ ] Shows event title, time, location, description
- [ ] Shows calendar name with color
- [ ] Shows recurrence info if recurring
- [ ] Edit button to open edit form
- [ ] Delete button with confirmation
- [ ] Close button

### Create Event Page/Dialog

- [ ] Route: `/calendar/event/new` or dialog
- [ ] Form fields:
  - [ ] Title (required)
  - [ ] Calendar selector (dropdown)
  - [ ] All-day toggle
  - [ ] Start date/time picker
  - [ ] End date/time picker
  - [ ] Timezone selector
  - [ ] Location (optional)
  - [ ] Description (textarea, optional)
  - [ ] Recurrence settings (optional)
- [ ] Validation before submit
- [ ] Save and Cancel buttons
- [ ] Loading state during save
- [ ] Success toast and redirect/close

### Edit Event Page/Dialog

- [ ] Route: `/calendar/event/{id}` or dialog
- [ ] Pre-filled with existing event data
- [ ] Same fields as create
- [ ] For recurring events: prompt for scope
  - [ ] "This event only"
  - [ ] "This and future events"
  - [ ] "All events in series"
- [ ] Save and Cancel buttons
- [ ] Delete button

### Recurrence Settings

- [ ] Toggle to enable recurrence
- [ ] Frequency: Daily, Weekly, Monthly, Yearly
- [ ] Interval: Every N days/weeks/months/years
- [ ] Weekly: Day checkboxes (Mon-Sun)
- [ ] Monthly: Day of month or day of week
- [ ] End condition:
  - [ ] Never
  - [ ] After N occurrences
  - [ ] On date

### Delete Event

- [ ] Confirmation dialog
- [ ] For recurring events: scope selection
- [ ] Loading state during delete
- [ ] Success toast and redirect/close

### Date/Time Pickers

- [ ] Calendar popup for date selection
- [ ] Time picker with dropdown or input
- [ ] Start/End validation (end after start)
- [ ] All-day hides time pickers
- [ ] Default duration: 1 hour

## Technical Notes

### Event Form Component
```vue
<!-- components/calendar/EventForm.vue -->
<template>
  <form @submit.prevent="handleSubmit" class="space-y-6">
    <!-- Title -->
    <div>
      <label class="block text-sm font-medium text-gray-700 mb-1">
        Title <span class="text-red-500">*</span>
      </label>
      <InputText
        v-model="form.summary"
        class="w-full"
        :class="{ 'p-invalid': errors.summary }"
        placeholder="Add title"
      />
      <small v-if="errors.summary" class="p-error">{{ errors.summary }}</small>
    </div>

    <!-- Calendar -->
    <div>
      <label class="block text-sm font-medium text-gray-700 mb-1">
        Calendar
      </label>
      <Dropdown
        v-model="form.calendar_id"
        :options="calendars"
        option-label="name"
        option-value="id"
        class="w-full"
      >
        <template #value="slotProps">
          <div v-if="slotProps.value" class="flex items-center gap-2">
            <span
              class="w-3 h-3 rounded-full"
              :style="{ backgroundColor: getCalendarColor(slotProps.value) }"
            />
            {{ getCalendarName(slotProps.value) }}
          </div>
        </template>
        <template #option="slotProps">
          <div class="flex items-center gap-2">
            <span
              class="w-3 h-3 rounded-full"
              :style="{ backgroundColor: slotProps.option.color }"
            />
            {{ slotProps.option.name }}
          </div>
        </template>
      </Dropdown>
    </div>

    <!-- All-day toggle -->
    <div class="flex items-center gap-2">
      <InputSwitch v-model="form.all_day" />
      <label class="text-sm text-gray-700">All-day event</label>
    </div>

    <!-- Date/Time -->
    <div class="grid grid-cols-2 gap-4">
      <div>
        <label class="block text-sm font-medium text-gray-700 mb-1">
          Start
        </label>
        <Calendar
          v-model="form.start"
          :show-time="!form.all_day"
          :hour-format="24"
          date-format="yy-mm-dd"
          class="w-full"
        />
      </div>
      <div>
        <label class="block text-sm font-medium text-gray-700 mb-1">
          End
        </label>
        <Calendar
          v-model="form.end"
          :show-time="!form.all_day"
          :hour-format="24"
          date-format="yy-mm-dd"
          class="w-full"
          :min-date="form.start"
        />
      </div>
    </div>

    <!-- Timezone -->
    <div v-if="!form.all_day">
      <label class="block text-sm font-medium text-gray-700 mb-1">
        Timezone
      </label>
      <Dropdown
        v-model="form.timezone"
        :options="timezones"
        filter
        class="w-full"
      />
    </div>

    <!-- Location -->
    <div>
      <label class="block text-sm font-medium text-gray-700 mb-1">
        Location
      </label>
      <InputText
        v-model="form.location"
        class="w-full"
        placeholder="Add location"
      />
    </div>

    <!-- Description -->
    <div>
      <label class="block text-sm font-medium text-gray-700 mb-1">
        Description
      </label>
      <Textarea
        v-model="form.description"
        class="w-full"
        rows="3"
        placeholder="Add description"
      />
    </div>

    <!-- Recurrence -->
    <div class="border rounded-lg p-4">
      <div class="flex items-center gap-2 mb-4">
        <InputSwitch v-model="hasRecurrence" />
        <label class="text-sm font-medium text-gray-700">Repeat</label>
      </div>

      <div v-if="hasRecurrence" class="space-y-4">
        <!-- Frequency -->
        <div class="grid grid-cols-2 gap-4">
          <div>
            <label class="block text-sm text-gray-600 mb-1">Repeat every</label>
            <div class="flex gap-2">
              <InputNumber
                v-model="form.recurrence.interval"
                :min="1"
                :max="99"
                class="w-20"
              />
              <Dropdown
                v-model="form.recurrence.frequency"
                :options="frequencyOptions"
                option-label="label"
                option-value="value"
                class="flex-1"
              />
            </div>
          </div>
        </div>

        <!-- Weekly days -->
        <div v-if="form.recurrence.frequency === 'weekly'">
          <label class="block text-sm text-gray-600 mb-2">On days</label>
          <div class="flex gap-2">
            <ToggleButton
              v-for="day in weekDays"
              :key="day.value"
              v-model="form.recurrence.by_day"
              :on-label="day.short"
              :off-label="day.short"
              :value="day.value"
            />
          </div>
        </div>

        <!-- End condition -->
        <div>
          <label class="block text-sm text-gray-600 mb-2">Ends</label>
          <div class="space-y-2">
            <div class="flex items-center gap-2">
              <RadioButton
                v-model="endCondition"
                value="never"
                input-id="end-never"
              />
              <label for="end-never" class="text-sm">Never</label>
            </div>
            <div class="flex items-center gap-2">
              <RadioButton
                v-model="endCondition"
                value="count"
                input-id="end-count"
              />
              <label for="end-count" class="text-sm">After</label>
              <InputNumber
                v-model="form.recurrence.count"
                :min="1"
                :max="999"
                :disabled="endCondition !== 'count'"
                class="w-20"
              />
              <span class="text-sm">occurrences</span>
            </div>
            <div class="flex items-center gap-2">
              <RadioButton
                v-model="endCondition"
                value="until"
                input-id="end-until"
              />
              <label for="end-until" class="text-sm">On</label>
              <Calendar
                v-model="form.recurrence.until"
                :disabled="endCondition !== 'until'"
                date-format="yy-mm-dd"
                class="w-40"
              />
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Actions -->
    <div class="flex justify-end gap-2 pt-4 border-t">
      <Button
        label="Cancel"
        severity="secondary"
        @click="$emit('cancel')"
      />
      <Button
        :label="isEditing ? 'Save' : 'Create'"
        :loading="isSubmitting"
        type="submit"
      />
    </div>
  </form>
</template>

<script setup lang="ts">
import type { CalendarEvent, EventFormData } from '~/types';

const props = defineProps<{
  event?: CalendarEvent;
  initialStart?: Date;
  initialEnd?: Date;
  initialAllDay?: boolean;
}>();

const emit = defineEmits<{
  submit: [data: EventFormData];
  cancel: [];
}>();

const calendarStore = useCalendarStore();

const isEditing = computed(() => !!props.event);
const isSubmitting = ref(false);
const errors = reactive<Record<string, string>>({});

// Form state
const form = reactive<EventFormData>({
  summary: props.event?.summary || '',
  calendar_id: props.event?.calendar_id || calendarStore.calendars[0]?.id || '',
  all_day: props.event?.all_day || props.initialAllDay || false,
  start: props.event ? new Date(props.event.start) : props.initialStart || new Date(),
  end: props.event ? new Date(props.event.end) : props.initialEnd || addHours(new Date(), 1),
  timezone: props.event?.timezone || Intl.DateTimeFormat().resolvedOptions().timeZone,
  location: props.event?.location || '',
  description: props.event?.description || '',
  recurrence: props.event?.recurrence || {
    frequency: 'weekly',
    interval: 1,
    by_day: [],
    count: null,
    until: null,
  },
});

const hasRecurrence = ref(!!props.event?.recurrence);
const endCondition = ref<'never' | 'count' | 'until'>('never');

const frequencyOptions = [
  { label: 'day(s)', value: 'daily' },
  { label: 'week(s)', value: 'weekly' },
  { label: 'month(s)', value: 'monthly' },
  { label: 'year(s)', value: 'yearly' },
];

const weekDays = [
  { value: 'MO', short: 'M', label: 'Monday' },
  { value: 'TU', short: 'T', label: 'Tuesday' },
  { value: 'WE', short: 'W', label: 'Wednesday' },
  { value: 'TH', short: 'T', label: 'Thursday' },
  { value: 'FR', short: 'F', label: 'Friday' },
  { value: 'SA', short: 'S', label: 'Saturday' },
  { value: 'SU', short: 'S', label: 'Sunday' },
];

const timezones = computed(() => {
  return Intl.supportedValuesOf('timeZone');
});

const calendars = computed(() => {
  return calendarStore.calendars.filter(c => !c.shared || c.permission === 'read-write');
});

const getCalendarColor = (id: string) => {
  return calendarStore.calendars.find(c => c.id === id)?.color || '#3788d8';
};

const getCalendarName = (id: string) => {
  return calendarStore.calendars.find(c => c.id === id)?.name || '';
};

// Validation
const validate = () => {
  errors.summary = '';

  if (!form.summary.trim()) {
    errors.summary = 'Title is required';
    return false;
  }

  if (form.end <= form.start) {
    errors.end = 'End time must be after start time';
    return false;
  }

  return true;
};

// Submit
const handleSubmit = async () => {
  if (!validate()) return;

  isSubmitting.value = true;

  const data: EventFormData = {
    ...form,
    recurrence: hasRecurrence.value ? form.recurrence : undefined,
  };

  emit('submit', data);
};

// Helpers
function addHours(date: Date, hours: number) {
  return new Date(date.getTime() + hours * 60 * 60 * 1000);
}
</script>
```

### Event Detail Dialog
```vue
<!-- components/calendar/EventDetailDialog.vue -->
<template>
  <Dialog
    :visible="visible"
    :header="event?.summary"
    :modal="true"
    :style="{ width: '450px' }"
    @update:visible="$emit('update:visible', $event)"
  >
    <div v-if="event" class="space-y-4">
      <!-- Time -->
      <div class="flex items-start gap-3">
        <i class="pi pi-clock text-gray-400 mt-1" />
        <div>
          <div class="text-sm">
            {{ formatEventTime(event) }}
          </div>
          <div v-if="event.recurrence" class="text-xs text-gray-500 mt-1">
            <i class="pi pi-replay mr-1" />
            {{ formatRecurrence(event.recurrence) }}
          </div>
        </div>
      </div>

      <!-- Location -->
      <div v-if="event.location" class="flex items-start gap-3">
        <i class="pi pi-map-marker text-gray-400 mt-1" />
        <div class="text-sm">{{ event.location }}</div>
      </div>

      <!-- Calendar -->
      <div class="flex items-center gap-3">
        <span
          class="w-3 h-3 rounded-full"
          :style="{ backgroundColor: calendarColor }"
        />
        <span class="text-sm">{{ calendarName }}</span>
      </div>

      <!-- Description -->
      <div v-if="event.description" class="pt-2 border-t">
        <p class="text-sm text-gray-600 whitespace-pre-wrap">
          {{ event.description }}
        </p>
      </div>
    </div>

    <template #footer>
      <div class="flex justify-between">
        <Button
          label="Delete"
          icon="pi pi-trash"
          severity="danger"
          text
          @click="confirmDelete"
        />
        <Button
          label="Edit"
          icon="pi pi-pencil"
          @click="$emit('edit', event)"
        />
      </div>
    </template>
  </Dialog>

  <!-- Delete confirmation -->
  <ConfirmDialog />

  <!-- Recurring event scope dialog -->
  <Dialog
    v-model:visible="showScopeDialog"
    header="Delete recurring event"
    :modal="true"
    :style="{ width: '400px' }"
  >
    <p class="mb-4">This is a recurring event. What do you want to delete?</p>
    <div class="space-y-2">
      <div class="flex items-center gap-2">
        <RadioButton v-model="deleteScope" value="this" input-id="scope-this" />
        <label for="scope-this">This event only</label>
      </div>
      <div class="flex items-center gap-2">
        <RadioButton v-model="deleteScope" value="this_and_future" input-id="scope-future" />
        <label for="scope-future">This and future events</label>
      </div>
      <div class="flex items-center gap-2">
        <RadioButton v-model="deleteScope" value="all" input-id="scope-all" />
        <label for="scope-all">All events in the series</label>
      </div>
    </div>
    <template #footer>
      <Button label="Cancel" severity="secondary" @click="showScopeDialog = false" />
      <Button label="Delete" severity="danger" @click="handleDelete" />
    </template>
  </Dialog>
</template>

<script setup lang="ts">
import { useConfirm } from 'primevue/useconfirm';
import type { CalendarEvent } from '~/types';

const props = defineProps<{
  visible: boolean;
  event: CalendarEvent | null;
}>();

const emit = defineEmits<{
  'update:visible': [value: boolean];
  edit: [event: CalendarEvent];
  delete: [event: CalendarEvent, scope?: string];
}>();

const confirm = useConfirm();
const calendarStore = useCalendarStore();

const showScopeDialog = ref(false);
const deleteScope = ref('this');

const calendarColor = computed(() => {
  if (!props.event) return '#3788d8';
  return calendarStore.calendars.find(c => c.id === props.event!.calendar_id)?.color || '#3788d8';
});

const calendarName = computed(() => {
  if (!props.event) return '';
  return calendarStore.calendars.find(c => c.id === props.event!.calendar_id)?.name || '';
});

const formatEventTime = (event: CalendarEvent) => {
  const start = new Date(event.start);
  const end = new Date(event.end);

  if (event.all_day) {
    if (start.toDateString() === end.toDateString()) {
      return start.toLocaleDateString('en-US', { weekday: 'long', month: 'long', day: 'numeric' });
    }
    return `${start.toLocaleDateString()} - ${end.toLocaleDateString()}`;
  }

  const dateStr = start.toLocaleDateString('en-US', { weekday: 'long', month: 'long', day: 'numeric' });
  const timeStr = `${start.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit' })} - ${end.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit' })}`;

  return `${dateStr}\n${timeStr}`;
};

const formatRecurrence = (recurrence: any) => {
  const freq = recurrence.frequency;
  const interval = recurrence.interval || 1;

  let str = interval === 1 ? `Every ${freq.replace('ly', '')}` : `Every ${interval} ${freq.replace('ly', '')}s`;

  if (recurrence.by_day?.length) {
    str += ` on ${recurrence.by_day.join(', ')}`;
  }

  return str;
};

const confirmDelete = () => {
  if (props.event?.is_recurring) {
    showScopeDialog.value = true;
  } else {
    confirm.require({
      message: 'Are you sure you want to delete this event?',
      header: 'Delete Event',
      icon: 'pi pi-exclamation-triangle',
      acceptClass: 'p-button-danger',
      accept: () => emit('delete', props.event!),
    });
  }
};

const handleDelete = () => {
  showScopeDialog.value = false;
  emit('delete', props.event!, deleteScope.value);
};
</script>
```

### Create Event Page
```vue
<!-- pages/calendar/event/new.vue -->
<template>
  <div class="max-w-2xl mx-auto">
    <div class="bg-white rounded-lg shadow p-6">
      <h1 class="text-2xl font-semibold mb-6">Create Event</h1>

      <EventForm
        :initial-start="initialStart"
        :initial-end="initialEnd"
        :initial-all-day="initialAllDay"
        @submit="handleCreate"
        @cancel="router.back()"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
definePageMeta({
  middleware: 'auth',
});

const route = useRoute();
const router = useRouter();
const toast = useAppToast();
const api = useApi();

const initialStart = computed(() => {
  return route.query.start ? new Date(route.query.start as string) : new Date();
});

const initialEnd = computed(() => {
  return route.query.end ? new Date(route.query.end as string) : addHours(new Date(), 1);
});

const initialAllDay = computed(() => {
  return route.query.allDay === 'true';
});

const handleCreate = async (data: EventFormData) => {
  try {
    await api.post(`/api/v1/calendars/${data.calendar_id}/events`, data);
    toast.success('Event created');
    router.push('/calendar');
  } catch (e: any) {
    toast.error(e.message || 'Failed to create event');
  }
};

function addHours(date: Date, hours: number) {
  return new Date(date.getTime() + hours * 60 * 60 * 1000);
}
</script>
```

## Definition of Done

- [ ] Event detail dialog shows all event info
- [ ] Create event form with all fields
- [ ] Edit event form pre-fills data
- [ ] Recurring event creation works
- [ ] Recurring event edit shows scope options
- [ ] Delete event with confirmation
- [ ] Delete recurring event with scope options
- [ ] Date/time pickers work correctly
- [ ] All-day toggle hides time pickers
- [ ] Form validation works
- [ ] Loading states during save/delete
- [ ] Success/error toasts displayed
- [ ] Calendar selector shows colors
- [ ] Timezone selector works
