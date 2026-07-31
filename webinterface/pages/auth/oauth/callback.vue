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
definePageMeta({
  layout: "auth",
});

const authStore = useAuthStore();
const error = ref("");

onMounted(async () => {
  // The backend redirects here with tokens in the URL *fragment* (after '#'),
  // not the query string, so they never reach the server/access logs (H16).
  const hash = window.location.hash.startsWith("#")
    ? window.location.hash.slice(1)
    : window.location.hash;
  const params = new URLSearchParams(hash);

  const oauthError = params.get("error");
  if (oauthError) {
    error.value = oauthError;
    return;
  }

  const accessToken = params.get("access_token");
  const refreshToken = params.get("refresh_token");
  const expiresAt = params.get("expires_at");

  if (accessToken && refreshToken && expiresAt) {
    try {
      authStore.setAuth({
        access_token: accessToken,
        refresh_token: refreshToken,
        expires_at: Number(expiresAt),
        token_type: "Bearer",
        user: {} as any, // fetched immediately below
      });

      // Strip the tokens out of the URL so they aren't left in history.
      history.replaceState(null, "", window.location.pathname);

      await authStore.fetchUser();
      // fetchUser() swallows errors and clears auth on failure — check the
      // store instead of relying on an exception.
      if (!authStore.isAuthenticated) {
        error.value = "Signed in, but loading your profile failed. Please try again.";
        return;
      }
      // "/" is the dashboard, the app's landing page since story 042.
      navigateTo("/", { replace: true });
    } catch (e: any) {
      error.value = "Failed to complete authentication. " + (e.message || "");
    }
  } else {
    error.value = "Invalid authentication response. Missing tokens.";
  }
});
</script>
