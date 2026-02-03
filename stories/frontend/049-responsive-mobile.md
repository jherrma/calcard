# Story 049: Responsive Design & Mobile Optimization

## Story
**As a** user accessing the application from different devices
**I want to** have a fully functional and optimized experience on mobile, tablet, and desktop
**So that** I can manage my calendars and contacts from any device

## Acceptance Criteria

### Responsive Layout
- [ ] Fluid layout adapts to viewport sizes
- [ ] Breakpoints: mobile (<768px), tablet (768-1024px), desktop (>1024px)
- [ ] Sidebar collapses to hamburger menu on mobile
- [ ] Navigation accessible on all screen sizes
- [ ] No horizontal scrolling on mobile
- [ ] Content readable without zooming

### Mobile Navigation
- [ ] Bottom navigation bar on mobile
- [ ] Hamburger menu for secondary navigation
- [ ] Swipe gestures for navigation (calendar)
- [ ] Pull-to-refresh on lists
- [ ] Back button behavior consistent with native apps

### Touch Optimization
- [ ] Touch targets minimum 44x44px
- [ ] Touch-friendly calendar day selection
- [ ] Swipe to delete on list items
- [ ] Long press for context menus
- [ ] No hover-only interactions
- [ ] Smooth scrolling and momentum

### Mobile Calendar
- [ ] Day view as default on mobile
- [ ] Swipe between days/weeks
- [ ] Compact event cards
- [ ] Full-screen event details
- [ ] Easy date picker access

### Mobile Contacts
- [ ] List view optimized for scrolling
- [ ] Large touch targets for contact items
- [ ] Quick actions (call, email) prominent
- [ ] Search always accessible
- [ ] Alphabet scroll indicator

### Mobile Forms
- [ ] Full-screen form dialogs
- [ ] Appropriate keyboard types (email, tel, etc.)
- [ ] Date/time pickers mobile-optimized
- [ ] Form validation inline
- [ ] Sticky submit buttons

### Performance
- [ ] Fast initial load on 3G
- [ ] Images lazy loaded
- [ ] Skeleton screens during load
- [ ] Minimal layout shift

## Technical Details

### Responsive Utilities
```typescript
// composables/useResponsive.ts
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useMediaQuery } from '@vueuse/core'

export function useResponsive() {
  const isMobile = useMediaQuery('(max-width: 767px)')
  const isTablet = useMediaQuery('(min-width: 768px) and (max-width: 1023px)')
  const isDesktop = useMediaQuery('(min-width: 1024px)')
  const isTouch = useMediaQuery('(pointer: coarse)')

  const breakpoint = computed(() => {
    if (isMobile.value) return 'mobile'
    if (isTablet.value) return 'tablet'
    return 'desktop'
  })

  const orientation = useMediaQuery('(orientation: portrait)')
  const isPortrait = computed(() => orientation.value)
  const isLandscape = computed(() => !orientation.value)

  return {
    isMobile,
    isTablet,
    isDesktop,
    isTouch,
    breakpoint,
    isPortrait,
    isLandscape
  }
}
```

