<template>
  <div>
    <h2 class="text-2xl font-bold text-surface-900 dark:text-surface-0 mb-6">Profile</h2>

    <CommonLoadingSpinner v-if="loading" />

    <template v-else-if="profile">
      <!-- Account Overview -->
      <div class="grid grid-cols-3 gap-4 mb-8">
        <div class="bg-surface-0 dark:bg-surface-900 rounded-xl border border-surface-200 dark:border-surface-800 p-4 text-center">
          <div class="text-2xl font-bold text-primary-600 dark:text-primary-400">{{ profile.stats.calendar_count }}</div>
          <div class="text-sm text-surface-500 mt-1">Calendars</div>
        </div>
        <div class="bg-surface-0 dark:bg-surface-900 rounded-xl border border-surface-200 dark:border-surface-800 p-4 text-center">
          <div class="text-2xl font-bold text-primary-600 dark:text-primary-400">{{ profile.stats.contact_count }}</div>
          <div class="text-sm text-surface-500 mt-1">Contacts</div>
        </div>
        <div class="bg-surface-0 dark:bg-surface-900 rounded-xl border border-surface-200 dark:border-surface-800 p-4 text-center">
          <div class="text-2xl font-bold text-primary-600 dark:text-primary-400">{{ profile.stats.app_password_count }}</div>
          <div class="text-sm text-surface-500 mt-1">App Passwords</div>
        </div>
      </div>

      <!-- Profile Form -->
      <div class="bg-surface-0 dark:bg-surface-900 rounded-xl border border-surface-200 dark:border-surface-800 p-6">
        <form @submit.prevent="handleSave" class="space-y-5">
          <div class="flex flex-col gap-2">
            <label for="display_name" class="text-sm font-medium text-surface-700 dark:text-surface-300">Display Name</label>
            <InputText
              id="display_name"
              v-model="form.display_name"
              placeholder="Your display name"
              class="w-full"
            />
          </div>

          <div class="flex flex-col gap-2">
            <label class="text-sm font-medium text-surface-700 dark:text-surface-300">Email</label>
            <InputText
              :model-value="profile.email"
              disabled
              class="w-full"
            />
            <small class="text-surface-500">Email cannot be changed</small>
          </div>

          <div class="flex flex-col gap-2">
            <label class="text-sm font-medium text-surface-700 dark:text-surface-300">Username</label>
            <InputText
              :model-value="authStore.user?.username"
              disabled
              class="w-full"
            />
          </div>

          <div class="flex items-center gap-3">
            <label class="text-sm font-medium text-surface-700 dark:text-surface-300">Role</label>
            <Tag :value="authStore.isAdmin ? 'Admin' : 'User'" :severity="authStore.isAdmin ? 'warn' : 'info'" />
          </div>

          <div class="flex flex-col gap-1">
            <label class="text-sm font-medium text-surface-700 dark:text-surface-300">Member Since</label>
            <span class="text-sm text-surface-600 dark:text-surface-400">{{ formatDate(profile.created_at) }}</span>
          </div>

          <div class="pt-2">
            <Button
              type="submit"
              label="Save Changes"
              icon="pi pi-check"
              :loading="saving"
            />
          </div>
        </form>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import type { UserProfile } from '~/types/settings';

definePageMeta({
  layout: 'settings',
  middleware: 'auth',
});

const authStore = useAuthStore();
const api = useApi();
const toast = useAppToast();

const loading = ref(true);
const saving = ref(false);
const profile = ref<UserProfile | null>(null);
const form = reactive({
  display_name: '',
});

const formatDate = (dateStr: string) => {
  return new Date(dateStr).toLocaleDateString(undefined, {
    year: 'numeric',
    month: 'long',
    day: 'numeric',
  });
};

const fetchProfile = async () => {
  loading.value = true;
  try {
    const data = await api<UserProfile>('/api/v1/users/me');
    profile.value = data;
    form.display_name = data.display_name || '';
  } catch {
    toast.error('Failed to load profile');
  } finally {
    loading.value = false;
  }
};

const handleSave = async () => {
  saving.value = true;
  try {
    await api('/api/v1/users/me', {
      method: 'PATCH',
      body: { display_name: form.display_name },
    });
    await authStore.fetchUser();
    toast.success('Profile updated successfully');
  } catch (e: any) {
    toast.error(e.data?.message || 'Failed to update profile');
  } finally {
    saving.value = false;
  }
};

onMounted(fetchProfile);
</script>
