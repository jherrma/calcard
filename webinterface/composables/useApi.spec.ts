// @vitest-environment nuxt
import { describe, it, expect, beforeEach, afterEach, vi, type MockInstance } from 'vitest';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import { createTestingPinia } from '@pinia/testing';
import { useApi, resetRateLimitNotice } from './useApi';
import { useAuthStore } from '~/stores/auth';

const { toastMock } = vi.hoisted(() => ({
  toastMock: { success: vi.fn(), error: vi.fn(), warn: vi.fn(), info: vi.fn() },
}));
mockNuxtImport('useAppToast', () => () => toastMock);

// `$fetch` is a Nuxt GLOBAL (not an unimport auto-import). Rebinding the name
// with vi.stubGlobal does NOT intercept it — useApi() captures its own
// reference to the global — so we spy on the shared `$fetch.create` METHOD
// instead. That lets us grab the config object useApi() hands to it and invoke
// its onResponse hook directly: no network, fully hermetic.
interface OfetchContext {
  response: { _data: unknown; status?: number };
}
type OnResponse = (ctx: OfetchContext) => void | Promise<void>;

let createSpy: MockInstance;
// The instance $fetch.create() hands back; useApi() wraps it for 429 handling.
let inner: ReturnType<typeof vi.fn>;

function buildConfig() {
  useApi();
  const cfg = createSpy.mock.calls[0]![0] as {
    onResponse: OnResponse;
    retry: number;
    retryStatusCodes: number[];
  };
  return cfg;
}

beforeEach(() => {
  createTestingPinia({ stubActions: false });
  inner = vi.fn();
  createSpy = vi.spyOn($fetch, 'create').mockImplementation(
    () => inner as unknown as typeof $fetch,
  );
  resetRateLimitNotice();
  Object.values(toastMock).forEach((fn) => fn.mockReset());
});

