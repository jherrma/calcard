<template>
  <div class="min-h-screen bg-surface-50 dark:bg-surface-950 transition-colors duration-300">
    <!-- Mobile sidebar backdrop -->
    <Transition
      enter-active-class="transition-opacity ease-linear duration-300"
      enter-from-class="opacity-0"
      enter-to-class="opacity-100"
      leave-active-class="transition-opacity ease-linear duration-300"
      leave-from-class="opacity-100"
      leave-to-class="opacity-0"
    >
      <div
        v-if="sidebarOpen"
        class="fixed inset-0 bg-surface-900/50 backdrop-blur-sm z-20 lg:hidden"
        @click="sidebarOpen = false"
      />
    </Transition>

    <!-- Sidebar -->
    <AppSidebar
      :open="sidebarOpen"
      @close="sidebarOpen = false"
    />

    <!-- Main content -->
    <div class="lg:pl-64 flex flex-col min-h-screen transition-all duration-300">
      <!-- Header -->
      <AppHeader @toggle-sidebar="sidebarOpen = !sidebarOpen" />

      <!-- Page content -->
      <main class="flex-1 p-4 lg:p-8">
        <slot />
      </main>
    </div>

    <!-- Toast notifications -->
    <Toast position="top-right" />
    <ConfirmDialog />

    <!-- Global loading overlay -->
    <Transition
      enter-active-class="transition-opacity duration-200"
      enter-from-class="opacity-0"
      enter-to-class="opacity-100"
      leave-active-class="transition-opacity duration-200"
      leave-from-class="opacity-100"
      leave-to-class="opacity-0"
    >
      <div
        v-if="isNavigating"
        class="fixed inset-0 bg-surface-0/50 dark:bg-surface-950/50 z-50 flex items-center justify-center backdrop-blur-sm"
      >
        <ProgressSpinner strokeWidth="4" />
      </div>
    </Transition>
  </div>
</template>

<script setup lang="ts">
import AppSidebar from '~/components/common/AppSidebar.vue';
import AppHeader from '~/components/common/AppHeader.vue';

const sidebarOpen = ref(false);

// Track navigation loading state
const nuxtApp = useNuxtApp();
const isNavigating = ref(false);

nuxtApp.hook('page:start', () => {
  isNavigating.value = true;
});

nuxtApp.hook('page:finish', () => {
  isNavigating.value = false;
  // Also close sidebar on navigation on mobile
  sidebarOpen.value = false;
});

// Close sidebar on route change (mobile)
const route = useRoute();
watch(() => route.path, () => {
  if (window.innerWidth < 1024) {
    sidebarOpen.value = false;
  }
});
</script>
