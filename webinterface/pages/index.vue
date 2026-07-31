<template>
  <div class="max-w-[1400px] mx-auto space-y-6">
    <!-- Header -->
    <header class="flex flex-wrap items-start justify-between gap-4">
      <div>
        <h1 class="text-2xl font-bold text-surface-900 dark:text-surface-0">
          {{ greeting }}
        </h1>
        <p class="text-sm text-surface-500 dark:text-surface-400 mt-1">{{ formattedToday }}</p>
      </div>
      <Button
        label="Refresh"
        icon="pi pi-refresh"
        severity="secondary"
        outlined
        size="small"
        :loading="dashboardStore.isLoadingEvents || dashboardStore.isLoadingContacts"
        @click="dashboardStore.refresh()"
      />
    </header>

    <!-- Partial-failure notice. Per-resource failures are swallowed so one dead
         calendar/address book degrades a single card instead of blanking the
         page — but the user still has to know the numbers are incomplete. -->
    <Message
      v-if="dashboardStore.eventsIncomplete || dashboardStore.contactsIncomplete"
      severity="warn"
      :closable="false"
    >
      Some data could not be loaded, so counts may be incomplete. Try refreshing.
    </Message>

    <!-- Quick actions -->
    <div class="flex flex-wrap gap-3">
      <Button
        label="New Event"
        icon="pi pi-calendar-plus"
        size="small"
        :disabled="!canCreateEvent"
        :title="canCreateEvent ? undefined : 'You have no calendar you can write to'"
        @click="showCreateEventDialog = true"
      />
      <Button
        label="New Contact"
        icon="pi pi-user-plus"
        severity="secondary"
        size="small"
        :disabled="!canCreateContact"
        :title="canCreateContact ? undefined : 'You have no address book you can write to'"
        @click="navigateTo('/contacts/new')"
      />
      <Button
        label="Import / Export"
        icon="pi pi-upload"
        severity="secondary"
        outlined
        size="small"
        @click="navigateTo('/settings/import-export')"
      />
    </div>

    <!-- Widgets. Single column on phones, two on tablets, three on wide screens. -->
    <div class="grid gap-6 md:grid-cols-2 xl:grid-cols-3">
      <DashboardTodayAgendaCard
        :all-day-events="dashboardStore.todayAllDayEvents"
        :timed-events="dashboardStore.todayTimedEvents"
        :now="dashboardStore.now"
        :loading="dashboardStore.isLoadingEvents"
        @select="openEvent"
        @view-calendar="navigateTo('/calendar')"
      />

      <DashboardUpcomingEventsCard
        :events="dashboardStore.upcomingEvents"
        :now="dashboardStore.now"
        :loading="dashboardStore.isLoadingEvents"
        @select="openEvent"
        @view-all="navigateTo('/calendar')"
      />

      <DashboardMiniCalendarCard
        :month="dashboardStore.miniMonth"
        :event-day-keys="dashboardStore.eventDayKeys"
        :now="dashboardStore.now"
        :calendar-count="calendarStore.calendars.length"
        :event-count="dashboardStore.eventCountInMonth(dashboardStore.miniMonth)"
        :loading="dashboardStore.isLoadingEvents"
        @update:month="dashboardStore.setMiniMonth($event)"
        @select-date="openDay"
      />

      <DashboardRecentContactsCard
        class="md:col-span-2"
        :contacts="dashboardStore.recentContacts"
        :can-create="canCreateContact"
        :loading="dashboardStore.isLoadingContacts"
        @select="openContact"
        @view-all="navigateTo('/contacts')"
        @add="navigateTo('/contacts/new')"
      />

      <DashboardQuickStatsCard
        :calendars="calendarStore.calendars.length"
        :events-this-month="dashboardStore.monthEventCount"
        :address-books="contactsStore.addressBooks.length"
        :contacts="dashboardStore.totalContacts"
        :loading="dashboardStore.isLoadingEvents || dashboardStore.isLoadingContacts"
      />
    </div>

    <!-- Quick-action create dialog. Only mounted when the user actually owns a
         writable calendar; EventForm's calendar select is fed from
         writableCalendars, so without one there is nothing to submit to. -->
    <EventCreateDialog
      v-if="canCreateEvent"
      :visible="showCreateEventDialog"
      @update:visible="showCreateEventDialog = $event"
      @created="dashboardStore.refresh()"
    />
  </div>
