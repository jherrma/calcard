<template>
  <div class="space-y-3">
    <div
      v-for="i in count"
      :key="i"
      class="animate-pulse"
    >
      <div class="flex items-center gap-4 p-4 bg-surface-0 dark:bg-surface-900 rounded-lg border border-surface-200 dark:border-surface-800">
        <div class="w-10 h-10 bg-surface-200 dark:bg-surface-700 rounded-full" />
        <div class="flex-1 space-y-2">
          <div class="h-4 bg-surface-200 dark:bg-surface-700 rounded w-3/4" />
          <div class="h-3 bg-surface-200 dark:bg-surface-700 rounded w-1/2" />
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
withDefaults(defineProps<{
  count?: number;
}>(), {
  count: 5,
});
</script>
