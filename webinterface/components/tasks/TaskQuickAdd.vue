<template>
  <div
    v-if="targetList"
    class="relative flex min-h-12 items-center gap-x-3 border-b border-surface-200 bg-surface-0 px-4 py-2 dark:border-surface-800 dark:bg-surface-900"
    :aria-busy="saving ? 'true' : undefined"
    data-test="quick-add"
  >
    <!-- Sits on the rows' check column: a dashed ring with a plus is a task
         that does not exist yet (the empty state uses the same dashed ring). -->
    <span
      class="flex h-6 w-6 shrink-0 items-center justify-center rounded-full border-2 border-dashed border-surface-400 text-surface-500 dark:border-surface-500 dark:text-surface-400"
      aria-hidden="true"
      data-test="quick-add-mark"
    >
      <svg v-if="saving" class="h-3.5 w-3.5 motion-safe:animate-spin" viewBox="0 0 16 16" fill="none" data-test="quick-add-spinner">
        <circle cx="8" cy="8" r="6" stroke="currentColor" stroke-width="2" stroke-opacity="0.3" />
        <path d="M14 8a6 6 0 0 0-6-6" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
      </svg>
      <i v-else class="pi pi-plus text-[10px]" />
    </span>

    <input
      ref="input"
      v-model="text"
      type="text"
      class="peer min-w-0 flex-1 bg-transparent text-[15px] leading-[1.375rem] text-surface-900 placeholder:text-surface-500 focus:outline-none dark:text-surface-100 dark:placeholder:text-surface-400"
      placeholder="Add a task"
      aria-label="Add a task"
      :aria-describedby="showList ? targetId : undefined"
      autocomplete="off"
      enterkeyhint="done"
      data-test="quick-add-input"
      @keydown.enter="onEnter"
    >
    <!-- Focus: the same inset ring a focused row shows (spec §8, ring-2
         ring-primary-500), drawn around the whole line while the box has focus. -->
    <span
      class="pointer-events-none absolute inset-0 opacity-0 ring-2 ring-inset ring-primary-500 peer-focus:opacity-100"
      aria-hidden="true"
      data-test="quick-add-focus"
    />

    <!-- Under All tasks the list a new task lands in is not obvious: name it. -->
    <span
      v-if="showList"
      :id="targetId"
      class="flex max-w-[40%] shrink-0 items-center gap-1.5 text-[13px] leading-[1.375rem] text-surface-500 dark:text-surface-400"
      data-test="quick-add-list"
    >
      <span class="sr-only">{{ 'Adds to ' }}</span>
      <span
        class="h-2 w-2 shrink-0 rounded-full"
        :style="{ backgroundColor: targetList.color }"
        aria-hidden="true"
      />
      <span class="truncate">{{ targetList.name }}</span>
    </span>

    <button
      v-tooltip.top="'Add with details'"
      type="button"
      class="-my-1 flex h-8 w-8 shrink-0 items-center justify-center rounded-full text-surface-500 hover:bg-surface-100 hover:text-surface-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 dark:text-surface-400 dark:hover:bg-surface-800 dark:hover:text-surface-200"
      aria-label="Add a task with details"
      data-test="quick-add-details"
      @click="emit('open-details', text)"
    >
      <i class="pi pi-pen-to-square text-sm" aria-hidden="true" />
    </button>
  </div>
</template>

<script setup lang="ts">
import { useTasksStore } from '~/stores/tasks';
import type { Calendar } from '~/types/calendar';
import type { Task } from '~/types/tasks';

/**
 * The one-line add at the top of the task list (#170, spec §7.3, §7.5):
 * type a title and press Enter, and the task is created in the selected list,
 * or under All tasks in the first list the user may write to
 * (`tasksStore.newTaskListId`). A trailing "tomorrow" or "!high" stays part of
 * the title: there is no shorthand parsing in v1.
 *
 * Absent when there is nowhere to add to: a read-only share or a subscription
 * is selected, or no task list is writable.
 *
 * Enter with a blank title does nothing. On success the box is cleared (and
 * keeps the focus for the next task); on failure the text stays, so nothing
 * typed is lost (the store has already toasted the reason). The pen button
 * opens the full "Add a task" dialog with what was typed (`open-details`).
 */
const emit = defineEmits<{
  /** A task was created (the store already put it into the list when it belongs to the view). */
  created: [task: Task];
  /** "Add a task with details": the page opens the dialog with this title. */
  'open-details': [summary: string];
}>();

const tasksStore = useTasksStore();
const input = ref<HTMLInputElement | null>(null);
const text = ref('');
const saving = ref(false);
const targetId = useId();

const targetList = computed<Calendar | null>(() => {
  const id = tasksStore.newTaskListId;
  return id === null ? null : tasksStore.taskLists.find((c: Calendar) => c.uuid === id) ?? null;
});

/** All tasks: name the list the task goes to (a single list's name is the view title). */
const showList = computed(() => tasksStore.selectedListId === null);

async function onEnter(event: KeyboardEvent) {
  // Enter that confirms an IME composition (Japanese, Chinese, …) is not a submit.
  if (event.isComposing || event.keyCode === 229) return;
  event.preventDefault();
  const list = targetList.value;
  const summary = text.value.trim();
  if (saving.value || !list || summary === '') return;

  saving.value = true;
  try {
    const task = await tasksStore.createTask(list.uuid, { summary });
    if (!task) return;
    // Clear only what was sent: text typed while the request ran stays.
    if (text.value.trim() === summary) text.value = '';
    emit('created', task);
  } finally {
    saving.value = false;
  }
}

defineExpose({
  /** Puts the cursor into the box (the "Nothing to do here" state). */
  focus: () => input.value?.focus(),
  /** Empties the box (the dialog opened from it created the task). */
  clear: () => {
    text.value = '';
  },
});
</script>
