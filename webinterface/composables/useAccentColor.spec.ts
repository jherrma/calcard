// @vitest-environment nuxt
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { nextTick } from 'vue';
import { createTestingPinia } from '@pinia/testing';
import { installMemoryStorage, installThrowingStorage } from '~/test/support/storage';
import { DEFAULT_ACCENT_COLOR } from '~/utils/accent';
import { usePreferencesStore, PREF_ACCENT_COLOR } from '~/stores/preferences';
import { useAuthStore } from '~/stores/auth';

// SCOPE: how the accent gets from the server onto the document, and back.
//
// Two things here are easy to get wrong and expensive to get wrong:
//
//  1. The device-local CACHE is a paint hint, not a source of truth. It must be
//     written only from a loaded server value, dropped on logout so the next
//     account on a shared machine does not inherit a colour, and — the subtle
//     one — NOT dropped at boot, when `isAuthenticated` is still false simply
//     because initAuth() has not run yet.
//  2. A save applies optimistically and must ROLL BACK if the server refuses,
//     or the screen would be showing a colour the account does not have.
//
// useAccentColor() is a singleton like useTheme(), so every test loads a fresh
// module via vi.resetModules() + dynamic import.

const CACHE_KEY = 'calcard-accent';
let storage: ReturnType<typeof installMemoryStorage>;

const accentVar = () => document.documentElement.style.getPropertyValue('--accent-500');

/** A fresh module instance with pinia already active, so stores resolve. */
async function loadAccent(opts: { authenticated?: boolean; stored?: string } = {}) {
  vi.resetModules();
  // createTestingPinia sets the active pinia itself. Importing `pinia`
  // directly to call setActivePinia does not resolve under vue-tsc.
  createTestingPinia({ stubActions: true, createSpy: vi.fn });

  const auth = useAuthStore();
  auth.isAuthenticated = opts.authenticated ?? false;

  const preferences = usePreferencesStore();
  if (opts.stored) {
    preferences.preferences = { ...preferences.preferences, [PREF_ACCENT_COLOR]: opts.stored };
    preferences.isLoaded = true;
  }

  const mod = await import('./useAccentColor');
  return { ...mod, auth, preferences };
}

beforeEach(() => {
  storage = installMemoryStorage();
  document.documentElement.style.removeProperty('--accent-500');
});

afterEach(() => {
  storage.restore();
});

describe('useAccentColor — startup', () => {
  it('defaults to blue with nothing cached and nobody logged in', async () => {
    const { useAccentColor } = await loadAccent();
    const { accentColor, isDefaultAccent } = useAccentColor();

    expect(accentColor.value).toBe(DEFAULT_ACCENT_COLOR);
    expect(isDefaultAccent.value).toBe(true);
  });

  it('paints the cached accent immediately, before any request', async () => {
    storage.data.set(CACHE_KEY, '#8b5cf6');
    const { useAccentColor } = await loadAccent();

    expect(useAccentColor().accentColor.value).toBe('#8b5cf6');
    expect(accentVar()).toBe('139 92 246');
  });

  it('keeps the cache when the session has not been restored yet', async () => {
    // The regression this guards: `isAuthenticated` is false at boot because
    // initAuth() runs later. Reading that as a logout wiped the cache on every
    // page load, so the app always painted blue first and then repainted.
    storage.data.set(CACHE_KEY, '#8b5cf6');
    const { useAccentColor } = await loadAccent({ authenticated: false });
    useAccentColor();
    await nextTick();

    expect(storage.data.get(CACHE_KEY)).toBe('#8b5cf6');
    expect(useAccentColor().accentColor.value).toBe('#8b5cf6');
  });

  it('ignores a junk cache rather than applying it', async () => {
    storage.data.set(CACHE_KEY, 'rebeccapurple');
    const { useAccentColor } = await loadAccent();
    expect(useAccentColor().accentColor.value).toBe(DEFAULT_ACCENT_COLOR);
  });

  it('survives a localStorage that throws on access', async () => {
    const restore = installThrowingStorage();
    try {
      const { useAccentColor } = await loadAccent();
      expect(() => useAccentColor()).not.toThrow();
      expect(useAccentColor().accentColor.value).toBe(DEFAULT_ACCENT_COLOR);
    } finally {
      restore();
    }
  });

  it('loads preferences when the session is already restored', async () => {
    const { useAccentColor, preferences } = await loadAccent({ authenticated: true });
    useAccentColor();
    expect(preferences.ensureLoaded).toHaveBeenCalled();
  });
});

