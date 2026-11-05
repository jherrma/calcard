// @vitest-environment nuxt
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import { createTestingPinia } from '@pinia/testing';
import { flushPromises } from '@vue/test-utils';
import { useAlarmsStore, ALARMS_STORAGE_PREFIX } from './alarms';
import { useAuthStore } from './auth';
import { toRFC3339 } from './calendars';
import type { UpcomingAlarm, UpcomingAlarmsResponse } from '~/types/alarms';
import { installMemoryStorage, installThrowingStorage } from '~/test/support/storage';

// SCOPE: the reminder store (#169, story 15): polling and its triggers, one timer
// per key, the display queue, acknowledgement with retries and persistence, and
// the failure rules (404 = off, 400 = halt). Rendering is story 16.

const { apiMock, tz } = vi.hoisted(() => ({
  apiMock: vi.fn(),
  tz: { ready: null as null | (() => Promise<void>) },
}));
mockNuxtImport('useApi', () => () => apiMock);
// The nuxtApp's `$timezoneReady` is a non-configurable property: stand in for it through a proxy.
mockNuxtImport('useNuxtApp', (original) => () =>
  new Proxy(original() as object, {
    get: (target, prop, receiver) => (prop === '$timezoneReady' && tz.ready ? tz.ready : Reflect.get(target, prop, receiver)),
  }),
);

const T0 = Date.parse('2026-11-04T09:00:00Z');
const MIN = 60_000;
const UPCOMING = '/api/v1/alarms/upcoming';
const ACK = '/api/v1/alarms/acknowledge';

const key = (n: number) => n.toString(16).padStart(32, '0');
const iso = (ms: number) => new Date(ms).toISOString().replace('.000Z', 'Z');

function alarm(n: number, offsetMs: number, over: Partial<UpcomingAlarm> = {}): UpcomingAlarm {
  const at = iso(Date.now() + offsetMs);
  return {
    key: key(n), trigger_at: at, uid: `u${n}`, recurrence_id: null, alarm_index: 0, action: 'display',
    description: null, object_type: 'event', object_id: `obj-${n}`, calendar_id: 1, calendar_uuid: 'cal',
    calendar_name: 'Personal', calendar_color: '#3788d8', summary: `Event ${n}`, location: null, all_day: false,
    start: at, start_date: null, end: null, due: null, due_date: null, ...over,
  };
}

let response: () => Partial<UpcomingAlarmsResponse>;
let ackHandler: (body: { keys: string[] }) => unknown;
let upcomingHandler: (() => unknown) | null;


function serve() {
  apiMock.mockImplementation(async (url: string, options: { method?: string; body?: { keys: string[] }; query?: Record<string, string> } = {}) => {
    if (url === UPCOMING) {
      if (upcomingHandler) return upcomingHandler();
      return { alarms: [], count: 0, from: '', until: '', truncated: false, server_time: iso(Date.now()), ...response() };
    }
    if (url === ACK) return ackHandler(options.body!);
    throw new Error(`unexpected request ${url}`);
  });
}

const gets = () => apiMock.mock.calls.filter(([u]) => u === UPCOMING);
const posts = () => apiMock.mock.calls.filter(([u]) => u === ACK);
const httpError = (status: number) => Object.assign(new Error(`HTTP ${status}`), { status });

let storage: ReturnType<typeof installMemoryStorage>;
let warn: ReturnType<typeof vi.spyOn>;
let errorLog: ReturnType<typeof vi.spyOn>;

function freshStore(userId = 'user-1') {
  createTestingPinia({ stubActions: false });
  const auth = useAuthStore();
  auth.user = { id: userId } as never;
  auth.isAuthenticated = true;
  const store = useAlarmsStore();
  store.reset(); // module-level flags survive a new Pinia
  return store;
}

/** start() and let the poll finish; the time-zone promise is already resolved. */
async function startStore(store: ReturnType<typeof useAlarmsStore>) {
  const p = store.start();
  await flushPromises();
  await p;
  await flushPromises();
}

