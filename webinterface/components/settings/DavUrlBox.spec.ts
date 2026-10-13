// @vitest-environment nuxt
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { mount } from '@vue/test-utils';
import { nextTick } from 'vue';
import { createTestingPinia } from '@pinia/testing';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import { useRuntimeConfig } from '#imports';
import DavUrlBox from './DavUrlBox.vue';
import { useAuthStore } from '~/stores/auth';
import type { User } from '~/types/auth';

// SCOPE: the URLs a user pastes into a DAV client (Thunderbird, DAVx5, …). A
// wrong one fails in the client with no diagnosis, so the exact strings, the
// account username they are built from and the copy buttons are pinned here.

const { toastSpies } = vi.hoisted(() => ({
  toastSpies: { success: vi.fn(), error: vi.fn(), warn: vi.fn(), info: vi.fn() },
}));
mockNuxtImport('useAppToast', () => () => toastSpies);

type Variant = 'caldav' | 'carddav' | 'app-passwords';

interface BoxVm {
  fields: { id: string; label: string; value: string }[];
  origin: string;
}

function setClipboard(value: unknown) {
  Object.defineProperty(globalThis.navigator, 'clipboard', {
    value,
    configurable: true,
    writable: true,
  });
}

function account(username: string): User {
  return {
    id: 'user-uuid',
    email: 'someone@example.org',
    username,
    is_admin: false,
    created_at: '2026-10-01T00:00:00Z',
  };
}

// username null = no user object in the store at all.
async function mountBox(variant: Variant, username: string | null = 'aBcDeFgHiJkLmNoP') {
  const pinia = createTestingPinia({ stubActions: true });
  const auth = useAuthStore();
  auth.user = username === null ? null : account(username);
  const wrapper = mount(DavUrlBox, {
    props: { variant },
    shallow: true,
    global: { plugins: [pinia], renderStubDefaultSlot: true },
  });
  await nextTick();
  return { wrapper, vm: wrapper.vm as unknown as BoxVm };
}

// The origin the component must use: the API origin, else this page's.
function expectedOrigin(): string {
  const base = (useRuntimeConfig().public.apiBaseUrl as string) || window.location.origin;
  return base.replace(/\/+$/, '');
}

const originalClipboard = globalThis.navigator.clipboard;
let writeText: ReturnType<typeof vi.fn>;

beforeEach(() => {
  vi.clearAllMocks();
  writeText = vi.fn().mockResolvedValue(undefined);
  setClipboard({ writeText });
});

afterEach(() => {
  setClipboard(originalClipboard);
});

describe('URLs', () => {
  it('shows the calendar home of the account on the CalDAV credentials page', async () => {
    const { wrapper, vm } = await mountBox('caldav');
    const url = `${expectedOrigin()}/dav/aBcDeFgHiJkLmNoP/calendars/`;

    expect(vm.origin).toBe(expectedOrigin());
    expect(vm.fields.map((f) => [f.label, f.value])).toEqual([['CalDAV URL', url]]);
    // The field is what the user sees and selects by hand when copying fails.
    expect(wrapper.find('#dav-url-caldav').html()).toContain(url);
    expect(wrapper.html()).not.toContain('/addressbooks/');
  });

  it('shows the address-book home of the account on the CardDAV credentials page', async () => {
    const { vm } = await mountBox('carddav');
    expect(vm.fields.map((f) => [f.label, f.value])).toEqual([
      ['CardDAV URL', `${expectedOrigin()}/dav/aBcDeFgHiJkLmNoP/addressbooks/`],
    ]);
  });

  it('shows the login username and both homes on the app-passwords page', async () => {
    const { vm } = await mountBox('app-passwords');
    expect(vm.fields.map((f) => [f.label, f.value])).toEqual([
      ['Username', 'aBcDeFgHiJkLmNoP'],
      ['CalDAV URL', `${expectedOrigin()}/dav/aBcDeFgHiJkLmNoP/calendars/`],
      ['CardDAV URL', `${expectedOrigin()}/dav/aBcDeFgHiJkLmNoP/addressbooks/`],
    ]);
  });

  it('never points a client at the REST API', async () => {
    const { vm } = await mountBox('app-passwords');
    for (const field of vm.fields) expect(field.value).not.toContain('/api/v1');
  });

  it('says which login to use', async () => {
    const credentialPage = (await mountBox('caldav')).wrapper.text();
    // A DAV credential logs in with its OWN username, an app password with the
    // account's; the URL carries the account username either way.
    expect(credentialPage).toContain('username and password of one of the credentials below');
    expect(credentialPage).toContain('account username aBcDeFgHiJkLmNoP and an app password');

    const appPasswordPage = (await mountBox('app-passwords')).wrapper.text();
    expect(appPasswordPage).toContain('account username (or e-mail address) and the app password');
  });

  it('shows a warning instead of a /dav//… URL when the username is unknown', async () => {
    // REVERT PROOF: /users/me used to omit the username (dto.UserProfileResponse
    // had no field), and every URL would have read /dav//calendars/.
    for (const user of [null, '']) {
      const { wrapper, vm } = await mountBox('caldav', user);
      expect(wrapper.text()).toContain('username could not be loaded');
      expect(wrapper.find('#dav-url-caldav').exists()).toBe(false);
      expect(wrapper.findAll('[aria-label^="Copy "]')).toHaveLength(0);
      expect(vm.fields.length).toBeGreaterThan(0); // computed, just not rendered
    }
  });
});

describe('copy buttons', () => {
  it('copies each field through its own button', async () => {
    const { wrapper, vm } = await mountBox('app-passwords');
    const buttons = wrapper.findAll('[aria-label^="Copy "]');
    expect(buttons.map((b) => b.attributes('aria-label'))).toEqual([
      'Copy Username',
      'Copy CalDAV URL',
      'Copy CardDAV URL',
    ]);

    await buttons[1]!.trigger('click');
    await nextTick();

    expect(writeText).toHaveBeenCalledWith(vm.fields[1]!.value);
    expect(toastSpies.success).toHaveBeenCalledWith('CalDAV URL copied');
  });

  it('tells the user to copy by hand over plain http, where there is no clipboard API', async () => {
    setClipboard(undefined);
    const { wrapper } = await mountBox('caldav');

    await wrapper.find('[aria-label="Copy CalDAV URL"]').trigger('click');
    await nextTick();

    expect(toastSpies.success).not.toHaveBeenCalled();
    expect(toastSpies.error).toHaveBeenCalledWith(expect.stringContaining('copy it manually'));
  });

  it('reports a refused clipboard write instead of claiming success', async () => {
    writeText.mockRejectedValue(new Error('denied'));
    const { wrapper } = await mountBox('carddav');

    await wrapper.find('[aria-label="Copy CardDAV URL"]').trigger('click');
    await nextTick();

    expect(toastSpies.success).not.toHaveBeenCalled();
    expect(toastSpies.error).toHaveBeenCalledWith(expect.stringContaining('copy it manually'));
  });
});
