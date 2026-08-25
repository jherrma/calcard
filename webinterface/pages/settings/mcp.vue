<template>
  <div>
    <h2 class="text-2xl font-bold text-surface-900 dark:text-surface-0 mb-2">MCP Access</h2>
    <p class="text-sm text-surface-500 dark:text-surface-400 mb-6">
      Let an AI assistant read and manage your calendars and contacts through the
      Model Context Protocol. A token grants the assistant everything you can see
      yourself — including collections shared with you — so create one per client
      and revoke it the moment you stop using it.
    </p>

    <!-- Connection details -->
    <div class="bg-surface-0 dark:bg-surface-900 rounded-xl border border-surface-200 dark:border-surface-800 p-4 mb-6">
      <h3 class="font-medium text-surface-900 dark:text-surface-0 mb-3">Connecting a client</h3>
      <div class="flex flex-col gap-2 mb-3">
        <label for="mcp-endpoint" class="text-sm font-medium text-surface-700 dark:text-surface-300">Server URL</label>
        <div class="flex gap-2">
          <InputText id="mcp-endpoint" :model-value="endpoint" readonly class="w-full font-mono text-sm" />
          <Button
            icon="pi pi-copy"
            severity="secondary"
            aria-label="Copy server URL"
            @click="copyToClipboard(endpoint, 'Server URL copied')"
          />
        </div>
      </div>
      <p class="text-sm text-surface-500 dark:text-surface-400 mb-2">
        The transport is Streamable HTTP. Authenticate with the token as a bearer credential:
      </p>
      <pre class="bg-surface-50 dark:bg-surface-800 rounded-lg p-3 text-xs overflow-x-auto text-surface-700 dark:text-surface-300"><code>claude mcp add --transport http calcard {{ endpoint }} \
  --header "Authorization: Bearer &lt;your token&gt;"</code></pre>
    </div>

    <!-- Create button -->
    <div class="mb-6">
      <Button label="Create Token" icon="pi pi-plus" @click="showCreateDialog = true" />
    </div>

    <!-- Token list -->
    <CommonLoadingSpinner v-if="loading" />

    <!-- A failed load must never render as "you have no tokens": that is a
         false statement about the user's account, and it hides the one token
         they may urgently want to revoke. -->
    <Message v-else-if="loadError" severity="error" :closable="false" class="mb-4">
      <div class="flex items-center justify-between gap-4">
        <span>{{ loadError }}</span>
        <Button label="Retry" icon="pi pi-refresh" severity="secondary" size="small" @click="fetchTokens" />
      </div>
    </Message>

    <div
      v-else-if="tokens.length === 0"
      class="bg-surface-0 dark:bg-surface-900 rounded-xl border border-surface-200 dark:border-surface-800 p-8 text-center"
    >
      <i class="pi pi-sparkles text-4xl text-surface-300 dark:text-surface-600 mb-3" />
      <p class="text-surface-500 dark:text-surface-400">No MCP tokens yet. Create one to connect an assistant.</p>
    </div>

    <div v-else class="space-y-3">
      <div
        v-for="token in tokens"
        :key="token.id"
        class="bg-surface-0 dark:bg-surface-900 rounded-xl border border-surface-200 dark:border-surface-800 p-4"
      >
        <div class="flex items-start justify-between gap-4">
          <div class="flex-1 min-w-0">
            <div class="flex items-center gap-2 mb-1 flex-wrap">
              <span class="font-medium text-surface-900 dark:text-surface-0">{{ token.name }}</span>
              <Tag v-if="isExpired(token)" value="expired" severity="danger" />
            </div>
            <div class="text-sm text-surface-500 dark:text-surface-400 space-y-0.5">
              <div>
                <span class="font-medium">Token:</span>
                <span class="font-mono">{{ token.token_prefix }}…</span>
              </div>
              <div><span class="font-medium">Created:</span> {{ formatDate(token.created_at) }}</div>
              <div v-if="token.expires_at">
                <span class="font-medium">Expires:</span> {{ formatDate(token.expires_at) }}
              </div>
              <div v-if="token.last_used_at">
                <span class="font-medium">Last used:</span> {{ formatRelative(token.last_used_at) }}
                <span v-if="token.last_used_ip"> from {{ token.last_used_ip }}</span>
              </div>
              <div v-else>Never used</div>
            </div>
          </div>
          <Button
            icon="pi pi-trash"
            severity="danger"
            text
            rounded
            aria-label="Revoke MCP token"
            @click="confirmRevoke(token)"
          />
        </div>
      </div>
    </div>

    <!-- Create dialog -->
    <Dialog
      v-model:visible="showCreateDialog"
      :header="createdToken ? 'MCP Token Created' : 'Create MCP Token'"
      :modal="true"
      :style="{ width: '30rem' }"
      :closable="!creating"
      @hide="resetCreateForm"
    >
      <template v-if="!createdToken">
        <form class="space-y-4" @submit.prevent="handleCreate">
          <div class="flex flex-col gap-2">
            <label for="mcp-name" class="text-sm font-medium text-surface-700 dark:text-surface-300">Name</label>
            <InputText
              id="mcp-name"
              v-model="createForm.name"
              placeholder="e.g., Claude on my laptop"
              class="w-full"
              :disabled="creating"
            />
            <small class="text-surface-500 dark:text-surface-400">
              Only for your own reference in this list.
            </small>
          </div>

          <div class="flex flex-col gap-2">
            <label for="mcp-expiry" class="text-sm font-medium text-surface-700 dark:text-surface-300">
              Expires
            </label>
            <DatePicker
              id="mcp-expiry"
              v-model="createForm.expiresAt"
              date-format="yy-mm-dd"
              :min-date="tomorrow"
              show-icon
              show-button-bar
              class="w-full"
              :disabled="creating"
            />
            <small class="text-surface-500 dark:text-surface-400">
              Optional. Leave empty for a token that stays valid until you revoke it.
            </small>
          </div>

          <Message v-if="createError" severity="error" :closable="true" @close="createError = ''">
            {{ createError }}
          </Message>

          <div class="flex justify-end gap-2 pt-2">
            <Button
              label="Cancel"
              severity="secondary"
              text
              :disabled="creating"
              @click="showCreateDialog = false"
            />
            <Button type="submit" label="Create" icon="pi pi-plus" :loading="creating" />
          </div>
        </form>
      </template>

      <template v-else>
        <Message severity="warn" :closable="false" class="mb-4">
          This token is shown once and cannot be retrieved again. Copy it now.
        </Message>

        <div class="space-y-4">
          <div class="flex flex-col gap-2">
            <label for="mcp-secret" class="text-sm font-medium text-surface-700 dark:text-surface-300">Token</label>
            <div class="flex gap-2">
              <InputText
                id="mcp-secret"
                :model-value="createdToken.token"
                readonly
                class="w-full font-mono text-sm"
              />
              <Button
                icon="pi pi-copy"
                severity="secondary"
                aria-label="Copy token"
                @click="copyToClipboard(createdToken!.token, 'Token copied')"
              />
            </div>
          </div>

          <div class="bg-surface-50 dark:bg-surface-800 rounded-lg p-4 space-y-2 text-sm">
            <h4 class="font-medium text-surface-900 dark:text-surface-0">Add it to your client</h4>
            <pre class="text-xs overflow-x-auto text-surface-600 dark:text-surface-400"><code>claude mcp add --transport http calcard {{ endpoint }} \
  --header "Authorization: Bearer {{ createdToken.token }}"</code></pre>
            <Button
              label="Copy command"
              icon="pi pi-copy"
              severity="secondary"
              text
              size="small"
              @click="copyToClipboard(connectCommand, 'Command copied')"
            />
          </div>

          <div class="flex justify-end pt-2">
            <Button label="Done" @click="showCreateDialog = false" />
          </div>
        </div>
      </template>
    </Dialog>
  </div>
