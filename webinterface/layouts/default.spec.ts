// @vitest-environment nuxt
import { afterEach, describe, expect, it } from 'vitest';
import { enableAutoUnmount, mount } from '@vue/test-utils';
import { defineComponent, h, nextTick } from 'vue';
import PrimeVue from 'primevue/config';
import ToastService from 'primevue/toastservice';
import DefaultLayout from './default.vue';
import ActionToast from '~/components/common/ActionToast.vue';
import { useAppToast } from '~/composables/useAppToast';

// SCOPE (#170 story 32, gap W3): the default layout is the one place that
// mounts <ActionToast />, the host of `useAppToast().action()` toasts. Without
// it a page can call action('Task completed', 'Undo', …) and nothing appears,
// so ticking a task would have no Undo (spec §9 Phase 3 "complete (undo
// works)"). The sidebar and header (stores, API) are stubbed; ActionToast,
// PrimeVue's Toast and the ToastService run for real.

enableAutoUnmount(afterEach);

const Stub = (name: string) => defineComponent({ name, render: () => h('div', { 'data-test': `stub-${name}` }) });

/** A page in the layout's slot that raises an action toast, as TaskList does after a tick. */
const Page = defineComponent({
  setup(_, { expose }) {
    expose({ appToast: useAppToast() });
    return () => h('p', { 'data-test': 'page' }, 'page');
  },
});

function mountLayout() {
  const wrapper = mount(DefaultLayout, {
    attachTo: document.body,
    slots: { default: Page },
    global: {
      plugins: [PrimeVue, ToastService],
      stubs: { AppSidebar: Stub('AppSidebar'), AppHeader: Stub('AppHeader'), ConfirmDialog: true, ProgressSpinner: true },
    },
  });
  const appToast = (wrapper.findComponent(Page).vm as unknown as { appToast: ReturnType<typeof useAppToast> }).appToast;
  return { wrapper, appToast };
}

describe('layouts/default — the action toast host', () => {
  it('mounts ActionToast, so an action toast ("Task completed" + Undo) appears with its button', async () => {
    const { wrapper, appToast } = mountLayout();
    expect(wrapper.find('[data-test="page"]').exists()).toBe(true);
    expect(wrapper.findComponent(ActionToast).exists()).toBe(true);

    let undone = 0;
    appToast.action('Task completed', 'Undo', () => { undone += 1; });
    await nextTick();

    const toast = document.querySelector<HTMLElement>('[data-test="action-toast"]');
    expect(toast, 'the action toast is rendered').not.toBeNull();
    expect(toast!.querySelector('[data-test="action-toast-summary"]')?.textContent).toBe('Task completed');
    const button = toast!.querySelector<HTMLButtonElement>('[data-test="action-toast-button"]')!;
    expect(button.textContent?.trim()).toBe('Undo');
    button.click();
    expect(undone).toBe(1);
  });
});
