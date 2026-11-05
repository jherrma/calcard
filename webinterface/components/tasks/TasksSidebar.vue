<template>
  <aside
    class="w-64 bg-surface-0 dark:bg-surface-900 border-r border-surface-200 dark:border-surface-800 flex-col hidden lg:flex"
    aria-label="Task lists"
  >
    <!-- tabindex -1: an overflowing scroller is a Tab stop of its own in
         Firefox; the rows inside are the keyboard's way through it. -->
    <div class="flex-1 overflow-y-auto p-4" tabindex="-1" data-test="lists-scroller">
      <!-- "All tasks": the union of every list. Single-select with the rows
           below, so the current one carries aria-current, not a checkbox. -->
      <button
        type="button"
        :class="rowClass(selectedId === null)"
        :aria-current="selectedId === null ? 'true' : undefined"
        data-list-id=""
        @click="emit('select', null)"
      >
        <span class="flex w-4 shrink-0 justify-center" aria-hidden="true">
          <i class="pi pi-list-check text-sm" />
        </span>
        <span class="min-w-0 flex-1 truncate">All tasks</span>
        <span v-if="allOpen" :class="countClass">{{ allOpen }}<span class="sr-only"> open</span></span>
      </button>

      <!-- My lists: owned task lists, mixed calendars and subscriptions. -->
      <div class="mt-6 mb-2 flex items-center justify-between">
        <h2 :id="ownHeadingId" class="text-sm font-semibold text-surface-700 dark:text-surface-300">My lists</h2>
        <button
          v-tooltip.top="'Create task list'"
          type="button"
          class="flex h-8 w-8 items-center justify-center rounded-full text-surface-500 dark:text-surface-400 hover:bg-surface-100 dark:hover:bg-surface-800 hover:text-surface-700 dark:hover:text-surface-200 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500"
          aria-label="Create task list"
          data-test="add-list"
          @click="emit('add-list')"
        >
          <i class="pi pi-plus text-sm" aria-hidden="true" />
        </button>
      </div>

      <!-- The lists come from GET /calendars: until it answers there is
           nothing to show, so placeholder lines hold the space. -->
      <div v-if="loading" aria-busy="true" data-test="lists-skeleton">
        <p class="sr-only">Loading task lists</p>
        <div
          v-for="i in 4"
          :key="i"
          class="flex items-center gap-3 px-3 py-2.5 motion-safe:animate-pulse"
          aria-hidden="true"
        >
          <span class="h-2 w-2 shrink-0 rounded-full bg-surface-200 dark:bg-surface-700" />
          <span class="h-3 rounded bg-surface-200 dark:bg-surface-700" :class="SKELETON_WIDTHS[(i - 1) % SKELETON_WIDTHS.length]" />
        </div>
      </div>

      <template v-else>
        <ul v-if="ownLists.length > 0" role="list" class="space-y-0.5" :aria-labelledby="ownHeadingId">
          <li v-for="list in ownLists" :key="list.uuid" class="group relative">
            <button
              type="button"
              :class="rowClass(selectedId === list.uuid)"
              :aria-current="selectedId === list.uuid ? 'true' : undefined"
              :data-list-id="list.uuid"
              @click="emit('select', list.uuid)"
            >
              <span class="flex w-4 shrink-0 justify-center" aria-hidden="true">
                <span class="h-2 w-2 rounded-full" :style="{ backgroundColor: list.color }" data-test="list-dot" />
              </span>
              <span class="min-w-0 flex-1 truncate">{{ list.name }}</span>
              <template v-if="isReadOnly(list)">
                <i
                  v-tooltip.top="'Read only'"
                  class="pi pi-lock text-xs text-surface-500 dark:text-surface-400"
                  aria-hidden="true"
                  data-test="read-only"
                />
                <span class="sr-only">Read only</span>
              </template>
              <span v-if="openCount(list)" :class="countClass">{{ openCount(list) }}<span class="sr-only"> open</span></span>
            </button>
            <!-- Owner-only actions (rename, colour, share, delete) of a task list.
                 Not on subscriptions (their settings page manages them) nor on
                 mixed calendars (the Calendar section manages those). A sibling
                 of the row button, since a button cannot hold another. -->
            <button
              v-if="canManage(list)"
              type="button"
              :class="menuButtonClass(list)"
              :aria-label="`List actions: ${list.name}`"
              aria-haspopup="true"
              :aria-controls="menuId"
              data-test="list-menu"
              @click="openMenu($event, list)"
            >
              <i class="pi pi-ellipsis-v text-sm" aria-hidden="true" />
            </button>
          </li>
        </ul>

        <template v-if="sharedLists.length > 0">
          <h2 :id="sharedHeadingId" class="mt-6 mb-2 text-sm font-semibold text-surface-700 dark:text-surface-300">
            Shared with me
          </h2>
          <ul role="list" class="space-y-0.5" :aria-labelledby="sharedHeadingId" data-test="shared-lists">
            <li v-for="list in sharedLists" :key="list.uuid">
              <button
                type="button"
                :class="rowClass(selectedId === list.uuid)"
                :aria-current="selectedId === list.uuid ? 'true' : undefined"
                :data-list-id="list.uuid"
                @click="emit('select', list.uuid)"
              >
                <span class="flex w-4 shrink-0 justify-center" aria-hidden="true">
                  <span class="h-2 w-2 rounded-full" :style="{ backgroundColor: list.color }" data-test="list-dot" />
                </span>
                <span class="min-w-0 flex-1">
                  <span class="block truncate">{{ list.name }}</span>
                  <span
                    v-if="list.owner?.display_name"
                    class="block truncate text-xs font-normal text-surface-500 dark:text-surface-400"
                  ><span class="sr-only">Shared by </span>{{ list.owner.display_name }}</span>
                </span>
                <template v-if="isReadOnly(list)">
                  <i
                    v-tooltip.top="'Read only'"
                    class="pi pi-lock text-xs text-surface-500 dark:text-surface-400"
                    aria-hidden="true"
                    data-test="read-only"
                  />
                  <span class="sr-only">Read only</span>
                </template>
                <span v-if="openCount(list)" :class="countClass">{{ openCount(list) }}<span class="sr-only"> open</span></span>
              </button>
            </li>
          </ul>
        </template>
      </template>
    </div>

    <Menu :id="menuId" ref="menu" :model="menuItems" popup data-test="list-menu-items" @hide="menuList = null" />
  </aside>
