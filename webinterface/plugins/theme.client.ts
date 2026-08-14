/**
 * Installs the theme at startup (story 046).
 *
 * `useTheme()` is self-initializing, so this exists to make sure that happens
 * once, app-wide, rather than whenever the first component that cares happens to
 * mount. Without it a route with no toggle on screen — the login page, a deep
 * link straight into /settings — would never start the `prefers-color-scheme`
 * listener, so a user on `system` would not follow their device until they
 * navigated somewhere that did.
 *
 * The inline head script in `nuxt.config.ts` has already put the right class on
 * <html> by now; this is what keeps it right afterwards.
 */
export default defineNuxtPlugin(() => {
  useTheme();
  // Same reasoning for the accent: it applies the cached colour before the
  // first Vue render and starts watching the preferences store, so the app
  // never paints blue and then repaints in the user's colour.
  useAccentColor();
});
