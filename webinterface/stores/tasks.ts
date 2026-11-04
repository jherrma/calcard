import type { Calendar } from '~/types/calendar';
import type {
  Task,
  TaskGroup,
  TaskGroupKey,
  TaskInput,
  TaskListResponse,
  TaskPatch,
  TaskSortBy,
  TaskSortDir,
  TaskStatusFilter,
  TasksSummary,
} from '~/types/tasks';
import { TASK_GROUP_ORDER } from '~/types/tasks';
import { compareTasks, isOverdue as isTaskOverdue, isTaskDone } from '~/utils/tasks';
import { toRFC3339, useCalendarStore } from '~/stores/calendars';

/**
 * The Tasks section's state (#170, spec §7.2): the task lists (read from the
 * calendars store), the loaded tasks, the list/filter/search/sort selection,
 * the summary counts behind the sidebar badge, and every task mutation.
 *
 * Wire rules (server/CLAUDE.md "Tasks (#170)"): every task route answers RAW
 * JSON, no `{ status, data }` envelope. Query parameters always go through
 * ofetch's `query` option, never into the URL string: the `+` of an `as_of`
 * offset such as `+01:00` would arrive as a space and the server answers 400.
 *
 * Errors of a mutation are toasted HERE (`useAppToast()`) and the action
 * resolves to null/false instead of throwing, so the callers (rows, quick-add,
 * dialogs) only branch on the result and never toast a second time. A failed
 * load sets `error` instead (the list shows its banner, spec §7.5).
 */

interface TasksState {
  /** Tasks of the selected list (or of all lists), as loaded plus local writes. Read `visibleTasks`/`groups`. */
  tasks: Task[];
  /** Calendar UUID of the selected task list; null = all lists (single-select). */
  selectedListId: string | null;
  statusFilter: TaskStatusFilter;
  /** The search text sent as `q` (trimmed; blank = no search). */
  searchQuery: string;
  sortBy: TaskSortBy;
  sortDir: TaskSortDir;
  summary: TasksSummary | null;
  /** `total` of the last task page (server count of the query), adjusted by local creates/deletes. `total > tasks.length` = truncated. */
  total: number;
  isLoading: boolean;
  /** Task ids with an in-flight write (pending UI). */
  isSaving: Set<string>;
  /** Why the last `fetchTasks` failed, or null. The previous rows stay. */
  error: string | null;
  /** Sequence tokens: a response that is no longer the newest request's is dropped. */
  tasksRequestSeq: number;
  summaryRequestSeq: number;
}

/** Group headings (spec §7.4), in sentence case. */
export const TASK_GROUP_LABELS: Record<TaskGroupKey, string> = {
  'needs-action': 'Needs action',
  'in-process': 'In progress',
  completed: 'Completed',
  cancelled: 'Cancelled',
};

/** localStorage key of the sort preference (device-local, like the theme). */
export const TASK_SORT_STORAGE_KEY = 'calcard-tasks-sort';

const DEFAULT_SORT_BY: TaskSortBy = 'due';
const DEFAULT_SORT_DIR: TaskSortDir = 'asc';
const SORT_BYS: readonly TaskSortBy[] = ['due', 'priority', 'title'];
const SORT_DIRS: readonly TaskSortDir[] = ['asc', 'desc'];

const LOAD_ERROR = "Couldn't load tasks. Check your connection and try again.";
const DELETE_MESSAGE = "Delete this task? It is removed from every device that syncs this list. This can't be undone.";

/**
 * The stored sort, or the default. Storage access is wrapped as a whole: in
 * Safari private mode and with blocked cookies the `localStorage` property READ
 * throws, not just `getItem` (same rule as `utils/theme.ts`). Junk values (a
 * stale build, devtools) count as absent.
 */
