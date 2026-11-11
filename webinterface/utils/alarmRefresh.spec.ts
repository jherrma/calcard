// @vitest-environment nuxt
import { afterEach, describe, expect, it, vi } from 'vitest';
import { flushPromises } from '@vue/test-utils';
import { createTestingPinia } from '@pinia/testing';
import { useAlarmsStore } from '~/stores/alarms';
import { refreshAlarmsSoon } from './alarmRefresh';

// SCOPE: the lazy hook the stores and Settings call (#169, story 16): it reaches the alarms
// store's debounced refreshSoon(), and never throws or lets a rejection escape.

afterEach(() => {
  vi.restoreAllMocks();
});

describe('refreshAlarmsSoon', () => {
  it('calls refreshSoon() of the alarms store', async () => {
    createTestingPinia({ stubActions: true });
    const store = useAlarmsStore();
    refreshAlarmsSoon();
    await flushPromises();
    expect(store.refreshSoon).toHaveBeenCalledTimes(1);
  });

  it('returns at once and is a no-op for a store that is not running', async () => {
    createTestingPinia({ stubActions: false });
    const store = useAlarmsStore();
    expect(store.running).toBe(false);
    expect(refreshAlarmsSoon()).toBeUndefined();
    await flushPromises();
    expect(store.lastPollAt).toBeNull();
  });
});
