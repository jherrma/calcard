// @vitest-environment nuxt
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { mount, flushPromises } from '@vue/test-utils';
import { nextTick } from 'vue';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import { useRouter } from '#imports';
import SetupPage from './setup.vue';

// SCOPE: the first-run setup page's behaviour that is easy to break silently —
// it must leave once setup is done (the server answers 409 forever after), it
// must lower-case the username the way the server does, and a failed follow-up
// sign-in must NOT read as "setup failed" (retrying would only hit a 409).
// The password field applies the server's full policy before anything is sent.

const { apiMock } = vi.hoisted(() => ({ apiMock: vi.fn() }));
mockNuxtImport('useApi', () => () => apiMock);

// Spy on the real router rather than mocking useRouter: Nuxt's own plugins call
// useRouter() too, and a bare stub breaks them (router.beforeResolve missing).
// (Created in beforeEach: useRouter() needs a running Nuxt app.)
let routerSpies: { push: ReturnType<typeof vi.spyOn>; replace: ReturnType<typeof vi.spyOn> };

const { authStoreMock } = vi.hoisted(() => ({
  authStoreMock: { setupAdmin: vi.fn(), login: vi.fn() },
}));
mockNuxtImport('useAuthStore', () => () => authStoreMock);

interface PageVm {
  form: { setup_token: string; username: string; email: string; display_name: string; password: string; confirmPassword: string };
  error: string;
  handleSetup: () => Promise<void>;
}

async function mountWrapper(adminConfigured: boolean, tokenRequired = false) {
  apiMock.mockResolvedValue({
    admin_configured: adminConfigured,
    smtp_enabled: false,
    registration_enabled: true,
    setup_token_required: tokenRequired,
  });
  const wrapper = mount(SetupPage, { shallow: true, global: { renderStubDefaultSlot: true } });
  await flushPromises();
  return wrapper;
}

async function mountPage(adminConfigured: boolean, tokenRequired = false) {
  return (await mountWrapper(adminConfigured, tokenRequired)).vm as unknown as PageVm;
}

function fillValidForm(vm: PageVm, username = 'Admin') {
  vm.form.username = username;
  vm.form.email = 'admin@example.test';
  vm.form.display_name = 'Admin';
  vm.form.password = 'SecurePass123!';
  vm.form.confirmPassword = 'SecurePass123!';
}

beforeEach(() => {
  vi.clearAllMocks();
  routerSpies = {
    push: vi.spyOn(useRouter(), 'push').mockResolvedValue(undefined),
    replace: vi.spyOn(useRouter(), 'replace').mockResolvedValue(undefined),
  };
  apiMock.mockReset();
  authStoreMock.setupAdmin.mockReset().mockResolvedValue(undefined);
  authStoreMock.login.mockReset().mockResolvedValue(undefined);
});

