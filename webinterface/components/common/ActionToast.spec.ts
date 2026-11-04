// @vitest-environment nuxt
import { afterEach, describe, expect, it, vi } from 'vitest';
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils';
import { defineComponent, h, nextTick } from 'vue';
import PrimeVue from 'primevue/config';
import Toast from 'primevue/toast';
import ToastService from 'primevue/toastservice';
import ActionToast from './ActionToast.vue';
import { ACTION_TOAST_GROUP, ACTION_TOAST_LIFE, useAppToast } from '~/composables/useAppToast';

// SCOPE (#170 story 25): `useAppToast().action()` and the Toast host that
// renders it. "Task completed" + "Undo" for 5 s at the bottom centre; the
// button runs the action once and closes the toast; a new action toast
// replaces the previous one; the status toasts (success/error/…) never show
// up here. The real PrimeVue Toast and ToastService run.

enableAutoUnmount(afterEach);

afterEach(() => {
  vi.useRealTimers();
});

/** ActionToast plus a handle on useAppToast() from inside the app (it injects the toast service). */
const Harness = defineComponent({
  setup(_, { expose }) {
    expose({ appToast: useAppToast() });
    return () => h(ActionToast);
  },
});

function mountHarness() {
  const wrapper = mount(Harness, { attachTo: document.body, global: { plugins: [PrimeVue, ToastService] } });
  return { wrapper, appToast: (wrapper.vm as unknown as { appToast: ReturnType<typeof useAppToast> }).appToast };
}

const toasts = () => Array.from(document.querySelectorAll<HTMLElement>('[data-test="action-toast"]'));

describe('useAppToast().action + ActionToast', () => {
  it('shows the summary and the action button, at the bottom centre', async () => {
    const { appToast } = mountHarness();

    appToast.action('Task completed', 'Undo', () => {});
    await nextTick();

    expect(toasts()).toHaveLength(1);
    expect(document.querySelector('[data-test="action-toast-summary"]')?.textContent).toBe('Task completed');
    const button = document.querySelector<HTMLButtonElement>('[data-test="action-toast-button"]')!;
    expect(button.type).toBe('button');
    expect(button.textContent?.trim()).toBe('Undo');
    expect(document.querySelector('.p-toast')?.getAttribute('data-p')).toBe('bottom-center');
  });

  it('the button runs the action once and closes the toast', async () => {
    const { appToast } = mountHarness();
    const undo = vi.fn();
    appToast.action('Task completed', 'Undo', undo);
    await nextTick();

    document.querySelector<HTMLButtonElement>('[data-test="action-toast-button"]')!.click();
    await flushPromises();

    expect(undo).toHaveBeenCalledTimes(1);
    expect(toasts()).toHaveLength(0);
  });

  it('closes by itself after 5 s', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'Date'] });
    const { appToast } = mountHarness();
    appToast.action('Task completed', 'Undo', () => {});
    await nextTick();

    expect(ACTION_TOAST_LIFE).toBe(5000);
    vi.advanceTimersByTime(4999);
    await nextTick();
    expect(toasts()).toHaveLength(1);
    vi.advanceTimersByTime(1);
    await nextTick();
    expect(toasts()).toHaveLength(0);
  });

  it('a new action toast replaces the previous one', async () => {
    const { appToast } = mountHarness();
    const first = vi.fn();
    appToast.action('Task completed', 'Undo', first);
    await nextTick();
    appToast.action('Task completed', 'Undo', () => {});
    await nextTick();

    expect(toasts()).toHaveLength(1);
    document.querySelector<HTMLButtonElement>('[data-test="action-toast-button"]')!.click();
    expect(first).not.toHaveBeenCalled();
  });

  // Spec §8 "Status changes announced" (#170): "Task completed" (this action
  // toast) and "Task deleted" (a status toast of the layout's top-right
  // <Toast>) are announced by their toasts alone — TaskList adds no live
  // region for them. That holds only while the text sits inside PrimeVue's
  // message element, which is the live region (role="alert", aria-live).
  it('"Task completed" and "Task deleted" are announced: their text sits inside the toast\'s live region', async () => {
    const Layout = defineComponent({
      setup(_, { expose }) {
        expose({ appToast: useAppToast() });
        // The default layout's two toast hosts.
        return () => [h(Toast, { position: 'top-right' }), h(ActionToast)];
      },
    });
    const wrapper = mount(Layout, { attachTo: document.body, global: { plugins: [PrimeVue, ToastService] } });
    const appToast = (wrapper.vm as unknown as { appToast: ReturnType<typeof useAppToast> }).appToast;

    appToast.action('Task completed', 'Undo', () => {});
    appToast.success('Task deleted');
    await nextTick();

    /** The live region holding the element whose own text is TEXT. */
    const regionOf = (text: string) => {
      const el = Array.from(document.body.querySelectorAll<HTMLElement>('*'))
        .find((node) => node.children.length === 0 && node.textContent?.trim() === text);
      expect(el, `"${text}" is rendered`).toBeDefined();
      return el!.closest<HTMLElement>('[role="alert"]');
    };
    for (const text of ['Task completed', 'Task deleted']) {
      const region = regionOf(text);
      expect(region, `"${text}" is inside a role="alert" element`).not.toBeNull();
      expect(region!.classList.contains('p-toast-message')).toBe(true);
      expect(region!.getAttribute('aria-live')).toBe('assertive');
      expect(region!.getAttribute('aria-atomic')).toBe('true');
    }
    // Each in its own toast: the action toast at the bottom, the status toast at the top right.
    expect(regionOf('Task completed')!.closest('.p-toast')?.getAttribute('data-p')).toBe('bottom-center');
    expect(regionOf('Task deleted')!.closest('.p-toast')?.getAttribute('data-p')).toBe('top-right');
  });

  it('status toasts are not rendered here, action toasts carry the group', async () => {
    const { appToast } = mountHarness();

    appToast.success('Saved');
    await nextTick();
    expect(toasts()).toHaveLength(0);
    expect(ACTION_TOAST_GROUP).toBe('app-action');
  });
});
