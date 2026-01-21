# Story 042: Dashboard Home Page

## Story
**As a** user
**I want to** see an overview of my calendars and contacts on a dashboard
**So that** I can quickly access recent items and get a summary of upcoming events

## Acceptance Criteria

### Dashboard Layout
- [ ] Dashboard is the default landing page after login
- [ ] Responsive grid layout adapts to screen size
- [ ] Quick action buttons for common tasks
- [ ] Welcome message with user's name

### Upcoming Events Widget
- [ ] Shows next 5-7 upcoming events
- [ ] Displays event title, date/time, and calendar color
- [ ] "Today" and "Tomorrow" labels for near events
- [ ] Clicking event navigates to calendar view
- [ ] Empty state when no upcoming events
- [ ] "View all" link to calendar page

### Today's Agenda Widget
- [ ] Shows all events for current day
- [ ] Timeline view with hour markers
- [ ] All-day events shown at top
- [ ] Current time indicator
- [ ] Click to view event details

### Recent Contacts Widget
- [ ] Shows 5-6 recently viewed/edited contacts
- [ ] Displays contact avatar, name, and organization
- [ ] Quick action buttons (email, phone)
- [ ] Clicking contact opens contact details
- [ ] Empty state for new users

### Calendar Summary Widget
- [ ] Mini calendar showing current month
- [ ] Days with events are highlighted
- [ ] Clicking a day navigates to that day's view
- [ ] Shows count of calendars and total events

### Quick Stats
- [ ] Total number of calendars
- [ ] Total number of events this month
- [ ] Total number of contacts
- [ ] Total number of address books

## Technical Details

