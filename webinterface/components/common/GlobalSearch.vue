<template>
  <div>
    <!-- Trigger. PrimeVue's Tooltip directive is not registered in this project,
         so the affordance is a plain title/aria-label. -->
    <button
      class="p-2 rounded-lg text-surface-500 dark:text-surface-400 hover:bg-surface-100 dark:hover:bg-surface-800 transition-colors focus:outline-none focus:ring-2 focus:ring-primary-500"
      :title="`Search events and contacts (${shortcutLabel})`"
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
        <i class="pi pi-search text-surface-500 dark:text-surface-400" />
        <input
          ref="inputRef"
          v-model="query"
          type="text"
          class="flex-1 bg-transparent border-0 outline-none text-base text-surface-900 dark:text-surface-100 placeholder:text-surface-400"
          placeholder="Search events and contacts…"
          aria-label="Search query"
          role="combobox"
          aria-controls="global-search-results"
          :aria-expanded="navRows.length > 0"
          :aria-activedescendant="activeRowId"
          autocomplete="off"
          @keydown.escape.stop.prevent="visible = false"
          @keydown.down.prevent="moveActive(1)"
          @keydown.up.prevent="moveActive(-1)"
          @keydown.enter.prevent="activateActive"
          @keydown.tab.exact.prevent="cycleCategory(1)"
          @keydown.tab.shift.prevent="cycleCategory(-1)"
        >
        <button
          v-if="query"
          class="p-1.5 rounded-full text-surface-500 dark:text-surface-400 hover:bg-surface-100 dark:hover:bg-surface-800"
          title="Clear search"
          aria-label="Clear search"
          @click="clearQuery"
        >
          <i class="pi pi-times text-sm" />
        </button>
        <kbd class="hidden sm:inline-block text-[0.625rem] px-1.5 py-0.5 rounded border border-surface-300 dark:border-surface-600 text-surface-500 dark:text-surface-400">
          ESC
        </kbd>
      </div>

      <!-- Content. Rows are options of one flat listbox spanning every category, so
           Up/Down crosses category boundaries the way Tab does not (story 044). -->
      <div id="global-search-results" ref="resultsRef" class="max-h-[60vh] overflow-y-auto" role="listbox" aria-label="Search results">
        <!-- Loading (only when there is nothing to keep on screen) -->
        <div
          v-if="(searchStore.isLoading || isPending) && !searchStore.hasResults"
          class="flex items-center justify-center gap-3 p-8 text-surface-500 dark:text-surface-400"
          data-testid="search-loading"
        >
          <ProgressSpinner style="width: 1.75rem; height: 1.75rem" stroke-width="4" />
          <span class="text-sm">Searching…</span>
        </div>

        <!-- Recent searches (empty query) -->
        <div v-else-if="!isSearchable && searchStore.recentSearches.length > 0" class="p-2">
          <div class="flex items-center justify-between px-2 py-1">
            <span class="text-xs font-semibold uppercase text-surface-500 dark:text-surface-400">Recent searches</span>
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
            <i class="pi pi-history text-surface-500 dark:text-surface-400 text-sm" />
            <span class="truncate">{{ recent }}</span>
          </button>
        </div>

        <!-- Empty state (nothing typed yet) -->
        <div v-else-if="!isSearchable" class="flex flex-col items-center gap-2 p-8 text-center">
          <i class="pi pi-search text-2xl text-surface-300 dark:text-surface-600" />
          <p class="text-sm text-surface-500 dark:text-surface-400">Start typing to search events, contacts, calendars and address books.</p>
          <p class="text-xs text-surface-500 dark:text-surface-400">At least {{ MIN_QUERY_LENGTH }} characters.</p>
          <div class="flex flex-wrap justify-center gap-4 pt-2 text-xs text-surface-500 dark:text-surface-400">
            <span><kbd class="search-kbd">↑</kbd><kbd class="search-kbd">↓</kbd> Navigate</span>
            <span><kbd class="search-kbd">Enter</kbd> Select</span>
            <span><kbd class="search-kbd">Tab</kbd> Switch category</span>
          </div>
        </div>

        <!-- Results. The error banner sits ABOVE them: when only one leg of the
             fan-out failed (or only the local calendar/address-book matches
             survived), hiding the results behind the error would throw away
             perfectly good hits. -->
        <div v-else-if="searchStore.hasResults" class="p-2">
          <div
            v-if="searchStore.error"
            class="flex items-start gap-2 mx-1 mb-2 px-3 py-2 rounded-lg bg-amber-50 dark:bg-amber-950/40 text-xs text-amber-800 dark:text-amber-200"
            data-testid="search-partial-error"
          >
            <i class="pi pi-exclamation-triangle mt-0.5" />
            <span>{{ searchStore.error }}</span>
          </div>

          <!-- Events -->
          <section v-if="searchStore.results.events.length > 0" class="mb-2">
            <SearchSectionHeader label="Events" :count="searchStore.results.events.length" />
            <button
              v-for="(hit, i) in quick(searchStore.results.events)"
              :id="rowDomId(navIndex('events', i))"
              :key="hit.key"
              role="option"
              :aria-selected="isActive('events', i)"
              class="w-full flex items-center gap-3 px-3 py-2 rounded-lg text-left hover:bg-surface-100 dark:hover:bg-surface-800"
              :class="{ 'bg-surface-100 dark:bg-surface-800': isActive('events', i) }"
              data-testid="event-row"
              @click="selectEvent(hit)"
              @mousemove="activeIndex = navIndex('events', i)"
            >
              <span class="w-1 h-8 rounded flex-shrink-0" :style="{ backgroundColor: hit.calendarColor }" />
              <span class="flex-1 min-w-0">
                <span class="block text-sm font-medium text-surface-900 dark:text-surface-100 truncate">
                  <HighlightText :text="hit.event.summary || '(no title)'" :highlight="searchStore.query" />
                </span>
                <span class="block text-xs text-surface-500 dark:text-surface-400 truncate">
                  {{ formatEventWhen(hit.event) }} · {{ hit.calendarName }}
                </span>
              </span>
              <Tag v-if="isPastEvent(hit.event)" value="Past" severity="secondary" class="flex-shrink-0 text-xs" />
              <span v-else class="text-xs text-surface-500 dark:text-surface-400 flex-shrink-0">{{ eventRelativeLabel(hit.event) }}</span>
            </button>
            <SearchViewAll
              v-if="isTruncated(searchStore.results.events)"
              :label="`View all ${totalLabel('events')} events`"
              @click="viewAll"
            />
          </section>

          <!-- Contacts -->
          <section v-if="searchStore.results.contacts.length > 0" class="mb-2">
            <SearchSectionHeader label="Contacts" :count="searchStore.results.contacts.length" />
            <button
              v-for="(hit, i) in quick(searchStore.results.contacts)"
              :id="rowDomId(navIndex('contacts', i))"
              :key="hit.contact.id"
              role="option"
              :aria-selected="isActive('contacts', i)"
              class="w-full flex items-center gap-3 px-3 py-2 rounded-lg text-left hover:bg-surface-100 dark:hover:bg-surface-800"
              :class="{ 'bg-surface-100 dark:bg-surface-800': isActive('contacts', i) }"
              data-testid="contact-row"
              @click="selectContact(hit)"
              @mousemove="activeIndex = navIndex('contacts', i)"
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
                <span class="block text-xs text-surface-500 dark:text-surface-400 truncate">
                  <HighlightText :text="contactMetaLine(hit)" :highlight="searchStore.query" />
                </span>
              </span>
            </button>
            <SearchViewAll
              v-if="isTruncated(searchStore.results.contacts)"
              :label="`View all ${totalLabel('contacts')} contacts`"
              @click="viewAll"
            />
          </section>

          <!-- Calendars -->
          <section v-if="searchStore.results.calendars.length > 0" class="mb-2">
            <SearchSectionHeader label="Calendars" :count="searchStore.results.calendars.length" />
            <button
              v-for="(cal, i) in quick(searchStore.results.calendars)"
              :id="rowDomId(navIndex('calendars', i))"
              :key="cal.uuid"
              role="option"
              :aria-selected="isActive('calendars', i)"
              class="w-full flex items-center gap-3 px-3 py-2 rounded-lg text-left hover:bg-surface-100 dark:hover:bg-surface-800"
              :class="{ 'bg-surface-100 dark:bg-surface-800': isActive('calendars', i) }"
              @click="selectCalendar()"
              @mousemove="activeIndex = navIndex('calendars', i)"
            >
              <span class="w-3 h-3 rounded-full flex-shrink-0" :style="{ backgroundColor: cal.color || '#3b82f6' }" />
              <span class="flex-1 min-w-0">
                <span class="block text-sm font-medium text-surface-900 dark:text-surface-100 truncate">
                  <HighlightText :text="cal.name" :highlight="searchStore.query" />
                </span>
                <span v-if="cal.description" class="block text-xs text-surface-500 dark:text-surface-400 truncate">
                  <HighlightText :text="cal.description" :highlight="searchStore.query" />
                </span>
              </span>
            </button>
            <SearchViewAll
              v-if="isTruncated(searchStore.results.calendars)"
              :label="`View all ${totalLabel('calendars')} calendars`"
              @click="viewAll"
            />
          </section>

          <!-- Address books -->
          <section v-if="searchStore.results.addressBooks.length > 0">
            <SearchSectionHeader label="Address books" :count="searchStore.results.addressBooks.length" />
            <button
              v-for="(ab, i) in quick(searchStore.results.addressBooks)"
              :id="rowDomId(navIndex('addressBooks', i))"
              :key="ab.UUID"
              role="option"
              :aria-selected="isActive('addressBooks', i)"
              class="w-full flex items-center gap-3 px-3 py-2 rounded-lg text-left hover:bg-surface-100 dark:hover:bg-surface-800"
              :class="{ 'bg-surface-100 dark:bg-surface-800': isActive('addressBooks', i) }"
              @click="selectAddressBook()"
              @mousemove="activeIndex = navIndex('addressBooks', i)"
            >
              <i class="pi pi-book text-surface-500 dark:text-surface-400 text-sm flex-shrink-0" />
              <span class="flex-1 min-w-0">
                <span class="block text-sm font-medium text-surface-900 dark:text-surface-100 truncate">
                  <HighlightText :text="ab.Name" :highlight="searchStore.query" />
                </span>
                <span v-if="ab.Description" class="block text-xs text-surface-500 dark:text-surface-400 truncate">
                  <HighlightText :text="ab.Description" :highlight="searchStore.query" />
                </span>
              </span>
            </button>
            <SearchViewAll
              v-if="isTruncated(searchStore.results.addressBooks)"
              :label="`View all ${totalLabel('addressBooks')} address books`"
              @click="viewAll"
            />
          </section>
        </div>

        <!-- Nothing usable at all AND something failed: report the failure rather
             than claiming there are no matches. -->
        <div v-else-if="searchStore.error" class="flex flex-col items-center gap-2 p-8 text-center" data-testid="search-error">
          <i class="pi pi-exclamation-triangle text-2xl text-red-400" />
          <p class="text-sm text-surface-600 dark:text-surface-300">{{ searchStore.error }}</p>
        </div>

        <!-- No results -->
        <div v-else class="flex flex-col items-center gap-2 p-8 text-center" data-testid="search-no-results">
          <i class="pi pi-search text-2xl text-surface-300 dark:text-surface-600" />
          <p class="text-sm text-surface-600 dark:text-surface-300">No results for “{{ searchStore.query }}”</p>
          <p class="text-xs text-surface-500 dark:text-surface-400">Try different keywords.</p>
        </div>
      </div>
    </Dialog>
  </div>