export function readStoredTaskSort(): { sortBy: TaskSortBy; sortDir: TaskSortDir } {
  try {
    const raw = window.localStorage.getItem(TASK_SORT_STORAGE_KEY);
    const parsed: unknown = raw ? JSON.parse(raw) : null;
    const stored = (parsed && typeof parsed === 'object' ? parsed : {}) as Record<string, unknown>;
    return {
      sortBy: SORT_BYS.includes(stored.sortBy as TaskSortBy) ? (stored.sortBy as TaskSortBy) : DEFAULT_SORT_BY,
      sortDir: SORT_DIRS.includes(stored.sortDir as TaskSortDir) ? (stored.sortDir as TaskSortDir) : DEFAULT_SORT_DIR,
    };
  } catch {
    return { sortBy: DEFAULT_SORT_BY, sortDir: DEFAULT_SORT_DIR };
  }
}

/** Persists the sort. A failure is swallowed: the in-memory choice still applies for this session. */
export function writeStoredTaskSort(sortBy: TaskSortBy, sortDir: TaskSortDir): void {
  try {
    window.localStorage.setItem(TASK_SORT_STORAGE_KEY, JSON.stringify({ sortBy, sortDir }));
  } catch {
    // Storage unavailable: keep the in-memory choice.
  }
}

/** Calendars that hold tasks: task lists and mixed calendars (subscriptions included). Never `events`, never kind-less. */
function isTaskList(c: Calendar): boolean {
  return c.kind === 'tasks' || c.kind === 'both';
}

/** Whether a task belongs to the current view: the selected list (or any) and the status filter. */
function matchesView(task: Task, selectedListId: string | null, statusFilter: TaskStatusFilter): boolean {
  if (selectedListId !== null && task.calendar_uuid !== selectedListId) return false;
  if (statusFilter === 'open') return !isTaskDone(task);
  if (statusFilter === 'completed') return isTaskDone(task);
  return true;
}

/** Whether a task's summary or notes contain the search text (the server's `q`: case-insensitive substring). */
function matchesSearch(task: Task, query: string): boolean {
  const needle = query.trim().toLowerCase();
  return (
    task.summary.toLowerCase().includes(needle) || (task.description ?? '').toLowerCase().includes(needle)
  );
}

/**
 * A write answer carries no `calendar_name`/`calendar_color` (only `GET /tasks`
 * items do): keep the old row's, else take them from the calendars store, so
 * the "All tasks" list dot keeps its colour.
 */
function withListInfo(fresh: Task, old?: Task): Task {
  const merged = { ...old, ...fresh } as Task;
  if (merged.calendar_name !== undefined && merged.calendar_color !== undefined) return merged;
  const list = useCalendarStore().calendars.find((c: Calendar) => c.uuid === fresh.calendar_uuid);
  if (!list) return merged;
  return { ...merged, calendar_name: merged.calendar_name ?? list.name, calendar_color: merged.calendar_color ?? list.color };
}

/**
 * Toasts a failed write. Store actions run outside component setup, where
 * PrimeVue's inject()-based `useToast()` throws, so it runs in the app context
 * (as useApi's 429 notice does). A 429 is skipped: useApi already raised its
 * one toast for the burst.
 */
function notifyError(e: unknown, title: string, fallback: string): void {
  if (httpErrorStatus(e) === 429) return;
  const message = clientErrorMessage(e, fallback);
  try {
    useNuxtApp().vueApp.runWithContext(() => useAppToast().error(message, title));
  } catch {
    console.error(title, e);
  }
}

