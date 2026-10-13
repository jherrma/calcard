// @vitest-environment nuxt
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { mount, flushPromises } from '@vue/test-utils';
import { nextTick } from 'vue';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import PasswordPage from './password.vue';

// SCOPE: the NEW password applies the server's full policy
// (utils/passwordPolicy.ts) before the request; the CURRENT password must not —
// it is whatever the account has, and an older or longer one has to keep
// working. The page used to check only "8 characters".

const { apiMock } = vi.hoisted(() => ({ apiMock: vi.fn() }));
mockNuxtImport('useApi', () => () => apiMock);

const { toastSpies } = vi.hoisted(() => ({
  toastSpies: { success: vi.fn(), error: vi.fn(), warn: vi.fn(), info: vi.fn() },
}));
mockNuxtImport('useAppToast', () => () => toastSpies);

const { authStoreMock } = vi.hoisted(() => ({ authStoreMock: { accessToken: 'old-token' } }));
mockNuxtImport('useAuthStore', () => () => authStoreMock);

interface PageVm {
  form: { current_password: string; new_password: string; confirm_password: string };
  error: string;
  handleSubmit: () => Promise<void>;
}

async function mountPage() {
  const wrapper = mount(PasswordPage, { shallow: true, global: { renderStubDefaultSlot: true } });
  await flushPromises();
  return { wrapper, vm: wrapper.vm as unknown as PageVm };
}

// Braced body: a beforeEach arrow that returns a function (mockReset returns
// the mock) is taken by vitest as a teardown and called.
beforeEach(() => {
  vi.clearAllMocks();
  apiMock.mockReset();
  apiMock.mockResolvedValue({ message: 'ok', access_token: 'new-token' });
  authStoreMock.accessToken = 'old-token';
});

describe('change-password page', () => {
  it.each([
    ['alllowercase1!', 'Password must contain at least one uppercase letter'],
    ['Correct horse battery 9', 'Password must contain at least one special character'],
    ['Aa1!' + 'x'.repeat(69), 'Password must be at most 72 bytes'],
  ])('refuses the new password %s with the rule it breaks', async (password, message) => {
    const { wrapper, vm } = await mountPage();
    vm.form.current_password = 'OldPass123!';
    vm.form.new_password = password;
    vm.form.confirm_password = password;
    await vm.handleSubmit();
    await nextTick();

    expect(apiMock).not.toHaveBeenCalled();
    expect(wrapper.text()).toContain(message);
  });

  it('does not apply the policy to the current password', async () => {
    // Revert caught: a policy on current_password would lock out accounts
    // whose password predates a rule (here: no capital, no special, > 72 bytes).
    const { vm } = await mountPage();
    const legacy = 'legacy1' + 'x'.repeat(80);
    vm.form.current_password = legacy;
    vm.form.new_password = 'Übermut2024!';
    vm.form.confirm_password = 'Übermut2024!';
    await vm.handleSubmit();

    expect(apiMock).toHaveBeenCalledTimes(1);
    const [url, options] = apiMock.mock.calls[0]!;
    expect(url).toBe('/api/v1/users/me/password');
    expect(options.body).toEqual({ current_password: legacy, new_password: 'Übermut2024!' });
    expect(authStoreMock.accessToken).toBe('new-token');
    expect(vm.error).toBe('');
  });
});