### Mobile Navigation Component
```vue
<!-- components/mobile/MobileNavigation.vue -->
<template>
  <div class="mobile-navigation">
    <!-- Bottom Tab Bar -->
    <nav class="bottom-tabs" aria-label="Main navigation">
      <NuxtLink
        v-for="tab in tabs"
        :key="tab.route"
        :to="tab.route"
        class="tab-item"
        :class="{ active: isActive(tab.route) }"
        :aria-current="isActive(tab.route) ? 'page' : undefined"
      >
        <i :class="tab.icon" aria-hidden="true"></i>
        <span class="tab-label">{{ tab.label }}</span>
      </NuxtLink>
    </nav>

    <!-- Hamburger Menu -->
    <Sidebar
      v-model:visible="menuOpen"
      position="left"
      class="mobile-sidebar"
    >
      <template #header>
        <div class="sidebar-header">
          <Avatar
            :image="user?.avatarUrl"
            :label="userInitials"
            size="large"
            shape="circle"
          />
          <div class="user-info">
            <span class="user-name">{{ user?.displayName }}</span>
            <span class="user-email">{{ user?.email }}</span>
          </div>
        </div>
      </template>

      <Menu :model="menuItems" class="sidebar-menu" />

      <template #footer>
        <Button
          label="Sign Out"
          icon="pi pi-sign-out"
          severity="secondary"
          class="sign-out-btn"
          @click="signOut"
        />
      </template>
    </Sidebar>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '~/stores/auth'

const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()

const menuOpen = ref(false)

const user = computed(() => authStore.user)
const userInitials = computed(() => {
  const name = user.value?.displayName || user.value?.email || ''
  return name.split(/[\s@]/).map(p => p[0]).slice(0, 2).join('').toUpperCase()
})

const tabs = [
  { label: 'Home', icon: 'pi pi-home', route: '/' },
  { label: 'Calendar', icon: 'pi pi-calendar', route: '/calendar' },
  { label: 'Contacts', icon: 'pi pi-users', route: '/contacts' },
  { label: 'More', icon: 'pi pi-bars', route: '#menu' }
]

const menuItems = [
  {
    label: 'Settings',
    icon: 'pi pi-cog',
    command: () => router.push('/settings')
  },
  {
    label: 'Import/Export',
    icon: 'pi pi-upload',
    command: () => router.push('/settings/import-export')
  },
  {
    label: 'Setup Help',
    icon: 'pi pi-question-circle',
    command: () => router.push('/setup')
  }
]

function isActive(path: string): boolean {
  if (path === '#menu') return false
  if (path === '/') return route.path === '/'
  return route.path.startsWith(path)
}

function handleTabClick(tab: typeof tabs[0]) {
  if (tab.route === '#menu') {
    menuOpen.value = true
  }
}

async function signOut() {
  await authStore.logout()
  router.push('/auth/login')
}
</script>

<style scoped>
.mobile-navigation {
  position: fixed;
  bottom: 0;
  left: 0;
  right: 0;
  z-index: 100;
}

.bottom-tabs {
  display: flex;
  background: var(--surface-card);
  border-top: 1px solid var(--surface-border);
  padding-bottom: env(safe-area-inset-bottom);
}

.tab-item {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 0.75rem 0.5rem;
  color: var(--text-color-secondary);
  text-decoration: none;
  transition: color 0.2s;
}

.tab-item.active {
  color: var(--primary-color);
}

.tab-item i {
  font-size: 1.25rem;
  margin-bottom: 0.25rem;
}

.tab-label {
  font-size: 0.625rem;
  font-weight: 500;
}

.sidebar-header {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  padding: 1rem;
  border-bottom: 1px solid var(--surface-border);
}

.user-info {
  display: flex;
  flex-direction: column;
}

.user-name {
  font-weight: 500;
}

.user-email {
  font-size: 0.75rem;
  color: var(--text-color-secondary);
}

.sidebar-menu {
  border: none;
}

.sign-out-btn {
  width: 100%;
  margin: 1rem;
}
</style>
```

