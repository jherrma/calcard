/**
 * Reminder refresh hook (#169, spec §10.3). Called by the calendars and tasks stores after a
 * successful mutation and by Settings > Calendar after a time-zone change: the alarms store
 * then polls once, debounced (2 s), so a reminder created, changed or removed in the web UI is
 * scheduled or cancelled within seconds instead of at the next 5-minute poll.
 *
 * A lazy `import()`: a static import of the alarms store from the calendars/tasks stores would
 * close an import cycle (alarms -> calendars via `toRFC3339`). A no-op while the alarms store
 * is not running. Never throws and never delays the caller; stories 17 and 18 (reminder fields
 * in the forms) need nothing beyond the store calls that already call it.
 */
export function refreshAlarmsSoon(): void {
  void import('~/stores/alarms')
    .then(({ useAlarmsStore }) => useAlarmsStore().refreshSoon())
    .catch((e: unknown) => console.warn('Reminders: could not schedule a refresh', e));
}
