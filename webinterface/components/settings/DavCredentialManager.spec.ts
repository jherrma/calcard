// @vitest-environment nuxt
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { mount } from '@vue/test-utils';
import { nextTick } from 'vue';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import DavCredentialManager from './DavCredentialManager.vue';
import type { DavCredentialCreateResponse } from '~/types/settings';

// SCOPE: the create dialog of the CalDAV/CardDAV credential pages — the
// generated username and password, the lowercase-only username rule the
// server enforces, the one-time display of the password, and the 409 path.

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

const ENDPOINT = '/api/v1/caldav-credentials';

interface ManagerVm {
  showCreateDialog: boolean;
  showPassword: boolean;
  createForm: { name: string; username: string; password: string; permission: string; expires_at: Date | null };
  createError: string;
  createNotice: string;
  usernameMessage: string | null;
  createdCredential: { username: string; password: string } | null;
  regenerateCredentials: () => void;
  handleCreate: () => Promise<void>;
  copyToClipboard: (text: string, message: string) => Promise<void>;
  clearCreateDialog: () => void;
}

async function mountManager() {
  const wrapper = mount(DavCredentialManager, {
    props: { title: 'CalDAV Credentials', description: 'd', apiEndpoint: ENDPOINT, icon: 'pi pi-calendar' },
    shallow: true,
    global: { renderStubDefaultSlot: true },
  });
  await nextTick();
  await nextTick();
  return { wrapper, vm: wrapper.vm as unknown as ManagerVm };
}

async function openDialog(vm: ManagerVm) {
  vm.showCreateDialog = true;
  await nextTick();
}

function created(username: string): DavCredentialCreateResponse {
  return { id: 'cred-1', name: 'Phone', username, permission: 'read-write', created_at: '2026-10-13T00:00:00Z' };
}

function conflict(username: string) {
  return { statusCode: 409, data: { error: 'conflict', message: `username '${username}' is already in use` } };
}

beforeEach(() => {
  vi.clearAllMocks();
  apiMock.mockReset();
  apiMock.mockResolvedValue({ credentials: [] });
});

describe('generated credentials', () => {
  it('opens with a generated 16-letter username and a strong password', async () => {
    const { vm } = await mountManager();
    await openDialog(vm);

    expect(vm.createForm.username).toMatch(/^[a-z]{16}$/);
    expect(vm.createForm.password).toHaveLength(24);
    expect(vm.createForm.password).toMatch(/[A-Z]/);
    expect(vm.createForm.password).toMatch(/[a-z]/);
    expect(vm.createForm.password).toMatch(/[0-9]/);
    expect(vm.usernameMessage).toBeNull();
  });

  it('draws new values on every open and on Generate', async () => {
    const { vm } = await mountManager();
    await openDialog(vm);
    const first = { ...vm.createForm };

    vm.regenerateCredentials();
    expect(vm.createForm.username).not.toBe(first.username);
    expect(vm.createForm.password).not.toBe(first.password);
    expect(vm.createForm.username).toMatch(/^[a-z]{16}$/);

    const second = { ...vm.createForm };
    vm.showCreateDialog = false;
    await nextTick();
    await openDialog(vm);
    expect(vm.createForm.username).not.toBe(second.username);
    expect(vm.createForm.password).not.toBe(second.password);
  });

  it('masks the password until the eye toggle reveals it', async () => {
    const { wrapper, vm } = await mountManager();
    await openDialog(vm);

    expect(wrapper.find('#cred-password').attributes('type')).toBe('password');
    vm.showPassword = true;
    await nextTick();
    expect(wrapper.find('#cred-password').attributes('type')).toBe('text');
  });

  it('copies the username and the password', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } });
    const { vm } = await mountManager();
    await openDialog(vm);

    await vm.copyToClipboard(vm.createForm.password, 'Password copied');
    expect(writeText).toHaveBeenCalledWith(vm.createForm.password);
    expect(toastSpies.success).toHaveBeenCalledWith('Password copied');
    vi.unstubAllGlobals();
  });
});

