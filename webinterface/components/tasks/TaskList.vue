<template>
  <div>
    <div :aria-busy="tasksStore.isLoading ? 'true' : undefined" data-test="task-list-view">
      <!-- A failed load: a banner on top, and whatever was loaded before stays below it. -->
      <div
        v-if="errorMessage"
        role="alert"
        class="flex flex-wrap items-center gap-x-3 gap-y-2 border-b border-amber-200 bg-amber-50 px-4 py-2 text-sm text-amber-900 dark:border-amber-900/60 dark:bg-amber-950/40 dark:text-amber-100"
        data-test="load-error"
      >
        <i class="pi pi-exclamation-triangle text-amber-700 dark:text-amber-300" aria-hidden="true" />
        <span class="min-w-0 flex-1" data-test="load-error-text">{{ errorMessage }}</span>
        <Button
          label="Try again"
          icon="pi pi-refresh"
          severity="secondary"
          size="small"
          data-test="retry"
          @click="emit('retry')"
        />
      </div>

      <div v-if="showSkeleton" class="p-4" role="status" data-test="list-skeleton">
        <span class="sr-only">Loading tasks</span>
        <CommonSkeletonList :count="6" />
      </div>

      <TaskEmptyState
        v-else-if="emptyVariant"
        :variant="emptyVariant"
        @create-list="emit('add-list')"
        @clear-filters="tasksStore.clearFilters()"
        @show-completed="showCompleted"
      />

      <template v-else-if="displayGroups.length > 0">
        <!-- Keyboard (spec §8): ArrowUp/ArrowDown move between rows (roving
             tabindex: one row, and only its check and menu, is in the Tab
             order), Space ticks, Enter opens the task, Delete asks to delete. -->
        <div
          ref="groupsEl"
          data-test="task-groups"
          @keydown="onKeydown"
          @focusin="onFocusin"
        >
          <section v-for="group in displayGroups" :key="group.key" :data-group="group.key" data-test="task-group">
            <TaskGroupHeader
              :id="headingId(group.key)"
              :label="group.label"
              :count="group.tasks.length"
              :collapsible="group.collapsible"
              :expanded="group.expanded"
              :controls="listId(group.key)"
              :muted="group.key === 'cancelled'"
              @toggle="toggleGroup(group.key)"
            />
            <ul
              :id="listId(group.key)"
              role="list"
              :aria-labelledby="headingId(group.key)"
              :hidden="!group.expanded || undefined"
              class="border-t border-surface-200 dark:border-surface-800"
            >
              <template v-if="group.expanded">
                <TaskRow
                  v-for="task in group.tasks"
                  :key="task.id"
                  :task="task"
                  :now="now"
                  :show-list="tasksStore.selectedListId === null"
                  :read-only="!isWritable(task)"
                  :pending="tasksStore.isSaving.has(task.id)"
                  :tabbable="task.id === rovingId"
                  :tabindex="task.id === rovingId ? 0 : -1"
                  class="focus:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-primary-500"
                  @toggle="toggle"
                  @delete="remove"
                  @open="openTask"
                />
              </template>
            </ul>
          </section>
        </div>

        <div
          v-if="truncated || completedLink"
          class="flex flex-col items-start gap-1 border-t border-surface-200 px-4 py-3 text-[13px] leading-5 dark:border-surface-800"
          data-test="list-footer"
        >
          <!-- Groups count the loaded rows only (R6), so say when that is not everything. -->
          <p v-if="truncated" class="text-surface-500 dark:text-surface-400" data-test="truncated">
            Showing {{ shownCount }} of {{ matchingTotal }}
          </p>
          <button
            v-if="completedLink"
            type="button"
            class="-mx-1 rounded px-1 font-medium text-primary-700 underline-offset-2 hover:underline focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 dark:text-primary-300"
            data-test="show-completed"
            @click="showCompleted"
          >
            {{ completedLink }}
          </button>
        </div>
      </template>
    </div>

    <!-- Status changes no toast announces (spec §8). "Task completed" and
         "Task deleted" are toasts, which PrimeVue renders as role="alert", so
         they are not repeated here. Outside the aria-busy view on purpose: a
         busy ancestor may hold the announcement back. -->
    <p class="sr-only" role="status" aria-live="polite" aria-atomic="true" data-test="task-announcer">{{ announcement }}</p>
  </div>
</template>

