<template>
  <div class="flex h-[calc(100vh-8rem)]">
    <!-- Sidebar -->
    <CalendarSidebar
      :calendars="calendarStore.calendarOptions"
      @toggle-calendar="calendarStore.toggleCalendarVisibility"
      @add-calendar="showAddCalendarDialog = true"
      @create-event="openCreateDialog()"
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
      />

      <!-- Calendar -->
      <div class="flex-1 p-4 overflow-hidden bg-surface-0 dark:bg-surface-900">
        <ClientOnly>
          <FullCalendar
            ref="calendarRef"
            :options="calendarOptions"
            class="h-full"
          />
          <template #fallback>
            <div class="flex items-center justify-center h-full">
              <ProgressSpinner />
            </div>
          </template>
        </ClientOnly>
      </div>
    </div>
  </div>

  <!-- Event Detail Dialog -->
  <EventDetailDialog
    :visible="showDetailDialog"
    :event="selectedEvent"
    @update:visible="showDetailDialog = $event"
    @edit="handleEditFromDetail"
    @delete="handleDeleteFromDetail"
  />

  <!-- Event Create Dialog -->
  <EventCreateDialog
    :visible="showCreateDialog"
    :initial-start="createInitialStart"
    :initial-end="createInitialEnd"
    :initial-all-day="createInitialAllDay"
    @update:visible="showCreateDialog = $event"
    @created="handleEventCreated"
  />

  <!-- Event Edit Dialog -->
  <EventEditDialog
    :visible="showEditDialog"
    :event="selectedEvent"
    @update:visible="showEditDialog = $event"
    @updated="handleEventUpdated"
  />
</template>

<script setup lang="ts">
import FullCalendar from '@fullcalendar/vue3';
import dayGridPlugin from '@fullcalendar/daygrid';
import timeGridPlugin from '@fullcalendar/timegrid';
import interactionPlugin from '@fullcalendar/interaction';
import type { CalendarOptions, EventClickArg, DateSelectArg, EventDropArg } from '@fullcalendar/core';
import type { EventResizeDoneArg } from '@fullcalendar/interaction';
import CalendarSidebar from '~/components/calendar/CalendarSidebar.vue';
import CalendarToolbar from '~/components/calendar/CalendarToolbar.vue';
import EventDetailDialog from '~/components/calendar/EventDetailDialog.vue';
import EventCreateDialog from '~/components/calendar/EventCreateDialog.vue';
import EventEditDialog from '~/components/calendar/EventEditDialog.vue';
import { useCalendarStore } from '~/stores/calendars';
import type { CalendarEvent } from '~/types/calendar';

definePageMeta({
  middleware: 'auth',
  layout: 'default',
});

const calendarStore = useCalendarStore();
const toast = useAppToast();

const calendarRef = ref<InstanceType<typeof FullCalendar>>();
const showAddCalendarDialog = ref(false);

// Dialog state
const showDetailDialog = ref(false);
const showCreateDialog = ref(false);
const showEditDialog = ref(false);
const selectedEvent = ref<CalendarEvent | null>(null);
const createInitialStart = ref<Date | undefined>();
const createInitialEnd = ref<Date | undefined>();
const createInitialAllDay = ref<boolean | undefined>();

// Track current date range for refetching
const currentDateRange = ref<{ start: Date; end: Date } | null>(null);

// Fetch calendars on mount, then refetch events (datesSet may fire before calendars load)
onMounted(async () => {
  await calendarStore.fetchCalendars();
  if (currentDateRange.value) {
    await calendarStore.fetchEvents(currentDateRange.value.start, currentDateRange.value.end);
  }
});

// Get calendar color
const getCalendarColor = (calendarId: number) => {
  const calendar = calendarStore.calendars.find(c => c.id === String(calendarId));
  return calendar?.color || '#3b82f6';
};

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

// Event handlers
const handleEventClick = (arg: EventClickArg) => {
  const event = calendarStore.events.find(e => e.id === arg.event.id);
  if (event) {
    selectedEvent.value = event;
    showDetailDialog.value = true;
  }
};

const handleDateSelect = (arg: DateSelectArg) => {
  createInitialStart.value = arg.start;
  createInitialEnd.value = arg.end;
  createInitialAllDay.value = arg.allDay;
  showCreateDialog.value = true;
};

const handleEventDrop = async (arg: EventDropArg) => {
  try {
    await calendarStore.updateEventTime(
      arg.event.id,
      String(arg.event.extendedProps.calendarId),
      arg.event.start!,
      arg.event.end || arg.event.start!
    );
    toast.success('Event rescheduled');
  } catch {
    arg.revert();
    toast.error('Failed to reschedule event');
  }
};

const handleEventResize = async (arg: EventResizeDoneArg) => {
  try {
    await calendarStore.updateEventTime(
      arg.event.id,
      String(arg.event.extendedProps.calendarId),
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
  currentDateRange.value = { start: arg.start, end: arg.end };
  calendarStore.fetchEvents(arg.start, arg.end);
};

// Dialog handlers
const openCreateDialog = () => {
  createInitialStart.value = undefined;
  createInitialEnd.value = undefined;
  createInitialAllDay.value = undefined;
  showCreateDialog.value = true;
};

const handleEditFromDetail = (event: CalendarEvent) => {
  showDetailDialog.value = false;
  selectedEvent.value = event;
  showEditDialog.value = true;
};

const handleDeleteFromDetail = async (event: CalendarEvent, scope?: string) => {
  try {
    await calendarStore.deleteEvent(String(event.calendar_id), event.id, scope, event.recurrence_id);
    showDetailDialog.value = false;
    toast.success('Event deleted');
    if (scope && currentDateRange.value) {
      await calendarStore.fetchEvents(currentDateRange.value.start, currentDateRange.value.end);
    }
  } catch (e: unknown) {
    toast.error((e as Error).message || 'Failed to delete event');
  }
};

const handleEventCreated = () => {
  if (currentDateRange.value) {
    calendarStore.fetchEvents(currentDateRange.value.start, currentDateRange.value.end);
  }
};

const handleEventUpdated = () => {
  if (currentDateRange.value) {
    calendarStore.fetchEvents(currentDateRange.value.start, currentDateRange.value.end);
  }
};

// Navigation
const goToToday = () => {
  calendarRef.value?.getApi().today();
  calendarStore.setCurrentDate(new Date());
};

const goToPrev = () => {
  calendarRef.value?.getApi().prev();
  const date = calendarRef.value?.getApi().getDate();
  if (date) calendarStore.setCurrentDate(date);
};

const goToNext = () => {
  calendarRef.value?.getApi().next();
  const date = calendarRef.value?.getApi().getDate();
  if (date) calendarStore.setCurrentDate(date);
};

const changeView = (view: string) => {
  calendarRef.value?.getApi().changeView(view);
  calendarStore.setCurrentView(view as 'dayGridMonth' | 'timeGridWeek' | 'timeGridDay');
};
</script>

<style>
@import '~/assets/css/fullcalendar.css';
</style>
