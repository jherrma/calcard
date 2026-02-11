<template>
  <div>
    <h2 class="text-2xl font-bold text-surface-900 dark:text-surface-0 mb-6 text-center">
      Sign in to your account
    </h2>

    <form @submit.prevent="handleLogin" class="space-y-5">
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
        <small v-if="v$.email.$error" class="p-error">{{ v$.email.$errors[0]?.$message }}</small>
      </div>

      <div class="flex flex-col gap-2">
        <div class="flex justify-between items-center">
          <label for="password" class="text-sm font-medium text-surface-700 dark:text-surface-300">Password</label>
          <NuxtLink
            v-if="systemSettings.smtp_enabled"
            to="/auth/forgot-password"
            class="text-xs text-primary-600 hover:text-primary-500 font-medium"
          >
            Forgot password?
          </NuxtLink>
        </div>
        <Password
          id="password"
          v-model="form.password"
          required
          :feedback="false"
          toggle-mask
          placeholder="••••••••"
          class="w-full"
          input-class="w-full"
          :class="{ 'p-invalid': v$.password.$error }"
        />
        <small v-if="v$.password.$error" class="p-error">{{ v$.password.$errors[0]?.$message }}</small>
      </div>

      <div class="flex items-center">
        <Checkbox v-model="form.remember" id="remember" :binary="true" />
        <label for="remember" class="ml-2 block text-sm text-surface-600 dark:text-surface-400">
          Remember me
        </label>
      </div>

      <Button
        type="submit"
        label="Sign in"
        :loading="isLoading"
        class="w-full"
        icon="pi pi-sign-in"
      />

      <Message v-if="error" severity="error" :closable="true" @close="error = ''">
        {{ error }}
      </Message>
    </form>

    <!-- External Auth Providers -->
    <div v-if="externalMethods.length > 0" class="mt-8">
      <div class="relative">
        <div class="absolute inset-0 flex items-center">
          <div class="w-full border-t border-surface-200 dark:border-surface-800" />
        </div>
        <div class="relative flex justify-center text-sm">
          <span class="px-2 bg-surface-0 dark:bg-surface-900 text-surface-500">Or continue with</span>
        </div>
      </div>

      <div class="mt-6 flex flex-col gap-3">
        <Button
          v-for="method in externalMethods"
          :key="method.id"
          :label="method.name"
          :icon="getProviderIcon(method)"
          severity="secondary"
          outlined
          class="w-full"
          @click="loginWithProvider(method)"
        />
      </div>
    </div>

    <p v-if="systemSettings.registration_enabled" class="mt-8 text-center text-sm text-surface-600 dark:text-surface-400">
      Don't have an account?
      <NuxtLink
        to="/auth/register"
        class="font-medium text-primary-600 hover:text-primary-500"
      >
        Create an account
      </NuxtLink>
    </p>
  </div>
</template>

<script setup lang="ts">
import { useVuelidate } from '@vuelidate/core';
import { required, email } from '@vuelidate/validators';
import type { SystemSettings, AuthMethod, AuthMethodsResponse } from '~/types/auth';

definePageMeta({
  layout: "auth",
  middleware: "guest",
});

const authStore = useAuthStore();
const router = useRouter();
const api = useApi();
const config = useRuntimeConfig();

const form = reactive({
  email: "",
  password: "",
  remember: false,
});

const rules = {
  email: { required, email },
  password: { required },
};

const v$ = useVuelidate(rules, form);

const isLoading = ref(false);
const error = ref("");
const systemSettings = ref<SystemSettings>({
  admin_configured: true,
  smtp_enabled: true,
  registration_enabled: true
});

const authMethods = ref<AuthMethod[]>([]);

const externalMethods = computed(() => {
  return authMethods.value.filter(m => m.type !== 'local');
});

// Helper to determine icon based on provider name or type
const getProviderIcon = (method: AuthMethod) => {
  if (method.icon) return method.icon;
  
  const lowerName = method.name.toLowerCase();
  if (lowerName.includes('google')) return 'pi pi-google';
  if (lowerName.includes('microsoft') || lowerName.includes('azure')) return 'pi pi-microsoft';
  if (lowerName.includes('github')) return 'pi pi-github';
  if (method.type === 'oidc' || method.type === 'oauth2') return 'pi pi-lock';
  
  return 'pi pi-key'; // Default
};

onMounted(async () => {
  try {
    // Fetch system settings
    const settings = await api<SystemSettings>("/api/v1/system/settings");
    systemSettings.value = settings;
    if (!settings.admin_configured) {
      router.push("/auth/setup");
    }

    // Fetch auth methods
    try {
        const response = await api<AuthMethodsResponse>("/api/v1/auth/methods");
        authMethods.value = response.methods;
    } catch (e) {
        // Fallback: if endpoint fails, assume just local auth
        console.warn("Failed to fetch auth methods, defaulting to local only.");
    }

  } catch (e) {
    console.error("Failed to fetch system settings", e);
  }
});

const handleLogin = async () => {
  const isFormCorrect = await v$.value.$validate();
  if (!isFormCorrect) return;

  error.value = "";
  isLoading.value = true;

  try {
    await authStore.login({
      email: form.email,
      password: form.password,
    });
    router.push("/calendar");
  } catch (e: any) {
    error.value = e.data?.message || "Invalid email or password";
  } finally {
    isLoading.value = false;
  }
};

const loginWithProvider = (method: AuthMethod) => {
  if (method.url) {
    window.location.href = method.url;
  } else {
    // Fallback construction if backend didn't provide full URL
    // This assumes a standard pattern logic
    window.location.href = `${config.public.apiBaseUrl}/api/v1/auth/external/${method.id}`;
  }
};
</script>
