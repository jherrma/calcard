<template>
  <span v-if="highlight" v-html="highlighted" />
  <span v-else>{{ text }}</span>
</template>

<script setup lang="ts">
const props = defineProps<{
  text: string;
  highlight?: string;
}>();

const highlighted = computed(() => {
  if (!props.highlight || !props.text) return props.text;
  const escaped = props.highlight.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  const regex = new RegExp(`(${escaped})`, 'gi');
  return props.text.replace(regex, '<mark class="bg-yellow-200 dark:bg-yellow-800 rounded px-0.5">$1</mark>');
});
</script>
