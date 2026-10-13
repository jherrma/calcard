// @vitest-environment nuxt
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { mount, flushPromises } from '@vue/test-utils';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import VerifyPage from './verify.vue';

// SCOPE: the page the activation e-mail links to. The server only has
// GET /api/v1/auth/verify?token=; the page used to POST {token} as a JSON
// body, which no route answers, so it could never succeed.

const { apiMock } = vi.hoisted(() => ({ apiMock: vi.fn() }));
mockNuxtImport('useApi', () => () => apiMock);

const { routeMock } = vi.hoisted(() => ({ routeMock: { query: {} as Record<string, unknown> } }));
mockNuxtImport('useRoute', () => () => routeMock);

interface PageVm {
  loading: boolean;
  success: boolean;
  errorMessage: string;
}

async function mountPage(query: Record<string, unknown>) {
  routeMock.query = query;
  const wrapper = mount(VerifyPage, { shallow: true, global: { renderStubDefaultSlot: true } });
  await flushPromises();
  return { wrapper, vm: wrapper.vm as unknown as PageVm };
}

// Braced body: a beforeEach arrow that returns a function (mockReset returns
// the mock) is taken by vitest as a teardown and called.
beforeEach(() => {
  apiMock.mockReset();
});

describe('verify page', () => {
  it('verifies with a GET carrying the token in the query, and says so', async () => {
    // useApi unwraps the {status, data} envelope.
    apiMock.mockResolvedValue('Account verified successfully');
    const { wrapper, vm } = await mountPage({ token: 'a1b2c3' });

    expect(apiMock).toHaveBeenCalledTimes(1);
    const [url, options] = apiMock.mock.calls[0]!;
    expect(url).toBe('/api/v1/auth/verify?token=a1b2c3');
    // No POST and no body: the route is GET-only.
    expect(options?.method ?? 'GET').toBe('GET');
    expect(options?.body).toBeUndefined();
    expect(vm.loading).toBe(false);
    expect(vm.success).toBe(true);
    expect(wrapper.text()).toContain('Email Verified');
  });

  it('encodes the token', async () => {
    apiMock.mockResolvedValue('Account verified successfully');
    await mountPage({ token: 'a b&c=d' });

    expect(apiMock.mock.calls[0]![0]).toBe('/api/v1/auth/verify?token=a%20b%26c%3Dd');
  });

  it("shows the server's message for a bad or expired token", async () => {
    apiMock.mockRejectedValue({
      statusCode: 400,
      data: { status: 'error', message: 'invalid or expired verification token' },
    });
    const { wrapper, vm } = await mountPage({ token: 'expired' });

    expect(vm.success).toBe(false);
    expect(vm.loading).toBe(false);
    expect(vm.errorMessage).toBe('invalid or expired verification token');
    expect(wrapper.text()).toContain('Verification Failed');
    expect(wrapper.text()).toContain('invalid or expired verification token');
  });

  it('does not call the server without a token', async () => {
    const { wrapper, vm } = await mountPage({});

    expect(apiMock).not.toHaveBeenCalled();
    expect(vm.success).toBe(false);
    expect(vm.loading).toBe(false);
    expect(wrapper.text()).toContain('Verification token is missing.');
  });
});
