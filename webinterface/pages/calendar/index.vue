<template>
  <div class="flex h-[calc(100vh-8rem)]">
    <!-- Sidebar -->
    <CalendarSidebar
      :calendars="calendarStore.calendarOptions"
      @toggle-calendar="calendarStore.toggleCalendarVisibility"
      @add-calendar="showAddCalendarDialog = true"
      @create-event="openCreateDialog()"
      @edit-calendar="openCalendarSettings"
      @share-calendar="openCalendarSharing"
      @delete-calendar="handleDeleteCalendar"
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

  <!-- Add Calendar Dialog -->
  <CalendarAddCalendarDialog
    :visible="showAddCalendarDialog"
    @update:visible="showAddCalendarDialog = $event"
    @created="handleCalendarCreated"
  />

  <!-- Share Dialog — the sidebar "Share" action goes straight here rather than
       into the settings dialog's sharing tab (story 043). -->
  <SharingShareDialog
    :visible="showShareDialog"
    resource-type="calendar"
    :resource-uuid="shareCalendar?.uuid"
    :resource-name="shareCalendar?.name"
    :can-manage="!shareCalendar?.shared"
    :public-enabled="shareCalendar?.public_enabled"
    @update:visible="showShareDialog = $event"
    @changed="handleCalendarUpdated"
  />

  <!-- Calendar Settings Dialog -->
  <CalendarSettingsDialog
    :visible="showCalendarSettingsDialog"
    :calendar="settingsCalendar"
    :initial-tab="settingsInitialTab"
    @update:visible="showCalendarSettingsDialog = $event"
    @updated="handleCalendarUpdated"
    @deleted="handleCalendarDeleted"
  />
</template>

<script setup lang="ts">
import FullCalendar from '@fullcalendar/vue3';
import dayGridPlugin from '@fullcalendar/daygrid';
import timeGridPlugin from '@fullcalendar/timegrid';
import interactionPlugin from '@fullcalendar/interaction';
import type { CalendarOptions, EventClickArg, DateSelectArg, EventDropArg } from '@fullcalendar/core';
import type { EventResizeDoneArg } from '@fullcalendar/interaction';
import { useConfirm } from 'primevue/useconfirm';
import CalendarSidebar from '~/components/calendar/CalendarSidebar.vue';
import CalendarToolbar from '~/components/calendar/CalendarToolbar.vue';
import EventDetailDialog from '~/components/calendar/EventDetailDialog.vue';
import EventCreateDialog from '~/components/calendar/EventCreateDialog.vue';
import EventEditDialog from '~/components/calendar/EventEditDialog.vue';
import { useCalendarStore } from '~/stores/calendars';
import { usePreferencesStore } from '~/stores/preferences';
import type { Calendar, CalendarEvent } from '~/types/calendar';

definePageMeta({
  middleware: 'auth',
  layout: 'default',
});

const calendarStore = useCalendarStore();
const preferencesStore = usePreferencesStore();
const toast = useAppToast();
const confirm = useConfirm();
const route = useRoute();

const calendarRef = ref<InstanceType<typeof FullCalendar>>();
const showAddCalendarDialog = ref(false);
const showCalendarSettingsDialog = ref(false);
const settingsCalendar = ref<Calendar | null>(null);
const settingsInitialTab = ref<string | undefined>();
const showShareDialog = ref(false);
const shareCalendar = ref<Calendar | null>(null);

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
  // Preferences drive the 12h/24h display and the defaults EventForm snapshots at
  // setup time, so they must be in the store before any create dialog can open
  // (story 103). ensureLoaded() never rejects, so it can't break this mount.
  await Promise.all([calendarStore.fetchCalendars(), preferencesStore.ensureLoaded()]);
  if (currentDateRange.value) {
    await calendarStore.fetchEvents(currentDateRange.value.start, currentDateRange.value.end);
  }
  // Calendars must be loaded first: resolving an event by id maps the numeric
  // calendar id in the link to the calendar's UUID (#52).
  await applyDeepLink().catch((e: unknown) => console.warn('Deep link failed', e));
});

/** Query params owned by the deep link — stripped once consumed, nothing else is. */
const DEEP_LINK_PARAMS = ['date', 'event', 'cal', 'recurrence'];

/**
 * Deep link from global search (story 044):
 *   /calendar?date=YYYY-MM-DD&event=<event id>&cal=<numeric calendar id>[&recurrence=<recurrence_id>]
 * Jumps the view to that date and opens the event's detail dialog.
 *
 * Watched (not just read once on mount) because the header search lives in the
 * layout: picking a result while already on /calendar changes only the query, which
 * does not remount this page.
 */
