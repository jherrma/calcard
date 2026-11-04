<template>
  <div class="flex flex-col gap-3 border-b border-surface-200 dark:border-surface-800 px-4 py-3">
    <!-- What is shown and how much of it is open. On phones the list Select
         stands in for the heading (the sidebar is hidden there), so the
         heading is kept for screen readers only. -->
    <div class="flex min-w-0 items-center gap-3">
      <h2
        class="sr-only lg:not-sr-only lg:min-w-0 lg:truncate text-lg font-semibold text-surface-900 dark:text-surface-100"
        data-test="view-title"
      >
        {{ viewTitle }}
      </h2>
      <Select
        :model-value="selectedListId ?? ALL_TASKS"
        :options="listOptions"
        option-label="label"
        option-value="value"
        aria-label="Task list"
        class="min-w-0 flex-1 sm:max-w-xs lg:hidden"
        size="small"
        data-test="list-select"
        @update:model-value="onSelectList"
      >
        <template #option="{ option }">
          <span class="flex min-w-0 items-center gap-2">
            <span class="flex w-4 shrink-0 justify-center" aria-hidden="true">
              <span v-if="option.color" class="h-2 w-2 rounded-full" :style="{ backgroundColor: option.color }" />
              <i v-else class="pi pi-list-check text-sm" />
            </span>
            <span class="truncate">{{ option.label }}</span>
            <i v-if="option.readOnly" class="pi pi-lock text-xs text-surface-500 dark:text-surface-400" aria-hidden="true" />
            <span v-if="option.readOnly" class="sr-only">Read only</span>
          </span>
        </template>
      </Select>
      <p
        v-if="openCount !== null"
        class="shrink-0 text-sm tabular-nums text-surface-500 dark:text-surface-400"
        data-test="open-count"
      >
        {{ openCount }} open
      </p>
    </div>

    <!-- Search takes the full width on phones, the filter and the sort wrap below it. -->
    <div class="flex flex-wrap items-center gap-2">
      <div class="relative w-full sm:w-auto sm:min-w-[12rem] sm:flex-1">
        <i
          class="pi pi-search pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-sm text-surface-500 dark:text-surface-400"
          aria-hidden="true"
        />
        <input
          v-model="searchText"
          type="search"
          placeholder="Search tasks"
          aria-label="Search tasks"
          autocomplete="off"
          class="h-9 w-full rounded-lg border border-surface-300 dark:border-surface-600 bg-surface-0 dark:bg-surface-800 pl-9 pr-3 text-sm text-surface-900 dark:text-surface-100 placeholder-surface-500 dark:placeholder-surface-400 focus:outline-none focus:ring-2 focus:ring-primary-500 focus:border-transparent"
          data-test="search"
          @input="debouncedSearch"
          @keydown.enter.prevent="sendSearch"
        >
      </div>

      <span :id="filterLabelId" class="sr-only">Show tasks</span>
      <SelectButton
        :model-value="statusFilter"
        :options="STATUS_OPTIONS"
        option-label="label"
        option-value="value"
        :allow-empty="false"
        :aria-labelledby="filterLabelId"
        size="small"
        data-test="status-filter"
        @update:model-value="onFilter"
      />

      <div class="flex items-center gap-1">
        <Select
          :model-value="sortBy"
          :options="SORT_OPTIONS"
          option-label="label"
          option-value="value"
          aria-label="Sort by"
          class="w-36"
          size="small"
          data-test="sort-by"
          @update:model-value="onSort"
        />
        <Button
          v-tooltip.bottom="orderDescription"
          :icon="sortDir === 'desc' ? 'pi pi-sort-amount-down' : 'pi pi-sort-amount-up-alt'"
          severity="secondary"
          text
          rounded
          size="small"
          aria-label="Reverse order"
          :aria-pressed="sortDir === 'desc' ? 'true' : 'false'"
          :aria-describedby="orderDescriptionId"
          data-test="sort-dir"
          @click="emit('toggle-sort-dir')"
        />
        <span :id="orderDescriptionId" class="sr-only">{{ orderDescription }}</span>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { Calendar } from '~/types/calendar';
import type { TaskSortBy, TaskSortDir, TaskStatusFilter, TasksSummary } from '~/types/tasks';

/**
 * The Tasks page header (#170, spec §7.3/§7.4): what is shown and its open
 * count, the search, the Open / Done / All filter, the in-group sort and its
 * direction, and on phones the list picker that replaces the sidebar.
 *
 * Presentational: it emits what the user chose and the page drives the store.
 * It never disables anything while the list loads (spec §7.5).
 */
