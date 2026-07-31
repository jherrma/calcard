<template>
  <!-- Category header for a global-search result group (story 044). Badge is not
       in the PrimeVue include list, so the count is a plain pill. -->
  <div class="flex items-center gap-2 px-3 pt-2 pb-1">
    <span class="text-xs font-semibold uppercase tracking-wide text-surface-400">{{ label }}</span>
    <span class="text-[0.625rem] px-1.5 py-0.5 rounded-full bg-surface-100 dark:bg-surface-800 text-surface-500">
      {{ count }}
    </span>
  </div>
</template>

<script setup lang="ts">
defineProps<{
  label: string;
  count: number;
}>();
</script>