describe('username rule', () => {
  it('reports upper case instead of lower-casing it, and sends nothing', async () => {
    const { wrapper, vm } = await mountManager();
    await openDialog(vm);
    apiMock.mockClear();

    vm.createForm.name = 'Phone';
    vm.createForm.username = 'Thunderbird';
    await nextTick();
    expect(vm.usernameMessage).toBe('Only lowercase letters a–z are allowed');
    expect(wrapper.text()).toContain('Only lowercase letters a–z are allowed');

    await vm.handleCreate();
    // REVERT PROOF: a form that lower-cases silently would send "thunderbird".
    expect(vm.createForm.username).toBe('Thunderbird');
    expect(apiMock).not.toHaveBeenCalled();
  });

  it('refuses digits and symbols the old rule allowed', async () => {
    const { vm } = await mountManager();
    await openDialog(vm);
    for (const name of ['phone2', 'my-phone', 'my_phone']) {
      vm.createForm.username = name;
      expect(vm.usernameMessage, name).toBe('Only lowercase letters a–z are allowed');
    }
    vm.createForm.username = 'ab';
    expect(vm.usernameMessage).toBe('Username must be 3–50 letters long');
  });
});

describe('creation', () => {
  it('sends the generated values and shows the password once', async () => {
    const { wrapper, vm } = await mountManager();
    await openDialog(vm);
    const { username, password } = vm.createForm;

    apiMock.mockReset();
    apiMock.mockResolvedValueOnce(created(username));
    apiMock.mockResolvedValueOnce({ credentials: [] });

    vm.createForm.name = '  Phone  ';
    await vm.handleCreate();
    await nextTick();

    expect(apiMock).toHaveBeenNthCalledWith(1, ENDPOINT, {
      method: 'POST',
      body: { name: 'Phone', username, password, permission: 'read-write' },
    });
    expect(apiMock).toHaveBeenNthCalledWith(2, ENDPOINT);
    // The password exists nowhere else afterwards: the list never carries it.
    expect(vm.createdCredential).toEqual({ username, password });
    expect(wrapper.text()).toContain('only be shown once');
    expect(wrapper.find('#created-password').attributes('model-value')).toBe(password);

    // Closing wipes it.
    vm.clearCreateDialog();
    expect(vm.createdCredential).toBeNull();
    expect(vm.createForm.password).toBe('');
  });

  it('a 409 for a generated username draws a new one, keeps the password and asks to retry', async () => {
    const { vm } = await mountManager();
    await openDialog(vm);
    const { username, password } = vm.createForm;
    apiMock.mockReset();
    apiMock.mockRejectedValueOnce(conflict(username));

    vm.createForm.name = 'Phone';
    await vm.handleCreate();

    expect(vm.createForm.username).toMatch(/^[a-z]{16}$/);
    expect(vm.createForm.username).not.toBe(username);
    expect(vm.createForm.password).toBe(password);
    expect(vm.createNotice).toContain(username);
    expect(vm.createNotice).toContain('press Create again');
    expect(vm.createdCredential).toBeNull();
    // Nothing is retried behind the user's back.
    expect(apiMock).toHaveBeenCalledTimes(1);
  });

  it('a 409 for a typed username keeps it and says so next to the field', async () => {
    const { vm } = await mountManager();
    await openDialog(vm);
    apiMock.mockReset();
    apiMock.mockRejectedValueOnce(conflict('myphone'));

    vm.createForm.name = 'Phone';
    vm.createForm.username = 'myphone';
    await vm.handleCreate();

    expect(vm.createForm.username).toBe('myphone');
    expect(vm.usernameMessage).toBe('This username is already in use. Choose another one or generate one.');
    vm.createForm.username = 'myotherphone';
    expect(vm.usernameMessage).toBeNull();
  });

  it('shows any other server message verbatim', async () => {
    const { vm } = await mountManager();
    await openDialog(vm);
    apiMock.mockReset();
    apiMock.mockRejectedValueOnce({ statusCode: 400, data: { message: 'expires_at must be in the future' } });

    vm.createForm.name = 'Phone';
    await vm.handleCreate();

    expect(vm.createError).toBe('expires_at must be in the future');
    expect(vm.createdCredential).toBeNull();
  });

  it('requires a name and an 8-character password before calling the API', async () => {
    const { vm } = await mountManager();
    await openDialog(vm);
    apiMock.mockClear();

    vm.createForm.name = '   ';
    await vm.handleCreate();
    expect(vm.createError).toBe('Name is required');

    vm.createForm.name = 'Phone';
    vm.createForm.password = 'short';
    await vm.handleCreate();
    expect(vm.createError).toBe('Password must be at least 8 characters');
    expect(apiMock).not.toHaveBeenCalled();
  });
});
