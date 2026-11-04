<template>
  <form class="flex flex-col gap-4" novalidate data-test="task-form" @submit.prevent="handleSubmit">
    <!-- A read-only list (a read-only share or a subscription): everything is shown, nothing can be changed. -->
    <p
      v-if="readOnly"
      class="flex items-center gap-2 rounded-md bg-surface-100 px-3 py-2 text-sm text-surface-700 dark:bg-surface-800 dark:text-surface-200"
      data-test="read-only-note"
    >
      <i class="pi pi-lock text-surface-500 dark:text-surface-400" aria-hidden="true" />
      This list is read only, so the task can't be changed here.
    </p>

    <!-- Title -->
    <div class="flex flex-col gap-1">
      <label :for="ids.title" class="text-sm font-medium text-surface-700 dark:text-surface-300">Title</label>
      <InputText
        :id="ids.title"
        v-model="form.summary"
        :readonly="readOnly"
        :invalid="!!titleError"
        :aria-describedby="titleError ? ids.titleError : undefined"
        autocomplete="off"
        data-test="title"
      />
      <small v-if="titleError" :id="ids.titleError" class="text-red-600 dark:text-red-400" data-test="title-error">
        {{ titleError }}
      </small>
    </div>

    <!-- Notes -->
    <div class="flex flex-col gap-1">
      <label :for="ids.notes" class="text-sm font-medium text-surface-700 dark:text-surface-300">Notes</label>
      <Textarea
        :id="ids.notes"
        v-model="form.description"
        rows="3"
        auto-resize
        :readonly="readOnly"
        data-test="notes"
      />
    </div>

    <!-- List: chosen when the task is added; a task never moves between lists. -->
    <div class="flex flex-col gap-1">
      <label :id="ids.listLabel" :for="ids.list" class="text-sm font-medium text-surface-700 dark:text-surface-300">List</label>
      <Select
        v-model="form.listId"
        :label-id="ids.list"
        :aria-labelledby="ids.listLabel"
        :options="listOptions"
        option-label="name"
        option-value="uuid"
        placeholder="Choose a list"
        :disabled="isEditing || readOnly"
        :invalid="!!listError"
        fluid
        data-test="list"
      >
        <template #value="{ value }">
          <span v-if="value" class="flex min-w-0 items-center gap-2">
            <span
              class="h-2 w-2 shrink-0 rounded-full"
              :class="{ 'bg-surface-400 dark:bg-surface-500': !listColor(value) }"
              :style="listColor(value) ? { backgroundColor: listColor(value) } : undefined"
              aria-hidden="true"
            />
            <span class="truncate">{{ listName(value) }}</span>
          </span>
          <span v-else class="text-surface-500 dark:text-surface-400">Choose a list</span>
        </template>
        <template #option="{ option }">
          <span class="flex min-w-0 items-center gap-2">
            <span class="h-2 w-2 shrink-0 rounded-full" :style="{ backgroundColor: option.color }" aria-hidden="true" />
            <span class="truncate">{{ option.name }}</span>
          </span>
        </template>
      </Select>
      <small v-if="listError" class="text-red-600 dark:text-red-400" data-test="list-error">{{ listError }}</small>
    </div>

    <!-- Repeat rule (spec §1c): read only; there is no recurrence editing in v1. -->
    <div v-if="recurring" class="flex flex-col gap-1" data-test="recurrence">
      <span class="text-sm font-medium text-surface-700 dark:text-surface-300">Repeats</span>
      <p class="flex items-center gap-2 text-sm text-surface-900 dark:text-surface-100">
        <i class="pi pi-replay text-surface-500 dark:text-surface-400" aria-hidden="true" />
        <span data-test="recurrence-text">{{ recurrenceText }}</span>
      </p>
      <small :id="ids.repeatHint" class="text-surface-500 dark:text-surface-400" data-test="recurrence-hint">
        Repeating tasks can be completed in your task app. Their status and progress can't be changed here.
      </small>
    </div>

    <!-- All day: one flag for due AND start (as the API has it). -->
    <div class="flex items-center gap-3">
      <InputSwitch v-model="form.allDay" :input-id="ids.allDay" :disabled="readOnly" data-test="all-day" />
      <label :for="ids.allDay" class="cursor-pointer text-sm text-surface-700 dark:text-surface-300">All day</label>
    </div>

    <div class="grid gap-4 sm:grid-cols-2">
      <!-- Due -->
      <div class="flex min-w-0 flex-col gap-1">
        <label :for="ids.due" class="text-sm font-medium text-surface-700 dark:text-surface-300">Due</label>
        <DatePicker
          v-model="form.due"
          :input-id="ids.due"
          :show-time="!form.allDay"
          :show-seconds="false"
          :hour-format="hourFormat"
          date-format="yy-mm-dd"
          show-button-bar
          placeholder="No due date"
          :disabled="readOnly"
          :invalid="!!dueError"
          :aria-describedby="dueError ? ids.dueError : undefined"
          fluid
          data-test="due"
        />
        <small v-if="dueError" :id="ids.dueError" class="text-red-600 dark:text-red-400" data-test="due-error">
          {{ dueError }}
        </small>
        <small
          v-if="recurringDueChanged"
          class="flex items-start gap-1.5 text-surface-600 dark:text-surface-300"
          data-test="recurring-due-note"
        >
          <i class="pi pi-info-circle mt-0.5 text-xs" aria-hidden="true" />
          This task repeats. A new due date moves the whole series, not just the next one.
        </small>
      </div>

      <!-- Start -->
      <div class="flex min-w-0 flex-col gap-1">
        <label :for="ids.start" class="text-sm font-medium text-surface-700 dark:text-surface-300">Start</label>
        <DatePicker
          v-model="form.start"
          :input-id="ids.start"
          :show-time="!form.allDay"
          :show-seconds="false"
          :hour-format="hourFormat"
          date-format="yy-mm-dd"
          show-button-bar
          placeholder="No start date"
          :disabled="readOnly"
          fluid
          data-test="start"
        />
      </div>
    </div>

    <!-- Priority: the same marks as the rows. -->
    <div class="flex flex-col gap-1">
      <span :id="ids.priorityLabel" class="text-sm font-medium text-surface-700 dark:text-surface-300">Priority</span>
      <SelectButton
        v-model="form.priority"
        :options="PRIORITY_OPTIONS"
        option-label="label"
        option-value="value"
        :allow-empty="false"
        :aria-labelledby="ids.priorityLabel"
        :disabled="readOnly"
        size="small"
        data-test="priority"
      >
        <!-- The marks only from sm: with them the four segments need ~300px,
             more than a 360px phone's dialog has (the labels got squeezed).
             The words carry the meaning; the wrapper takes the `hidden`,
             because primeicons' unlayered `.pi { display }` beats Tailwind's. -->
        <template #option="{ option }">
          <span class="flex items-center gap-1.5">
            <span v-if="option.icon" class="hidden sm:inline-flex" aria-hidden="true" data-test="priority-icon">
              <i :class="[option.icon, 'text-xs']" />
            </span>
            {{ option.label }}
          </span>
        </template>
      </SelectButton>
    </div>

    <!-- Progress -->
    <div class="flex flex-col gap-1">
      <label :id="ids.percentLabel" :for="ids.percent" class="text-sm font-medium text-surface-700 dark:text-surface-300">
        Progress
      </label>
      <div class="flex items-center gap-4">
        <Slider
          v-model="percentSlider"
          :min="0"
          :max="100"
          :step="5"
          :aria-labelledby="ids.percentLabel"
          :disabled="readOnly || recurring"
          class="min-w-0 flex-1"
          data-test="percent-slider"
        />
        <InputNumber
          v-model="form.percent"
          :input-id="ids.percent"
          :min="0"
          :max="100"
          suffix=" %"
          :allow-empty="true"
          :disabled="readOnly || recurring"
          :input-style="{ width: '5.5rem' }"
          data-test="percent"
        />
      </div>
    </div>

    <!-- Status -->
    <div class="flex flex-col gap-1">
      <label :id="ids.statusLabel" :for="ids.status" class="text-sm font-medium text-surface-700 dark:text-surface-300">Status</label>
      <Select
        v-model="form.status"
        :label-id="ids.status"
        :aria-labelledby="ids.statusLabel"
        :options="STATUS_OPTIONS"
        option-label="label"
        option-value="value"
        :disabled="readOnly || recurring"
        fluid
        data-test="status"
      />
    </div>

    <!-- Actions -->
    <div class="flex flex-wrap items-center gap-2 pt-2">
      <Button
        v-if="isEditing && !readOnly"
        label="Delete task"
        icon="pi pi-trash"
        severity="danger"
        text
        data-test="delete-task"
        @click="emit('delete')"
      />
      <div class="ml-auto flex gap-2">
        <Button :label="readOnly ? 'Close' : 'Cancel'" severity="secondary" text data-test="cancel" @click="emit('cancel')" />
        <Button v-if="!readOnly" type="submit" label="Save task" :loading="isSubmitting" data-test="save-task" />
      </div>
    </div>
  </form>