async function advance(ms: number) {
  await vi.advanceTimersByTimeAsync(ms);
  await flushPromises();
}

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'Date'] });
  vi.setSystemTime(T0);
  apiMock.mockReset();
  response = () => ({});
  ackHandler = () => undefined;
  upcomingHandler = null;
  serve();
  storage = installMemoryStorage();
  warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
  errorLog = vi.spyOn(console, 'error').mockImplementation(() => {});
});

afterEach(() => {
  useAlarmsStore().reset();
  storage.restore();
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe('start and the poll request', () => {
  it('polls at once when the time-zone call is already settled', async () => {
    const store = freshStore();
    await startStore(store);
    expect(gets()).toHaveLength(1);
  });

  it('waits for the time-zone promise, but at most 3 s', async () => {
    const store = freshStore();
    tz.ready = () => new Promise<void>(() => {});
    try {
      const p = store.start();
      await advance(2_999);
      expect(gets()).toHaveLength(0);
      await advance(1);
      await p;
      expect(gets()).toHaveLength(1);
    } finally {
      tz.ready = null;
    }
  });

  it('polls as soon as the time-zone promise settles, before the cap', async () => {
    const store = freshStore();
    let settle!: () => void;
    tz.ready = () => new Promise<void>((r) => (settle = r));
    try {
      const p = store.start();
      await advance(1_000);
      expect(gets()).toHaveLength(0);
      settle();
      await flushPromises();
      await p;
      expect(gets()).toHaveLength(1);
    } finally {
      tz.ready = null;
    }
  });

  it('a stop() while waiting for the time zone aborts the start', async () => {
    const store = freshStore();
    tz.ready = () => new Promise<void>(() => {});
    try {
      const p = store.start();
      await flushPromises();
      store.stop();
      await advance(5_000);
      await p;
      expect(gets()).toHaveLength(0);
      expect(vi.getTimerCount()).toBe(0);
    } finally {
      tz.ready = null;
    }
  });

  it('sends `until` (now + 24 h, local offset) through the query option and no zone', async () => {
    const store = freshStore();
    await startStore(store);
    const [url, options] = gets()[0]!;
    expect(url).toBe(UPCOMING);
    expect(options.query).toEqual({ until: toRFC3339(new Date(T0 + 24 * 60 * MIN)) });
    expect(options.query.until).toMatch(/[+-]\d{2}:\d{2}$/);
    expect(url).not.toContain('+');
    expect(Object.keys(options.query)).toEqual(['until']);
  });

  it('start() twice installs one set of timers and polls once', async () => {
    const store = freshStore();
    await startStore(store);
    const timers = vi.getTimerCount();
    await startStore(store);
    expect(gets()).toHaveLength(1);
    expect(vi.getTimerCount()).toBe(timers);
  });

  it('estimates the clock skew from server_time and the request midpoint', async () => {
    const store = freshStore();
    response = () => ({ server_time: iso(Date.now() + 90_000) });
    await startStore(store);
    expect(store.skewMs).toBe(90_000);
  });

  it('keeps only one request in flight and follows up once', async () => {
    const store = freshStore();
    await startStore(store);
    let release!: () => void;
    upcomingHandler = () => new Promise((resolve) => (release = () => resolve({ alarms: [], server_time: iso(Date.now()), truncated: false })));
    const first = store.refresh();
    void store.refresh();
    void store.refresh();
    await flushPromises();
    expect(gets()).toHaveLength(2); // the start poll + the one in flight
    upcomingHandler = null;
    release();
    await first;
    await flushPromises();
    expect(gets()).toHaveLength(3); // exactly one follow-up
  });
});

describe('polling triggers', () => {
  it('polls every 5 minutes', async () => {
    const store = freshStore();
    await startStore(store);
    await advance(5 * MIN);
    expect(gets()).toHaveLength(2);
    await advance(5 * MIN);
    expect(gets()).toHaveLength(3);
  });

  it('keeps polling every 5 minutes while the tab is hidden', async () => {
    const store = freshStore();
    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden');
    await startStore(store);
    await advance(5 * MIN);
    expect(gets()).toHaveLength(2);
  });

  it('a tab returning to visible polls when the last poll is older than 60 s, not before', async () => {
    const store = freshStore();
    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible');
    await startStore(store);
    await advance(30_000);
    document.dispatchEvent(new Event('visibilitychange'));
    await flushPromises();
    expect(gets()).toHaveLength(1);
    await advance(40_000); // 70 s since the poll
    document.dispatchEvent(new Event('visibilitychange'));
    await flushPromises();
    expect(gets()).toHaveLength(2);
  });

  it('a hidden visibilitychange does nothing', async () => {
    const store = freshStore();
    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden');
    await startStore(store);
    await advance(90_000);
    const before = gets().length;
    document.dispatchEvent(new Event('visibilitychange'));
    await flushPromises();
    expect(gets()).toHaveLength(before);
  });

  it('coming online polls and flushes pending acknowledgements', async () => {
    const store = freshStore();
    await startStore(store);
    ackHandler = () => {
      throw httpError(0);
    };
    store.markShown([key(1)]);
    await advance(1_000); // first attempt fails; the 5 s retry is armed
    expect(posts()).toHaveLength(1);
    ackHandler = () => undefined;
    window.dispatchEvent(new Event('online'));
    await flushPromises();
    expect(gets()).toHaveLength(2);
    expect(posts()).toHaveLength(2);
    expect(store.shown[key(1)]).toBeDefined();
  });

  it('refreshSoon debounces by 2 s', async () => {
    const store = freshStore();
    await startStore(store);
    store.refreshSoon();
    await advance(1_000);
    store.refreshSoon();
    await advance(1_999);
    expect(gets()).toHaveLength(1);
    await advance(1);
    expect(gets()).toHaveLength(2);
  });

  it('a heartbeat gap above 90 s (sleep) queues every passed alarm and polls', async () => {
    const store = freshStore();
    response = () => ({ alarms: [alarm(1, 10 * MIN), alarm(2, 40 * MIN)] });
    await startStore(store);
    expect(store.queue).toHaveLength(0);
    // The machine sleeps 20 minutes: the clock jumps, no timer runs.
    vi.setSystemTime(Date.now() + 20 * MIN);
    await advance(30_000); // the heartbeat fires
    expect(store.queue.map((a) => a.key)).toEqual([key(1)]);
    expect(gets()).toHaveLength(2);
  });

  it('a normal heartbeat does not poll', async () => {
    const store = freshStore();
    await startStore(store);
    await advance(2 * MIN);
    expect(gets()).toHaveLength(1);
  });
});

describe('timers and the display queue', () => {
  it('queues an alarm 10 minutes ahead at its trigger, once', async () => {
    const store = freshStore();
    response = () => ({ alarms: [alarm(1, 10 * MIN)] });
    await startStore(store);
    expect(store.queue).toHaveLength(0);
    await advance(10 * MIN - 1);
    // (a 5-minute poll ran in between and returned the same key: still one timer)
    expect(store.queue).toHaveLength(0);
    await advance(1);
    expect(store.queue.map((a) => a.key)).toEqual([key(1)]);
    await advance(MIN);
    expect(store.queue).toHaveLength(1);
  });

  it('a second poll with the same key arms no second timer', async () => {
    const store = freshStore();
    const a = alarm(1, 10 * MIN);
    response = () => ({ alarms: [a] });
    await startStore(store);
    const timers = vi.getTimerCount();
    await store.refresh();
    await flushPromises();
    expect(vi.getTimerCount()).toBe(timers);
    expect(Object.keys(store.scheduled)).toEqual([key(1)]);
  });

  it('honours the skew: a server clock ahead fires earlier', async () => {
    const store = freshStore();
    response = () => ({ alarms: [alarm(1, 10 * MIN)], server_time: iso(Date.now() + 2 * MIN) });
    await startStore(store);
    // trigger_at was built from the client clock, the server is 2 min ahead: 8 min left.
    await advance(8 * MIN - 1);
    expect(store.queue).toHaveLength(0);
    await advance(1);
    expect(store.queue).toHaveLength(1);
  });

  it('queues overdue alarms at once on load', async () => {
    const store = freshStore();
    response = () => ({ alarms: [alarm(1, -3 * 60 * MIN), alarm(2, -MIN), alarm(3, 5 * MIN)] });
    await startStore(store);
    expect(store.queue.map((a) => a.key)).toEqual([key(1), key(2)]);
    expect(Object.keys(store.scheduled)).toEqual([key(3)]);
  });

  it('cancels the timer of a key that vanished from the next poll', async () => {
    const store = freshStore();
    response = () => ({ alarms: [alarm(1, 7 * MIN)] });
    await startStore(store);
    response = () => ({ alarms: [] });
    await advance(5 * MIN); // the poll
    expect(store.scheduled).toEqual({});
    await advance(5 * MIN);
    expect(store.queue).toEqual([]);
  });

  it('a moved alarm: the old key is cancelled, the new key fires', async () => {
    const store = freshStore();
    response = () => ({ alarms: [alarm(1, 7 * MIN)] });
    await startStore(store);
    response = () => ({ alarms: [alarm(2, 7 * MIN)] }); // 12 minutes after the start
    await advance(5 * MIN);
    expect(Object.keys(store.scheduled)).toEqual([key(2)]);
    await advance(8 * MIN);
    expect(store.queue.map((a) => a.key)).toEqual([key(2)]);
  });

  it('truncated: keys after the last returned trigger keep their timers', async () => {
    const store = freshStore();
    const first = alarm(1, 6 * MIN);
    const later = alarm(2, 60 * MIN);
    response = () => ({ alarms: [first, later] });
    await startStore(store);
    response = () => ({ alarms: [first], truncated: true }); // the list was cut after key 1
    await store.refresh();
    await flushPromises();
    expect(Object.keys(store.scheduled).sort()).toEqual([key(1), key(2)]);
    response = () => ({ alarms: [first], truncated: false }); // complete answer: key 2 is gone
    await store.refresh();
    await flushPromises();
    expect(Object.keys(store.scheduled)).toEqual([key(1)]);
  });

  it('keeps queued keys when a later poll no longer returns them', async () => {
    const store = freshStore();
    response = () => ({ alarms: [alarm(1, -MIN)] });
    await startStore(store);
    response = () => ({ alarms: [] });
    await store.refresh();
    await flushPromises();
    expect(store.queue.map((a) => a.key)).toEqual([key(1)]);
  });

  it('re-arms when the timer fires but the server clock says it is too early (clock set back)', async () => {
    const store = freshStore();
    const a = alarm(1, 10 * MIN);
    let serverOffset = 0;
    response = () => ({ alarms: [a], server_time: iso(Date.now() + serverOffset) });
    await startStore(store);
    serverOffset = -3 * MIN; // the next poll reveals the server clock is 3 minutes behind
    await advance(5 * MIN);
    expect(store.skewMs).toBe(-3 * MIN);
    await advance(5 * MIN); // the original timer fires: 10 min client time, 7 min server time
    expect(store.queue).toHaveLength(0);
    expect(Object.keys(store.scheduled)).toEqual([key(1)]);
    await advance(3 * MIN);
    expect(store.queue).toHaveLength(1);
  });

  it('checkDue queues what has passed and cancels its timer', async () => {
    const store = freshStore();
    response = () => ({ alarms: [alarm(1, 10 * MIN)] });
    await startStore(store);
    vi.setSystemTime(Date.now() + 11 * MIN);
    store.checkDue();
    expect(store.queue).toHaveLength(1);
    expect(store.scheduled).toEqual({});
  });
});

describe('acknowledgement', () => {
  it('markShown posts { keys } once, after a 1 s debounce, with no times', async () => {
    const store = freshStore();
    response = () => ({ alarms: [alarm(1, -MIN)] });
    await startStore(store);
    store.markShown([key(1)]);
    expect(store.queue).toEqual([]);
    expect(store.pendingAck[key(1)]).toBeDefined();
    await advance(999);
    expect(posts()).toHaveLength(0);
    await advance(1);
    expect(posts()).toHaveLength(1);
    expect(posts()[0]![1]).toEqual({ method: 'POST', body: { keys: [key(1)] } });
    expect(store.pendingAck).toEqual({});
    expect(store.shown[key(1)]).toBeDefined();
  });

  it('collects keys marked within the debounce into one request', async () => {
    const store = freshStore();
    await startStore(store);
    store.markShown([key(1)]);
    await advance(500);
    store.markShown([key(2)]);
    await advance(1_000);
    expect(posts()).toHaveLength(1);
    expect(posts()[0]![1].body.keys).toEqual([key(1), key(2)]);
  });

  it('sends batches of at most 500 keys', async () => {
    const store = freshStore();
    await startStore(store);
    store.markShown(Array.from({ length: 1_200 }, (_, i) => key(i + 1)));
    await advance(1_000);
    expect(posts().map(([, o]) => o.body.keys.length)).toEqual([500, 500, 200]);
    expect(store.pendingAck).toEqual({});
  });

  it('retries a failed POST after 5 s, 30 s, 2 min, then with every poll; never re-queues a pending key', async () => {
    const store = freshStore();
    let fail = true;
    ackHandler = () => {
      if (fail) throw httpError(500);
      return undefined;
    };
    const a = alarm(1, -MIN);
    response = () => ({ alarms: [a] });
    await startStore(store);
    store.markShown([key(1)]);
    await advance(1_000);
    expect(posts()).toHaveLength(1);
    await advance(4_999);
    expect(posts()).toHaveLength(1);
    await advance(1);
    expect(posts()).toHaveLength(2);
    await advance(29_999);
    expect(posts()).toHaveLength(2);
    await advance(1);
    expect(posts()).toHaveLength(3);
    await advance(2 * MIN - 1);
    expect(posts()).toHaveLength(3);
    await advance(1);
    expect(posts()).toHaveLength(4);
    // The schedule is used up: no timer retries, the next poll does.
    // (a poll already ran at the 5-minute mark inside this advance? total elapsed ~2m36s: not yet)
    await advance(10 * MIN);
    expect(posts().length).toBeGreaterThan(4);
    // The alarm came back in every poll but was never queued again.
    expect(store.queue).toEqual([]);
    fail = false;
    await store.refresh();
    await flushPromises();
    expect(store.pendingAck).toEqual({});
    expect(store.queue).toEqual([]);
    expect(store.shown[key(1)]).toBeDefined();
  });

  it('a shown key returned by a poll is not queued again', async () => {
    const store = freshStore();
    response = () => ({ alarms: [alarm(1, -MIN)] });
    await startStore(store);
    store.markShown([key(1)]);
    await advance(1_000);
    await store.refresh();
    await flushPromises();
    expect(store.queue).toEqual([]);
    expect(Object.keys(store.scheduled)).toEqual([]);
  });

  it('mirrors shown and pendingAck to localStorage under the user, with trigger times', async () => {
    const store = freshStore('u-42');
    ackHandler = () => {
      throw httpError(500);
    };
    response = () => ({ alarms: [alarm(1, -MIN)] });
    await startStore(store);
    store.markShown([key(1)]);
    const stored = JSON.parse(storage.data.get(`${ALARMS_STORAGE_PREFIX}u-42`)!);
    expect(stored.pending[key(1)]).toBe(T0 - MIN);
    expect(stored.shown).toEqual({});
  });

  it('after a reload with a pending acknowledgement the alarm is not shown again and the POST is retried', async () => {
    storage.data.set(`${ALARMS_STORAGE_PREFIX}user-1`, JSON.stringify({ shown: {}, pending: { [key(1)]: T0 - MIN } }));
    const store = freshStore('user-1');
    storage.data.set(`${ALARMS_STORAGE_PREFIX}user-1`, JSON.stringify({ shown: {}, pending: { [key(1)]: T0 - MIN } }));
    response = () => ({ alarms: [alarm(1, -MIN)] });
    await startStore(store);
    expect(posts()).toHaveLength(1);
    expect(posts()[0]![1].body.keys).toEqual([key(1)]);
    expect(store.queue).toEqual([]);
  });

  it('a shown key from storage keeps the alarm out of the queue', async () => {
    const store = freshStore('user-1');
    storage.data.set(`${ALARMS_STORAGE_PREFIX}user-1`, JSON.stringify({ shown: { [key(1)]: T0 - MIN }, pending: {} }));
    response = () => ({ alarms: [alarm(1, -MIN)] });
    await startStore(store);
    expect(store.queue).toEqual([]);
    expect(posts()).toHaveLength(0);
  });

  it('drops stored keys 25 h after their trigger', async () => {
    const store = freshStore('user-1');
    storage.data.set(
      `${ALARMS_STORAGE_PREFIX}user-1`,
      JSON.stringify({ shown: { [key(1)]: T0 - 26 * 60 * MIN, [key(2)]: T0 - 24 * 60 * MIN }, pending: {} }),
    );
    await startStore(store);
    expect(Object.keys(store.shown)).toEqual([key(2)]);
  });

  it('ignores garbage in storage', async () => {
    const store = freshStore('user-1');
    storage.data.set(`${ALARMS_STORAGE_PREFIX}user-1`, '{not json');
    await startStore(store);
    expect(store.shown).toEqual({});
    expect(gets()).toHaveLength(1);
  });

  it('a throwing localStorage breaks nothing: alarms still queue, mark and acknowledge', async () => {
    const restore = installThrowingStorage();
    try {
      const store = freshStore();
      response = () => ({ alarms: [alarm(1, -MIN)] });
      await startStore(store);
      expect(store.queue).toHaveLength(1);
      store.markShown([key(1)]);
      await advance(1_000);
      expect(posts()).toHaveLength(1);
      expect(store.shown[key(1)]).toBeDefined();
      store.reset();
    } finally {
      restore();
    }
  });
});

describe('failure handling', () => {
  it('a failed poll keeps the timers and only warns', async () => {
    const store = freshStore();
    response = () => ({ alarms: [alarm(1, 7 * MIN)] });
    await startStore(store);
    upcomingHandler = () => {
      throw httpError(500);
    };
    await advance(5 * MIN);
    expect(warn).toHaveBeenCalled();
    expect(Object.keys(store.scheduled)).toEqual([key(1)]);
    await advance(2 * MIN);
    expect(store.queue).toHaveLength(1);
    // and polling goes on
    upcomingHandler = null;
    await advance(5 * MIN);
    expect(gets().length).toBeGreaterThanOrEqual(3);
  });

  it('a 404 disables polling until the next sign-in', async () => {
    const store = freshStore();
    upcomingHandler = () => {
      throw httpError(404);
    };
    await startStore(store);
    expect(store.disabled).toBe(true);
    await advance(30 * MIN);
    expect(gets()).toHaveLength(1);
    window.dispatchEvent(new Event('online'));
    await flushPromises();
    expect(gets()).toHaveLength(1);
    expect(vi.getTimerCount()).toBe(0);
    // next sign-in: stop (sign-out), start again
    upcomingHandler = null;
    store.stop();
    await startStore(store);
    expect(store.disabled).toBe(false);
    expect(gets()).toHaveLength(2);
  });

  it('a 400 on the poll stops polling for the session and logs once', async () => {
    const store = freshStore();
    upcomingHandler = () => {
      throw httpError(400);
    };
    await startStore(store);
    expect(store.halted).toBe(true);
    await advance(30 * MIN);
    await store.refresh();
    expect(gets()).toHaveLength(1);
    expect(errorLog).toHaveBeenCalledTimes(1);
  });

  it('a 400 keeps the timers of alarms already armed', async () => {
    const store = freshStore();
    response = () => ({ alarms: [alarm(1, 7 * MIN)] });
    await startStore(store);
    upcomingHandler = () => {
      throw httpError(400);
    };
    await advance(5 * MIN);
    expect(store.halted).toBe(true);
    await advance(2 * MIN);
    expect(store.queue).toHaveLength(1);
  });
});

describe('stop and reset', () => {
  it('stop clears every timer and listener', async () => {
    const store = freshStore();
    response = () => ({ alarms: [alarm(1, 7 * MIN)] });
    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible');
    await startStore(store);
    store.stop();
    expect(vi.getTimerCount()).toBe(0);
    await advance(30 * MIN);
    document.dispatchEvent(new Event('visibilitychange'));
    window.dispatchEvent(new Event('online'));
    await flushPromises();
    expect(gets()).toHaveLength(1);
    expect(store.queue).toEqual([]);
    store.stop(); // idempotent
  });

  it('a poll answer arriving after stop is dropped', async () => {
    const store = freshStore();
    await startStore(store);
    let release!: () => void;
    upcomingHandler = () =>
      new Promise((resolve) => {
        release = () => resolve({ alarms: [alarm(1, -MIN)], server_time: iso(Date.now()), truncated: false });
      });
    const p = store.refresh();
    await flushPromises();
    store.stop();
    release();
    await p;
    await flushPromises();
    expect(store.queue).toEqual([]);
  });

  it('reset removes that user\'s stored entry and empties the state', async () => {
    const store = freshStore('user-9');
    ackHandler = () => {
      throw httpError(500);
    };
    response = () => ({ alarms: [alarm(1, -MIN)] });
    await startStore(store);
    store.markShown([key(1)]);
    expect(storage.data.has(`${ALARMS_STORAGE_PREFIX}user-9`)).toBe(true);
    store.reset();
    expect(storage.data.has(`${ALARMS_STORAGE_PREFIX}user-9`)).toBe(false);
    expect(store.pendingAck).toEqual({});
    expect(store.queue).toEqual([]);
    expect(vi.getTimerCount()).toBe(0);
  });
});

describe('drop (another tab shows the key, story 16)', () => {
  it('cancels the timer, leaves the queue, acknowledges nothing and is never queued again', async () => {
    const store = freshStore();
    response = () => ({ alarms: [alarm(1, 10 * MIN), alarm(2, -MIN)] });
    await startStore(store);
    expect(store.queue.map((a) => a.key)).toEqual([key(2)]);

    store.drop([key(1), key(2)]);
    expect(store.queue).toEqual([]);
    expect(store.scheduled).toEqual({});
    expect(Object.keys(store.dropped).sort()).toEqual([key(1), key(2)]);

    await advance(11 * MIN); // the timer of key 1 would have fired
    expect(store.queue).toEqual([]);
    expect(store.pendingAck).toEqual({});
    expect(posts()).toHaveLength(0);
  });

  it('a later poll that still returns the key does not queue or arm it', async () => {
    const store = freshStore();
    response = () => ({ alarms: [alarm(1, -MIN)] });
    await startStore(store);
    store.drop([key(1)]);
    store.refreshSoon();
    await advance(2_100);
    expect(store.queue).toEqual([]);
    expect(store.scheduled).toEqual({});
  });

  it('reset forgets dropped keys', () => {
    const store = freshStore();
    store.drop([key(1)]);
    store.reset();
    expect(store.dropped).toEqual({});
  });
});
