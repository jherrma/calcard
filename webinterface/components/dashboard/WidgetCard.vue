<template>
  <section
    class="flex flex-col rounded-2xl border border-surface-200 dark:border-surface-800 bg-surface-0 dark:bg-surface-900 shadow-sm"
  >
    <header
      class="flex items-center justify-between gap-3 px-5 py-3.5 border-b border-surface-200 dark:border-surface-800"
    >
      <div class="flex items-center gap-2 min-w-0">
        <i v-if="icon" :class="[icon, 'text-primary-600 dark:text-primary-400']" />
        <h2 class="text-sm font-semibold text-surface-900 dark:text-surface-100 truncate">
          {{ title }}
        </h2>
        <span
          v-if="badge !== undefined && badge !== null"
          class="bg-surface-100 dark:bg-surface-800 text-surface-600 dark:text-surface-300 text-xs px-2 py-0.5 rounded-full font-semibold tabular-nums"
        >
          {{ badge }}
        </span>
      </div>
      <div class="flex items-center gap-1 flex-shrink-0">
        <slot name="actions" />
      </div>
    </header>

    <div class="flex-1 p-4">
      <slot />
    </div>

    <footer
      v-if="$slots.footer"
      class="px-5 py-3 border-t border-surface-200 dark:border-surface-800 text-xs text-surface-500 dark:text-surface-400"
    >
      <slot name="footer" />
    </footer>
  </section>
</template>

<script setup lang="ts">
// Shared shell for the dashboard widgets (story 042) so every card gets the same
// header/padding/border treatment. Deliberately a plain element rather than
// PrimeVue's <Card>: the widgets need a header row with inline actions and a
// flex body that fills the grid cell, which Card's fixed slot structure fights.
defineProps<{
  title: string;
  /** PrimeIcons class, e.g. `pi pi-clock`. */
  icon?: string;
  /** Optional count pill next to the title. */
  badge?: number | string | null;
}>();
</script>