</template>

<script setup lang="ts">
import { useVuelidate } from '@vuelidate/core';
import { helpers, required } from '@vuelidate/validators';
import { usePreferencesStore } from '~/stores/preferences';
import { TASK_GROUP_LABELS, useTasksStore } from '~/stores/tasks';
import type { Calendar } from '~/types/calendar';
import type { PriorityLevel, Task, TaskFormData, TaskStatus } from '~/types/tasks';
import {
  TASK_SUMMARY_MAX,
  TASK_SUMMARY_TOO_LONG,
  describeRecurrence,
  encodeTaskTime,
  isDueBeforeStart,
  newTaskFormData,
  summaryLength,
  taskToFormData,
} from '~/utils/taskForm';

/**
 * The create/edit form of a task (#170, spec §7.3), built like EventForm:
 * title, notes, list, all day + due + start, priority, progress, status.
 *
 * - Create (no `task`): the list is chosen from the WRITABLE task lists.
 * - Edit (`task`): the list is shown but disabled (moving between lists is a
 *   non-goal). A repeating task shows its rule as read-only text, its status
 *   and progress are disabled (the server refuses completing it with a 409,
 *   spec §1c), and a changed due date gets a note.
 * - `readOnly` (the task's list is read-only to the user): nothing can be
 *   changed and there is no Save.
 *
 * It emits the form values together with the values it started from, so the
 * dialog can send only what changed (`buildTaskPatch`) or build the POST
 * body (`buildTaskInput`). Text is plain text: shown as stored, emitted as
 * typed.
 */
