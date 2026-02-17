<template>
  <div>
    <h2 class="text-2xl font-bold text-surface-900 dark:text-surface-0 mb-2">App Passwords</h2>
    <p class="text-sm text-surface-500 mb-6">
      App passwords allow third-party applications to access your account with specific scopes.
      They are separate from your main password.
    </p>

    <!-- Create button -->
    <div class="mb-6">
      <Button
        label="Create App Password"
        icon="pi pi-plus"
        @click="showCreateDialog = true"
      />
    </div>

    <!-- App passwords list -->
    <CommonLoadingSpinner v-if="loading" />

    <div v-else-if="appPasswords.length === 0" class="bg-surface-0 dark:bg-surface-900 rounded-xl border border-surface-200 dark:border-surface-800 p-8 text-center">
      <i class="pi pi-key text-4xl text-surface-300 dark:text-surface-600 mb-3" />
      <p class="text-surface-500">No app passwords yet. Create one to get started.</p>
    </div>

    <div v-else class="space-y-3">
      <div
        v-for="ap in appPasswords"
        :key="ap.id"
        class="bg-surface-0 dark:bg-surface-900 rounded-xl border border-surface-200 dark:border-surface-800 p-4"
      >
        <div class="flex items-start justify-between gap-4">
          <div class="flex-1 min-w-0">
            <div class="flex items-center gap-2 mb-1 flex-wrap">
              <span class="font-medium text-surface-900 dark:text-surface-0">{{ ap.name }}</span>
              <Tag v-for="scope in ap.scopes" :key="scope" :value="scope" severity="info" />
            </div>
            <div class="text-sm text-surface-500 space-y-0.5">
              <div><span class="font-medium">Created:</span> {{ formatDate(ap.created_at) }}</div>
              <div v-if="ap.last_used_at">
                <span class="font-medium">Last used:</span> {{ formatRelative(ap.last_used_at) }}
                <span v-if="ap.last_used_ip"> from {{ ap.last_used_ip }}</span>
              </div>
              <div v-else class="text-surface-400">Never used</div>
            </div>
          </div>
          <Button
            icon="pi pi-trash"
            severity="danger"
            text
            rounded
            @click="confirmRevoke(ap)"
            aria-label="Revoke app password"
          />
        </div>
      </div>
    </div>

    <!-- Create Dialog -->
    <Dialog
      v-model:visible="showCreateDialog"
      :header="createdPassword ? 'App Password Created' : 'Create App Password'"
      :modal="true"
      :style="{ width: '28rem' }"
      :closable="!creating"
      @hide="onDialogHide"
    >
      <!-- Creation form -->
      <template v-if="!createdPassword">
        <form @submit.prevent="handleCreate" class="space-y-4">
          <div class="flex flex-col gap-2">
            <label for="ap-name" class="text-sm font-medium text-surface-700 dark:text-surface-300">Name</label>
            <InputText
              id="ap-name"
              v-model="createForm.name"
              placeholder="e.g., Thunderbird, iPhone"
              class="w-full"
              :disabled="creating"
            />
          </div>

          <div class="flex flex-col gap-2">
            <label class="text-sm font-medium text-surface-700 dark:text-surface-300">Scopes</label>
            <div class="flex flex-col gap-2">
              <div class="flex items-center gap-2">
                <Checkbox v-model="createForm.scopes" input-id="scope-caldav" value="caldav" :disabled="creating" />
                <label for="scope-caldav" class="text-sm text-surface-700 dark:text-surface-300">CalDAV (Calendar sync)</label>
              </div>
              <div class="flex items-center gap-2">
                <Checkbox v-model="createForm.scopes" input-id="scope-carddav" value="carddav" :disabled="creating" />
                <label for="scope-carddav" class="text-sm text-surface-700 dark:text-surface-300">CardDAV (Contact sync)</label>
              </div>
            </div>
          </div>

          <Message v-if="createError" severity="error" :closable="true" @close="createError = ''">
            {{ createError }}
          </Message>

          <div class="flex justify-end gap-2 pt-2">
            <Button label="Cancel" severity="secondary" text @click="showCreateDialog = false" :disabled="creating" />
            <Button type="submit" label="Create" icon="pi pi-plus" :loading="creating" />
          </div>
        </form>
      </template>

      <!-- Created password display -->
      <template v-else>
        <Message severity="warn" :closable="false" class="mb-4">
          This password will only be shown once. Copy it now.
        </Message>

        <div class="space-y-4">
          <div class="flex flex-col gap-2">
            <label class="text-sm font-medium text-surface-700 dark:text-surface-300">Password</label>
            <div class="flex gap-2">
              <InputText
                :model-value="createdPassword.password"
                readonly
                class="w-full font-mono text-sm"
              />
              <Button
                icon="pi pi-copy"
                severity="secondary"
                @click="copyToClipboard(createdPassword!.password)"
                aria-label="Copy password"
              />
            </div>
          </div>

          <div class="bg-surface-50 dark:bg-surface-800 rounded-lg p-4 space-y-2 text-sm">
            <h4 class="font-medium text-surface-900 dark:text-surface-0">Connection Details</h4>
            <div class="text-surface-600 dark:text-surface-400">
              <div><span class="font-medium">Username:</span> {{ createdPassword.credentials.username }}</div>
              <div><span class="font-medium">Server URL:</span> {{ createdPassword.credentials.server_url }}</div>
            </div>
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
import type { AppPassword, CreateAppPasswordResponse } from '~/types/settings';

