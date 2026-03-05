<template>
  <Dialog
    :visible="visible"
    header="Create Calendar"
    :modal="true"
    :style="{ width: '450px' }"
    @update:visible="$emit('update:visible', $event)"
  >
    <div class="space-y-4">
      <div>
        <label class="block text-sm font-medium text-surface-700 dark:text-surface-300 mb-1">Name *</label>
        <InputText v-model="form.name" class="w-full" placeholder="My Calendar" />
      </div>

      <div>
        <label class="block text-sm font-medium text-surface-700 dark:text-surface-300 mb-1">Color</label>
        <div class="flex items-center gap-3">
          <ColorPicker v-model="form.color" />
          <InputText v-model="form.color" class="w-32 font-mono text-sm" />
        </div>
      </div>

      <div>
        <label class="block text-sm font-medium text-surface-700 dark:text-surface-300 mb-1">Timezone</label>
        <Select
          v-model="form.timezone"
          :options="timezones"
          filter
          class="w-full"
        />
      </div>

      <div>
        <label class="block text-sm font-medium text-surface-700 dark:text-surface-300 mb-1">Description</label>
        <Textarea v-model="form.description" class="w-full" rows="2" />
      </div>
    </div>

    <template #footer>
      <Button label="Cancel" severity="secondary" @click="$emit('update:visible', false)" />
      <Button
        label="Create"
        :loading="isCreating"
        :disabled="!form.name.trim()"
        @click="create"
      />
    </template>
  </Dialog>
</template>

<script setup lang="ts">
import { useCalendarStore } from '~/stores/calendars';

defineProps<{
  visible: boolean;
}>();

const emit = defineEmits<{
  'update:visible': [value: boolean];
  created: [];
}>();

const calendarStore = useCalendarStore();
const toast = useAppToast();

const isCreating = ref(false);

const form = reactive({
  name: '',
  color: '3788d8',
  timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
  description: '',
});

const timezones = computed(() => Intl.supportedValuesOf('timeZone'));

const create = async () => {
  isCreating.value = true;
  try {
    await calendarStore.createCalendar({
      ...form,
      color: form.color.startsWith('#') ? form.color : `#${form.color}`,
    });
    toast.success('Calendar created');
    emit('created');
    emit('update:visible', false);
    form.name = '';
    form.description = '';
  } catch (e: unknown) {
    toast.error((e as Error).message || 'Failed to create calendar');
  } finally {
    isCreating.value = false;
  }
};
</script>