const applyDeepLink = async () => {
  const q = route.query;
  const str = (v: unknown) => (typeof v === 'string' ? v : '');
  const dateParam = str(q.date);
  const eventParam = str(q.event);
  const calParam = str(q.cal);
  const recurrenceParam = str(q.recurrence);

  if (!dateParam && !eventParam) return;

  // Parsed as local midnight (a bare 'YYYY-MM-DD' would be read as UTC and can
  // land on the previous day west of Greenwich).
  const target = dateParam ? new Date(`${dateParam}T00:00:00`) : null;
  const validTarget = target && !Number.isNaN(target.getTime()) ? target : null;

  if (validTarget) {
    calendarStore.setCurrentDate(validTarget);
    // On first load FullCalendar hasn't mounted yet (it sits behind ClientOnly)
    // and picks the date up via initialDate; afterwards gotoDate is needed.
    calendarRef.value?.getApi().gotoDate(validTarget);
  }

  if (eventParam) {
    // Prefer the loaded occurrence so a recurring instance opens with its own
    // start/end.
    const local = calendarStore.events.find(
      e => e.id === eventParam && (e.recurrence_id || '') === recurrenceParam
    );
    if (local) {
      selectedEvent.value = local;
      showDetailDialog.value = true;
    } else if (calParam) {
      // NOT loaded: the target date is usually outside the range FullCalendar has
      // fetched (the refetch triggered by gotoDate is async and hasn't landed), so
      // `events` legitimately misses. GET /events/:id is no help for a recurring
      // series — it returns the MASTER and ignores any recurrence hint — which used
      // to open a dialog showing the first occurrence's date while the grid sat on
      // the clicked one. Resolve the occurrence from the expanded day instead, and
      // only fall back to the single-event endpoint for non-recurring links.
      try {
        const resolved = recurrenceParam && validTarget
          ? await calendarStore.fetchEventOccurrence(calParam, eventParam, recurrenceParam, validTarget)
          : await calendarStore.getEvent(calParam, eventParam);
        if (resolved) {
          selectedEvent.value = resolved;
          showDetailDialog.value = true;
        } else {
          // The occurrence was deleted or moved since the search results were built.
          toast.warn('That event occurrence no longer exists');
        }
      } catch {
        toast.error('Could not open that event');
      }
    }
  }

  // Consume ONLY the deep-link params — anything else on /calendar belongs to
  // another feature. Without this, choosing the same search result twice would be
  // a no-op (identical route → no navigation, no watcher) and a page reload would
  // silently reopen the dialog.
  const rest = Object.fromEntries(
    Object.entries(route.query).filter(([key]) => !DEEP_LINK_PARAMS.includes(key))
  );
  await navigateTo({ path: '/calendar', query: rest }, { replace: true });
};

// Floating promise: nothing awaits the watcher, so swallow rejections explicitly
// (an aborted navigation inside applyDeepLink would otherwise surface as an
// unhandled rejection).
watch(() => route.query, () => {
  applyDeepLink().catch((e: unknown) => console.warn('Deep link failed', e));
});

// Get calendar color
const getCalendarColor = (calendarId: number) => {
  const calendar = calendarStore.calendars.find(c => String(c.id) === String(calendarId));
  return calendar?.color || '#3b82f6';
};

// Map store events to FullCalendar event objects
const mappedEvents = computed(() =>
  calendarStore.visibleEvents.map(event => ({
    // Expanded occurrences of a recurring event all carry the same backend id
    // (the series UUID) and differ only by recurrence_id. FullCalendar needs a
    // unique id per rendered event, and click/drag handlers need to know which
    // occurrence was touched, so make the FC id composite and carry the real
    // identifiers in extendedProps.
    id: event.recurrence_id ? `${event.id}::${event.recurrence_id}` : event.id,
    title: event.summary,
    start: event.start,
    end: event.end,
    allDay: event.all_day,
    backgroundColor: getCalendarColor(event.calendar_id),
    borderColor: getCalendarColor(event.calendar_id),
    extendedProps: {
      eventId: event.id, // real backend id for API calls
      recurrenceId: event.recurrence_id,
      calendarId: event.calendar_id,
      description: event.description,
      location: event.location,
    },
  }))
);

const is12h = computed(() => preferencesStore.timeFormat === '12h');

