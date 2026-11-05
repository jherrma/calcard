// @vitest-environment nuxt
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { mount, flushPromises } from '@vue/test-utils';
import { createTestingPinia } from '@pinia/testing';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import CalendarSettingsPage from './calendar.vue';
import {
  usePreferencesStore,
  DEFAULT_PREFERENCES,
  PREF_DEFAULT_ALL_DAY,
  PREF_DEFAULT_EVENT_DURATION,
  PREF_FIRST_DAY_OF_WEEK,
  PREF_TIME_FORMAT,
  PREF_TIMEZONE,
} from '~/stores/preferences';

// SCOPE: the first-day-of-the-week field of Settings → Calendar — that it is
// seeded from the server, offers exactly the values the server accepts, counts
// toward the dirty check and is saved with the rest of the form.

const { toastSpies } = vi.hoisted(() => ({
  toastSpies: { success: vi.fn(), error: vi.fn(), warn: vi.fn(), info: vi.fn() },
}));
mockNuxtImport('useAppToast', () => () => toastSpies);

const { apiMock } = vi.hoisted(() => ({ apiMock: vi.fn() }));
mockNuxtImport('useApi', () => () => apiMock);
// The reminder refresh hook (#169, story 16) is a lazy import of the alarms store: stand in for it.
const { refreshMock } = vi.hoisted(() => ({ refreshMock: vi.fn() }));
mockNuxtImport('refreshAlarmsSoon', () => refreshMock);

interface PageVm {
  form: { duration: number; allDay: boolean; timeFormat: string; firstDayOfWeek: number; timezone: string };
  isDirty: boolean;
  handleSave: () => Promise<void>;
  applyStoreValues: () => void;
}

/** What the server answers: every key, the given ones overriding the defaults. */
function serverPrefs(overrides: Record<string, string> = {}) {
  return { preferences: { ...DEFAULT_PREFERENCES, ...overrides } };
}

async function mountPage(stored: Record<string, string> = {}) {
  apiMock.mockImplementation((_url: string, opts?: { method?: string; body?: { preferences: Record<string, string> } }) =>
    Promise.resolve(opts?.method === 'PATCH' ? serverPrefs(opts.body?.preferences) : serverPrefs(stored)));
  const pinia = createTestingPinia({ stubActions: false });
  const wrapper = mount(CalendarSettingsPage, { shallow: true, global: { plugins: [pinia] } });
  await flushPromises();
  return { wrapper, vm: wrapper.vm as unknown as PageVm, store: usePreferencesStore() };
}

function firstDaySelect(wrapper: Awaited<ReturnType<typeof mountPage>>['wrapper']) {
  const select = wrapper
    .findAllComponents({ name: 'SelectButton' })
    .find((c) => c.attributes('aria-labelledby') === 'pref-first-day-label');
  expect(select, 'the first-day SelectButton must be rendered').toBeDefined();
  return select!;
}

/**
 * A bound prop of a stubbed PrimeVue component. SelectButton inherits its props
 * from a base component via `extends`, which the shallow stub does not copy, so
 * they arrive as attrs — keyed as written in the template — rather than props.
 */
function boundProp(component: ReturnType<typeof firstDaySelect>, name: string): unknown {
  const kebab = name.replace(/[A-Z]/g, (c) => `-${c.toLowerCase()}`);
  return component.props(name) ?? component.vm.$attrs[name] ?? component.vm.$attrs[kebab];
}

beforeEach(() => {
  vi.clearAllMocks();
  apiMock.mockReset();
});

