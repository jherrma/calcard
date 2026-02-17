<template>
  <div>
    <h2 class="text-2xl font-bold text-surface-900 dark:text-surface-0 mb-6">Change Password</h2>

    <div class="bg-surface-0 dark:bg-surface-900 rounded-xl border border-surface-200 dark:border-surface-800 p-6">
      <form @submit.prevent="handleSubmit" class="space-y-5">
        <div class="flex flex-col gap-2">
          <label for="current_password" class="text-sm font-medium text-surface-700 dark:text-surface-300">Current Password</label>
          <Password
            id="current_password"
            v-model="form.current_password"
            :feedback="false"
            toggle-mask
            placeholder="Enter current password"
            class="w-full"
            input-class="w-full"
            :class="{ 'p-invalid': v$.current_password.$error }"
          />
          <small v-if="v$.current_password.$error" class="p-error">{{ v$.current_password.$errors[0]?.$message }}</small>
        </div>

        <div class="flex flex-col gap-2">
          <label for="new_password" class="text-sm font-medium text-surface-700 dark:text-surface-300">New Password</label>
          <Password
            id="new_password"
            v-model="form.new_password"
            :feedback="false"
            toggle-mask
            placeholder="Enter new password"
            class="w-full"
            input-class="w-full"
            :class="{ 'p-invalid': v$.new_password.$error }"
          />
          <small v-if="v$.new_password.$error" class="p-error">{{ v$.new_password.$errors[0]?.$message }}</small>
          <AuthPasswordStrength :password="form.new_password" />
        </div>

        <div class="flex flex-col gap-2">
          <label for="confirm_password" class="text-sm font-medium text-surface-700 dark:text-surface-300">Confirm New Password</label>
          <Password
            id="confirm_password"
            v-model="form.confirm_password"
            :feedback="false"
            toggle-mask
            placeholder="Confirm new password"
            class="w-full"
            input-class="w-full"
            :class="{ 'p-invalid': v$.confirm_password.$error }"
          />
          <small v-if="v$.confirm_password.$error" class="p-error">{{ v$.confirm_password.$errors[0]?.$message }}</small>
        </div>

        <Message v-if="error" severity="error" :closable="true" @close="error = ''">
          {{ error }}
        </Message>

        <Button
          type="submit"
          label="Change Password"
          icon="pi pi-lock"
          :loading="isLoading"
        />
      </form>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useVuelidate } from '@vuelidate/core';
import { required, minLength, sameAs } from '@vuelidate/validators';
import type { ChangePasswordResponse } from '~/types/settings';

definePageMeta({
  layout: 'settings',
  middleware: 'auth',
});

const authStore = useAuthStore();
const api = useApi();
const toast = useAppToast();

const isLoading = ref(false);
const error = ref('');

const form = reactive({
  current_password: '',
  new_password: '',
  confirm_password: '',
});

const rules = {
  current_password: { required },
  new_password: { required, minLength: minLength(8) },
  confirm_password: { required, sameAs: sameAs(computed(() => form.new_password)) },
};

const v$ = useVuelidate(rules, form);

const handleSubmit = async () => {
  const valid = await v$.value.$validate();
  if (!valid) return;

  error.value = '';
  isLoading.value = true;

  try {
    const response = await api<ChangePasswordResponse>('/api/v1/users/me/password', {
      method: 'PUT',
      body: {
        current_password: form.current_password,
        new_password: form.new_password,
      },
    });

    authStore.accessToken = response.access_token;
    toast.success('Password changed successfully. Other sessions may need to re-authenticate.');

    form.current_password = '';
    form.new_password = '';
    form.confirm_password = '';
    v$.value.$reset();
  } catch (e: any) {
    error.value = e.data?.message || 'Failed to change password';
  } finally {
    isLoading.value = false;
  }
};
</script>
