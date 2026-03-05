<template>
  <li class="flex gap-4">
    <div
      class="flex-shrink-0 w-8 h-8 bg-primary-100 dark:bg-primary-900 text-primary-700 dark:text-primary-300 rounded-full flex items-center justify-center font-semibold text-sm"
    >
      {{ number }}
    </div>
    <div class="flex-1">
      <h4 class="font-medium text-surface-900 dark:text-surface-0 mb-1">{{ title }}</h4>
      <div class="text-sm text-surface-600 dark:text-surface-400">
        <slot />
      </div>
    </div>
  </li>
</template>

<script setup lang="ts">
defineProps<{
  number: number;
  title: string;
}>();
</script>