definePageMeta({
  layout: 'settings',
  middleware: 'auth',
});

const api = useApi();
const toast = useAppToast();
const confirm = useConfirm();

const loading = ref(true);
const creating = ref(false);
const createError = ref('');
const appPasswords = ref<AppPassword[]>([]);
const showCreateDialog = ref(false);
const createdPassword = ref<CreateAppPasswordResponse | null>(null);

const createForm = reactive({
  name: '',
  scopes: ['caldav', 'carddav'],
});

const resetCreateForm = () => {
  createForm.name = '';
  createForm.scopes = ['caldav', 'carddav'];
  createError.value = '';
  createdPassword.value = null;
};

const onDialogHide = () => {
  resetCreateForm();
};

const formatDate = (dateStr: string) => {
  return new Date(dateStr).toLocaleDateString(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
  });
};

const formatRelative = (dateStr: string) => {
  const date = new Date(dateStr);
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffMins = Math.floor(diffMs / 60000);
  if (diffMins < 1) return 'just now';
  if (diffMins < 60) return `${diffMins}m ago`;
  const diffHours = Math.floor(diffMins / 60);
  if (diffHours < 24) return `${diffHours}h ago`;
  const diffDays = Math.floor(diffHours / 24);
  if (diffDays < 30) return `${diffDays}d ago`;
  return formatDate(dateStr);
};

const fetchAppPasswords = async () => {
  loading.value = true;
  try {
    const data = await api<{ app_passwords: AppPassword[] }>('/api/v1/app-passwords');
    appPasswords.value = data.app_passwords || [];
  } catch {
    toast.error('Failed to load app passwords');
  } finally {
    loading.value = false;
  }
};

const handleCreate = async () => {
  if (!createForm.name) {
    createError.value = 'Name is required';
    return;
  }
  if (createForm.scopes.length === 0) {
    createError.value = 'At least one scope is required';
    return;
  }

  creating.value = true;
  createError.value = '';

  try {
    const response = await api<CreateAppPasswordResponse>('/api/v1/app-passwords', {
      method: 'POST',
      body: {
        name: createForm.name,
        scopes: createForm.scopes,
      },
    });

    createdPassword.value = response;
    await fetchAppPasswords();
  } catch (e: any) {
    createError.value = e.data?.message || 'Failed to create app password';
  } finally {
    creating.value = false;
  }
};

const copyToClipboard = async (text: string) => {
  try {
    await navigator.clipboard.writeText(text);
    toast.success('Copied to clipboard');
  } catch {
    toast.error('Failed to copy');
  }
};

const confirmRevoke = (ap: AppPassword) => {
  confirm.require({
    message: `Are you sure you want to revoke "${ap.name}"? Any clients using this password will lose access.`,
    header: 'Revoke App Password',
    icon: 'pi pi-exclamation-triangle',
    acceptClass: 'p-button-danger',
    accept: () => revokeAppPassword(ap),
  });
};

const revokeAppPassword = async (ap: AppPassword) => {
  try {
    await api(`/api/v1/app-passwords/${ap.id}`, { method: 'DELETE' });
    appPasswords.value = appPasswords.value.filter(p => p.id !== ap.id);
    toast.success(`"${ap.name}" has been revoked`);
  } catch {
    toast.error('Failed to revoke app password');
  }
};

onMounted(fetchAppPasswords);
</script>
