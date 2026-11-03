<template>
  <Dialog
    :visible="visible"
    :header="readOnly ? 'Task details' : 'Edit task'"
    modal
    :closable="true"
    :style="{ width: '36rem' }"
    :breakpoints="DIALOG_BREAKPOINTS"
    data-test="task-edit-dialog"
    @update:visible="emit('update:visible', $event)"
    @after-hide="emit('after-hide')"
  >
    <TaskForm
      v-if="task"
      :key="task.id"
      :task="task"
      :read-only="readOnly"
      :is-submitting="isSubmitting"
      @submit="handleSubmit"
      @cancel="emit('update:visible', false)"
      @delete="handleDelete"
    />
  </Dialog>
</template>

<script setup lang="ts">
import TaskForm from '~/components/tasks/TaskForm.vue';
import { useTasksStore } from '~/stores/tasks';
import type { Task, TaskFormData } from '~/types/tasks';
import { buildTaskPatch, isEmptyTaskPatch } from '~/utils/taskForm';

/**
 * Edit a task (#170, spec §7.3): TaskForm in a dialog, like EventEditDialog.
 * Full width on phones.
 *
 * - Save sends only the fields that changed (`buildTaskPatch`); a form saved
 *   without a change sends nothing and just closes. On a failed save the
 *   dialog stays open with the edits (the store has toasted what failed).
 * - A task in a read-only list (read-only share, subscription) opens read
 *   only: "Task details", no Save, no Delete.
 * - "Delete task" asks first (`tasksStore.deleteTask`, the confirm dialog);
 *   the delete is permanent on the server, so there is no Undo. Once deleted
 *   the dialog closes with a "Task deleted" toast.
 *
 * Focus is trapped while open and goes back to what opened it (PrimeVue
 * Dialog); `after-hide` lets the page put it on the task's row when that row
 * was re-rendered meanwhile (a saved status moves it to another group).
 */
const props = defineProps<{
  visible: boolean;
  task: Task | null;
  /** The task's list is read-only to the user. */
  readOnly?: boolean;
}>();

const emit = defineEmits<{
  'update:visible': [value: boolean];
  /** The dialog has finished closing (after its leave transition). */
  'after-hide': [];
  saved: [task: Task];
  deleted: [task: Task];
}>();

const DIALOG_BREAKPOINTS = { '640px': 'calc(100vw - 2rem)' };

const tasksStore = useTasksStore();
const toast = useAppToast();
const isSubmitting = ref(false);

async function handleSubmit(data: TaskFormData, initial: TaskFormData) {
  const task = props.task;
  if (!task || props.readOnly || isSubmitting.value) return;
  const patch = buildTaskPatch(initial, data, task.is_recurring);
  if (isEmptyTaskPatch(patch)) {
    emit('update:visible', false);
    return;
  }
  isSubmitting.value = true;
  try {
    const saved = await tasksStore.updateTask(task, patch);
    if (!saved) return;
    emit('update:visible', false);
    emit('saved', saved);
  } finally {
    isSubmitting.value = false;
  }
}

async function handleDelete() {
  const task = props.task;
  if (!task || props.readOnly) return;
  const deleted = await tasksStore.deleteTask(task);
  if (!deleted) return;
  toast.success('Task deleted');
  emit('update:visible', false);
  emit('deleted', task);
}
</script>
