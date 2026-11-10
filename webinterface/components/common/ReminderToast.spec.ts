// @vitest-environment nuxt
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import { createTestingPinia } from '@pinia/testing';
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils';
import PrimeVue from 'primevue/config';
import ToastService from 'primevue/toastservice';
// @ts-expect-error PrimeVue ships no type declaration for this entry point
import ToastEventBus from 'primevue/toasteventbus';
import ReminderToast from './ReminderToast.vue';
import { useAlarmsStore } from '~/stores/alarms';
import type { UpcomingAlarm } from '~/types/alarms';

// SCOPE (#169 story 16, spec §10.5, §10.7, §10.10): the reminder toasts. One sticky toast per
// key, a summary above 3, the Open and Dismiss buttons, `markShown` only for a rendered toast in
// a visible document, the claim between several tabs (a stub BroadcastChannel carries messages
// between the components, each with its own Pinia) and the fallback without the API. The real
// PrimeVue Toast and ToastService run; the alarms store is real but never started.

const { apiMock, navMock } = vi.hoisted(() => ({ apiMock: vi.fn(), navMock: vi.fn() }));
mockNuxtImport('useApi', () => () => apiMock);
mockNuxtImport('navigateTo', () => navMock);

enableAutoUnmount(afterEach);

const T0 = new Date(2026, 10, 5, 8, 45, 0).getTime();
const key = (n: number) => n.toString(16).padStart(32, '0');
const iso = (ms: number) => new Date(ms).toISOString();

function alarm(n: number, over: Partial<UpcomingAlarm> = {}): UpcomingAlarm {
  const start = new Date(2026, 10, 5, 9, 0, 0).getTime();
  return {
    key: key(n), trigger_at: iso(T0), uid: `u${n}`, recurrence_id: null, alarm_index: 0, action: 'display',
    description: null, object_type: 'event', object_id: `obj-${n}`, calendar_id: 7, calendar_uuid: 'cal-uuid',
    calendar_name: 'Personal', calendar_color: '#3788d8', summary: `Event ${n}`, location: null, all_day: false,
    start: iso(start), start_date: null, end: iso(start + 3_600_000), due: null, due_date: null, ...over,
  };
}

// ---- a BroadcastChannel stub that connects every instance of the same name -------------------

const instances: StubBroadcast[] = [];
class StubBroadcast {
  listeners = new Set<(e: MessageEvent) => void>();
  closed = false;
  constructor(public name: string) {
    instances.push(this);
  }
  postMessage(data: unknown) {
    for (const other of instances) {
      if (other === this || other.closed || other.name !== this.name) continue;
      queueMicrotask(() => other.listeners.forEach((l) => l({ data } as MessageEvent)));
    }
  }
  addEventListener(_t: string, l: (e: MessageEvent) => void) {
    this.listeners.add(l);
  }
  removeEventListener(_t: string, l: (e: MessageEvent) => void) {
    this.listeners.delete(l);
  }
  close() {
    this.closed = true;
  }
}

let visibility: 'visible' | 'hidden';
function setVisibility(v: 'visible' | 'hidden') {
  visibility = v;
  document.dispatchEvent(new Event('visibilitychange'));
}

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'Date'] });
  vi.setSystemTime(T0);
  instances.length = 0;
  visibility = 'visible';
  Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => visibility });
  vi.stubGlobal('BroadcastChannel', StubBroadcast);
  apiMock.mockReset();
  apiMock.mockResolvedValue(undefined);
  navMock.mockReset();
  adds = vi.spyOn(ToastEventBus, 'emit');
});

