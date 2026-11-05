// @vitest-environment nuxt
import { afterEach, describe, expect, it } from 'vitest';
import { enableAutoUnmount, mount } from '@vue/test-utils';
import { defineComponent, h } from 'vue';
import PrimeVue from 'primevue/config';
import ToastService from 'primevue/toastservice';
import SettingsLayout from './settings.vue';
import ReminderToast from '~/components/common/ReminderToast.vue';

// SCOPE (#169 story 16): the settings layout, like the default one, mounts the reminder toast
// host, so a due reminder shows on the settings pages too.

enableAutoUnmount(afterEach);

const Stub = (name: string) => defineComponent({ name, render: () => h('div', { 'data-test': `stub-${name}` }) });

describe('layouts/settings', () => {
  it('mounts the reminder toast host', () => {
    const wrapper = mount(SettingsLayout, {
      slots: { default: () => h('p', 'page') },
      global: {
        plugins: [PrimeVue, ToastService],
        stubs: {
          AppSidebar: Stub('AppSidebar'),
          AppHeader: Stub('AppHeader'),
          ConfirmDialog: true,
          ProgressSpinner: true,
          NuxtLink: true,
          ReminderToast: true,
        },
      },
    });
    expect(wrapper.findComponent(ReminderToast).exists()).toBe(true);
  });
});
