<template>
  <li
    class="relative grid min-h-12 grid-cols-[1.5rem_minmax(0,1fr)_auto_2rem] items-start gap-x-3 border-b border-surface-200 bg-surface-0 px-4 py-3 last:border-b-0 dark:border-surface-800 dark:bg-surface-900 sm:grid-cols-[1.5rem_minmax(0,1fr)_auto_1rem_minmax(5.5rem,auto)_2rem]"
    :class="{ 'opacity-60': showPending }"
    :aria-busy="pending ? 'true' : undefined"
    :data-task-id="task.id"
    data-test="task-row"
  >
    <!--
      One task in the flat ruled list (spec §7.5): hairline rule below, no card.
      Grid, sm and up:  [check 24] [title + notes, grows] [list, All only] [priority] [due, right] [menu]
      Below sm the list and the priority mark drop to a second line under the
      title: their wrapper is a flex line there and `display: contents` from sm,
      so each becomes a grid cell of the row. Fixed tracks keep the priority
      marks, due dates and menus aligned down the list. (Comments stay inside
      the root element: one at the root would make the component a fragment.)
    -->
    <!-- Overdue: a 3px bar on the start edge, in the alarm colour. Only overdue rows have it. -->
    <span
      v-if="overdue"
      class="absolute inset-y-0 start-0 w-[3px] bg-red-600 dark:bg-red-400"
      aria-hidden="true"
      data-test="overdue-bar"
    />

    <div class="col-start-1 row-start-1 flex h-[1.375rem] items-center">
      <TaskCheck
        :checked="task.completed"
        :task-title="title"
        :pending="showPending"
        :disabled-reason="disabledReason"
        :tabbable="tabbable"
        @toggle="onToggle"
      />
    </div>

    <div class="col-start-2 row-start-1 min-w-0">
      <div class="flex min-w-0 items-center gap-2">
        <!-- The title opens the task (story 26). Out of the Tab order: the
             keyboard opens it with Enter on the row or "Edit task" in the menu. -->
        <button
          type="button"
          tabindex="-1"
          class="min-w-0 cursor-pointer truncate text-left text-[15px] leading-[1.375rem] decoration-1 underline-offset-2 hover:underline focus:outline-none"
          :class="done ? 'font-normal text-surface-500 dark:text-surface-400' : 'font-medium text-surface-900 dark:text-surface-100'"
          data-test="task-title"
          @click="emit('open', task)"
        >
          <!-- The strike is a 1px line drawn as a background that grows left
               to right (200ms) — text-decoration cannot be animated that way.
               Instant under reduced motion; a real line-through in forced
               colours, where backgrounds are not painted reliably. -->
          <span
            class="bg-[linear-gradient(currentColor,currentColor)] bg-[position:0_58%] bg-no-repeat transition-[background-size] duration-200 ease-out motion-reduce:transition-none forced-colors:bg-none"
            :class="done ? 'bg-[length:100%_1px] forced-colors:line-through' : 'bg-[length:0%_1px]'"
            :data-struck="done ? 'true' : undefined"
            data-test="task-title-text"
          >{{ title }}</span>
        </button>
        <span
          v-if="task.status === 'cancelled'"
          class="shrink-0 rounded border border-surface-300 px-1.5 text-xs leading-5 text-surface-500 dark:border-surface-600 dark:text-surface-400"
          data-test="cancelled-tag"
        >Cancelled</span>
        <i
          v-if="task.is_recurring"
          v-tooltip.top="'Repeats'"
          class="pi pi-replay shrink-0 text-xs text-surface-500 dark:text-surface-400"
          role="img"
          aria-label="Repeats"
          data-test="repeat-icon"
        />
      </div>
      <p
        v-if="notes"
        class="truncate text-[13px] leading-5 text-surface-500 dark:text-surface-400"
        data-test="task-notes"
      >
        {{ notes }}
      </p>
    </div>

    <!-- List (All tasks only) and priority: a second line below sm, own cells
         from sm, where the list cell has a fixed width so the dots line up. -->
    <div
      v-if="showList || priorityMark"
      class="col-start-2 row-start-2 mt-1 flex min-w-0 items-center gap-3 sm:contents"
      data-test="task-meta"
    >
      <span
        v-if="showList"
        class="flex min-w-0 items-center gap-1.5 text-[13px] leading-[1.375rem] text-surface-500 dark:text-surface-400 sm:col-start-3 sm:row-start-1 sm:w-36"
        data-test="task-list"
      >
        <span
          class="h-2 w-2 shrink-0 rounded-full"
          :class="{ 'bg-surface-400 dark:bg-surface-500': !task.calendar_color }"
          :style="task.calendar_color ? { backgroundColor: task.calendar_color } : undefined"
          aria-hidden="true"
          data-test="list-dot"
        />
        <span class="truncate"><span class="sr-only">{{ 'List: ' }}</span>{{ listName }}</span>
      </span>
      <span
        v-if="priorityMark"
        class="flex h-[1.375rem] w-4 shrink-0 items-center justify-center sm:col-start-4 sm:row-start-1"
      >
        <i
          v-tooltip.top="priorityMark.label"
          :class="[priorityMark.icon, priorityMark.tone, 'text-sm']"
          role="img"
          :aria-label="priorityMark.label"
          :data-priority="priorityLevelValue"
          data-test="priority-mark"
        />
      </span>
    </div>

    <!-- Due date: right-aligned, never wrapped, tabular figures so dates line up. -->
    <div class="col-start-3 row-start-1 flex justify-end sm:col-start-5">
      <span
        v-if="dueLabel"
        class="inline-flex items-center gap-1 whitespace-nowrap text-[13px] leading-[1.375rem] tabular-nums"
        :class="dueTone"
        data-test="due-label"
      >
        <i v-if="overdue" class="pi pi-exclamation-circle text-xs" aria-hidden="true" data-test="overdue-icon" />
        <span class="sr-only">{{ overdue ? 'Overdue, due ' : 'Due ' }}</span>
        <time :datetime="dueDateTime">{{ dueLabel }}</time>
      </span>
    </div>

    <!-- Overflow menu. None on a read-only list or subscription: every write would be refused. -->
    <div class="col-start-4 row-start-1 flex justify-end sm:col-start-6">
      <template v-if="!readOnly">
        <button
          type="button"
          class="-my-[5px] flex h-8 w-8 items-center justify-center rounded-full text-surface-500 hover:bg-surface-100 hover:text-surface-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 dark:text-surface-400 dark:hover:bg-surface-800 dark:hover:text-surface-200"
          :aria-label="`Task actions: ${title}`"
          aria-haspopup="true"
          :aria-controls="menuId"
          :tabindex="tabbable ? undefined : -1"
          data-test="row-menu"
          @click="openMenu"
        >
          <i class="pi pi-ellipsis-v text-sm" aria-hidden="true" />
        </button>
        <Menu :id="menuId" ref="menu" :model="menuItems" popup data-test="row-menu-items" />
      </template>
    </div>
  </li>
