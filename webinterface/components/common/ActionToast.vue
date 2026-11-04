<template>
  <Toast
    :group="ACTION_TOAST_GROUP"
    position="bottom-center"
    :breakpoints="TOAST_BREAKPOINTS"
    @mouse-enter="keepOpen"
    @mouse-leave="keepOpen"
  >
    <template #message="{ message }">
      <div
        class="flex min-w-0 flex-1 items-center gap-3"
        data-test="action-toast"
        @focusin="pauseTimer"
        @focusout="resumeTimer"
      >
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
 *
 * The 5 s countdown stops while the pointer is over the toast OR the focus is
 * inside it, so a keyboard user who tabbed to "Undo" is not timed out under
 * their fingers. PrimeVue's Toast pauses on mouse enter only (and, in 4.5.5,
 * only when the host passes an `onMouseEnter` handler: the two no-op listeners
 * above). To pause on focus too, this component takes the real mouseenter and
 * mouseleave of the message away from PrimeVue (a capture listener on the
 * document), keeps "hovered" and "focused" itself, and raises ONE synthetic
 * mouseenter when either becomes true and ONE synthetic mouseleave when both
 * are false again, which PrimeVue turns into pause and resume. Raising them
 * once each matters: a second enter would pause from a stale clock.
 */
import { ACTION_TOAST_GROUP, type ActionToastMessage } from '~/composables/useAppToast';

const toast = useToast();

interface PauseState {
  hovered: boolean;
  focused: boolean;
  paused: boolean;
}

/** One state per message element: a replaced toast starts unpaused. */
const states = new WeakMap<HTMLElement, PauseState>();

function stateOf(el: HTMLElement): PauseState {
  let state = states.get(el);
  if (!state) {
    state = { hovered: false, focused: false, paused: false };
    states.set(el, state);
  }
  return state;
}

/** PrimeVue only pauses the countdown on hover when a handler is passed; the pausing itself needs no code. */
function keepOpen() {}

const isActionMessage = (el: unknown): el is HTMLElement =>
  el instanceof HTMLElement && el.matches('[role="alert"]') && el.querySelector('[data-test="action-toast"]') !== null;

/** Pause (one synthetic mouseenter) or resume (one synthetic mouseleave) when the wish changed. */
function sync(el: HTMLElement) {
  const state = stateOf(el);
  const wantPaused = state.hovered || state.focused;
  if (wantPaused === state.paused) return;
  state.paused = wantPaused;
  el.dispatchEvent(new MouseEvent(wantPaused ? 'mouseenter' : 'mouseleave'));
}

/** The pointer's own enter/leave on a message: noted here, never passed on to PrimeVue. */
function onPointer(event: MouseEvent) {
  const el = event.target;
  if (!event.isTrusted || !isActionMessage(el)) return;
  event.stopImmediatePropagation();
  stateOf(el).hovered = event.type === 'mouseenter';
  sync(el);
}

const messageOf = (event: Event) => (event.currentTarget as HTMLElement).closest<HTMLElement>('[role="alert"]');

/** Focus moved into the toast. */
function pauseTimer(event: FocusEvent) {
  const el = messageOf(event);
  if (!el) return;
  stateOf(el).focused = true;
  sync(el);
}

/** Focus left the toast (moving between its own controls does not count). */
function resumeTimer(event: FocusEvent) {
  const el = messageOf(event);
  if (!el || el.contains(event.relatedTarget as Node | null)) return;
  stateOf(el).focused = false;
  sync(el);
}

onMounted(() => {
  document.addEventListener('mouseenter', onPointer, true);
  document.addEventListener('mouseleave', onPointer, true);
});
onBeforeUnmount(() => {
  document.removeEventListener('mouseenter', onPointer, true);
  document.removeEventListener('mouseleave', onPointer, true);
});

/** The action runs once and the toast closes; a failing action never leaves it open. */
function run(message: ActionToastMessage) {
  toast.remove(message);
  message.onAction();
}
</script>