describe('setup page', () => {
  it('sends visitors away once an account exists', async () => {
    // Revert caught: dropping the onMounted check leaves a dead form that 409s.
    await mountPage(true);
    expect(routerSpies.replace).toHaveBeenCalledWith('/auth/login');
  });

  it('stays on the page while the server is unconfigured', async () => {
    await mountPage(false);
    expect(routerSpies.replace).not.toHaveBeenCalled();
  });

  it('lower-cases the username and signs the admin in without remember-me', async () => {
    // Revert caught: the server lower-cases usernames, so "Admin" must not be
    // rejected client-side or stored differently; the admin gets a session-only
    // sign-in (remember: false), not a 7-day cookie.
    const vm = await mountPage(false);
    fillValidForm(vm, 'Admin');
    await vm.handleSetup();

    expect(authStoreMock.setupAdmin).toHaveBeenCalledWith(expect.objectContaining({ username: 'admin' }));
    expect(authStoreMock.login).toHaveBeenCalledWith({
      email: 'admin@example.test',
      password: 'SecurePass123!',
      remember: false,
    });
    expect(routerSpies.push).toHaveBeenCalledWith('/');
  });

  it('shows the server message and does not sign in when setup fails', async () => {
    const vm = await mountPage(false);
    authStoreMock.setupAdmin.mockRejectedValue({ data: { message: 'setup already completed' } });
    fillValidForm(vm);
    await vm.handleSetup();

    expect(vm.error).toBe('setup already completed');
    expect(authStoreMock.login).not.toHaveBeenCalled();
  });

  it('does not report "setup failed" when only the follow-up sign-in fails', async () => {
    // Revert caught: one try/catch around both calls tells the user setup
    // failed, and retrying returns 409 for an account that does exist.
    const vm = await mountPage(false);
    authStoreMock.login.mockRejectedValue(new Error('network'));
    fillValidForm(vm);
    await vm.handleSetup();

    expect(vm.error).toBe('');
    expect(routerSpies.replace).toHaveBeenCalledWith('/auth/login');
  });

  // Revert caught: with only minLength(8) these reach the server, which
  // answers 400 (or, over 72 bytes, used to fail at the bcrypt hash).
  it.each([
    ['alllowercase1!', 'Password must contain at least one uppercase letter'],
    ['NoSpecial123', 'Password must contain at least one special character'],
    ['Aa1!' + 'x'.repeat(69), 'Password must be at most 72 bytes'],
  ])('refuses the password %s with the rule it breaks', async (password, message) => {
    const wrapper = await mountWrapper(false);
    const vm = wrapper.vm as unknown as PageVm;
    fillValidForm(vm);
    vm.form.password = password;
    vm.form.confirmPassword = password;
    await vm.handleSetup();
    await nextTick();

    expect(authStoreMock.setupAdmin).not.toHaveBeenCalled();
    expect(wrapper.text()).toContain(message);
  });

  it('refuses a username the server would reject', async () => {
    const vm = await mountPage(false);
    fillValidForm(vm, 'bad name');
    await vm.handleSetup();

    expect(authStoreMock.setupAdmin).not.toHaveBeenCalled();
  });

  it('sends the setup token only when the server asks for one', async () => {
    const vm = await mountPage(false, true);
    fillValidForm(vm);
    vm.form.setup_token = 's3cret-token';
    await vm.handleSetup();

    expect(authStoreMock.setupAdmin).toHaveBeenCalledWith(expect.objectContaining({ setup_token: 's3cret-token' }));
  });

  it('refuses to submit without the token when one is required', async () => {
    // Revert caught: the server answers 403 to a missing token; the form should
    // stop first instead of burning the user's attempt.
    const vm = await mountPage(false, true);
    fillValidForm(vm);
    await vm.handleSetup();

    expect(authStoreMock.setupAdmin).not.toHaveBeenCalled();
  });

  it('does not send a token field when none is required', async () => {
    const vm = await mountPage(false, false);
    fillValidForm(vm);
    vm.form.setup_token = 'leftover';
    await vm.handleSetup();

    const sent = authStoreMock.setupAdmin.mock.calls[0]?.[0];
    expect(sent).not.toHaveProperty('setup_token');
  });

  it('reveals the token field after a 403 even if the settings fetch had failed', async () => {
    // Revert caught: with the settings call failing, the field never shows and
    // every submit 403s with no way to enter the token.
    apiMock.mockRejectedValue(new Error('settings unreachable'));
    const wrapper = mount(SetupPage, { shallow: true, global: { renderStubDefaultSlot: true } });
    await flushPromises();
    const vm = wrapper.vm as unknown as PageVm & { tokenRequired: boolean };
    expect(vm.tokenRequired).toBe(false);

    authStoreMock.setupAdmin.mockRejectedValue({ statusCode: 403, data: { message: 'invalid setup token' } });
    fillValidForm(vm);
    await vm.handleSetup();

    expect(vm.tokenRequired).toBe(true);
    expect(vm.error).toBe('invalid setup token');
  });
});
