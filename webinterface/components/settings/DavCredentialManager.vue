<template>
  <div>
    <h2 class="text-2xl font-bold text-surface-900 dark:text-surface-0 mb-2">{{ title }}</h2>
    <p class="text-sm text-surface-500 dark:text-surface-400 mb-6">{{ description }}</p>
    <SettingsDavUrlBox :variant="apiEndpoint.includes('carddav') ? 'carddav' : 'caldav'" />

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
      <p class="text-surface-500 dark:text-surface-400">No credentials yet. Create one to get started.</p>
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
            <div class="text-sm text-surface-500 dark:text-surface-400 space-y-0.5">
              <div><span class="font-medium">Username:</span> {{ cred.username }}</div>
              <div><span class="font-medium">Created:</span> {{ formatDate(cred.created_at) }}</div>
              <div v-if="cred.expires_at"><span class="font-medium">Expires:</span> {{ formatDate(cred.expires_at) }}</div>
              <div v-if="cred.last_used_at">
                <span class="font-medium">Last used:</span> {{ formatRelative(cred.last_used_at) }}
                <span v-if="cred.last_used_ip"> from {{ cred.last_used_ip }}</span>
              </div>
              <div v-else class="text-surface-500 dark:text-surface-400">Never used</div>
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
      :header="createdCredential ? 'Credential Created' : 'Create Credential'"
      :modal="true"
      :style="{ width: '28rem' }"
      :closable="!creating"
      @after-hide="clearCreateDialog"
    >
      <!-- Shown once: the server keeps only a hash of the password. -->
      <div v-if="createdCredential" class="space-y-4">
        <Message severity="warn" :closable="false">
          This password will only be shown once. Copy it into your client now.
        </Message>

        <div class="flex flex-col gap-2">
          <label for="created-username" class="text-sm font-medium text-surface-700 dark:text-surface-300">Username</label>
          <div class="flex gap-2">
            <InputText id="created-username" :model-value="createdCredential.username" readonly class="w-full font-mono text-sm" />
            <Button
              icon="pi pi-copy"
              severity="secondary"
              aria-label="Copy username"
              title="Copy username"
              @click="copyToClipboard(createdCredential!.username, 'Username copied')"
            />
          </div>
        </div>

        <div class="flex flex-col gap-2">
          <label for="created-password" class="text-sm font-medium text-surface-700 dark:text-surface-300">Password</label>
          <div class="flex gap-2">
            <InputText id="created-password" :model-value="createdCredential.password" readonly class="w-full font-mono text-sm" />
            <Button
              icon="pi pi-copy"
              severity="secondary"
              aria-label="Copy password"
              title="Copy password"
              @click="copyToClipboard(createdCredential!.password, 'Password copied')"
            />
          </div>
        </div>

        <div class="flex justify-end pt-2">
          <Button label="Done" @click="showCreateDialog = false" />
        </div>
      </div>

      <form v-else @submit.prevent="handleCreate" class="space-y-4">
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

        <!-- Username and password are generated when the dialog opens. -->
        <div class="flex flex-col gap-4 rounded-lg border border-surface-200 dark:border-surface-700 p-3">
          <div class="flex items-center justify-between gap-2">
            <span class="text-sm text-surface-500 dark:text-surface-400">Generated for you. You can change them.</span>
            <Button
              icon="pi pi-refresh"
              severity="secondary"
              text
              rounded
              aria-label="Generate a new username and password"
              title="Generate a new username and password"
              :disabled="creating"
              @click="regenerateCredentials"
            />
          </div>

          <div class="flex flex-col gap-2">
            <label for="cred-username" class="text-sm font-medium text-surface-700 dark:text-surface-300">Username</label>
            <div class="flex gap-2">
              <InputText
                id="cred-username"
                v-model="createForm.username"
                class="w-full font-mono text-sm"
                autocomplete="off"
                autocapitalize="off"
                spellcheck="false"
                :invalid="!!usernameMessage"
                aria-describedby="cred-username-help"
                :disabled="creating"
              />
              <Button
                icon="pi pi-copy"
                severity="secondary"
                text
                aria-label="Copy username"
                title="Copy username"
                :disabled="!createForm.username"
                @click="copyToClipboard(createForm.username, 'Username copied')"
              />
            </div>
            <small v-if="usernameMessage" id="cred-username-help" class="text-red-600 dark:text-red-400">
              {{ usernameMessage }}
            </small>
            <small v-else id="cred-username-help" class="text-surface-500 dark:text-surface-400">
              {{ usernameHint }}
            </small>
          </div>

          <div class="flex flex-col gap-2">
            <label for="cred-password" class="text-sm font-medium text-surface-700 dark:text-surface-300">Password</label>
            <div class="flex gap-2">
              <InputText
                id="cred-password"
                v-model="createForm.password"
                :type="showPassword ? 'text' : 'password'"
                class="w-full font-mono text-sm"
                autocomplete="new-password"
                spellcheck="false"
                :disabled="creating"
              />
              <Button
                :icon="showPassword ? 'pi pi-eye-slash' : 'pi pi-eye'"
                severity="secondary"
                text
                :aria-label="showPassword ? 'Hide password' : 'Show password'"
                :title="showPassword ? 'Hide password' : 'Show password'"
                :aria-pressed="showPassword"
                @click="showPassword = !showPassword"
              />
              <Button
                icon="pi pi-copy"
                severity="secondary"
                text
                aria-label="Copy password"
                title="Copy password"
                :disabled="!createForm.password"
                @click="copyToClipboard(createForm.password, 'Password copied')"
              />
            </div>
            <AuthPasswordStrength :password="createForm.password" />
          </div>
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
            Expiration <span class="text-surface-500 dark:text-surface-400 font-normal">(optional)</span>
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

        <Message v-if="createNotice" severity="warn" :closable="true" @close="createNotice = ''">
          {{ createNotice }}
        </Message>

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

