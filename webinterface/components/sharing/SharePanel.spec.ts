// @vitest-environment nuxt
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { mount } from '@vue/test-utils';
import { nextTick } from 'vue';
import { createTestingPinia } from '@pinia/testing';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import SharePanel from './SharePanel.vue';
import { useSharingStore } from '~/stores/sharing';
import type { Share, SharePermission, ShareResourceType } from '~/types/sharing';

// SCOPE: the acceptance criteria of story 043 that live in this component and
// were previously covered by code only — owner-only gating, the read-write ->
// read downgrade confirmation, the last-share / bulk-revoke confirmations, and
// the rule that a FAILED share load must never be rendered as "this calendar is
// private". Each test names the revert that breaks it.

// useConfirm() needs a ConfirmationService provider a bare mount lacks. The stub
// captures the options so a test can inspect the copy and fire accept() itself.
const { confirmRequire } = vi.hoisted(() => ({ confirmRequire: vi.fn() }));
vi.mock('primevue/useconfirm', () => ({
  useConfirm: () => ({ require: confirmRequire }),
}));

// useAppToast() calls PrimeVue's useToast(), which needs a ToastService provider.
const { toastSpies } = vi.hoisted(() => ({
  toastSpies: { success: vi.fn(), error: vi.fn(), warn: vi.fn(), info: vi.fn() },
}));
mockNuxtImport('useAppToast', () => () => toastSpies);

interface ConfirmOptions {
  header?: string;
  message?: string;
  accept?: () => void | Promise<void>;
}

/** The most recent confirm.require() call's options. */
function lastConfirm(): ConfirmOptions {
  const call = confirmRequire.mock.calls.at(-1);
  return (call?.[0] ?? {}) as ConfirmOptions;
}

interface PanelVm {
  requestPermissionChange: (share: Share, permission: SharePermission) => void;
  confirmRemove: (share: Share) => void;
  confirmRemoveAll: () => void;
  reload: () => void;
}

function share(id: string, email: string, permission = 'read'): Share {
  return {
    id,
    shared_with: { id: `u-${id}`, username: email.split('@')[0]!, display_name: `User ${id}`, email },
    permission,
    created_at: '2026-01-01T00:00:00Z',
  };
}

async function mountPanel(props: {
  resourceType?: ShareResourceType;
  resourceUuid?: string;
  canManage?: boolean;
} = {}) {
  const pinia = createTestingPinia({ stubActions: true });
  const wrapper = mount(SharePanel, {
    props: {
      resourceType: props.resourceType ?? 'calendar',
      resourceUuid: props.resourceUuid ?? 'cal-uuid-1',
      ...(props.canManage === undefined ? {} : { canManage: props.canManage }),
    },
    shallow: true,
    // Stubs would otherwise swallow the copy we assert on (it sits in the
    // default slot of <Message>).
    global: { plugins: [pinia], renderStubDefaultSlot: true },
  });
  await nextTick();
  return { wrapper, vm: wrapper.vm as unknown as PanelVm, store: useSharingStore() };
}

beforeEach(() => {
  confirmRequire.mockReset();
  toastSpies.success.mockReset();
  toastSpies.error.mockReset();
  vi.stubGlobal('$fetch', vi.fn());
});

describe('owner-only gating (#53)', () => {
  it('renders the owner notice and never calls the share endpoints for a sharee', async () => {
    const { wrapper, store } = await mountPanel({ canManage: false });

    expect(wrapper.text()).toContain('Only the owner of this calendar');
    expect(wrapper.text()).not.toContain('Invite someone');
    // REVERT PROOF: hardcoding can-manage to true fetches, and every share
    // endpoint 404s for a sharee.
    expect(store.fetchShares).not.toHaveBeenCalled();
  });

  it('loads the list for the owner, keyed on the UUID', async () => {
    const { wrapper, store } = await mountPanel({ resourceType: 'addressbook', resourceUuid: 'ab-uuid-9' });

    expect(store.fetchShares).toHaveBeenCalledWith('addressbook', 'ab-uuid-9');
    expect(wrapper.text()).toContain('Invite someone');
  });
});

describe('load failure is never rendered as "private"', () => {
  it('shows the reason instead of asserting the resource is private', async () => {
    const { wrapper, store } = await mountPanel();
    store.sharesError = 'internal server error';
    store.shares = [];
    await nextTick();

    expect(wrapper.text()).toContain('internal server error');
    // REVERT PROOF: without the !sharesError guard the empty state wins and the
    // owner is told nobody else can see a calendar they shared with three people.
    expect(wrapper.text()).not.toContain('is private');
  });

  it('still claims privacy when the list genuinely loaded and is empty', async () => {
    const { wrapper, store } = await mountPanel();
    store.sharesError = null;
    store.shares = [];
    await nextTick();

    expect(wrapper.text()).toContain('This calendar is private');
  });

  it('keeps the surviving rows visible when a bulk revoke partially failed', async () => {
    const { wrapper, store } = await mountPanel();
    store.shares = [share('s2', 'kept@example.com')];
    store.sharesError = 'share not found';
    await nextTick();

    expect(wrapper.text()).toContain('kept@example.com');
    expect(wrapper.text()).toContain('share not found');
    expect(wrapper.text()).not.toContain('is private');
  });

  it('retries the load from the error state', async () => {
    const { vm, store } = await mountPanel();
    vi.mocked(store.fetchShares).mockClear();

    vm.reload();

    expect(store.fetchShares).toHaveBeenCalledWith('calendar', 'cal-uuid-1');
  });
});