<script setup lang="ts">
import TaskEmptyState from '~/components/tasks/TaskEmptyState.vue';
import TaskGroupHeader from '~/components/tasks/TaskGroupHeader.vue';
import TaskRow from '~/components/tasks/TaskRow.vue';
import { TASK_GROUP_LABELS, useTasksStore } from '~/stores/tasks';
import type { Calendar } from '~/types/calendar';
import type { Task, TaskEmptyVariant, TaskGroupKey, TaskStatusFilter } from '~/types/tasks';
import { TASK_GROUP_ORDER } from '~/types/tasks';
import { compareTasks } from '~/utils/tasks';

/**
 * The grouped task list (#170, spec §7.3, §7.4, §7.5, §8) with its loading,
 * empty and error states. It reads the tasks store and wires the rows to it:
 * a tick is `toggleComplete` (with the "Task completed" toast and its Undo;
 * an untick is said by the polite live region), a delete is `deleteTask` (it
 * asks first, then "Task deleted").
 *
 * Groups by status in the order Needs action, In progress, Completed,
 * Cancelled; empty ones are left out and the sort applies within each.
 * Completed and Cancelled are collapsible and start collapsed, except that
 * under the Done filter (where they are the whole view) Completed starts open.
 *
 * A ticked row stays where it is for 600 ms (the check draws and the title
 * strikes through on the SAME element) and only then leaves the Open view or
 * moves into Completed.
 */
const props = defineProps<{
  /** The page is loading the task lists (the calendars): show the skeleton, never "No task lists yet". */
  listsLoading?: boolean;
  /** Loading the task lists failed and none are known: an error, not "No task lists yet". */
  listsFailed?: boolean;
}>();

const emit = defineEmits<{
  /** "Create task list" in the no-lists state. */
  'add-list': [];
  /** "Try again" in the error banner. */
  retry: [];
  /** Enter on a row, a click on its title or "Edit task": open the task in the editor. */
  open: [task: Task];
  /** The empty state shown (null = none); the page focuses the quick-add for `empty`. */
  'empty-state': [variant: TaskEmptyVariant | null];
}>();

/** How long a ticked row stays in place before it leaves or moves (spec §7.4). */
const LINGER_MS = 600;
const LISTS_ERROR = "Couldn't load your task lists. Check your connection and try again.";
/** What the polite live region says after an untick (spec §8 copy). */
const UNCOMPLETED_MESSAGE = 'Task marked as not done';
/**
 * The live region is emptied first and the words put in after this delay, so
 * the same words twice in a row are announced twice (a region whose text does
 * not change says nothing).
 */
const ANNOUNCE_DELAY_MS = 100;
/** Then emptied again, so no stale sentence waits at the end of the list for a screen reader's reading cursor. */
const ANNOUNCE_CLEAR_MS = 5000;

const tasksStore = useTasksStore();
const toast = useAppToast();
const baseId = useId();

const headingId = (key: TaskGroupKey) => `${baseId}-${key}-heading`;
const listId = (key: TaskGroupKey) => `${baseId}-${key}`;

const writableIds = computed(() => new Set(tasksStore.writableTaskLists.map((l: Calendar) => l.uuid)));
const isWritable = (task: Task) => writableIds.value.has(task.calendar_uuid);

// --- Clock -------------------------------------------------------------------
// The rows' due labels and overdue markers follow `now`. It ticks on each
// minute boundary, so "17:00" turns overdue at 17:00 and all-day dates roll
// over at local midnight (a minute boundary in every zone).
const now = ref(new Date());
let clockTimer: ReturnType<typeof setTimeout> | undefined;

function armClock() {
  clockTimer = setTimeout(() => {
    now.value = new Date();
    armClock();
  }, CLOCK_INTERVAL_MS - (Date.now() % CLOCK_INTERVAL_MS));
}

// --- Rows held in place after a tick -----------------------------------------
/** Task id -> the group a just-ticked row stays in until its linger ends. */
const held = reactive(new Map<string, TaskGroupKey>());
const holdTimers = new Map<string, ReturnType<typeof setTimeout>>();

function hold(task: Task) {
  const key = held.get(task.id) ?? task.status;
  held.set(task.id, key);
  clearTimeout(holdTimers.get(task.id));
  holdTimers.set(
    task.id,
    setTimeout(() => {
      holdTimers.delete(task.id);
      if (!movesIntoCollapsedGroup(task.id)) held.delete(task.id);
    }, LINGER_MS),
  );
}

/**
 * Under All a ticked row would leave for the collapsed Completed (or
 * Cancelled) group, where the Undo of the toast has nothing to point at: it
 * stays where it was ticked, struck through, until the list is loaded again,
 * the view changes or its target group is opened.
 */