afterEach(() => {
  adds.mockRestore();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

/** Mounts one "tab": a ReminderToast with its own Pinia. */
function mountTab() {
  const pinia = createTestingPinia({ stubActions: false, stubPatch: false });
  const wrapper = mount(ReminderToast, { attachTo: document.body, global: { plugins: [pinia, PrimeVue, ToastService] } });
  const store = useAlarmsStore(pinia);
  return { wrapper, store };
}

async function settle(ms = 400) {
  await vi.advanceTimersByTimeAsync(ms);
  await flushPromises();
}

/** Toasts added through the (process-wide) PrimeVue bus: what several tabs in one test would each render. */
let adds: ReturnType<typeof vi.spyOn>;
const addedToasts = () => adds.mock.calls.filter(([event]: unknown[]) => event === 'add').length;

const toasts = () => Array.from(document.querySelectorAll<HTMLElement>('[data-test="reminder-toast"]'));
const titles = () => toasts().map((t) => t.querySelector('[data-test="reminder-toast-title"]')!.textContent);

describe('one toast per key', () => {
  it('shows a sticky info toast per alarm with title, detail and both buttons', async () => {
    const { store } = mountTab();
    store.queue = [alarm(1, { location: 'Room 2', start: iso(new Date(2026, 10, 5, 9, 0).getTime()) }), alarm(2, { summary: '' })];
    await settle();

    expect(titles()).toEqual(['Event 1', 'Untitled']);
    expect(toasts()[0]!.querySelector('[data-test="reminder-toast-detail"]')!.textContent).toBe('Starts 09:00 · Room 2');
    for (const t of toasts()) {
      expect(t.querySelector('[data-test="reminder-toast-open"]')!.textContent!.trim()).toBe('Open');
      expect(t.querySelector('[data-test="reminder-toast-dismiss"]')!.textContent!.trim()).toBe('Dismiss');
    }
    // role="alert" on the PrimeVue message, severity info.
    const message = document.querySelector('.p-toast-message')!;
    expect(message.getAttribute('role')).toBe('alert');
    expect(message.className).toContain('p-toast-message-info');
  });

  it('is sticky: still there a long time later', async () => {
    const { store } = mountTab();
    store.queue = [alarm(1)];
    await settle();
    await settle(10 * 60_000);
    expect(toasts()).toHaveLength(1);
  });

  it('does not show the same key twice', async () => {
    const { store } = mountTab();
    store.queue = [alarm(1)];
    await settle();
    store.queue = [alarm(1)]; // a poll re-queues it before the acknowledgement landed
    await settle();
    expect(toasts()).toHaveLength(1);
  });

  it('labels the buttons with the event name', async () => {
    const { store } = mountTab();
    store.queue = [alarm(1, { summary: 'Team sync' })];
    await settle();
    expect(document.querySelector('[data-test="reminder-toast-open"]')!.getAttribute('aria-label')).toBe('Open Team sync');
    expect(document.querySelector('[data-test="reminder-toast-dismiss"]')!.getAttribute('aria-label')).toBe(
      'Dismiss reminder for Team sync',
    );
  });

  it('never takes focus', async () => {
    const { store } = mountTab();
    const before = document.activeElement;
    store.queue = [alarm(1)];
    await settle();
    expect(document.activeElement).toBe(before);
  });
});

describe('the summary toast above 3', () => {
  it('3 alarms stay single toasts', async () => {
    const { store } = mountTab();
    store.queue = [alarm(1), alarm(2), alarm(3)];
    await settle();
    expect(toasts()).toHaveLength(3);
  });

  it('4 alarms become one summary toast; acknowledging it acknowledges all four keys', async () => {
    const { store } = mountTab();
    store.queue = [alarm(1), alarm(2), alarm(3), alarm(4)];
    await settle();

    expect(toasts()).toHaveLength(1);
    expect(titles()).toEqual(['You missed 4 reminders']);
    expect(document.querySelectorAll('[data-test="reminder-toast-lines"] li')).toHaveLength(4);
    expect(document.querySelector('[data-test="reminder-toast-open"]')).toBeNull();
    expect(Object.keys(store.pendingAck).sort()).toEqual([key(1), key(2), key(3), key(4)]);
    expect(store.queue).toEqual([]);
  });

  it('lists up to 5 plus "and N more"', async () => {
    const { store } = mountTab();
    store.queue = [1, 2, 3, 4, 5, 6, 7].map((n) => alarm(n));
    await settle();
    const lines = Array.from(document.querySelectorAll('[data-test="reminder-toast-lines"] li')).map((l) => l.textContent);
    expect(lines).toHaveLength(6);
    expect(lines[5]).toBe('and 2 more');
    expect(Object.keys(store.pendingAck)).toHaveLength(7);
  });

  it('Dismiss closes the summary', async () => {
    const { store } = mountTab();
    store.queue = [1, 2, 3, 4].map((n) => alarm(n));
    await settle();
    document.querySelector<HTMLButtonElement>('[data-test="reminder-toast-dismiss"]')!.click();
    await settle(1000);
    expect(toasts()).toHaveLength(0);
  });
});

describe('Open and Dismiss', () => {
  async function showOne(a: UpcomingAlarm) {
    const { store } = mountTab();
    store.queue = [a];
    await settle();
    return store;
  }

  it('an event goes to its day, event id and calendar', async () => {
    await showOne(alarm(1, { start: iso(new Date(2026, 10, 5, 9).getTime()) }));
    document.querySelector<HTMLButtonElement>('[data-test="reminder-toast-open"]')!.click();
    await settle(1000);
    expect(navMock).toHaveBeenCalledWith('/calendar?date=2026-11-05&event=obj-1&cal=7');
    expect(toasts()).toHaveLength(0);
  });

  it('an occurrence of a series carries its recurrence id', async () => {
    await showOne(alarm(1, { recurrence_id: '20261105T090000Z' }));
    document.querySelector<HTMLButtonElement>('[data-test="reminder-toast-open"]')!.click();
    expect(navMock).toHaveBeenCalledWith('/calendar?date=2026-11-05&event=obj-1&cal=7&recurrence=20261105T090000Z');
  });

  it('an all-day event uses its own date', async () => {
    await showOne(alarm(1, { all_day: true, start: null, end: null, start_date: '2026-11-06' }));
    document.querySelector<HTMLButtonElement>('[data-test="reminder-toast-open"]')!.click();
    expect(navMock).toHaveBeenCalledWith('/calendar?date=2026-11-06&event=obj-1&cal=7');
  });

  it('a task goes to its list', async () => {
    await showOne(alarm(1, { object_type: 'task', calendar_uuid: 'list-9', start: null, end: null, due: iso(T0 + 3_600_000) }));
    document.querySelector<HTMLButtonElement>('[data-test="reminder-toast-open"]')!.click();
    expect(navMock).toHaveBeenCalledWith('/tasks?list=list-9');
  });

  it('Dismiss closes the toast without navigating; it was acknowledged when shown', async () => {
    const store = await showOne(alarm(1));
    expect(store.pendingAck[key(1)]).toBeDefined();
    document.querySelector<HTMLButtonElement>('[data-test="reminder-toast-dismiss"]')!.click();
    await settle(1000);
    expect(toasts()).toHaveLength(0);
    expect(navMock).not.toHaveBeenCalled();
  });
});

describe('visibility gates the acknowledgement', () => {
  it('a hidden tab queues, claims nothing and acknowledges nothing', async () => {
    visibility = 'hidden';
    const { store } = mountTab();
    store.queue = [alarm(1)];
    await settle();
    expect(toasts()).toHaveLength(0);
    expect(store.queue).toHaveLength(1);
    expect(store.pendingAck).toEqual({});
    expect(instances.every((i) => i.closed || i.listeners.size >= 0)).toBe(true);
  });

  it('shows the queue when the tab returns, and only then marks it shown', async () => {
    visibility = 'hidden';
    const { store } = mountTab();
    store.queue = [alarm(1), alarm(2)];
    await settle();
    expect(store.pendingAck).toEqual({});

    setVisibility('visible');
    await settle();
    expect(titles()).toEqual(['Event 1', 'Event 2']);
    expect(Object.keys(store.pendingAck).sort()).toEqual([key(1), key(2)]);
    expect(store.queue).toEqual([]);
  });

  it('a tab that turns hidden while the claim is waited out neither shows nor acknowledges', async () => {
    const { store } = mountTab();
    store.queue = [alarm(1)];
    await vi.advanceTimersByTimeAsync(100);
    visibility = 'hidden';
    await settle();
    expect(toasts()).toHaveLength(0);
    expect(store.pendingAck).toEqual({});
    expect(store.queue).toHaveLength(1);

    setVisibility('visible');
    await settle();
    expect(toasts()).toHaveLength(1);
  });

  it('a hidden tab never posts a claim', async () => {
    visibility = 'hidden';
    const posted = vi.spyOn(StubBroadcast.prototype, 'postMessage');
    const { store } = mountTab();
    store.queue = [alarm(1)];
    await settle();
    expect(posted).not.toHaveBeenCalled();
    posted.mockRestore();
  });
});

describe('several tabs', () => {
  it('two visible tabs: exactly one shows the toast, the other drops the key without acknowledging', async () => {
    const a = mountTab();
    const b = mountTab();
    a.store.queue = [alarm(1)];
    b.store.queue = [alarm(1)];
    await settle();

    expect(addedToasts()).toBe(1);
    const winner = Object.keys(a.store.pendingAck).length === 1 ? a : b;
    const loser = winner === a ? b : a;
    expect(Object.keys(winner.store.pendingAck)).toEqual([key(1)]);
    expect(loser.store.queue).toEqual([]);
    expect(loser.store.pendingAck).toEqual({});
    expect(loser.store.dropped[key(1)]).toBeDefined();
  });

  it('a key the other tab shows is not queued again by a later poll', async () => {
    const a = mountTab();
    const b = mountTab();
    a.store.queue = [alarm(1)];
    b.store.queue = [alarm(1)];
    await settle();
    const loser = Object.keys(a.store.pendingAck).length === 0 ? a : b;
    loser.store.enqueue(alarm(1));
    expect(loser.store.queue).toEqual([]);
  });

  it('a tab that lost to a claim nobody follows up takes the key back after 5 s', async () => {
    const { store } = mountTab();
    store.queue = [alarm(1)];
    // Another tab claimed the same key a moment earlier (smaller `at`) ...
    const foreign = new StubBroadcast('calcard-reminders');
    foreign.postMessage({ type: 'claim', key: key(1), tab: '0-other', at: T0 - 50 });
    await vi.advanceTimersByTimeAsync(0);
    await settle(400);
    // ... and never showed it: this tab yielded and has not acknowledged.
    expect(addedToasts()).toBe(0);
    expect(store.pendingAck).toEqual({});

    await settle(5500);
    expect(addedToasts()).toBe(1);
    expect(Object.keys(store.pendingAck)).toEqual([key(1)]);
  });

  it('a `shown` from another tab drops the key and cancels the claim', async () => {
    const { store } = mountTab();
    store.queue = [alarm(1)];
    await vi.advanceTimersByTimeAsync(100);
    const foreign = new StubBroadcast('calcard-reminders');
    foreign.postMessage({ type: 'shown', key: key(1) });
    await settle();
    expect(addedToasts()).toBe(0);
    expect(store.queue).toEqual([]);
    expect(store.pendingAck).toEqual({});
  });

  it('without BroadcastChannel every tab shows its own toast', async () => {
    vi.stubGlobal('BroadcastChannel', undefined);
    const a = mountTab();
    const b = mountTab();
    a.store.queue = [alarm(1)];
    b.store.queue = [alarm(1)];
    await settle(10);
    expect(addedToasts()).toBe(2);
    expect(Object.keys(a.store.pendingAck)).toEqual([key(1)]);
    expect(Object.keys(b.store.pendingAck)).toEqual([key(1)]);
  });

  it('a throwing BroadcastChannel constructor falls back to showing', async () => {
    vi.stubGlobal('BroadcastChannel', class {
      constructor() {
        throw new Error('SecurityError');
      }
    });
    const { store } = mountTab();
    store.queue = [alarm(1)];
    await settle(10);
    expect(toasts()).toHaveLength(1);
  });

  it('closes its channel on unmount', async () => {
    const { wrapper } = mountTab();
    wrapper.unmount();
    expect(instances.every((i) => i.closed)).toBe(true);
  });
});
