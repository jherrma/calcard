# Story 034: Calendar Views

## Title
Implement Calendar Views with FullCalendar

## Description
As a user, I want to view my calendars in month, week, and day views so that I can see my schedule at different levels of detail.

## Related Acceptance Criteria

| ID | Criterion |
|----|-----------|
| CD-3.2.1 | Users can view calendar in month view |
| CD-3.2.2 | Users can view calendar in week view |
| CD-3.2.3 | Users can view calendar in day view |
| CD-3.2.10 | Users can drag-and-drop events to reschedule |
| CD-3.2.11 | Users can resize events to change duration |

## Acceptance Criteria

### Calendar Page

- [ ] Route: `/calendar`
- [ ] FullCalendar component integrated
- [ ] View switcher (Month, Week, Day)
- [ ] Today button to jump to current date
- [ ] Previous/Next navigation
- [ ] Date picker for quick navigation
- [ ] Calendar list sidebar (show/hide calendars)

### Month View

- [ ] Grid of days showing current month
- [ ] Events displayed as colored bars
- [ ] Multi-day events span across days
- [ ] Event count indicator for days with many events
- [ ] Click on day to create event
- [ ] Click on event to view/edit

### Week View

- [ ] 7-day view with time grid
- [ ] All-day events in header row
- [ ] Time slots (configurable: 30min/1hr)
- [ ] Current time indicator line
- [ ] Scroll to current time on load
- [ ] Events sized by duration

### Day View

- [ ] Single day with time grid
- [ ] All-day events in header
- [ ] More detailed event display
- [ ] Current time indicator

### Calendar Sidebar

- [ ] List of all calendars (owned + shared)
- [ ] Checkbox to show/hide each calendar
- [ ] Calendar color indicator
- [ ] "Add Calendar" button
- [ ] Calendar actions menu (edit, share, delete)

### Event Interactions

- [ ] Click event to open detail popup
- [ ] Drag to reschedule (calls API)
- [ ] Resize to change duration (calls API)
- [ ] Double-click empty slot to create event
- [ ] Visual feedback during drag/resize

### Loading & Error States

- [ ] Loading skeleton while fetching events
- [ ] Error message if fetch fails
- [ ] Retry button on error
- [ ] Optimistic updates for drag/resize

## Technical Notes

### Calendar Store
```typescript
// stores/calendars.ts
import { defineStore } from 'pinia';
import type { Calendar, CalendarEvent, EventsQuery } from '~/types';

interface CalendarState {
  calendars: Calendar[];
  events: CalendarEvent[];
  visibleCalendarIds: Set<string>;
  isLoading: boolean;
  error: string | null;
  currentView: 'dayGridMonth' | 'timeGridWeek' | 'timeGridDay';
  currentDate: Date;
}

export const useCalendarStore = defineStore('calendars', {
  state: (): CalendarState => ({
    calendars: [],
    events: [],
    visibleCalendarIds: new Set(),
    isLoading: false,
    error: null,
    currentView: 'dayGridMonth',
    currentDate: new Date(),
  }),

  getters: {
    visibleEvents: (state) => {
      return state.events.filter(e => state.visibleCalendarIds.has(e.calendar_id));
    },

    calendarOptions: (state) => {
      return state.calendars.map(cal => ({
        ...cal,
        visible: state.visibleCalendarIds.has(cal.id),
      }));
    },
  },

  actions: {
    async fetchCalendars() {
      const api = useApi();
      const response = await api.get<{ calendars: Calendar[] }>('/api/v1/calendars');
      this.calendars = response.calendars;

      // Initially show all calendars
      this.visibleCalendarIds = new Set(this.calendars.map(c => c.id));
    },

    async fetchEvents(start: Date, end: Date) {
      this.isLoading = true;
      this.error = null;

      try {
        const api = useApi();
        const allEvents: CalendarEvent[] = [];

        // Fetch events for each visible calendar
        for (const calId of this.visibleCalendarIds) {
          const response = await api.get<{ events: CalendarEvent[] }>(
            `/api/v1/calendars/${calId}/events?start=${start.toISOString()}&end=${end.toISOString()}`
          );
          allEvents.push(...response.events);
        }

        this.events = allEvents;
      } catch (e: any) {
        this.error = e.message || 'Failed to load events';
      } finally {
        this.isLoading = false;
      }
    },

    toggleCalendarVisibility(calendarId: string) {
      if (this.visibleCalendarIds.has(calendarId)) {
        this.visibleCalendarIds.delete(calendarId);
      } else {
        this.visibleCalendarIds.add(calendarId);
      }
    },

    async updateEventTime(eventId: string, calendarId: string, start: Date, end: Date) {
      const api = useApi();
      await api.patch(`/api/v1/calendars/${calendarId}/events/${eventId}`, {
        start: start.toISOString(),
        end: end.toISOString(),
      });

      // Update local state
      const event = this.events.find(e => e.id === eventId);
      if (event) {
        event.start = start.toISOString();
        event.end = end.toISOString();
      }
    },
  },
});
```