</template>

<script setup lang="ts">
import HighlightText from '~/components/common/HighlightText.vue';
import SearchSectionHeader from '~/components/common/SearchSectionHeader.vue';
import SearchViewAll from '~/components/common/SearchViewAll.vue';
import { MIN_QUERY_LENGTH, useSearchStore } from '~/stores/search';
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

/** Rows shown per category in the palette; "View all" opens the full results page. */
const QUICK_LIMIT = 5;

type Category = 'events' | 'contacts' | 'calendars' | 'addressBooks';

/** Render/navigation order of the categories — Tab walks it, Up/Down flattens it. */
const CATEGORY_ORDER: Category[] = ['events', 'contacts', 'calendars', 'addressBooks'];

const searchStore = useSearchStore();

const visible = ref(false);
const query = ref('');
const inputRef = ref<HTMLInputElement | null>(null);
const resultsRef = ref<HTMLElement | null>(null);
/** Index into the FLAT row list (`navRows`) of the keyboard-highlighted row. */
const activeIndex = ref(0);

const isSearchable = computed(() => query.value.trim().length >= MIN_QUERY_LENGTH);

// True while the debounce is still pending (what's typed differs from what the
// store last searched). Without this the "no results" state flashes for 300ms on
// the first keystroke, quoting an empty query.
const isPending = computed(() => isSearchable.value && searchStore.query !== query.value.trim());

