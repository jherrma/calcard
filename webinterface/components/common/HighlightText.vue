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
  // Match against the RAW text and split on the (regex-escaped, never
  // HTML-escaped) term. Splitting with a capturing group yields the matched
  // substrings at the odd indices. Every piece — matched and unmatched alike —
  // is HTML-escaped AFTER the split, so the <mark> wrapper can never be injected
  // inside an HTML entity (which corrupted names containing & < > " '), and the
  // attacker-influenceable vCard text still can't smuggle in markup/script.
  const escapedTerm = props.highlight.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  const regex = new RegExp(`(${escapedTerm})`, 'gi');
  return props.text
    .split(regex)
    .map((part, i) =>
      i % 2 === 1
        ? `<mark class="bg-yellow-200 dark:bg-yellow-800 rounded px-0.5">${escapeHtml(part)}</mark>`
        : escapeHtml(part),
    )
    .join('');
});
</script>