function movesIntoCollapsedGroup(taskId: string): boolean {
  if (tasksStore.statusFilter !== 'all') return false;
  const status = tasksStore.tasks.find((t: Task) => t.id === taskId)?.status;
  return status !== undefined && isCollapsible(status) && !expanded.value.has(status);
}

function releaseAll() {
  holdTimers.forEach((timer) => clearTimeout(timer));
  holdTimers.clear();
  held.clear();
}

// Another list, filter or search: the held rows belong to the old view. So do
// they when the list is loaded again (a re-render of the whole list).
watch(
  () => [tasksStore.selectedListId, tasksStore.statusFilter, tasksStore.searchQuery],
  releaseAll,
);
watch(
  () => tasksStore.isLoading,
  (loading: boolean) => {
    if (loading) releaseAll();
  },
);

// --- Groups ------------------------------------------------------------------
/** The defaults: open groups shown; Completed and Cancelled collapsed, but Completed open under Done. */
function defaultExpanded(filter: TaskStatusFilter): Set<TaskGroupKey> {
  const keys = new Set<TaskGroupKey>(['needs-action', 'in-process']);
  if (filter === 'completed') keys.add('completed');
  return keys;
}

const expanded = ref(defaultExpanded(tasksStore.statusFilter));
watch(
  () => tasksStore.statusFilter,
  (filter: TaskStatusFilter) => {
    expanded.value = defaultExpanded(filter);
  },
);

const isCollapsible = (key: TaskGroupKey) => key === 'completed' || key === 'cancelled';

function toggleGroup(key: TaskGroupKey) {
  const next = new Set(expanded.value);
  if (next.has(key)) next.delete(key);
  else next.add(key);
  expanded.value = next;
  // Opening a group lets the rows held back for it move in.
  if (next.has(key)) releaseSettled();
}

/** Drops the held rows whose linger is over (no timer is running for them). */
function releaseSettled() {
  for (const id of [...held.keys()]) {
    if (!holdTimers.has(id)) held.delete(id);
  }
}

/**
 * The view's rows plus the ticked rows still lingering after they left it
 * (under Open a completed row is no longer in `visibleTasks`, but the store
 * keeps it in `tasks`).
 */
const rows = computed<Task[]>(() => {
  const visible = tasksStore.visibleTasks;
  if (held.size === 0) return visible;
  const shown = new Set(visible.map((t: Task) => t.id));
  return [...visible, ...tasksStore.tasks.filter((t: Task) => held.has(t.id) && !shown.has(t.id))];
});

/**
 * `tasksStore.groups`, with each held row in the group it was ticked in.
 * The sort ignores the status, so a held row keeps its place in that group.
 */
const displayGroups = computed(() => {
  const order = compareTasks(tasksStore.sortBy, tasksStore.sortDir);
  return TASK_GROUP_ORDER.map((key: TaskGroupKey) => ({
    key,
    label: TASK_GROUP_LABELS[key],
    tasks: rows.value.filter((t: Task) => (held.get(t.id) ?? t.status) === key).sort(order),
    collapsible: isCollapsible(key),
    expanded: !isCollapsible(key) || expanded.value.has(key),
  })).filter((group) => group.tasks.length > 0);
});

// --- States ------------------------------------------------------------------
const searchActive = computed(() => tasksStore.searchQuery.trim() !== '');

/** Every calendar is known and none holds tasks (spec §1a: e.g. an OAuth account). */
const noLists = computed(() => !props.listsLoading && !props.listsFailed && tasksStore.taskLists.length === 0);

const errorMessage = computed(() => {
  if (noLists.value) return null;
  if (props.listsFailed) return LISTS_ERROR;
  return tasksStore.error;
});

const showSkeleton = computed(
  () => !noLists.value && (props.listsLoading || tasksStore.isLoading) && rows.value.length === 0,
);

/**
 * The done tasks of the selected list, or of every list under All tasks, from
 * the summary (it ignores the search and the status filter). 0 while the
 * summary is not loaded.
 */
const completedCount = computed(() => {
  const summary = tasksStore.summary;
  if (!summary) return 0;
  const listId = tasksStore.selectedListId;
  return listId === null ? summary.completed : (summary.by_calendar[listId]?.completed ?? 0);
});

