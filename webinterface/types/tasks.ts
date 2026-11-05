/**
 * Task (VTODO) types (#170). They mirror the task API (server/CLAUDE.md
 * "Tasks (#170)", Go DTOs in `server/internal/adapter/http/dto/task_dto.go`),
 * which answers RAW JSON: no `{ status, data }` envelope, so nothing is
 * unwrapped. Errors are `{ status: "error", message }`.
 *
 * A task list is a calendar whose `kind` is `tasks` (or `both`); it is listed
 * by `GET /calendars` like any calendar (`types/calendar.ts`).
 */

import type { Reminder, ReminderInput } from './calendar';

/** A task's status on the wire: lower-case with hyphens. */
export type TaskStatus = 'needs-action' | 'in-process' | 'completed' | 'cancelled';

/**
 * The `status` filter of the task lists: `open` (needs-action + in-process,
 * the server default), `completed` (DONE: completed AND cancelled), `all`.
 */
export type TaskStatusFilter = 'open' | 'completed' | 'all';

/**
 * One task as every task route answers it (Go `TaskResponse`). Every nullable
 * key is always present, as `null`.
 *
 * Due date: a timed task has `due` (RFC 3339 in UTC, `Z`) and `due_date: null`;
 * an all-day task has `due_date` (`YYYY-MM-DD`, a calendar date, never to be
 * passed to `new Date()`, which reads it as UTC midnight and shifts it a day
 * west of Greenwich) and `due: null`; a task without one has both null. The
 * helpers in `utils/tasks.ts` read it that way. `start`/`start_date` follow the
 * same rule for DTSTART, by its own value type.
 *
 * R3: a floating DUE (no zone in the iCalendar) arrives as UTC wall time. A
 * known approximation of the server, not something the client can repair.
 */
export interface Task {
  /** The task's UUID: what every task URL takes as `:task_id`. */
  id: string;
  /** Numeric calendar id (parity with `CalendarEvent.calendar_id`). */
  calendar_id: number;
  /** UUID of the task list: every task URL, `?list=`, `by_calendar` and the store key by it. */
  calendar_uuid: string;
  uid: string;
  summary: string;
  description: string;
  status: TaskStatus;
  /** `status === 'completed'`: the checkbox state. A cancelled task is not `completed`. */
  completed: boolean;
  completed_at: string | null;
  /** 0..100, or null when the task carries none. */
  percent_complete: number | null;
  /**
   * RFC 5545 PRIORITY 1..9 (1 highest), or null for none (absent or 0).
   * Shown as a level through `priorityLevel()`: 1-4 high, 5 medium, 6-9 low.
   */
  priority: number | null;
  /** Timed due instant, RFC 3339 UTC; null for an all-day or undated task. */
  due: string | null;
  /** All-day due date `YYYY-MM-DD`; null for a timed or undated task. */
  due_date: string | null;
  /**
   * The task's all-day flag (DUE's value type, else DTSTART's). True with
   * `due_date: null` means a date-only start and no due date.
   */
  due_all_day: boolean;
  start: string | null;
  start_date: string | null;
  /** RRULE or RDATE on the task: listed once, never expanded, and not completable over REST (409). */
  is_recurring: boolean;
  /** The repeat rule, read-only (there is no recurrence editing in v1). */
  rrule: string | null;
  etag: string;
  /** The task's VALARMs (story 18): `[]` for none; the editable ones are `RELATED=END` (relative to the due date). */
  reminders?: Reminder[];
  created_at: string;
  updated_at: string;
  /**
   * Name and colour of the task list. Only on the items of the cross-list
   * `GET /api/v1/tasks`; the per-list routes and every write answer without
   * them.
   */
  calendar_name?: string;
  calendar_color?: string;
}

/** `GET /calendars/:calendar_id/tasks` and `GET /tasks`: one page. */
export interface TaskListResponse {
  tasks: Task[];
  /** Tasks on this page. */
  count: number;
  /** Tasks across all pages; `total > count` means the list is truncated. */
  total: number;
}

/**
 * Body of `POST /calendars/:calendar_id/tasks` (Go `CreateTaskRequest`).
 * `due_all_day` decides the format of `due` AND `start`: `YYYY-MM-DD` when
 * true, else RFC 3339 with an offset. `null`, absent and `''` all mean "none"
 * for due, start, priority and percent. There is no recurrence field.
 */
export interface TaskInput {
  /** Required, trimmed by the server, at most 500 characters. */
  summary: string;
  description?: string;
  due?: string | null;
  due_all_day?: boolean;
  start?: string | null;
  /** 0..9, stored as given; the form sends 1 (high), 5 (medium), 9 (low). */
  priority?: number | null;
  /** 0..100. */
  percent_complete?: number | null;
  /** Default `needs-action`; `completed` also stamps COMPLETED and 100%. */
  status?: TaskStatus;
  /** IANA zone the timed due/start are written in (`TZID`); without one they are stored in UTC. */
  timezone?: string;
  /** Editable reminders, relative to the due date (needs a due date); a list replaces the stored editable ones. */
  reminders?: ReminderInput[];
}

