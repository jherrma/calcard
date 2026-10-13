import type { PrimeVueConfiguration } from 'primevue/config';
import { usePreferencesStore } from '~/stores/preferences';

/**
 * Keeps PrimeVue's DatePicker popups on the user's first day of the week.
 *
 * A DatePicker lays its month out from the GLOBAL PrimeVue locale
 * (`config.locale.firstDayOfWeek`, Sunday unless told otherwise) and has no prop
 * for it, so mirroring the preference into that one field is what makes the
 * event form's, the contact form's and every other picker agree with the
 * calendar page and the dashboard mini calendar. PrimeVue's config is reactive,
 * so a picker that is already open re-renders on a change.
 *
 * Started once at boot by `plugins/first-day-of-week.client.ts`. The field is
 * written in place and the locale object never replaced: it also carries the
 * day and month names the pickers render.
 */
export function useDatePickerFirstDay(config: PrimeVueConfiguration | undefined) {
  const preferences = usePreferencesStore();
  return watch(
    () => preferences.firstDayOfWeek,
    (day) => {
      if (config?.locale) config.locale.firstDayOfWeek = day;
    },
    { immediate: true },
  );
}