</template>

<script setup lang="ts">
import type { MCPToken, MCPTokenCreateResponse, MCPTokenListResponse } from '~/types/settings';

definePageMeta({
  layout: 'settings',
  middleware: 'auth',
});

const api = useApi();
const toast = useAppToast();
const confirm = useConfirm();
const config = useRuntimeConfig();

const loading = ref(true);
const loadError = ref<string | null>(null);
const creating = ref(false);
const createError = ref('');
const tokens = ref<MCPToken[]>([]);
const showCreateDialog = ref(false);
const createdToken = ref<MCPTokenCreateResponse | null>(null);

const createForm = reactive<{ name: string; expiresAt: Date | null }>({
  name: '',
  expiresAt: null,
});

// An expiry must be in the future, and a date-only picker resolves to midnight,
// so the earliest selectable day is tomorrow.
const tomorrow = new Date(Date.now() + 24 * 60 * 60 * 1000);

// The MCP endpoint sits at /mcp on the API origin, not under /api/v1 — the
// protocol addresses one URL that clients are configured with directly. In the
// single-container deployment apiBaseUrl is empty, so the origin is this page's.
const endpoint = computed(() => {
  const base = (config.public.apiBaseUrl as string) || window.location.origin;
  return `${base.replace(/\/$/, '')}/mcp`;
});

