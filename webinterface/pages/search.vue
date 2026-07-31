<template>
  <div class="p-4 lg:p-6 max-w-3xl mx-auto">
    <h1 class="text-xl font-semibold text-surface-900 dark:text-surface-100 mb-4">Search</h1>

    <!-- Query input. Mirrored into ?q= so a results page can be bookmarked/shared. -->
    <div class="relative">
      <i class="pi pi-search absolute left-3 top-1/2 -translate-y-1/2 text-surface-400 text-sm" />
      <input
        v-model="query"
        type="text"
        placeholder="Search events, contacts, calendars and address books..."
        aria-label="Search query"
        autocomplete="off"
        class="w-full pl-9 pr-3 py-2 text-sm rounded-lg border border-surface-300 dark:border-surface-600 bg-surface-0 dark:bg-surface-800 text-surface-900 dark:text-surface-100 placeholder-surface-400 focus:outline-none focus:ring-2 focus:ring-primary-500 focus:border-transparent"
      >
    </div>

    <p v-if="isSearchable && !searchStore.isLoading" class="mt-3 text-xs text-surface-500">
      {{ searchStore.totalCount }} {{ searchStore.totalCount === 1 ? 'result' : 'results' }}
      for “{{ searchStore.query }}” · events are searched from {{ windowLabel }}.
    </p>

    <!-- A failed leg is reported above the results, never instead of them. -->
    <div
      v-if="searchStore.error"
      class="flex items-start gap-2 mt-4 px-3 py-2 rounded-lg bg-amber-50 dark:bg-amber-950/40 text-xs text-amber-800 dark:text-amber-200"
      data-testid="search-page-error"
    >
      <i class="pi pi-exclamation-triangle mt-0.5" />
      <span>{{ searchStore.error }}</span>
    </div>

    <div v-if="searchStore.isLoading" class="flex items-center justify-center gap-3 p-10 text-surface-500">
      <ProgressSpinner style="width: 1.75rem; height: 1.75rem" stroke-width="4" />
      <span class="text-sm">Searching…</span>
    </div>

    <div v-else-if="!isSearchable" class="flex flex-col items-center gap-2 p-10 text-center">
      <i class="pi pi-search text-2xl text-surface-300 dark:text-surface-600" />
      <p class="text-sm text-surface-500">Type at least {{ MIN_QUERY_LENGTH }} characters to search.</p>
    </div>

    <!-- Full result set: NO per-category cap here — that is what separates this page
         from the header palette (story 044). -->
    <div v-else-if="searchStore.hasResults" class="mt-4 space-y-6">
      <section v-if="searchStore.results.events.length > 0">
        <SearchSectionHeader label="Events" :count="searchStore.results.events.length" />
        <button
          v-for="hit in searchStore.results.events"
          :key="hit.key"
          class="w-full flex items-center gap-3 px-3 py-2 rounded-lg text-left hover:bg-surface-100 dark:hover:bg-surface-800"
          data-testid="page-event-row"
          @click="selectEvent(hit)"
        >
          <span class="w-1 h-8 rounded flex-shrink-0" :style="{ backgroundColor: hit.calendarColor }" />
          <span class="flex-1 min-w-0">
            <span class="block text-sm font-medium text-surface-900 dark:text-surface-100 truncate">
              <HighlightText :text="hit.event.summary || '(no title)'" :highlight="searchStore.query" />
            </span>
            <span class="block text-xs text-surface-500 truncate">
              {{ formatEventWhen(hit.event) }} · {{ hit.calendarName }}
            </span>
          </span>
          <Tag v-if="isPastEvent(hit.event)" value="Past" severity="secondary" class="flex-shrink-0 text-xs" />
          <span v-else class="text-xs text-surface-500 flex-shrink-0">{{ eventRelativeLabel(hit.event) }}</span>
        </button>
      </section>

      <section v-if="searchStore.results.contacts.length > 0">
        <SearchSectionHeader label="Contacts" :count="searchStore.results.contacts.length" />
        <button
          v-for="hit in searchStore.results.contacts"
          :key="hit.contact.id"
          class="w-full flex items-center gap-3 px-3 py-2 rounded-lg text-left hover:bg-surface-100 dark:hover:bg-surface-800"
          data-testid="page-contact-row"
          @click="selectContact(hit)"
        >
          <span
            class="w-8 h-8 rounded-full flex items-center justify-center text-white text-xs font-semibold flex-shrink-0"
            :style="{ backgroundColor: searchAvatarColor(hit.contact.formatted_name || hit.contact.id) }"
          >
            {{ searchInitials(hit.contact.formatted_name) }}
          </span>
          <span class="flex-1 min-w-0">
            <span class="block text-sm font-medium text-surface-900 dark:text-surface-100 truncate">
              <HighlightText :text="hit.contact.formatted_name" :highlight="searchStore.query" />
            </span>
            <span class="block text-xs text-surface-500 truncate">
              <HighlightText :text="contactMetaLine(hit)" :highlight="searchStore.query" />
            </span>
          </span>
        </button>
      </section>

      <section v-if="searchStore.results.calendars.length > 0">
        <SearchSectionHeader label="Calendars" :count="searchStore.results.calendars.length" />
        <NuxtLink
          v-for="cal in searchStore.results.calendars"
          :key="cal.uuid"
          to="/calendar"
          class="w-full flex items-center gap-3 px-3 py-2 rounded-lg hover:bg-surface-100 dark:hover:bg-surface-800"
        >
          <span class="w-3 h-3 rounded-full flex-shrink-0" :style="{ backgroundColor: cal.color || '#3b82f6' }" />
          <span class="flex-1 min-w-0">
            <span class="block text-sm font-medium text-surface-900 dark:text-surface-100 truncate">
              <HighlightText :text="cal.name" :highlight="searchStore.query" />
            </span>
            <span v-if="cal.description" class="block text-xs text-surface-500 truncate">
              <HighlightText :text="cal.description" :highlight="searchStore.query" />
            </span>
          </span>
        </NuxtLink>
      </section>

      <section v-if="searchStore.results.addressBooks.length > 0">
        <SearchSectionHeader label="Address books" :count="searchStore.results.addressBooks.length" />
        <NuxtLink
          v-for="ab in searchStore.results.addressBooks"
          :key="ab.UUID"
          to="/contacts"
          class="w-full flex items-center gap-3 px-3 py-2 rounded-lg hover:bg-surface-100 dark:hover:bg-surface-800"
        >
          <i class="pi pi-book text-surface-400 text-sm flex-shrink-0" />
          <span class="flex-1 min-w-0">
            <span class="block text-sm font-medium text-surface-900 dark:text-surface-100 truncate">
              <HighlightText :text="ab.Name" :highlight="searchStore.query" />
            </span>
            <span v-if="ab.Description" class="block text-xs text-surface-500 truncate">
              <HighlightText :text="ab.Description" :highlight="searchStore.query" />
            </span>
          </span>
        </NuxtLink>
      </section>
    </div>

    <div v-else class="flex flex-col items-center gap-2 p-10 text-center" data-testid="page-no-results">
      <i class="pi pi-search text-2xl text-surface-300 dark:text-surface-600" />
      <p class="text-sm text-surface-600 dark:text-surface-300">No results for “{{ searchStore.query }}”</p>
      <p class="text-xs text-surface-400">Try different keywords or check the spelling.</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import HighlightText from '~/components/common/HighlightText.vue';