// 300ms debounce (story 044). The store's requestSeq guard is what actually makes
// this safe — debouncing only reduces the number of fan-outs, it cannot prevent an
// earlier slow response from landing after a later fast one.
const runSearch = useDebounceFn(() => {
  // Read the query AT FIRE TIME rather than closing over the value that scheduled
  // the timer. vueuse's useDebounceFn hands back no cancel handle and only clears
  // its timer when the wrapper is invoked again, so a timer armed for text the user
  // has since deleted (or a dialog they have since closed) still fires. This guard
  // is what actually makes clearing/closing drop the pending search — without it a
  // deleted query would claim the newest requestSeq and repopulate the results.
  const value = query.value.trim();
  if (!visible.value || value.length < MIN_QUERY_LENGTH) return;
  searchStore.search(value);
}, 300);

watch(query, () => {
  activeIndex.value = 0;
  if (isSearchable.value) {
    runSearch();
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

// Cmd/Ctrl+K from anywhere in the app (story 044). Registered on window because the
// palette must be reachable without the header button ever being focused.
const isApplePlatform = () =>
  typeof navigator !== 'undefined' && /Mac|iPhone|iPad|iPod/i.test(navigator.platform || navigator.userAgent || '');
const shortcutLabel = computed(() => (isApplePlatform() ? '⌘K' : 'Ctrl+K'));

const handleGlobalKeydown = (e: KeyboardEvent) => {
  if (!(e.metaKey || e.ctrlKey)) return;
  if ((e.key || '').toLowerCase() !== 'k') return;
  // Firefox binds Ctrl/Cmd+K to the search bar and Chrome to the omnibox.
  e.preventDefault();
  if (visible.value) {
    focusInput();
    return;
  }
  openSearch();
};

onMounted(() => window.addEventListener('keydown', handleGlobalKeydown));
onBeforeUnmount(() => window.removeEventListener('keydown', handleGlobalKeydown));

function quick<T>(items: T[]): T[] {
  return items.slice(0, QUICK_LIMIT);
}

function isTruncated<T>(items: T[]): boolean {
  return items.length > QUICK_LIMIT;
}

/**
 * How many matches "View all" leads to: "42", or "100+" when the server itself
 * capped that category (#156). Never claim a total the server didn't promise.
 */
function totalLabel(category: Category): string {
  const count = searchStore.results[category].length;
  return searchStore.results.hasMore[category] ? `${count}+` : String(count);
}

/** How many rows of `category` are actually rendered (the quick view is capped). */
function visibleCount(category: Category): number {
  return Math.min(searchStore.results[category].length, QUICK_LIMIT);
}

/** Position of a row in the flat list — the category's offset plus its own index. */
function navIndex(category: Category, indexWithinCategory: number): number {
  let offset = 0;
  for (const c of CATEGORY_ORDER) {
    if (c === category) break;
    offset += visibleCount(c);
  }
  return offset + indexWithinCategory;
}

function isActive(category: Category, indexWithinCategory: number): boolean {
  return activeIndex.value === navIndex(category, indexWithinCategory);
}

interface NavRow {
  category: Category;
  activate: () => void;
}

/**
 * The rendered rows in render order, each with the action Enter should perform.
 * Kept in step with the template by construction: both walk CATEGORY_ORDER and
 * both cap at QUICK_LIMIT.
 */
const navRows = computed<NavRow[]>(() => {
  if (!isSearchable.value || !searchStore.hasResults) return [];
  const rows: NavRow[] = [];
  for (const hit of quick(searchStore.results.events)) {
    rows.push({ category: 'events', activate: () => selectEvent(hit) });
  }
  for (const hit of quick(searchStore.results.contacts)) {
    rows.push({ category: 'contacts', activate: () => selectContact(hit) });
  }
  // Collection rows all navigate to the same list page, so only their count matters.
  for (let i = 0; i < visibleCount('calendars'); i++) {
    rows.push({ category: 'calendars', activate: () => selectCalendar() });
  }
  for (let i = 0; i < visibleCount('addressBooks'); i++) {
    rows.push({ category: 'addressBooks', activate: () => selectAddressBook() });
  }
  return rows;
});

// A shrinking result set must not leave the highlight past the end (Enter would
// then do nothing).
watch(
  () => navRows.value.length,
  (len) => {
    if (activeIndex.value >= len) activeIndex.value = 0;
  }
);

function rowDomId(index: number): string {
  return `global-search-row-${index}`;
}

/** aria-activedescendant: undefined (absent) when nothing is highlighted. */
const activeRowId = computed(() =>
  navRows.value.length > 0 ? rowDomId(activeIndex.value) : undefined
);

const scrollActiveIntoView = () => {
  nextTick(() => {
    const el = resultsRef.value?.querySelector(`[id="${rowDomId(activeIndex.value)}"]`);
    // happy-dom (and older browsers) may not implement scrollIntoView.
    if (el && typeof (el as HTMLElement).scrollIntoView === 'function') {
      (el as HTMLElement).scrollIntoView({ block: 'nearest' });
    }
  });
};

/** Up/Down over the flat list, wrapping at both ends like a command palette. */
const moveActive = (delta: number) => {
  const len = navRows.value.length;
  if (len === 0) return;
  activeIndex.value = (activeIndex.value + delta + len) % len;
  scrollActiveIntoView();
};

/** Tab / Shift+Tab jump to the FIRST row of the next / previous populated category. */
const cycleCategory = (delta: number) => {
  const rows = navRows.value;
  if (rows.length === 0) return;
  const present = CATEGORY_ORDER.filter((c) => rows.some((r) => r.category === c));
  if (present.length < 2) return;
  const current = rows[activeIndex.value]?.category ?? present[0]!;
  const at = present.indexOf(current);
  const next = present[(at + delta + present.length) % present.length]!;
  activeIndex.value = rows.findIndex((r) => r.category === next);
  scrollActiveIntoView();
};

const activateActive = () => {
  navRows.value[activeIndex.value]?.activate();
};

const finishSelection = () => {
  searchStore.rememberSearch(query.value);
  visible.value = false;
};

const selectEvent = (hit: EventHit) => {
  const date = toLocalDateParam(new Date(hit.event.start));
  finishSelection();
  // The calendar page reads these params to jump to the date and open the event's
  // detail dialog (story 044). `cal` is the numeric calendar id the store maps to
  // a UUID for the API call (#52). `recurrence` is what lets the page resolve the
  // clicked OCCURRENCE instead of the series master.
  navigateTo({
    path: '/calendar',
    query: {
      date,
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

/** "View all" hands the query to the full results page, which drops the cap. */
const viewAll = () => {
  const q = query.value.trim();
  finishSelection();
  navigateTo({ path: '/search', query: { q } });
};
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

.global-search-dialog .search-kbd {
  border: 1px solid var(--p-surface-300, #d1d5db);
  border-radius: 4px;
  padding: 0.0625rem 0.25rem;
  margin-right: 0.125rem;
  font-size: 0.625rem;
}
</style>