### Mobile Calendar View
```vue
<!-- components/calendar/MobileCalendar.vue -->
<template>
  <div
    ref="calendarRef"
    class="mobile-calendar"
    @touchstart="handleTouchStart"
    @touchmove="handleTouchMove"
    @touchend="handleTouchEnd"
  >
    <!-- Date Header -->
    <header class="calendar-header">
      <button
        class="nav-button"
        aria-label="Previous day"
        @click="previousDay"
      >
        <i class="pi pi-chevron-left"></i>
      </button>

      <button
        class="date-display"
        @click="showDatePicker = true"
      >
        <span class="date-weekday">{{ weekday }}</span>
        <span class="date-full">{{ formattedDate }}</span>
      </button>

      <button
        class="nav-button"
        aria-label="Next day"
        @click="nextDay"
      >
        <i class="pi pi-chevron-right"></i>
      </button>
    </header>

    <!-- Today Button -->
    <Button
      v-if="!isToday"
      label="Today"
      size="small"
      text
      class="today-button"
      @click="goToToday"
    />

    <!-- Events List -->
    <div
      class="events-container"
      :style="{ transform: `translateX(${swipeOffset}px)` }"
    >
      <div v-if="loading" class="loading-state">
        <ProgressSpinner />
      </div>

      <div v-else-if="events.length === 0" class="empty-state">
        <i class="pi pi-calendar"></i>
        <p>No events for this day</p>
        <Button
          label="Add Event"
          icon="pi pi-plus"
          @click="createEvent"
        />
      </div>

      <div v-else class="event-list">
        <!-- All Day Events -->
        <div v-if="allDayEvents.length > 0" class="all-day-section">
          <span class="section-label">All Day</span>
          <div
            v-for="event in allDayEvents"
            :key="event.id"
            class="event-card all-day"
            :style="{ backgroundColor: event.calendar.color + '20', borderColor: event.calendar.color }"
            @click="openEvent(event)"
          >
            <span class="event-title">{{ event.summary }}</span>
          </div>
        </div>

        <!-- Timed Events -->
        <div
          v-for="event in timedEvents"
          :key="event.id"
          class="event-card"
          :style="{ borderLeftColor: event.calendar.color }"
          @click="openEvent(event)"
        >
          <div class="event-time">
            <span class="time-start">{{ formatTime(event.startTime) }}</span>
            <span class="time-end">{{ formatTime(event.endTime) }}</span>
          </div>
          <div class="event-content">
            <span class="event-title">{{ event.summary }}</span>
            <span v-if="event.location" class="event-location">
              <i class="pi pi-map-marker"></i>
              {{ event.location }}
            </span>
          </div>
        </div>
      </div>
    </div>

    <!-- FAB for new event -->
    <Button
      icon="pi pi-plus"
      rounded
      class="fab"
      aria-label="Create event"
      @click="createEvent"
    />

    <!-- Date Picker Dialog -->
    <Dialog
      v-model:visible="showDatePicker"
      header="Select Date"
      :modal="true"
      class="date-picker-dialog"
    >
      <Calendar
        v-model="selectedDate"
        inline
        @date-select="onDateSelected"
      />
    </Dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { format, isToday as checkIsToday, addDays } from 'date-fns'
import { useCalendarStore } from '~/stores/calendars'

const props = defineProps<{
  initialDate?: Date
}>()

const emit = defineEmits(['openEvent', 'createEvent'])

const calendarStore = useCalendarStore()

const selectedDate = ref(props.initialDate || new Date())
const showDatePicker = ref(false)
const loading = ref(false)
const swipeOffset = ref(0)

// Touch handling
let touchStartX = 0
let touchStartY = 0
let isSwiping = false

const isToday = computed(() => checkIsToday(selectedDate.value))
const weekday = computed(() => format(selectedDate.value, 'EEEE'))
const formattedDate = computed(() => format(selectedDate.value, 'MMMM d, yyyy'))

const events = computed(() => calendarStore.getEventsForDate(selectedDate.value))
const allDayEvents = computed(() => events.value.filter(e => e.isAllDay))
const timedEvents = computed(() =>
  events.value
    .filter(e => !e.isAllDay)
    .sort((a, b) => new Date(a.startTime).getTime() - new Date(b.startTime).getTime())
)

function previousDay() {
  selectedDate.value = addDays(selectedDate.value, -1)
}

function nextDay() {
  selectedDate.value = addDays(selectedDate.value, 1)
}

function goToToday() {
  selectedDate.value = new Date()
}

function onDateSelected() {
  showDatePicker.value = false
}

function formatTime(dateStr: string): string {
  return format(new Date(dateStr), 'HH:mm')
}

function openEvent(event: any) {
  emit('openEvent', event)
}

function createEvent() {
  emit('createEvent', selectedDate.value)
}

// Swipe gesture handling
function handleTouchStart(e: TouchEvent) {
  touchStartX = e.touches[0].clientX
  touchStartY = e.touches[0].clientY
  isSwiping = false
}

function handleTouchMove(e: TouchEvent) {
  const touchX = e.touches[0].clientX
  const touchY = e.touches[0].clientY
  const diffX = touchX - touchStartX
  const diffY = touchY - touchStartY

  // Only horizontal swipe
  if (Math.abs(diffX) > Math.abs(diffY) && Math.abs(diffX) > 10) {
    isSwiping = true
    swipeOffset.value = diffX * 0.5 // Dampened movement
  }
}

function handleTouchEnd(e: TouchEvent) {
  if (isSwiping) {
    if (swipeOffset.value > 50) {
      previousDay()
    } else if (swipeOffset.value < -50) {
      nextDay()
    }
  }
  swipeOffset.value = 0
  isSwiping = false
}
</script>

<style scoped>
.mobile-calendar {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
  padding-bottom: calc(60px + env(safe-area-inset-bottom));
}

.calendar-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 1rem;
  background: var(--surface-card);
  border-bottom: 1px solid var(--surface-border);
}

.nav-button {
  width: 44px;
  height: 44px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: none;
  border: none;
  border-radius: 50%;
  color: var(--text-color);
  cursor: pointer;
}

.nav-button:active {
  background: var(--surface-hover);
}

.date-display {
  display: flex;
  flex-direction: column;
  align-items: center;
  background: none;
  border: none;
  cursor: pointer;
  padding: 0.5rem 1rem;
  border-radius: 8px;
}

.date-display:active {
  background: var(--surface-hover);
}

.date-weekday {
  font-size: 0.75rem;
  color: var(--text-color-secondary);
  text-transform: uppercase;
}

.date-full {
  font-size: 1.125rem;
  font-weight: 600;
}

.today-button {
  align-self: center;
  margin: 0.5rem 0;
}

.events-container {
  flex: 1;
  overflow-y: auto;
  padding: 1rem;
  transition: transform 0.1s ease-out;
}

.loading-state,
.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 200px;
  color: var(--text-color-secondary);
}

.empty-state i {
  font-size: 3rem;
  margin-bottom: 1rem;
  opacity: 0.5;
}

.event-list {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
}

.all-day-section {
  margin-bottom: 1rem;
}

.section-label {
  font-size: 0.75rem;
  font-weight: 600;
  text-transform: uppercase;
  color: var(--text-color-secondary);
  display: block;
  margin-bottom: 0.5rem;
}

.event-card {
  display: flex;
  gap: 0.75rem;
  padding: 0.75rem;
  background: var(--surface-card);
  border-radius: 8px;
  border-left: 4px solid;
  cursor: pointer;
  transition: background-color 0.2s;
}

.event-card:active {
  background: var(--surface-hover);
}

.event-card.all-day {
  border: 1px solid;
  border-left-width: 4px;
}

.event-time {
  display: flex;
  flex-direction: column;
  align-items: center;
  min-width: 50px;
}

.time-start {
  font-weight: 600;
}

.time-end {
  font-size: 0.75rem;
  color: var(--text-color-secondary);
}

.event-content {
  flex: 1;
  min-width: 0;
}

.event-title {
  display: block;
  font-weight: 500;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.event-location {
  display: flex;
  align-items: center;
  gap: 0.25rem;
  font-size: 0.75rem;
  color: var(--text-color-secondary);
  margin-top: 0.25rem;
}

.fab {
  position: fixed;
  bottom: calc(80px + env(safe-area-inset-bottom));
  right: 1rem;
  width: 56px;
  height: 56px;
  box-shadow: var(--shadow-lg);
}

.date-picker-dialog :deep(.p-dialog-content) {
  padding: 0;
}
</style>
```

