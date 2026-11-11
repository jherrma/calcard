// @vitest-environment nuxt
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import { createTestingPinia } from '@pinia/testing';
import { nextTick } from 'vue';

// SCOPE: the set-if-empty PUT fired by plugins/timezone.client.ts when the app
// becomes authenticated: once per tab per signed-in period, body = browser zone,
// the answer lands in the preferences store, failures only warn.

const { apiMock } = vi.hoisted(() => ({ apiMock: vi.fn() }));
mockNuxtImport('useApi', () => () => apiMock);

const URL = '/api/v1/users/me/preferences/timezone-default';

async function load(opts: { signedIn: boolean }) {
  vi.resetModules();
  createTestingPinia({ stubActions: false });
  const { useAuthStore } = await import('~/stores/auth');
  const { usePreferencesStore } = await import('~/stores/preferences');
  const auth = useAuthStore();
  auth.isAuthenticated = opts.signedIn;
  const plugin = (await import('./timezone.client')).default as any;
  const nuxtApp = useNuxtApp();
  const result = await plugin(nuxtApp);
  await nextTick();
  return { auth, prefs: usePreferencesStore(), ready: result.provide.timezoneReady as () => Promise<void> };
}

const puts = () => apiMock.mock.calls.filter(([url]) => url === URL);

let warn: ReturnType<typeof vi.spyOn>;
beforeEach(() => {
  apiMock.mockReset();
  apiMock.mockImplementation(() => Promise.resolve({ timezone: 'Europe/Berlin' }));
  warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
  vi.spyOn(Intl, 'DateTimeFormat').mockImplementation(
    () => ({ resolvedOptions: () => ({ timeZone: 'Europe/Berlin' }) }) as unknown as Intl.DateTimeFormat,
  );
});
afterEach(() => vi.restoreAllMocks());

describe('timezone plugin', () => {
  it('sends nothing while signed out', async () => {
    await load({ signedIn: false });
    expect(puts()).toHaveLength(0);
  });

  it('sends one PUT with the browser zone after sign-in and stores the answer', async () => {
    apiMock.mockImplementation(() => Promise.resolve({ timezone: 'Asia/Tokyo' }));
    const { auth, prefs, ready } = await load({ signedIn: false });

    auth.isAuthenticated = true;
    await nextTick();
    await ready();

    expect(puts()).toHaveLength(1);
    expect(apiMock).toHaveBeenCalledWith(URL, { method: 'PUT', body: { timezone: 'Europe/Berlin' } });
    // The server keeps an already stored zone: its answer wins over the browser's.
    expect(prefs.timezone).toBe('Asia/Tokyo');
  });

  it('sends one PUT when a session is already restored at boot (immediate watch)', async () => {
    const { ready, prefs } = await load({ signedIn: true });
    await ready();

    expect(puts()).toHaveLength(1);
    expect(prefs.timezone).toBe('Europe/Berlin');
  });

  it('does not repeat while signed in, and sends once more after sign-out and sign-in', async () => {
    const { auth, ready } = await load({ signedIn: true });
    await ready();
    auth.isAuthenticated = true;
    await nextTick();
    expect(puts()).toHaveLength(1);

    auth.isAuthenticated = false;
    await nextTick();
    auth.isAuthenticated = true;
    await nextTick();
    await ready();
    expect(puts()).toHaveLength(2);
  });

  it.each([
    ['a 400', { status: 400, data: { message: 'invalid time zone' } }],
    ['a network error', new TypeError('Failed to fetch')],
  ])('only warns on %s, settles the promise, and does not retry', async (_n, err) => {
    apiMock.mockImplementation(() => Promise.reject(err));
    const { ready, prefs } = await load({ signedIn: true });

    await expect(ready()).resolves.toBeUndefined();
    expect(warn).toHaveBeenCalledTimes(1);
    expect(puts()).toHaveLength(1);
    expect(prefs.timezone).toBe('UTC');
  });
});