describe('permission change confirmation', () => {
  it('asks before narrowing read-write to read, and only then applies it', async () => {
    const { vm, store } = await mountPanel();
    const target = share('s1', 'a@example.com', 'read-write');

    vm.requestPermissionChange(target, 'read');

    expect(confirmRequire).toHaveBeenCalledTimes(1);
    expect(lastConfirm().header).toBe('Reduce access');
    expect(lastConfirm().message).toContain('no longer be able to add, edit or delete');
    // REVERT PROOF: drop the confirm branch and the PATCH fires here already.
    expect(store.updateShare).not.toHaveBeenCalled();

    await lastConfirm().accept?.();
    expect(store.updateShare).toHaveBeenCalledWith('calendar', 'cal-uuid-1', 's1', 'read');
  });

  it('does nothing at all when the confirmation is dismissed', async () => {
    const { vm, store } = await mountPanel();

    vm.requestPermissionChange(share('s1', 'a@example.com', 'read-write'), 'read');
    // No accept() — the user cancelled. The Select is bound one-way, so there is
    // nothing to roll back either.
    expect(store.updateShare).not.toHaveBeenCalled();
  });

  it('widens read -> read-write directly: nothing is taken away', async () => {
    const { vm, store } = await mountPanel();

    vm.requestPermissionChange(share('s1', 'a@example.com', 'read'), 'read-write');

    expect(confirmRequire).not.toHaveBeenCalled();
    expect(store.updateShare).toHaveBeenCalledWith('calendar', 'cal-uuid-1', 's1', 'read-write');
  });

  it('ignores a re-selection of the permission already granted', async () => {
    const { vm, store } = await mountPanel();

    vm.requestPermissionChange(share('s1', 'a@example.com', 'read'), 'read');

    expect(confirmRequire).not.toHaveBeenCalled();
    expect(store.updateShare).not.toHaveBeenCalled();
  });
});

describe('revoke confirmation', () => {
  it('spells out that the resource becomes private when the last share goes', async () => {
    const { vm, store } = await mountPanel();
    store.shares = [share('s1', 'a@example.com')];
    await nextTick();

    vm.confirmRemove(store.shares[0]!);

    expect(lastConfirm().message).toContain('becomes private again');
    expect(store.revokeShare).not.toHaveBeenCalled();

    await lastConfirm().accept?.();
    expect(store.revokeShare).toHaveBeenCalledWith('calendar', 'cal-uuid-1', 's1');
  });

  it('uses the plain copy while other shares remain', async () => {
    const { vm, store } = await mountPanel();
    store.shares = [share('s1', 'a@example.com'), share('s2', 'b@example.com')];
    await nextTick();

    vm.confirmRemove(store.shares[0]!);

    expect(lastConfirm().message).toContain("Remove User s1's access to this calendar");
    expect(lastConfirm().message).not.toContain('becomes private again');
  });
});

describe('bulk revoke', () => {
  it('offers "Remove all" only once there is more than one share', async () => {
    const { wrapper, store } = await mountPanel();
    store.shares = [share('s1', 'a@example.com')];
    await nextTick();
    expect(wrapper.html()).not.toContain('Remove all');

    store.shares = [share('s1', 'a@example.com'), share('s2', 'b@example.com')];
    await nextTick();
    expect(wrapper.html()).toContain('Remove all');
  });

  it('confirms first, then revokes everything', async () => {
    const { vm, store } = await mountPanel();
    store.shares = [share('s1', 'a@example.com'), share('s2', 'b@example.com')];
    await nextTick();
    vi.mocked(store.revokeAllShares).mockResolvedValue({ revoked: 2, failed: 0, reason: null });

    vm.confirmRemoveAll();

    expect(lastConfirm().header).toBe('Remove all shares');
    expect(lastConfirm().message).toContain('Remove all 2 people');
    expect(store.revokeAllShares).not.toHaveBeenCalled();

    await lastConfirm().accept?.();
    expect(store.revokeAllShares).toHaveBeenCalledWith('calendar', 'cal-uuid-1');
    expect(toastSpies.success).toHaveBeenCalledWith('Removed 2 of 2 shares', 'Removed');
  });

  it('names the server reason for the shares it could not remove', async () => {
    const { vm, store } = await mountPanel();
    store.shares = [share('s1', 'a@example.com'), share('s2', 'b@example.com')];
    await nextTick();
    vi.mocked(store.revokeAllShares).mockResolvedValue({ revoked: 1, failed: 1, reason: 'share not found' });

    vm.confirmRemoveAll();
    await lastConfirm().accept?.();

    // REVERT PROOF: dropping `reason` leaves "1 share could not be removed",
    // with a row still on screen and no explanation for it.
    expect(toastSpies.error).toHaveBeenCalledWith('1 share could not be removed: share not found');
  });
});