### Calendar Page
```vue
<!-- pages/calendar/index.vue -->
<template>
  <div class="flex h-[calc(100vh-8rem)]">
    <!-- Sidebar -->
    <CalendarSidebar
      :calendars="calendarStore.calendarOptions"
      @toggle-calendar="calendarStore.toggleCalendarVisibility"
      @add-calendar="showAddCalendarDialog = true"
    />

    <!-- Main calendar area -->
    <div class="flex-1 flex flex-col min-w-0">
      <!-- Toolbar -->
      <CalendarToolbar
        :current-date="calendarStore.currentDate"
        :current-view="calendarStore.currentView"
        @today="goToToday"
        @prev="goToPrev"
        @next="goToNext"
        @view-change="changeView"
        @date-change="goToDate"
      />

      <!-- Calendar -->
      <div class="flex-1 p-4 overflow-hidden">
        <FullCalendar
          ref="calendarRef"
          :options="calendarOptions"
          class="h-full"
        />
      </div>
    </div>

    <!-- Event detail dialog -->
    <EventDetailDialog
      v-model:visible="showEventDetail"
      :event="selectedEvent"
      @edit="editEvent"
      @delete="deleteEvent"
    />

    <!-- Add calendar dialog -->
    <AddCalendarDialog
      v-model:visible="showAddCalendarDialog"
      @created="onCalendarCreated"
    />
  </div>
</template>

<script setup lang="ts">
import FullCalendar from '@fullcalendar/vue3';
import dayGridPlugin from '@fullcalendar/daygrid';
import timeGridPlugin from '@fullcalendar/timegrid';
import interactionPlugin from '@fullcalendar/interaction';
import type { CalendarOptions, EventClickArg, DateSelectArg, EventDropArg, EventResizeArg } from '@fullcalendar/core';

definePageMeta({
  middleware: 'auth',
});

const calendarStore = useCalendarStore();
const router = useRouter();
const toast = useAppToast();

const calendarRef = ref<InstanceType<typeof FullCalendar>>();
const showEventDetail = ref(false);
const selectedEvent = ref<CalendarEvent | null>(null);
const showAddCalendarDialog = ref(false);

// Fetch calendars on mount
onMounted(async () => {
  await calendarStore.fetchCalendars();
});

// FullCalendar options
const calendarOptions = computed<CalendarOptions>(() => ({
  plugins: [dayGridPlugin, timeGridPlugin, interactionPlugin],
  initialView: calendarStore.currentView,
  initialDate: calendarStore.currentDate,
  headerToolbar: false, // We use custom toolbar

  // Events
  events: calendarStore.visibleEvents.map(event => ({
    id: event.id,
    title: event.summary,
    start: event.start,
    end: event.end,
    allDay: event.all_day,
    backgroundColor: getCalendarColor(event.calendar_id),
    borderColor: getCalendarColor(event.calendar_id),
    extendedProps: {
      calendarId: event.calendar_id,
      description: event.description,
      location: event.location,
    },
  })),

  // Interactions
  editable: true,
  selectable: true,
  selectMirror: true,

  // Event handlers
  eventClick: handleEventClick,
  select: handleDateSelect,
  eventDrop: handleEventDrop,
  eventResize: handleEventResize,
  datesSet: handleDatesSet,

  // Display options
  nowIndicator: true,
  dayMaxEvents: true,
  weekends: true,
  slotMinTime: '06:00:00',
  slotMaxTime: '22:00:00',
  slotDuration: '00:30:00',

  // Responsive
  height: '100%',
}));

// Get calendar color
const getCalendarColor = (calendarId: string) => {
  const calendar = calendarStore.calendars.find(c => c.id === calendarId);
  return calendar?.color || '#3788d8';
};

// Event handlers
const handleEventClick = (arg: EventClickArg) => {
  const event = calendarStore.events.find(e => e.id === arg.event.id);
  if (event) {
    selectedEvent.value = event;
    showEventDetail.value = true;
  }
};

const handleDateSelect = (arg: DateSelectArg) => {
  // Navigate to create event page with pre-filled dates
  router.push({
    path: '/calendar/event/new',
    query: {
      start: arg.startStr,
      end: arg.endStr,
      allDay: arg.allDay.toString(),
    },
  });
};

const handleEventDrop = async (arg: EventDropArg) => {
  try {
    await calendarStore.updateEventTime(
      arg.event.id,
      arg.event.extendedProps.calendarId,
      arg.event.start!,
      arg.event.end || arg.event.start!
    );
    toast.success('Event rescheduled');
  } catch {
    arg.revert();
    toast.error('Failed to reschedule event');
  }
};

const handleEventResize = async (arg: EventResizeArg) => {
  try {
    await calendarStore.updateEventTime(
      arg.event.id,
      arg.event.extendedProps.calendarId,
      arg.event.start!,
      arg.event.end!
    );
    toast.success('Event duration updated');
  } catch {
    arg.revert();
    toast.error('Failed to update event');
  }
};

const handleDatesSet = (arg: { start: Date; end: Date }) => {
  calendarStore.fetchEvents(arg.start, arg.end);
};

// Navigation
const goToToday = () => {
  calendarRef.value?.getApi().today();
  calendarStore.currentDate = new Date();
};

const goToPrev = () => {
  calendarRef.value?.getApi().prev();
  calendarStore.currentDate = calendarRef.value?.getApi().getDate() || new Date();
};

const goToNext = () => {
  calendarRef.value?.getApi().next();
  calendarStore.currentDate = calendarRef.value?.getApi().getDate() || new Date();
};

const goToDate = (date: Date) => {
  calendarRef.value?.getApi().gotoDate(date);
  calendarStore.currentDate = date;
};

const changeView = (view: string) => {
  calendarRef.value?.getApi().changeView(view);
  calendarStore.currentView = view as any;
};

// Actions
const editEvent = (event: CalendarEvent) => {
  showEventDetail.value = false;
  router.push(`/calendar/event/${event.id}`);
};

const deleteEvent = async (event: CalendarEvent) => {
  // Handle in dialog
};

const onCalendarCreated = () => {
  calendarStore.fetchCalendars();
};
</script>
```

