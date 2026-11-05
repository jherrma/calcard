import type { UpcomingAlarm, UpcomingAlarmsResponse, AcknowledgeAlarmsBody } from '~/types/alarms';

/**
 * Reminders (#169, spec §10). While a user is signed in the store polls
 * `GET /api/v1/alarms/upcoming` (raw JSON), arms ONE timer per alarm key, moves
 * due alarms into `queue` (story 16 renders it) and acknowledges each alarm
 * after it was shown with `POST /api/v1/alarms/acknowledge` (204).
 *
 * Every cross-store access is a lazy `import()`: a static import of the
 * calendars store closes the cycle useApi -> auth -> ... -> useApi that breaks
 * `mockNuxtImport('useApi')` in specs. (`useAuthStore` is a static dependency
 * on purpose: auth reaches this store only lazily.)
 */

export const ALARMS_STORAGE_PREFIX = 'calcard-alarms:';
export const POLL_INTERVAL_MS = 5 * 60_000;
export const HEARTBEAT_MS = 30_000;
export const HEARTBEAT_GAP_MS = 90_000;
export const STALE_POLL_MS = 60_000;
export const REFRESH_SOON_MS = 2_000;
export const ACK_DEBOUNCE_MS = 1_000;
export const ACK_RETRY_MS = [5_000, 30_000, 120_000] as const;
export const ACK_BATCH = 500;
export const TIMEZONE_WAIT_MS = 3_000;
const LOOK_AHEAD_MS = 24 * 3_600_000;
/** A stored key is dropped this long after its trigger: the server's window no longer reaches it. */
const KEEP_AFTER_TRIGGER_MS = 25 * 3_600_000;

interface AlarmsState {
  /** Every known alarm of the armed or queued keys, by key. */
  alarms: Record<string, UpcomingAlarm>;
  /** Keys with an armed timer -> trigger instant (epoch ms). The timers themselves live outside the state. */
  scheduled: Record<string, number>;
  /** Keys whose toast was shown and whose acknowledgement is confirmed -> trigger instant (epoch ms). */
  shown: Record<string, number>;
  /** Keys shown but not yet confirmed by the server -> trigger instant (epoch ms). */
  pendingAck: Record<string, number>;
  /** Due alarms waiting to be displayed, oldest first. */
  queue: UpcomingAlarm[];
  /** server clock minus client clock (ms). */
  skewMs: number;
  /** The server answered 404: the feature is off; no polling until the next sign-in. */
  disabled: boolean;
  /** A 400 on the poll (a bug): no polling for the rest of the session. */
  halted: boolean;
  lastPollAt: number | null;
  running: boolean;
  /** The signed-in user whose storage entry is in use (remembered so reset() can remove it after clearAuth). */
  userId: string | null;
}

// Timers and listeners are not serialisable state.
let timers = new Map<string, ReturnType<typeof setTimeout>>();
let pollTimer: ReturnType<typeof setInterval> | null = null;
let heartbeatTimer: ReturnType<typeof setInterval> | null = null;
let refreshSoonTimer: ReturnType<typeof setTimeout> | null = null;
let ackTimer: ReturnType<typeof setTimeout> | null = null;
let removeListeners: (() => void) | null = null;
let lifecycle = 0; // bumped by stop(): aborts a start() that is still waiting
let pollSeq = 0;
let pollInFlight = false;
let pollAgain = false;
let ackInFlight = false;
let ackFailures = 0;
let lastBeat = 0;
let loggedHalt = false;

function statusOf(error: unknown): number | undefined {
  const e = error as { status?: number; statusCode?: number } | null;
  return e?.status ?? e?.statusCode;
}

function storageKey(userId: string): string {
  return `${ALARMS_STORAGE_PREFIX}${userId}`;
}

function clearTimers() {
  for (const t of timers.values()) clearTimeout(t);
  timers = new Map();
}

