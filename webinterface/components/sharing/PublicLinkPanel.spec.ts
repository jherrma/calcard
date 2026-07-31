// @vitest-environment nuxt
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { mount } from '@vue/test-utils';
import { nextTick } from 'vue';
import { createTestingPinia } from '@pinia/testing';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import PublicLinkPanel from './PublicLinkPanel.vue';
import { useSharingStore } from '~/stores/sharing';

// SCOPE: story 043's public-link criteria that live in this component — the
// toggle in both directions, the regenerate confirmation, the fact that a failed
// status call must not be rendered as a link-less "public" panel, and the copy
// button on a plain-http deployment (docker-compose's default), where
// navigator.clipboard does not exist at all.

const { confirmRequire } = vi.hoisted(() => ({ confirmRequire: vi.fn() }));
vi.mock('primevue/useconfirm', () => ({
  useConfirm: () => ({ require: confirmRequire }),
}));

const { toastSpies } = vi.hoisted(() => ({
  toastSpies: { success: vi.fn(), error: vi.fn(), warn: vi.fn(), info: vi.fn() },
}));
mockNuxtImport('useAppToast', () => () => toastSpies);

interface ConfirmOptions {
  header?: string;
  message?: string;
  accept?: () => void | Promise<void>;
}

function lastConfirm(): ConfirmOptions {
  return (confirmRequire.mock.calls.at(-1)?.[0] ?? {}) as ConfirmOptions;
}

interface PanelVm {
  togglePublic: (next: boolean) => Promise<void>;
  copyUrl: () => Promise<void>;
  confirmRegenerate: () => void;
}

/** Replace navigator.clipboard, which happy-dom provides but plain http does not. */
function setClipboard(value: unknown) {
  Object.defineProperty(globalThis.navigator, 'clipboard', {
    value,
    configurable: true,
    writable: true,
  });
}

async function mountPanel(publicEnabled = false) {
  const pinia = createTestingPinia({ stubActions: true });
  const wrapper = mount(PublicLinkPanel, {
    props: { calendarUuid: 'cal-uuid-1', publicEnabled },
    shallow: true,
    global: { plugins: [pinia], renderStubDefaultSlot: true },
  });
  await nextTick();
  return { wrapper, vm: wrapper.vm as unknown as PanelVm, store: useSharingStore() };
}

const originalClipboard = globalThis.navigator.clipboard;

beforeEach(() => {
  confirmRequire.mockReset();
  toastSpies.success.mockReset();
  toastSpies.error.mockReset();
  vi.stubGlobal('$fetch', vi.fn());
});

afterEach(() => {
  setClipboard(originalClipboard);
  vi.unstubAllGlobals();
});

describe('status loading', () => {
  it('fetches the status on mount — the only source of the public URL', async () => {
    const { store } = await mountPanel();
    expect(store.fetchPublicAccess).toHaveBeenCalledWith('cal-uuid-1');
  });

  it('renders the failure reason instead of a public panel with no link', async () => {
    const { wrapper, store } = await mountPanel(true);
    store.isLoadingPublic = false;
    store.publicError = 'internal server error';
    store.publicAccess = null;
    await nextTick();

    expect(wrapper.text()).toContain('internal server error');
    // REVERT PROOF: without the publicError branch the seeded publicEnabled
    // renders the "This calendar is public" panel with no URL in it.
    expect(wrapper.text()).not.toContain('Subscription URL');
  });

  it('shows the URL once the status lands', async () => {
    const { wrapper, store } = await mountPanel();
    store.isLoadingPublic = false;
    store.publicAccess = { enabled: true, public_url: 'https://x/public/calendar/tok.ics' };
    await nextTick();

    expect(wrapper.text()).toContain('Subscription URL');
    expect(wrapper.html()).toContain('https://x/public/calendar/tok.ics');
  });
});

describe('toggle', () => {
  it('enables through POST …/public { enabled: true }', async () => {
    const { vm, store } = await mountPanel();
    await vm.togglePublic(true);

    expect(store.setPublicAccess).toHaveBeenCalledWith('cal-uuid-1', true);
    expect(toastSpies.success).toHaveBeenCalled();
  });

  it('disables through the same call — there is no DELETE route', async () => {
    const { vm, store } = await mountPanel(true);
    await vm.togglePublic(false);

    expect(store.setPublicAccess).toHaveBeenCalledWith('cal-uuid-1', false);
  });

  it('surfaces the server reason and leaves the switch to the store', async () => {
    const { vm, store } = await mountPanel();
    vi.mocked(store.setPublicAccess).mockRejectedValue(new Error('calendar not found'));

    await vm.togglePublic(true);

    expect(toastSpies.error).toHaveBeenCalledWith('calendar not found');
  });
});

describe('regenerate', () => {
  it('warns that existing subscribers break before minting a new token', async () => {
    const { vm, store } = await mountPanel(true);

    vm.confirmRegenerate();

    expect(lastConfirm().header).toBe('Regenerate public link');
    expect(lastConfirm().message).toContain('stops working immediately');
    expect(store.regeneratePublicToken).not.toHaveBeenCalled();

    await lastConfirm().accept?.();
    expect(store.regeneratePublicToken).toHaveBeenCalledWith('cal-uuid-1');
  });
});

describe('copy button', () => {
  it('copies and confirms when the Clipboard API is available', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    setClipboard({ writeText });
    const { vm, store } = await mountPanel(true);
    store.publicAccess = { enabled: true, public_url: 'https://x/public/calendar/tok.ics' };
    await nextTick();

    await vm.copyUrl();

    expect(writeText).toHaveBeenCalledWith('https://x/public/calendar/tok.ics');
    expect(toastSpies.success).toHaveBeenCalledWith('Link copied to clipboard');
  });

  it('tells the user to copy manually in a non-secure context instead of rejecting silently', async () => {
    // Plain http on a LAN address (docker-compose's default) has no
    // navigator.clipboard at all. REVERT PROOF: calling writeText unguarded
    // makes this handler reject unhandled — no toast of any kind.
    setClipboard(undefined);
    const { vm, store } = await mountPanel(true);
    store.publicAccess = { enabled: true, public_url: 'https://x/public/calendar/tok.ics' };
    await nextTick();

    await expect(vm.copyUrl()).resolves.toBeUndefined();

    expect(toastSpies.success).not.toHaveBeenCalled();
    expect(toastSpies.error).toHaveBeenCalledWith(
      'Could not copy automatically — select the URL and copy it manually',
    );
  });

  it('reports a rejected write (permission denied) rather than claiming success', async () => {
    setClipboard({ writeText: vi.fn().mockRejectedValue(new Error('NotAllowedError')) });
    const { vm, store } = await mountPanel(true);
    store.publicAccess = { enabled: true, public_url: 'https://x/public/calendar/tok.ics' };
    await nextTick();

    await vm.copyUrl();

    expect(toastSpies.success).not.toHaveBeenCalled();
    expect(toastSpies.error).toHaveBeenCalled();
  });
});
