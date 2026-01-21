# Story 038: Settings Pages

## Title
Implement User Settings, Profile, and Credentials Management

## Description
As a user, I want to manage my profile, passwords, and access credentials through the settings pages.

## Related Acceptance Criteria

| ID | Criterion |
|----|-----------|
| UM-1.3.1 | Users can update their display name |
| UM-1.3.2 | Users can view their account creation date |
| UM-1.3.3 | Users can delete their account |
| UM-1.2.1 | Users can change their password |
| AU-2.4.1 | Users can create app-specific passwords |
| AU-2.4.3 | Users can name app passwords |
| AU-2.4.4 | Users can view list of app passwords |
| AU-2.4.6 | Users can revoke individual app passwords |
| AU-2.5.1 | Users can create CalDAV access credentials |
| AU-2.6.1 | Users can create CardDAV access credentials |

## Acceptance Criteria

### Settings Layout

- [ ] Route: `/settings`
- [ ] Sidebar navigation for settings sections
- [ ] Sections:
  - [ ] Profile
  - [ ] Password
  - [ ] App Passwords
  - [ ] CalDAV Credentials
  - [ ] CardDAV Credentials
  - [ ] Connected Accounts (OAuth)
  - [ ] Danger Zone (delete account)

### Profile Settings

- [ ] Route: `/settings/profile`
- [ ] Display current user info
- [ ] Editable fields:
  - [ ] Display name
  - [ ] Username (with warning about DAV URL change)
- [ ] Read-only fields:
  - [ ] Email
  - [ ] Account created date
  - [ ] Last login
- [ ] Stats display:
  - [ ] Number of calendars
  - [ ] Number of contacts
  - [ ] Number of app passwords

### Password Settings

- [ ] Route: `/settings/password`
- [ ] Change password form:
  - [ ] Current password
  - [ ] New password
  - [ ] Confirm new password
- [ ] Password strength indicator
- [ ] Warning: "All other sessions will be logged out"
- [ ] Success message on change

### App Passwords

- [ ] Route: `/settings/app-passwords`
- [ ] List of existing app passwords:
  - [ ] Name
  - [ ] Scopes (CalDAV, CardDAV)
  - [ ] Created date
  - [ ] Last used date
  - [ ] Revoke button
- [ ] "Create App Password" button
- [ ] Create dialog:
  - [ ] Name input
  - [ ] Scope checkboxes (CalDAV, CardDAV)
  - [ ] Generated password display (copy button)
  - [ ] Warning: "This password will only be shown once"

### CalDAV Credentials

- [ ] Route: `/settings/caldav-credentials`
- [ ] List of existing credentials:
  - [ ] Name
  - [ ] Username
  - [ ] Permission (read/read-write)
  - [ ] Expires at
  - [ ] Last used date
  - [ ] Revoke button
- [ ] "Create Credential" button
- [ ] Create dialog:
  - [ ] Name input
  - [ ] Custom username input
  - [ ] Permission selector
  - [ ] Expiration date (optional)
  - [ ] Generated password display

### CardDAV Credentials

- [ ] Route: `/settings/carddav-credentials`
- [ ] Same UI as CalDAV credentials
- [ ] Separate list and creation

### Connected Accounts

- [ ] Route: `/settings/connections`
- [ ] List of linked OAuth providers:
  - [ ] Provider name and icon
  - [ ] Provider email
  - [ ] Linked date
  - [ ] Unlink button (if other auth method exists)
- [ ] "Link Account" buttons for available providers
- [ ] Warning when unlinking last auth method

### Delete Account

- [ ] Route: `/settings/danger`
- [ ] Warning about data deletion
- [ ] Requires password confirmation
- [ ] Requires typing "DELETE" to confirm
- [ ] Lists what will be deleted:
  - [ ] All calendars and events
  - [ ] All address books and contacts
  - [ ] All app passwords and credentials

## Technical Notes

