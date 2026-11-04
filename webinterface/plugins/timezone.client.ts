/**
 * Fills the user's `timezone` preference from the browser the first time they
 * are signed in (#169, story 02), "set-if-empty" on the server.
 *
 * One `PUT /api/v1/users/me/preferences/timezone-default` per tab and per
 * authenticated period: it fires when `isAuthenticated` turns true (password
 * sign-in, OAuth callback, or a session restored from the refresh cookie) and
 * the latch re-arms when it turns false. The server keeps an already stored
 * zone, so the answer — not the browser's zone — is what lands in the store.
 * Failures only `console.warn`: the zone is a convenience, never a blocker.
 *
 * `$timezoneReady()` returns the pending call's promise (it always resolves,
 * never rejects) so a caller that needs the stored zone can wait for it.
 */
export default defineNuxtPlugin(() => {
  const auth = useAuthStore();
  const preferences = usePreferencesStore();
  let sent = false;
  let pending: Promise<void> = Promise.resolve();

  const run = async () => {
    try {
      const timezone = Intl.DateTimeFormat().resolvedOptions().timeZone;
      const data = await useApi()<{ timezone?: string }>('/api/v1/users/me/preferences/timezone-default', {
        method: 'PUT',
        body: { timezone },
      });
      // A sign-out while in flight means the answer belongs to a session that is gone.
      if (auth.isAuthenticated && data?.timezone) preferences.setTimezone(data.timezone);
    } catch (e) {
      console.warn('Could not set the default time zone', e);
    }
  };

  watch(
    () => auth.isAuthenticated,
    (authenticated) => {
      if (!authenticated) {
        sent = false;
        return;
      }
      if (sent) return;
      sent = true;
      pending = run();
    },
    { immediate: true },
  );

  return { provide: { timezoneReady: () => pending } };
});
