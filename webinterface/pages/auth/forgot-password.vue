<template>
  <div>
    <h2 class="text-2xl font-bold text-surface-900 dark:text-surface-0 mb-2 text-center">
      Reset Password
    </h2>
    <p class="text-surface-600 dark:text-surface-400 mb-6 text-center text-sm">
      Enter your email address and we'll send you a link to reset your password.
    </p>

    <div v-if="submitted" class="text-center py-4">
      <Message severity="success" :closable="false">
        If an account exists with that email, we have sent password reset instructions.
      </Message>
      <div class="mt-6">
        <Button label="Back to Login" text @click="navigateTo('/auth/login')" class="w-full" />
      </div>
    </div>

    <form v-else @submit.prevent="handleSubmit" class="space-y-5">
      <div class="flex flex-col gap-2">
        <label for="email" class="text-sm font-medium text-surface-700 dark:text-surface-300">Email Address</label>
        <InputText
          id="email"
          v-model="form.email"
          type="email"
          required
          placeholder="you@example.com"
          class="w-full"
          :class="{ 'p-invalid': v$.email.$error }"
        />
        <small v-if="v$.email.$error" class="p-error">{{ v$.email.$errors[0].$message }}</small>
      </div>

      <Button
        type="submit"
        label="Send Reset Link"
        :loading="isLoading"
        class="w-full"
      />

      <div class="text-center">
        <NuxtLink
          to="/auth/login"
          class="text-sm font-medium text-primary-600 hover:text-primary-500"
        >
          Back to Login
        </NuxtLink>
      </div>
    </form>
  </div>
</template>

<script setup lang="ts">
import { useVuelidate } from '@vuelidate/core';
import { required, email } from '@vuelidate/validators';

definePageMeta({
  layout: "auth",
  middleware: "guest",
});

const api = useApi();

const form = reactive({
  email: "",
});

const rules = {
  email: { required, email },
};

const v$ = useVuelidate(rules, form);

const isLoading = ref(false);
const submitted = ref(false);

const handleSubmit = async () => {
  const isFormCorrect = await v$.value.$validate();
  if (!isFormCorrect) return;

  isLoading.value = true;

  try {
    await api("/api/v1/auth/forgot-password", {
      method: "POST",
      body: { email: form.email },
    });
  } catch (e) {
    // We ignore errors here to prevent email enumeration
    console.error(e);
  } finally {
    isLoading.value = false;
    submitted.value = true;
  }
};
</script>
