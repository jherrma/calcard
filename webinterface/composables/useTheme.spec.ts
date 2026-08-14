// @vitest-environment nuxt
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { nextTick } from 'vue';
import { DARK_MODE_CLASS, THEME_STORAGE_KEY, THEME_TRANSITION_CLASS } from '~/utils/theme';
import { installMemoryStorage, installThrowingStorage } from '~/test/support/storage';

// SCOPE: the reactive half of story 046 — what useTheme() does to the document
// and to localStorage. The pure helpers it stands on are covered in
// utils/theme.spec.ts.
//
// useTheme() holds MODULE-LEVEL singleton state and initializes itself exactly
// once, so every test here loads a fresh copy of the module via vi.resetModules()
// + dynamic import. Calling useTheme() twice in one module would (correctly) skip
// initialization the second time and the tests would leak into each other.

let systemPrefersDark = false;
let mediaListeners: ((event: MediaQueryListEvent) => void)[] = [];
let originalMatchMedia: PropertyDescriptor | undefined;
let storage: ReturnType<typeof installMemoryStorage>;

/**
 * Replaces window.matchMedia with one whose answer we control and whose change
 * event we can fire, so "the user changed their OS theme while the app was open"
 * is testable. happy-dom ships a matchMedia, but nothing can move it.
 */
function installMatchMedia() {
  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    value: (query: string) => ({
      media: query,
      get matches() {
        return systemPrefersDark;
      },
      addEventListener: (_: string, listener: (event: MediaQueryListEvent) => void) => {
        mediaListeners.push(listener);
      },
      removeEventListener: (_: string, listener: (event: MediaQueryListEvent) => void) => {
        mediaListeners = mediaListeners.filter(l => l !== listener);
      },
    }),
  });
}

/** Fires a prefers-color-scheme change at whoever subscribed. */
async function setSystemDark(matches: boolean) {
  systemPrefersDark = matches;
  for (const listener of mediaListeners) {
    listener({ matches } as MediaQueryListEvent);
  }
  await nextTick();
}

/** A fresh module instance, so the singleton starts uninitialized every time. */
async function loadTheme() {
  vi.resetModules();
  return await import('./useTheme');
}

const isDarkOnDocument = () => document.documentElement.classList.contains(DARK_MODE_CLASS);

beforeEach(() => {
  originalMatchMedia ??= Object.getOwnPropertyDescriptor(window, 'matchMedia');
  systemPrefersDark = false;
  mediaListeners = [];
  installMatchMedia();
  storage = installMemoryStorage();
  document.documentElement.classList.remove(DARK_MODE_CLASS, THEME_TRANSITION_CLASS);
  document.documentElement.style.colorScheme = '';
});

afterEach(() => {
  vi.useRealTimers();
  storage.restore();
  if (originalMatchMedia) Object.defineProperty(window, 'matchMedia', originalMatchMedia);
});

describe('useTheme — initialization', () => {
  it('defaults to system and resolves light when the device is light', async () => {
    const { useTheme } = await loadTheme();
    const { themeMode, resolvedTheme, isDark } = useTheme();

    expect(themeMode.value).toBe('system');
    expect(resolvedTheme.value).toBe('light');
    expect(isDark.value).toBe(false);
    expect(isDarkOnDocument()).toBe(false);
  });

  it('resolves dark under system when the device is dark, and marks the document', async () => {
    systemPrefersDark = true;
    const { useTheme } = await loadTheme();
    const { resolvedTheme } = useTheme();

    expect(resolvedTheme.value).toBe('dark');
    expect(isDarkOnDocument()).toBe(true);
    expect(document.documentElement.style.colorScheme).toBe('dark');
  });

  it('restores a stored explicit mode over the device preference', async () => {
    systemPrefersDark = true;
    storage.data.set(THEME_STORAGE_KEY, 'light');
    const { useTheme } = await loadTheme();
    const { themeMode, resolvedTheme } = useTheme();

    expect(themeMode.value).toBe('light');
    expect(resolvedTheme.value).toBe('light');
    expect(isDarkOnDocument()).toBe(false);
  });

  it('ignores a junk stored mode and falls back to system', async () => {
    storage.data.set(THEME_STORAGE_KEY, 'neon');
    const { useTheme } = await loadTheme();
    expect(useTheme().themeMode.value).toBe('system');
  });

  it('does not animate the initial paint', async () => {
    // The inline head script has already applied the right theme by this point.
    // Animating here would mean transitioning FROM the wrong colours, i.e. the
    // very flash the script exists to prevent.
    systemPrefersDark = true;
    const { useTheme } = await loadTheme();
    useTheme();
    await nextTick();
    expect(document.documentElement.classList.contains(THEME_TRANSITION_CLASS)).toBe(false);
  });

  it('shares one state across calls instead of re-reading storage', async () => {
    const { useTheme } = await loadTheme();
    const first = useTheme();
    first.setTheme('dark');
    await nextTick();

    const second = useTheme();
    expect(second.themeMode.value).toBe('dark');
    expect(second.isDark.value).toBe(true);
  });
});

