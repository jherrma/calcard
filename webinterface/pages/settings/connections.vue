<template>
  <div>
    <h2 class="text-2xl font-bold text-surface-900 dark:text-surface-0 mb-2">Connected Accounts</h2>
    <p class="text-sm text-surface-500 dark:text-surface-400 mb-6">
      Manage your linked external authentication providers. You can link additional providers or unlink existing ones.
    </p>

    <CommonLoadingSpinner v-if="loading" />

    <template v-else>
      <!-- Linked providers -->
      <div v-if="linkedProviders.length > 0" class="mb-8">
        <h3 class="text-sm font-semibold text-surface-700 dark:text-surface-300 uppercase tracking-wide mb-3">Linked Providers</h3>
        <div class="space-y-3">
          <div
            v-for="provider in linkedProviders"
            :key="provider.provider"
            class="bg-surface-0 dark:bg-surface-900 rounded-xl border border-surface-200 dark:border-surface-800 p-4"
          >
            <div class="flex items-center justify-between gap-4">
              <div class="flex items-center gap-3">
                <i :class="[getProviderIcon(provider.provider), 'text-xl text-surface-600 dark:text-surface-400']" />
                <div>
                  <div class="font-medium text-surface-900 dark:text-surface-0 capitalize">{{ provider.provider }}</div>
                  <div class="text-sm text-surface-500 dark:text-surface-400">{{ provider.email }}</div>
                  <div class="text-xs text-surface-500 dark:text-surface-400">Linked {{ formatDate(provider.linked_at) }}</div>
                </div>
              </div>
              <Button
                label="Unlink"
                icon="pi pi-times"
                severity="danger"
                text
                size="small"
                :disabled="!canUnlink"
                @click="confirmUnlink(provider)"
              />
            </div>
          </div>
        </div>
        <p v-if="!canUnlink" class="text-xs text-surface-500 dark:text-surface-400 mt-2">
          You cannot unlink your only authentication method. Set a password first.
        </p>
      </div>

      <!-- Available providers to link -->
      <div v-if="availableProviders.length > 0">
        <h3 class="text-sm font-semibold text-surface-700 dark:text-surface-300 uppercase tracking-wide mb-3">Available Providers</h3>
        <div class="space-y-3">
          <div
            v-for="method in availableProviders"
            :key="method.id"
            class="bg-surface-0 dark:bg-surface-900 rounded-xl border border-surface-200 dark:border-surface-800 p-4"
          >
            <div class="flex items-center justify-between gap-4">
              <div class="flex items-center gap-3">
                <i :class="[method.icon || getProviderIcon(method.id), 'text-xl text-surface-600 dark:text-surface-400']" />
                <div>
                  <div class="font-medium text-surface-900 dark:text-surface-0">{{ method.name }}</div>
                  <div class="text-sm text-surface-500 dark:text-surface-400">Not linked</div>
                </div>
              </div>
              <Button
                label="Link"
                icon="pi pi-link"
                severity="secondary"
                size="small"
                @click="linkProvider(method)"
              />
            </div>
          </div>
        </div>
      </div>

      <!-- No providers available -->
      <div
        v-if="linkedProviders.length === 0 && availableProviders.length === 0"
        class="bg-surface-0 dark:bg-surface-900 rounded-xl border border-surface-200 dark:border-surface-800 p-8 text-center"
      >
        <i class="pi pi-link text-4xl text-surface-300 dark:text-surface-600 mb-3" />
        <p class="text-surface-500 dark:text-surface-400">No external authentication providers are configured.</p>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import type { LinkedProvider, LinkedProvidersResponse } from '~/types/settings';
import type { AuthMethod, AuthMethodsResponse } from '~/types/auth';

definePageMeta({
  layout: 'settings',
  middleware: 'auth',
});

const api = useApi();
const toast = useAppToast();
const confirm = useConfirm();

const loading = ref(true);
const linkedProviders = ref<LinkedProvider[]>([]);
const hasPassword = ref(true);
const authMethods = ref<AuthMethod[]>([]);

const canUnlink = computed(() => {
  return hasPassword.value || linkedProviders.value.length > 1;
});

const availableProviders = computed(() => {
  const linkedNames = new Set(linkedProviders.value.map(p => p.provider));
  return authMethods.value.filter(m => m.type !== 'local' && !linkedNames.has(m.id));
});

const getProviderIcon = (provider: string) => {
  const lower = provider.toLowerCase();
  if (lower.includes('google')) return 'pi pi-google';
  if (lower.includes('microsoft') || lower.includes('azure')) return 'pi pi-microsoft';
  if (lower.includes('github')) return 'pi pi-github';
  return 'pi pi-lock';
};

const formatDate = (dateStr: string) => {
  return new Date(dateStr).toLocaleDateString(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
  });
};

const fetchData = async () => {
  loading.value = true;
  try {
    const [providersData, methodsData] = await Promise.all([
      api<LinkedProvidersResponse>('/api/v1/auth/oauth/providers'),
      api<AuthMethodsResponse>('/api/v1/auth/methods'),
    ]);
    linkedProviders.value = providersData.providers || [];
    hasPassword.value = providersData.has_password;
    authMethods.value = methodsData.methods || [];
  } catch {
    toast.error('Failed to load connected accounts');
  } finally {
    loading.value = false;
  }
};

const linkProvider = async (method: AuthMethod) => {
  // The /link route is POST + JWT, so a top-level navigation can't reach it.
  // Kick it off with an authenticated XHR; the backend sets the signed
  // oauth_context cookie on this response and returns the provider URL, then we
  // navigate the browser there. credentials: 'include' ensures the cookie is
  // stored even when the SPA and API are on different origins.
  try {
    const res = await api<{ url: string }>(`/api/v1/auth/oauth/${method.id}/link`, {
      method: 'POST',
      credentials: 'include',
    });
    window.location.href = res.url;
  } catch {
    toast.error('Failed to start linking');
  }
};

const confirmUnlink = (provider: LinkedProvider) => {
  confirm.require({
    message: `Are you sure you want to unlink ${provider.provider}? You will no longer be able to sign in with this provider.`,
    header: 'Unlink Provider',
    icon: 'pi pi-exclamation-triangle',
    acceptClass: 'p-button-danger',
    accept: () => unlinkProvider(provider),
  });
};

const unlinkProvider = async (provider: LinkedProvider) => {
  try {
    await api(`/api/v1/auth/oauth/${provider.provider}`, { method: 'DELETE' });
    linkedProviders.value = linkedProviders.value.filter(p => p.provider !== provider.provider);
    toast.success(`${provider.provider} has been unlinked`);
  } catch {
    toast.error('Failed to unlink provider');
  }
};

onMounted(() => {
  // The OAuth callback redirects link results here in the URL fragment
  // (#linked=<provider> on success, #error=<message> on failure). Surface them
  // as toasts, then strip the fragment so a refresh doesn't re-toast.
  const raw = window.location.hash.startsWith('#')
    ? window.location.hash.slice(1)
    : window.location.hash;
  const params = new URLSearchParams(raw);
  const linked = params.get('linked');
  const err = params.get('error');
  if (linked) toast.success(`${linked} has been linked to your account`);
  if (err) toast.error(err);
  if (linked || err) history.replaceState(null, '', window.location.pathname);
  fetchData();
});
</script>
