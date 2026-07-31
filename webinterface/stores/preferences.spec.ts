// @vitest-environment nuxt
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import { createTestingPinia } from '@pinia/testing';
import {
  usePreferencesStore,
  DEFAULT_PREFERENCES,
  PREF_DEFAULT_ALL_DAY,
  PREF_DEFAULT_EVENT_DURATION,
  PREF_TIME_FORMAT,
} from './preferences';

const { apiMock } = vi.hoisted(() => ({ apiMock: vi.fn() }));
mockNuxtImport('useApi', () => () => apiMock);

// GET/PATCH both return { preferences: {...} } AFTER useApi unwrapped the
// backend's { status: 'ok', data: ... } envelope.
function envelope(prefs: Record<string, string>) {
  return { preferences: prefs };
}

const fullMap = {
  [PREF_DEFAULT_EVENT_DURATION]: '60',
  [PREF_DEFAULT_ALL_DAY]: 'false',
  [PREF_TIME_FORMAT]: '24h',
};

beforeEach(() => {
  createTestingPinia({ stubActions: false });
  apiMock.mockReset();
});

describe('initial state', () => {
  it('starts on the defaults so getters are usable before the fetch resolves', () => {
    const store = usePreferencesStore();

    expect(store.preferences).toEqual(DEFAULT_PREFERENCES);
    expect(store.isLoaded).toBe(false);
    expect(store.defaultEventDuration).toBe(60);
    expect(store.defaultAllDay).toBe(false);
    expect(store.timeFormat).toBe('24h');
  });
});

describe('getters', () => {
  it('parses stored strings into a number / boolean / union', () => {
    const store = usePreferencesStore();
    store.preferences = {
      [PREF_DEFAULT_EVENT_DURATION]: '90',
      [PREF_DEFAULT_ALL_DAY]: 'true',
      [PREF_TIME_FORMAT]: '12h',
    };

    expect(store.defaultEventDuration).toBe(90);
    expect(store.defaultAllDay).toBe(true);
    expect(store.timeFormat).toBe('12h');
  });

  it.each([
    ['a value outside the allowed set', '37'],
    ['a non-numeric value', 'sixty'],
    ['an empty value', ''],
    ['a negative value', '-60'],
  ])('falls back to 60 minutes for %s', (_label, value) => {
    const store = usePreferencesStore();
    store.preferences = { ...DEFAULT_PREFERENCES, [PREF_DEFAULT_EVENT_DURATION]: value };

    expect(store.defaultEventDuration).toBe(60);
  });

  it('treats anything other than "true" as all-day off and anything but "12h" as 24h', () => {
    const store = usePreferencesStore();
    store.preferences = {
      ...DEFAULT_PREFERENCES,
      [PREF_DEFAULT_ALL_DAY]: 'yes',
      [PREF_TIME_FORMAT]: 'military',
    };

    expect(store.defaultAllDay).toBe(false);
    expect(store.timeFormat).toBe('24h');
  });

  it('falls back when a key is missing entirely', () => {
    const store = usePreferencesStore();
    store.preferences = {};

    expect(store.defaultEventDuration).toBe(60);
    expect(store.defaultAllDay).toBe(false);
    expect(store.timeFormat).toBe('24h');
  });
});

describe('fetchPreferences', () => {
  it('GETs the endpoint and merges the response onto the defaults', async () => {
    apiMock.mockResolvedValueOnce(envelope({ [PREF_TIME_FORMAT]: '12h' }));

    const store = usePreferencesStore();
    await store.fetchPreferences();

    expect(apiMock).toHaveBeenCalledWith('/api/v1/users/me/preferences');
    // Keys the server omitted keep their default rather than becoming undefined.
    expect(store.preferences).toEqual({ ...DEFAULT_PREFERENCES, [PREF_TIME_FORMAT]: '12h' });
    expect(store.timeFormat).toBe('12h');
    expect(store.isLoaded).toBe(true);
    expect(store.isLoading).toBe(false);
    expect(store.error).toBeNull();
  });

  it('records the error and rethrows, leaving the defaults in place', async () => {
    apiMock.mockRejectedValueOnce({ data: { message: 'boom' } });

    const store = usePreferencesStore();
    await expect(store.fetchPreferences()).rejects.toBeDefined();

    expect(store.error).toBe('boom');
    expect(store.isLoaded).toBe(false);
    expect(store.isLoading).toBe(false);
    expect(store.preferences).toEqual(DEFAULT_PREFERENCES);
  });
});