const emptyVariant = computed<TaskEmptyVariant | null>(() => {
  if (noLists.value) return 'no-lists';
  // A failed load is never shown as "nothing here": that would be a false
  // statement about the user's tasks.
  if (errorMessage.value || showSkeleton.value || rows.value.length > 0) return null;
  if (searchActive.value || tasksStore.statusFilter === 'completed') return 'no-match';
  if (tasksStore.statusFilter === 'open' && completedCount.value > 0) return 'all-done';
  return 'empty';
});

/**
 * The loaded rows that belong to the view, and how many tasks match it: a row
 * ticked away under Open stays in `tasks` for the Undo, but is neither shown
 * nor counted any more, and the server's `total` still counts it.
 */
const shownCount = computed(() => tasksStore.visibleTasks.length);
const matchingTotal = computed(() => tasksStore.total - (tasksStore.tasks.length - shownCount.value));

/** More tasks match than were loaded (R6): the group counts are of the loaded rows. */
const truncated = computed(() => matchingTotal.value > shownCount.value);

/**
 * "Show N completed" under Open (N = the done tasks of the selected list, or of
 * every list under All tasks), only when there are some. The summary ignores
 * the search, so a search shows the link without a number.
 */
const completedLink = computed(() => {
  if (tasksStore.statusFilter !== 'open') return null;
  const completed = completedCount.value;
  if (completed <= 0) return null;
  return !searchActive.value ? `Show ${completed} completed` : 'Show completed';
});

function showCompleted() {
  void tasksStore.setStatusFilter('completed');
}

// --- Actions -----------------------------------------------------------------
function canToggle(task: Task): boolean {
  // An open repeating task cannot be completed over REST; a completed one can be reopened.
  const repeatingOpen = task.is_recurring && !task.completed;
  return isWritable(task) && !repeatingOpen && !tasksStore.isSaving.has(task.id);
}

// --- Announcements (spec §8) -------------------------------------------------
// "Task completed" and "Task deleted" are toasts, and PrimeVue renders every
// toast as role="alert" (assertive), so a screen reader already hears them;
// saying them again here would read them twice. The untick has no toast, so
// the polite region says it.
const announcement = ref('');
let announceTimer: ReturnType<typeof setTimeout> | undefined;

function announce(message: string) {
  clearTimeout(announceTimer);
  announcement.value = '';
  announceTimer = setTimeout(() => {
    announcement.value = message;
    announceTimer = setTimeout(() => {
      announcement.value = '';
    }, ANNOUNCE_CLEAR_MS);
  }, ANNOUNCE_DELAY_MS);
}

/**
 * Ticks or unticks. A completed task gets the "Task completed" toast with
 * Undo for 5 s; an unticked one is announced "Task marked as not done".
 */
async function toggle(task: Task) {
  if (!canToggle(task)) return;
  hold(task);
  const completing = !task.completed;
  const result = await tasksStore.toggleComplete(task);
  if (!result) return;
  if (completing && result.completed) {
    toast.action('Task completed', 'Undo', () => undo(result, task));
  } else if (!completing && !result.completed) {
    announce(UNCOMPLETED_MESSAGE);
  }
}

/**
 * Undo puts the task back as it was before the tick (`before`: its status and
 * percent, e.g. "in progress, 40 %" or "cancelled"), announced like an untick,
 * unless the task was changed some other way meanwhile.
 */
function undo(completed: Task, before: Task) {
  const current = tasksStore.tasks.find((t: Task) => t.id === completed.id) ?? completed;
  if (!current.completed) return;
  void tasksStore.undoComplete(before).then((result: Task | null) => {
    if (result && !result.completed) announce(UNCOMPLETED_MESSAGE);
  });
}

/** Delete asks first (the store's confirmation); the delete is permanent, so the toast offers no Undo. */
async function remove(task: Task) {
  if (await tasksStore.deleteTask(task)) toast.success('Task deleted');
}

/**
 * Opens a task in the editor. The row takes the focus first, so the dialog
 * (which returns the focus to whatever had it when it opened) comes back to
 * this row, also when it was opened with the mouse or from the row's menu.
 */
function openTask(task: Task) {
  const li = rowElement(task.id);
  if (li) {
    activeId.value = task.id;
    li.focus();
  }
  emit('open', task);
}

// --- Keyboard and focus ------------------------------------------------------
const groupsEl = ref<HTMLElement | null>(null);
/** The row that holds the list's one Tab stop (the last one focused or arrowed to). */
const activeId = ref<string | null>(null);
/**
 * The focus was in the list just before the rows last changed. Read from
 * `document.activeElement` before Vue patches the DOM (a pre-flush watcher
 * below), not tracked with focusout: browsers differ on whether, and when,
 * removing the focused element fires blur/focusout.
 */
