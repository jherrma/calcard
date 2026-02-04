<template>
  <div>
    <h2 class="text-2xl font-bold text-surface-900 dark:text-surface-0 mb-2 text-center">
      Set New Password
    </h2>
    <p class="text-surface-600 dark:text-surface-400 mb-6 text-center text-sm">
      Please enter your new password below.
    </p>

    <div v-if="success" class="text-center py-4">
      <Message severity="success" :closable="false">
        Your password has been successfully reset.
      </Message>
      <div class="mt-6">
        <Button label="Go to Login" @click="navigateTo('/auth/login')" class="w-full" />
      </div>
    </div>

    <form v-else @submit.prevent="handleSubmit" class="space-y-4">
      <div class="flex flex-col gap-1.5">
        <label for="password" class="text-sm font-medium text-surface-700 dark:text-surface-300">New Password</label>
        <Password
          id="password"
          v-model="form.password"
          required
          toggle-mask
          placeholder="••••••••"
          class="w-full"
          input-class="w-full"
          :class="{ 'p-invalid': v$.password.$error }"
          :feedback="false"
        />
        <AuthPasswordStrength :password="form.password" />
        <small v-if="v$.password.$error" class="p-error">{{ v$.password.$errors[0].$message }}</small>
      </div>

      <div class="flex flex-col gap-1.5">
        <label for="confirmPassword" class="text-sm font-medium text-surface-700 dark:text-surface-300">Confirm New Password</label>
        <Password
          id="confirmPassword"
          v-model="form.confirmPassword"
          required
          :feedback="false"
          toggle-mask
          placeholder="••••••••"
          class="w-full"
          input-class="w-full"
          :class="{ 'p-invalid': v$.confirmPassword.$error }"
        />
        <small v-if="v$.confirmPassword.$error" class="p-error">{{ v$.confirmPassword.$errors[0].$message }}</small>
      </div>

      <div class="pt-2">
        <Button
          type="submit"
          label="Reset Password"
          :loading="isLoading"
          class="w-full"
        />
      </div>

      <Message v-if="error" severity="error" :closable="true" @close="error = ''">
        {{ error }}
      </Message>
    </form>
  </div>
</template>

<script setup lang="ts">
import { useVuelidate } from '@vuelidate/core';
import { required, minLength, sameAs } from '@vuelidate/validators';

definePageMeta({
  layout: "auth",
  middleware: "guest",
});

const route = useRoute();
const api = useApi();

const form = reactive({
  password: "",
  confirmPassword: "",
});

const rules = {
  password: { required, minLength: minLength(8) },
  confirmPassword: { 
    required, 
    sameAsPassword: sameAs(computed(() => form.password)) 
  },
};

const v$ = useVuelidate(rules, form);

const isLoading = ref(false);
const error = ref("");
const success = ref(false);

const handleSubmit = async () => {
  const isFormCorrect = await v$.value.$validate();
  if (!isFormCorrect) return;

  const token = route.query.token;
  if (!token) {
    error.value = "Invalid or missing token.";
    return;
  }

  error.value = "";
  isLoading.value = true;

  try {
    await api("/api/v1/auth/reset-password", {
      method: "POST",
      body: { 
        token,
        password: form.password 
      },
    });
    success.value = true;
  } catch (e: any) {
    error.value = e.data?.message || "Failed to reset password. The link may be expired.";
  } finally {
    isLoading.value = false;
  }
};
</script>
