<template>
  <div>
    <!-- Trigger. PrimeVue's Tooltip directive is not registered in this project,
         so the affordance is a plain title/aria-label. -->
    <button
      class="p-2 rounded-lg text-surface-500 hover:bg-surface-100 dark:hover:bg-surface-800 transition-colors focus:outline-none focus:ring-2 focus:ring-primary-500"
      title="Search events and contacts"
      aria-label="Search events and contacts"
      @click="openSearch"
    >
      <i class="pi pi-search text-lg" />
    </button>

    <Dialog
      v-model:visible="visible"
      modal
      :show-header="false"
      dismissable-mask
      :draggable="false"
      position="top"
      class="global-search-dialog w-[min(40rem,92vw)] mt-[8vh]"
      @show="focusInput"
    >
      <!-- Input row -->
      <div class="flex items-center gap-3 px-4 py-3 border-b border-surface-200 dark:border-surface-700">
        <i class="pi pi-search text-surface-400" />
        <input
          ref="inputRef"
          v-model="query"
          type="text"
          class="flex-1 bg-transparent border-0 outline-none text-base text-surface-900 dark:text-surface-100 placeholder:text-surface-400"
          placeholder="Search events and contacts…"
          aria-label="Search query"
          @keydown.escape.stop.prevent="visible = false"
        >
        <button
          v-if="query"
          class="p-1.5 rounded-full text-surface-400 hover:bg-surface-100 dark:hover:bg-surface-800"
          title="Clear search"
          aria-label="Clear search"
          @click="clearQuery"
        >
          <i class="pi pi-times text-sm" />
        </button>
        <kbd class="hidden sm:inline-block text-[0.625rem] px-1.5 py-0.5 rounded border border-surface-300 dark:border-surface-600 text-surface-400">
          ESC
        </kbd>
      </div>

      <!-- Content -->
      <div class="max-h-[60vh] overflow-y-auto">
        <!-- Loading (only when there is nothing to keep on screen) -->
        <div
          v-if="(searchStore.isLoading || isPending) && !searchStore.hasResults"
          class="flex items-center justify-center gap-3 p-8 text-surface-500"
        >
          <ProgressSpinner style="width: 1.75rem; height: 1.75rem" stroke-width="4" />
          <span class="text-sm">Searching…</span>
        </div>

        <!-- Recent searches (empty query) -->
        <div v-else-if="!isSearchable && searchStore.recentSearches.length > 0" class="p-2">
          <div class="flex items-center justify-between px-2 py-1">
            <span class="text-xs font-semibold uppercase text-surface-400">Recent searches</span>
            <button
              class="text-xs text-primary-600 dark:text-primary-400 hover:underline"
              @click="searchStore.clearRecentSearches()"
            >
              Clear
            </button>
          </div>
          <button
            v-for="recent in searchStore.recentSearches"
            :key="recent"
            class="w-full flex items-center gap-3 px-3 py-2 rounded-lg text-left text-sm text-surface-600 dark:text-surface-300 hover:bg-surface-100 dark:hover:bg-surface-800"
            @click="query = recent"
          >
            <i class="pi pi-history text-surface-400 text-sm" />
            <span class="truncate">{{ recent }}</span>
          </button>
        </div>

        <!-- Empty state (nothing typed yet) -->
        <div v-else-if="!isSearchable" class="flex flex-col items-center gap-2 p-8 text-center">
          <i class="pi pi-search text-2xl text-surface-300 dark:text-surface-600" />
          <p class="text-sm text-surface-500">Start typing to search events, contacts, calendars and address books.</p>
          <p class="text-xs text-surface-400">At least {{ MIN_QUERY_LENGTH }} characters.</p>
        </div>

        <!-- Error -->
        <div v-else-if="searchStore.error" class="flex flex-col items-center gap-2 p-8 text-center">
          <i class="pi pi-exclamation-triangle text-2xl text-red-400" />
          <p class="text-sm text-surface-600 dark:text-surface-300">{{ searchStore.error }}</p>
        </div>

        <!-- Results -->
        <div v-else-if="searchStore.hasResults" class="p-2">
          <!-- Events -->
          <section v-if="searchStore.results.events.length > 0" class="mb-2">
            <CommonSearchSectionHeader label="Events" :count="searchStore.results.events.length" />
            <button
              v-for="hit in visibleSlice(searchStore.results.events, 'events')"
              :key="hit.key"
              class="w-full flex items-center gap-3 px-3 py-2 rounded-lg text-left hover:bg-surface-100 dark:hover:bg-surface-800"
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
              <Tag v-if="isPast(hit.event)" value="Past" severity="secondary" class="flex-shrink-0 text-xs" />
              <span v-else class="text-xs text-surface-500 flex-shrink-0">{{ relativeLabel(hit.event) }}</span>
            </button>
            <CommonSearchViewAll
              v-if="isTruncated(searchStore.results.events, 'events')"
              :label="`View all ${searchStore.results.events.length} events`"
              @click="expanded.events = true"
            />
          </section>

          <!-- Contacts -->
          <section v-if="searchStore.results.contacts.length > 0" class="mb-2">
            <CommonSearchSectionHeader label="Contacts" :count="searchStore.results.contacts.length" />
            <button
              v-for="hit in visibleSlice(searchStore.results.contacts, 'contacts')"
              :key="hit.contact.id"
              class="w-full flex items-center gap-3 px-3 py-2 rounded-lg text-left hover:bg-surface-100 dark:hover:bg-surface-800"
              @click="selectContact(hit)"
            >
              <span
                class="w-8 h-8 rounded-full flex items-center justify-center text-white text-xs font-semibold flex-shrink-0"
                :style="{ backgroundColor: avatarColor(hit.contact.formatted_name || hit.contact.id) }"
              >
                {{ initials(hit.contact.formatted_name) }}
              </span>
              <span class="flex-1 min-w-0">
                <span class="block text-sm font-medium text-surface-900 dark:text-surface-100 truncate">
                  <HighlightText :text="hit.contact.formatted_name" :highlight="searchStore.query" />
                </span>
                <span class="block text-xs text-surface-500 truncate">
                  <HighlightText :text="contactMeta(hit)" :highlight="searchStore.query" />
                </span>
              </span>
            </button>
            <CommonSearchViewAll
              v-if="isTruncated(searchStore.results.contacts, 'contacts')"
              :label="`View all ${searchStore.results.contacts.length} contacts`"
              @click="expanded.contacts = true"
            />
          </section>

          <!-- Calendars -->
          <section v-if="searchStore.results.calendars.length > 0" class="mb-2">
            <CommonSearchSectionHeader label="Calendars" :count="searchStore.results.calendars.length" />
            <button
              v-for="cal in visibleSlice(searchStore.results.calendars, 'calendars')"
              :key="cal.uuid"
              class="w-full flex items-center gap-3 px-3 py-2 rounded-lg text-left hover:bg-surface-100 dark:hover:bg-surface-800"
              @click="selectCalendar()"
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
            </button>
            <CommonSearchViewAll
              v-if="isTruncated(searchStore.results.calendars, 'calendars')"
              :label="`View all ${searchStore.results.calendars.length} calendars`"
              @click="expanded.calendars = true"
            />
          </section>

          <!-- Address books -->
          <section v-if="searchStore.results.addressBooks.length > 0">
            <CommonSearchSectionHeader label="Address books" :count="searchStore.results.addressBooks.length" />
            <button
              v-for="ab in visibleSlice(searchStore.results.addressBooks, 'addressBooks')"
              :key="ab.UUID"
              class="w-full flex items-center gap-3 px-3 py-2 rounded-lg text-left hover:bg-surface-100 dark:hover:bg-surface-800"
              @click="selectAddressBook()"
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
            </button>
            <CommonSearchViewAll
              v-if="isTruncated(searchStore.results.addressBooks, 'addressBooks')"
              :label="`View all ${searchStore.results.addressBooks.length} address books`"
              @click="expanded.addressBooks = true"
            />
          </section>
        </div>

        <!-- No results -->
        <div v-else class="flex flex-col items-center gap-2 p-8 text-center">
          <i class="pi pi-search text-2xl text-surface-300 dark:text-surface-600" />
          <p class="text-sm text-surface-600 dark:text-surface-300">No results for “{{ searchStore.query }}”</p>
          <p class="text-xs text-surface-400">
            Try different keywords. Events are searched from {{ windowLabel }}.
          </p>
        </div>
      </div>
    </Dialog>
  </div>
