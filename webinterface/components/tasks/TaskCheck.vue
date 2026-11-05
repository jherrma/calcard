<template>
  <span class="relative flex h-6 w-6 shrink-0 items-center justify-center">
    <!-- A 24px layout box; the 44x44 button is centred over it, so the touch
         target overhangs into the row's padding and gap, never into the title.
         (Comments stay inside the root: one at the root makes a fragment.) -->
    <button
      v-tooltip.top="disabledReason ?? ''"
      type="button"
      role="checkbox"
      :aria-checked="checked ? 'true' : 'false'"
      :aria-label="`${checked ? 'Mark as not done' : 'Complete'}: ${taskTitle}`"
      :aria-disabled="isDisabled ? 'true' : undefined"
      :aria-describedby="isDisabled ? reasonId : undefined"
      :aria-busy="pending ? 'true' : undefined"
      :tabindex="tabbable ? undefined : -1"
      class="group absolute left-1/2 top-1/2 flex h-11 w-11 -translate-x-1/2 -translate-y-1/2 items-center justify-center rounded-full focus:outline-none"
      :class="isDisabled ? 'cursor-not-allowed' : 'cursor-pointer'"
      data-test="task-check"
      @click="onClick"
      @keydown="onKeydown"
      @keyup.space.prevent
    >
      <!-- Pending write: a small spinner stands in for the ring. -->
      <svg
        v-if="pending"
        class="h-5 w-5 text-primary-600 dark:text-primary-400 motion-safe:animate-spin"
        viewBox="0 0 24 24"
        fill="none"
        aria-hidden="true"
        data-test="check-spinner"
      >
        <circle cx="12" cy="12" r="9" stroke="currentColor" stroke-width="2.5" opacity="0.25" />
        <path d="M21 12a9 9 0 0 0-9-9" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" />
      </svg>

      <!-- The ring. On tick it fills and the check draws on (180ms, SVG
           stroke-dashoffset); instant under prefers-reduced-motion. The focus
           ring hugs the 24px circle rather than the larger hit area. -->
      <span
        v-else
        class="flex h-6 w-6 items-center justify-center rounded-full border-2 transition-colors duration-[180ms] ease-out motion-reduce:transition-none group-focus-visible:ring-2 group-focus-visible:ring-primary-500 group-focus-visible:ring-offset-2 group-focus-visible:ring-offset-surface-0 dark:group-focus-visible:ring-offset-surface-900"
        :class="ringClass"
        data-test="check-ring"
      >
        <svg class="h-3.5 w-3.5 text-surface-0 dark:text-surface-900" viewBox="0 0 16 16" fill="none" aria-hidden="true">
          <path
            d="M3.5 8.5l3 3 6-6.5"
            pathLength="1"
            stroke="currentColor"
            stroke-width="2.25"
            stroke-linecap="round"
            stroke-linejoin="round"
            class="transition-[stroke-dashoffset] duration-[180ms] ease-out motion-reduce:transition-none"
            :style="{ strokeDasharray: 1, strokeDashoffset: checked ? 0 : 1 }"
            data-test="check-mark"
          />
        </svg>
      </span>
    </button>
    <span v-if="isDisabled" :id="reasonId" class="sr-only" data-test="check-reason">{{ disabledReason }}</span>
  </span>
</template>

<script setup lang="ts">
/**
 * The completion control of a task row (#170, spec §7.3, §7.5, §8): a real
 * `<button role="checkbox">` labelled "Complete: <title>" or "Mark as not done:
 * <title>". Click, Space and Enter emit `toggle`; the row (and the list) decide
 * what that does.
 *
 * Disabled is `aria-disabled` plus a reason in `aria-describedby` (and the same
 * words as a tooltip), never only a grey look: the button stays focusable so a
 * screen reader can reach the reason. While `pending` a spinner replaces the
 * ring and nothing is emitted.
 *
 * The tick is the page's one animation: the ring fills with the accent and the
 * check strokes on over 180ms. A CSS transition, so it plays only when
 * `checked` changes, never when a row first renders already completed.
 */
const props = withDefaults(defineProps<{
  /** The task is completed (`task.completed`). */
  checked: boolean;
  /** The task's title, for the accessible name. */
  taskTitle: string;
  /** A write for this task is in flight: spinner, `aria-busy`, no `toggle`. */
  pending?: boolean;
  /** Why the task cannot be ticked here (repeating, read-only list). Set = disabled. */
  disabledReason?: string | null;
  /** In the Tab order (default). False = `tabindex="-1"`: the list's roving tabindex keeps only the active row's controls tabbable. */
  tabbable?: boolean;
}>(), {
  tabbable: true,
});

const emit = defineEmits<{
  toggle: [];
}>();

const reasonId = useId();

const isDisabled = computed(() => !!props.disabledReason);

// surface-500/400, not 400/500: surface-400 is 2.6:1 on a light ground and
// fails the 3:1 a control's boundary needs (WCAG 1.4.11).
const ringClass = computed(() => {
  if (isDisabled.value) {
    return props.checked
      ? 'border-surface-400 bg-surface-400 dark:border-surface-500 dark:bg-surface-500'
      : 'border-surface-300 dark:border-surface-600';
  }
  return props.checked
    ? 'border-primary-600 bg-primary-600 dark:border-primary-400 dark:bg-primary-400'
    : 'border-surface-500 dark:border-surface-400 group-hover:border-primary-600 dark:group-hover:border-primary-400';
});

function activate() {
  if (isDisabled.value || props.pending) return;
  emit('toggle');
}

function onClick() {
  activate();
}

/**
 * Space and Enter toggle explicitly. The default is prevented so the button's
 * own synthesized click (Enter on keydown, Space on keyup, the keyup is
 * prevented in the template) cannot toggle a second time; a held key's
 * auto-repeat is ignored.
 */
function onKeydown(event: KeyboardEvent) {
  if (event.key !== 'Enter' && event.key !== ' ') return;
  event.preventDefault();
  if (event.repeat) return;
  activate();
}
</script>
