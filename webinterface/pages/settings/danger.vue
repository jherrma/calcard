<template>
  <div>
    <h2 class="text-2xl font-bold text-red-600 dark:text-red-400 mb-6">Danger Zone</h2>

    <div class="bg-surface-0 dark:bg-surface-900 rounded-xl border-2 border-red-300 dark:border-red-800 p-6">
      <h3 class="text-lg font-semibold text-surface-900 dark:text-surface-0 mb-2">Delete Account</h3>
      <p class="text-sm text-surface-600 dark:text-surface-400 mb-4">
        Once you delete your account, there is no going back. This action is permanent.
      </p>

      <div class="bg-red-50 dark:bg-red-900/20 rounded-lg p-4 mb-6">
        <p class="text-sm font-medium text-red-800 dark:text-red-300 mb-2">This will permanently delete:</p>
        <ul class="text-sm text-red-700 dark:text-red-400 space-y-1 list-disc list-inside">
          <li>Your user account and profile</li>
          <li>All calendars and events</li>
          <li>All address books and contacts</li>
          <li>All app passwords and DAV credentials</li>
          <li>All linked OAuth accounts</li>
        </ul>
      </div>

      <form @submit.prevent="handleDelete" class="space-y-4">
        <div class="flex flex-col gap-2">
          <label for="delete-password" class="text-sm font-medium text-surface-700 dark:text-surface-300">
            Enter your password to confirm
          </label>
          <Password
            id="delete-password"
            v-model="form.password"
            :feedback="false"
            toggle-mask
            placeholder="Your password"
            class="w-full"
            input-class="w-full"
            :disabled="isDeleting"
          />
        </div>

        <div class="flex flex-col gap-2">
          <label for="delete-confirm" class="text-sm font-medium text-surface-700 dark:text-surface-300">
            Type <code class="bg-surface-100 dark:bg-surface-800 px-1.5 py-0.5 rounded text-red-600 dark:text-red-400 font-mono text-sm">DELETE</code> to confirm
          </label>
          <InputText
            id="delete-confirm"
            v-model="form.confirmation"
            placeholder="DELETE"
            class="w-full"
            :disabled="isDeleting"
          />
        </div>

        <Message v-if="error" severity="error" :closable="true" @close="error = ''">
          {{ error }}
        </Message>

        <Button
          type="submit"
          label="Delete My Account"
          icon="pi pi-trash"
          severity="danger"
          :loading="isDeleting"
          :disabled="!canSubmit"
        />
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
const api = useApi();
const toast = useAppToast();

const isDeleting = ref(false);
const error = ref('');

const form = reactive({
  password: '',
  confirmation: '',
});

const canSubmit = computed(() => {
  return form.password.length > 0 && form.confirmation === 'DELETE';
});

const handleDelete = async () => {
  if (!canSubmit.value) return;

  isDeleting.value = true;
  error.value = '';

  try {
    await api('/api/v1/users/me', {
      method: 'DELETE',
      body: {
        password: form.password,
        confirmation: form.confirmation,
      },
    });

    authStore.clearAuth();
    navigateTo('/auth/login');
    toast.success('Your account has been deleted');
  } catch (e: any) {
    error.value = e.data?.message || 'Failed to delete account';
  } finally {
    isDeleting.value = false;
  }
};
</script>
