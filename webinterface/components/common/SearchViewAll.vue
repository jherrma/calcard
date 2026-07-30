<template>
  <!-- "View all" expands a truncated global-search category in place. There is no
       full-results page (no /api/v1/search endpoint to back one), so expanding the
       already-fetched result set is the honest affordance. -->
  <button
    class="w-full px-3 py-1.5 text-xs font-medium text-left text-primary-600 dark:text-primary-400 hover:underline"
    @click="$emit('click')"
  >
    {{ label }}
  </button>
</template>

<script setup lang="ts">
defineProps<{
  label: string;
}>();

defineEmits<{
  click: [];
}>();
</script>