// FullCalendar options
const calendarOptions = computed<CalendarOptions>(() => ({
  plugins: [dayGridPlugin, timeGridPlugin, interactionPlugin],
  initialView: calendarStore.currentView,
  initialDate: calendarStore.currentDate,
  headerToolbar: false, // We use custom toolbar

  events: mappedEvents.value,

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

  // Time display follows the user's 12h/24h preference (story 103). Both formats
  // are spelled out because FullCalendar's defaults differ per view.
  slotLabelFormat: {
    hour: 'numeric' as const,
    minute: '2-digit' as const,
    hour12: is12h.value,
  },
  eventTimeFormat: {
    hour: 'numeric' as const,
    minute: '2-digit' as const,
    hour12: is12h.value,
  },

  // Display options
  nowIndicator: true,
  dayMaxEvents: true,
  weekends: true,
  // Show the full day so events before 06:00 / after 22:00 (night shifts, early
  // flights) stay reachable — the old 06:00–22:00 clamp hid them with no way to
  // scroll to them (#28). scrollTime keeps the viewport opening in the morning.
  slotMinTime: '00:00:00',
  slotMaxTime: '24:00:00',
  slotDuration: '00:30:00',
  scrollTime: '07:00:00',

  // Responsive
  height: '100%',
}));

// Event handlers
const handleEventClick = (arg: EventClickArg) => {
  // Resolve the clicked occurrence by both the real event id and its
  // recurrence_id — matching on arg.event.id alone always resolved to the
  // first occurrence, since every occurrence shared the series UUID.
  const eventId = arg.event.extendedProps.eventId as string;
  const recurrenceId = arg.event.extendedProps.recurrenceId as string | undefined;
  const event = calendarStore.events.find(
    e => e.id === eventId && (e.recurrence_id || '') === (recurrenceId || '')
  );
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
  const recurrenceId = arg.event.extendedProps.recurrenceId as string | undefined;
  try {
    await calendarStore.updateEventTime(
      arg.event.extendedProps.eventId as string,
      String(arg.event.extendedProps.calendarId),
      arg.event.start!,
      arg.event.end || arg.event.start!,
      recurrenceId ? 'this' : undefined,
      recurrenceId
    );
    // A scoped update creates a RECURRENCE-ID exception, which changes what the
    // series returns — refetch the visible range instead of trusting local state.
    if (recurrenceId && currentDateRange.value) {
      await calendarStore.fetchEvents(currentDateRange.value.start, currentDateRange.value.end);
    }
    toast.success('Event rescheduled');
  } catch {
    arg.revert();
    toast.error('Failed to reschedule event');
  }
};

const handleEventResize = async (arg: EventResizeDoneArg) => {
  const recurrenceId = arg.event.extendedProps.recurrenceId as string | undefined;
  try {
    await calendarStore.updateEventTime(
      arg.event.extendedProps.eventId as string,
      String(arg.event.extendedProps.calendarId),
      arg.event.start!,
      arg.event.end!,
      recurrenceId ? 'this' : undefined,
      recurrenceId
    );
    // Scoped resize creates a RECURRENCE-ID exception — refetch the visible range.
    if (recurrenceId && currentDateRange.value) {
      await calendarStore.fetchEvents(currentDateRange.value.start, currentDateRange.value.end);
    }
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

// Calendar settings/management
const openCalendarSettings = (calendar: Calendar) => {
  settingsCalendar.value = calendar;
  settingsInitialTab.value = 'general';
  showCalendarSettingsDialog.value = true;
};

const openCalendarSharing = (calendar: Calendar) => {
  shareCalendar.value = calendar;
  showShareDialog.value = true;
};

const handleDeleteCalendar = (calendar: Calendar) => {
  confirm.require({
    message: `Are you sure you want to delete "${calendar.name}"? All events will be permanently deleted.`,
    header: 'Delete Calendar',
    icon: 'pi pi-exclamation-triangle',
    acceptClass: 'p-button-danger',
    accept: async () => {
      try {
        await calendarStore.deleteCalendar(calendar.uuid);
        toast.success('Calendar deleted');
      } catch (e: unknown) {
        toast.error((e as Error).message || 'Failed to delete calendar');
      }
    },
  });
};

const handleCalendarCreated = () => {
  if (currentDateRange.value) {
    calendarStore.fetchEvents(currentDateRange.value.start, currentDateRange.value.end);
  }
};

const handleCalendarUpdated = () => {
  calendarStore.fetchCalendars();
};

const handleCalendarDeleted = () => {
  // Events are already removed from store by deleteCalendar action
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

const VALID_VIEWS = ['dayGridMonth', 'timeGridWeek', 'timeGridDay'] as const;
type CalendarViewName = (typeof VALID_VIEWS)[number];

const changeView = (view: string | null | undefined) => {
  // The view SelectButton can emit null when deselected; passing that straight
  // to FullCalendar's changeView() throws and blanks the calendar. Ignore any
  // value that isn't a known view (belt-and-suspenders alongside allowEmpty=false).
  if (!view || !VALID_VIEWS.includes(view as CalendarViewName)) return;
  calendarRef.value?.getApi().changeView(view);
  calendarStore.setCurrentView(view as CalendarViewName);
};
</script>

<style>
@import '~/assets/css/fullcalendar.css';
</style>
