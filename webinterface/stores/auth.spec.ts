// @vitest-environment nuxt
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import { createTestingPinia } from '@pinia/testing';
import { useAuthStore } from './auth';
import type { LoginResponse, RefreshResponse } from '~/types/auth';

// A minimal, valid login response. expires_at is in the PAST by default so
// setAuth's scheduleTokenRefresh doesn't leave a dangling timer in unit tests.
function loginResponse(over: Partial<LoginResponse> = {}): LoginResponse {
  return {
    access_token: 'access-1',
    refresh_token: 'refresh-token-1',
    token_type: 'Bearer',
    expires_at: Math.floor(Date.now() / 1000) - 10,
    user: { id: '1', email: 'a@b.c', is_admin: false } as LoginResponse['user'],
    ...over,
  };
}

// One shared mock fetch; useApi() returns it, matching `const api = useApi(); await api(url, opts)`.
const { apiMock } = vi.hoisted(() => ({ apiMock: vi.fn() }));
mockNuxtImport('useApi', () => () => apiMock);

// Per-name cookie store: useCookie(name) returns a STABLE ref object per name, so
// refresh_token and remember_me are independent. This lets us assert the store
// writes the rotated token back AND that the "Remember me" choice (remember_me)
// survives a rotation. cookieOptionCalls records the options each writer passes
// (keyed by name) so we can assert Secure/SameSite/maxAge per cookie (#75, #19).
const { cookies, cookieOptionCalls } = vi.hoisted(() => ({
  cookies: new Map<string, { value: string | null }>(),
  cookieOptionCalls: [] as Array<{ name: string; opts: any }>,
}));
mockNuxtImport('useCookie', () => (name: string, opts?: any) => {
  if (opts) cookieOptionCalls.push({ name, opts });
  let ref = cookies.get(name);
  if (!ref) {
    ref = { value: null };
    cookies.set(name, ref);
  }
  return ref;
});

// navigateTo is a no-op in specs (we only exercise the success path here).
mockNuxtImport('navigateTo', () => () => {});

beforeEach(() => {
  createTestingPinia({ stubActions: false });
  apiMock.mockReset();
  cookies.clear();
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

    cookies.set('refresh_token', { value: 'refresh-token-1' });
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
    expect(cookies.get('refresh_token')?.value).toBe('refresh-token-2');
    // The write-back handle must carry the full cookie attributes — an
    // options-less write would downgrade the cookie to a session cookie.
    expect(
      cookieOptionCalls.some(
        ({ name, opts }) =>
          name === 'refresh_token' &&
          opts.sameSite === 'strict' &&
          opts.maxAge === 60 * 60 * 24 * 7,
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

    cookies.set('refresh_token', { value: 'refresh-token-1' });
    apiMock.mockResolvedValueOnce({
      access_token: 'access-2',
      refresh_token: 'refresh-token-2',
      token_type: 'Bearer',
      expires_at: Math.floor(Date.now() / 1000) + 600,
    } satisfies RefreshResponse);

    const store = useAuthStore();
    await store.refreshToken();

    expect(store.accessToken).toBe('access-2');
    expect(cookies.get('refresh_token')?.value).toBe('refresh-token-2');
    expect(apiMock).toHaveBeenCalledTimes(1);
  });

  it('single-flights concurrent refreshes into one in-flight request', async () => {
    vi.stubGlobal('navigator', {});
    cookies.set('refresh_token', { value: 'refresh-token-1' });

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
    expect(cookies.get('refresh_token')?.value).toBe('refresh-token-2');
  });
});

describe("'Remember me' cookie lifetime (#19)", () => {
  const SEVEN_DAYS = 60 * 60 * 24 * 7;
  const refreshWrites = () =>
    cookieOptionCalls.filter((c) => c.name === 'refresh_token');

  it('setAuth(remember=false) stores a SESSION cookie (no maxAge) and records the choice', () => {
    const store = useAuthStore();
    store.setAuth(loginResponse(), false);

    // The refresh token is written without a maxAge → a session cookie that the
    // browser drops on close (this is the whole point of "remember me = off").
    expect(refreshWrites().length).toBeGreaterThan(0);
    expect(refreshWrites().every((c) => c.opts.maxAge === undefined)).toBe(true);
    // The choice is persisted so rotations can preserve it.
    expect(cookies.get('remember_me')?.value).toBe('0');
  });

  it('setAuth(remember=true) persists a 7-day cookie', () => {
    const store = useAuthStore();
    store.setAuth(loginResponse(), true);

    expect(refreshWrites().some((c) => c.opts.maxAge === SEVEN_DAYS)).toBe(true);
    expect(cookies.get('remember_me')?.value).toBe('1');
  });

  it('login() defaults to a session cookie and never forwards "remember" to the backend', async () => {
    apiMock.mockResolvedValueOnce(loginResponse());
    const store = useAuthStore();

    await store.login({ email: 'a@b.c', password: 'pw' });

    // remember omitted → session cookie.
    expect(refreshWrites().every((c) => c.opts.maxAge === undefined)).toBe(true);
    expect(cookies.get('remember_me')?.value).toBe('0');
    // The login request body carries only credentials — no client-only flag.
    expect(apiMock).toHaveBeenCalledWith('/api/v1/auth/login', expect.objectContaining({
      method: 'POST',
      body: { email: 'a@b.c', password: 'pw' },
    }));
  });

  it('a rotation PRESERVES a remember=false session cookie (no silent upgrade to 7 days)', async () => {
    // This is the #75 interaction: performRefresh rewrites the cookie on every
    // rotation. A fixed 7-day write there would re-persist a session cookie.
    vi.stubGlobal('navigator', {});
    const store = useAuthStore();
    store.setAuth(loginResponse(), false); // remember_me='0', session refresh_token
    cookieOptionCalls.length = 0; // only inspect the rotation's writes

    apiMock.mockResolvedValueOnce({
      access_token: 'access-2',
      refresh_token: 'refresh-token-2',
      token_type: 'Bearer',
      expires_at: Math.floor(Date.now() / 1000) + 600,
    } satisfies RefreshResponse);

    await store.refreshToken();

    // The rotated refresh token is STILL a session cookie — the choice survived.
    expect(refreshWrites().length).toBeGreaterThan(0);
    expect(refreshWrites().every((c) => c.opts.maxAge === undefined)).toBe(true);
    expect(cookies.get('refresh_token')?.value).toBe('refresh-token-2');
  });

  it('a rotation KEEPS a remember=true cookie persistent (7 days)', async () => {
    vi.stubGlobal('navigator', {});
    const store = useAuthStore();
    store.setAuth(loginResponse(), true); // remember_me='1', persistent refresh_token
    cookieOptionCalls.length = 0;

    apiMock.mockResolvedValueOnce({
      access_token: 'access-2',
      refresh_token: 'refresh-token-2',
      token_type: 'Bearer',
      expires_at: Math.floor(Date.now() / 1000) + 600,
    } satisfies RefreshResponse);

    await store.refreshToken();

    expect(refreshWrites().some((c) => c.opts.maxAge === SEVEN_DAYS)).toBe(true);
    expect(cookies.get('refresh_token')?.value).toBe('refresh-token-2');
  });
});
