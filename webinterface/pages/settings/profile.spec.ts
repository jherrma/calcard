// @vitest-environment nuxt
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { mount } from '@vue/test-utils';
import { nextTick } from 'vue';
import { createTestingPinia } from '@pinia/testing';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import ProfilePage from './profile.vue';
import type { UserProfile } from '~/types/settings';

// SCOPE: the Username field. GET /users/me used to omit the username, so the
// box rendered empty for every account; it is the segment of the DAV URLs and
// the app-password login name, so it is shown read-only with a copy button.

const { toastSpies } = vi.hoisted(() => ({
  toastSpies: { success: vi.fn(), error: vi.fn(), warn: vi.fn(), info: vi.fn() },
}));
mockNuxtImport('useAppToast', () => () => toastSpies);

const { apiMock } = vi.hoisted(() => ({ apiMock: vi.fn() }));
mockNuxtImport('useApi', () => () => apiMock);

function profile(overrides: Partial<UserProfile> = {}): UserProfile {
  return {
    id: 'user-uuid',
    email: 'someone@example.org',
    username: 'aBcDeFgHiJkLmNoP',
    display_name: 'Someone',
    is_active: true,
    email_verified: true,
    created_at: '2026-10-01T00:00:00Z',
    updated_at: '2026-10-01T00:00:00Z',
    auth_methods: ['local'],
    stats: { calendar_count: 1, contact_count: 0, app_password_count: 0 },
    ...overrides,
  };
}

function setClipboard(value: unknown) {
  Object.defineProperty(globalThis.navigator, 'clipboard', {
    value,
    configurable: true,
    writable: true,
  });
}

async function mountPage() {
  const wrapper = mount(ProfilePage, {
    shallow: true,
    global: { plugins: [createTestingPinia({ stubActions: true })], renderStubDefaultSlot: true },
  });
  await nextTick();
  await nextTick();
  return wrapper;
}

const originalClipboard = globalThis.navigator.clipboard;
let writeText: ReturnType<typeof vi.fn>;

beforeEach(() => {
  vi.clearAllMocks();
  apiMock.mockReset();
  writeText = vi.fn().mockResolvedValue(undefined);
  setClipboard({ writeText });
});

afterEach(() => {
  setClipboard(originalClipboard);
});

describe('username', () => {
  it('shows the username from /users/me', async () => {
    apiMock.mockResolvedValue(profile());
    const wrapper = await mountPage();

    expect(apiMock).toHaveBeenCalledWith('/api/v1/users/me');
    expect(wrapper.find('#profile-username').html()).toContain('aBcDeFgHiJkLmNoP');
  });

  it('copies it exactly as stored', async () => {
    apiMock.mockResolvedValue(profile());
    const wrapper = await mountPage();

    const button = wrapper.find('[aria-label="Copy username"]');
    expect(button.attributes('disabled')).not.toBe('true');
    await button.trigger('click');
    await nextTick();

    expect(writeText).toHaveBeenCalledWith('aBcDeFgHiJkLmNoP');
    expect(toastSpies.success).toHaveBeenCalledWith('Username copied');
  });

  it('offers nothing to copy when the server sends no username', async () => {
    // An older server without the field: the box is empty, not "undefined".
    const { username: _omitted, ...withoutUsername } = profile();
    apiMock.mockResolvedValue(withoutUsername);
    const wrapper = await mountPage();

    expect(wrapper.find('#profile-username').html()).not.toContain('undefined');
    expect(wrapper.find('[aria-label="Copy username"]').attributes('disabled')).toBe('true');
  });
});
