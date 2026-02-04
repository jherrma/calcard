<template>
  <div>
    <h2 class="text-2xl font-bold text-surface-900 dark:text-surface-0 mb-2 text-center">
      Initial Setup
    </h2>
    <p class="text-surface-600 dark:text-surface-400 mb-6 text-center text-sm">
      Create the first administrator account to get started with CalCard.
    </p>

    <form @submit.prevent="handleSetup" class="space-y-4">
      <div class="flex flex-col gap-1.5">
        <label for="username" class="text-sm font-medium text-surface-700 dark:text-surface-300">Admin Username</label>
        <InputText
          id="username"
          v-model="form.username"
          required
          placeholder="admin"
          class="w-full"
          :class="{ 'p-invalid': v$.username.$error }"
        />
        <small v-if="v$.username.$error" class="p-error">{{ v$.username.$errors[0].$message }}</small>
      </div>

      <div class="flex flex-col gap-1.5">
        <label for="email" class="text-sm font-medium text-surface-700 dark:text-surface-300">Admin Email</label>
        <InputText
          id="email"
          v-model="form.email"
          type="email"
          required
          placeholder="admin@example.com"
          class="w-full"
          :class="{ 'p-invalid': v$.email.$error }"
        />
        <small v-if="v$.email.$error" class="p-error">{{ v$.email.$errors[0].$message }}</small>
      </div>

      <div class="flex flex-col gap-1.5">
        <label for="display_name" class="text-sm font-medium text-surface-700 dark:text-surface-300">Display Name</label>
        <InputText
          id="display_name"
          v-model="form.display_name"
          placeholder="System Administrator"
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
        <small v-if="v$.password.$error" class="p-error">{{ v$.password.$errors[0].$message }}</small>
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
        <small v-if="v$.confirmPassword.$error" class="p-error">{{ v$.confirmPassword.$errors[0].$message }}</small>
      </div>

      <div class="pt-2">
        <Button
          type="submit"
          label="Finish Setup"
          :loading="isLoading"
          class="w-full"
          severity="primary"
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
import { required, email, minLength, sameAs } from '@vuelidate/validators';

definePageMeta({
  layout: "auth",
});

const authStore = useAuthStore();
const router = useRouter();

const form = reactive({
  username: "",
  email: "",
  display_name: "",
  password: "",
  confirmPassword: "",
});

const rules = {
  username: { required, minLength: minLength(3) },
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

const handleSetup = async () => {
  const isFormCorrect = await v$.value.$validate();
  if (!isFormCorrect) return;

  error.value = "";
  isLoading.value = true;

  try {
    await authStore.setupAdmin({
      username: form.username,
      email: form.email,
      display_name: form.display_name,
      password: form.password,
    });
    // Redirect to login after setup
    router.push("/auth/login");
  } catch (e: any) {
    error.value = e.data?.message || "Setup failed. Please try again.";
  } finally {
    isLoading.value = false;
  }
};
</script>
