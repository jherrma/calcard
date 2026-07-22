// @vitest-environment nuxt
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createTestingPinia } from '@pinia/testing';
import { useApi } from './useApi';
import { useAuthStore } from '~/stores/auth';

// `$fetch` is a Nuxt GLOBAL (not an unimport auto-import), so it is stubbed via
// vi.stubGlobal rather than mockNuxtImport. We capture the config object useApi()
// hands to $fetch.create so we can invoke its onResponse hook directly — no
// network, fully hermetic.
const fetchCreate = vi.fn((_config?: unknown) => vi.fn());

interface OfetchContext {
  response: { _data: unknown; status?: number };
}
type OnResponse = (ctx: OfetchContext) => void | Promise<void>;

function buildConfig() {
  useApi();
  const cfg = fetchCreate.mock.calls[0]![0] as { onResponse: OnResponse };
  return cfg;
}

beforeEach(() => {
  createTestingPinia({ stubActions: false });
  fetchCreate.mockClear();
  const f = vi.fn() as unknown as { create: typeof fetchCreate };
  f.create = fetchCreate;
  vi.stubGlobal('$fetch', f);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('useApi onResponse unwrapping', () => {
  it('unwraps the { status: "ok", data } SuccessResponse envelope', async () => {
    const cfg = buildConfig();
    const response = { _data: { status: 'ok', data: { calendars: [{ id: 1 }] } } };
    await cfg.onResponse({ response });
    // The inner payload replaces the envelope.
    expect(response._data).toEqual({ calendars: [{ id: 1 }] });
  });

  it('leaves a raw AddressBook-shaped response untouched (not wrapped)', async () => {
    const cfg = buildConfig();
    const raw = { addressbooks: [{ ID: 1, Name: 'Personal' }] };
    const response = { _data: raw };
    await cfg.onResponse({ response });
    // No `status: "ok"` key → passthrough, same object reference.
    expect(response._data).toBe(raw);
  });

  it('leaves a raw Contacts-list response (capital C, Total/Limit/Offset) untouched', async () => {
    const cfg = buildConfig();
    const raw = { Contacts: [{ id: 'c1' }], Total: 1, Limit: 200, Offset: 0 };
    const response = { _data: raw };
    await cfg.onResponse({ response });
    expect(response._data).toBe(raw);
    expect((response._data as { Contacts: unknown[] }).Contacts).toHaveLength(1);
  });
});

describe('token refresh scheduling uses expires_at (Unix timestamp), not expires_in', () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it('schedules a refresh 60s before the absolute expiry', () => {
    vi.useFakeTimers();
    const store = useAuthStore();
    const spy = vi.spyOn(store, 'refreshToken').mockResolvedValue(undefined);

    const now = Math.floor(Date.now() / 1000);
    store.scheduleTokenRefresh(now + 120); // expires_at 120s from now → fire at +60s

    vi.advanceTimersByTime(59_000);
    expect(spy).not.toHaveBeenCalled();
    vi.advanceTimersByTime(2_000);
    expect(spy).toHaveBeenCalledTimes(1);
  });

  it('treats the argument as an absolute timestamp: a bare "expires_in" value schedules nothing', () => {
    vi.useFakeTimers();
    const store = useAuthStore();
    const spy = vi.spyOn(store, 'refreshToken').mockResolvedValue(undefined);

    // 3600 interpreted as a Unix ts is decades in the past → (3600 - now - 60) < 0,
    // so no timer is armed. If the code used expires_in seconds this would (wrongly) fire.
    store.scheduleTokenRefresh(3600);

    vi.advanceTimersByTime(10_000_000);
    expect(spy).not.toHaveBeenCalled();
  });
});