### Dashboard Page Component
```vue
<template>
  <div class="dashboard">
    <header class="dashboard-header">
      <h1>Welcome back, {{ user?.displayName || user?.username }}</h1>
      <p class="text-muted">{{ formattedDate }}</p>
    </header>

    <div class="quick-actions">
      <Button
        label="New Event"
        icon="pi pi-calendar-plus"
        @click="createEvent"
      />
      <Button
        label="New Contact"
        icon="pi pi-user-plus"
        severity="secondary"
        @click="createContact"
      />
      <Button
        label="Import"
        icon="pi pi-upload"
        severity="secondary"
        @click="navigateTo('/settings/import-export')"
      />
    </div>

    <div class="dashboard-grid">
      <!-- Today's Agenda -->
      <Card class="agenda-card">
        <template #title>
          <div class="card-header">
            <span>Today's Agenda</span>
            <Button
              label="View Calendar"
              link
              size="small"
              @click="navigateTo('/calendar')"
            />
          </div>
        </template>
        <template #content>
          <div v-if="loading" class="loading-state">
            <ProgressSpinner />
          </div>
          <div v-else-if="todayEvents.length === 0" class="empty-state">
            <i class="pi pi-calendar"></i>
            <p>No events scheduled for today</p>
          </div>
          <div v-else class="agenda-timeline">
            <!-- All-day events -->
            <div v-if="allDayEvents.length > 0" class="all-day-section">
              <span class="time-label">All Day</span>
              <div
                v-for="event in allDayEvents"
                :key="event.id"
                class="agenda-event all-day"
                :style="{ borderLeftColor: event.calendar.color }"
                @click="openEvent(event)"
              >
                <span class="event-title">{{ event.summary }}</span>
              </div>
            </div>

            <!-- Timed events -->
            <div
              v-for="event in timedEvents"
              :key="event.id"
              class="agenda-event"
              :style="{ borderLeftColor: event.calendar.color }"
              @click="openEvent(event)"
            >
              <span class="time-label">{{ formatTime(event.startTime) }}</span>
              <div class="event-details">
                <span class="event-title">{{ event.summary }}</span>
                <span class="event-duration">{{ formatDuration(event) }}</span>
              </div>
            </div>

            <!-- Current time indicator -->
            <div
              class="current-time-indicator"
              :style="{ top: currentTimePosition + '%' }"
            >
              <span class="current-time">{{ currentTime }}</span>
            </div>
          </div>
        </template>
      </Card>

      <!-- Upcoming Events -->
      <Card class="upcoming-card">
        <template #title>
          <div class="card-header">
            <span>Upcoming Events</span>
            <Badge :value="upcomingEvents.length" />
          </div>
        </template>
        <template #content>
          <div v-if="loading" class="loading-state">
            <ProgressSpinner />
          </div>
          <div v-else-if="upcomingEvents.length === 0" class="empty-state">
            <i class="pi pi-calendar"></i>
            <p>No upcoming events</p>
          </div>
          <div v-else class="event-list">
            <div
              v-for="event in upcomingEvents"
              :key="event.id"
              class="event-item"
              @click="openEvent(event)"
            >
              <div
                class="event-color"
                :style="{ backgroundColor: event.calendar.color }"
              ></div>
              <div class="event-info">
                <span class="event-title">{{ event.summary }}</span>
                <span class="event-time">
                  {{ formatEventDate(event) }}
                </span>
              </div>
              <span class="event-relative">{{ getRelativeDate(event) }}</span>
            </div>
          </div>
        </template>
      </Card>

      <!-- Mini Calendar -->
      <Card class="mini-calendar-card">
        <template #title>
          {{ currentMonthName }}
        </template>
        <template #content>
          <Calendar
            v-model="selectedDate"
            inline
            :minDate="minDate"
            @date-select="onDateSelect"
          >
            <template #date="{ date }">
              <span :class="{ 'has-events': hasEvents(date) }">
                {{ date.day }}
              </span>
            </template>
          </Calendar>
        </template>
      </Card>

      <!-- Recent Contacts -->
      <Card class="contacts-card">
        <template #title>
          <div class="card-header">
            <span>Recent Contacts</span>
            <Button
              label="View All"
              link
              size="small"
              @click="navigateTo('/contacts')"
            />
          </div>
        </template>
        <template #content>
          <div v-if="loading" class="loading-state">
            <ProgressSpinner />
          </div>
          <div v-else-if="recentContacts.length === 0" class="empty-state">
            <i class="pi pi-users"></i>
            <p>No recent contacts</p>
            <Button
              label="Add Contact"
              size="small"
              @click="createContact"
            />
          </div>
          <div v-else class="contact-list">
            <div
              v-for="contact in recentContacts"
              :key="contact.id"
              class="contact-item"
              @click="openContact(contact)"
            >
              <Avatar
                :image="contact.photoUrl"
                :label="getInitials(contact)"
                shape="circle"
                size="large"
              />
              <div class="contact-info">
                <span class="contact-name">{{ contact.formattedName }}</span>
                <span class="contact-org">{{ contact.organization || contact.email }}</span>
              </div>
              <div class="contact-actions">
                <Button
                  v-if="contact.email"
                  icon="pi pi-envelope"
                  text
                  rounded
                  size="small"
                  @click.stop="emailContact(contact)"
                />
                <Button
                  v-if="contact.phone"
                  icon="pi pi-phone"
                  text
                  rounded
                  size="small"
                  @click.stop="callContact(contact)"
                />
              </div>
            </div>
          </div>
        </template>
      </Card>

      <!-- Quick Stats -->
      <Card class="stats-card">
        <template #title>Overview</template>
        <template #content>
          <div class="stats-grid">
            <div class="stat-item">
              <i class="pi pi-calendar stat-icon"></i>
              <div class="stat-info">
                <span class="stat-value">{{ stats.calendarCount }}</span>
                <span class="stat-label">Calendars</span>
              </div>
            </div>
            <div class="stat-item">
              <i class="pi pi-clock stat-icon"></i>
              <div class="stat-info">
                <span class="stat-value">{{ stats.eventCount }}</span>
                <span class="stat-label">Events this month</span>
              </div>
            </div>
            <div class="stat-item">
              <i class="pi pi-book stat-icon"></i>
              <div class="stat-info">
                <span class="stat-value">{{ stats.addressbookCount }}</span>
                <span class="stat-label">Address Books</span>
              </div>
            </div>
            <div class="stat-item">
              <i class="pi pi-users stat-icon"></i>
              <div class="stat-info">
                <span class="stat-value">{{ stats.contactCount }}</span>
                <span class="stat-label">Contacts</span>
              </div>
            </div>
          </div>
        </template>
      </Card>
    </div>

    <!-- Event Dialog -->
    <EventDialog
      v-model:visible="showEventDialog"
      :event="selectedEvent"
      @saved="refreshData"
    />

    <!-- Contact Dialog -->
    <ContactDialog
      v-model:visible="showContactDialog"
      :contact="selectedContact"
      @saved="refreshData"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '~/stores/auth'
import { useCalendarStore } from '~/stores/calendars'
import { useContactStore } from '~/stores/contacts'
import { formatDistanceToNow, format, isToday, isTomorrow, startOfMonth, endOfMonth } from 'date-fns'

const router = useRouter()
const authStore = useAuthStore()
const calendarStore = useCalendarStore()
const contactStore = useContactStore()

const loading = ref(true)
const selectedDate = ref(new Date())
const showEventDialog = ref(false)
const showContactDialog = ref(false)
const selectedEvent = ref(null)
const selectedContact = ref(null)
const currentTime = ref('')
const currentTimePosition = ref(0)

let timeUpdateInterval: NodeJS.Timer | null = null

const user = computed(() => authStore.user)

const formattedDate = computed(() => {
  return format(new Date(), 'EEEE, MMMM d, yyyy')
})

const currentMonthName = computed(() => {
  return format(selectedDate.value, 'MMMM yyyy')
})

const minDate = computed(() => new Date())

const todayEvents = computed(() => {
  return calendarStore.getEventsForDate(new Date())
})

const allDayEvents = computed(() => {
  return todayEvents.value.filter(e => e.isAllDay)
})

const timedEvents = computed(() => {
  return todayEvents.value
    .filter(e => !e.isAllDay)
    .sort((a, b) => new Date(a.startTime).getTime() - new Date(b.startTime).getTime())
})

const upcomingEvents = computed(() => {
  return calendarStore.getUpcomingEvents(7).slice(0, 7)
})

const recentContacts = computed(() => {
  return contactStore.recentContacts.slice(0, 6)
})

const eventDates = computed(() => {
  const dates = new Set<string>()
  const start = startOfMonth(selectedDate.value)
  const end = endOfMonth(selectedDate.value)
  calendarStore.getEventsInRange(start, end).forEach(event => {
    dates.add(format(new Date(event.startTime), 'yyyy-MM-dd'))
  })
  return dates
})

const stats = computed(() => ({
  calendarCount: calendarStore.calendars.length,
  eventCount: calendarStore.getEventsInRange(
    startOfMonth(new Date()),
    endOfMonth(new Date())
  ).length,
  addressbookCount: contactStore.addressbooks.length,
  contactCount: contactStore.totalContacts
}))

onMounted(async () => {
  await refreshData()
  updateCurrentTime()
  timeUpdateInterval = setInterval(updateCurrentTime, 60000)
})

onUnmounted(() => {
  if (timeUpdateInterval) {
    clearInterval(timeUpdateInterval)
  }
})

async function refreshData() {
  loading.value = true
  try {
    await Promise.all([
      calendarStore.fetchCalendars(),
      calendarStore.fetchEvents(),
      contactStore.fetchAddressbooks(),
      contactStore.fetchRecentContacts()
    ])
  } finally {
    loading.value = false
  }
}

function updateCurrentTime() {
  const now = new Date()
  currentTime.value = format(now, 'HH:mm')
  // Position based on time of day (0-100%)
  const minutes = now.getHours() * 60 + now.getMinutes()
  currentTimePosition.value = (minutes / 1440) * 100
}

function formatTime(dateString: string): string {
  return format(new Date(dateString), 'HH:mm')
}

function formatDuration(event: any): string {
  const start = new Date(event.startTime)
  const end = new Date(event.endTime)
  const diff = (end.getTime() - start.getTime()) / (1000 * 60)
  if (diff < 60) return `${diff}m`
  const hours = Math.floor(diff / 60)
  const mins = diff % 60
  return mins > 0 ? `${hours}h ${mins}m` : `${hours}h`
}

function formatEventDate(event: any): string {
  const date = new Date(event.startTime)
  if (event.isAllDay) {
    return format(date, 'EEE, MMM d')
  }
  return format(date, 'EEE, MMM d \'at\' HH:mm')
}

function getRelativeDate(event: any): string {
  const date = new Date(event.startTime)
  if (isToday(date)) return 'Today'
  if (isTomorrow(date)) return 'Tomorrow'
  return formatDistanceToNow(date, { addSuffix: true })
}

function hasEvents(date: { day: number; month: number; year: number }): boolean {
  const dateStr = `${date.year}-${String(date.month + 1).padStart(2, '0')}-${String(date.day).padStart(2, '0')}`
  return eventDates.value.has(dateStr)
}

function onDateSelect(date: Date) {
  router.push(`/calendar?date=${format(date, 'yyyy-MM-dd')}`)
}

function getInitials(contact: any): string {
  const names = contact.formattedName?.split(' ') || []
  return names.map((n: string) => n[0]).slice(0, 2).join('').toUpperCase()
}

function createEvent() {
  selectedEvent.value = null
  showEventDialog.value = true
}

function createContact() {
  selectedContact.value = null
  showContactDialog.value = true
}

function openEvent(event: any) {
  selectedEvent.value = event
  showEventDialog.value = true
}

function openContact(contact: any) {
  router.push(`/contacts/${contact.id}`)
}

function emailContact(contact: any) {
  window.location.href = `mailto:${contact.email}`
}

function callContact(contact: any) {
  window.location.href = `tel:${contact.phone}`
}

function navigateTo(path: string) {
  router.push(path)
}
</script>

<style scoped>
.dashboard {
  padding: 1.5rem;
  max-width: 1400px;
  margin: 0 auto;
}

.dashboard-header {
  margin-bottom: 1.5rem;
}

.dashboard-header h1 {
  margin: 0;
  font-size: 1.75rem;
}

.text-muted {
  color: var(--text-color-secondary);
  margin-top: 0.25rem;
}

.quick-actions {
  display: flex;
  gap: 0.75rem;
  margin-bottom: 1.5rem;
  flex-wrap: wrap;
}

.dashboard-grid {
  display: grid;
  grid-template-columns: repeat(12, 1fr);
  gap: 1.5rem;
}

.agenda-card {
  grid-column: span 4;
}

.upcoming-card {
  grid-column: span 4;
}

.mini-calendar-card {
  grid-column: span 4;
}

.contacts-card {
  grid-column: span 8;
}

.stats-card {
  grid-column: span 4;
}

@media (max-width: 1200px) {
  .agenda-card,
  .upcoming-card,
  .mini-calendar-card {
    grid-column: span 6;
  }

  .contacts-card,
  .stats-card {
    grid-column: span 6;
  }
}

@media (max-width: 768px) {
  .dashboard-grid > * {
    grid-column: span 12;
  }
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.loading-state,
.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 2rem;
  color: var(--text-color-secondary);
}

.empty-state i {
  font-size: 2.5rem;
  margin-bottom: 1rem;
  opacity: 0.5;
}

/* Agenda styles */
.agenda-timeline {
  position: relative;
}

.all-day-section {
  margin-bottom: 1rem;
  padding-bottom: 1rem;
  border-bottom: 1px solid var(--surface-border);
}

.agenda-event {
  display: flex;
  align-items: flex-start;
  gap: 0.75rem;
  padding: 0.75rem;
  margin-bottom: 0.5rem;
  border-left: 3px solid;
  background: var(--surface-ground);
  border-radius: 0 4px 4px 0;
  cursor: pointer;
  transition: background-color 0.2s;
}

.agenda-event:hover {
  background: var(--surface-hover);
}

.time-label {
  font-size: 0.875rem;
  font-weight: 500;
  color: var(--text-color-secondary);
  min-width: 50px;
}

.event-details {
  flex: 1;
}

.event-title {
  display: block;
  font-weight: 500;
}

.event-duration {
  font-size: 0.75rem;
  color: var(--text-color-secondary);
}

.current-time-indicator {
  position: absolute;
  left: 0;
  right: 0;
  display: flex;
  align-items: center;
  gap: 0.5rem;
  pointer-events: none;
}

.current-time-indicator::after {
  content: '';
  flex: 1;
  height: 2px;
  background: var(--primary-color);
}

.current-time {
  font-size: 0.75rem;
  font-weight: 600;
  color: var(--primary-color);
}

/* Event list styles */
.event-list {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.event-item {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  padding: 0.75rem;
  border-radius: 6px;
  cursor: pointer;
  transition: background-color 0.2s;
}

.event-item:hover {
  background: var(--surface-hover);
}

.event-color {
  width: 4px;
  height: 40px;
  border-radius: 2px;
}

.event-info {
  flex: 1;
  min-width: 0;
}

.event-info .event-title {
  display: block;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.event-time {
  font-size: 0.75rem;
  color: var(--text-color-secondary);
}

.event-relative {
  font-size: 0.75rem;
  color: var(--text-color-secondary);
  white-space: nowrap;
}

/* Mini calendar styles */
:deep(.has-events) {
  background: var(--primary-100);
  border-radius: 50%;
}

/* Contact list styles */
.contact-list {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.contact-item {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  padding: 0.5rem;
  border-radius: 6px;
  cursor: pointer;
  transition: background-color 0.2s;
}

.contact-item:hover {
  background: var(--surface-hover);
}

.contact-info {
  flex: 1;
  min-width: 0;
}

.contact-name {
  display: block;
  font-weight: 500;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.contact-org {
  font-size: 0.75rem;
  color: var(--text-color-secondary);
}

.contact-actions {
  display: flex;
  gap: 0.25rem;
}

/* Stats styles */
.stats-grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 1rem;
}

.stat-item {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  padding: 0.75rem;
  background: var(--surface-ground);
  border-radius: 8px;
}

.stat-icon {
  font-size: 1.5rem;
  color: var(--primary-color);
}

.stat-info {
  display: flex;
  flex-direction: column;
}

.stat-value {
  font-size: 1.25rem;
  font-weight: 600;
}

.stat-label {
  font-size: 0.75rem;
  color: var(--text-color-secondary);
}
</style>
```

## Dependencies
- Story 031 (Frontend Project Setup)
- Story 034 (Calendar Views)
- Story 035 (Event Management UI)
- Story 036 (Contact List UI)

## Estimation
- **Complexity:** Medium
- **Components:** 1 main page, 5 widget cards, 2 dialogs

## Notes
- Dashboard data should be cached and refreshed periodically
- Consider adding customizable widget arrangement in future
- Mobile view stacks all widgets vertically
- Auto-refresh interval configurable (default: 5 minutes)
