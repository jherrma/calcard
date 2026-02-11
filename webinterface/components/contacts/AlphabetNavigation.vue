<template>
  <div class="flex flex-col items-center justify-center gap-0.5 py-2 px-1 text-xs select-none">
    <button
      v-for="letter in alphabet"
      :key="letter"
      class="w-5 h-5 flex items-center justify-center rounded-sm transition-colors"
      :class="
        availableLetters.includes(letter)
          ? 'text-primary-600 dark:text-primary-400 hover:bg-primary-100 dark:hover:bg-primary-900/30 cursor-pointer font-semibold'
          : 'text-surface-300 dark:text-surface-600 cursor-default'
      "
      :disabled="!availableLetters.includes(letter)"
      @click="availableLetters.includes(letter) && $emit('scroll-to', letter)"
    >
      {{ letter }}
    </button>
  </div>
</template>

<script setup lang="ts">
defineProps<{
  availableLetters: string[];
}>();

defineEmits<{
  'scroll-to': [letter: string];
}>();

const alphabet = ['#', ...'ABCDEFGHIJKLMNOPQRSTUVWXYZ'.split('')];
</script>
