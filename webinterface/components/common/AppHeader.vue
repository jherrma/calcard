<template>
  <header class="bg-surface-0 dark:bg-surface-900 shadow-sm border-b border-surface-200 dark:border-surface-800 sticky top-0 z-20 transition-colors duration-300">
    <div class="flex items-center justify-between h-16 px-4">
      <!-- Mobile menu button -->
      <button
        class="lg:hidden p-2 rounded-md text-surface-500 hover:bg-surface-100 dark:hover:bg-surface-800 focus:outline-none focus:ring-2 focus:ring-primary-500"
        @click="$emit('toggle-sidebar')"
        aria-label="Toggle sidebar"
      >
        <i class="pi pi-bars text-xl" />
      </button>

      <!-- Page title -->
      <h1 class="text-lg font-semibold text-surface-900 dark:text-surface-0 hidden sm:block ml-2 lg:ml-0">
        {{ pageTitle }}
      </h1>

      <!-- Spacer -->
      <div class="flex-1" />

      <!-- User menu -->
      <div class="relative" ref="menuRef">
        <button
          class="flex items-center gap-2 p-1.5 rounded-lg hover:bg-surface-100 dark:hover:bg-surface-800 transition-colors focus:outline-none focus:ring-2 focus:ring-primary-500"
          @click="userMenuOpen = !userMenuOpen"
          aria-haspopup="true"
          :aria-expanded="userMenuOpen"
        >
          <Avatar
            :label="userInitials"
            shape="circle"
            class="bg-primary-500 text-white"
          />
          <span class="hidden sm:block text-sm font-medium text-surface-700 dark:text-surface-200 max-w-[150px] truncate">
            {{ authStore.user?.display_name || authStore.user?.username }}
          </span>
          <i class="pi pi-chevron-down text-xs text-surface-500" />
        </button>

        <!-- Dropdown menu -->
        <Transition
          enter-active-class="transition ease-out duration-100"
          enter-from-class="transform opacity-0 scale-95"
          enter-to-class="transform opacity-100 scale-100"
          leave-active-class="transition ease-in duration-75"
          leave-from-class="transform opacity-100 scale-100"
          leave-to-class="transform opacity-0 scale-95"
        >
          <div
            v-if="userMenuOpen"
            class="absolute right-0 mt-2 w-56 bg-white dark:bg-surface-900 rounded-lg shadow-lg border border-surface-200 dark:border-surface-700 py-1 z-30"
          >
            <div class="px-4 py-3 border-b border-surface-200 dark:border-surface-700 sm:hidden">
              <p class="text-sm font-medium text-surface-900 dark:text-surface-0 truncate">
                {{ authStore.user?.display_name || authStore.user?.username }}
              </p>
              <p class="text-xs text-surface-500 truncate">
                {{ authStore.user?.email }}
              </p>
            </div>
            
            <NuxtLink
              to="/settings/profile"
              class="block px-4 py-2.5 text-sm text-surface-700 dark:text-surface-200 hover:bg-surface-100 dark:hover:bg-surface-800"
              @click="userMenuOpen = false"
            >
              <i class="pi pi-user mr-2 text-surface-500" />
              Profile
            </NuxtLink>
            <NuxtLink
              to="/settings/app-passwords"
              class="block px-4 py-2.5 text-sm text-surface-700 dark:text-surface-200 hover:bg-surface-100 dark:hover:bg-surface-800"
              @click="userMenuOpen = false"
            >
              <i class="pi pi-key mr-2 text-surface-500" />
              App Passwords
            </NuxtLink>
            <NuxtLink
              to="/setup"
              class="block px-4 py-2.5 text-sm text-surface-700 dark:text-surface-200 hover:bg-surface-100 dark:hover:bg-surface-800"
              @click="userMenuOpen = false"
            >
              <i class="pi pi-question-circle mr-2 text-surface-500" />
              Help & Setup
            </NuxtLink>
            <div class="border-t border-surface-200 dark:border-surface-700 my-1"></div>
            <button
              class="w-full text-left px-4 py-2.5 text-sm text-red-600 dark:text-red-400 hover:bg-surface-50 dark:hover:bg-surface-800"
              @click="handleLogout"
            >
              <i class="pi pi-sign-out mr-2" />
              Logout
            </button>
          </div>
        </Transition>
      </div>
    </div>
  </header>
</template>

<script setup lang="ts">
import { useAuthStore } from '~/stores/auth';

defineEmits<{
  'toggle-sidebar': [];
}>();

const authStore = useAuthStore();
const route = useRoute();
const userMenuOpen = ref(false);
const menuRef = ref<HTMLElement | null>(null);

const pageTitle = computed(() => {
  const titles: Record<string, string> = {
    '/calendar': 'Calendar',
    '/contacts': 'Contacts',
    '/settings': 'Settings',
    '/settings/profile': 'Profile Settings',
    '/settings/password': 'Change Password',
    '/settings/app-passwords': 'App Passwords',
    '/settings/caldav-credentials': 'CalDAV Credentials',
    '/settings/carddav-credentials': 'CardDAV Credentials',
    '/settings/connections': 'Connected Accounts',
    '/settings/admin': 'Admin Console',
    '/settings/danger': 'Danger Zone',
  };
  // Check exact match first, then startsWith for nested routes
  if (titles[route.path]) return titles[route.path];
  
  for (const [path, title] of Object.entries(titles)) {
    if (route.path.startsWith(path) && path !== '/') {
      return title;
    }
  }
  
  return 'CalCard';
});

const userInitials = computed(() => {
  const name = authStore.user?.display_name || authStore.user?.username || 'U';
  return name.charAt(0).toUpperCase();
});

const handleLogout = async () => {
  userMenuOpen.value = false;
  await authStore.logout();
};

// Close menu when clicking outside
const handleClickOutside = (event: MouseEvent) => {
  if (menuRef.value && !menuRef.value.contains(event.target as Node)) {
    userMenuOpen.value = false;
  }
};

onMounted(() => {
  document.addEventListener('click', handleClickOutside);
});

onUnmounted(() => {
  document.removeEventListener('click', handleClickOutside);
});
</script>
