// @vitest-environment nuxt
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { mount, flushPromises } from '@vue/test-utils';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import AboutAppPage from './about-app.vue';

const { apiMock } = vi.hoisted(() => ({ apiMock: vi.fn() }));
mockNuxtImport('useApi', () => () => apiMock);

const NuxtLinkStub = {
  props: ['to'],
  template: '<a :href="to"><slot /></a>',
};

async function render() {
  const wrapper = mount(AboutAppPage, { global: { stubs: { NuxtLink: NuxtLinkStub } } });
  await flushPromises();
  return wrapper;
}

describe('settings/about-app page', () => {
  beforeEach(() => {
    apiMock.mockReset();
  });

  it('shows the heading and the version from /system/settings', async () => {
    apiMock.mockResolvedValue({ admin_configured: true, version: '1.2.3' });
    const wrapper = await render();

    expect(apiMock).toHaveBeenCalledWith('/api/v1/system/settings');
    expect(wrapper.text()).toContain('About CalCard');
    expect(wrapper.get('[data-testid="version"]').text()).toBe('Version 1.2.3');
  });

  it('shows a loading state until the version arrives', async () => {
    let resolve: (v: unknown) => void = () => {};
    apiMock.mockReturnValue(new Promise((r) => { resolve = r; }));
    const wrapper = mount(AboutAppPage, { global: { stubs: { NuxtLink: NuxtLinkStub } } });
    await flushPromises();
    expect(wrapper.find('[data-testid="version-loading"]').exists()).toBe(true);

    resolve({ version: '2.0.0' });
    await flushPromises();
    expect(wrapper.find('[data-testid="version-loading"]').exists()).toBe(false);
    expect(wrapper.get('[data-testid="version"]').text()).toBe('Version 2.0.0');
  });

  it.each([
    ['missing', { admin_configured: true }],
    ['empty', { version: '' }],
  ])('falls back to "unknown" when the version is %s', async (_n, payload) => {
    apiMock.mockResolvedValue(payload);
    const wrapper = await render();
    expect(wrapper.get('[data-testid="version"]').text()).toBe('Version unknown');
  });

  it('falls back to "unknown" when the request fails', async () => {
    apiMock.mockRejectedValue(new Error('offline'));
    const wrapper = await render();
    expect(wrapper.get('[data-testid="version"]').text()).toBe('Version unknown');
  });

  it('links to the open-source attribution page', async () => {
    apiMock.mockResolvedValue({ version: '1.0.0' });
    const wrapper = await render();
    expect(wrapper.find('a[href="/settings/about"]').exists()).toBe(true);
  });
});
