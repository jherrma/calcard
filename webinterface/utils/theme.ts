/**
 * Theme primitives for story 046 — the single source of truth for *how* a theme
 * is stored and applied. `useTheme()` owns the reactive state; this module owns
 * the constants and the pure/DOM-poking functions underneath it.
 *
 * This file is imported by `nuxt.config.ts` as well as auto-imported at runtime,
 * because the inline flash-prevention script has to agree with the app about the
 * storage key and the class name. Keep it free of Nuxt/Vue imports and of any
 * top-level DOM access, or the config load breaks.
 *
 * ONE class drives everything: `.dark-mode` on `<html>`. PrimeVue is configured
 * for it (`darkModeSelector` in `nuxt.config.ts`), Tailwind is configured for it
 * (`darkMode: ['selector', '.dark-mode']` in `tailwind.config.ts`) and
 * `assets/css/fullcalendar.css` is written against it. Changing it means
 * changing all four.
 */

/** The modes the user can pick. `system` defers to the OS preference. */
export const THEME_MODES = ['light', 'dark', 'system'] as const;

export type ThemeMode = (typeof THEME_MODES)[number];

/** What `system` collapses to once the OS preference is known. */
export type ResolvedTheme = 'light' | 'dark';

export const DEFAULT_THEME_MODE: ThemeMode = 'system';

/** localStorage key holding the ThemeMode. Also read by the inline head script. */
export const THEME_STORAGE_KEY = 'calcard-theme';

/** Class toggled on `<html>`. Read by PrimeVue, Tailwind and fullcalendar.css. */
export const DARK_MODE_CLASS = 'dark-mode';

/**
 * Class held on `<html>` only for the moment of a user-initiated switch, so the
 * colour change can animate without paying for a permanent global transition.
 * Deliberately NOT applied during startup — that would animate the very flash
 * the inline script exists to prevent.
 */
export const THEME_TRANSITION_CLASS = 'theme-switching';

/** How long `THEME_TRANSITION_CLASS` stays on, in ms. Matches theme.css. */
export const THEME_TRANSITION_MS = 200;

export const DARK_MEDIA_QUERY = '(prefers-color-scheme: dark)';

export interface ThemeOption {
  value: ThemeMode;
  /** Menu/button label. */
  label: string;
  icon: string;
  /** One-line explanation for the settings page. */
  hint: string;
}

/**
 * The three choices, in the order they are offered. Shared by the header toggle
 * and the settings page so the two can never drift apart. Deliberately not
 * `readonly` — PrimeVue's `options` prop is typed as a mutable array.
 */
export const THEME_OPTIONS: ThemeOption[] = [
  { value: 'light', label: 'Light', icon: 'pi pi-sun', hint: 'Always use the light theme.' },
  { value: 'dark', label: 'Dark', icon: 'pi pi-moon', hint: 'Always use the dark theme.' },
  {
    value: 'system',
    label: 'System',
    icon: 'pi pi-desktop',
    hint: 'Follow your device setting, switching automatically when it does.',
  },
];

/** Narrows an unknown (a localStorage string, a query param) to a ThemeMode. */
export function isThemeMode(value: unknown): value is ThemeMode {
  return typeof value === 'string' && (THEME_MODES as readonly string[]).includes(value);
}

/** Collapses a mode plus the current OS preference into a concrete theme. */
export function resolveTheme(mode: ThemeMode, prefersDark: boolean): ResolvedTheme {
  if (mode === 'system') return prefersDark ? 'dark' : 'light';
  return mode;
}

/**
 * Current OS preference. Returns false when `matchMedia` is missing rather than
 * throwing — happy-dom in specs and old browsers both lack it, and "assume
 * light" is the safe answer for a preference we cannot read.
 */
export function prefersDarkScheme(): boolean {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return false;
  return window.matchMedia(DARK_MEDIA_QUERY).matches;
}

/**
 * Stored mode, or the default. Anything unrecognised is treated as absent: the
 * key is user-writable (devtools, a stale build, a hand-edited profile) and a
 * junk value must not leave the app unstyled.
 *
 * localStorage access is wrapped because *reading* it throws outright in Safari
 * private mode and when cookies are blocked — not merely returning null.
 */
export function readStoredThemeMode(): ThemeMode {
  try {
    const raw = window.localStorage.getItem(THEME_STORAGE_KEY);
    return isThemeMode(raw) ? raw : DEFAULT_THEME_MODE;
  } catch {
    return DEFAULT_THEME_MODE;
  }
}

/**
 * Persists the mode. A failure is swallowed: losing persistence is survivable
 * (the theme still applies for this session), but throwing out of a click
 * handler is not.
 */
export function writeStoredThemeMode(mode: ThemeMode): void {
  try {
    window.localStorage.setItem(THEME_STORAGE_KEY, mode);
  } catch {
    // Storage unavailable — the in-memory state is still correct.
  }
}

/**
 * Applies a resolved theme to the document.
 *
 * `color-scheme` is set alongside the class so the browser's own chrome follows
 * along: scrollbars, native form controls, the canvas behind the page and the
 * default text-selection colours. That is what covers the story's "scrollbars
 * styled for dark mode" criterion — no scrollbar CSS of our own required.
 */
export function applyResolvedTheme(theme: ResolvedTheme): void {
  if (typeof document === 'undefined') return;
  const root = document.documentElement;
  root.classList.toggle(DARK_MODE_CLASS, theme === 'dark');
  root.style.colorScheme = theme;
}