const props = defineProps<{
  /** The task to edit; absent = a new task. */
  task?: Task | null;
  /** New task: the list chosen at the start (`tasksStore.newTaskListId`). */
  defaultListId?: string | null;
  /** New task: the title typed into the quick-add before "Add with details". */
  initialSummary?: string;
  readOnly?: boolean;
  isSubmitting?: boolean;
}>();

const emit = defineEmits<{
  submit: [form: TaskFormData, initial: TaskFormData];
  cancel: [];
  /** "Delete task" (edit only); the dialog asks and deletes. */
  delete: [];
}>();

const PRIORITY_OPTIONS: { label: string; value: PriorityLevel; icon?: string }[] = [
  { label: 'None', value: 'none' },
  { label: 'Low', value: 'low', icon: 'pi pi-angle-down' },
  { label: 'Medium', value: 'medium', icon: 'pi pi-angle-up' },
  { label: 'High', value: 'high', icon: 'pi pi-angle-double-up' },
];

const STATUS_OPTIONS: { label: string; value: TaskStatus }[] = (
  ['needs-action', 'in-process', 'completed', 'cancelled'] as const
).map((value) => ({ label: TASK_GROUP_LABELS[value], value }));

const tasksStore = useTasksStore();
const preferencesStore = usePreferencesStore();
const uid = useId();
const ids = {
  title: `${uid}-title`,
  titleError: `${uid}-title-error`,
  notes: `${uid}-notes`,
  list: `${uid}-list`,
  listLabel: `${uid}-list-label`,
  repeatHint: `${uid}-repeat-hint`,
  allDay: `${uid}-all-day`,
  due: `${uid}-due`,
  dueError: `${uid}-due-error`,
  start: `${uid}-start`,
  priorityLabel: `${uid}-priority-label`,
  percent: `${uid}-percent`,
  percentLabel: `${uid}-percent-label`,
  status: `${uid}-status`,
  statusLabel: `${uid}-status-label`,
};

