<template>
  <span v-if="highlight" v-html="highlighted" />
  <span v-else>{{ text }}</span>
</template>

<script setup lang="ts">
const props = defineProps<{
  text: string;
  highlight?: string;
}>();

function escapeHtml(s: string): string {
  return s
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

const highlighted = computed(() => {
  if (!props.highlight || !props.text) return props.text;
  // HTML-escape the (attacker-influenceable) contact text BEFORE injecting the
  // <mark> wrapper, so vendored vCard data can't smuggle in markup/script.
  const safeText = escapeHtml(props.text);
  const escapedTerm = escapeHtml(props.highlight).replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  const regex = new RegExp(`(${escapedTerm})`, 'gi');
  return safeText.replace(regex, '<mark class="bg-yellow-200 dark:bg-yellow-800 rounded px-0.5">$1</mark>');
});
</script>
