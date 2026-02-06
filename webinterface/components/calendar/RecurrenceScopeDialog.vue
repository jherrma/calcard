<template>
  <Dialog
    :visible="visible"
    :header="action === 'delete' ? 'Delete Recurring Event' : 'Edit Recurring Event'"
    modal
    :closable="true"
    :style="{ width: '28rem' }"
    @update:visible="$emit('update:visible', $event)"
  >
    <p class="text-surface-600 dark:text-surface-400 mb-4">
      {{ action === 'delete' ? 'How would you like to delete this event?' : 'How would you like to edit this event?' }}
    </p>

    <div class="flex flex-col gap-3">
      <div class="flex items-center gap-2">
        <RadioButton v-model="selectedScope" input-id="scope-this" value="this" />
        <label for="scope-this" class="text-sm text-surface-700 dark:text-surface-300 cursor-pointer">This event only</label>
      </div>
      <div class="flex items-center gap-2">
        <RadioButton v-model="selectedScope" input-id="scope-future" value="this_and_future" />
        <label for="scope-future" class="text-sm text-surface-700 dark:text-surface-300 cursor-pointer">This and future events</label>
      </div>
      <div class="flex items-center gap-2">
        <RadioButton v-model="selectedScope" input-id="scope-all" value="all" />
        <label for="scope-all" class="text-sm text-surface-700 dark:text-surface-300 cursor-pointer">All events in the series</label>
      </div>
    </div>

    <template #footer>
      <div class="flex justify-end gap-2">
        <Button label="Cancel" text @click="$emit('cancel')" />
        <Button
          :label="action === 'delete' ? 'Delete' : 'Save'"
          :severity="action === 'delete' ? 'danger' : undefined"
          @click="$emit('confirm', selectedScope)"
        />
      </div>
    </template>
  </Dialog>
</template>

<script setup lang="ts">
defineProps<{
  visible: boolean;
  action: 'edit' | 'delete';
}>();

defineEmits<{
  'update:visible': [value: boolean];
  'confirm': [scope: string];
  'cancel': [];
}>();

const selectedScope = ref('this');
</script>