describe('Settings → Calendar: first day of the week', () => {
  it('defaults to Sunday when the user never chose one', async () => {
    const { vm } = await mountPage();

    expect(apiMock).toHaveBeenCalledWith('/api/v1/users/me/preferences');
    expect(vm.form.firstDayOfWeek).toBe(0);
    expect(vm.isDirty).toBe(false);
  });

  it('seeds the field from the stored preference', async () => {
    const { vm } = await mountPage({ [PREF_FIRST_DAY_OF_WEEK]: '1' });

    expect(vm.form.firstDayOfWeek).toBe(1);
    expect(vm.isDirty).toBe(false);
  });

  it('offers Sunday, Monday and Saturday and says where the setting applies', async () => {
    const { wrapper } = await mountPage();

    const select = firstDaySelect(wrapper);
    // A required choice: deselecting must not be able to send an empty value.
    expect(boundProp(select, 'allowEmpty')).toBe(false);
    expect(boundProp(select, 'options')).toEqual([
      { label: 'Sunday', value: 0 },
      { label: 'Monday', value: 1 },
      { label: 'Saturday', value: 6 },
    ]);

    const help = wrapper.find('#pref-first-day-help').text();
    expect(help).toContain('calendar');
    expect(help).toContain('dashboard mini calendar');
  });

  it('marks the form dirty on a change, and Reset restores the saved value', async () => {
    const { vm } = await mountPage({ [PREF_FIRST_DAY_OF_WEEK]: '1' });

    vm.form.firstDayOfWeek = 6;
    expect(vm.isDirty).toBe(true);

    vm.applyStoreValues();
    expect(vm.form.firstDayOfWeek).toBe(1);
    expect(vm.isDirty).toBe(false);
  });

  it('saves the week start as a string alongside the other calendar keys', async () => {
    const { vm, store } = await mountPage();

    vm.form.firstDayOfWeek = 6;
    await vm.handleSave();

    expect(apiMock).toHaveBeenLastCalledWith('/api/v1/users/me/preferences', {
      method: 'PATCH',
      body: {
        preferences: {
          [PREF_DEFAULT_EVENT_DURATION]: '60',
          [PREF_DEFAULT_ALL_DAY]: 'false',
          [PREF_TIME_FORMAT]: '24h',
          [PREF_FIRST_DAY_OF_WEEK]: '6',
          [PREF_TIMEZONE]: 'UTC',
        },
      },
    });
    expect(store.firstDayOfWeek).toBe(6);
    expect(vm.form.firstDayOfWeek).toBe(6);
    expect(vm.isDirty).toBe(false);
    expect(toastSpies.success).toHaveBeenCalled();
  });

  it('keeps the unsaved choice and toasts the server message when the save is refused', async () => {
    const { vm, store } = await mountPage();
    apiMock.mockRejectedValueOnce({ data: { message: 'first_day_of_week must be one of 0, 1, 6' } });

    vm.form.firstDayOfWeek = 1;
    await vm.handleSave();

    expect(toastSpies.error).toHaveBeenCalledWith('first_day_of_week must be one of 0, 1, 6');
    expect(store.firstDayOfWeek).toBe(0);
    expect(vm.form.firstDayOfWeek).toBe(1);
    expect(vm.isDirty).toBe(true);
  });
});

describe('Settings → Calendar: time zone', () => {
  it('lists UTC and the Intl zones, and shows the stored value', async () => {
    const { wrapper, vm } = await mountPage({ [PREF_TIMEZONE]: 'Europe/Berlin' });
    const select = wrapper.findAllComponents({ name: 'Select' }).find((c) => c.attributes('id') === 'pref-timezone');
    expect(select, 'the time-zone Select must be rendered').toBeDefined();
    const options = (select!.props('options') ?? select!.vm.$attrs.options) as string[];

    expect(options[0]).toBe('UTC');
    expect(options).toContain('Europe/Berlin');
    expect(new Set(options).size).toBe(options.length);
    expect(vm.form.timezone).toBe('Europe/Berlin');
    expect(vm.isDirty).toBe(false);
  });

  it('saves a change through the PATCH', async () => {
    const { vm, store } = await mountPage();

    vm.form.timezone = 'America/New_York';
    expect(vm.isDirty).toBe(true);
    await vm.handleSave();

    const [url, opts] = apiMock.mock.calls.at(-1)!;
    expect(url).toBe('/api/v1/users/me/preferences');
    expect(opts.method).toBe('PATCH');
    expect(opts.body.preferences[PREF_TIMEZONE]).toBe('America/New_York');
    expect(store.timezone).toBe('America/New_York');
  });

  it('shows the server message when the zone is refused', async () => {
    const { vm, store } = await mountPage();
    apiMock.mockRejectedValueOnce({ data: { message: 'timezone must be a valid IANA time zone' } });

    vm.form.timezone = 'Mars/Base';
    await vm.handleSave();

    expect(toastSpies.error).toHaveBeenCalledWith('timezone must be a valid IANA time zone');
    expect(store.timezone).toBe('UTC');
  });
});

describe('reminder refresh on a time-zone change (#169)', () => {
  beforeEach(() => refreshMock.mockReset());

  it('asks for one refresh when the time zone changed', async () => {
    const { vm } = await mountPage();
    vm.form.timezone = 'America/New_York';
    await vm.handleSave();
    expect(refreshMock).toHaveBeenCalledTimes(1);
  });

  it('does not when only another preference changed', async () => {
    const { vm } = await mountPage();
    vm.form.duration = 45;
    await vm.handleSave();
    expect(refreshMock).not.toHaveBeenCalled();
  });

  it('does not when the save failed', async () => {
    const { vm } = await mountPage();
    apiMock.mockRejectedValueOnce({ data: { message: 'nope' } });
    vm.form.timezone = 'America/New_York';
    await vm.handleSave();
    expect(refreshMock).not.toHaveBeenCalled();
  });
});
