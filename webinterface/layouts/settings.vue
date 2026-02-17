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

    <!-- App Sidebar -->
    <AppSidebar
      :open="sidebarOpen"
      @close="sidebarOpen = false"
    />

    <!-- Main content -->
    <div class="lg:pl-64 flex flex-col min-h-screen transition-all duration-300">
      <!-- Header -->
      <AppHeader @toggle-sidebar="sidebarOpen = !sidebarOpen" />

      <!-- Settings area -->
      <div class="flex-1 flex flex-col">
        <!-- Mobile settings nav (horizontal scroll) -->
        <nav class="flex overflow-x-auto border-b border-surface-200 dark:border-surface-800 bg-surface-0 dark:bg-surface-900 px-4 gap-1 lg:hidden">
          <NuxtLink
            v-for="item in visibleNavItems"
            :key="item.to"
            :to="item.to"
            custom
            v-slot="{ href, navigate }"
          >
            <a
              :href="href"
              @click="navigate"
              :class="[
                'flex items-center gap-2 px-3 py-3 text-sm font-medium whitespace-nowrap border-b-2 transition-colors',
                route.path === item.to
                  ? 'border-primary-500 text-primary-600 dark:text-primary-400'
                  : 'border-transparent text-surface-600 dark:text-surface-400 hover:text-surface-900 dark:hover:text-surface-100',
                item.danger ? 'text-red-600 dark:text-red-400' : ''
              ]"
            >
              <i :class="[item.icon, 'text-sm']" />
              {{ item.label }}
            </a>
          </NuxtLink>
        </nav>

        <div class="flex-1 flex">
          <!-- Desktop settings sidebar -->
          <aside class="hidden lg:flex flex-col w-64 border-r border-surface-200 dark:border-surface-800 bg-surface-0 dark:bg-surface-900 p-4">
            <nav class="space-y-1">
              <NuxtLink
                v-for="item in visibleNavItems"
                :key="item.to"
                :to="item.to"
                custom
                v-slot="{ href, navigate }"
              >
                <a
                  :href="href"
                  @click="navigate"
                  :class="[
                    'flex items-center gap-3 px-3 py-2.5 rounded-full text-sm font-medium transition-all duration-200',
                    route.path === item.to
                      ? 'bg-primary-50 dark:bg-primary-900/20 text-primary-700 dark:text-primary-300'
                      : item.danger
                        ? 'text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/10'
                        : 'text-surface-700 dark:text-surface-300 hover:bg-surface-100 dark:hover:bg-surface-800'
                  ]"
                >
                  <i :class="[item.icon, 'text-lg']" />
                  {{ item.label }}
                </a>
              </NuxtLink>
            </nav>
          </aside>

          <!-- Page content -->
          <main class="flex-1 p-4 lg:p-8">
            <div class="max-w-3xl">
              <slot />
            </div>
          </main>
        </div>
      </div>
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

const authStore = useAuthStore();
const route = useRoute();
const sidebarOpen = ref(false);

interface SettingsNavItem {
  to: string;
  label: string;
  icon: string;
  admin?: boolean;
  danger?: boolean;
}

const navItems: SettingsNavItem[] = [
  { to: '/settings/profile', label: 'Profile', icon: 'pi pi-user' },
  { to: '/settings/password', label: 'Password', icon: 'pi pi-lock' },
  { to: '/settings/app-passwords', label: 'App Passwords', icon: 'pi pi-key' },
  { to: '/settings/caldav-credentials', label: 'CalDAV Credentials', icon: 'pi pi-calendar' },
  { to: '/settings/carddav-credentials', label: 'CardDAV Credentials', icon: 'pi pi-id-card' },
  { to: '/settings/connections', label: 'Connected Accounts', icon: 'pi pi-link' },
  { to: '/settings/admin', label: 'Admin Console', icon: 'pi pi-shield', admin: true },
  { to: '/settings/danger', label: 'Danger Zone', icon: 'pi pi-exclamation-triangle', danger: true },
];

const visibleNavItems = computed(() => {
  return navItems.filter(item => !item.admin || authStore.isAdmin);
});

// Track navigation loading state
const nuxtApp = useNuxtApp();
const isNavigating = ref(false);

nuxtApp.hook('page:start', () => {
  isNavigating.value = true;
});

nuxtApp.hook('page:finish', () => {
  isNavigating.value = false;
  sidebarOpen.value = false;
});

watch(() => route.path, () => {
  if (window.innerWidth < 1024) {
    sidebarOpen.value = false;
  }
});
</script>
