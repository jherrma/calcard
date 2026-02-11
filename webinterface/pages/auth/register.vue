<template>
  <div>
    <h2 class="text-2xl font-bold text-surface-900 dark:text-surface-0 mb-6 text-center">
      Create your account
    </h2>

    <div v-if="isRegistered" class="text-center py-8">
      <div class="mb-4 flex justify-center">
        <div class="bg-green-100 dark:bg-green-900/30 p-3 rounded-full">
          <i class="pi pi-check text-green-600 dark:text-green-400 text-3xl"></i>
        </div>
      </div>
      <h3 class="text-xl font-semibold mb-2">Registration Successful!</h3>
      <p v-if="needsEmailVerification" class="text-surface-600 dark:text-surface-400 mb-6">
        Please check your email to verify your account before logging in.
      </p>
      <p v-else class="text-surface-600 dark:text-surface-400 mb-6">
        Your account is ready. You can now sign in.
      </p>
      <Button label="Back to Login" @click="navigateTo('/auth/login')" class="w-full" />
    </div>

    <form v-else @submit.prevent="handleRegister" class="space-y-4">

      <div class="flex flex-col gap-1.5">
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
        <small v-if="v$.email.$error" class="p-error">{{ v$.email.$errors[0]?.$message }}</small>
      </div>

      <div class="flex flex-col gap-1.5">
        <label for="display_name" class="text-sm font-medium text-surface-700 dark:text-surface-300">Display Name (Optional)</label>
        <InputText
          id="display_name"
          v-model="form.display_name"
          placeholder="John Doe"
          class="w-full"
        />
      </div>

      <div class="flex flex-col gap-1.5">
        <label for="password" class="text-sm font-medium text-surface-700 dark:text-surface-300">Password</label>
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
        <small v-if="v$.password.$error" class="p-error">{{ v$.password.$errors[0]?.$message }}</small>
      </div>

      <div class="flex flex-col gap-1.5">
        <label for="confirmPassword" class="text-sm font-medium text-surface-700 dark:text-surface-300">Confirm Password</label>
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
        <small v-if="v$.confirmPassword.$error" class="p-error">{{ v$.confirmPassword.$errors[0]?.$message }}</small>
      </div>

      <div class="pt-2">
        <Button
          type="submit"
          label="Create Account"
          :loading="isLoading"
          class="w-full"
        />
      </div>

      <Message v-if="error" severity="error" :closable="true" @close="error = ''">
        {{ error }}
      </Message>
    </form>

    <p v-if="!isRegistered" class="mt-8 text-center text-sm text-surface-600 dark:text-surface-400">
      Already have an account?
      <NuxtLink
        to="/auth/login"
        class="font-medium text-primary-600 hover:text-primary-500"
      >
        Sign in
      </NuxtLink>
    </p>
  </div>
</template>

<script setup lang="ts">
import { useVuelidate } from '@vuelidate/core';
import { required, email, minLength, sameAs, helpers } from '@vuelidate/validators';

definePageMeta({
  layout: "auth",
  middleware: "guest",
});

const authStore = useAuthStore();

const form = reactive({
  email: "",
  display_name: "",
  password: "",
  confirmPassword: "",
});

const rules = {
  email: { required, email },
  password: { required, minLength: minLength(8) },
  confirmPassword: { 
    required, 
    sameAsPassword: sameAs(computed(() => form.password)) 
  },
};

const v$ = useVuelidate(rules, form);

const isLoading = ref(false);
const error = ref("");
const isRegistered = ref(false);
const needsEmailVerification = ref(false);

const handleRegister = async () => {
  const isFormCorrect = await v$.value.$validate();
  if (!isFormCorrect) return;

  error.value = "";
  isLoading.value = true;

  try {
    const result = await authStore.register({
      email: form.email,
      display_name: form.display_name,
      password: form.password,
    });
    isRegistered.value = true;
    needsEmailVerification.value = !(result?.is_active && result?.email_verified);
  } catch (e: any) {
    error.value = e.data?.message || "Registration failed. Please try again.";
  } finally {
    isLoading.value = false;
  }
};
</script>
