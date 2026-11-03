import type { ToastMessageOptions } from 'primevue/toast';

/** Toast group of the action toasts, rendered by `components/common/ActionToast.vue` (default layout). */
export const ACTION_TOAST_GROUP = 'app-action';

/** How long an action toast (e.g. "Task completed" + Undo) stays, in ms. */
export const ACTION_TOAST_LIFE = 5000;

/** A toast carrying one action button: what `useAppToast().action()` adds and `ActionToast.vue` renders. */
export interface ActionToastMessage extends ToastMessageOptions {
  /** The button's label, e.g. "Undo". */
  actionLabel: string;
  /** Runs when the button is pressed; the toast then closes. */
  onAction: () => void;
}

export const useAppToast = () => {
  const toast = useToast();

  return {
    success: (message: string, title = 'Success') => {
      toast.add({
        severity: 'success',
        summary: title,
        detail: message,
        life: 3000,
      });
    },
    error: (message: string, title = 'Error') => {
      toast.add({
        severity: 'error',
        summary: title,
        detail: message,
        life: 5000,
      });
    },
    warn: (message: string, title = 'Warning') => {
      toast.add({
        severity: 'warn',
        summary: title,
        detail: message,
        life: 4000,
      });
    },
    info: (message: string, title = 'Info') => {
      toast.add({
        severity: 'info',
        summary: title,
        detail: message,
        life: 3000,
      });
    },
    /**
     * A short notice with one action button ("Task completed" + "Undo"), at the
     * bottom of the screen for `life` ms. One at a time: a new one replaces the
     * previous, so a run of actions never stacks up over the content. Rendered
     * by ActionToast.vue, which only the default layout mounts.
     */
    action: (summary: string, actionLabel: string, onAction: () => void, life = ACTION_TOAST_LIFE) => {
      const message: ActionToastMessage = {
        severity: 'contrast',
        summary,
        life,
        group: ACTION_TOAST_GROUP,
        actionLabel,
        onAction,
      };
      toast.removeGroup(ACTION_TOAST_GROUP);
      toast.add(message);
    },
  };
};