afterEach(() => {
  vi.restoreAllMocks();
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

// A FetchError as ofetch builds it for a 429: message from method/url/status,
// `data` and `response` as getters over the same response object.
function tooManyRequests(
  retryAfter?: string,
  body: Record<string, unknown> = {
    error: 'rate_limit_exceeded',
    message: 'Too many requests. Please try again later.',
  },
) {
  const response = {
    status: 429,
    headers: new Headers(retryAfter === undefined ? {} : { 'Retry-After': retryAfter }),
    _data: body as unknown,
  };
  const err = new Error('[GET] "/api/v1/calendars": 429 Too Many Requests');
  Object.defineProperty(err, 'response', { get: () => response });
  Object.defineProperty(err, 'data', { get: () => response._data });
  Object.assign(err, { status: 429, statusCode: 429 });
  return err as Error & { data: Record<string, unknown>; status: number };
}

describe('HTTP 429 handling', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('keeps ofetch retrying ONLY 401s — 429 is handled by the wrapper, not doubled', () => {
    const cfg = buildConfig();
    expect(cfg.retry).toBe(1);
    expect(cfg.retryStatusCodes).toEqual([401]);
  });

  it('waits Retry-After and re-issues a GET, passing the options through', async () => {
    inner.mockRejectedValueOnce(tooManyRequests('2')).mockResolvedValueOnce({ calendars: [] });
    const api = useApi();

    const p = api('/api/v1/calendars', { query: { a: 1 } });
    await vi.advanceTimersByTimeAsync(1_999);
    expect(inner).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(1);
    expect(inner).toHaveBeenCalledTimes(2);

    await expect(p).resolves.toEqual({ calendars: [] });
    expect(inner).toHaveBeenNthCalledWith(2, '/api/v1/calendars', { query: { a: 1 } });
    expect(toastMock.warn).not.toHaveBeenCalled();
  });

  it('retries HEAD too, and honours an HTTP-date Retry-After', async () => {
    vi.setSystemTime(Date.UTC(2026, 9, 11, 12, 0, 0));
    const at = new Date(Date.now() + 3_000).toUTCString();
    inner.mockRejectedValueOnce(tooManyRequests(at)).mockResolvedValueOnce('ok');

    const p = useApi()('/health', { method: 'HEAD' });
    await vi.advanceTimersByTimeAsync(3_000);
    await expect(p).resolves.toBe('ok');
    expect(inner).toHaveBeenCalledTimes(2);
  });

  it('gives up after 2 retries and rejects with a friendly message', async () => {
    inner.mockImplementation(() => Promise.reject(tooManyRequests('1')));

    const settled = useApi()('/api/v1/calendars').catch((e: unknown) => e);
    await vi.advanceTimersByTimeAsync(10_000);
    const err = (await settled) as ReturnType<typeof tooManyRequests>;

    expect(inner).toHaveBeenCalledTimes(3); // the request + 2 retries
    expect(err.message).toBe('Too many requests, please try again in 1 second.');
    expect(err.status).toBe(429);
    expect(toastMock.warn).toHaveBeenCalledTimes(1);
  });

  it('does not wait out a long Retry-After: rejects at once, all message fields friendly', async () => {
    inner.mockRejectedValueOnce(tooManyRequests('60', {
      error: 'rate_limit_exceeded',
      message: 'Too many requests. Please try again later.',
      retry_after: 60,
    }));

    const err = (await useApi()('/api/v1/calendars').catch((e: unknown) => e)) as ReturnType<
      typeof tooManyRequests
    >;

    const friendly = 'Too many requests, please try again in 60 seconds.';
    expect(inner).toHaveBeenCalledTimes(1);
    // Every way a caller reads an error: Error.message (most stores),
    // data.message (auth/settings pages) and data.error (sharing).
    expect(err.message).toBe(friendly);
    expect(err.data.message).toBe(friendly);
    expect(err.data.error).toBe(friendly);
    expect(err.data.retry_after).toBe(60);
    expect(toastMock.warn).toHaveBeenCalledWith(friendly, 'Too many requests');
  });

  it('never repeats a write, even with a short Retry-After', async () => {
    inner.mockRejectedValue(tooManyRequests('1'));

    const err = (await useApi()('/api/v1/calendars', { method: 'POST', body: {} }).catch(
      (e: unknown) => e,
    )) as Error;

    expect(inner).toHaveBeenCalledTimes(1);
    expect(err.message).toBe('Too many requests, please try again in 1 second.');
  });

  it('without any Retry-After it does not guess: no retry, generic wording', async () => {
    inner.mockRejectedValueOnce(tooManyRequests(undefined, { message: 'Too many attempts.' }));

    const err = (await useApi()('/api/v1/users/me').catch((e: unknown) => e)) as Error;

    expect(inner).toHaveBeenCalledTimes(1);
    expect(err.message).toBe('Too many requests, please try again in a moment.');
  });

  it("uses the body's retry_after when the header is not visible", async () => {
    inner
      .mockRejectedValueOnce(tooManyRequests(undefined, { retry_after: 3 }))
      .mockResolvedValueOnce('ok');

    const p = useApi()('/api/v1/calendars');
    await vi.advanceTimersByTimeAsync(3_000);
    await expect(p).resolves.toBe('ok');
  });

  it('raises ONE toast for a burst of rejected requests, and again once it has passed', async () => {
    inner.mockImplementation(() => Promise.reject(tooManyRequests('30')));
    const api = useApi();

    await Promise.all([
      api('/api/v1/calendars').catch(() => {}),
      useApi()('/api/v1/addressbooks').catch(() => {}), // a second instance shares the latch
      api('/api/v1/calendars', { method: 'DELETE' }).catch(() => {}),
    ]);
    expect(toastMock.warn).toHaveBeenCalledTimes(1);

    await vi.advanceTimersByTimeAsync(30_000);
    await api('/api/v1/calendars').catch(() => {});
    expect(toastMock.warn).toHaveBeenCalledTimes(2);
  });

  it('leaves every other error untouched', async () => {
    const boom = Object.assign(new Error('[GET] "/x": 500 Internal Server Error'), { status: 500 });
    inner.mockRejectedValueOnce(boom);

    await expect(useApi()('/x')).rejects.toBe(boom);
    expect(boom.message).toBe('[GET] "/x": 500 Internal Server Error');
    expect(inner).toHaveBeenCalledTimes(1);
    expect(toastMock.warn).not.toHaveBeenCalled();
  });
});