</template>

<script setup lang="ts">
import HighlightText from '~/components/common/HighlightText.vue';
import CommonSearchSectionHeader from '~/components/common/SearchSectionHeader.vue';
import CommonSearchViewAll from '~/components/common/SearchViewAll.vue';
import { MIN_QUERY_LENGTH, SEARCH_WINDOW_MONTHS, useSearchStore } from '~/stores/search';
import type { CalendarEvent } from '~/types/calendar';
import type { ContactHit, EventHit } from '~/types/search';

/** Rows shown per category before "View all" expands the section in place. */
const QUICK_LIMIT = 5;

type Category = 'events' | 'contacts' | 'calendars' | 'addressBooks';

const searchStore = useSearchStore();

const visible = ref(false);
const query = ref('');
const inputRef = ref<HTMLInputElement | null>(null);
const expanded = ref<Record<Category, boolean>>({
  events: false,
  contacts: false,
  calendars: false,
  addressBooks: false,
});

const isSearchable = computed(() => query.value.trim().length >= MIN_QUERY_LENGTH);

// True while the debounce is still pending (what's typed differs from what the
// store last searched). Without this the "no results" state flashes for 300ms on
// the first keystroke, quoting an empty query.
const isPending = computed(() => isSearchable.value && searchStore.query !== query.value.trim());
const windowLabel = computed(() => `${SEARCH_WINDOW_MONTHS} months back to ${SEARCH_WINDOW_MONTHS} months ahead`);

