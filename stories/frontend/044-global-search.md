# Story 044: Global Search

## Story
**As a** user
**I want to** search across all my calendars and contacts from a single search bar
**So that** I can quickly find events and contacts without navigating to different pages

## Acceptance Criteria

### Search Interface
- [ ] Global search accessible via keyboard shortcut (Cmd/Ctrl + K)
- [ ] Search icon in header opens search dialog
- [ ] Search dialog appears as modal overlay
- [ ] Auto-focus on search input when opened
- [ ] Close with Escape key or clicking outside

### Search Functionality
- [ ] Single input searches both events and contacts
- [ ] Results categorized by type (Events, Contacts)
- [ ] Debounced search (300ms delay)
- [ ] Minimum 2 characters to trigger search
- [ ] Recent searches displayed before typing
- [ ] Clear search button

### Event Search Results
- [ ] Shows event title, date/time, and calendar name
- [ ] Calendar color indicator
- [ ] Highlights matching text
- [ ] Shows "Today", "Tomorrow", or date for upcoming events
- [ ] Past events marked visually
- [ ] Clicking navigates to event in calendar view

### Contact Search Results
- [ ] Shows contact name, organization, email
- [ ] Contact avatar/initials
- [ ] Address book name
- [ ] Highlights matching text
- [ ] Clicking opens contact details

### Results Navigation
- [ ] Keyboard navigation (Up/Down arrows)
- [ ] Enter to select highlighted result
- [ ] Tab to switch between categories
- [ ] "View all" link for each category
- [ ] Empty state with helpful message

### Performance
- [ ] Loading indicator during search
- [ ] Results cached for session
- [ ] Maximum 5 results per category in quick view
- [ ] Full results page for comprehensive search

## Technical Details

