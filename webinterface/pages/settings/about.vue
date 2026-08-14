<template>
  <div>
    <h2 class="text-2xl font-bold text-surface-900 dark:text-surface-0 mb-2">Open Source Attribution</h2>
    <p class="text-sm text-surface-500 dark:text-surface-400 mb-6">
      CalCard is built on the work of the open-source projects listed below. Select a package to
      open its repository.
    </p>

    <!-- Filter (story 101: the list must be filterable by library name). Plain
         input with an absolutely positioned icon, matching the contacts toolbar —
         PrimeVue's IconField is not in nuxt.config's component include list. -->
    <div class="mb-6 relative max-w-md">
      <label for="os-filter" class="sr-only">Filter packages by name</label>
      <i class="pi pi-search absolute left-3 top-1/2 -translate-y-1/2 text-surface-500 dark:text-surface-400 text-sm" />
      <input
        id="os-filter"
        v-model="filter"
        type="text"
        autocomplete="off"
        placeholder="Filter by package name..."
        class="w-full pl-9 pr-3 py-2 text-sm rounded-lg border border-surface-300 dark:border-surface-600 bg-surface-0 dark:bg-surface-800 text-surface-900 dark:text-surface-100 placeholder-surface-400 focus:outline-none focus:ring-2 focus:ring-primary-500 focus:border-transparent"
      >
    </div>

    <div class="space-y-10">
      <AboutOpenSourceList
        title="Backend (Go modules)"
        icon="pi pi-server"
        :packages="backendPackages"
        :filter="filter"
        :loading="loading"
        :error="backendError"
      />

      <AboutOpenSourceList
        title="Frontend (npm packages)"
        icon="pi pi-desktop"
        :packages="frontendPackages"
        :filter="filter"
        :loading="loading"
        :error="frontendError"
      />
    </div>

    <!-- Provenance + the best-effort caveat, so "unknown" is never read as
         "unlicensed". -->
    <div class="mt-10 pt-6 border-t border-surface-200 dark:border-surface-800 space-y-1">
      <p v-if="backendNote" class="text-xs text-surface-500 dark:text-surface-400">
        <span class="font-medium">Backend:</span> {{ backendNote }}
      </p>
      <p v-if="frontendNote" class="text-xs text-surface-500 dark:text-surface-400">
        <span class="font-medium">Frontend:</span> {{ frontendNote }}
      </p>
    </div>
  </div>
</template>

<script setup lang="ts">
definePageMeta({
  layout: 'settings',
  middleware: 'auth',
});

useHead({ title: 'Open Source Attribution — CalCard' });

const filter = ref('');

const {
  backendPackages,
  frontendPackages,
  backendNote,
  frontendNote,
  backendError,
  frontendError,
  loading,
  load,
} = useOpenSourceAttribution();

onMounted(load);
</script>