### Calendar Sidebar
```vue
<!-- components/calendar/CalendarSidebar.vue -->
<template>
  <aside class="w-64 bg-white border-r flex flex-col">
    <div class="p-4 border-b">
      <Button
        label="New Event"
        icon="pi pi-plus"
        class="w-full"
        @click="$emit('create-event')"
      />
    </div>

    <div class="flex-1 overflow-y-auto p-4">
      <div class="flex items-center justify-between mb-3">
        <h3 class="text-sm font-semibold text-gray-700">My Calendars</h3>
        <button
          class="text-gray-400 hover:text-gray-600"
          @click="$emit('add-calendar')"
        >
          <i class="pi pi-plus text-sm" />
        </button>
      </div>

      <div class="space-y-1">
        <div
          v-for="calendar in calendars"
          :key="calendar.id"
          class="flex items-center gap-2 p-2 rounded-lg hover:bg-gray-50 group"
        >
          <Checkbox
            :model-value="calendar.visible"
            :binary="true"
            @change="$emit('toggle-calendar', calendar.id)"
          />
          <span
            class="w-3 h-3 rounded-full flex-shrink-0"
            :style="{ backgroundColor: calendar.color }"
          />
          <span class="flex-1 text-sm truncate">{{ calendar.name }}</span>
          <button
            class="opacity-0 group-hover:opacity-100 text-gray-400 hover:text-gray-600"
            @click="showCalendarMenu($event, calendar)"
          >
            <i class="pi pi-ellipsis-v text-sm" />
          </button>
        </div>
      </div>

      <!-- Shared calendars -->
      <div v-if="sharedCalendars.length > 0" class="mt-6">
        <h3 class="text-sm font-semibold text-gray-700 mb-3">Shared with me</h3>
        <div class="space-y-1">
          <div
            v-for="calendar in sharedCalendars"
            :key="calendar.id"
            class="flex items-center gap-2 p-2 rounded-lg hover:bg-gray-50"
          >
            <Checkbox
              :model-value="calendar.visible"
              :binary="true"
              @change="$emit('toggle-calendar', calendar.id)"
            />
            <span
              class="w-3 h-3 rounded-full flex-shrink-0"
              :style="{ backgroundColor: calendar.color }"
            />
            <div class="flex-1 min-w-0">
              <span class="text-sm truncate block">{{ calendar.name }}</span>
              <span class="text-xs text-gray-500">{{ calendar.owner?.display_name }}</span>
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
import type { Calendar } from '~/types';

const props = defineProps<{
  calendars: (Calendar & { visible: boolean })[];
}>();

const emit = defineEmits<{
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
```

