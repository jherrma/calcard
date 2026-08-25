// @vitest-environment nuxt
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { mount } from '@vue/test-utils';
import { nextTick } from 'vue';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import McpSettingsPage from './mcp.vue';
import type { MCPToken, MCPTokenCreateResponse } from '~/types/settings';

// SCOPE: the parts of story 104's settings page that are load-bearing and would
// otherwise be covered by code only — the one-time delivery of the secret, the
// endpoint URL the user is told to configure (a wrong one makes every client
// fail with no clue why), and revocation. Each test names the revert it catches.

const { confirmRequire } = vi.hoisted(() => ({ confirmRequire: vi.fn() }));
vi.mock('primevue/useconfirm', () => ({
  useConfirm: () => ({ require: confirmRequire }),
}));

const { toastSpies } = vi.hoisted(() => ({
  toastSpies: { success: vi.fn(), error: vi.fn(), warn: vi.fn(), info: vi.fn() },
}));
mockNuxtImport('useAppToast', () => () => toastSpies);

const { apiMock } = vi.hoisted(() => ({ apiMock: vi.fn() }));
mockNuxtImport('useApi', () => () => apiMock);

interface ConfirmOptions {
  header?: string;
  message?: string;
  accept?: () => void | Promise<void>;
}

function lastConfirm(): ConfirmOptions {
  return (confirmRequire.mock.calls.at(-1)?.[0] ?? {}) as ConfirmOptions;
}

interface PageVm {
  tokens: MCPToken[];
  loadError: string | null;
  endpoint: string;
  createdToken: MCPTokenCreateResponse | null;
  createError: string;
  createForm: { name: string; expiresAt: Date | null };
  handleCreate: () => Promise<void>;
  confirmRevoke: (token: MCPToken) => void;
}

function token(overrides: Partial<MCPToken> = {}): MCPToken {
  return {
    id: 'tok-1',
    name: 'Claude on my laptop',
    token_prefix: 'calcard_mcp_Ab3xK9',
    expires_at: null,
    last_used_at: null,
    last_used_ip: '',
    created_at: '2026-08-01T00:00:00Z',
    ...overrides,
  };
}

async function mountPage() {
  const wrapper = mount(McpSettingsPage, { shallow: true, global: { renderStubDefaultSlot: true } });
  await nextTick();
  await nextTick();
  return { wrapper, vm: wrapper.vm as unknown as PageVm };
}

beforeEach(() => {
  vi.clearAllMocks();
  apiMock.mockReset();
  apiMock.mockResolvedValue({ tokens: [] });
});

describe('listing', () => {
  it('renders each token by its prefix and never has a full secret to render', async () => {
    apiMock.mockResolvedValue({ tokens: [token({ last_used_ip: '203.0.113.7', last_used_at: new Date().toISOString() })] });

    const { wrapper, vm } = await mountPage();

    expect(apiMock).toHaveBeenCalledWith('/api/v1/mcp-tokens');
    expect(vm.tokens).toHaveLength(1);
    expect(wrapper.text()).toContain('Claude on my laptop');
    expect(wrapper.text()).toContain('calcard_mcp_Ab3xK9');
    expect(wrapper.text()).toContain('203.0.113.7');
  });

  it('shows the empty state when the account has no tokens', async () => {
    const { wrapper } = await mountPage();
    expect(wrapper.text()).toContain('No MCP tokens yet');
  });

  it('flags an expired token so a dead client is explicable', async () => {
    apiMock.mockResolvedValue({ tokens: [token({ expires_at: '2020-01-01T00:00:00Z' })] });
    const { wrapper } = await mountPage();
    // REVERT PROOF: without isExpired the row looks identical to a live token
    // and the user has no way to see why their assistant stopped working.
    expect(wrapper.html()).toContain('expired');
  });

  it('reports a failed load instead of claiming there are no tokens', async () => {
    apiMock.mockRejectedValue(new Error('boom'));
    const { wrapper, vm } = await mountPage();

    expect(toastSpies.error).toHaveBeenCalledWith('Failed to load MCP tokens');
    expect(wrapper.text()).toContain('Failed to load MCP tokens');
    // REVERT PROOF: without the loadError branch the empty state claims the
    // account has no tokens, on no evidence — and hides the very token the
    // user may be here to revoke.
    expect(wrapper.text()).not.toContain('No MCP tokens yet');

    // A successful retry clears it.
    apiMock.mockReset();
    apiMock.mockResolvedValue({ tokens: [token()] });
    await (vm as unknown as { fetchTokens: () => Promise<void> }).fetchTokens();
    expect(vm.loadError).toBeNull();
  });
});

