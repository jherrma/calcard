<template>
  <div>
    <h2 class="text-2xl font-bold text-surface-900 dark:text-surface-0 mb-6">Admin Console</h2>

    <div class="bg-surface-0 dark:bg-surface-900 rounded-xl border border-surface-200 dark:border-surface-800 p-8 text-center">
      <i class="pi pi-shield text-5xl text-surface-300 dark:text-surface-600 mb-4" />
      <h3 class="text-lg font-semibold text-surface-900 dark:text-surface-0 mb-2">Coming Soon</h3>
      <p class="text-surface-500 mb-6">
        The admin console is under development. The following features are planned:
      </p>
      <ul class="text-left max-w-md mx-auto space-y-2 text-sm text-surface-600 dark:text-surface-400">
        <li class="flex items-center gap-2">
          <i class="pi pi-users text-primary-500" />
          User management (list, activate, deactivate, delete)
        </li>
        <li class="flex items-center gap-2">
          <i class="pi pi-cog text-primary-500" />
          Server configuration
        </li>
        <li class="flex items-center gap-2">
          <i class="pi pi-chart-bar text-primary-500" />
          Usage statistics and monitoring
        </li>
        <li class="flex items-center gap-2">
          <i class="pi pi-envelope text-primary-500" />
          Email/SMTP settings
        </li>
        <li class="flex items-center gap-2">
          <i class="pi pi-lock text-primary-500" />
          OAuth/SAML provider management
        </li>
      </ul>
    </div>
  </div>
</template>

<script setup lang="ts">
definePageMeta({
  layout: 'settings',
  middleware: 'auth',
});

const authStore = useAuthStore();

// Guard: redirect non-admins
onMounted(() => {
  if (!authStore.isAdmin) {
    navigateTo('/settings/profile', { replace: true });
  }
});
</script>
