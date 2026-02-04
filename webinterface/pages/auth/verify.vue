<template>
  <div class="text-center py-8">
    <div v-if="loading" class="flex flex-col items-center">
      <ProgressSpinner style="width: 50px; height: 50px" strokeWidth="4" />
      <p class="mt-4 text-surface-600 dark:text-surface-400">Verifying your email...</p>
    </div>

    <div v-else-if="success">
      <div class="mb-4 flex justify-center">
        <div class="bg-green-100 dark:bg-green-900/30 p-3 rounded-full">
          <i class="pi pi-check text-green-600 dark:text-green-400 text-3xl"></i>
        </div>
      </div>
      <h2 class="text-2xl font-bold text-surface-900 dark:text-surface-0 mb-2">Email Verified</h2>
      <p class="text-surface-600 dark:text-surface-400 mb-6">
        Thank you for verifying your email address. You can now log in to your account.
      </p>
      <Button label="Continue to Login" @click="navigateTo('/auth/login')" class="w-full" />
    </div>

    <div v-else>
      <div class="mb-4 flex justify-center">
        <div class="bg-red-100 dark:bg-red-900/30 p-3 rounded-full">
          <i class="pi pi-times text-red-600 dark:text-red-400 text-3xl"></i>
        </div>
      </div>
      <h2 class="text-2xl font-bold text-surface-900 dark:text-surface-0 mb-2">Verification Failed</h2>
      <p class="text-surface-600 dark:text-surface-400 mb-6">
        {{ errorMessage || 'The verification link is invalid or has expired.' }}
      </p>
      <div class="space-y-3">
        <Button label="Back to Login" text @click="navigateTo('/auth/login')" class="w-full" />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
definePageMeta({
  layout: "auth",
  middleware: "guest",
});

const route = useRoute();
const api = useApi();

const loading = ref(true);
const success = ref(false);
const errorMessage = ref("");

onMounted(async () => {
  const token = route.query.token;
  
  if (!token) {
    loading.value = false;
    errorMessage.value = "Verification token is missing.";
    return;
  }

  try {
    await api("/api/v1/auth/verify", {
      method: "POST",
      body: { token },
    });
    success.value = true;
  } catch (e: any) {
    errorMessage.value = e.data?.message || "Verification failed.";
  } finally {
    loading.value = false;
  }
});
</script>