### Settings Layout
```vue
<!-- layouts/settings.vue -->
<template>
  <div class="min-h-screen bg-gray-100">
    <AppHeader @toggle-sidebar="sidebarOpen = !sidebarOpen" />

    <div class="flex">
      <!-- Settings sidebar -->
      <aside class="w-64 bg-white border-r min-h-[calc(100vh-4rem)] hidden lg:block">
        <nav class="p-4 space-y-1">
          <NuxtLink
            v-for="item in settingsNav"
            :key="item.to"
            :to="item.to"
            :class="[
              'flex items-center gap-3 px-3 py-2 rounded-lg text-sm',
              isActive(item.to)
                ? 'bg-primary-50 text-primary-700 font-medium'
                : 'text-gray-700 hover:bg-gray-50'
            ]"
          >
            <i :class="item.icon" />
            {{ item.label }}
          </NuxtLink>
        </nav>
      </aside>

      <!-- Content -->
      <main class="flex-1 p-6">
        <div class="max-w-3xl">
          <slot />
        </div>
      </main>
    </div>
  </div>
</template>

<script setup lang="ts">
const route = useRoute();
const sidebarOpen = ref(false);

const settingsNav = [
  { to: '/settings/profile', label: 'Profile', icon: 'pi pi-user' },
  { to: '/settings/password', label: 'Password', icon: 'pi pi-lock' },
  { to: '/settings/app-passwords', label: 'App Passwords', icon: 'pi pi-key' },
  { to: '/settings/caldav-credentials', label: 'CalDAV Credentials', icon: 'pi pi-calendar' },
  { to: '/settings/carddav-credentials', label: 'CardDAV Credentials', icon: 'pi pi-users' },
  { to: '/settings/connections', label: 'Connected Accounts', icon: 'pi pi-link' },
  { to: '/settings/danger', label: 'Danger Zone', icon: 'pi pi-exclamation-triangle' },
];

const isActive = (path: string) => route.path === path;
</script>
```

### Profile Page
```vue
<!-- pages/settings/profile.vue -->
<template>
  <div>
    <h1 class="text-2xl font-semibold mb-6">Profile</h1>

    <div class="bg-white rounded-lg shadow">
      <!-- Stats -->
      <div class="p-6 border-b">
        <h2 class="text-lg font-medium mb-4">Account Overview</h2>
        <div class="grid grid-cols-3 gap-4">
          <div class="text-center p-4 bg-gray-50 rounded-lg">
            <div class="text-2xl font-bold text-primary-600">{{ stats.calendar_count }}</div>
            <div class="text-sm text-gray-500">Calendars</div>
          </div>
          <div class="text-center p-4 bg-gray-50 rounded-lg">
            <div class="text-2xl font-bold text-primary-600">{{ stats.contact_count }}</div>
            <div class="text-sm text-gray-500">Contacts</div>
          </div>
          <div class="text-center p-4 bg-gray-50 rounded-lg">
            <div class="text-2xl font-bold text-primary-600">{{ stats.app_password_count }}</div>
            <div class="text-sm text-gray-500">App Passwords</div>
          </div>
        </div>
      </div>

      <!-- Profile form -->
      <form @submit.prevent="updateProfile" class="p-6 space-y-4">
        <div>
          <label class="block text-sm font-medium text-gray-700 mb-1">Email</label>
          <InputText :value="user?.email" disabled class="w-full bg-gray-50" />
          <small class="text-gray-500">Email cannot be changed</small>
        </div>

        <div>
          <label class="block text-sm font-medium text-gray-700 mb-1">Username</label>
          <InputText v-model="form.username" class="w-full" />
          <Message v-if="form.username !== user?.username" severity="warn" class="mt-2">
            Changing your username will change your CalDAV/CardDAV URLs.
            You will need to reconfigure your sync clients.
          </Message>
        </div>

        <div>
          <label class="block text-sm font-medium text-gray-700 mb-1">Display Name</label>
          <InputText v-model="form.display_name" class="w-full" />
        </div>

        <div>
          <label class="block text-sm font-medium text-gray-700 mb-1">Member Since</label>
          <InputText :value="formatDate(user?.created_at)" disabled class="w-full bg-gray-50" />
        </div>

        <div class="pt-4">
          <Button
            label="Save Changes"
            :loading="isSaving"
            type="submit"
          />
        </div>
      </form>
    </div>
  </div>
</template>

<script setup lang="ts">
definePageMeta({
  layout: 'settings',
  middleware: 'auth',
});

const authStore = useAuthStore();
const toast = useAppToast();
const api = useApi();

const user = computed(() => authStore.user);
const isSaving = ref(false);

const form = reactive({
  username: user.value?.username || '',
  display_name: user.value?.display_name || '',
});

const stats = ref({
  calendar_count: 0,
  contact_count: 0,
  app_password_count: 0,
});

onMounted(async () => {
  const profile = await api.get<any>('/api/v1/users/me');
  stats.value = profile.stats;
});

const updateProfile = async () => {
  isSaving.value = true;
  try {
    await api.patch('/api/v1/users/me', form);
    await authStore.fetchUser();
    toast.success('Profile updated');
  } catch (e: any) {
    toast.error(e.message || 'Failed to update profile');
  } finally {
    isSaving.value = false;
  }
};

const formatDate = (dateStr?: string) => {
  if (!dateStr) return '';
  return new Date(dateStr).toLocaleDateString('en-US', {
    year: 'numeric',
    month: 'long',
    day: 'numeric',
  });
};
</script>
```

