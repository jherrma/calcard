// @vitest-environment nuxt
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { mount, flushPromises } from '@vue/test-utils';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import ResetPasswordPage from './reset-password.vue';

// SCOPE: the body the reset page sends. The server reads `new_password`; the
// page once sent `password`, so every reset reached the server with an empty
// password and failed with "password must be at least 8 characters".

const { apiMock } = vi.hoisted(() => ({ apiMock: vi.fn() }));
mockNuxtImport('useApi', () => () => apiMock);
mockNuxtImport('useRoute', () => () => ({ query: { token: 'reset-token' } }));

interface PageVm {
  form: { password: string; confirmPassword: string };
  error: string;
  success: boolean;
  handleSubmit: () => Promise<void>;
}

async function mountPage() {
  const wrapper = mount(ResetPasswordPage, { shallow: true, global: { renderStubDefaultSlot: true } });
  await flushPromises();
  return wrapper.vm as unknown as PageVm;
}

describe('reset-password page', () => {
  beforeEach(() => {
    apiMock.mockReset();
    apiMock.mockResolvedValue({});
  });

  it('sends the token and the password as new_password', async () => {
    const vm = await mountPage();
    vm.form.password = 'Secure#Pass1';
    vm.form.confirmPassword = 'Secure#Pass1';
    await vm.handleSubmit();

    expect(apiMock).toHaveBeenCalledTimes(1);
    const [url, options] = apiMock.mock.calls[0]!;
    expect(url).toBe('/api/v1/auth/reset-password');
    expect(options.body).toEqual({ token: 'reset-token', new_password: 'Secure#Pass1' });
    expect(vm.success).toBe(true);
  });

  it('does not call the server for a password that breaks the policy', async () => {
    const vm = await mountPage();
    vm.form.password = 'alllowercase1!';
    vm.form.confirmPassword = 'alllowercase1!';
    await vm.handleSubmit();

    expect(apiMock).not.toHaveBeenCalled();
    expect(vm.success).toBe(false);
  });
});
