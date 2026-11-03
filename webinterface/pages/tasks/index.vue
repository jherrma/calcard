<template>
  <div class="flex h-[calc(100vh-8rem)] overflow-hidden rounded-xl border border-surface-200 dark:border-surface-800 bg-surface-0 dark:bg-surface-900">
    <TasksSidebar
      :lists="tasksStore.taskLists"
      :selected-id="tasksStore.selectedListId"
      :writable-ids="writableIds"
      :summary="tasksStore.summary"
      :loading="listsLoading"
      @select="selectList"
      @add-list="openAddTaskList"
      @edit-list="openListSettings($event, 'general')"
      @share-list="openListSettings($event, 'sharing')"
      @delete-list="deleteList"
    />

    <div class="flex min-w-0 flex-1 flex-col">
      <TaskToolbar
        :lists="tasksStore.taskLists"
        :selected-list-id="tasksStore.selectedListId"
        :writable-ids="writableIds"
        :summary="tasksStore.summary"
        :status-filter="tasksStore.statusFilter"
        :search-query="tasksStore.searchQuery"
        :sort-by="tasksStore.sortBy"
        :sort-dir="tasksStore.sortDir"
        @select-list="selectList"
        @filter-change="tasksStore.setStatusFilter"
        @search="tasksStore.setSearchQuery"
        @sort-change="tasksStore.setSort"
        @toggle-sort-dir="tasksStore.toggleSortDir"
      />

      <!-- The quick-add stays above the scroll container, so it never scrolls
           away under the sticky group headings (Tab order: toolbar, quick-add, rows). -->
      <TaskQuickAdd ref="quickAdd" @created="onCreated" @open-details="openCreateDialog" />

      <!-- The list area (the scroll container the group headings stick to).
           tabindex -1: Firefox makes an overflowing scroller a Tab stop of its
           own (an unnamed one between the quick-add and the rows); the rows
           are the keyboard's way through the list and scroll it as they take
           the focus. -->
      <div class="min-h-0 flex-1 overflow-y-auto" tabindex="-1" data-test="task-list-area">
        <TaskList
          ref="taskList"
          :lists-loading="listsLoading"
          :lists-failed="listsFailed"
          @add-list="openAddTaskList"
          @retry="retry"
          @open="openTask"
          @empty-state="onEmptyState"
        />
      </div>
    </div>

    <TaskCreateDialog
      v-model:visible="showCreateDialog"
      :default-list-id="tasksStore.newTaskListId"
      :initial-summary="createSummary"
      @created="onDialogCreated"
    />
    <TaskEditDialog
      v-model:visible="showEditDialog"
      :task="openedTask"
      :read-only="openedReadOnly"
      @after-hide="onEditDialogHidden"
    />
    <AddTaskListDialog v-model:visible="showAddTaskListDialog" @created="onListCreated" />
    <TaskListSettingsDialog
      ref="listSettings"
      v-model:visible="showListSettings"
      :list="settingsList"
      :initial-tab="settingsTab"
      @updated="tasksStore.applyListInfo()"
      @deleted="onListDeleted"
    />
  </div>
</template>

<script setup lang="ts">
import AddTaskListDialog from '~/components/tasks/AddTaskListDialog.vue';
import TaskCreateDialog from '~/components/tasks/TaskCreateDialog.vue';
import TaskEditDialog from '~/components/tasks/TaskEditDialog.vue';
import TaskList from '~/components/tasks/TaskList.vue';
import TaskListSettingsDialog from '~/components/tasks/TaskListSettingsDialog.vue';
import TaskQuickAdd from '~/components/tasks/TaskQuickAdd.vue';
import TasksSidebar from '~/components/tasks/TasksSidebar.vue';
import TaskToolbar from '~/components/tasks/TaskToolbar.vue';
import { useCalendarStore } from '~/stores/calendars';
import { useTasksStore } from '~/stores/tasks';
import type { Calendar } from '~/types/calendar';
import type { Task, TaskEmptyVariant } from '~/types/tasks';

