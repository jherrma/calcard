<template>
  <Dialog
    :visible="visible"
    header="Create task list"
    :modal="true"
    :style="{ width: '28rem' }"
    :breakpoints="DIALOG_BREAKPOINTS"
    @update:visible="emit('update:visible', $event)"
    @after-hide="emit('after-hide')"
  >
    <div class="space-y-4">
      <div>
        <label :for="nameId" class="block text-sm font-medium text-surface-700 dark:text-surface-300 mb-1">Name</label>
        <InputText
          :id="nameId"
          v-model="form.name"
          class="w-full"
          autocomplete="off"
          autofocus
          data-test="list-name"
          @keydown.enter.prevent="create"
        />
      </div>

      <div>
        <label :for="colorId" class="block text-sm font-medium text-surface-700 dark:text-surface-300 mb-1">Color</label>
        <div class="flex items-center gap-3">
          <ColorPicker v-model="form.color" />
          <InputText
            :id="colorId"
            v-model="form.color"
            class="w-32 font-mono text-sm"
            :invalid="colorInvalid"
            :aria-describedby="colorInvalid ? colorErrorId : undefined"
            data-test="list-color"
            @keydown.enter.prevent="create"
          />
        </div>
        <p
          v-if="colorInvalid"
          :id="colorErrorId"
          class="mt-1 text-sm text-red-600 dark:text-red-400"
          data-test="color-error"
        >
          {{ COLOR_FORMAT_ERROR }}
        </p>
      </div>
    </div>

    <template #footer>
      <Button label="Cancel" severity="secondary" @click="emit('update:visible', false)" />
      <Button
        label="Create task list"
        :loading="isCreating"
        :disabled="!canCreate"
        data-test="create-list"
        @click="create"
      />
    </template>
  </Dialog>
</template>

<script setup lang="ts">
import { useTasksStore } from '~/stores/tasks';
import type { Calendar } from '~/types/calendar';

/**
 * "Create task list" (#170, spec §7.3): name + colour, then
 * `tasksStore.createTaskList` (`POST /calendars` with `kind: 'tasks'`, a
 * VTODO-only calendar that task apps on a phone list as a reminder list, then
 * a refresh of the calendars store). Pattern of the calendar section's
 * AddCalendarDialog, which stays as it is (it creates event calendars).
 * Opened from the Tasks sidebar's "+" and the "No task lists yet" state.
 * The store toasts a failure; the dialog then stays open with what was typed.
 */
const props = defineProps<{
  visible: boolean;
}>();

const emit = defineEmits<{
  'update:visible': [value: boolean];
  /** The new list, already in `tasksStore.taskLists`. */
  created: [list: Calendar];
  'after-hide': [];
}>();

const tasksStore = useTasksStore();
const toast = useAppToast();

const nameId = useId();
const colorId = useId();
const colorErrorId = useId();

const isCreating = ref(false);
const form = reactive({ name: '', color: '' });

/** Every opening starts empty, on a colour none of the lists has yet. */
function reset() {
  form.name = '';
  form.color = suggestTaskListColor(tasksStore.taskLists.map((l: Calendar) => l.color)).slice(1);
}

watch(() => props.visible, (open) => {
  if (open) reset();
}, { immediate: true });

const colorInvalid = computed(() => normalizeAccentColor(form.color) === null);
const canCreate = computed(() => form.name.trim() !== '' && !colorInvalid.value && !isCreating.value);

async function create() {
  const color = normalizeAccentColor(form.color);
  const name = form.name.trim();
  if (!canCreate.value || color === null) return;
  isCreating.value = true;
  try {
    const created = await tasksStore.createTaskList(name, color);
    if (!created) return;
    toast.success('Task list created');
    emit('created', created);
    emit('update:visible', false);
  } finally {
    isCreating.value = false;
  }
}
</script>