### App Passwords Page
```vue
<!-- pages/settings/app-passwords.vue -->
<template>
  <div>
    <div class="flex items-center justify-between mb-6">
      <h1 class="text-2xl font-semibold">App Passwords</h1>
      <Button
        label="Create App Password"
        icon="pi pi-plus"
        @click="showCreateDialog = true"
      />
    </div>

    <div class="bg-white rounded-lg shadow">
      <div class="p-4 border-b">
        <p class="text-sm text-gray-600">
          App passwords let you sign in to CalDAV/CardDAV clients without using your main password.
          Each app password can be limited to specific services.
        </p>
      </div>

      <!-- Password list -->
      <div v-if="passwords.length === 0" class="p-8 text-center text-gray-500">
        No app passwords created yet
      </div>

      <div v-else class="divide-y">
        <div
          v-for="pwd in passwords"
          :key="pwd.id"
          class="p-4 flex items-center justify-between"
        >
          <div>
            <div class="font-medium">{{ pwd.name }}</div>
            <div class="text-sm text-gray-500">
              <span class="inline-flex gap-1">
                <Tag
                  v-for="scope in pwd.scopes"
                  :key="scope"
                  :value="scope"
                  severity="info"
                  class="text-xs"
                />
              </span>
            </div>
            <div class="text-xs text-gray-400 mt-1">
              Created {{ formatDate(pwd.created_at) }}
              <span v-if="pwd.last_used_at">
                · Last used {{ formatRelative(pwd.last_used_at) }}
              </span>
            </div>
          </div>
          <Button
            label="Revoke"
            severity="danger"
            text
            size="small"
            @click="confirmRevoke(pwd)"
          />
        </div>
      </div>
    </div>

    <!-- Create dialog -->
    <Dialog
      v-model:visible="showCreateDialog"
      header="Create App Password"
      :modal="true"
      :style="{ width: '450px' }"
    >
      <div v-if="!createdPassword" class="space-y-4">
        <div>
          <label class="block text-sm font-medium text-gray-700 mb-1">Name</label>
          <InputText
            v-model="newPassword.name"
            class="w-full"
            placeholder="e.g., DAVx5 Phone"
          />
        </div>

        <div>
          <label class="block text-sm font-medium text-gray-700 mb-2">Access</label>
          <div class="space-y-2">
            <div class="flex items-center gap-2">
              <Checkbox v-model="newPassword.scopes" value="caldav" input-id="scope-caldav" />
              <label for="scope-caldav">CalDAV (Calendars)</label>
            </div>
            <div class="flex items-center gap-2">
              <Checkbox v-model="newPassword.scopes" value="carddav" input-id="scope-carddav" />
              <label for="scope-carddav">CardDAV (Contacts)</label>
            </div>
          </div>
        </div>
      </div>

      <!-- Created password display -->
      <div v-else class="space-y-4">
        <Message severity="warn" :closable="false">
          Make sure to copy this password now. You won't be able to see it again!
        </Message>

        <div>
          <label class="block text-sm font-medium text-gray-700 mb-1">Password</label>
          <div class="flex gap-2">
            <InputText
              :value="createdPassword"
              readonly
              class="flex-1 font-mono"
            />
            <Button
              icon="pi pi-copy"
              severity="secondary"
              @click="copyPassword"
            />
          </div>
        </div>

        <div class="text-sm text-gray-600">
          <p class="font-medium">To use this password:</p>
          <ul class="list-disc list-inside mt-2 space-y-1">
            <li>Username: <code class="bg-gray-100 px-1">{{ authStore.user?.username }}</code></li>
            <li>Password: <code class="bg-gray-100 px-1">(the password above)</code></li>
          </ul>
        </div>
      </div>

      <template #footer>
        <div v-if="!createdPassword">
          <Button label="Cancel" severity="secondary" @click="showCreateDialog = false" />
          <Button
            label="Create"
            :loading="isCreating"
            :disabled="!newPassword.name || newPassword.scopes.length === 0"
            @click="createPassword"
          />
        </div>
        <div v-else>
          <Button label="Done" @click="closeCreateDialog" />
        </div>
      </template>
    </Dialog>

    <ConfirmDialog />
  </div>
</template>

<script setup lang="ts">
import type { AppPassword } from '~/types';

definePageMeta({
  layout: 'settings',
  middleware: 'auth',
});

const authStore = useAuthStore();
const toast = useAppToast();
const confirm = useConfirm();
const api = useApi();

const passwords = ref<AppPassword[]>([]);
const showCreateDialog = ref(false);
const isCreating = ref(false);
const createdPassword = ref<string | null>(null);

const newPassword = reactive({
  name: '',
  scopes: ['caldav', 'carddav'],
});

onMounted(async () => {
  await fetchPasswords();
});

const fetchPasswords = async () => {
  const response = await api.get<{ app_passwords: AppPassword[] }>('/api/v1/app-passwords');
  passwords.value = response.app_passwords;
};

const createPassword = async () => {
  isCreating.value = true;
  try {
    const response = await api.post<{ password: string }>('/api/v1/app-passwords', newPassword);
    createdPassword.value = response.password;
    await fetchPasswords();
  } catch (e: any) {
    toast.error(e.message || 'Failed to create password');
  } finally {
    isCreating.value = false;
  }
};

const closeCreateDialog = () => {
  showCreateDialog.value = false;
  createdPassword.value = null;
  newPassword.name = '';
  newPassword.scopes = ['caldav', 'carddav'];
};

const copyPassword = async () => {
  if (createdPassword.value) {
    await navigator.clipboard.writeText(createdPassword.value);
    toast.success('Password copied');
  }
};

const confirmRevoke = (pwd: AppPassword) => {
  confirm.require({
    message: `Revoke "${pwd.name}"? Any apps using this password will stop working.`,
    header: 'Revoke App Password',
    icon: 'pi pi-exclamation-triangle',
    acceptClass: 'p-button-danger',
    accept: () => revokePassword(pwd),
  });
};

const revokePassword = async (pwd: AppPassword) => {
  try {
    await api.delete(`/api/v1/app-passwords/${pwd.id}`);
    toast.success('Password revoked');
    await fetchPasswords();
  } catch {
    toast.error('Failed to revoke password');
  }
};

const formatDate = (dateStr: string) => {
  return new Date(dateStr).toLocaleDateString();
};

const formatRelative = (dateStr: string) => {
  const date = new Date(dateStr);
  const now = new Date();
  const diff = now.getTime() - date.getTime();
  const days = Math.floor(diff / (1000 * 60 * 60 * 24));

  if (days === 0) return 'today';
  if (days === 1) return 'yesterday';
  if (days < 7) return `${days} days ago`;
  return date.toLocaleDateString();
};
</script>
```