const connectCommand = computed(() =>
  createdToken.value
    ? `claude mcp add --transport http calcard ${endpoint.value} --header "Authorization: Bearer ${createdToken.value.token}"`
    : ''
);

const isExpired = (token: MCPToken) =>
  !!token.expires_at && new Date(token.expires_at).getTime() < Date.now();

const formatDate = (dateStr: string) =>
  new Date(dateStr).toLocaleDateString(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
  });

const formatRelative = (dateStr: string) => {
  const diffMins = Math.floor((Date.now() - new Date(dateStr).getTime()) / 60000);
  if (diffMins < 1) return 'just now';
  if (diffMins < 60) return `${diffMins}m ago`;
  const diffHours = Math.floor(diffMins / 60);
  if (diffHours < 24) return `${diffHours}h ago`;
  const diffDays = Math.floor(diffHours / 24);
  if (diffDays < 30) return `${diffDays}d ago`;
  return formatDate(dateStr);
};

const resetCreateForm = () => {
  createForm.name = '';
  createForm.expiresAt = null;
  createError.value = '';
  createdToken.value = null;
};

const fetchTokens = async () => {
  loading.value = true;
  try {
    const data = await api<MCPTokenListResponse>('/api/v1/mcp-tokens');
    tokens.value = data.tokens || [];
    loadError.value = null;
  } catch {
    loadError.value = 'Failed to load MCP tokens. Your existing tokens are unaffected.';
    toast.error('Failed to load MCP tokens');
  } finally {
    loading.value = false;
  }
};

const handleCreate = async () => {
  if (!createForm.name.trim()) {
    createError.value = 'Name is required';
    return;
  }

  creating.value = true;
  createError.value = '';

  try {
    createdToken.value = await api<MCPTokenCreateResponse>('/api/v1/mcp-tokens', {
      method: 'POST',
      body: {
        name: createForm.name.trim(),
        // The picker yields local midnight; the API takes RFC 3339.
        expires_at: createForm.expiresAt ? createForm.expiresAt.toISOString() : null,
      },
    });
    await fetchTokens();
  } catch (e: any) {
    createError.value = e.data?.message || 'Failed to create MCP token';
  } finally {
    creating.value = false;
  }
};

const copyToClipboard = async (text: string, message: string) => {
  try {
    await navigator.clipboard.writeText(text);
    toast.success(message);
  } catch {
    toast.error('Failed to copy');
  }
};

const confirmRevoke = (token: MCPToken) => {
  confirm.require({
    message: `Revoke "${token.name}"? Any assistant using it loses access immediately.`,
    header: 'Revoke MCP Token',
    icon: 'pi pi-exclamation-triangle',
    acceptClass: 'p-button-danger',
    accept: () => revokeToken(token),
  });
};

const revokeToken = async (token: MCPToken) => {
  try {
    await api(`/api/v1/mcp-tokens/${token.id}`, { method: 'DELETE' });
    tokens.value = tokens.value.filter(t => t.id !== token.id);
    toast.success(`"${token.name}" has been revoked`);
  } catch {
    toast.error('Failed to revoke MCP token');
  }
};

onMounted(fetchTokens);
</script>
