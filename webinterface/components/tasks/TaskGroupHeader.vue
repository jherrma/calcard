<template>
  <h2
    class="sticky top-0 z-[1] flex min-h-11 items-center bg-surface-0 px-4 text-[13px] font-semibold leading-5 dark:bg-surface-900"
    :class="muted ? 'text-surface-500 dark:text-surface-400' : 'text-surface-900 dark:text-surface-100'"
    :data-muted="muted ? 'true' : undefined"
    data-test="group-header"
  >
    <button
      v-if="collapsible"
      type="button"
      class="-mx-2 flex min-h-11 items-center rounded-md px-2 hover:bg-surface-100 focus:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-primary-500 dark:hover:bg-surface-800"
      :aria-expanded="expanded ? 'true' : 'false'"
      :aria-controls="controls"
      data-test="group-toggle"
      @click="emit('toggle')"
    >
      <span>{{ label }}</span>
      <span class="sr-only">{{ ', ' }}</span>
      <span class="ms-2 font-normal tabular-nums text-surface-500 dark:text-surface-400" data-test="group-count">{{ count }}</span>
      <span class="sr-only">{{ countWord }}</span>
      <i
        class="pi ms-2 text-[11px] text-surface-500 dark:text-surface-400"
        :class="expanded ? 'pi-chevron-down' : 'pi-chevron-right'"
        aria-hidden="true"
        data-test="group-chevron"
      />
    </button>
    <template v-else>
      <span>{{ label }}</span>
      <span class="sr-only">{{ ', ' }}</span>
      <span class="ms-2 font-normal tabular-nums text-surface-500 dark:text-surface-400" data-test="group-count">{{ count }}</span>
      <span class="sr-only">{{ countWord }}</span>
    </template>
  </h2>
</template>

<script setup lang="ts">
/**
 * A status group's heading in the task list (#170, spec §7.4, §7.5, §8):
 * 13px/600 sentence case with the count in quiet ink, sticky at the top of the
 * list's scroll area while its group is on screen. The accessible name carries
 * the count ("Needs action, 9 tasks"); the visible text is "Needs action 9".
 * Collapsible groups (Completed, Cancelled) put the text in a
 * `<button aria-expanded aria-controls>`; the chevron trails the count so
 * every label starts on the same edge as the checks below.
 *
 * Presentational: the list owns the collapsed state and passes the id of the
 * `<ul>` the button controls. (No comment at the template root: it would make
 * the component a fragment and the list's `id` would not reach the `<h2>`.)
 */
const props = defineProps<{
  label: string;
  /** Rows in the group (the loaded rows; "Showing N of total" says when that is not everything). */
  count: number;
  /** Completed and Cancelled: the heading is a disclosure button. */
  collapsible?: boolean;
  /** Collapsible groups only: the rows are shown. */
  expanded?: boolean;
  /** Id of the group's `<ul>` (`aria-controls`). */
  controls?: string;
  /** Quiet ink for the heading (the Cancelled group). */
  muted?: boolean;
}>();

const emit = defineEmits<{
  toggle: [];
}>();

const countWord = computed(() => (props.count === 1 ? ' task' : ' tasks'));
</script>