describe('ensureLoaded', () => {
  it('coalesces concurrent callers into a single request', async () => {
    apiMock.mockResolvedValue(envelope(fullMap));

    const store = usePreferencesStore();
    await Promise.all([store.ensureLoaded(), store.ensureLoaded(), store.ensureLoaded()]);

    expect(apiMock).toHaveBeenCalledTimes(1);
    expect(store.isLoaded).toBe(true);
  });

  it('does not refetch once loaded', async () => {
    apiMock.mockResolvedValue(envelope(fullMap));

    const store = usePreferencesStore();
    await store.ensureLoaded();
    await store.ensureLoaded();

    expect(apiMock).toHaveBeenCalledTimes(1);
  });

  it('never rejects on failure — the caller keeps working with defaults', async () => {
    apiMock.mockRejectedValueOnce(new Error('offline'));

    const store = usePreferencesStore();
    await expect(store.ensureLoaded()).resolves.toBeUndefined();

    expect(store.isLoaded).toBe(false);
    expect(store.error).toBe('Failed to load preferences');
    expect(store.defaultEventDuration).toBe(60);
  });

  it('retries after a failed load, since nothing was cached', async () => {
    apiMock
      .mockRejectedValueOnce(new Error('offline'))
      .mockResolvedValueOnce(envelope({ ...fullMap, [PREF_TIME_FORMAT]: '12h' }));

    const store = usePreferencesStore();
    await store.ensureLoaded();
    await store.ensureLoaded();

    expect(apiMock).toHaveBeenCalledTimes(2);
    expect(store.timeFormat).toBe('12h');
  });
});

// The SPA never reloads between a logout and the next login, so the cached map
// and the once-per-session latch must be droppable (story 103 review).
describe('reset', () => {
  it('clears the cache so the next ensureLoaded refetches for the new session', async () => {
    apiMock
      .mockResolvedValueOnce(envelope({ ...fullMap, [PREF_TIME_FORMAT]: '12h', [PREF_DEFAULT_EVENT_DURATION]: '30' }))
      .mockResolvedValueOnce(envelope(fullMap));

    const store = usePreferencesStore();
    await store.ensureLoaded();
    expect(store.timeFormat).toBe('12h');

    store.reset();

    // Back to defaults immediately — a component rendering between logout and
    // the next load must not still see the previous user's values.
    expect(store.preferences).toEqual(DEFAULT_PREFERENCES);
    expect(store.isLoaded).toBe(false);
    expect(store.isLoading).toBe(false);
    expect(store.error).toBeNull();

    await store.ensureLoaded();

    expect(apiMock).toHaveBeenCalledTimes(2);
    expect(store.timeFormat).toBe('24h');
    expect(store.defaultEventDuration).toBe(60);
  });

  it('discards a response that arrives after the reset (logout mid-request)', async () => {
    let resolve!: (v: unknown) => void;
    apiMock.mockReturnValueOnce(new Promise((r) => { resolve = r; }));

    const store = usePreferencesStore();
    const pending = store.ensureLoaded();

    // Logout happens while the GET is still in flight.
    store.reset();
    resolve(envelope({ ...fullMap, [PREF_TIME_FORMAT]: '12h' }));
    await pending;

    // The previous session's answer is dropped, latch included, so the next
    // caller fetches again instead of inheriting it.
    expect(store.preferences).toEqual(DEFAULT_PREFERENCES);
    expect(store.timeFormat).toBe('24h');
    expect(store.isLoaded).toBe(false);
    expect(store.isLoading).toBe(false);
  });
});

describe('updatePreferences', () => {
  it('PATCHes the given keys and adopts the returned map', async () => {
    apiMock.mockResolvedValueOnce(envelope({
      [PREF_DEFAULT_EVENT_DURATION]: '30',
      [PREF_DEFAULT_ALL_DAY]: 'true',
      [PREF_TIME_FORMAT]: '12h',
    }));

    const store = usePreferencesStore();
    await store.updatePreferences({
      [PREF_DEFAULT_EVENT_DURATION]: '30',
      [PREF_DEFAULT_ALL_DAY]: 'true',
      [PREF_TIME_FORMAT]: '12h',
    });

    expect(apiMock).toHaveBeenCalledWith('/api/v1/users/me/preferences', {
      method: 'PATCH',
      body: {
        preferences: {
          [PREF_DEFAULT_EVENT_DURATION]: '30',
          [PREF_DEFAULT_ALL_DAY]: 'true',
          [PREF_TIME_FORMAT]: '12h',
        },
      },
    });
    expect(store.defaultEventDuration).toBe(30);
    expect(store.defaultAllDay).toBe(true);
    expect(store.timeFormat).toBe('12h');
    expect(store.isLoaded).toBe(true);
  });

  it('rethrows a rejected update so the page can toast the server message', async () => {
    apiMock.mockRejectedValueOnce({ data: { message: 'invalid preference value' } });

    const store = usePreferencesStore();
    await expect(
      store.updatePreferences({ [PREF_DEFAULT_EVENT_DURATION]: '37' }),
    ).rejects.toBeDefined();

    // Local state is untouched: the server refused the write.
    expect(store.preferences).toEqual(DEFAULT_PREFERENCES);
  });
});