</template>

<script setup lang="ts">
import type { MenuItem } from 'primevue/menuitem';
import type { Calendar } from '~/types/calendar';
import type { TasksSummary } from '~/types/tasks';

/**
 * The Tasks section's list picker (#170, spec §7.3): "All tasks", the user's
 * own task lists, then the lists shared with them, each with its colour and
 * its open count; "+" asks for a new list. Single-select: it emits the chosen
 * calendar UUID (null = All tasks) and the page owns the selection. A task
 * list the user owns also has a menu (List settings, Share list, Delete task
 * list); the page opens TaskListSettingsDialog for them.
 */
const props = defineProps<{
  /** Every calendar that holds tasks (`tasksStore.taskLists`), in server order. */
  lists: Calendar[];
  /** UUID of the selected list; null = All tasks. */
  selectedId: string | null;
  /** UUIDs of the lists the user may write to (`tasksStore.writableTaskLists`); every other list shows the lock. */
  writableIds: string[];
  /** Counts behind "All tasks" (`open`) and each list (`by_calendar[uuid].open`); null until loaded. */
  summary: TasksSummary | null;
  /** True while the lists themselves are being fetched. */
  loading?: boolean;
}>();

const emit = defineEmits<{
  select: [listId: string | null];
  'add-list': [];
  /** List settings (rename, colour) of an owned task list. */
  'edit-list': [list: Calendar];
  /** Its sharing. */
  'share-list': [list: Calendar];
  /** Delete it (the page asks first). */
  'delete-list': [list: Calendar];
}>();

const ownHeadingId = useId();
const sharedHeadingId = useId();
const menuId = useId();

const SKELETON_WIDTHS = ['w-28', 'w-20', 'w-32', 'w-24'];

// A subscription is owned, so it sits with the user's own lists, behind the
// lock (story 100: the server refuses every write to it).
const ownLists = computed(() => props.lists.filter((l: Calendar) => !l.shared));
const sharedLists = computed(() => props.lists.filter((l: Calendar) => l.shared));

const writable = computed(() => new Set(props.writableIds));
const isReadOnly = (list: Calendar) => !writable.value.has(list.uuid);

// Zero is left blank: an empty count column reads faster than a column of 0s.
const allOpen = computed(() => props.summary?.open ?? 0);
const openCount = (list: Calendar) => props.summary?.by_calendar[list.uuid]?.open ?? 0;

const countClass = 'ml-1 shrink-0 text-xs font-normal tabular-nums text-surface-500 dark:text-surface-400';

/**
 * The list menu is the owner's: renaming, sharing and deleting are refused to
 * a sharee (404). Only real task lists get it here: a subscription is managed
 * on its settings page, a mixed (`both`) calendar in the Calendar section.
 */
const canManage = (list: Calendar) => !list.shared && !list.subscribed && list.kind === 'tasks';

const menu = ref<{ toggle: (event: Event) => void } | null>(null);
/** The list whose menu is open (its button stays visible meanwhile). */
const menuList = ref<Calendar | null>(null);

const menuItems = computed<MenuItem[]>(() => {
  const list = menuList.value;
  if (!list) return [];
  return [
    { label: 'List settings', icon: 'pi pi-cog', command: () => emit('edit-list', list) },
    { label: 'Share list', icon: 'pi pi-share-alt', command: () => emit('share-list', list) },
    { label: 'Delete task list', icon: 'pi pi-trash', command: () => emit('delete-list', list) },
  ];
});

function openMenu(event: Event, list: Calendar) {
  menuList.value = list;
  menu.value?.toggle(event);
}

// Every row keeps the menu's 2.5rem at its end, so the counts form one column.
// The menu button shows on hover, on keyboard focus, on the selected row,
// while its menu is open, and always where there is no hover (touch).
const menuButtonClass = (list: Calendar) => [
  'absolute right-1 top-1/2 -translate-y-1/2 flex h-8 w-8 items-center justify-center rounded-full',
  'text-surface-500 dark:text-surface-400 hover:bg-surface-200 dark:hover:bg-surface-700 hover:text-surface-700 dark:hover:text-surface-200',
  'focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500',
  props.selectedId === list.uuid || menuList.value?.uuid === list.uuid
    ? 'opacity-100'
    : 'opacity-0 group-hover:opacity-100 group-focus-within:opacity-100 [@media(hover:none)]:opacity-100',
];

// Selection is a tint plus weight (the main nav's active style). The 3px
// start-edge bar is NOT used here: in the list it means "overdue".
const rowClass = (selected: boolean) => [
  'flex w-full items-center gap-3 rounded-lg py-2 pl-3 pr-10 text-left text-sm transition-colors',
  'focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500',
  selected
    ? 'bg-primary-50 dark:bg-primary-900/20 text-primary-700 dark:text-primary-300 font-semibold'
    : 'text-surface-700 dark:text-surface-300 hover:bg-surface-100 dark:hover:bg-surface-800',
];
</script>
