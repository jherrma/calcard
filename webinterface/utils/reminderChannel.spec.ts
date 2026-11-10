import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { CLAIM_WAIT_MS, createReminderChannel, type ChannelLike } from './reminderChannel';

// SCOPE (#169 story 16, spec §10.7): the cross-tab claim. A stub bus stands in for
// BroadcastChannel: a posted message reaches every OTHER channel on it (a microtask later).

class Bus {
  channels: StubChannel[] = [];
}
class StubChannel implements ChannelLike {
  listeners = new Set<(e: MessageEvent) => void>();
  closed = false;
  constructor(private bus: Bus) {
    bus.channels.push(this);
  }
  postMessage(message: unknown) {
    if (this.closed) throw new Error('closed');
    for (const other of this.bus.channels) {
      if (other === this || other.closed) continue;
      queueMicrotask(() => other.listeners.forEach((l) => l({ data: message } as MessageEvent)));
    }
  }
  addEventListener(_t: 'message', l: (e: MessageEvent) => void) {
    this.listeners.add(l);
  }
  removeEventListener(_t: 'message', l: (e: MessageEvent) => void) {
    this.listeners.delete(l);
  }
  close() {
    this.closed = true;
  }
}

const K = 'a'.repeat(32);

beforeEach(() => {
  vi.useFakeTimers();
});
afterEach(() => {
  vi.useRealTimers();
});

function tab(bus: Bus, id: string, at: number, onShown = vi.fn()) {
  const channel = createReminderChannel({ onShown, tabId: id, now: () => at, createChannel: () => new StubChannel(bus) });
  return { channel, onShown };
}

describe('claim', () => {
  it('the lower (at, tab) wins, the other tab loses', async () => {
    const bus = new Bus();
    const a = tab(bus, 'tab-a', 1000);
    const b = tab(bus, 'tab-b', 1050);
    const ra = a.channel.claim(K);
    const rb = b.channel.claim(K);
    await vi.advanceTimersByTimeAsync(CLAIM_WAIT_MS);
    expect(await ra).toBe('won');
    expect(await rb).toBe('lost');
  });

  it('the tab id breaks a tie on `at`', async () => {
    const bus = new Bus();
    const a = tab(bus, 'tab-a', 1000);
    const b = tab(bus, 'tab-b', 1000);
    const ra = a.channel.claim(K);
    const rb = b.channel.claim(K);
    await vi.advanceTimersByTimeAsync(CLAIM_WAIT_MS);
    expect(await ra).toBe('won');
    expect(await rb).toBe('lost');
  });

  it('waits 300 ms before it decides', async () => {
    const bus = new Bus();
    const a = tab(bus, 'tab-a', 1000);
    let result: string | null = null;
    void a.channel.claim(K).then((r) => (result = r));
    await vi.advanceTimersByTimeAsync(CLAIM_WAIT_MS - 1);
    expect(result).toBeNull();
    await vi.advanceTimersByTimeAsync(1);
    expect(result).toBe('won');
  });

  it('`shown` from another tab cancels the claim timer and reports the key', async () => {
    const bus = new Bus();
    const a = tab(bus, 'tab-a', 1000);
    const b = tab(bus, 'tab-b', 1100);
    const rb = b.channel.claim(K);
    await vi.advanceTimersByTimeAsync(10);
    a.channel.announceShown(K);
    await vi.advanceTimersByTimeAsync(0);
    expect(await rb).toBe('cancelled');
    expect(b.onShown).toHaveBeenCalledWith(K);
    expect(vi.getTimerCount()).toBe(0); // the 300 ms timer is gone
  });

  it('a key announced as shown is never claimed afterwards', async () => {
    const bus = new Bus();
    const a = tab(bus, 'tab-a', 1000);
    const b = tab(bus, 'tab-b', 1100);
    a.channel.announceShown(K);
    await vi.advanceTimersByTimeAsync(0);
    expect(await b.channel.claim(K)).toBe('cancelled');
  });

  it('ignores malformed messages', async () => {
    const bus = new Bus();
    const a = tab(bus, 'tab-a', 1000);
    const stub = new StubChannel(bus);
    stub.postMessage({ type: 'claim', key: 5 });
    stub.postMessage('nonsense');
    stub.postMessage(null);
    await vi.advanceTimersByTimeAsync(0);
    expect(a.onShown).not.toHaveBeenCalled();
  });
});

describe('without BroadcastChannel', () => {
  it('claim wins at once, so every tab shows its own', async () => {
    const channel = createReminderChannel({ onShown: vi.fn(), createChannel: () => null });
    expect(channel.available).toBe(false);
    expect(await channel.claim(K)).toBe('won');
    expect(() => channel.announceShown(K)).not.toThrow();
  });

  it('falls back when constructing the channel throws', async () => {
    const channel = createReminderChannel({
      onShown: vi.fn(),
      createChannel: () => {
        throw new Error('SecurityError');
      },
    });
    expect(channel.available).toBe(false);
    expect(await channel.claim(K)).toBe('won');
  });

  it('the default factory tolerates a missing global', async () => {
    vi.stubGlobal('BroadcastChannel', undefined);
    try {
      const channel = createReminderChannel({ onShown: vi.fn() });
      expect(channel.available).toBe(false);
      expect(await channel.claim(K)).toBe('won');
    } finally {
      vi.unstubAllGlobals();
    }
  });
});

describe('close', () => {
  it('cancels waiting claims and stops listening', async () => {
    const bus = new Bus();
    const a = tab(bus, 'tab-a', 1000);
    const b = tab(bus, 'tab-b', 1100);
    const ra = a.channel.claim(K);
    a.channel.close();
    expect(await ra).toBe('cancelled');
    b.channel.announceShown(K);
    await vi.advanceTimersByTimeAsync(0);
    expect(a.onShown).not.toHaveBeenCalled();
  });
});