const isEditing = !!props.task;
const recurring = computed(() => !!props.task?.is_recurring);

/** Where the form started: what "changed" is measured against. */
function startValues(): TaskFormData {
  return props.task
    ? taskToFormData(props.task)
    : newTaskFormData(props.defaultListId ?? '', props.initialSummary ?? '');
}

const initial = Object.freeze(startValues());
const form = reactive<TaskFormData>(startValues());

// DatePicker wants '12' / '24', not the '12h' / '24h' that is stored.
const hourFormat = computed(() => (preferencesStore.timeFormat === '12h' ? '12' : '24'));

/** The lists a task can be added to: only writable task lists (never a read-only share or a subscription). */
const listOptions = computed<Calendar[]>(() => tasksStore.writableTaskLists);

function listById(uuid: string): Calendar | undefined {
  return tasksStore.taskLists.find((c: Calendar) => c.uuid === uuid);
}
const listName = (uuid: string) =>
  listById(uuid)?.name ?? (props.task?.calendar_uuid === uuid ? props.task.calendar_name ?? '' : '');
const listColor = (uuid: string) =>
  listById(uuid)?.color ?? (props.task?.calendar_uuid === uuid ? props.task.calendar_color : undefined);

const recurrenceText = computed(() => describeRecurrence(props.task?.rrule ?? null));

const recurringDueChanged = computed(
  () =>
    recurring.value &&
    encodeTaskTime(form.due, form.allDay) !== encodeTaskTime(initial.due, initial.allDay),
);

/** The slider needs a number; "no progress" (null) sits at 0 until it is moved. */
const percentSlider = computed<number | number[]>({
  get: () => form.percent ?? 0,
  set: (value: number | number[]) => {
    form.percent = Array.isArray(value) ? value[0] ?? 0 : value;
  },
});

// All day off: a date becomes a date-time at the current hour on that day
// (like EventForm), not at midnight, which would mean "due the day before".
watch(
  () => form.allDay,
  (allDay: boolean, wasAllDay: boolean) => {
    if (!wasAllDay || allDay) return;
    const hour = new Date().getHours();
    for (const key of ['due', 'start'] as const) {
      const value = form[key];
      if (!value) continue;
      const next = new Date(value.getTime());
      next.setHours(hour, 0, 0, 0);
      form[key] = next;
    }
  },
);

// --- Validation (Vuelidate) -----------------------------------------------------
const rules = {
  summary: {
    required: helpers.withMessage('Enter a title for the task.', required),
    maxLength: helpers.withMessage(
      TASK_SUMMARY_TOO_LONG,
      (value: string) => summaryLength(value) <= TASK_SUMMARY_MAX,
    ),
  },
  listId: {
    required: helpers.withMessage('Choose the list to add the task to.', required),
  },
  due: {
    notBeforeStart: helpers.withMessage(
      'The due date is before the start. Pick a later due date or an earlier start.',
      (due: Date | null) => !isDueBeforeStart(due, form.start, form.allDay),
    ),
  },
};

const v$ = useVuelidate(rules, form);

function errorOf(field: 'summary' | 'listId' | 'due'): string {
  const state = v$.value[field];
  if (!state.$error) return '';
  return String(unref(state.$errors[0]?.$message) ?? '');
}

const titleError = computed(() => errorOf('summary'));
const listError = computed(() => errorOf('listId'));
const dueError = computed(() => errorOf('due'));

async function handleSubmit() {
  if (props.readOnly || props.isSubmitting) return;
  const valid = await v$.value.$validate();
  if (!valid) return;
  emit('submit', { ...form }, initial);
}

defineExpose({ form, initial, handleSubmit });
</script>
