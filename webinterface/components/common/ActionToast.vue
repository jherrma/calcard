<template>
  <Toast
    :group="ACTION_TOAST_GROUP"
    position="bottom-center"
    :breakpoints="{ '640px': { width: 'calc(100vw - 2rem)' } }"
  >
    <template #message="{ message }">
      <div class="flex min-w-0 flex-1 items-center gap-3" data-test="action-toast">
        <span class="min-w-0 flex-1 text-sm font-medium" data-test="action-toast-summary">{{ message.summary }}</span>
        <button
          type="button"
          class="-my-1 shrink-0 rounded-md px-2 py-1 text-sm font-semibold text-primary-300 hover:bg-white/10 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-300 dark:text-primary-700 dark:hover:bg-surface-100 dark:focus-visible:ring-primary-700"
          data-test="action-toast-button"
          @click="run(message)"
        >
          {{ message.actionLabel }}
        </button>
      </div>
    </template>
  </Toast>
</template>

<script setup lang="ts">
/**
 * The action toasts of `useAppToast().action()`: a notice with one button
 * ("Task completed" + "Undo"). Bottom centre in the inverse surface
 * (PrimeVue's `contrast` severity), apart from the status toasts at the top
 * right; full width minus the 16px gutters on phones (PrimeVue's 25rem would
 * overflow a 360px screen). Mounted once, in the default layout.
 */
import { ACTION_TOAST_GROUP, type ActionToastMessage } from '~/composables/useAppToast';

const toast = useToast();

/** The action runs once and the toast closes; a failing action never leaves it open. */
function run(message: ActionToastMessage) {
  toast.remove(message);
  message.onAction();
}
</script>