// 300ms debounce (story 044). The store's requestSeq guard is what actually makes
// this safe — debouncing only reduces the number of fan-outs, it cannot prevent an
// earlier slow response from landing after a later fast one.
const runSearch = useDebounceFn((value: string) => {
  searchStore.search(value);
}, 300);

watch(query, (value) => {
  // Collapse expansions when the query changes: "view all" applies to one result set.
  expanded.value = { events: false, contacts: false, calendars: false, addressBooks: false };
  if (value.trim().length >= MIN_QUERY_LENGTH) {
    runSearch(value);
  } else {
    // Short/empty query: clear immediately (and invalidate in-flight requests)
    // rather than waiting out the debounce.
    searchStore.search('');
  }
});

const openSearch = () => {
  searchStore.loadRecentSearches();
  visible.value = true;
};

const focusInput = () => {
  nextTick(() => inputRef.value?.focus());
};

const clearQuery = () => {
  query.value = '';
  inputRef.value?.focus();
};

// Closing (Escape, mask click, or a selection) drops the query and any in-flight
// fan-out so the next open starts from the recent-searches state.
watch(visible, (open) => {
  if (!open) {
    query.value = '';
    searchStore.reset();
  }
});

function visibleSlice<T>(items: T[], category: Category): T[] {
  return expanded.value[category] ? items : items.slice(0, QUICK_LIMIT);
}

function isTruncated<T>(items: T[], category: Category): boolean {
  return !expanded.value[category] && items.length > QUICK_LIMIT;
}

const finishSelection = () => {
  searchStore.rememberSearch(query.value);
  visible.value = false;
};