import SearchSectionHeader from '~/components/common/SearchSectionHeader.vue';
import { MIN_QUERY_LENGTH, SEARCH_WINDOW_MONTHS, useSearchStore } from '~/stores/search';
import type { ContactHit, EventHit } from '~/types/search';
import {
  contactMetaLine,
  eventRelativeLabel,
  formatEventWhen,
  isPastEvent,
  searchAvatarColor,
  searchInitials,
  toLocalDateParam,
} from '~/utils/searchFormat';

definePageMeta({
  middleware: 'auth',
  layout: 'default',
});

/**
 * Full results page for global search (story 044). The header palette caps each
 * category at five rows and links here with ?q=…; this page runs the same store
 * action and renders EVERY match. It deliberately re-uses the search store (and
 * therefore its 60s query cache), so arriving from "View all" normally renders
 * without issuing a single new request.
 */
const route = useRoute();
const searchStore = useSearchStore();

const initialQuery = typeof route.query.q === 'string' ? route.query.q : '';
const query = ref(initialQuery);

const isSearchable = computed(() => query.value.trim().length >= MIN_QUERY_LENGTH);
const windowLabel = computed(() => `${SEARCH_WINDOW_MONTHS} months back to ${SEARCH_WINDOW_MONTHS} months ahead`);

const runSearch = useDebounceFn(() => {
  // Same fire-time re-read as the palette: useDebounceFn cannot be cancelled, so a
  // timer armed for text the user has since deleted must no-op instead of searching.
  const value = query.value.trim();
  if (value.length < MIN_QUERY_LENGTH) return;
  searchStore.search(value);
}, 300);

watch(query, (value) => {
  const trimmed = value.trim();
  // Keep the URL shareable/reloadable without pushing a history entry per keystroke.
  navigateTo({ path: '/search', query: trimmed ? { q: trimmed } : {} }, { replace: true });
  if (trimmed.length >= MIN_QUERY_LENGTH) {
    runSearch();
  } else {
    searchStore.search('');
  }
});

// Arriving here from the header palette while ALREADY on /search changes only the
// query string — the page is not remounted, and the palette's close handler resets
// the store — so mirror ?q= back into the input and search again (normally served
// from the store's cache).
watch(
  () => route.query.q,
  (q) => {
    const next = typeof q === 'string' ? q : '';
    if (next !== query.value) query.value = next;
  }
);

onMounted(() => {
  if (isSearchable.value) searchStore.search(query.value);
});

const selectEvent = (hit: EventHit) => {
  navigateTo({
    path: '/calendar',
    query: {
      date: toLocalDateParam(new Date(hit.event.start)),
      event: hit.event.id,
      cal: hit.calendarId,
      ...(hit.event.recurrence_id ? { recurrence: hit.event.recurrence_id } : {}),
    },
  });
};

const selectContact = (hit: ContactHit) => {
  // NUMERIC address book id — `Contact.addressbook_id`, not the UUID.
  navigateTo(`/contacts/${hit.contact.id}?ab=${encodeURIComponent(hit.addressBookId)}`);
};
</script>
