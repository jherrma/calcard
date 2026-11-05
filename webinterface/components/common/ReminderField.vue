<template>
  <div class="flex flex-col gap-2" data-test="reminder-field">
    <ul
      v-if="items.length > 0"
      aria-label="Reminders"
      class="flex flex-wrap gap-2 list-none m-0 p-0"
      data-test="reminder-list"
    >
      <li
        v-for="item in items"
        :key="`${item.key}-${item.reminder.index}`"
        class="inline-flex items-center gap-1 rounded-full border border-surface-200 dark:border-surface-700 bg-surface-50 dark:bg-surface-800 pl-3 pr-1 py-1 text-sm text-surface-700 dark:text-surface-200"
        :data-test="item.reminder.editable ? 'reminder-chip' : 'reminder-chip-readonly'"
      >
        <i class="pi pi-bell text-xs text-surface-500 dark:text-surface-400" aria-hidden="true" />
        <span>{{ item.label }}</span>
        <span
          v-if="!item.reminder.editable"
          class="text-xs text-surface-500 dark:text-surface-400"
          data-test="reminder-foreign"
        >Set in another app</span>
        <button
          v-else-if="!readOnly"
          type="button"
          class="inline-flex items-center justify-center w-6 h-6 rounded-full text-surface-500 dark:text-surface-400 hover:bg-surface-200 dark:hover:bg-surface-700 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500"
          :aria-label="`Remove reminder: ${item.label}`"
          data-test="reminder-remove"
          @click="remove(item.reminder)"
        >
          <i class="pi pi-times text-xs" aria-hidden="true" />
        </button>
        <span v-else class="pr-2" />
      </li>
    </ul>
    <p
      v-else-if="readOnly"
      class="text-sm text-surface-500 dark:text-surface-400 m-0"
      data-test="reminder-none"
    >No reminders</p>

    <template v-if="!readOnly">
      <p
        v-if="disabled"
        class="text-sm text-surface-500 dark:text-surface-400 m-0"
        data-test="reminder-disabled-hint"
      >{{ disabledHint }}</p>
      <p
        v-if="atCap && !disabled"
        class="text-xs text-surface-500 dark:text-surface-400 m-0"
        data-test="reminder-cap"
      >At most {{ MAX_EDITABLE_REMINDERS }} reminders.</p>
      <template v-if="disabled || !atCap">
        <Select
          v-model="picked"
          :options="presetOptions"
          option-label="label"
          option-value="value"
          placeholder="Add reminder"
          aria-label="Add reminder"
          :disabled="disabled"
          class="w-full sm:w-64"
          data-test="reminder-add"
          @update:model-value="onPick"
        />

        <div
          v-if="customOpen && !disabled"
          class="flex flex-wrap items-center gap-2"
          data-test="reminder-custom"
        >
          <template v-if="allDay">
            <InputNumber v-model="customDays" :min="0" :max="999" input-class="w-16" aria-label="Days before" />
            <span class="text-sm text-surface-600 dark:text-surface-400">days before at</span>
            <InputNumber v-model="customHour" :min="0" :max="23" input-class="w-14" aria-label="Hour" />
            <span class="text-sm text-surface-600 dark:text-surface-400">:</span>
            <InputNumber v-model="customMinute" :min="0" :max="59" input-class="w-14" aria-label="Minute" />
          </template>
          <template v-else>
            <InputNumber v-model="customAmount" :min="1" :max="999" input-class="w-16" aria-label="Amount" />
            <Select
              v-model="customUnit"
              :options="CUSTOM_UNITS"
              option-label="label"
              option-value="value"
              aria-label="Unit"
            />
            <span class="text-sm text-surface-600 dark:text-surface-400">{{ target === 'task' ? 'before due' : 'before' }}</span>
          </template>
          <Button type="button" label="Add" size="small" data-test="reminder-custom-add" @click="addCustom" />
          <Button type="button" label="Cancel" size="small" text @click="customOpen = false" />
        </div>
      </template>
    </template>
  </div>
</template>

<script setup lang="ts">
import type { Reminder } from '~/types/calendar';
import type { CustomUnit, ReminderTarget } from '~/utils/reminders';

// The reminder chips and the "Add reminder" menu of the event form (#169, spec §10.9). v-model is the full list of
// the item's reminders as the API answers them (editable and not): a read-only one is shown and carried along, never
// sent. New ones are created relative (D23).
const props = withDefaults(defineProps<{
  modelValue: Reminder[];
  allDay?: boolean;
  readOnly?: boolean;
  /** `task`: relative to the due date (task presets and wording, end-related new reminders). */
  target?: ReminderTarget;
  /** Chips stay, adding is off (a task without a due date). */
  disabled?: boolean;
  /** Shown under the chips while `disabled`. */
  disabledHint?: string;
}>(), { allDay: false, readOnly: false, target: 'event', disabled: false, disabledHint: '' });

const emit = defineEmits<{ 'update:modelValue': [value: Reminder[]] }>();

const CUSTOM = 'custom';

const items = computed(() =>
  props.modelValue.map((reminder) => ({
    reminder,
    key: reminderKey(reminder),
    label: describeReminder(reminder, props.allDay, props.target),
  })),
);

const editableCount = computed(() => editableReminders(props.modelValue).length);
const atCap = computed(() => editableCount.value >= MAX_EDITABLE_REMINDERS);

const presetOptions = computed(() => [
  ...reminderPresets(props.allDay, props.target).map((p) => ({ label: p.label, value: p.offset as number | string })),
  { label: 'Custom…', value: CUSTOM },
]);

const picked = ref<number | string | null>(null);
const customOpen = ref(false);
const customAmount = ref<number | null>(30);
const customUnit = ref<CustomUnit>('minutes');
const customDays = ref<number | null>(1);
const customHour = ref<number | null>(9);
const customMinute = ref<number | null>(0);

// A switch between all-day and timed makes an open custom form's inputs meaningless.
watch(() => props.allDay, () => {
  customOpen.value = false;
});

function add(offset: number) {
  if (atCap.value) return;
  // normalizeReminders merges a duplicate, so adding the same offset twice changes nothing.
  emit('update:modelValue', normalizeReminders([...props.modelValue, offsetReminder(offset, props.target)]));
}

function onPick(value: number | string | null) {
  if (value === null) return;
  if (value === CUSTOM) {
    customOpen.value = true;
  } else {
    add(value as number);
  }
  // The menu is an action, not a selection: show the placeholder again.
  picked.value = null;
}

function addCustom() {
  if (props.allDay) {
    add(customAllDayOffset(customDays.value ?? 0, customHour.value ?? 0, customMinute.value ?? 0));
  } else {
    add(customTimedOffset(customAmount.value ?? 1, customUnit.value));
  }
  customOpen.value = false;
}

function remove(reminder: Reminder) {
  if (!reminder.editable) return;
  emit('update:modelValue', props.modelValue.filter((r) => r !== reminder));
}
</script>
