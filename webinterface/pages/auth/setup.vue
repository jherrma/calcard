<template>
  <div>
    <h2 class="text-2xl font-bold text-surface-900 dark:text-surface-0 mb-2 text-center">
      Initial Setup
    </h2>
    <p class="text-surface-600 dark:text-surface-400 mb-6 text-center text-sm">
      Create the first administrator account to get started with CalCard.
    </p>

    <form @submit.prevent="handleSetup" class="space-y-4">
      <div v-if="tokenRequired" class="flex flex-col gap-1.5">
        <label for="setup_token" class="text-sm font-medium text-surface-700 dark:text-surface-300">Setup Token</label>
        <InputText
          id="setup_token"
          v-model="form.setup_token"
          required
          autocomplete="off"
          placeholder="The value of CALDAV_SETUP_TOKEN"
          class="w-full"
          :class="{ 'p-invalid': v$.setup_token.$error }"
        />
        <small v-if="v$.setup_token.$error" class="p-error">{{ v$.setup_token.$errors[0]?.$message }}</small>
        <small class="text-surface-500 dark:text-surface-400">
          This server requires its setup token before the first account can be created.
        </small>
      </div>

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
        <small v-if="v$.username.$error" class="p-error">{{ v$.username.$errors[0]?.$message }}</small>
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
        <small v-if="v$.email.$error" class="p-error">{{ v$.email.$errors[0]?.$message }}</small>
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
import { required, requiredIf, email, minLength, sameAs, helpers } from '@vuelidate/validators';
import type { SystemSettings } from '~/types/auth';

definePageMeta({
  layout: "auth",
  middleware: "guest",
});

const authStore = useAuthStore();
const router = useRouter();

const form = reactive({
  setup_token: "",
  username: "",
  email: "",
  display_name: "",
  password: "",
  confirmPassword: "",
});

// Whether the server wants CALDAV_SETUP_TOKEN (known after the settings fetch).
const tokenRequired = ref(false);

const rules = {
  setup_token: {
    required: requiredIf(tokenRequired),
  },
  username: {
    required,
    minLength: minLength(3),
    // Same shape as the server rule; the server (and handleSetup) lower-case it.
    pattern: helpers.withMessage(
      "Use letters, digits, '.', '_' or '-' (start with a letter or digit); it is stored in lower case",
      helpers.regex(/^[a-zA-Z0-9][a-zA-Z0-9._-]{2,63}$/),
    ),
  },
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

// Setup is one-shot: once an account exists the server refuses it (409), so
// send anyone who lands here later to the login page instead of a dead form.
onMounted(async () => {
  try {
    const settings = await useApi()<SystemSettings>("/api/v1/system/settings");
    if (settings.admin_configured) await router.replace("/auth/login");
    tokenRequired.value = !!settings.setup_token_required;
  } catch {
    // Settings unreachable: leave the form; submitting will surface the error.
  }
});

const handleSetup = async () => {
  const isFormCorrect = await v$.value.$validate();
  if (!isFormCorrect) return;

  error.value = "";
  isLoading.value = true;

  try {
    await authStore.setupAdmin({
      // The server lower-cases usernames; do it here so "Admin" isn't rejected.
      username: form.username.trim().toLowerCase(),
      email: form.email,
      display_name: form.display_name,
      password: form.password,
      ...(tokenRequired.value ? { setup_token: form.setup_token } : {}),
    });
  } catch (e: any) {
    error.value = e.data?.message || "Setup failed. Please try again.";
    // A 403 means the server wants a setup token. If the settings fetch failed
    // earlier the field was never shown, so reveal it now instead of dead-ending.
    if ((e.statusCode ?? e.status) === 403) tokenRequired.value = true;
    isLoading.value = false;
    return;
  }

  // The account exists now. A failed sign-in must not read as "setup failed"
  // (retrying would only answer 409), so send the admin to the login page.
  try {
    await authStore.login({ email: form.email, password: form.password, remember: false });
    await router.push("/");
  } catch {
    await router.replace("/auth/login");
  } finally {
    isLoading.value = false;
  }
};
</script>
