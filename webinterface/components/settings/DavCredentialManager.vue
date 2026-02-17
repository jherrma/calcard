<template>
  <div>
    <h2 class="text-2xl font-bold text-surface-900 dark:text-surface-0 mb-2">{{ title }}</h2>
    <p class="text-sm text-surface-500 mb-6">{{ description }}</p>

    <!-- Create button -->
    <div class="mb-6">
      <Button
        label="Create Credential"
        icon="pi pi-plus"
        @click="showCreateDialog = true"
      />
    </div>

    <!-- Credentials list -->
    <CommonLoadingSpinner v-if="loading" />

    <div v-else-if="credentials.length === 0" class="bg-surface-0 dark:bg-surface-900 rounded-xl border border-surface-200 dark:border-surface-800 p-8 text-center">
      <i :class="[icon, 'text-4xl text-surface-300 dark:text-surface-600 mb-3']" />
      <p class="text-surface-500">No credentials yet. Create one to get started.</p>
    </div>

    <div v-else class="space-y-3">
      <div
        v-for="cred in credentials"
        :key="cred.id"
        class="bg-surface-0 dark:bg-surface-900 rounded-xl border border-surface-200 dark:border-surface-800 p-4"
      >
        <div class="flex items-start justify-between gap-4">
          <div class="flex-1 min-w-0">
            <div class="flex items-center gap-2 mb-1">
              <span class="font-medium text-surface-900 dark:text-surface-0">{{ cred.name }}</span>
              <Tag :value="cred.permission" :severity="cred.permission === 'read-write' ? 'success' : 'info'" />
            </div>
            <div class="text-sm text-surface-500 space-y-0.5">
              <div><span class="font-medium">Username:</span> {{ cred.username }}</div>
              <div><span class="font-medium">Created:</span> {{ formatDate(cred.created_at) }}</div>
              <div v-if="cred.expires_at"><span class="font-medium">Expires:</span> {{ formatDate(cred.expires_at) }}</div>
              <div v-if="cred.last_used_at">
                <span class="font-medium">Last used:</span> {{ formatRelative(cred.last_used_at) }}
                <span v-if="cred.last_used_ip"> from {{ cred.last_used_ip }}</span>
              </div>
              <div v-else class="text-surface-400">Never used</div>
            </div>
          </div>
          <Button
            icon="pi pi-trash"
            severity="danger"
            text
            rounded
            @click="confirmRevoke(cred)"
            aria-label="Revoke credential"
          />
        </div>
      </div>
    </div>

    <!-- Create Dialog -->
    <Dialog
      v-model:visible="showCreateDialog"
      header="Create Credential"
      :modal="true"
      :style="{ width: '28rem' }"
      :closable="!creating"
    >
      <form @submit.prevent="handleCreate" class="space-y-4">
        <div class="flex flex-col gap-2">
          <label for="cred-name" class="text-sm font-medium text-surface-700 dark:text-surface-300">Name</label>
          <InputText
            id="cred-name"
            v-model="createForm.name"
            placeholder="e.g., Thunderbird, iPhone"
            class="w-full"
            :disabled="creating"
          />
        </div>

        <div class="flex flex-col gap-2">
          <label for="cred-username" class="text-sm font-medium text-surface-700 dark:text-surface-300">Username</label>
          <InputText
            id="cred-username"
            v-model="createForm.username"
            placeholder="Username for this credential"
            class="w-full"
            :disabled="creating"
          />
        </div>

        <div class="flex flex-col gap-2">
          <label for="cred-password" class="text-sm font-medium text-surface-700 dark:text-surface-300">Password</label>
          <Password
            id="cred-password"
            v-model="createForm.password"
            :feedback="false"
            toggle-mask
            placeholder="Password for this credential"
            class="w-full"
            input-class="w-full"
            :disabled="creating"
          />
          <AuthPasswordStrength :password="createForm.password" />
        </div>

        <div class="flex flex-col gap-2">
          <label class="text-sm font-medium text-surface-700 dark:text-surface-300">Permission</label>
          <div class="flex gap-4">
            <div class="flex items-center gap-2">
              <RadioButton v-model="createForm.permission" input-id="perm-read" value="read" :disabled="creating" />
              <label for="perm-read" class="text-sm text-surface-700 dark:text-surface-300">Read only</label>
            </div>
            <div class="flex items-center gap-2">
              <RadioButton v-model="createForm.permission" input-id="perm-rw" value="read-write" :disabled="creating" />
              <label for="perm-rw" class="text-sm text-surface-700 dark:text-surface-300">Read &amp; Write</label>
            </div>
          </div>
        </div>

        <div class="flex flex-col gap-2">
          <label for="cred-expires" class="text-sm font-medium text-surface-700 dark:text-surface-300">
            Expiration <span class="text-surface-400 font-normal">(optional)</span>
          </label>
          <DatePicker
            id="cred-expires"
            v-model="createForm.expires_at"
            :min-date="tomorrow"
            date-format="yy-mm-dd"
            placeholder="No expiration"
            class="w-full"
            :disabled="creating"
            show-icon
          />
        </div>

        <Message v-if="createError" severity="error" :closable="true" @close="createError = ''">
          {{ createError }}
        </Message>

        <div class="flex justify-end gap-2 pt-2">
          <Button label="Cancel" severity="secondary" text @click="showCreateDialog = false" :disabled="creating" />
          <Button type="submit" label="Create" icon="pi pi-plus" :loading="creating" />
        </div>
      </form>
    </Dialog>
  </div>