### Delete Account Page
```vue
<!-- pages/settings/danger.vue -->
<template>
  <div>
    <h1 class="text-2xl font-semibold mb-6 text-red-600">Danger Zone</h1>

    <div class="bg-white rounded-lg shadow border-2 border-red-200">
      <div class="p-6">
        <h2 class="text-lg font-medium text-red-600 mb-4">Delete Account</h2>

        <p class="text-gray-600 mb-4">
          Once you delete your account, there is no going back. Please be certain.
        </p>

        <div class="bg-red-50 border border-red-200 rounded-lg p-4 mb-6">
          <p class="font-medium text-red-800 mb-2">This will permanently delete:</p>
          <ul class="list-disc list-inside text-sm text-red-700 space-y-1">
            <li>All your calendars and events</li>
            <li>All your address books and contacts</li>
            <li>All your app passwords and credentials</li>
            <li>All calendar and address book shares</li>
            <li>Your account and profile information</li>
          </ul>
        </div>

        <form @submit.prevent="deleteAccount" class="space-y-4">
          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1">
              Enter your password to confirm
            </label>
            <Password
              v-model="form.password"
              :feedback="false"
              toggle-mask
              class="w-full"
            />
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1">
              Type <strong>DELETE</strong> to confirm
            </label>
            <InputText
              v-model="form.confirmation"
              class="w-full"
              placeholder="DELETE"
            />
          </div>

          <Button
            label="Delete My Account"
            severity="danger"
            :loading="isDeleting"
            :disabled="form.confirmation !== 'DELETE' || !form.password"
            type="submit"
          />
        </form>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
definePageMeta({
  layout: 'settings',
  middleware: 'auth',
});

const authStore = useAuthStore();
const toast = useAppToast();
const api = useApi();

const isDeleting = ref(false);

const form = reactive({
  password: '',
  confirmation: '',
});

const deleteAccount = async () => {
  isDeleting.value = true;
  try {
    await api.delete('/api/v1/users/me', {
      body: JSON.stringify({
        password: form.password,
        confirmation: form.confirmation,
      }),
    });
    authStore.clearAuth();
    navigateTo('/auth/login');
  } catch (e: any) {
    toast.error(e.message || 'Failed to delete account');
  } finally {
    isDeleting.value = false;
  }
};
</script>
```

## Definition of Done

- [ ] Settings layout with sidebar navigation
- [ ] Profile page displays and updates user info
- [ ] Username change shows warning about DAV URLs
- [ ] Password change form works
- [ ] App passwords list displayed
- [ ] Create app password shows password once
- [ ] Copy password button works
- [ ] Revoke app password with confirmation
- [ ] CalDAV credentials management
- [ ] CardDAV credentials management
- [ ] Connected accounts (OAuth) management
- [ ] Delete account requires password + "DELETE"
- [ ] Delete account lists what will be deleted
- [ ] Success/error toasts displayed
- [ ] Responsive design
