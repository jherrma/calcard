<template>
  <div class="flex h-[calc(100vh-8rem)]">
    <!-- Sidebar -->
    <CalendarSidebar
      :calendars="calendarStore.calendarOptions"
      @toggle-calendar="calendarStore.toggleCalendarVisibility"
      @add-calendar="showAddCalendarDialog = true"
      @create-event="navigateToCreateEvent"
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
</template>

<script setup lang="ts">
import FullCalendar from '@fullcalendar/vue3';
import dayGridPlugin from '@fullcalendar/daygrid';
import timeGridPlugin from '@fullcalendar/timegrid';
import interactionPlugin from '@fullcalendar/interaction';
import type { CalendarOptions, EventClickArg, DateSelectArg, EventDropArg, EventResizeArg } from '@fullcalendar/core';
import CalendarSidebar from '~/components/calendar/CalendarSidebar.vue';
import CalendarToolbar from '~/components/calendar/CalendarToolbar.vue';
import { useCalendarStore } from '~/stores/calendars';
import type { CalendarEvent } from '~/types/calendar';

definePageMeta({
  middleware: 'auth',
  layout: 'default',
});

const calendarStore = useCalendarStore();
const router = useRouter();
const toast = useAppToast();

const calendarRef = ref<InstanceType<typeof FullCalendar>>();
const showAddCalendarDialog = ref(false);

// Fetch calendars on mount
onMounted(async () => {
  await calendarStore.fetchCalendars();
});

// Get calendar color
const getCalendarColor = (calendarId: string) => {
  const calendar = calendarStore.calendars.find(c => c.id === calendarId);
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
    router.push(`/calendar/event/${event.calendar_id}/${event.id}`);
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
      arg.event.extendedProps.calendarId as string,
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
      arg.event.extendedProps.calendarId as string,
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

const navigateToCreateEvent = () => {
  router.push('/calendar/event/new');
};
</script>

<style>
@import '~/assets/css/fullcalendar.css';
</style>
