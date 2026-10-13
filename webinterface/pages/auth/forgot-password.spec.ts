// @vitest-environment nuxt
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { mount, flushPromises } from '@vue/test-utils';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import ForgotPasswordPage from './forgot-password.vue';

// SCOPE: what the forgot-password page tells the user. The server answers 200
// for every address, known or not, so success is always the green notice. A
// failure (the mail server is down) is a 500, and it must read as an error:
// the page used to swallow it and show the green notice, so the user waited
// for a mail that was never sent.

const { apiMock } = vi.hoisted(() => ({ apiMock: vi.fn() }));
mockNuxtImport('useApi', () => () => apiMock);

interface PageVm {
  form: { email: string };
  submitted: boolean;
  error: string;
  handleSubmit: () => Promise<void>;
}

async function mountPage() {
  const wrapper = mount(ForgotPasswordPage, { shallow: true, global: { renderStubDefaultSlot: true } });
  await flushPromises();
  const vm = wrapper.vm as unknown as PageVm;
  vm.form.email = 'someone@example.test';
  return vm;
}

describe('forgot-password page', () => {
  beforeEach(() => {
    apiMock.mockReset();
  });

  it('shows the success notice when the server accepts the request', async () => {
    apiMock.mockResolvedValue({});
    const vm = await mountPage();
    await vm.handleSubmit();

    expect(apiMock).toHaveBeenCalledWith('/api/v1/auth/forgot-password', {
      method: 'POST',
      body: { email: 'someone@example.test' },
    });
    expect(vm.submitted).toBe(true);
    expect(vm.error).toBe('');
  });

  it("shows the server's message and no success notice when sending failed", async () => {
    apiMock.mockRejectedValue({ data: { message: 'A server error occurred while sending the reset link. Please try again later.' } });
    const vm = await mountPage();
    await vm.handleSubmit();

    expect(vm.submitted).toBe(false);
    expect(vm.error).toContain('A server error occurred');
  });

  it('falls back to a generic message when the server cannot be reached', async () => {
    apiMock.mockRejectedValue(new Error('network down'));
    const vm = await mountPage();
    await vm.handleSubmit();

    expect(vm.submitted).toBe(false);
    expect(vm.error).toBe('A server error occurred. Please try again later.');
  });
});
