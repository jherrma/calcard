<template>
  <CalendarSettingsDialog
    ref="settings"
    :visible="visible"
    :calendar="list"
    :kind="list?.kind ?? 'tasks'"
    :initial-tab="initialTab"
    @update:visible="emit('update:visible', $event)"
    @updated="emit('updated')"
    @deleted="emit('deleted', $event)"
  />
</template>

<script setup lang="ts">
import CalendarSettingsDialog from '~/components/calendar/CalendarSettingsDialog.vue';
import type { Calendar } from '~/types/calendar';

/**
 * Rename, recolour, share or delete a task list (#170, spec §7.3): the
 * calendar settings dialog with the task list's `kind`, which swaps the copy
 * ("Delete task list" and its permanent-deletion warning) and hides Timezone
 * and Public Access. Opened from the Tasks sidebar's list menu, owner-only.
 */
defineProps<{
  visible: boolean;
  /** The task list (an owned `kind: 'tasks'` calendar); null before one was chosen. */
  list: Calendar | null;
  /** `general` (rename, colour) or `sharing`. */
  initialTab?: string;
}>();

const emit = defineEmits<{
  'update:visible': [value: boolean];
  /** Saved, or its shares changed. */
  updated: [];
  /** Deleted on the server (and dropped from the calendars store). */
  deleted: [list: Calendar];
}>();

const settings = ref<InstanceType<typeof CalendarSettingsDialog> | null>(null);

defineExpose({
  /** Asks "Delete task list?" for `list` and deletes it on accept, without opening the dialog. */
  confirmDelete: (list: Calendar) => settings.value?.confirmDelete(list),
});
</script>
