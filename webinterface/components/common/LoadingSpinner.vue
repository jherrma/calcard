<template>
  <div class="flex items-center justify-center" :class="containerClass">
    <ProgressSpinner
      :style="{ width: size, height: size }"
      stroke-width="4"
    />
  </div>
</template>

<script setup lang="ts">
const props = withDefaults(defineProps<{
  size?: string;
  fullPage?: boolean;
}>(), {
  size: '50px',
  fullPage: false,
});

const containerClass = computed(() => {
  return props.fullPage ? 'fixed inset-0 bg-surface-0/50 dark:bg-surface-950/50 z-50 backdrop-blur-sm' : '';
});
</script>