</template>

<script setup lang="ts">
import type { DavCredential, DavCredentialListResponse, DavCredentialCreateResponse } from '~/types/settings';

const props = defineProps<{
  title: string;
  description: string;
  apiEndpoint: string;
  icon: string;
}>();

const api = useApi();
const toast = useAppToast();
const confirm = useConfirm();

const loading = ref(true);
const credentials = ref<DavCredential[]>([]);
const showCreateDialog = ref(false);
const creating = ref(false);
const createError = ref('');

const tomorrow = computed(() => {
  const d = new Date();
  d.setDate(d.getDate() + 1);
  return d;
});

const createForm = reactive({
  name: '',
  username: '',
  password: '',
  permission: 'read-write',
  expires_at: null as Date | null,
});

const resetCreateForm = () => {
  createForm.name = '';
  createForm.username = '';
  createForm.password = '';
  createForm.permission = 'read-write';
  createForm.expires_at = null;
  createError.value = '';
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

const fetchCredentials = async () => {
  loading.value = true;
  try {
    const data = await api<DavCredentialListResponse>(props.apiEndpoint);
    credentials.value = data.credentials || [];
  } catch {
    toast.error('Failed to load credentials');
  } finally {
    loading.value = false;
  }
};

const handleCreate = async () => {
  if (!createForm.name || !createForm.username || !createForm.password) {
    createError.value = 'Name, username, and password are required';
    return;
  }

  creating.value = true;
  createError.value = '';

  try {
    const body: Record<string, unknown> = {
      name: createForm.name,
      username: createForm.username,
      password: createForm.password,
      permission: createForm.permission,
    };
    if (createForm.expires_at) {
      body.expires_at = createForm.expires_at.toISOString();
    }

    await api<DavCredentialCreateResponse>(props.apiEndpoint, {
      method: 'POST',
      body,
    });

    toast.success('Credential created successfully');
    showCreateDialog.value = false;
    resetCreateForm();
    await fetchCredentials();
  } catch (e: any) {
    createError.value = e.data?.message || 'Failed to create credential';
  } finally {
    creating.value = false;
  }
};

const confirmRevoke = (cred: DavCredential) => {
  confirm.require({
    message: `Are you sure you want to revoke "${cred.name}"? Any clients using this credential will lose access.`,
    header: 'Revoke Credential',
    icon: 'pi pi-exclamation-triangle',
    acceptClass: 'p-button-danger',
    accept: () => revokeCredential(cred),
  });
};

const revokeCredential = async (cred: DavCredential) => {
  try {
    await api(`${props.apiEndpoint}/${cred.id}`, { method: 'DELETE' });
    credentials.value = credentials.value.filter(c => c.id !== cred.id);
    toast.success(`"${cred.name}" has been revoked`);
  } catch {
    toast.error('Failed to revoke credential');
  }
};

onMounted(fetchCredentials);
</script>