/**
 * Body of `PATCH /calendars/:calendar_id/tasks/:task_id` (Go
 * `UpdateTaskRequest`). Only the keys sent change; an absent key keeps the
 * stored value. `null` (or `''` for description, due, start) REMOVES
 * description, due, start, priority or percent_complete; summary, status and
 * due_all_day cannot be removed. Sending an unchanged value writes nothing.
 * On a recurring task, `status` completed/cancelled or `percent_complete: 100`
 * is a 409, even when it is the stored value.
 */
export interface TaskPatch {
  summary?: string;
  description?: string | null;
  due?: string | null;
  due_all_day?: boolean;
  start?: string | null;
  priority?: number | null;
  percent_complete?: number | null;
  status?: TaskStatus;
  /** Applies to the timed due/start sent in the same body; without it they are written in UTC. */
  timezone?: string;
  /** The full list of editable reminders the form keeps (replaces the stored ones); sent only when changed. */
  reminders?: ReminderInput[];
}

/** One task list's entry in `TasksSummary.by_calendar`. */
export interface TaskListCounts {
  open: number;
  overdue: number;
  /** The list's done tasks, completed AND cancelled (like `TasksSummary.completed`). */
  completed: number;
}

/**
 * `GET /api/v1/tasks/summary?as_of=` (send `as_of` through ofetch's `query`
 * option: an interpolated `+01:00` arrives as a space and is a 400).
 * `completed` counts the done tasks, completed AND cancelled. `overdue` and
 * `due_today` overlap (a timed task due earlier today is both).
 * `by_calendar` has an entry, keyed by calendar UUID, for every readable task
 * list, zeros included.
 */
export interface TasksSummary {
  open: number;
  overdue: number;
  due_today: number;
  completed: number;
  by_calendar: Record<string, TaskListCounts>;
}

/**
 * The list's groups (spec §7.4): one per status. Grouping is by status only;
 * "overdue" is a row marker (`dueBucket`), never a group.
 */
export type TaskGroupKey = TaskStatus;

/** The groups in display order: Needs action, In progress, Completed, Cancelled. */
export const TASK_GROUP_ORDER: readonly TaskGroupKey[] = ['needs-action', 'in-process', 'completed', 'cancelled'];

/**
 * One group of the list as `useTasksStore().groups` hands it out: its status,
 * its heading ("Needs action", …) and its rows, sorted with `compareTasks`.
 * Never empty (empty groups are left out).
 */
export interface TaskGroup {
  key: TaskGroupKey;
  label: string;
  tasks: Task[];
}

/** How the user sorts within a group. */
export type TaskSortBy = 'due' | 'priority' | 'title';
export type TaskSortDir = 'asc' | 'desc';

/**
 * Where a task's due date falls relative to now (`dueBucket`): drives the
 * overdue styling, and the due sort follows its order; grouping is by status.
 */
export type DueBucket = 'overdue' | 'today' | 'later' | 'none';

/** The display level of a priority (`priorityLevel`). */
export type PriorityLevel = 'high' | 'medium' | 'low' | 'none';

/** Which empty state the task list shows (`TaskEmptyState`, spec §7.5). */
export type TaskEmptyVariant = 'no-lists' | 'empty' | 'no-match' | 'all-done';

/**
 * What `TaskForm` edits and emits (story 26): the writable fields in form
 * shape. `utils/taskForm.ts` turns it into the POST body (`buildTaskInput`)
 * or into a PATCH of the changed keys only (`buildTaskPatch`).
 */
export interface TaskFormData {
  /** As typed (the server trims it). Plain text: never escaped or unescaped here. */
  summary: string;
  /** The notes, plain text, sent exactly as typed. '' = none. */
  description: string;
  /** Calendar UUID of the task list. Chosen on create only: a task never moves between lists. */
  listId: string;
  /**
   * One flag for due AND start, as on the wire (`due_all_day`): dates when
   * true (`YYYY-MM-DD` from the LOCAL date), date-times otherwise.
   */
  allDay: boolean;
  due: Date | null;
  start: Date | null;
  /** The level the form shows; saved as 1 (high), 5 (medium), 9 (low) or none. */
  priority: PriorityLevel;
  /** 0..100, or null when the task has none. */
  percent: number | null;
  status: TaskStatus;
  /** All the task's reminders as the API answered them, editable or not; read-only ones are never sent. */
  reminders: Reminder[];
}
