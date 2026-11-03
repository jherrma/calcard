<template>
  <div>
    <h2 class="text-2xl font-bold text-surface-900 dark:text-surface-0 mb-2">About CalCard</h2>
    <p class="text-sm text-surface-500 dark:text-surface-400 mb-6">
      Information about the server you are connected to.
    </p>

    <div class="rounded-lg border border-surface-200 dark:border-surface-700 bg-surface-0 dark:bg-surface-900 p-4 max-w-md">
      <div v-if="loading" class="flex items-center gap-2 text-sm text-surface-500 dark:text-surface-400" data-testid="version-loading">
        <i class="pi pi-spin pi-spinner" />
        <span>Loading version...</span>
      </div>
      <p v-else class="text-lg font-medium text-surface-900 dark:text-surface-0" data-testid="version">
        Version {{ displayVersion }}
      </p>
    </div>

    <p class="mt-6 text-sm text-surface-500 dark:text-surface-400">
      CalCard is built on open-source software.
      <NuxtLink to="/settings/about" class="text-primary-600 dark:text-primary-400 hover:underline">
        View the open-source attribution
      </NuxtLink>
    </p>
  </div>
</template>

<script setup lang="ts">
import type { SystemSettings } from '~/types/auth';

definePageMeta({
  layout: 'settings',
  middleware: 'auth',
});

useHead({ title: 'About CalCard — CalCard' });

const api = useApi();

const loading = ref(true);
const version = ref('');

// The version comes from the public settings endpoint. Nothing on the settings
// pages loads it already, so it is fetched here; a failure just shows "unknown".
const displayVersion = computed(() => version.value.trim() || 'unknown');

onMounted(async () => {
  try {
    const settings = await api<SystemSettings>('/api/v1/system/settings');
    version.value = settings.version ?? '';
  } catch {
    version.value = '';
  } finally {
    loading.value = false;
  }
});
</script>
