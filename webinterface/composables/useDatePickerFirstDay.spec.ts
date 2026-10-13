// @vitest-environment nuxt
import { describe, it, expect, beforeEach } from 'vitest';
import { nextTick, reactive } from 'vue';
import { createTestingPinia } from '@pinia/testing';
import { defaultOptions } from 'primevue/config';
import type { PrimeVueConfiguration } from 'primevue/config';
import { useDatePickerFirstDay } from './useDatePickerFirstDay';
import { usePreferencesStore, DEFAULT_PREFERENCES, PREF_FIRST_DAY_OF_WEEK } from '~/stores/preferences';

// SCOPE: PrimeVue's DatePicker takes its week start from the GLOBAL locale, so
// the preference has to be mirrored there or every date picker stays
// Sunday-first while the calendar shows Monday-first. Each test uses its own
// reactive copy of PrimeVue's default config (the real one is reactive too).

const DAY_NAMES_MIN = [...defaultOptions.locale!.dayNamesMin];

function primeVueConfig(): PrimeVueConfiguration {
  return reactive({ locale: JSON.parse(JSON.stringify(defaultOptions.locale)) });
}

beforeEach(() => {
  createTestingPinia({ stubActions: false });
});

describe('useDatePickerFirstDay', () => {
  it('writes the current preference at once', () => {
    const preferences = usePreferencesStore();
    preferences.preferences = { ...DEFAULT_PREFERENCES, [PREF_FIRST_DAY_OF_WEEK]: '1' };
    const config = primeVueConfig();

    const stop = useDatePickerFirstDay(config);

    expect(config.locale!.firstDayOfWeek).toBe(1);
    stop();
  });

  it('follows a change, and a reset (logout) back to Sunday', async () => {
    const preferences = usePreferencesStore();
    const config = primeVueConfig();
    const stop = useDatePickerFirstDay(config);
    expect(config.locale!.firstDayOfWeek).toBe(0);

    preferences.preferences = { ...preferences.preferences, [PREF_FIRST_DAY_OF_WEEK]: '6' };
    await nextTick();
    expect(config.locale!.firstDayOfWeek).toBe(6);

    preferences.reset();
    await nextTick();
    expect(config.locale!.firstDayOfWeek).toBe(0);
    stop();
  });

  it('changes only the week start, keeping the day names the pickers render', async () => {
    const preferences = usePreferencesStore();
    const config = primeVueConfig();
    const locale = config.locale;
    const stop = useDatePickerFirstDay(config);

    preferences.preferences = { ...preferences.preferences, [PREF_FIRST_DAY_OF_WEEK]: '1' };
    await nextTick();

    expect(config.locale).toBe(locale);
    expect(config.locale!.dayNamesMin).toEqual(DAY_NAMES_MIN);
    stop();
  });

  it('does nothing without a PrimeVue config instead of throwing', () => {
    expect(() => useDatePickerFirstDay(undefined)()).not.toThrow();
  });
});