### Global Search Component
```vue
<template>
  <div>
    <!-- Search Trigger Button -->
    <Button
      icon="pi pi-search"
      text
      rounded
      class="search-trigger"
      v-tooltip.bottom="'Search (⌘K)'"
      @click="openSearch"
    />

    <!-- Search Dialog -->
    <Dialog
      v-model:visible="visible"
      :modal="true"
      :showHeader="false"
      :dismissableMask="true"
      class="search-dialog"
      position="top"
      :style="{ width: '600px', marginTop: '10vh' }"
    >
      <div class="search-container">
        <!-- Search Input -->
        <div class="search-input-wrapper">
          <i class="pi pi-search search-icon"></i>
          <input
            ref="searchInput"
            v-model="query"
            type="text"
            placeholder="Search events and contacts..."
            class="search-input"
            @keydown="handleKeydown"
          />
          <Button
            v-if="query"
            icon="pi pi-times"
            text
            rounded
            size="small"
            @click="clearSearch"
          />
          <div class="search-shortcut">
            <kbd>ESC</kbd>
          </div>
        </div>

        <!-- Search Content -->
        <div class="search-content">
          <!-- Loading State -->
          <div v-if="loading" class="search-loading">
            <ProgressSpinner strokeWidth="3" />
            <span>Searching...</span>
          </div>

          <!-- Recent Searches (when query is empty) -->
          <div v-else-if="!query && recentSearches.length > 0" class="recent-searches">
            <div class="section-header">
              <span>Recent Searches</span>
              <Button
                label="Clear"
                link
                size="small"
                @click="clearRecentSearches"
              />
            </div>
            <div class="recent-list">
              <div
                v-for="(search, index) in recentSearches"
                :key="index"
                class="recent-item"
                @click="applyRecentSearch(search)"
              >
                <i class="pi pi-history"></i>
                <span>{{ search }}</span>
              </div>
            </div>
          </div>

          <!-- No Query State -->
          <div v-else-if="!query" class="empty-query">
            <p>Start typing to search events and contacts</p>
            <div class="search-tips">
              <div class="tip">
                <kbd>↑</kbd> <kbd>↓</kbd> Navigate
              </div>
              <div class="tip">
                <kbd>Enter</kbd> Select
              </div>
              <div class="tip">
                <kbd>Tab</kbd> Switch category
              </div>
            </div>
          </div>

          <!-- Search Results -->
          <div v-else-if="hasResults" class="search-results">
            <!-- Events Section -->
            <div v-if="results.events.length > 0" class="result-section">
              <div class="section-header">
                <span>Events</span>
                <Badge :value="results.totalEvents" />
              </div>
              <div class="result-list">
                <div
                  v-for="(event, index) in results.events"
                  :key="event.id"
                  class="result-item"
                  :class="{ active: isActive('event', index) }"
                  @click="selectEvent(event)"
                  @mouseenter="setActive('event', index)"
                >
                  <div
                    class="calendar-color"
                    :style="{ backgroundColor: event.calendar.color }"
                  ></div>
                  <div class="result-info">
                    <span class="result-title" v-html="highlightMatch(event.summary)"></span>
                    <span class="result-meta">
                      {{ formatEventDate(event) }} · {{ event.calendar.name }}
                    </span>
                  </div>
                  <Tag
                    v-if="isPastEvent(event)"
                    value="Past"
                    severity="secondary"
                    size="small"
                  />
                  <span v-else class="result-relative">
                    {{ getRelativeDate(event) }}
                  </span>
                </div>
              </div>
              <Button
                v-if="results.totalEvents > 5"
                :label="`View all ${results.totalEvents} events`"
                link
                size="small"
                class="view-all"
                @click="viewAllEvents"
              />
            </div>

            <!-- Contacts Section -->
            <div v-if="results.contacts.length > 0" class="result-section">
              <div class="section-header">
                <span>Contacts</span>
                <Badge :value="results.totalContacts" />
              </div>
              <div class="result-list">
                <div
                  v-for="(contact, index) in results.contacts"
                  :key="contact.id"
                  class="result-item"
                  :class="{ active: isActive('contact', index) }"
                  @click="selectContact(contact)"
                  @mouseenter="setActive('contact', index)"
                >
                  <Avatar
                    :image="contact.photoUrl"
                    :label="getInitials(contact)"
                    shape="circle"
                    size="small"
                  />
                  <div class="result-info">
                    <span class="result-title" v-html="highlightMatch(contact.formattedName)"></span>
                    <span class="result-meta">
                      {{ contact.organization || contact.email || contact.addressbook.name }}
                    </span>
                  </div>
                </div>
              </div>
              <Button
                v-if="results.totalContacts > 5"
                :label="`View all ${results.totalContacts} contacts`"
                link
                size="small"
                class="view-all"
                @click="viewAllContacts"
              />
            </div>
          </div>

          <!-- No Results -->
          <div v-else-if="query && !loading" class="no-results">
            <i class="pi pi-search"></i>
            <p>No results found for "{{ query }}"</p>
            <span class="text-muted">Try different keywords or check spelling</span>
          </div>
        </div>
      </div>
    </Dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted, nextTick } from 'vue'
import { useRouter } from 'vue-router'
import { useDebounceFn, useStorage } from '@vueuse/core'
import { format, isToday, isTomorrow, isPast, formatDistanceToNow } from 'date-fns'

interface SearchResults {
  events: any[]
  contacts: any[]
  totalEvents: number
  totalContacts: number
}

const router = useRouter()

const visible = ref(false)
const query = ref('')
const loading = ref(false)
const searchInput = ref<HTMLInputElement | null>(null)
const activeCategory = ref<'event' | 'contact'>('event')
const activeIndex = ref(0)
const recentSearches = useStorage<string[]>('recentSearches', [])

const results = ref<SearchResults>({
  events: [],
  contacts: [],
  totalEvents: 0,
  totalContacts: 0
})

const hasResults = computed(() =>
  results.value.events.length > 0 || results.value.contacts.length > 0
)

// Keyboard shortcut handler
function handleGlobalKeydown(e: KeyboardEvent) {
  if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
    e.preventDefault()
    openSearch()
  }
}

onMounted(() => {
  document.addEventListener('keydown', handleGlobalKeydown)
})

onUnmounted(() => {
  document.removeEventListener('keydown', handleGlobalKeydown)
})

function openSearch() {
  visible.value = true
  nextTick(() => {
    searchInput.value?.focus()
  })
}

function clearSearch() {
  query.value = ''
  results.value = { events: [], contacts: [], totalEvents: 0, totalContacts: 0 }
  searchInput.value?.focus()
}

function clearRecentSearches() {
  recentSearches.value = []
}

function applyRecentSearch(search: string) {
  query.value = search
}

// Debounced search
const performSearch = useDebounceFn(async () => {
  if (query.value.length < 2) {
    results.value = { events: [], contacts: [], totalEvents: 0, totalContacts: 0 }
    return
  }

  loading.value = true
  try {
    const { data } = await useApi().get('/api/v1/search', {
      params: { q: query.value, limit: 5 }
    })
    results.value = data

    // Save to recent searches
    if (!recentSearches.value.includes(query.value)) {
      recentSearches.value = [query.value, ...recentSearches.value].slice(0, 5)
    }
  } catch (error) {
    console.error('Search failed:', error)
  } finally {
    loading.value = false
  }
}, 300)

watch(query, () => {
  if (query.value.length >= 2) {
    performSearch()
  } else {
    results.value = { events: [], contacts: [], totalEvents: 0, totalContacts: 0 }
  }
})

function handleKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    visible.value = false
  } else if (e.key === 'ArrowDown') {
    e.preventDefault()
    navigateDown()
  } else if (e.key === 'ArrowUp') {
    e.preventDefault()
    navigateUp()
  } else if (e.key === 'Enter') {
    e.preventDefault()
    selectCurrent()
  } else if (e.key === 'Tab') {
    e.preventDefault()
    switchCategory()
  }
}

function navigateDown() {
  const maxIndex = activeCategory.value === 'event'
    ? results.value.events.length - 1
    : results.value.contacts.length - 1

  if (activeIndex.value < maxIndex) {
    activeIndex.value++
  } else if (activeCategory.value === 'event' && results.value.contacts.length > 0) {
    // Move to contacts section
    activeCategory.value = 'contact'
    activeIndex.value = 0
  }
}

function navigateUp() {
  if (activeIndex.value > 0) {
    activeIndex.value--
  } else if (activeCategory.value === 'contact' && results.value.events.length > 0) {
    // Move to events section
    activeCategory.value = 'event'
    activeIndex.value = results.value.events.length - 1
  }
}

function switchCategory() {
  if (activeCategory.value === 'event' && results.value.contacts.length > 0) {
    activeCategory.value = 'contact'
    activeIndex.value = 0
  } else if (activeCategory.value === 'contact' && results.value.events.length > 0) {
    activeCategory.value = 'event'
    activeIndex.value = 0
  }
}

function selectCurrent() {
  if (activeCategory.value === 'event' && results.value.events[activeIndex.value]) {
    selectEvent(results.value.events[activeIndex.value])
  } else if (activeCategory.value === 'contact' && results.value.contacts[activeIndex.value]) {
    selectContact(results.value.contacts[activeIndex.value])
  }
}

function isActive(category: 'event' | 'contact', index: number): boolean {
  return activeCategory.value === category && activeIndex.value === index
}

function setActive(category: 'event' | 'contact', index: number) {
  activeCategory.value = category
  activeIndex.value = index
}

function selectEvent(event: any) {
  visible.value = false
  router.push({
    path: '/calendar',
    query: {
      date: format(new Date(event.startTime), 'yyyy-MM-dd'),
      event: event.id
    }
  })
}

function selectContact(contact: any) {
  visible.value = false
  router.push(`/contacts/${contact.id}`)
}

function viewAllEvents() {
  visible.value = false
  router.push({
    path: '/calendar/search',
    query: { q: query.value }
  })
}

function viewAllContacts() {
  visible.value = false
  router.push({
    path: '/contacts',
    query: { q: query.value }
  })
}

function highlightMatch(text: string): string {
  if (!query.value || !text) return text
  const regex = new RegExp(`(${escapeRegex(query.value)})`, 'gi')
  return text.replace(regex, '<mark>$1</mark>')
}

function escapeRegex(str: string): string {
  return str.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

function formatEventDate(event: any): string {
  const date = new Date(event.startTime)
  if (event.isAllDay) {
    return format(date, 'EEE, MMM d')
  }
  return format(date, 'EEE, MMM d, HH:mm')
}

function getRelativeDate(event: any): string {
  const date = new Date(event.startTime)
  if (isToday(date)) return 'Today'
  if (isTomorrow(date)) return 'Tomorrow'
  return formatDistanceToNow(date, { addSuffix: true })
}

function isPastEvent(event: any): boolean {
  return isPast(new Date(event.endTime || event.startTime))
}

function getInitials(contact: any): string {
  const names = contact.formattedName?.split(' ') || []
  return names.map((n: string) => n[0]).slice(0, 2).join('').toUpperCase()
}
</script>

<style scoped>
.search-dialog :deep(.p-dialog-content) {
  padding: 0;
  border-radius: 12px;
  overflow: hidden;
}

.search-container {
  display: flex;
  flex-direction: column;
}

.search-input-wrapper {
  display: flex;
  align-items: center;
  padding: 1rem;
  border-bottom: 1px solid var(--surface-border);
  gap: 0.75rem;
}

.search-icon {
  font-size: 1.25rem;
  color: var(--text-color-secondary);
}

.search-input {
  flex: 1;
  border: none;
  outline: none;
  font-size: 1.125rem;
  background: transparent;
  color: var(--text-color);
}

.search-input::placeholder {
  color: var(--text-color-secondary);
}

.search-shortcut kbd {
  background: var(--surface-ground);
  border: 1px solid var(--surface-border);
  border-radius: 4px;
  padding: 0.125rem 0.5rem;
  font-size: 0.75rem;
  color: var(--text-color-secondary);
}

.search-content {
  max-height: 60vh;
  overflow-y: auto;
}

.search-loading {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 0.75rem;
  padding: 2rem;
  color: var(--text-color-secondary);
}

.recent-searches,
.empty-query,
.no-results {
  padding: 1rem;
}

.section-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 0.5rem 0;
  font-size: 0.75rem;
  font-weight: 600;
  text-transform: uppercase;
  color: var(--text-color-secondary);
}

.recent-list {
  display: flex;
  flex-direction: column;
}

.recent-item {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  padding: 0.5rem;
  cursor: pointer;
  border-radius: 6px;
  color: var(--text-color-secondary);
}

.recent-item:hover {
  background: var(--surface-hover);
  color: var(--text-color);
}

.empty-query {
  text-align: center;
  color: var(--text-color-secondary);
}

.search-tips {
  display: flex;
  justify-content: center;
  gap: 1.5rem;
  margin-top: 1rem;
}

.tip {
  font-size: 0.75rem;
}

.tip kbd {
  background: var(--surface-ground);
  border: 1px solid var(--surface-border);
  border-radius: 4px;
  padding: 0.125rem 0.375rem;
  font-size: 0.625rem;
  margin-right: 0.25rem;
}

.search-results {
  padding: 0.5rem;
}

.result-section {
  margin-bottom: 0.5rem;
}

.result-section .section-header {
  padding: 0.5rem;
}

.result-list {
  display: flex;
  flex-direction: column;
}

.result-item {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  padding: 0.75rem;
  cursor: pointer;
  border-radius: 6px;
  transition: background-color 0.15s;
}

.result-item:hover,
.result-item.active {
  background: var(--surface-hover);
}

.calendar-color {
  width: 4px;
  height: 32px;
  border-radius: 2px;
}

.result-info {
  flex: 1;
  min-width: 0;
}

.result-title {
  display: block;
  font-weight: 500;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.result-title :deep(mark) {
  background: var(--yellow-200);
  color: inherit;
  padding: 0 2px;
  border-radius: 2px;
}

.result-meta {
  font-size: 0.75rem;
  color: var(--text-color-secondary);
}

.result-relative {
  font-size: 0.75rem;
  color: var(--text-color-secondary);
  white-space: nowrap;
}

.view-all {
  width: 100%;
  margin-top: 0.5rem;
}

.no-results {
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 2rem;
  color: var(--text-color-secondary);
}

.no-results i {
  font-size: 2rem;
  opacity: 0.5;
  margin-bottom: 0.75rem;
}

.text-muted {
  font-size: 0.875rem;
}
</style>
```