export const useTasksStore = defineStore('tasks', {
  state: (): TasksState => ({
    tasks: [],
    selectedListId: null,
    statusFilter: 'open',
    searchQuery: '',
    ...readStoredTaskSort(),
    summary: null,
    total: 0,
    isLoading: false,
    isSaving: new Set(),
    error: null,
    tasksRequestSeq: 0,
    summaryRequestSeq: 0,
  }),

  getters: {
    /**
     * Every calendar that holds tasks, read from the calendars store's ONE
     * `GET /calendars` (no request of its own): `kind` `tasks` and `both`
     * (mixed legacy calendars and subscriptions), owned and shared.
     */
    taskLists(): Calendar[] {
      return useCalendarStore().calendars.filter(isTaskList);
    },

    /** Task lists the user may write to (mirrors `writableCalendars`): no subscription, no read-only share. */
    writableTaskLists(): Calendar[] {
      return this.taskLists.filter(
        (c: Calendar) => !c.subscribed && (!c.shared || c.permission === 'read-write'),
      );
    },

    /**
     * Where a new task goes from the current view (quick-add, "Add a task"):
     * the selected list when the user may write to it, under All tasks the
     * first writable list; null when there is none (a read-only share or a
     * subscription is selected, or no list is writable), and then nothing
     * offers to add a task.
     */
    newTaskListId(state: TasksState): string | null {
      const writable: Calendar[] = this.writableTaskLists;
      if (state.selectedListId !== null) {
        return writable.some((c: Calendar) => c.uuid === state.selectedListId) ? state.selectedListId : null;
      }
      return writable[0]?.uuid ?? null;
    },

    /** The loaded tasks that belong to the current view (selected list, status filter). */
    visibleTasks(state: TasksState): Task[] {
      return state.tasks.filter((t: Task) => matchesView(t, state.selectedListId, state.statusFilter));
    },

    /** Status groups in display order (Needs action, In progress, Completed, Cancelled), empties left out, each sorted. */
    groups(): TaskGroup[] {
      const order = compareTasks(this.sortBy, this.sortDir);
      return TASK_GROUP_ORDER.map((key: TaskGroupKey) => ({
        key,
        label: TASK_GROUP_LABELS[key],
        tasks: this.visibleTasks.filter((t: Task) => t.status === key).sort(order),
      })).filter((group: TaskGroup) => group.tasks.length > 0);
    },

    /** The badge count: the SERVER's overdue count over every list, not a count of the (possibly truncated) loaded rows. */
    overdueCount(state: TasksState): number {
      return state.summary?.overdue ?? 0;
    },

    /**
     * `isOverdue(task, now)` of `utils/tasks.ts`: the server's overdue rule, so
     * a row that is red is a row the badge counts once the summary is fetched
     * again. The sidebar does that at local midnight, at the next timed due
     * instant of a loaded task, when the tab becomes visible again, and the
     * store after each task mutation (rows can turn red between those moments).
     */
    isOverdue(): (task: Pick<Task, 'status' | 'due' | 'due_date'>, now: Date) => boolean {
      return (task, now) => isTaskOverdue(task, now);
    },
  },

  actions: {
    /**
     * Loads the tasks of the selected list (or of all lists) with the status
     * filter and the search text: ONE `GET /api/v1/tasks` (cross-list, items
     * carry `calendar_name`/`calendar_color`), narrowed by `calendar_uuid`.
     * On failure the previous rows stay and `error` is set.
     */
    async fetchTasks(): Promise<void> {
      const seq = ++this.tasksRequestSeq;
      this.isLoading = true;
      this.error = null;
      const query: Record<string, string> = { status: this.statusFilter };
      if (this.selectedListId) query.calendar_uuid = this.selectedListId;
      const searchText = this.searchQuery.trim();
      if (searchText) query.q = searchText;
      try {
        const api = useApi();
        const response = await api<TaskListResponse>('/api/v1/tasks', { query });
        if (seq !== this.tasksRequestSeq) return;
        this.tasks = response.tasks ?? [];
        this.total = response.total ?? this.tasks.length;
      } catch (e: unknown) {
        if (seq !== this.tasksRequestSeq) return;
        this.error = clientErrorMessage(e, LOAD_ERROR);
      } finally {
        if (seq === this.tasksRequestSeq) this.isLoading = false;
      }
    },

    /**
     * Loads the summary counts at the browser's local now: `as_of` is
     * `toRFC3339(new Date())` (local offset, which defines "today") and goes
     * through the `query` option. A failure keeps the previous counts: the
     * badge is background information on every page, not worth a toast.
     */
    async fetchSummary(): Promise<void> {
      const seq = ++this.summaryRequestSeq;
      try {
        const api = useApi();
        const summary = await api<TasksSummary>('/api/v1/tasks/summary', {
          query: { as_of: toRFC3339(new Date()) },
        });
        if (seq === this.summaryRequestSeq) this.summary = summary;
      } catch (e: unknown) {
        console.warn('Failed to load the task summary', e);
      }
    },

    /** Selects one task list (calendar UUID) and loads its tasks. */
    selectList(listId: string): Promise<void> {
      this.selectedListId = listId;
      return this.fetchTasks();
    },

    /** Selects "All tasks" and loads them. */
    selectAll(): Promise<void> {
      this.selectedListId = null;
      return this.fetchTasks();
    },

    setStatusFilter(filter: TaskStatusFilter): Promise<void> {
      this.statusFilter = filter;
      return this.fetchTasks();
    },

    setSearchQuery(query: string): Promise<void> {
      this.searchQuery = query;
      return this.fetchTasks();
    },

    /** "Clear filters": back to Open with no search, in ONE load (the list and the sort stay). */
    clearFilters(): Promise<void> {
      this.searchQuery = '';
      this.statusFilter = 'open';
      return this.fetchTasks();
    },

    /** Sets the in-group sort (direction kept when omitted) and remembers it on this device. */
    setSort(sortBy: TaskSortBy, sortDir?: TaskSortDir): void {
      this.sortBy = sortBy;
      this.sortDir = sortDir ?? this.sortDir;
      writeStoredTaskSort(this.sortBy, this.sortDir);
    },

    toggleSortDir(): void {
      this.setSort(this.sortBy, this.sortDir === 'asc' ? 'desc' : 'asc');
    },

    /** Puts a write answer in place of its row (keeping the list name/colour); the merged task, also when no row holds it. */
    replaceTask(fresh: Task): Task {
      const idx = this.tasks.findIndex((t: Task) => t.id === fresh.id);
      const merged = withListInfo(fresh, idx >= 0 ? this.tasks[idx] : undefined);
      if (idx >= 0) this.tasks[idx] = merged;
      return merged;
    },

    /**
     * Creates a task in a list (calendar UUID). The new row joins the view when
     * it belongs there. Resolves to the task, or null after a toast.
     */
    async createTask(listId: string, input: TaskInput): Promise<Task | null> {
      try {
        const api = useApi();
        const created = await api<Task>(`/api/v1/calendars/${listId}/tasks`, { method: 'POST', body: input });
        const task = withListInfo(created);
        if (
          matchesView(task, this.selectedListId, this.statusFilter) &&
          !this.tasks.some((t: Task) => t.id === task.id)
        ) {
          this.tasks.push(task);
          this.total += 1;
        }
        return task;
      } catch (e: unknown) {
        notifyError(e, "Couldn't add the task", RETRY_HINT);
        return null;
      } finally {
        void this.fetchSummary();
      }
    },

    /**
     * PATCHes a task (only the keys in `patch` change; `null` removes) and puts
     * the answer in place of its row. Resolves to the task, or null after a toast.
     */
    async updateTask(task: Task, patch: TaskPatch): Promise<Task | null> {
      this.isSaving.add(task.id);
      try {
        const api = useApi();
        const fresh = await api<Task>(`/api/v1/calendars/${task.calendar_uuid}/tasks/${task.id}`, {
          method: 'PATCH',
          body: patch,
        });
        return this.replaceTask(fresh);
      } catch (e: unknown) {
        notifyError(e, "Couldn't save the task", RETRY_HINT);
        return null;
      } finally {
        this.isSaving.delete(task.id);
        void this.fetchSummary();
      }
    },

    /**
     * Ticks or unticks a task, optimistically: the row flips at once, then
     * `POST …/complete` (or `…/uncomplete` for a completed task; a cancelled one
     * is completed) runs. Success puts the server row in place; failure puts
     * the old row back (unless something newer replaced it meanwhile) and
     * toasts, with the server's sentence for the 409 on a repeating task.
     * A second toggle while one is in flight is ignored. Resolves to the
     * server row, or null.
     */
    async toggleComplete(task: Task): Promise<Task | null> {
      if (this.isSaving.has(task.id)) return null;
      const idx = this.tasks.findIndex((t: Task) => t.id === task.id);
      const before: Task = idx >= 0 ? { ...this.tasks[idx]! } : { ...task };
      const completing = !before.completed;
      const optimistic: Task = completing
        ? { ...before, status: 'completed', completed: true, completed_at: new Date().toISOString(), percent_complete: 100 }
        : {
            ...before,
            status: 'needs-action',
            completed: false,
            completed_at: null,
            percent_complete: (before.percent_complete ?? 0) >= 100 ? 0 : before.percent_complete,
          };
      if (idx >= 0) this.tasks[idx] = optimistic;

      this.isSaving.add(task.id);
      try {
        const api = useApi();
        const fresh = await api<Task>(
          `/api/v1/calendars/${before.calendar_uuid}/tasks/${before.id}/${completing ? 'complete' : 'uncomplete'}`,
          { method: 'POST' },
        );
        return this.replaceTask(fresh);
      } catch (e: unknown) {
        const at = this.tasks.findIndex((t: Task) => t.id === task.id);
        if (at >= 0 && toRaw(this.tasks[at]) === optimistic) this.tasks[at] = before;
        notifyError(
          e,
          completing ? "Couldn't complete the task" : "Couldn't mark the task as not done",
          RETRY_HINT,
        );
        return null;
      } finally {
        this.isSaving.delete(task.id);
        void this.fetchSummary();
      }
    },

    /**
     * Undo of a tick: puts the task back as it was before it, a PATCH of the
     * status and percent the row had (the server's own uncomplete would make it
     * needs-action at 0 %, losing "in progress at 40 %" or "cancelled"). The
     * server row goes back into the visible list when it matches the view,
     * also after the view changed since the tick (another list, filter or
     * search), as `createTask` does. Resolves to the task, or null after a toast.
     */
    async undoComplete(before: Task): Promise<Task | null> {
      const fresh = await this.updateTask(before, {
        status: before.status,
        percent_complete: before.percent_complete,
      });
      if (fresh) this.restoreRow(fresh);
      return fresh;
    },

    /** Adds a server row to the loaded page when it belongs to the view and is not there yet. */
    restoreRow(task: Task): void {
      if (this.tasks.some((t: Task) => t.id === task.id)) return;
      if (!matchesView(task, this.selectedListId, this.statusFilter)) return;
      if (this.searchQuery.trim() !== '' && !matchesSearch(task, this.searchQuery)) return;
      this.tasks.push(task);
      this.total += 1;
    },

    /**
     * Asks "Delete this task?" (PrimeVue `useConfirm()`, the layout's
     * ConfirmDialog) and deletes on accept. Resolves true once deleted, false
     * when the user cancels or the delete fails (toasted).
     */
    deleteTask(task: Task): Promise<boolean> {
      let confirm: ReturnType<typeof useConfirm>;
      try {
        // useConfirm() injects, so like the toast it needs the app context.
        confirm = useNuxtApp().vueApp.runWithContext(() => useConfirm());
      } catch (e: unknown) {
        console.error('No confirmation service: the task was not deleted', e);
        return Promise.resolve(false);
      }
      return new Promise<boolean>((resolve) => {
        let settled = false;
        const settle = (deleted: boolean) => {
          if (settled) return;
          settled = true;
          resolve(deleted);
        };
        confirm.require({
          header: 'Delete task',
          message: DELETE_MESSAGE,
          icon: 'pi pi-exclamation-triangle',
          acceptClass: 'p-button-danger',
          acceptLabel: 'Delete task',
          rejectLabel: 'Cancel',
          accept: () => {
            void this.removeTask(task).then(settle);
          },
          reject: () => settle(false),
          // Escape or the close button.
          onHide: () => settle(false),
        });
      });
    },

    /**
     * The delete itself, without asking (`deleteTask` asks first). A 404 means
     * the task is already gone (another device deleted it): the row is dropped
     * and it counts as deleted. Resolves true when deleted.
     */
    async removeTask(task: Task): Promise<boolean> {
      this.isSaving.add(task.id);
      try {
        const api = useApi();
        await api(`/api/v1/calendars/${task.calendar_uuid}/tasks/${task.id}`, { method: 'DELETE' });
        this.dropTask(task.id);
        return true;
      } catch (e: unknown) {
        if (httpErrorStatus(e) === 404) {
          this.dropTask(task.id);
          return true;
        }
        notifyError(e, "Couldn't delete the task", RETRY_HINT);
        return false;
      } finally {
        this.isSaving.delete(task.id);
        void this.fetchSummary();
      }
    },

    dropTask(taskId: string): void {
      const before = this.tasks.length;
      this.tasks = this.tasks.filter((t: Task) => t.id !== taskId);
      if (this.tasks.length < before) this.total = Math.max(0, this.total - 1);
    },

    /**
     * Creates a task list: `POST /calendars` with `kind: 'tasks'` (a VTODO-only
     * calendar, never shown in the Calendar section), then refreshes the
     * calendars store so `taskLists` has it. Resolves to the new list, or null
     * after a toast.
     */
    async createTaskList(name: string, color: string): Promise<Calendar | null> {
      let created: Calendar;
      try {
        const api = useApi();
        created = await api<Calendar>('/api/v1/calendars', {
          method: 'POST',
          body: { name, color, kind: 'tasks' },
        });
      } catch (e: unknown) {
        notifyError(e, "Couldn't create the task list", RETRY_HINT);
        return null;
      }
      const calendarStore = useCalendarStore();
      await calendarStore.fetchCalendars();
      // fetchCalendars swallows its failure; the 201 body (it carries `kind`)
      // keeps the new list visible until the next refetch.
      if (!calendarStore.calendars.some((c: Calendar) => c.uuid === created.uuid)) {
        calendarStore.calendars.push(created);
      }
      void this.fetchSummary();
      return created;
    },

    /**
     * A task list was deleted (permanently, with every task in it): its rows
     * leave the loaded page and `total`, and the counts are asked for again.
     * The selection is the caller's: the page also owns `?list=`.
     */
    forgetTaskList(listId: string): void {
      const before = this.tasks.length;
      this.tasks = this.tasks.filter((t: Task) => t.calendar_uuid !== listId);
      this.total = Math.max(0, this.total - (before - this.tasks.length));
      void this.fetchSummary();
    },

    /**
     * A list was renamed or recoloured: the loaded rows carry the list's name
     * and colour (`GET /tasks` sends them), so they take the calendars store's
     * current values. No request.
     */
    applyListInfo(): void {
      const lists = new Map(useCalendarStore().calendars.map((c: Calendar) => [c.uuid, c] as const));
      this.tasks = this.tasks.map((t: Task) => {
        const list = lists.get(t.calendar_uuid);
        if (!list || (t.calendar_name === list.name && t.calendar_color === list.color)) return t;
        return { ...t, calendar_name: list.name, calendar_color: list.color };
      });
    },

    /**
     * Drops every per-user value and invalidates in-flight loads, for a logout
     * (Pinia outlives the session in the tab). The sort preference is the
     * device's and stays. `authStore.clearAuth()` calls it through a lazy
     * import (a static import there would close the cycle
     * useApi -> auth -> tasks -> calendars -> useApi).
     */
    reset(): void {
      this.tasksRequestSeq++;
      this.summaryRequestSeq++;
      this.tasks = [];
      this.selectedListId = null;
      this.statusFilter = 'open';
      this.searchQuery = '';
      this.summary = null;
      this.total = 0;
      this.isLoading = false;
      this.isSaving = new Set();
      this.error = null;
    },
  },
});