### Swipe to Delete Component
```vue
<!-- components/mobile/SwipeToDelete.vue -->
<template>
  <div
    ref="containerRef"
    class="swipe-container"
    @touchstart="handleTouchStart"
    @touchmove="handleTouchMove"
    @touchend="handleTouchEnd"
  >
    <div
      class="swipe-content"
      :style="{ transform: `translateX(${offset}px)` }"
    >
      <slot />
    </div>

    <div class="swipe-actions" :style="{ width: `${actionsWidth}px` }">
      <button
        v-if="showEdit"
        class="action-button edit"
        @click="$emit('edit')"
      >
        <i class="pi pi-pencil"></i>
      </button>
      <button
        class="action-button delete"
        @click="$emit('delete')"
      >
        <i class="pi pi-trash"></i>
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'

const props = withDefaults(defineProps<{
  showEdit?: boolean
  threshold?: number
}>(), {
  showEdit: true,
  threshold: 0.3
})

const emit = defineEmits(['edit', 'delete'])

const containerRef = ref<HTMLElement | null>(null)
const offset = ref(0)
const isDragging = ref(false)
const startX = ref(0)
const startOffset = ref(0)

const actionsWidth = computed(() => props.showEdit ? 120 : 60)
const maxOffset = computed(() => -actionsWidth.value)

function handleTouchStart(e: TouchEvent) {
  startX.value = e.touches[0].clientX
  startOffset.value = offset.value
  isDragging.value = true
}

function handleTouchMove(e: TouchEvent) {
  if (!isDragging.value) return

  const diff = e.touches[0].clientX - startX.value
  const newOffset = startOffset.value + diff

  // Clamp between maxOffset and 0
  offset.value = Math.max(maxOffset.value, Math.min(0, newOffset))

  // Add resistance when overscrolling
  if (newOffset > 0) {
    offset.value = newOffset * 0.2
  }
}

function handleTouchEnd() {
  isDragging.value = false

  const thresholdPx = actionsWidth.value * props.threshold

  if (offset.value < -thresholdPx) {
    // Snap to open
    offset.value = maxOffset.value
  } else {
    // Snap to closed
    offset.value = 0
  }
}

function close() {
  offset.value = 0
}

defineExpose({ close })
</script>

<style scoped>
.swipe-container {
  position: relative;
  overflow: hidden;
}

.swipe-content {
  position: relative;
  z-index: 1;
  background: var(--surface-card);
  transition: transform 0.2s ease-out;
}

.swipe-actions {
  position: absolute;
  top: 0;
  right: 0;
  bottom: 0;
  display: flex;
}

.action-button {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 60px;
  border: none;
  color: white;
  font-size: 1.25rem;
  cursor: pointer;
}

.action-button.edit {
  background: var(--primary-color);
}

.action-button.delete {
  background: var(--red-500);
}
</style>
```

