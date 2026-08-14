import type { ResolvedTheme, ThemeMode } from '~/utils/theme';

/**
 * Light / dark / system theming (story 046).
 *
 * Unlike the other composables here this one owns **module-level singleton
 * state**, because the theme is a property of the document, not of a component:
 * the header toggle, the settings page and the startup plugin all have to read
 * and write the same value. Every call after the first returns that same state.
 *
 * Deliberate deviation from the house style: the OS preference is read through a
 * hand-rolled `matchMedia` listener rather than vueuse's `usePreferredDark`.
 * The listener has to outlive every component (it is installed from a plugin,
 * outside any effect scope) and specs need to drive it deterministically, both
 * of which are easier to reason about with the raw API. `utils/theme.ts` holds
 * the DOM-facing pieces.
 */

/** The user's choice. Written only through `setTheme`. */
const themeModeState = ref<ThemeMode>(DEFAULT_THEME_MODE);

/** The OS preference, kept live by the `matchMedia` listener. */
const prefersDark = ref(false);

/** What `themeModeState` + `prefersDark` currently mean. */
const resolvedTheme = computed<ResolvedTheme>(() =>
  resolveTheme(themeModeState.value, prefersDark.value),
);

const isDark = computed(() => resolvedTheme.value === 'dark');

let initialized = false;
let transitionTimer: ReturnType<typeof setTimeout> | null = null;

/**
 * Marks the document as mid-switch for the length of the animation.
 *
 * The transition lives on a class that is added and removed rather than on a
 * permanent global rule, so the cost is paid only on the handful of frames after
 * a switch — and never during startup, where animating would reintroduce exactly
 * the flash the inline head script exists to prevent.
 */
function beginThemeTransition(): void {
  if (typeof document === 'undefined') return;
  const root = document.documentElement;
  root.classList.add(THEME_TRANSITION_CLASS);
  if (transitionTimer) clearTimeout(transitionTimer);
  transitionTimer = setTimeout(() => {
    root.classList.remove(THEME_TRANSITION_CLASS);
    transitionTimer = null;
  }, THEME_TRANSITION_MS);
}

/**
 * Tracks `prefers-color-scheme` for the lifetime of the app. Never detached:
 * the state it feeds is a singleton, so there is no point at which unsubscribing
 * would be correct.
 */
function listenForSystemPreference(): void {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return;
  const query = window.matchMedia(DARK_MEDIA_QUERY);
  const onChange = (event: MediaQueryListEvent) => {
    prefersDark.value = event.matches;
  };
  if (typeof query.addEventListener === 'function') {
    query.addEventListener('change', onChange);
  } else if (typeof query.addListener === 'function') {
    // Safari < 14 never got addEventListener on MediaQueryList.
    query.addListener(onChange);
  }
}

/**
 * Reads the persisted choice, syncs the document with it and starts watching.
 * Idempotent — the plugin calls it at startup and every component call re-enters
 * it, which must be a no-op.
 */
function initTheme(): void {
  if (initialized) return;
  initialized = true;

  // Read both inputs BEFORE the watchers exist, so restoring the stored value
  // does not count as a change and animate on first paint.
  themeModeState.value = readStoredThemeMode();
  prefersDark.value = prefersDarkScheme();

  // The inline head script has already done this, but it only runs on a full
  // document load. Re-applying makes the composable correct on its own terms
  // (and in specs, where no inline script ever ran).
  applyResolvedTheme(resolvedTheme.value);

  watch(resolvedTheme, (theme) => {
    beginThemeTransition();
    applyResolvedTheme(theme);
  });

  // Persistence hangs off the value rather than off `setTheme`, so it holds no
  // matter who does the writing.
  watch(themeModeState, (mode) => {
    writeStoredThemeMode(mode);
  });

  listenForSystemPreference();
}

export function useTheme() {
  initTheme();

  /**
   * Writable so `v-model` works on the settings page; the setter is the only
   * way in, which is what keeps persistence from being forgettable.
   */
  const themeMode = computed<ThemeMode>({
    get: () => themeModeState.value,
    set: (mode) => setTheme(mode),
  });

  function setTheme(mode: ThemeMode): void {
    // Guard the entry point rather than trusting callers: this value comes off
    // a clicked menu item and goes straight into localStorage.
    if (!isThemeMode(mode)) return;
    themeModeState.value = mode;
  }

  return {
    /** The user's choice: 'light' | 'dark' | 'system'. Assignable. */
    themeMode,
    /** That choice with 'system' collapsed to a concrete theme. */
    resolvedTheme,
    isDark,
    setTheme,
  };
}