/**
 * The Tasks section (#170, spec §7.1). The selected list lives in the URL as
 * `?list=<calendar uuid>` (no `?list=` = All tasks), so a reload, a link from
 * search or the dashboard, and the back button all land on the same view.
 * The sidebar and the phone picker write the param; a watcher applies it.
 */
definePageMeta({
  middleware: 'auth',
  layout: 'default',
});

const calendarStore = useCalendarStore();
const tasksStore = useTasksStore();
const route = useRoute();
const toast = useAppToast();

/** True while this page loads the lists (the sidebar shows placeholder lines). */
const listsLoading = ref(false);
/**
 * This page's lists load failed and no calendar is known: the list shows an
 * error, not "No task lists yet" (a failed load is never shown as empty).
 */
const listsFailed = ref(false);
/** Opens AddTaskListDialog ("+" in the sidebar, "Create task list" in the no-lists state). */
const showAddTaskListDialog = ref(false);
/** TaskListSettingsDialog: the owned task list it shows, and the tab it opens on. */
const showListSettings = ref(false);
const settingsList = ref<Calendar | null>(null);
const settingsTab = ref<'general' | 'sharing'>('general');
const listSettings = ref<InstanceType<typeof TaskListSettingsDialog> | null>(null);
/** The task open in TaskEditDialog (kept after it closes, so the dialog can finish its leave transition). */
const openedTask = ref<Task | null>(null);
const showEditDialog = ref(false);
/** "Add a task" with details, and the title the quick-add held when it was opened. */
const showCreateDialog = ref(false);
const createSummary = ref('');

const quickAdd = ref<InstanceType<typeof TaskQuickAdd> | null>(null);
const taskList = ref<InstanceType<typeof TaskList> | null>(null);
/** Whether this visit already asked for the lists (a stale `?list=` is then really unknown). */
let listsFetched = false;

const writableIds = computed(() => tasksStore.writableTaskLists.map((l: Calendar) => l.uuid));

const isKnownList = (uuid: string) => tasksStore.taskLists.some((l: Calendar) => l.uuid === uuid);

function listParam(): string | null {
  const value = route.query.list;
  return typeof value === 'string' && value !== '' ? value : null;
}

async function loadLists() {
  listsFetched = true;
  listsLoading.value = true;
  // fetchCalendars swallows its failure into `error` and never clears it on
  // success, so clear it first to read this load's outcome.
  calendarStore.error = null;
  try {
    await calendarStore.fetchCalendars();
  } finally {
    listsLoading.value = false;
    listsFailed.value = calendarStore.error !== null && calendarStore.calendars.length === 0;
  }
}

/** Points the URL at a list (null = All tasks), keeping every other query param. */
function writeListParam(listId: string | null) {
  if (listParam() === listId) return;
  const rest = Object.fromEntries(Object.entries(route.query).filter(([key]) => key !== 'list'));
  // An aborted navigation rejects; nothing awaits this, so say so instead of
  // leaving an unhandled rejection.
  Promise.resolve(
    navigateTo({ path: route.path, query: listId ? { ...rest, list: listId } : rest }, { replace: true }),
  ).catch((e: unknown) => console.warn('Could not update the task list in the URL', e));
}

/**
 * Shows what `?list=` names. A UUID the calendars store does not know (a list
 * made on another device since the store loaded) gets one refetch; if it is
 * still unknown (deleted, not a task list, no longer shared) the page shows
 * All tasks and drops the param, instead of asking the server for it and
 * showing "Calendar not found". `force` reloads even when that view is
 * already selected (page mount).
 */
async function applyListParam(force: boolean) {
  const wanted = listParam();
  if (wanted && !isKnownList(wanted) && !listsFetched) await loadLists();
  const target = wanted && isKnownList(wanted) ? wanted : null;
  // selectList/selectAll set the selection synchronously, so the watcher run
  // that the URL fix below triggers finds it in place and does nothing.
  const loading = force || target !== tasksStore.selectedListId
    ? (target ? tasksStore.selectList(target) : tasksStore.selectAll())
    : undefined;
  if (wanted && !target) writeListParam(null);
  await loading;
}