### Calendar Toolbar
```vue
<!-- components/calendar/CalendarToolbar.vue -->
<template>
  <div class="flex items-center justify-between p-4 bg-white border-b">
    <div class="flex items-center gap-2">
      <Button
        label="Today"
        severity="secondary"
        size="small"
        @click="$emit('today')"
      />
      <ButtonGroup>
        <Button
          icon="pi pi-chevron-left"
          severity="secondary"
          size="small"
          @click="$emit('prev')"
        />
        <Button
          icon="pi pi-chevron-right"
          severity="secondary"
          size="small"
          @click="$emit('next')"
        />
      </ButtonGroup>
      <h2 class="text-lg font-semibold text-gray-900 ml-4">
        {{ formattedDate }}
      </h2>
    </div>

    <div class="flex items-center gap-2">
      <SelectButton
        :model-value="currentView"
        :options="viewOptions"
        option-label="label"
        option-value="value"
        @update:model-value="$emit('view-change', $event)"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
const props = defineProps<{
  currentDate: Date;
  currentView: string;
}>();

defineEmits<{
  today: [];
  prev: [];
  next: [];
  'view-change': [view: string];
  'date-change': [date: Date];
}>();

const viewOptions = [
  { label: 'Month', value: 'dayGridMonth' },
  { label: 'Week', value: 'timeGridWeek' },
  { label: 'Day', value: 'timeGridDay' },
];

const formattedDate = computed(() => {
  const options: Intl.DateTimeFormatOptions = {
    month: 'long',
    year: 'numeric',
  };

  if (props.currentView === 'timeGridDay') {
    options.day = 'numeric';
  }

  return props.currentDate.toLocaleDateString('en-US', options);
});
</script>
```

## Styling

```css
/* assets/css/fullcalendar.css */
.fc {
  --fc-border-color: #e5e7eb;
  --fc-today-bg-color: #f0f9ff;
}

.fc .fc-button-primary {
  @apply bg-primary-500 border-primary-500;
}

.fc .fc-button-primary:hover {
  @apply bg-primary-600 border-primary-600;
}

.fc .fc-event {
  @apply cursor-pointer;
}

.fc .fc-daygrid-event {
  @apply rounded px-1;
}

.fc .fc-timegrid-event {
  @apply rounded;
}
```

## Definition of Done

- [ ] FullCalendar integrated and displaying
- [ ] Month, Week, Day views working
- [ ] View switching functional
- [ ] Today/Prev/Next navigation working
- [ ] Calendar sidebar shows all calendars
- [ ] Calendar visibility toggles work
- [ ] Events display with correct colors
- [ ] Click on event opens detail popup
- [ ] Drag-and-drop reschedules events
- [ ] Resize changes event duration
- [ ] Click empty slot to create event
- [ ] Loading states displayed
- [ ] Error handling implemented
- [ ] Responsive on mobile/tablet