</template>

<script setup lang="ts">
import EventCreateDialog from '~/components/calendar/EventCreateDialog.vue';
import { useAuthStore } from '~/stores/auth';
import { useCalendarStore } from '~/stores/calendars';
import { useContactsStore } from '~/stores/contacts';
import { useDashboardStore } from '~/stores/dashboard';
import type { CalendarEvent } from '~/types/calendar';
import type { Contact } from '~/types/contacts';

// Story 042 makes the dashboard the landing page. It lives at `/` (replacing the
// former redirect to /calendar) rather than at /dashboard, so the sidebar logo
// link and its existing `isExactActive` handling for a `/` nav entry keep
// working, and `/` stops being a route that only ever bounced. The post-login
// redirects in auth/login, the OAuth callback and middleware/guest now point
// here instead of /calendar.
definePageMeta({
  middleware: 'auth',
  layout: 'default',
});

/** How often the clock (Today/Tomorrow labels, now-marker) is refreshed. */
const CLOCK_INTERVAL_MS = 60 * 1000;
/** How often dashboard data is silently revalidated (story: default 5 minutes). */
const REFRESH_INTERVAL_MS = 5 * 60 * 1000;

const authStore = useAuthStore();
const calendarStore = useCalendarStore();
const contactsStore = useContactsStore();
const dashboardStore = useDashboardStore();

const showCreateEventDialog = ref(false);

const greeting = computed(() => {
  const name = authStore.user?.display_name || authStore.user?.username;
  return name ? `Welcome back, ${name}` : 'Welcome back';
});

const formattedToday = computed(() =>
  new Date(dashboardStore.now).toLocaleDateString(undefined, {
    weekday: 'long',
    year: 'numeric',
    month: 'long',
    day: 'numeric',
  })
);

// Read-only shares can't be written to (#53): offering these actions would just
// earn a 403 from the API.
const canCreateEvent = computed(() => calendarStore.writableCalendars.length > 0);
const canCreateContact = computed(() => contactsStore.writableAddressBooks.length > 0);

let clockTimer: ReturnType<typeof setInterval> | null = null;
let refreshTimer: ReturnType<typeof setInterval> | null = null;

onMounted(async () => {
  clockTimer = setInterval(() => dashboardStore.tick(), CLOCK_INTERVAL_MS);
  refreshTimer = setInterval(() => dashboardStore.refresh(), REFRESH_INTERVAL_MS);
  dashboardStore.tick();
  // Mount and the background timer share one path on purpose: the store survives
  // client-side navigation, so re-entering the dashboard must RE-READ the events
  // rather than keep the first visit's snapshot (an event created on the calendar
  // page in between would otherwise be missing from the agenda).
  await dashboardStore.refresh();
});

onBeforeUnmount(() => {
  if (clockTimer) clearInterval(clockTimer);
  if (refreshTimer) clearInterval(refreshTimer);
});

// Navigating into the calendar rather than opening a detail dialog here:
// EventDetailDialog unconditionally offers Edit/Delete, which would 403 on a
// read-only shared calendar. The calendar page owns that flow already, so point
// it at the right day and let it take over.
const openEvent = (event: CalendarEvent) => {
  const start = new Date(event.start);
  calendarStore.setCurrentDate(Number.isNaN(start.getTime()) ? new Date() : start);
  calendarStore.setCurrentView('timeGridDay');
  navigateTo('/calendar');
};

const openDay = (date: Date) => {
  calendarStore.setCurrentDate(date);
  calendarStore.setCurrentView('timeGridDay');
  navigateTo('/calendar');
};

// The contact detail route needs the numeric address-book id alongside the
// contact uuid (see pages/contacts/index.vue's selectContact).
const openContact = (contact: Contact) => {
  navigateTo(`/contacts/${contact.id}?ab=${contact.addressbook_id}`);
};
</script>
