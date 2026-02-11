<template>
  <div class="text-center py-12">
    <div v-if="error" class="flex flex-col items-center">
      <div class="bg-red-100 dark:bg-red-900/30 p-3 rounded-full mb-4">
        <i class="pi pi-times text-red-600 dark:text-red-400 text-3xl"></i>
      </div>
      <h2 class="text-2xl font-bold text-surface-900 dark:text-surface-0 mb-2">Authentication Failed</h2>
      <p class="text-surface-600 dark:text-surface-400 mb-6">{{ error }}</p>
      <Button label="Back to Login" @click="navigateTo('/auth/login')" class="w-full" />
    </div>
    
    <div v-else class="flex flex-col items-center">
      <ProgressSpinner style="width: 50px; height: 50px" strokeWidth="4" />
      <h2 class="text-2xl font-bold text-surface-900 dark:text-surface-0 mt-6 mb-2">Signing you in...</h2>
      <p class="text-surface-600 dark:text-surface-400">Please wait while we complete the authentication process.</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { LoginResponse } from '~/types/auth';

definePageMeta({
  layout: "auth",
});

const route = useRoute();
const authStore = useAuthStore();
const error = ref("");

onMounted(async () => {
  const { access_token, refresh_token, expires_in, error: oauthError } = route.query;

  if (oauthError) {
    error.value = oauthError as string;
    return;
  }

  if (access_token && refresh_token) {
    try {
      // We manually set the auth state from the redirect parameters
      authStore.setAuth({
        access_token: access_token as string,
        refresh_token: refresh_token as string,
        expires_at: Math.floor(Date.now() / 1000) + (Number(expires_in) || 3600),
        token_type: 'Bearer',
        user: {} as any // Will be fetched immediately
      });

      await authStore.fetchUser();
      navigateTo("/calendar");
    } catch (e: any) {
      error.value = "Failed to complete authentication. " + (e.message || "");
    }
  } else {
    error.value = "Invalid authentication response. Missing tokens.";
  }
});
</script>
