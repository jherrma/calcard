<template>
  <div
    class="flex flex-col items-center px-6 py-16 text-center"
    :data-variant="variant"
    data-test="empty-state"
  >
    <!-- The two "nothing open" states echo the completion control: a hollow
         ring for an empty list, a ticked one when everything is done. -->
    <span
      v-if="variant === 'empty'"
      class="mb-4 flex h-10 w-10 items-center justify-center rounded-full border-2 border-dashed border-surface-300 dark:border-surface-600"
      aria-hidden="true"
      data-test="empty-mark"
    />
    <span
      v-else-if="variant === 'all-done'"
      class="mb-4 flex h-10 w-10 items-center justify-center rounded-full bg-primary-600 dark:bg-primary-400"
      aria-hidden="true"
      data-test="empty-mark"
    >
      <svg class="h-5 w-5 text-surface-0 dark:text-surface-900" viewBox="0 0 16 16" fill="none">
        <path d="M3.5 8.5l3 3 6-6.5" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
      </svg>
    </span>
    <i
      v-else
      class="pi mb-4 text-4xl text-surface-300 dark:text-surface-600"
      :class="variant === 'no-lists' ? 'pi-list-check' : 'pi-search'"
      aria-hidden="true"
      data-test="empty-mark"
    />

    <h2 class="text-base font-semibold text-surface-900 dark:text-surface-100" data-test="empty-heading">
      {{ copy.heading }}
    </h2>
    <p
      v-if="copy.text"
      class="mt-1 max-w-xs text-sm text-surface-500 dark:text-surface-400"
      data-test="empty-text"
    >
      {{ copy.text }}
    </p>

    <Button
      v-if="variant === 'no-lists'"
      class="mt-5"
      label="Create task list"
      icon="pi pi-plus"
      size="small"
      data-test="empty-action"
      @click="emit('create-list')"
    />
    <Button
      v-else-if="variant === 'no-match'"
      class="mt-5"
      label="Clear filters"
      icon="pi pi-filter-slash"
      severity="secondary"
      outlined
      size="small"
      data-test="empty-action"
      @click="emit('clear-filters')"
    />
    <button
      v-else-if="variant === 'all-done'"
      type="button"
      class="mt-3 rounded px-1 text-sm font-medium text-primary-700 underline-offset-2 hover:underline focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 dark:text-primary-300"
      data-test="empty-action"
      @click="emit('show-completed')"
    >
      Show completed
    </button>
  </div>
</template>

<script setup lang="ts">
import type { TaskEmptyVariant } from '~/types/tasks';

/**
 * The task list's empty states (#170, spec §7.5; DECISIONS 4: all four):
 *
 * - `no-lists`: the user has no calendar that holds tasks (e.g. an
 *   OAuth-provisioned account, spec §1a). "Create task list" emits
 *   `create-list` (the page opens AddTaskListDialog, story 27).
 * - `empty`: the list has no tasks. The quick-add above it takes the focus
 *   (story 26).
 * - `no-match`: the search or the Done filter hides everything. "Clear
 *   filters" emits `clear-filters`.
 * - `all-done`: Open shows nothing but completed tasks exist. "Show completed"
 *   emits `show-completed` (the Done filter).
 *
 * Presentational; the list decides which variant applies. The marks are
 * decorative (aria-hidden), so their low-contrast tokens are exempt.
 */
const props = defineProps<{
  variant: TaskEmptyVariant;
}>();

const emit = defineEmits<{
  'create-list': [];
  'clear-filters': [];
  'show-completed': [];
}>();

const COPY: Record<TaskEmptyVariant, { heading: string; text?: string }> = {
  'no-lists': {
    heading: 'No task lists yet',
    text: 'Create a list to start adding tasks. Task apps on your phone will find it too.',
  },
  empty: { heading: 'Nothing to do here' },
  'no-match': { heading: 'No tasks match' },
  'all-done': { heading: 'All caught up' },
};

const copy = computed(() => COPY[props.variant]);
</script>
