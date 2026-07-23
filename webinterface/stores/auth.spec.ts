// @vitest-environment nuxt
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import { createTestingPinia } from '@pinia/testing';
import { useAuthStore } from './auth';
import type { RefreshResponse } from '~/types/auth';

// One shared mock fetch; useApi() returns it, matching `const api = useApi(); await api(url, opts)`.
const { apiMock } = vi.hoisted(() => ({ apiMock: vi.fn() }));
mockNuxtImport('useApi', () => () => apiMock);

// A single shared cookie object so every useCookie('refresh_token') call in the
// store observes the same value — this lets us assert the store WRITES the
// rotated token back, and mirrors real cross-call cookie reads. cookieOptionCalls
// records the options each writer passes so we can assert the rotated cookie
// keeps its Secure/SameSite/maxAge attributes (#75).
const { cookieState, cookieOptionCalls } = vi.hoisted(() => ({
  cookieState: { value: null as string | null },
  cookieOptionCalls: [] as any[],
}));
mockNuxtImport('useCookie', () => (_name: string, opts?: any) => {
  if (opts) cookieOptionCalls.push(opts);
  return cookieState;
});

// navigateTo is a no-op in specs (we only exercise the success path here).
mockNuxtImport('navigateTo', () => () => {});

beforeEach(() => {
  createTestingPinia({ stubActions: false });
  apiMock.mockReset();
  cookieState.value = null;
  cookieOptionCalls.length = 0;
  vi.unstubAllGlobals();
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('refresh token rotation persistence (#75)', () => {
  it('persists the rotated refresh_token into the cookie and updates the access token', async () => {
    // navigator.locks present → the store runs the refresh under the lock.
    const request = vi.fn((_name: string, cb: () => Promise<void>) => cb());
    vi.stubGlobal('navigator', { locks: { request } });

    cookieState.value = 'refresh-token-1';
    const resp: RefreshResponse = {
      access_token: 'access-2',
      refresh_token: 'refresh-token-2',
      token_type: 'Bearer',
      expires_at: Math.floor(Date.now() / 1000) + 600,
    };
    apiMock.mockResolvedValueOnce(resp);

    const store = useAuthStore();
    await store.refreshToken();

    // The access token in memory is updated.
    expect(store.accessToken).toBe('access-2');
    // The rotated refresh token replaces the presented one in the cookie.
    expect(cookieState.value).toBe('refresh-token-2');
    // The write-back handle must carry the full cookie attributes — an
    // options-less write would downgrade the cookie to a session cookie.
    expect(
      cookieOptionCalls.some(
        (o) => o.sameSite === 'strict' && o.maxAge === 60 * 60 * 24 * 7,
      ),
    ).toBe(true);
    // The lock was actually used to serialize the refresh across tabs.
    expect(request).toHaveBeenCalledWith('caldav-auth-refresh', expect.any(Function));
    // The token presented to the backend was the ORIGINAL (pre-rotation) one.
    expect(apiMock).toHaveBeenCalledWith('/api/v1/auth/refresh', expect.objectContaining({
      method: 'POST',
      body: { refresh_token: 'refresh-token-1' },
      retry: false,
    }));
  });

  it('degrades gracefully when navigator.locks is unavailable (still rotates)', async () => {
    // No locks API (older browser / SSR) → the store refreshes directly.
    vi.stubGlobal('navigator', {});

    cookieState.value = 'refresh-token-1';
    apiMock.mockResolvedValueOnce({
      access_token: 'access-2',
      refresh_token: 'refresh-token-2',
      token_type: 'Bearer',
      expires_at: Math.floor(Date.now() / 1000) + 600,
    } satisfies RefreshResponse);

    const store = useAuthStore();
    await store.refreshToken();

    expect(store.accessToken).toBe('access-2');
    expect(cookieState.value).toBe('refresh-token-2');
    expect(apiMock).toHaveBeenCalledTimes(1);
  });

  it('single-flights concurrent refreshes into one in-flight request', async () => {
    vi.stubGlobal('navigator', {});
    cookieState.value = 'refresh-token-1';

    // A deferred response so both callers observe the same in-flight promise.
    let resolve!: (v: RefreshResponse) => void;
    apiMock.mockReturnValueOnce(new Promise<RefreshResponse>((r) => { resolve = r; }));

    const store = useAuthStore();
    const p1 = store.refreshToken();
    const p2 = store.refreshToken();

    // Second call reuses the first promise: no second network request yet.
    expect(apiMock).toHaveBeenCalledTimes(1);

    resolve({
      access_token: 'access-2',
      refresh_token: 'refresh-token-2',
      token_type: 'Bearer',
      expires_at: Math.floor(Date.now() / 1000) + 600,
    });
    await Promise.all([p1, p2]);

    // Exactly one refresh happened even though two callers raced.
    expect(apiMock).toHaveBeenCalledTimes(1);
    expect(cookieState.value).toBe('refresh-token-2');
  });
});
