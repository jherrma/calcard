// @vitest-environment nuxt
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { mount, flushPromises } from '@vue/test-utils';
import { nextTick } from 'vue';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import RegisterPage from './register.vue';

// SCOPE: the password field applies the server's full policy
// (utils/passwordPolicy.ts) before anything is sent. It used to check only
// "8 characters", so every other rule — and the 72-byte maximum — surfaced as
// a server round trip.

const { authStoreMock } = vi.hoisted(() => ({
  authStoreMock: { register: vi.fn() },
}));
mockNuxtImport('useAuthStore', () => () => authStoreMock);

interface PageVm {
  form: { email: string; display_name: string; password: string; confirmPassword: string };
  error: string;
  isRegistered: boolean;
  handleRegister: () => Promise<void>;
}

async function mountPage() {
  const wrapper = mount(RegisterPage, { shallow: true, global: { renderStubDefaultSlot: true } });
  await flushPromises();
  return { wrapper, vm: wrapper.vm as unknown as PageVm };
}

function fill(vm: PageVm, password: string) {
  vm.form.email = 'new@example.test';
  vm.form.display_name = 'New';
  vm.form.password = password;
  vm.form.confirmPassword = password;
}

// Braced body: a beforeEach arrow that returns a function (mockReset returns
// the mock) is taken by vitest as a teardown and called.
beforeEach(() => {
  authStoreMock.register.mockReset();
  authStoreMock.register.mockResolvedValue({ is_active: true, email_verified: true });
});

describe('register page password policy', () => {
  it.each([
    ['alllowercase1!', 'Password must contain at least one uppercase letter'],
    ['Correct horse battery 9', 'Password must contain at least one special character'],
    ['Aa1!' + 'x'.repeat(69), 'Password must be at most 72 bytes'],
    ['Aa1!' + 'ä'.repeat(35), 'Password must be at most 72 bytes'],
  ])('refuses %s with the rule it breaks', async (password, message) => {
    const { wrapper, vm } = await mountPage();
    fill(vm, password);
    await vm.handleRegister();
    await nextTick();

    expect(authStoreMock.register).not.toHaveBeenCalled();
    expect(wrapper.text()).toContain(message);
  });

  it('sends a password that meets the policy, non-ASCII capital included', async () => {
    const { vm } = await mountPage();
    fill(vm, 'Übermut2024!');
    await vm.handleRegister();

    expect(authStoreMock.register).toHaveBeenCalledWith({
      email: 'new@example.test',
      display_name: 'New',
      password: 'Übermut2024!',
    });
    expect(vm.isRegistered).toBe(true);
  });
});