describe('useAccentColor — following the store', () => {
  it('applies and caches a colour once the server value lands', async () => {
    const { useAccentColor, preferences } = await loadAccent();
    const { accentColor } = useAccentColor();

    preferences.preferences = { ...preferences.preferences, [PREF_ACCENT_COLOR]: '#16a34a' };
    preferences.isLoaded = true;
    await nextTick();

    expect(accentColor.value).toBe('#16a34a');
    expect(accentVar()).toBe('22 163 74');
    expect(storage.data.get(CACHE_KEY)).toBe('#16a34a');
  });

  it('does not cache the getter fallback before preferences have loaded', async () => {
    const { useAccentColor, preferences } = await loadAccent();
    useAccentColor();

    // isLoaded stays false: the store is still serving defaults, and persisting
    // "blue" here would make every future reload paint blue first.
    preferences.preferences = { ...preferences.preferences, [PREF_ACCENT_COLOR]: '#16a34a' };
    await nextTick();

    expect(storage.data.has(CACHE_KEY)).toBe(false);
  });

  it('loads preferences when the session is restored after boot', async () => {
    const { useAccentColor, auth, preferences } = await loadAccent({ authenticated: false });
    useAccentColor();

    auth.isAuthenticated = true;
    await nextTick();

    expect(preferences.ensureLoaded).toHaveBeenCalled();
  });

  it('drops the cache and reverts to blue on a real logout', async () => {
    const { useAccentColor, auth } = await loadAccent({
      authenticated: true,
      stored: '#8b5cf6',
    });
    const { accentColor } = useAccentColor();
    await nextTick();
    expect(accentColor.value).toBe('#8b5cf6');

    auth.isAuthenticated = false;
    await nextTick();

    // Otherwise the next account signing in on this device would briefly wear
    // the previous one's colour.
    expect(storage.data.has(CACHE_KEY)).toBe(false);
    expect(accentColor.value).toBe(DEFAULT_ACCENT_COLOR);
  });
});

describe('useAccentColor — saving', () => {
  it('normalizes, applies and persists', async () => {
    const { useAccentColor, preferences } = await loadAccent({ authenticated: true });
    const { setAccentColor, accentColor } = useAccentColor();

    await setAccentColor('  #8B5CF6 ');

    expect(accentColor.value).toBe('#8b5cf6');
    expect(accentVar()).toBe('139 92 246');
    expect(preferences.updatePreferences).toHaveBeenCalledWith({ [PREF_ACCENT_COLOR]: '#8b5cf6' });
    expect(storage.data.get(CACHE_KEY)).toBe('#8b5cf6');
  });

  it('expands shorthand before sending, because the server rejects it', async () => {
    const { useAccentColor, preferences } = await loadAccent({ authenticated: true });
    await useAccentColor().setAccentColor('fff');
    expect(preferences.updatePreferences).toHaveBeenCalledWith({ [PREF_ACCENT_COLOR]: '#ffffff' });
  });

  it('rejects an unparseable colour without touching the document', async () => {
    const { useAccentColor, preferences } = await loadAccent({ authenticated: true });
    const { setAccentColor, accentColor } = useAccentColor();

    await expect(setAccentColor('rebeccapurple')).rejects.toThrow();
    expect(preferences.updatePreferences).not.toHaveBeenCalled();
    expect(accentColor.value).toBe(DEFAULT_ACCENT_COLOR);
  });

  it('rolls back when the server refuses the write', async () => {
    const { useAccentColor, preferences } = await loadAccent({ authenticated: true });
    const { setAccentColor, accentColor } = useAccentColor();
    const boom = new Error('400');
    vi.mocked(preferences.updatePreferences).mockRejectedValueOnce(boom);

    await expect(setAccentColor('#8b5cf6')).rejects.toThrow(boom);

    // The screen must not keep showing a colour the account does not have.
    expect(accentColor.value).toBe(DEFAULT_ACCENT_COLOR);
    expect(accentVar()).toBe('59 130 246');
    expect(storage.data.has(CACHE_KEY)).toBe(false);
  });

  it('rolls back to the PREVIOUS colour, not to blue', async () => {
    const { useAccentColor, preferences } = await loadAccent({
      authenticated: true,
      stored: '#16a34a',
    });
    const { setAccentColor, accentColor } = useAccentColor();
    await nextTick();
    expect(accentColor.value).toBe('#16a34a');

    vi.mocked(preferences.updatePreferences).mockRejectedValueOnce(new Error('400'));
    await expect(setAccentColor('#8b5cf6')).rejects.toThrow();

    expect(accentColor.value).toBe('#16a34a');
    expect(accentVar()).toBe('22 163 74');
  });
});