// Username and password are generated in the browser (utils/davCredentials.ts)
// every time the dialog opens; "Generate" draws both again.
const showPassword = ref(false);
// The username the generator produced last. Tells a generated name, which may
// be replaced after a 409, from one the user typed, which must not be.
const generatedUsername = ref('');
// A 409 for a username the user typed, shown while that username is unchanged.
const usernameConflict = ref<{ username: string; message: string } | null>(null);
// Not an error: e.g. "the generated username was taken, here is a new one".
const createNotice = ref('');
// The credential just created. Its password is shown once, here: the server
// stores only a hash and the list never contains it.
const createdCredential = ref<{ username: string; password: string } | null>(null);

const usernameHint = `Lowercase letters a–z only, ${DAV_USERNAME_MIN_LENGTH}–${DAV_USERNAME_MAX_LENGTH} characters.`;

// Upper case is reported, never lower-cased behind the user's back: the name
// has to reach the DAV client exactly as the server stores it.
const usernameMessage = computed(() => {
  const conflict = usernameConflict.value;
  if (conflict && conflict.username === createForm.username) return conflict.message;
  return davUsernameError(createForm.username);
});

const regenerateCredentials = () => {
  createForm.username = generateDavUsername();
  createForm.password = generateDavPassword();
  generatedUsername.value = createForm.username;
  usernameConflict.value = null;
  createNotice.value = '';
};

const resetCreateForm = () => {
  createForm.name = '';
  createForm.permission = 'read-write';
  createForm.expires_at = null;
  createError.value = '';
  createdCredential.value = null;
  showPassword.value = false;
  regenerateCredentials();
};

// Fresh values on every open. Closing drops the secrets (after the animation,
// so the dialog does not flip back to the form while fading out).
watch(showCreateDialog, (open) => {
  if (open) resetCreateForm();
});

const clearCreateDialog = () => {
  createdCredential.value = null;
  createForm.password = '';
  showPassword.value = false;
};

// Also over plain http, where navigator.clipboard does not exist.
const copyToClipboard = useCopyToClipboard();

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

// 409: the username is taken (credential usernames are unique across all
// accounts). A generated one is replaced and the user asked to submit again —
// never silently, they may already have copied it. A typed one is kept.
const handleUsernameTaken = () => {
  const taken = createForm.username;
  if (taken === generatedUsername.value) {
    createForm.username = generateDavUsername();
    generatedUsername.value = createForm.username;
    createNotice.value = `The username "${taken}" is already in use, so a new one was generated. Check it and press Create again.`;
  } else {
    usernameConflict.value = {
      username: taken,
      message: 'This username is already in use. Choose another one or generate one.',
    };
  }
};

const handleCreate = async () => {
  createError.value = '';
  createNotice.value = '';
  if (!createForm.name.trim()) {
    createError.value = 'Name is required';
    return;
  }
  // Already shown next to the field.
  if (davUsernameError(createForm.username)) return;
  if (createForm.password.length < DAV_PASSWORD_MIN_LENGTH) {
    createError.value = `Password must be at least ${DAV_PASSWORD_MIN_LENGTH} characters`;
    return;
  }

  creating.value = true;

  try {
    const body: Record<string, unknown> = {
      name: createForm.name.trim(),
      username: createForm.username,
      password: createForm.password,
      permission: createForm.permission,
    };
    if (createForm.expires_at) {
      body.expires_at = createForm.expires_at.toISOString();
    }

    const created = await api<DavCredentialCreateResponse>(props.apiEndpoint, {
      method: 'POST',
      body,
    });

    createdCredential.value = { username: created?.username || createForm.username, password: createForm.password };
    toast.success('Credential created successfully');
    await fetchCredentials();
  } catch (e: any) {
    if ((e?.statusCode ?? e?.status) === 409) {
      handleUsernameTaken();
    } else {
      createError.value = e?.data?.message || 'Failed to create credential';
    }
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