/** The sidebar or the phone picker chose a list (null = All tasks). */
function selectList(listId: string | null) {
  if (listId !== tasksStore.selectedListId) {
    void (listId ? tasksStore.selectList(listId) : tasksStore.selectAll());
  }
  writeListParam(listId);
}

function openAddTaskList() {
  showAddTaskListDialog.value = true;
}

/** A new list is where the user goes next: it is selected (and in the URL), so the quick-add adds to it. */
function onListCreated(list: Calendar) {
  selectList(list.uuid);
}

/** The sidebar's list menu: "List settings" (General) or "Share list" (Sharing). */
function openListSettings(list: Calendar, tab: 'general' | 'sharing') {
  settingsList.value = list;
  settingsTab.value = tab;
  showListSettings.value = true;
}

/** The sidebar's "Delete task list": the settings dialog's own question and delete, without opening it. */
function deleteList(list: Calendar) {
  listSettings.value?.confirmDelete(list);
}

/**
 * A list was deleted for good: its rows leave the page and the counts are
 * refreshed. When it was the selected one, the view goes to All tasks (and
 * the param leaves the URL); with no list left the "No task lists yet" state
 * shows by itself.
 */
function onListDeleted(list: Calendar) {
  tasksStore.forgetTaskList(list.uuid);
  if (tasksStore.selectedListId === list.uuid) selectList(null);
}

/** A row asked to open its task: the editor, read only when the user may not write to its list. */
function openTask(task: Task) {
  openedTask.value = task;
  showEditDialog.value = true;
}

const openedReadOnly = computed(() => {
  const task = openedTask.value;
  return task !== null && !writableIds.value.includes(task.calendar_uuid);
});

/**
 * The editor closed. PrimeVue gives the focus back to the row that opened it;
 * when that row was re-rendered meanwhile (a new status moved it to another
 * group) or deleted, the focus fell to the page, so it goes to the task's row
 * (or to the list's current row) instead.
 */
function onEditDialogHidden() {
  const focused = document.activeElement;
  const lost = !focused || focused === document.body || !focused.isConnected;
  if (lost && openedTask.value) taskList.value?.focusTask(openedTask.value.id);
}

function openCreateDialog(summary: string) {
  createSummary.value = summary;
  showCreateDialog.value = true;
}

/**
 * A new task that the current view does not show (the Done filter, or the
 * dialog put it into another list) would vanish without a word: say where it went.
 */
function onCreated(task: Task) {
  if (tasksStore.visibleTasks.some((t: Task) => t.id === task.id)) return;
  const list = tasksStore.taskLists.find((l: Calendar) => l.uuid === task.calendar_uuid);
  toast.success(list ? `Task added to ${list.name}` : 'Task added');
}

/** The dialog opened from the quick-add created the task: its text is used up. */
function onDialogCreated(task: Task) {
  quickAdd.value?.clear();
  onCreated(task);
}

/** "Nothing to do here": the quick-add takes the focus (spec §7.5), unless a dialog has it. */
function onEmptyState(variant: TaskEmptyVariant | null) {
  if (variant !== 'empty' || showCreateDialog.value || showEditDialog.value) return;
  if (showAddTaskListDialog.value || showListSettings.value) return;
  void nextTick(() => quickAdd.value?.focus());
}

/** "Try again" in the list's error banner: the lists first if they failed, then the tasks. */
async function retry() {
  if (listsFailed.value) await loadLists();
  await tasksStore.fetchTasks();
}

// A link into /tasks while already here (the search page, story 28) changes
// only the query, which does not remount the page.
watch(
  () => route.query.list,
  () => {
    applyListParam(false).catch((e: unknown) => console.warn('Could not open the task list', e));
  },
);

onMounted(async () => {
  // The summary needs nothing else; the lists are reused when another page
  // already loaded them (one GET /calendars serves both sections).
  void tasksStore.fetchSummary();
  if (calendarStore.calendars.length === 0) await loadLists();
  await applyListParam(true);
});
</script>
