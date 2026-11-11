<template>
  <Toast
    :group="REMINDER_TOAST_GROUP"
    position="top-right"
    :breakpoints="TOAST_BREAKPOINTS"
  >
    <template #message="{ message }">
      <div class="flex min-w-0 flex-1 items-start gap-3" data-test="reminder-toast">
        <i class="pi pi-bell mt-0.5 shrink-0 text-lg" aria-hidden="true" />
        <div class="min-w-0 flex-1">
          <p class="break-words text-sm font-semibold" data-test="reminder-toast-title">{{ message.summary }}</p>
          <ul
            v-if="dataOf(message).lines"
            class="mt-1 list-disc space-y-0.5 pl-4 text-sm"
            data-test="reminder-toast-lines"
          >
            <li v-for="(line, i) in dataOf(message).lines" :key="i" class="break-words">{{ line }}</li>
          </ul>
          <p v-else class="mt-0.5 break-words text-sm" data-test="reminder-toast-detail">{{ message.detail }}</p>
          <div class="mt-2 flex flex-wrap gap-2">
            <button
              v-if="dataOf(message).link"
              type="button"
              class="rounded-md px-2 py-1 text-sm font-semibold hover:bg-black/10 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 dark:hover:bg-white/10"
              data-test="reminder-toast-open"
              :aria-label="`Open ${dataOf(message).title}`"
              @click="open(message)"
            >
              Open
            </button>
            <button
              type="button"
              class="rounded-md px-2 py-1 text-sm font-semibold hover:bg-black/10 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 dark:hover:bg-white/10"
              data-test="reminder-toast-dismiss"
              :aria-label="dataOf(message).dismissLabel"
              @click="dismiss(message)"
            >
              Dismiss
            </button>
          </div>
        </div>
      </div>
    </template>
  </Toast>
</template>

<script setup lang="ts">
/**
 * The reminder toasts (#169, spec §10.5, §10.7): sticky `info` toasts for the alarms store's
 * display queue, in the `app-reminder` group, top right. Mounted by the default and settings
 * layouts, not by the auth layout. It cannot reuse `useAppToast().action()` (one at a time, 5 s).
 *
 * The rules: nothing is claimed or shown while the document is hidden (the queue waits and is
 * worked off at `visibilitychange`); a visible tab first claims each key through the
 * BroadcastChannel (`utils/reminderChannel.ts`) so exactly one tab shows it; a toast that is
 * rendered in a visible document calls `markShown(keys)`, which is the acknowledgement. More
 * than 3 reminders due at once become one summary toast acknowledging all of them. A tab that
 * loses the claim yields the key (no acknowledgement) and takes it back after 5 s if the winner
 * never announced it.
 */
import type { ToastMessageOptions } from 'primevue/toast';
import type { UpcomingAlarm } from '~/types/alarms';
import type { ClaimResult } from '~/utils/reminderChannel';
import { REMINDER_TOAST_GROUP, type ReminderToastData, type ReminderToastMessage } from '~/composables/useAppToast';

/** A tab that lost a claim waits this long for the winner's `shown` before claiming again. */
const YIELD_MS = 5000;

const store = useAlarmsStore();
const toast = useToast();

const dataOf = (message: ToastMessageOptions) => (message as Partial<ReminderToastMessage>).reminder ?? ({} as Partial<ReminderToastData>);

const isVisible = () => typeof document !== 'undefined' && document.visibilityState === 'visible';

const channel = createReminderChannel({
  onShown: (key) => {
    clearYield(key);
    store.drop([key]);
  },
});

const claiming = new Set<string>();
const yielded = new Map<string, ReturnType<typeof setTimeout>>();
let ready: string[] = [];
let flushTimer: ReturnType<typeof setTimeout> | null = null;
let unmounted = false;

function clearYield(key: string) {
  const t = yielded.get(key);
  if (t) clearTimeout(t);
  yielded.delete(key);
}

/** Claims every queued key this tab is not already handling. Only in a visible document. */
function process() {
  if (unmounted || !isVisible()) return;
  for (const alarm of [...store.queue]) {
    const key = alarm.key;
    if (claiming.has(key) || yielded.has(key) || ready.includes(key)) continue;
    claiming.add(key);
    void channel.claim(key).then((result) => settle(key, result));
  }
}

function settle(key: string, result: ClaimResult) {
  claiming.delete(key);
  if (unmounted || result === 'cancelled') return;
  if (result === 'lost') {
    yielded.set(
      key,
      setTimeout(() => {
        yielded.delete(key);
        process();
      }, YIELD_MS),
    );
    return;
  }
  if (!store.queue.some((a) => a.key === key)) return; // dropped or shown meanwhile
  if (!isVisible()) return; // stays queued; the next visibilitychange claims again
  ready.push(key);
  if (!flushTimer) flushTimer = setTimeout(flush, 0);
}

/** Renders what this tab won, then acknowledges it (only a rendered toast counts as shown). */
function flush() {
  flushTimer = null;
  const keys = ready;
  ready = [];
  if (unmounted || !isVisible()) return;
  const alarms = keys
    .map((key) => store.queue.find((a) => a.key === key))
    .filter((a): a is UpcomingAlarm => a !== undefined);
  if (alarms.length === 0) return;
  const now = store.serverNow();

  if (alarms.length > MAX_SINGLE_TOASTS) {
    const summary: ReminderToastMessage = {
      severity: 'info',
      summary: `You missed ${alarms.length} reminders`,
      group: REMINDER_TOAST_GROUP,
      reminder: {
        keys: alarms.map((a) => a.key),
        title: 'reminders',
        lines: summaryLines(alarms),
        dismissLabel: `Dismiss ${alarms.length} missed reminders`,
      },
    };
    toast.add(summary);
  } else {
    for (const alarm of alarms) {
      const title = alarmTitle(alarm);
      const message: ReminderToastMessage = {
        severity: 'info',
        summary: title,
        detail: alarmDetail(alarm, now),
        group: REMINDER_TOAST_GROUP,
        reminder: {
          keys: [alarm.key],
          title,
          link: alarmLink(alarm),
          dismissLabel: `Dismiss reminder for ${title}`,
        },
      };
      toast.add(message);
    }
  }
  const shown = alarms.map((a) => a.key);
  store.markShown(shown);
  for (const key of shown) channel.announceShown(key);
}

/** "Open" goes to the event's day or the task's list and closes the toast (already acknowledged). */
function open(message: ToastMessageOptions) {
  const link = dataOf(message).link;
  toast.remove(message);
  if (link) void navigateTo(link);
}

function dismiss(message: ToastMessageOptions) {
  toast.remove(message);
}

watch(() => store.queue.map((a) => a.key).join(','), process);

onMounted(() => {
  document.addEventListener('visibilitychange', process);
  process();
});

onBeforeUnmount(() => {
  unmounted = true;
  document.removeEventListener('visibilitychange', process);
  if (flushTimer) clearTimeout(flushTimer);
  for (const t of yielded.values()) clearTimeout(t);
  yielded.clear();
  channel.close();
});
</script>