describe('endpoint', () => {
  it('points at /mcp on the API origin, not under /api/v1', async () => {
    const { vm } = await mountPage();
    // The protocol addresses one URL that clients are configured with directly.
    // Getting this wrong makes every client fail with no diagnosis.
    expect(vm.endpoint).toMatch(/\/mcp$/);
    expect(vm.endpoint).not.toContain('/api/v1');
  });
});

describe('creation', () => {
  it('requires a name before calling the API', async () => {
    const { vm } = await mountPage();
    apiMock.mockClear();

    vm.createForm.name = '   ';
    await vm.handleCreate();

    expect(vm.createError).toBe('Name is required');
    expect(apiMock).not.toHaveBeenCalled();
  });

  it('delivers the secret once and refreshes the list', async () => {
    const created: MCPTokenCreateResponse = {
      id: 'tok-9',
      name: 'Laptop',
      token: 'calcard_mcp_SUPERSECRETVALUE',
      token_prefix: 'calcard_mcp_SUPERS',
      expires_at: null,
      created_at: '2026-08-25T00:00:00Z',
    };
    const { wrapper, vm } = await mountPage();

    apiMock.mockReset();
    apiMock.mockResolvedValueOnce(created);
    apiMock.mockResolvedValueOnce({ tokens: [token({ id: 'tok-9', name: 'Laptop' })] });

    vm.createForm.name = 'Laptop';
    await vm.handleCreate();
    await nextTick();

    expect(apiMock).toHaveBeenNthCalledWith(1, '/api/v1/mcp-tokens', {
      method: 'POST',
      body: { name: 'Laptop', expires_at: null },
    });
    expect(vm.createdToken?.token).toBe('calcard_mcp_SUPERSECRETVALUE');
    // The secret is rendered only in this post-create panel; the server cannot
    // produce it again, so failing to show it here strands the user.
    expect(wrapper.text()).toContain('shown once');
    expect(vm.tokens).toHaveLength(1);
  });

  it('sends the chosen expiry as RFC 3339', async () => {
    const { vm } = await mountPage();
    apiMock.mockReset();
    apiMock.mockResolvedValueOnce({ id: 'x', name: 'x', token: 't', token_prefix: 'p', expires_at: null, created_at: '' });
    apiMock.mockResolvedValueOnce({ tokens: [] });

    vm.createForm.name = 'Expiring';
    vm.createForm.expiresAt = new Date('2027-01-01T00:00:00.000Z');
    await vm.handleCreate();

    expect(apiMock).toHaveBeenNthCalledWith(1, '/api/v1/mcp-tokens', {
      method: 'POST',
      body: { name: 'Expiring', expires_at: '2027-01-01T00:00:00.000Z' },
    });
  });

  it('surfaces the validation message from the server rather than a generic failure', async () => {
    const { vm } = await mountPage();
    apiMock.mockReset();
    apiMock.mockRejectedValueOnce({ data: { message: 'expires_at must be in the future' } });

    vm.createForm.name = 'Bad';
    await vm.handleCreate();

    expect(vm.createError).toBe('expires_at must be in the future');
    expect(vm.createdToken).toBeNull();
  });
});

describe('revocation', () => {
  it('confirms first, then deletes and drops the row', async () => {
    apiMock.mockResolvedValue({ tokens: [token()] });
    const { vm } = await mountPage();
    apiMock.mockReset();
    apiMock.mockResolvedValue(undefined);

    vm.confirmRevoke(vm.tokens[0]!);

    // REVERT PROOF: revoking without a confirmation makes an irreversible
    // action a single mis-click.
    expect(confirmRequire).toHaveBeenCalled();
    expect(lastConfirm().message).toContain('Claude on my laptop');

    await lastConfirm().accept?.();
    await nextTick();

    expect(apiMock).toHaveBeenCalledWith('/api/v1/mcp-tokens/tok-1', { method: 'DELETE' });
    expect(vm.tokens).toHaveLength(0);
    expect(toastSpies.success).toHaveBeenCalled();
  });

  it('keeps the row when the delete fails', async () => {
    apiMock.mockResolvedValue({ tokens: [token()] });
    const { vm } = await mountPage();
    apiMock.mockReset();
    apiMock.mockRejectedValue(new Error('nope'));

    vm.confirmRevoke(vm.tokens[0]!);
    await lastConfirm().accept?.();
    await nextTick();

    // Removing it optimistically would tell the user a still-live token is gone.
    expect(vm.tokens).toHaveLength(1);
    expect(toastSpies.error).toHaveBeenCalledWith('Failed to revoke MCP token');
  });
});