</template>

<script setup lang="ts">
import type { MenuItem } from 'primevue/menuitem';
import TaskCheck from '~/components/tasks/TaskCheck.vue';
import { usePreferencesStore } from '~/stores/preferences';
import type { PriorityLevel, Task } from '~/types/tasks';
import { dueBucket, formatDue, isTaskDone, priorityLevel } from '~/utils/tasks';

/**
 * One task (#170, spec §7.3, §7.5, §8): completion control, title, notes
 * snippet, list (All tasks only), priority mark, due label, repeat icon and
 * the overflow menu.
 *
 * Presentational like the toolbar: it emits `toggle`, `delete` and `open`
 * (title click, "Edit task") with the task, and the list wires them to the
 * store (`toggleComplete`, `deleteTask`) and to the editor (story 26), where
 * the Undo toast and the announcements belong (stories 25 and 29). The
 * user's 12h/24h preference is read from the preferences store.
 *
 * `now` is the list's clock: the due label and the overdue marker follow it,
 * so they roll over at midnight when the list ticks it.
 */
const props = withDefaults(defineProps<{
  task: Task;
  /** The moment the due label and overdue state are computed for. */
  now: Date;
  /** Show the task's list (dot + name): only in "All tasks". */
  showList?: boolean;
  /** The task's list is read-only to the user (read-only share, subscription): no tick, no menu. */
  readOnly?: boolean;
  /** A write for this task is in flight (`tasksStore.isSaving.has(task.id)`). */
  pending?: boolean;
  /**
   * The check and the menu button are in the Tab order (default). The list
   * passes false for every row but the active one (roving tabindex), so Tab
   * enters and leaves the list in one step and the arrow keys move between rows.
   */
  tabbable?: boolean;
}>(), {
  tabbable: true,
});

