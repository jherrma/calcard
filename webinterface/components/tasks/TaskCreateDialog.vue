<template>
  <Dialog
    :visible="visible"
    header="Add a task"
    modal
    :closable="true"
    :style="{ width: '36rem' }"
    :breakpoints="DIALOG_BREAKPOINTS"
    data-test="task-create-dialog"
    @update:visible="emit('update:visible', $event)"
  >
    <TaskForm
      :default-list-id="defaultListId"
      :initial-summary="initialSummary"
      :is-submitting="isSubmitting"
      @submit="handleSubmit"
      @cancel="emit('update:visible', false)"
    />
  </Dialog>
</template>

<script setup lang="ts">
import TaskForm from '~/components/tasks/TaskForm.vue';
import { useTasksStore } from '~/stores/tasks';
import type { Task, TaskFormData } from '~/types/tasks';
import { buildTaskInput } from '~/utils/taskForm';

/**
 * "Add a task" (#170, spec §7.3): TaskForm in a dialog, like
 * EventCreateDialog. Full width on phones. Save creates the task in the
 * chosen list (`createTask`); on success the dialog closes and emits
 * `created`, on failure it stays open with everything typed (the store has
 * toasted what failed). Focus is trapped while open and goes back to what
 * opened it (PrimeVue Dialog).
 */
defineProps<{
  visible: boolean;
  /** The list preselected in the form (`tasksStore.newTaskListId`). */
  defaultListId?: string | null;
  /** The title typed into the quick-add, if the dialog was opened from there. */
  initialSummary?: string;
}>();

const emit = defineEmits<{
  'update:visible': [value: boolean];
  created: [task: Task];
}>();

const tasksStore = useTasksStore();
const isSubmitting = ref(false);

async function handleSubmit(form: TaskFormData) {
  if (isSubmitting.value) return;
  isSubmitting.value = true;
  try {
    const task = await tasksStore.createTask(form.listId, buildTaskInput(form));
    if (!task) return;
    emit('update:visible', false);
    emit('created', task);
  } finally {
    isSubmitting.value = false;
  }
}
</script>
