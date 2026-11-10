import type { ToastMessageOptions } from 'primevue/toast';

/** Toast group of the action toasts, rendered by `components/common/ActionToast.vue` (default layout). */
export const ACTION_TOAST_GROUP = 'app-action';

/** Toast group of the reminder toasts, rendered by `components/common/ReminderToast.vue` (default and settings layouts; #169). */
export const REMINDER_TOAST_GROUP = 'app-reminder';

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

  /** A plain notice of one severity: its default title and how long it stays (ms). */
  const notice =
    (severity: 'success' | 'error' | 'warn' | 'info', defaultTitle: string, life: number) =>
    (message: string, title = defaultTitle) => {
      toast.add({ severity, summary: title, detail: message, life });
    };

  return {
    success: notice('success', 'Success', 3000),
    error: notice('error', 'Error', 5000),
    warn: notice('warn', 'Warning', 4000),
    info: notice('info', 'Info', 3000),
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

/** What a reminder toast carries (`ReminderToast.vue`, #169): the keys it acknowledged and its button targets. */
export interface ReminderToastData {
  keys: string[];
  /** The event or task name, for the buttons' aria-labels. */
  title: string;
  /** Where "Open" goes; absent on the summary toast. */
  link?: string;
  /** The summary toast's list. */
  lines?: string[];
  dismissLabel: string;
}

export interface ReminderToastMessage extends ToastMessageOptions {
  reminder: ReminderToastData;
}
