import { usePreferencesStore, PREF_ACCENT_COLOR } from '~/stores/preferences';
import { useAuthStore } from '~/stores/auth';

/**
 * Accent colour (story 046) — the counterpart to `useTheme()`.
 *
 * Where the theme is a device-local choice, the accent is a **server**
 * preference: it has no reason to differ between a laptop and a phone, and
 * nothing needs it before login. `stores/preferences.ts` owns the value; this
 * composable owns *applying* it, and is the only thing that writes it.
 *
 * A singleton for the same reason `useTheme()` is one — it drives CSS custom
 * properties on `<html>` and a PrimeVue palette, both of which are per-document.
 *
 * There is a **device-local cache** on top of the server value, written on every
 * successful load or save. It is a paint hint only, never the source of truth:
 * without it, every reload would render blue until the preferences request
 * came back and then visibly repaint in the user's colour.
 */

const CACHE_KEY = 'calcard-accent';

/** What is currently on the document. */
const appliedAccent = ref(DEFAULT_ACCENT_COLOR);

let initialized = false;

function readCachedAccent(): string | null {
  try {
    const raw = window.localStorage.getItem(CACHE_KEY);
    return isAccentColor(raw) ? raw : null;
  } catch {
    return null;
  }
}

function writeCachedAccent(hex: string | null): void {
  try {
    if (hex === null) window.localStorage.removeItem(CACHE_KEY);
    else window.localStorage.setItem(CACHE_KEY, hex);
  } catch {
    // Storage unavailable. The accent still applies for this session; only the
    // head start on the next page load is lost.
  }
}

function apply(hex: string): void {
  if (hex === appliedAccent.value) return;
  appliedAccent.value = hex;
  applyAccentColor(hex);
}

/**
 * Applies the cached accent at once, then keeps the document in step with the
 * store. Idempotent: the plugin calls it at boot, components re-enter it.
 */
function initAccentColor(): void {
  if (initialized) return;
  initialized = true;

  const preferences = usePreferencesStore();
  const auth = useAuthStore();

  const cached = readCachedAccent();
  if (cached) apply(cached);

  // The store may ALREADY hold a loaded value — another page can have fetched
  // preferences before anything asked for the accent. The server value outranks
  // the cache, and the watcher below would never fire for a value that was
  // already there.
  if (preferences.isLoaded) {
    apply(preferences.accentColor);
    writeCachedAccent(preferences.accentColor);
  }

  watch(
    () => preferences.accentColor,
    (hex) => {
      apply(hex);
      // Only cache what the server actually told us. Caching the getter's
      // fallback would persist "blue" for a user whose preferences simply had
      // not loaded yet, and they would then see blue first on every reload.
      if (preferences.isLoaded) writeCachedAccent(hex);
    },
  );

  // Deliberately NOT immediate, and gated on the previous value.
  //
  // At boot `isAuthenticated` is still false — `initAuth()` has not finished
  // restoring the session yet — so an immediate run would read that as a logout
  // and wipe the cache on every single page load, defeating the entire point of
  // having one. Only a genuine true→false transition is a logout.
  watch(
    () => auth.isAuthenticated,
    (authenticated, wasAuthenticated) => {
      if (authenticated) {
        // Never rejects; a failed load leaves the cached/default accent up.
        preferences.ensureLoaded();
        return;
      }
      if (!wasAuthenticated) return;
      // Logged out — drop the cache so the next account on this device does not
      // inherit the previous one's colour, and go back to the default. The
      // store's own reset() (called from clearAuth) handles the state.
      writeCachedAccent(null);
      apply(DEFAULT_ACCENT_COLOR);
    },
  );

  // Covers the case the watcher cannot: a component reaching for the accent
  // when the session was already restored before this ran.
  if (auth.isAuthenticated) preferences.ensureLoaded();
}

export function useAccentColor() {
  initAccentColor();

  const preferences = usePreferencesStore();

  /**
   * Persists an accent. Applies it first so the UI responds to the click rather
   * than to the round-trip, then rolls back if the server refuses — the value
   * is free-form enough that a rejection is a real possibility, and leaving a
   * colour on screen that is not the one stored would be a lie.
   */
  async function setAccentColor(input: string): Promise<void> {
    const hex = normalizeAccentColor(input);
    if (!hex) throw new Error('Enter a colour like #3b82f6');

    const previous = appliedAccent.value;
    apply(hex);
    try {
      await preferences.updatePreferences({ [PREF_ACCENT_COLOR]: hex });
      writeCachedAccent(hex);
    } catch (e) {
      apply(previous);
      throw e;
    }
  }

  return {
    /** The accent currently on the document. */
    accentColor: readonly(appliedAccent),
    isDefaultAccent: computed(() => appliedAccent.value === DEFAULT_ACCENT_COLOR),
    setAccentColor,
  };
}
