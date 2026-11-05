/**
 * One reminder toast per key across several open tabs (#169, spec §10.7).
 *
 * `BroadcastChannel('calcard-reminders')`: before showing a reminder a VISIBLE tab posts
 * `{ type: 'claim', key, tab, at }` and waits 300 ms; the smallest `(at, tab)` among the claims it
 * saw wins, shows the toast and posts `{ type: 'shown', key }`; every tab receiving `shown`
 * drops the key. Hidden tabs never claim (the caller's job). Without `BroadcastChannel` (or when
 * it throws) every tab shows its own: `claim()` then wins at once. `navigator.locks` is not used
 * (secure contexts only; the default compose deployment is plain http).
 */

export const REMINDER_CHANNEL_NAME = 'calcard-reminders';
export const CLAIM_WAIT_MS = 300;

export interface ClaimMessage {
  type: 'claim';
  key: string;
  tab: string;
  at: number;
}
export interface ShownMessage {
  type: 'shown';
  key: string;
}
export type ReminderChannelMessage = ClaimMessage | ShownMessage;

/** `won`: show it; `lost`: another tab shows it; `cancelled`: another tab already did, or the channel closed. */
export type ClaimResult = 'won' | 'lost' | 'cancelled';

export interface ChannelLike {
  postMessage(message: unknown): void;
  addEventListener(type: 'message', listener: (event: MessageEvent) => void): void;
  removeEventListener(type: 'message', listener: (event: MessageEvent) => void): void;
  close(): void;
}

export interface ReminderChannelOptions {
  /** Called when another tab posted `shown` for a key. */
  onShown: (key: string) => void;
  /** Injection point for specs; defaults to `new BroadcastChannel(...)`. */
  createChannel?: () => ChannelLike | null;
  /** This tab's id (specs). */
  tabId?: string;
  now?: () => number;
}

export interface ReminderChannel {
  /** false without BroadcastChannel: every tab shows its own reminders. */
  readonly available: boolean;
  readonly tabId: string;
  claim(key: string): Promise<ClaimResult>;
  /** Posts `shown` so the other tabs drop the key. */
  announceShown(key: string): void;
  close(): void;
}

function defaultChannel(): ChannelLike | null {
  try {
    if (typeof BroadcastChannel === 'undefined') return null;
    return new BroadcastChannel(REMINDER_CHANNEL_NAME);
  } catch {
    return null;
  }
}

const randomId = () => Math.random().toString(36).slice(2, 10) + Date.now().toString(36);

function isMessage(data: unknown): data is ReminderChannelMessage {
  if (!data || typeof data !== 'object') return false;
  const m = data as Record<string, unknown>;
  if (typeof m.key !== 'string') return false;
  if (m.type === 'shown') return true;
  return m.type === 'claim' && typeof m.tab === 'string' && typeof m.at === 'number';
}

export function createReminderChannel(options: ReminderChannelOptions): ReminderChannel {
  const tabId = options.tabId ?? randomId();
  const now = options.now ?? Date.now;
  let channel: ChannelLike | null = null;
  try {
    channel = (options.createChannel ?? defaultChannel)();
  } catch {
    channel = null;
  }

  // Claims seen per key (own and foreign), and the claims being waited out.
  const claims = new Map<string, Array<{ at: number; tab: string }>>();
  const waiting = new Map<string, { timer: ReturnType<typeof setTimeout>; resolve: (r: ClaimResult) => void }>();
  // Keys another tab announced as shown: a late claim of ours for them is cancelled.
  const announced = new Set<string>();

  const post = (message: ReminderChannelMessage) => {
    try {
      channel?.postMessage(message);
    } catch {
      // A closed channel: this tab then behaves like one without the API.
    }
  };

  const onMessage = (event: MessageEvent) => {
    const data = event.data as unknown;
    if (!isMessage(data)) return;
    if (data.type === 'claim') {
      if (data.tab === tabId) return;
      const list = claims.get(data.key) ?? [];
      list.push({ at: data.at, tab: data.tab });
      claims.set(data.key, list);
      return;
    }
    announced.add(data.key);
    const pending = waiting.get(data.key);
    if (pending) {
      clearTimeout(pending.timer);
      waiting.delete(data.key);
      claims.delete(data.key);
      pending.resolve('cancelled');
    }
    options.onShown(data.key);
  };
  channel?.addEventListener('message', onMessage);

  return {
    available: channel !== null,
    tabId,

    claim(key: string): Promise<ClaimResult> {
      if (!channel) return Promise.resolve('won');
      if (announced.has(key)) return Promise.resolve('cancelled');
      const existing = waiting.get(key);
      if (existing) {
        // One claim per key at a time: a second caller waits for the same outcome.
        return new Promise<ClaimResult>((resolve) => {
          const first = existing.resolve;
          existing.resolve = (r) => {
            first(r);
            resolve(r);
          };
        });
      }
      const at = now();
      const list = claims.get(key) ?? [];
      list.push({ at, tab: tabId });
      claims.set(key, list);
      post({ type: 'claim', key, tab: tabId, at });
      return new Promise<ClaimResult>((resolve) => {
        const timer = setTimeout(() => {
          waiting.delete(key);
          const seen = claims.get(key) ?? [];
          claims.delete(key);
          let best = { at, tab: tabId };
          for (const c of seen) {
            if (c.at < best.at || (c.at === best.at && c.tab < best.tab)) best = c;
          }
          const winner = best.tab === tabId;
          // The window of the other tabs ends later than ours: the winner's `shown` arrives afterwards.
          resolve(winner ? 'won' : 'lost');
        }, CLAIM_WAIT_MS);
        waiting.set(key, { timer, resolve });
      });
    },

    announceShown(key: string) {
      announced.add(key);
      post({ type: 'shown', key });
    },

    close() {
      for (const [key, pending] of waiting) {
        clearTimeout(pending.timer);
        pending.resolve('cancelled');
        waiting.delete(key);
      }
      claims.clear();
      try {
        channel?.removeEventListener('message', onMessage);
        channel?.close();
      } catch {
        // ignore
      }
      channel = null;
    },
  };
}