describe('useTheme — switching', () => {
  it('applies and persists an explicit choice', async () => {
    const { useTheme } = await loadTheme();
    const { setTheme, resolvedTheme } = useTheme();

    setTheme('dark');
    await nextTick();

    expect(resolvedTheme.value).toBe('dark');
    expect(isDarkOnDocument()).toBe(true);
    expect(storage.data.get(THEME_STORAGE_KEY)).toBe('dark');
  });

  it('switches back to light, clearing the class', async () => {
    storage.data.set(THEME_STORAGE_KEY, 'dark');
    const { useTheme } = await loadTheme();
    const { setTheme } = useTheme();
    expect(isDarkOnDocument()).toBe(true);

    setTheme('light');
    await nextTick();

    expect(isDarkOnDocument()).toBe(false);
    expect(document.documentElement.style.colorScheme).toBe('light');
    expect(storage.data.get(THEME_STORAGE_KEY)).toBe('light');
  });

  it('accepts an assignment through the writable themeMode (v-model on the settings page)', async () => {
    const { useTheme } = await loadTheme();
    const { themeMode } = useTheme();

    themeMode.value = 'dark';
    await nextTick();

    // Assigning must go through the same choke point as setTheme, or the
    // settings page would change the theme without persisting it.
    expect(isDarkOnDocument()).toBe(true);
    expect(storage.data.get(THEME_STORAGE_KEY)).toBe('dark');
  });

  it('ignores an invalid mode rather than writing it to storage', async () => {
    const { useTheme } = await loadTheme();
    const { setTheme, themeMode } = useTheme();

    setTheme('neon' as never);
    await nextTick();

    expect(themeMode.value).toBe('system');
    expect(storage.data.has(THEME_STORAGE_KEY)).toBe(false);
  });

  it('animates a user-initiated switch and cleans the class up afterwards', async () => {
    vi.useFakeTimers();
    const { useTheme } = await loadTheme();
    const { setTheme } = useTheme();

    setTheme('dark');
    await nextTick();
    expect(document.documentElement.classList.contains(THEME_TRANSITION_CLASS)).toBe(true);

    vi.runAllTimers();
    // Left on, the rule would animate every hover and every route change.
    expect(document.documentElement.classList.contains(THEME_TRANSITION_CLASS)).toBe(false);
  });

  it('still applies the theme when localStorage refuses the write', async () => {
    const { useTheme } = await loadTheme();
    const { setTheme, resolvedTheme } = useTheme();

    const restore = installThrowingStorage();
    try {
      expect(() => setTheme('dark')).not.toThrow();
      await nextTick();
      // Losing persistence is survivable; losing the theme for this session is not.
      expect(resolvedTheme.value).toBe('dark');
      expect(isDarkOnDocument()).toBe(true);
    } finally {
      restore();
    }
  });
});

describe('useTheme — following the device', () => {
  it('flips live when the OS preference changes under system', async () => {
    const { useTheme } = await loadTheme();
    const { resolvedTheme } = useTheme();
    expect(resolvedTheme.value).toBe('light');

    await setSystemDark(true);

    expect(resolvedTheme.value).toBe('dark');
    expect(isDarkOnDocument()).toBe(true);
  });

  it('leaves an explicit choice alone when the OS preference changes', async () => {
    storage.data.set(THEME_STORAGE_KEY, 'light');
    const { useTheme } = await loadTheme();
    const { resolvedTheme } = useTheme();

    await setSystemDark(true);

    expect(resolvedTheme.value).toBe('light');
    expect(isDarkOnDocument()).toBe(false);
  });

  it('picks the device preference back up when the user returns to system', async () => {
    storage.data.set(THEME_STORAGE_KEY, 'light');
    const { useTheme } = await loadTheme();
    const { setTheme, resolvedTheme } = useTheme();

    await setSystemDark(true);
    expect(resolvedTheme.value).toBe('light');

    setTheme('system');
    await nextTick();

    // The OS change arrived while the mode was explicit; going back to system
    // must honour the CURRENT device state, not the one seen at startup.
    expect(resolvedTheme.value).toBe('dark');
    expect(isDarkOnDocument()).toBe(true);
  });

  it('survives a browser with no matchMedia at all', async () => {
    Object.defineProperty(window, 'matchMedia', { configurable: true, value: undefined });
    const { useTheme } = await loadTheme();

    expect(() => useTheme()).not.toThrow();
    expect(useTheme().resolvedTheme.value).toBe('light');
  });
});
