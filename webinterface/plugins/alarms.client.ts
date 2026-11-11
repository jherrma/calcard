/**
 * Starts and stops the reminder store with the session (#169, spec §10.2):
 * `isAuthenticated` true -> `alarms.start()`, false -> `alarms.stop()`. The watch is
 * immediate so a session restored before the plugin ran is picked up.
 */
export default defineNuxtPlugin(() => {
  const auth = useAuthStore();
  const alarms = useAlarmsStore();

  watch(
    () => auth.isAuthenticated,
    (authenticated) => {
      if (authenticated) void alarms.start();
      else alarms.stop();
    },
    { immediate: true },
  );
});
