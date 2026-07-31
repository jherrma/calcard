<template>
  <!-- "View all" leaves the capped palette for the full results page
       (/search?q=…), which renders every match in every category (story 044). -->
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