let focusWasInside = false;

/** Rows a user can reach, in screen order (collapsed groups left out). */
const flatRows = computed<Task[]>(() =>
  displayGroups.value.filter((g) => g.expanded).flatMap((g) => g.tasks),
);
const flatIds = computed(() => flatRows.value.map((t: Task) => t.id));

const rovingId = computed(() => {
  const ids = flatIds.value;
  if (activeId.value !== null && ids.includes(activeId.value)) return activeId.value;
  return ids[0] ?? null;
});

function rowElement(id: string): HTMLElement | null {
  const items = groupsEl.value?.querySelectorAll<HTMLElement>('li[data-task-id]') ?? [];
  return Array.from(items).find((li) => li.dataset.taskId === id) ?? null;
}

function focusRow(id: string) {
  activeId.value = id;
  void nextTick(() => rowElement(id)?.focus());
}

function rowIdOf(target: EventTarget | null): string | null {
  const li = (target as HTMLElement | null)?.closest?.<HTMLElement>('li[data-task-id]');
  return li && groupsEl.value?.contains(li) ? li.dataset.taskId ?? null : null;
}

function onFocusin(event: FocusEvent) {
  const id = rowIdOf(event.target);
  if (id !== null) activeId.value = id;
}

function onKeydown(event: KeyboardEvent) {
  const id = rowIdOf(event.target);
  if (id === null) return;
  const task = flatRows.value.find((t: Task) => t.id === id);
  if (!task) return;

  if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
    event.preventDefault();
    const index = flatIds.value.indexOf(id);
    const next = flatIds.value[index + (event.key === 'ArrowDown' ? 1 : -1)];
    if (next !== undefined) focusRow(next);
    return;
  }

  // The rest acts on the row itself; its check and menu handle their own keys.
  const li = rowElement(id);
  if (event.target !== li || event.defaultPrevented) return;
  if (event.key === ' ') {
    event.preventDefault();
    if (!event.repeat) void toggle(task);
  } else if (event.key === 'Enter') {
    event.preventDefault();
    openTask(task);
  } else if (event.key === 'Delete' || event.key === 'Backspace') {
    event.preventDefault();
    if (isWritable(task)) void remove(task);
  }
}

// Before the rows change on screen: is the focus in the list (a row, its
// check or its menu button), and on which row? Focus in a dialog, the toast
// or the toolbar is not, and is never pulled back. The focused row holds the
// Tab stop from here on, also where no focusin was heard.
watch(
  flatIds,
  () => {
    const focused = document.activeElement;
    focusWasInside = focused !== null && focused !== document.body && groupsEl.value?.contains(focused) === true;
    const id = focusWasInside ? rowIdOf(focused) : null;
    if (id !== null) activeId.value = id;
  },
  { flush: 'pre' },
);

// The focused row was re-rendered or left: ticked under Open, deleted, moved
// into another group (a new element) or into a collapsed one. Focus would fall
// back to the page, so the row (or, when it is gone, its neighbour) takes it
// again and the keyboard user stays in place.
watch(
  flatIds,
  (ids: string[], previous: string[] | undefined) => {
    const active = activeId.value;
    if (active === null) return;
    let target: string | null = active;
    if (!ids.includes(active)) {
      const index = previous?.indexOf(active) ?? -1;
      target = ids[Math.min(Math.max(index, 0), ids.length - 1)] ?? null;
      activeId.value = target;
    }
    const focused = document.activeElement;
    const focusLost = !focused || focused === document.body || !focused.isConnected;
    if (target !== null && focusWasInside && focusLost) rowElement(target)?.focus();
  },
  { flush: 'post' },
);

watch(emptyVariant, (variant: TaskEmptyVariant | null) => emit('empty-state', variant), { immediate: true });

/**
 * Puts the focus on a task's row (the page, after the editor closed and the
 * row was re-rendered meanwhile); when the row is gone, on the row that holds
 * the list's Tab stop. Nothing when the list has no rows.
 */
function focusTask(taskId: string) {
  const target = flatIds.value.includes(taskId) ? taskId : rovingId.value;
  if (target === null) return;
  activeId.value = target;
  rowElement(target)?.focus();
}

defineExpose({ focusTask });

onMounted(armClock);

onBeforeUnmount(() => {
  clearTimeout(clockTimer);
  clearTimeout(announceTimer);
  releaseAll();
});
</script>
