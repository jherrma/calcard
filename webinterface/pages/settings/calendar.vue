<template>
  <div>
    <h2 class="text-2xl font-bold text-surface-900 dark:text-surface-0 mb-6">Calendar</h2>

    <CommonLoadingSpinner v-if="loading" />

    <div
      v-else
      class="bg-surface-0 dark:bg-surface-900 rounded-xl border border-surface-200 dark:border-surface-800 p-6"
    >
      <form class="space-y-6" @submit.prevent="handleSave">
        <!-- Default event duration -->
        <div class="flex flex-col gap-2">
          <label for="pref-duration" class="text-sm font-medium text-surface-700 dark:text-surface-300">
            Default event duration
          </label>
          <Select
            id="pref-duration"
            v-model="form.duration"
            :options="durationOptions"
            option-label="label"
            option-value="value"
            class="w-full sm:w-64"
          />
          <small class="text-surface-500">How long a new event lasts when you create one.</small>
        </div>

        <!-- Default all-day -->
        <div class="flex flex-col gap-2">
          <div class="flex items-center gap-3">
            <InputSwitch v-model="form.allDay" input-id="pref-all-day" />
            <label for="pref-all-day" class="text-sm font-medium text-surface-700 dark:text-surface-300 cursor-pointer">
              Create new events as all-day
            </label>
          </div>
          <small class="text-surface-500">
            New events start with the all-day switch on. Dragging a time range in the week view still wins.
          </small>
        </div>

        <!-- Time format -->
        <div class="flex flex-col gap-2">
          <label id="pref-time-format-label" class="text-sm font-medium text-surface-700 dark:text-surface-300">
            Time format
          </label>
          <SelectButton
            v-model="form.timeFormat"
            :options="timeFormatOptions"
            option-label="label"
            option-value="value"
            :allow-empty="false"
            aria-labelledby="pref-time-format-label"
          />
          <small class="text-surface-500">Applies to the calendar grid, event details and the time pickers.</small>
        </div>

        <div class="flex items-center gap-3 pt-2">
          <Button
            type="submit"
            label="Save Changes"
            icon="pi pi-check"
            :loading="saving"
            :disabled="!isDirty"
          />
          <Button
            v-if="isDirty"
            type="button"
            label="Reset"
            severity="secondary"
            text
            @click="applyStoreValues"
          />
        </div>
      </form>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { TimeFormat } from '~/types/settings';
import {
  usePreferencesStore,
  EVENT_DURATION_OPTIONS,
  PREF_DEFAULT_ALL_DAY,
  PREF_DEFAULT_EVENT_DURATION,
  PREF_TIME_FORMAT,
} from '~/stores/preferences';

definePageMeta({
  layout: 'settings',
  middleware: 'auth',
});

const preferencesStore = usePreferencesStore();
const toast = useAppToast();

const loading = ref(true);
const saving = ref(false);

const durationOptions = EVENT_DURATION_OPTIONS;
// Sample times make the difference concrete instead of making the user guess
// what "24-hour" renders as.
const timeFormatOptions: { label: string; value: TimeFormat }[] = [
  { label: '12-hour (1:00 PM)', value: '12h' },
  { label: '24-hour (13:00)', value: '24h' },
];

const form = reactive({
  duration: preferencesStore.defaultEventDuration,
  allDay: preferencesStore.defaultAllDay,
  timeFormat: preferencesStore.timeFormat as TimeFormat,
});

const applyStoreValues = () => {
  form.duration = preferencesStore.defaultEventDuration;
  form.allDay = preferencesStore.defaultAllDay;
  form.timeFormat = preferencesStore.timeFormat;
};

const isDirty = computed(() =>
  form.duration !== preferencesStore.defaultEventDuration
  || form.allDay !== preferencesStore.defaultAllDay
  || form.timeFormat !== preferencesStore.timeFormat,
);

const handleSave = async () => {
  saving.value = true;
  try {
    // Send all three keys, not just the changed ones: the endpoint upserts, so a
    // full payload also repairs a row left behind by an older build.
    await preferencesStore.updatePreferences({
      [PREF_DEFAULT_EVENT_DURATION]: String(form.duration),
      [PREF_DEFAULT_ALL_DAY]: String(form.allDay),
      [PREF_TIME_FORMAT]: form.timeFormat,
    });
    // Re-read from the store: the server response is authoritative.
    applyStoreValues();
    toast.success('Calendar preferences saved');
  } catch (e: any) {
    toast.error(e?.data?.message || 'Failed to save preferences');
  } finally {
    saving.value = false;
  }
};

onMounted(async () => {
  await preferencesStore.ensureLoaded();
  if (preferencesStore.error) {
    toast.error(preferencesStore.error);
  }
  applyStoreValues();
  loading.value = false;
});
</script>