const props = defineProps<{
  /** Every calendar that holds tasks (`tasksStore.taskLists`). */
  lists: Calendar[];
  /** UUID of the selected list; null = All tasks. */
  selectedListId: string | null;
  /** UUIDs of the lists the user may write to; the others are marked read only in the phone picker. */
  writableIds: string[];
  /** The counts; "N open" is the selected list's `by_calendar` entry, or `open` for All tasks. */
  summary: TasksSummary | null;
  statusFilter: TaskStatusFilter;
  /** The search text the store holds (`tasksStore.searchQuery`). */
  searchQuery: string;
  sortBy: TaskSortBy;
  sortDir: TaskSortDir;
}>();

const emit = defineEmits<{
  'select-list': [listId: string | null];
  'filter-change': [filter: TaskStatusFilter];
  /** The raw search text, debounced; the store trims it. */
  search: [query: string];
  'sort-change': [sortBy: TaskSortBy];
  'toggle-sort-dir': [];
}>();

/** The phone picker's value for "All tasks" (PrimeVue Select treats null as "nothing chosen"). */
const ALL_TASKS = '__all__';

const STATUS_OPTIONS: { label: string; value: TaskStatusFilter }[] = [
  { label: 'Open', value: 'open' },
  { label: 'Done', value: 'completed' },
  { label: 'All', value: 'all' },
];

const SORT_OPTIONS: { label: string; value: TaskSortBy }[] = [
  { label: 'Due date', value: 'due' },
  { label: 'Priority', value: 'priority' },
  { label: 'Title', value: 'title' },
];

/** What the current order means, in the words of the chosen sort. */
const ORDER_WORDS: Record<TaskSortBy, Record<TaskSortDir, string>> = {
  due: { asc: 'Earliest due first', desc: 'Latest due first' },
  priority: { asc: 'Highest priority first', desc: 'Lowest priority first' },
  title: { asc: 'A to Z', desc: 'Z to A' },
};

const SEARCH_DEBOUNCE_MS = 300;

const filterLabelId = useId();
const orderDescriptionId = useId();

const selectedList = computed(() =>
  props.selectedListId === null ? undefined : props.lists.find((l: Calendar) => l.uuid === props.selectedListId),
);

const viewTitle = computed(() => selectedList.value?.name ?? 'All tasks');

/** Open tasks in what is shown: the list's own count, or every list's. Null until the summary is loaded. */
const openCount = computed<number | null>(() => {
  const summary = props.summary;
  if (!summary) return null;
  if (props.selectedListId === null) return summary.open;
  return summary.by_calendar[props.selectedListId]?.open ?? null;
});

interface ListOption {
  label: string;
  value: string;
  color?: string;
  readOnly: boolean;
}

// Own lists first, then shared ones: the sidebar's order.
const listOptions = computed<ListOption[]>(() => {
  const writable = new Set(props.writableIds);
  const ordered = [
    ...props.lists.filter((l: Calendar) => !l.shared),
    ...props.lists.filter((l: Calendar) => l.shared),
  ];
  return [
    { label: 'All tasks', value: ALL_TASKS, readOnly: false },
    ...ordered.map((l: Calendar) => ({ label: l.name, value: l.uuid, color: l.color, readOnly: !writable.has(l.uuid) })),
  ];
});

const orderDescription = computed(() => ORDER_WORDS[props.sortBy][props.sortDir]);

function onSelectList(value: unknown) {
  if (typeof value !== 'string' || value === '') return;
  const listId = value === ALL_TASKS ? null : value;
  if (listId !== props.selectedListId) emit('select-list', listId);
}

// A SelectButton can emit null (deselect) despite allow-empty=false in some
// paths; only a real, different filter goes out (same guard as the calendar
// page's view switcher, #26).
function onFilter(value: unknown) {
  if (!STATUS_OPTIONS.some((o) => o.value === value)) return;
  if (value !== props.statusFilter) emit('filter-change', value as TaskStatusFilter);
}

function onSort(value: unknown) {
  if (!SORT_OPTIONS.some((o) => o.value === value)) return;
  if (value !== props.sortBy) emit('sort-change', value as TaskSortBy);
}

// --- Search ---------------------------------------------------------------
// The debounce cannot be cancelled (webinterface CLAUDE.md, Global Search), so
// the callback reads the LIVE text and sends it only when it differs from what
// was sent last. Enter sends at once; the pending debounce then finds nothing
// new. A change of `searchQuery` from outside (e.g. "Clear filters") replaces
// the text; the echo of our own `search` does not, so typing ahead of a slow
// request is never overwritten.
const searchText = ref(props.searchQuery);
let lastSent = props.searchQuery;

function sendSearch() {
  const text = searchText.value;
  if (text.trim() === lastSent.trim()) return;
  lastSent = text;
  emit('search', text);
}

const debouncedSearch = useDebounceFn(sendSearch, SEARCH_DEBOUNCE_MS);

watch(
  () => props.searchQuery,
  (query: string) => {
    if (query === lastSent) return;
    lastSent = query;
    searchText.value = query;
  },
);
</script>