### Responsive CSS Utilities
```css
/* assets/css/responsive.css */

/* Container queries support */
@supports (container-type: inline-size) {
  .responsive-container {
    container-type: inline-size;
  }
}

/* Mobile-first breakpoints */
.hide-mobile {
  display: none;
}

.show-mobile {
  display: block;
}

@media (min-width: 768px) {
  .hide-mobile {
    display: block;
  }

  .show-mobile {
    display: none;
  }

  .hide-tablet {
    display: none;
  }
}

@media (min-width: 1024px) {
  .hide-tablet {
    display: block;
  }

  .hide-desktop {
    display: none;
  }
}

/* Touch-specific styles */
@media (pointer: coarse) {
  /* Larger touch targets */
  button,
  a,
  .clickable {
    min-height: 44px;
    min-width: 44px;
  }

  /* Remove hover effects that don't work well on touch */
  .no-touch-hover:hover {
    background: inherit;
    color: inherit;
  }
}

/* Safe area handling for notched devices */
.safe-area-top {
  padding-top: env(safe-area-inset-top);
}

.safe-area-bottom {
  padding-bottom: env(safe-area-inset-bottom);
}

.safe-area-left {
  padding-left: env(safe-area-inset-left);
}

.safe-area-right {
  padding-right: env(safe-area-inset-right);
}

/* Full-screen mobile dialogs */
@media (max-width: 767px) {
  .mobile-fullscreen {
    width: 100vw !important;
    height: 100vh !important;
    max-width: none !important;
    max-height: none !important;
    margin: 0 !important;
    border-radius: 0 !important;
  }
}

/* Stack buttons on mobile */
@media (max-width: 767px) {
  .button-group-responsive {
    flex-direction: column;
  }

  .button-group-responsive > * {
    width: 100%;
  }
}
```

## Dependencies
- Story 031 (Frontend Project Setup)
- Story 033 (Layout & Navigation)
- Story 034 (Calendar Views)
- Story 036 (Contact List UI)
- @vueuse/core for media queries

## Estimation
- **Complexity:** High
- **Components:** 3 components, 1 composable, CSS

## Notes
- Test on real devices, not just emulators
- Consider PWA features for mobile (Story 050)
- Touch gestures should have visual feedback
- Performance is critical on mobile
- Consider iOS and Android differences
- Use CSS container queries where supported