export const useAlarmsStore = defineStore('alarms', {
  state: (): AlarmsState => ({
    alarms: {},
    scheduled: {},
    shown: {},
    pendingAck: {},
    queue: [],
    skewMs: 0,
    disabled: false,
    halted: false,
    lastPollAt: null,
    running: false,
    userId: null,
  }),

  actions: {
    /** `Date.now()` on the server's clock (an action, not a getter: a getter would cache the time). */
    serverNow(): number {
      return Date.now() + this.skewMs;
    },

    // ---- lifecycle -------------------------------------------------------

    /**
     * Starts polling once the time-zone default settled (at most 3 s), then polls, installs
     * the 5-minute interval, the 30 s heartbeat and the visibility/online listeners.
     * Idempotent: a second call while running does nothing.
     */
    async start(): Promise<void> {
      if (this.running) return;
      this.running = true;
      this.disabled = false;
      this.halted = false;
      loggedHalt = false;
      const mine = ++lifecycle;

      const auth = useAuthStore();
      this.userId = auth.user?.id ?? this.userId;
      this.loadPersisted();

      let waitTimer: ReturnType<typeof setTimeout> | undefined;
      try {
        const ready = (useNuxtApp() as unknown as { $timezoneReady?: () => Promise<void> }).$timezoneReady?.();
        if (ready) {
          await Promise.race([
            Promise.resolve(ready).catch(() => {}),
            new Promise<void>((resolve) => {
              waitTimer = setTimeout(resolve, TIMEZONE_WAIT_MS);
            }),
          ]);
        }
      } catch {
        // No time-zone plugin: poll right away.
      } finally {
        if (waitTimer) clearTimeout(waitTimer);
      }
      if (mine !== lifecycle) return; // stop() ran while waiting
      // The user may only be known now (a restored session fills it after the flag flips).
      if (!this.userId) {
        this.userId = auth.user?.id ?? null;
        this.loadPersisted();
      }

      lastBeat = Date.now();
      pollTimer = setInterval(() => void this.refresh(), POLL_INTERVAL_MS);
      heartbeatTimer = setInterval(() => {
        const now = Date.now();
        const gap = now - lastBeat;
        lastBeat = now;
        if (gap > HEARTBEAT_GAP_MS) {
          this.checkDue();
          void this.refresh();
        }
      }, HEARTBEAT_MS);

      const onVisible = () => {
        if (document.visibilityState !== 'visible') return;
        this.checkDue();
        if (this.lastPollAt === null || Date.now() - this.lastPollAt > STALE_POLL_MS) void this.refresh();
      };
      const onOnline = () => {
        void this.refresh();
        void this.flushAcks();
      };
      document.addEventListener('visibilitychange', onVisible);
      window.addEventListener('online', onOnline);
      removeListeners = () => {
        document.removeEventListener('visibilitychange', onVisible);
        window.removeEventListener('online', onOnline);
      };

      void this.flushAcks();
      await this.refresh();
    },

    /** Clears every timer, interval and listener. Idempotent. State and storage stay (see `reset`). */
    stop(): void {
      lifecycle++;
      pollSeq++;
      this.running = false;
      this.stopPolling();
      this.dropTimers();
      if (ackTimer) clearTimeout(ackTimer);
      ackTimer = null;
      pollInFlight = false;
      pollAgain = false;
      ackInFlight = false;
    },

    /** Ends the polling triggers (interval, heartbeat, debounce, listeners); alarm timers keep running. */
    stopPolling(): void {
      if (pollTimer) clearInterval(pollTimer);
      if (heartbeatTimer) clearInterval(heartbeatTimer);
      if (refreshSoonTimer) clearTimeout(refreshSoonTimer);
      pollTimer = heartbeatTimer = refreshSoonTimer = null;
      removeListeners?.();
      removeListeners = null;
    },

    /** Cancels every armed alarm timer. */
    dropTimers(): void {
      clearTimers();
      this.scheduled = {};
    },

    /**
     * Logout: stop, drop the signed-in user's state and their `localStorage` entry. Called by
     * `authStore.clearAuth()` through a lazy import (Pinia outlives the session in the tab).
     */
    reset(): void {
      this.stop();
      this.removePersisted();
      this.alarms = {};
      this.shown = {};
      this.pendingAck = {};
      this.queue = [];
      this.skewMs = 0;
      this.disabled = false;
      this.halted = false;
      this.lastPollAt = null;
      this.userId = null;
      ackFailures = 0;
      loggedHalt = false;
    },

    // ---- polling ---------------------------------------------------------

    /**
     * One `GET /alarms/upcoming` (one in flight; a request while one runs schedules exactly one
     * follow-up). Failures keep the timers and only warn; 404 switches the feature off; 400 stops polling.
     */
    async refresh(): Promise<void> {
      if (!this.running || this.disabled || this.halted) return;
      if (pollInFlight) {
        pollAgain = true;
        return;
      }
      pollInFlight = true;
      const seq = ++pollSeq;
      try {
        const { toRFC3339 } = await import('~/stores/calendars');
        if (seq !== pollSeq) return;
        const requestStart = Date.now();
        const response = await useApi()<UpcomingAlarmsResponse>('/api/v1/alarms/upcoming', {
          // Through `query`, never interpolated: an unencoded "+" would reach the server as a space (400).
          query: { until: toRFC3339(new Date(requestStart + LOOK_AHEAD_MS)) },
        });
        const requestEnd = Date.now();
        if (seq !== pollSeq) return; // superseded, or stopped/reset meanwhile
        if (!response || !Array.isArray(response.alarms)) throw new Error('Unexpected alarms response');
        const serverNow = Date.parse(response.server_time);
        if (!Number.isNaN(serverNow)) this.skewMs = serverNow - (requestStart + requestEnd) / 2;
        this.lastPollAt = requestEnd;
        this.applyPoll(response);
      } catch (e) {
        if (seq !== pollSeq) return;
        const status = statusOf(e);
        if (status === 404) {
          this.disable();
        } else if (status === 400) {
          this.halted = true;
          this.stopPolling();
          if (!loggedHalt) {
            loggedHalt = true;
            console.error('Reminders: the server rejected the poll (400); polling stopped for this session', e);
          }
        } else {
          console.warn('Reminders: could not fetch upcoming alarms', e);
        }
      } finally {
        if (seq === pollSeq) {
          pollInFlight = false;
          // After the retry schedule is used up, unsent acknowledgements ride along with every poll.
          if (ackFailures >= ACK_RETRY_MS.length) void this.flushAcks();
          if (pollAgain) {
            pollAgain = false;
            void this.refresh();
          }
        }
      }
    },

    /** Debounced (2 s) `refresh()`, for the hooks after a create/edit/delete (story 16). */
    refreshSoon(): void {
      if (!this.running) return;
      if (refreshSoonTimer) clearTimeout(refreshSoonTimer);
      refreshSoonTimer = setTimeout(() => {
        refreshSoonTimer = null;
        void this.refresh();
      }, REFRESH_SOON_MS);
    },

    applyPoll(response: UpcomingAlarmsResponse): void {
      const now = this.serverNow();
      const blocked = [...Object.keys(this.shown), ...Object.keys(this.pendingAck)];
      const triggers = new Map(Object.entries(this.scheduled));
      const result = mergePoll(
        {
          scheduled: Object.keys(this.scheduled),
          queued: this.queue.map((a) => a.key),
          blocked,
          triggers,
        },
        response,
        now,
      );
      for (const key of result.cancel) this.disarm(key);
      // Fresh data for the keys that stay (a renamed event keeps its key).
      for (const alarm of response.alarms) {
        if (alarm.key in this.scheduled) this.alarms[alarm.key] = alarm;
      }
      for (const alarm of result.schedule) this.arm(alarm);
      for (const alarm of result.queue) this.enqueue(alarm);
    },

    disable(): void {
      this.disabled = true;
      this.stopPolling();
      this.dropTimers();
    },

    // ---- timers ----------------------------------------------------------

    arm(alarm: UpcomingAlarm): void {
      const trigger = Date.parse(alarm.trigger_at);
      this.alarms[alarm.key] = alarm;
      this.scheduled[alarm.key] = trigger;
      const existing = timers.get(alarm.key);
      if (existing) clearTimeout(existing);
      timers.set(
        alarm.key,
        setTimeout(() => this.onTimer(alarm.key), delayMs(trigger, Date.now(), this.skewMs)),
      );
    },

    disarm(key: string): void {
      const t = timers.get(key);
      if (t) clearTimeout(t);
      timers.delete(key);
      delete this.scheduled[key];
      if (!this.queue.some((a) => a.key === key)) delete this.alarms[key];
    },

    onTimer(key: string): void {
      timers.delete(key);
      const trigger = this.scheduled[key];
      const alarm = this.alarms[key];
      if (trigger === undefined || !alarm) return;
      if (this.serverNow() < trigger) {
        // Fired early: the clock was set back (or the delay was capped). Wait again.
        timers.set(key, setTimeout(() => this.onTimer(key), delayMs(trigger, Date.now(), this.skewMs)));
        return;
      }
      this.enqueue(alarm);
    },

    /** Moves an alarm into the display queue (never one that is pending, shown or already queued). */
    enqueue(alarm: UpcomingAlarm): void {
      const t = timers.get(alarm.key);
      if (t) clearTimeout(t);
      timers.delete(alarm.key);
      delete this.scheduled[alarm.key];
      if (alarm.key in this.shown || alarm.key in this.pendingAck) return;
      if (this.queue.some((a) => a.key === alarm.key)) return;
      this.alarms[alarm.key] = alarm;
      this.queue.push(alarm);
    },

    /** Queues every armed alarm whose time has passed (after a sleep, or a tab returning to the front). */
    checkDue(): void {
      const now = this.serverNow();
      for (const [key, trigger] of Object.entries(this.scheduled)) {
        const alarm = this.alarms[key];
        if (alarm && trigger <= now) this.enqueue(alarm);
      }
    },

    // ---- acknowledgement -------------------------------------------------

    /**
     * Story 16 calls this when toasts were rendered in a visible document: the keys leave the
     * queue, wait in `pendingAck` and are acknowledged after a 1 s debounce.
     */
    markShown(keys: string[]): void {
      let changed = false;
      for (const key of keys) {
        const alarm = this.alarms[key] ?? this.queue.find((a) => a.key === key);
        const trigger = alarm ? Date.parse(alarm.trigger_at) : this.scheduled[key];
        const t = timers.get(key);
        if (t) clearTimeout(t);
        timers.delete(key);
        delete this.scheduled[key];
        this.queue = this.queue.filter((a) => a.key !== key);
        delete this.alarms[key];
        if (key in this.shown || key in this.pendingAck) continue;
        this.pendingAck[key] = Number.isNaN(trigger) || trigger === undefined ? Date.now() : trigger;
        changed = true;
      }
      if (!changed) return;
      this.persist();
      this.scheduleFlush(ACK_DEBOUNCE_MS);
    },

    scheduleFlush(delay: number): void {
      if (!this.running) return;
      if (ackTimer) clearTimeout(ackTimer);
      ackTimer = setTimeout(() => {
        ackTimer = null;
        void this.flushAcks();
      }, delay);
    },

    /**
     * Sends every pending key in batches of at most 500 (`{ keys }` only). 204 moves them to
     * `shown`; a failure keeps them and retries after 5 s, 30 s, 2 min, then with every poll.
     */
    async flushAcks(): Promise<void> {
      if (ackInFlight) return;
      const keys = Object.keys(this.pendingAck);
      if (keys.length === 0) return;
      if (ackTimer) clearTimeout(ackTimer);
      ackTimer = null;
      ackInFlight = true;
      const mine = lifecycle;
      try {
        const api = useApi();
        for (let i = 0; i < keys.length; i += ACK_BATCH) {
          const batch = keys.slice(i, i + ACK_BATCH);
          const body: AcknowledgeAlarmsBody = { keys: batch };
          await api('/api/v1/alarms/acknowledge', { method: 'POST', body });
          if (mine !== lifecycle) return; // stopped or reset while in flight
          for (const key of batch) {
            const trigger = this.pendingAck[key];
            delete this.pendingAck[key];
            if (trigger !== undefined) this.shown[key] = trigger;
          }
          this.persist();
        }
        ackFailures = 0;
      } catch (e) {
        if (mine !== lifecycle) return;
        if (statusOf(e) === 404) {
          this.disable();
          return;
        }
        console.warn('Reminders: could not acknowledge alarms', e);
        const delay = ACK_RETRY_MS[ackFailures];
        ackFailures++;
        // Beyond the schedule the next poll retries (see refresh()).
        if (delay !== undefined) this.scheduleFlush(delay);
      } finally {
        if (mine === lifecycle) {
          ackInFlight = false;
          // Keys marked while the request ran.
          if (ackFailures === 0 && Object.keys(this.pendingAck).length > 0) this.scheduleFlush(ACK_DEBOUNCE_MS);
        }
      }
    },

    // ---- persistence -----------------------------------------------------

    /** Writes `shown` and `pendingAck` (each key with its trigger) to localStorage; never throws. */
    persist(): void {
      if (!this.userId) return;
      try {
        this.prune();
        window.localStorage.setItem(
          storageKey(this.userId),
          JSON.stringify({ shown: this.shown, pending: this.pendingAck }),
        );
      } catch {
        // Safari private mode / blocked storage: the in-memory sets still protect this tab.
      }
    },

    loadPersisted(): void {
      if (!this.userId) return;
      try {
        const raw = window.localStorage.getItem(storageKey(this.userId));
        if (!raw) return;
        const parsed = JSON.parse(raw) as { shown?: unknown; pending?: unknown };
        const read = (value: unknown, into: Record<string, number>) => {
          if (!value || typeof value !== 'object') return;
          for (const [key, trigger] of Object.entries(value as Record<string, unknown>)) {
            if (/^[0-9a-f]{32}$/.test(key) && typeof trigger === 'number') into[key] = trigger;
          }
        };
        read(parsed.shown, this.shown);
        read(parsed.pending, this.pendingAck);
        this.prune();
      } catch {
        // Unreadable or throwing storage: start empty.
      }
    },

    removePersisted(): void {
      if (!this.userId) return;
      try {
        window.localStorage.removeItem(storageKey(this.userId));
      } catch {
        // ignore
      }
    },

    /** Drops keys whose trigger lies more than 25 h back: the server no longer returns them. */
    prune(): void {
      const cutoff = Date.now() - KEEP_AFTER_TRIGGER_MS;
      for (const key of Object.keys(this.shown)) if ((this.shown[key] ?? 0) < cutoff) delete this.shown[key];
      for (const key of Object.keys(this.pendingAck)) if ((this.pendingAck[key] ?? 0) < cutoff) delete this.pendingAck[key];
    },
  },
});