### Search Store
```typescript
// stores/search.ts
import { defineStore } from 'pinia'
import { ref } from 'vue'

interface SearchResult {
  events: any[]
  contacts: any[]
  totalEvents: number
  totalContacts: number
}

export const useSearchStore = defineStore('search', () => {
  const cache = ref<Map<string, SearchResult>>(new Map())
  const lastQuery = ref('')

  async function search(query: string, limit: number = 5): Promise<SearchResult> {
    // Check cache first
    const cacheKey = `${query}_${limit}`
    if (cache.value.has(cacheKey)) {
      return cache.value.get(cacheKey)!
    }

    const { data } = await useApi().get('/api/v1/search', {
      params: { q: query, limit }
    })

    // Cache results
    cache.value.set(cacheKey, data)
    lastQuery.value = query

    return data
  }

  function clearCache() {
    cache.value.clear()
  }

  return {
    cache,
    lastQuery,
    search,
    clearCache
  }
})
```

## Dependencies
- Story 031 (Frontend Project Setup)
- Story 033 (Layout & Navigation) - header integration
- Story 034 (Calendar Views) - event navigation
- Story 036 (Contact List UI) - contact navigation

## Estimation
- **Complexity:** Medium
- **Components:** 1 main component, 1 store

## Notes
- Keyboard shortcut should work globally when dialog is closed
- Consider adding search filters (date range, specific calendars, etc.)
- Results should highlight matching text portions
- Cache should be cleared when data changes (events/contacts CRUD)
- Recent searches persisted in localStorage
