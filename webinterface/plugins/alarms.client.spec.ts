// @vitest-environment nuxt
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createTestingPinia } from '@pinia/testing';
import { nextTick } from 'vue';

// SCOPE: plugins/alarms.client.ts starts the alarms store when the app becomes
// authenticated and stops it when it is not (immediate watch).

async function load(signedIn: boolean) {
  vi.resetModules();
  createTestingPinia({ stubActions: true });
  const { useAuthStore } = await import('~/stores/auth');
  const { useAlarmsStore } = await import('~/stores/alarms');
  const auth = useAuthStore();
  auth.isAuthenticated = signedIn;
  const alarms = useAlarmsStore();
  const plugin = (await import('./alarms.client')).default as any;
  await plugin(useNuxtApp());
  await nextTick();
  return { auth, alarms };
}

afterEach(() => vi.restoreAllMocks());
beforeEach(() => vi.spyOn(console, 'warn').mockImplementation(() => {}));

describe('alarms plugin', () => {
  it('does not start while signed out, and stops (idempotently) on the immediate watch', async () => {
    const { alarms } = await load(false);
    expect(alarms.start).not.toHaveBeenCalled();
    expect(alarms.stop).toHaveBeenCalledTimes(1);
  });

  it('starts at once for a session restored before the plugin ran', async () => {
    const { alarms } = await load(true);
    expect(alarms.start).toHaveBeenCalledTimes(1);
    expect(alarms.stop).not.toHaveBeenCalled();
  });

  it('starts on sign-in and stops on sign-out', async () => {
    const { auth, alarms } = await load(false);
    auth.isAuthenticated = true;
    await nextTick();
    expect(alarms.start).toHaveBeenCalledTimes(1);
    auth.isAuthenticated = false;
    await nextTick();
    expect(alarms.stop).toHaveBeenCalledTimes(2);
  });
});