const selectEvent = (hit: EventHit) => {
  const start = new Date(hit.event.start);
  finishSelection();
  // The calendar page reads these params to jump to the date and open the event's
  // detail dialog (story 044). `cal` is the numeric calendar id the store maps to
  // a UUID for the API call (#52).
  navigateTo({
    path: '/calendar',
    query: {
      date: toDateParam(start),
      event: hit.event.id,
      cal: hit.calendarId,
      ...(hit.event.recurrence_id ? { recurrence: hit.event.recurrence_id } : {}),
    },
  });
};

const selectContact = (hit: ContactHit) => {
  const id = hit.contact.id;
  const ab = hit.addressBookId;
  finishSelection();
  // The contact detail route needs the NUMERIC address book id as ?ab= — it is
  // what `Contact.addressbook_id` carries, and the page refuses to load without it.
  navigateTo(`/contacts/${id}?ab=${encodeURIComponent(ab)}`);
};

const selectCalendar = () => {
  finishSelection();
  navigateTo('/calendar');
};

const selectAddressBook = () => {
  finishSelection();
  navigateTo('/contacts');
};

function toDateParam(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

const dayFormatter = new Intl.DateTimeFormat(undefined, { weekday: 'short', day: 'numeric', month: 'short' });
const timeFormatter = new Intl.DateTimeFormat(undefined, { hour: '2-digit', minute: '2-digit' });

function formatEventWhen(event: CalendarEvent): string {
  const start = new Date(event.start);
  if (Number.isNaN(start.getTime())) return '';
  if (event.all_day) return dayFormatter.format(start);
  return `${dayFormatter.format(start)}, ${timeFormatter.format(start)}`;
}

function startOfDay(d: Date): number {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
}

const DAY_MS = 86_400_000;

function relativeLabel(event: CalendarEvent): string {
  const start = new Date(event.start);
  if (Number.isNaN(start.getTime())) return '';
  const days = Math.round((startOfDay(start) - startOfDay(new Date())) / DAY_MS);
  if (days === 0) return 'Today';
  if (days === 1) return 'Tomorrow';
  return dayFormatter.format(start);
}

function isPast(event: CalendarEvent): boolean {
  const reference = new Date(event.end || event.start);
  if (Number.isNaN(reference.getTime())) return false;
  return reference.getTime() < Date.now();
}

function initials(name: string | undefined): string {
  const parts = (name || '').split(/\s+/).filter(Boolean);
  if (parts.length >= 2) return (parts[0]!.charAt(0) + parts[parts.length - 1]!.charAt(0)).toUpperCase();
  return (parts[0]?.charAt(0) || '?').toUpperCase();
}

// Same hash-to-palette scheme as the contact list/detail views, so a contact keeps
// one colour everywhere.
const AVATAR_COLORS = [
  '#3b82f6', '#ef4444', '#10b981', '#f59e0b', '#8b5cf6',
  '#ec4899', '#06b6d4', '#f97316', '#6366f1', '#14b8a6',
];

function avatarColor(seed: string): string {
  let hash = 0;
  for (let i = 0; i < seed.length; i++) {
    hash = seed.charCodeAt(i) + ((hash << 5) - hash);
  }
  return AVATAR_COLORS[Math.abs(hash) % AVATAR_COLORS.length]!;
}

function contactMeta(hit: ContactHit): string {
  const contact = hit.contact;
  const email = contact.emails?.find((e) => e.primary)?.value || contact.emails?.[0]?.value || '';
  return [contact.organization, email, hit.addressBookName].filter(Boolean).join(' · ');
}
</script>

<style>
/* The dialog is a search palette: the input row owns the top edge, so drop the
   default content padding instead of nesting another padded wrapper.
   Deliberately NOT scoped — PrimeVue teleports the dialog to <body>, so a scoped
   [data-v-…] selector would not reach .p-dialog-content. The class prefix keeps
   the rule confined to this dialog. Unlayered CSS also outranks PrimeVue's
   @layer primevue styles, so no !important is needed. */
.global-search-dialog .p-dialog-content {
  padding: 0;
  overflow: hidden;
}
</style>
