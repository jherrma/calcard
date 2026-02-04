<template>
  <div class="flex items-center justify-between p-4 bg-surface-0 dark:bg-surface-900 border-b border-surface-200 dark:border-surface-800">
    <div class="flex items-center gap-2">
      <Button
        label="Today"
        severity="secondary"
        size="small"
        @click="$emit('today')"
      />
      <div class="flex">
        <Button
          icon="pi pi-chevron-left"
          severity="secondary"
          size="small"
          class="rounded-r-none"
          @click="$emit('prev')"
        />
        <Button
          icon="pi pi-chevron-right"
          severity="secondary"
          size="small"
          class="rounded-l-none border-l-0"
          @click="$emit('next')"
        />
      </div>
      <h2 class="text-lg font-semibold text-surface-900 dark:text-surface-0 ml-4">
        {{ formattedDate }}
      </h2>
    </div>

    <div class="flex items-center gap-2">
      <SelectButton
        :model-value="currentView"
        :options="viewOptions"
        option-label="label"
        option-value="value"
        @update:model-value="$emit('view-change', $event)"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
const props = defineProps<{
  currentDate: Date;
  currentView: string;
}>();

defineEmits<{
  today: [];
  prev: [];
  next: [];
  'view-change': [view: string];
  'date-change': [date: Date];
}>();

const viewOptions = [
  { label: 'Month', value: 'dayGridMonth' },
  { label: 'Week', value: 'timeGridWeek' },
  { label: 'Day', value: 'timeGridDay' },
];

const formattedDate = computed(() => {
  const options: Intl.DateTimeFormatOptions = {
    month: 'long',
    year: 'numeric',
  };

  if (props.currentView === 'timeGridDay') {
    options.day = 'numeric';
  }

  return props.currentDate.toLocaleDateString('en-US', options);
});
</script>