const emit = defineEmits<{
  toggle: [task: Task];
  delete: [task: Task];
  /** Open the task in the editor (title click, "Edit task"); read-only rows open it read only. */
  open: [task: Task];
}>();

const READ_ONLY_REASON = 'This list is read only';
const RECURRING_REASON = 'Repeating tasks can be completed in your task app';

/**
 * How long a write may run before the row shows it (60% opacity, spinner).
 * A tick flips the row at once (optimistic) AND starts the write: showing the
 * spinner immediately would replace the check before its 180ms draw and the
 * 200ms strike could play, and flash on every fast answer.
 */
const PENDING_DELAY_MS = 300;

const QUIET = 'text-surface-500 dark:text-surface-400';

const PRIORITY_MARKS: Record<Exclude<PriorityLevel, 'none'>, { icon: string; label: string; tone: string }> = {
  high: { icon: 'pi pi-angle-double-up', label: 'High priority', tone: 'text-primary-700 dark:text-primary-300' },
  medium: { icon: 'pi pi-angle-up', label: 'Medium priority', tone: QUIET },
  low: { icon: 'pi pi-angle-down', label: 'Low priority', tone: QUIET },
};

const preferencesStore = usePreferencesStore();
const menu = ref<{ toggle: (event: Event) => void } | null>(null);
const menuId = useId();

const title = computed(() => props.task.summary.trim() || '(No title)');

/** Completed or cancelled: struck through and muted. */
const done = computed(() => isTaskDone(props.task));

/** The description on one line (the row truncates it). */
const notes = computed(() => props.task.description.replace(/\s+/g, ' ').trim());

const listName = computed(() => props.task.calendar_name ?? '');

const priorityLevelValue = computed(() => priorityLevel(props.task.priority));
const priorityMark = computed(() =>
  priorityLevelValue.value === 'none' ? null : PRIORITY_MARKS[priorityLevelValue.value],
);

const bucket = computed(() => dueBucket(props.task, props.now));
const overdue = computed(() => bucket.value === 'overdue');
const dueLabel = computed(() => formatDue(props.task, props.now, undefined, preferencesStore.timeFormat));
const dueDateTime = computed(() => props.task.due_date ?? props.task.due ?? undefined);

// red-600 is 4.83:1 on surface-0 and red-400 6.4:1 on dark surface-900, so the
// red-700 fallback is not needed. Due today is in ink, later dates are quiet.
const dueTone = computed(() => {
  if (overdue.value) return 'text-red-600 dark:text-red-400';
  if (bucket.value === 'today' && !done.value) return 'text-surface-900 dark:text-surface-100';
  return QUIET;
});

const disabledReason = computed(() => {
  if (props.readOnly) return READ_ONLY_REASON;
  if (props.task.is_recurring) return RECURRING_REASON;
  return null;
});

// --- Pending ---------------------------------------------------------------
const showPending = ref(false);
let pendingTimer: ReturnType<typeof setTimeout> | undefined;

watch(
  () => props.pending,
  (pending: boolean | undefined) => {
    clearTimeout(pendingTimer);
    if (!pending) {
      showPending.value = false;
      return;
    }
    pendingTimer = setTimeout(() => {
      showPending.value = true;
    }, PENDING_DELAY_MS);
  },
  { immediate: true },
);

onBeforeUnmount(() => clearTimeout(pendingTimer));

// --- Actions ---------------------------------------------------------------
/** The check and the menu item: never while a write is in flight or when the tick is disabled. */
function onToggle() {
  if (props.pending || disabledReason.value) return;
  emit('toggle', props.task);
}

// A repeating task cannot be ticked here, so its menu offers no "Complete"
// either; editing and deleting it are allowed.
const menuItems = computed<MenuItem[]>(() => [
  ...(props.task.is_recurring
    ? []
    : [
        {
          label: props.task.completed ? 'Mark as not done' : 'Complete',
          icon: props.task.completed ? 'pi pi-undo' : 'pi pi-check',
          command: onToggle,
        },
      ]),
  { label: 'Edit task', icon: 'pi pi-pencil', command: () => emit('open', props.task) },
  { label: 'Delete task', icon: 'pi pi-trash', command: () => emit('delete', props.task) },
]);

function openMenu(event: Event) {
  menu.value?.toggle(event);
}
</script>
