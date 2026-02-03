# Story 033: Layout and Navigation

## Title
Implement Application Layout and Navigation

## Description
As a user, I want a consistent layout with navigation so that I can easily move between different sections of the application.

## Related Acceptance Criteria

| ID | Criterion |
|----|-----------|
| UI-7.1.1 | Web UI is responsive (mobile, tablet, desktop) |
| UI-7.1.3 | Loading states are shown during async operations |
| UI-7.1.4 | Error messages are user-friendly |
| UI-7.1.5 | Success confirmations are shown for actions |

## Acceptance Criteria

### Application Layout

- [ ] Consistent layout across all authenticated pages
- [ ] Header with logo and user menu
- [ ] Sidebar navigation (collapsible on mobile)
- [ ] Main content area
- [ ] Footer (optional)
- [ ] Responsive breakpoints: mobile (<768px), tablet (768-1024px), desktop (>1024px)

### Header

- [ ] Application logo/name
- [ ] Current page title (optional)
- [ ] User avatar and dropdown menu
- [ ] User menu items:
  - [ ] Profile/Settings
  - [ ] App Passwords
  - [ ] Help/Setup Instructions
  - [ ] Logout

### Sidebar Navigation

- [ ] Navigation links:
  - [ ] Calendar (icon + label)
  - [ ] Contacts (icon + label)
  - [ ] Settings (icon + label)
- [ ] Active state indicator
- [ ] Collapsed state on mobile (hamburger menu)
- [ ] Hover states and transitions
- [ ] Keyboard accessible

### Toast Notifications

- [ ] Global toast/notification system
- [ ] Success messages (green)
- [ ] Error messages (red)
- [ ] Warning messages (yellow)
- [ ] Info messages (blue)
- [ ] Auto-dismiss after configurable time
- [ ] Manual dismiss button
- [ ] Stack multiple notifications

### Loading States

- [ ] Global loading indicator for page transitions
- [ ] Component-level loading spinners
- [ ] Skeleton loaders for lists
- [ ] Button loading states (disable + spinner)

### Error Handling

- [ ] Global error boundary
- [ ] 404 page
- [ ] 500 error page
- [ ] Network error handling
- [ ] Session expired handling (redirect to login)

## Technical Notes

### Default Layout
```vue
<!-- layouts/default.vue -->
<template>
  <div class="min-h-screen bg-gray-100">
    <!-- Mobile sidebar backdrop -->
    <div
      v-if="sidebarOpen"
      class="fixed inset-0 bg-gray-600 bg-opacity-75 z-20 lg:hidden"
      @click="sidebarOpen = false"
    />

    <!-- Sidebar -->
    <AppSidebar
      :open="sidebarOpen"
      @close="sidebarOpen = false"
    />

    <!-- Main content -->
    <div class="lg:pl-64 flex flex-col min-h-screen">
      <!-- Header -->
      <AppHeader @toggle-sidebar="sidebarOpen = !sidebarOpen" />

      <!-- Page content -->
      <main class="flex-1 p-4 lg:p-6">
        <slot />
      </main>
    </div>

    <!-- Toast notifications -->
    <Toast position="top-right" />

    <!-- Global loading overlay -->
    <div
      v-if="isNavigating"
      class="fixed inset-0 bg-white bg-opacity-75 z-50 flex items-center justify-center"
    >
      <ProgressSpinner />
    </div>
  </div>
</template>

<script setup lang="ts">
const sidebarOpen = ref(false);

// Track navigation loading state
const nuxtApp = useNuxtApp();
const isNavigating = ref(false);

nuxtApp.hook('page:start', () => {
  isNavigating.value = true;
});

nuxtApp.hook('page:finish', () => {
  isNavigating.value = false;
});

// Close sidebar on route change (mobile)
const route = useRoute();
watch(() => route.path, () => {
  sidebarOpen.value = false;
});
</script>
```

### Sidebar Component
```vue
<!-- components/common/AppSidebar.vue -->
<template>
  <aside
    :class="[
      'fixed inset-y-0 left-0 z-30 w-64 bg-white shadow-lg transform transition-transform duration-300 ease-in-out lg:translate-x-0',
      open ? 'translate-x-0' : '-translate-x-full'
    ]"
  >
    <!-- Logo -->
    <div class="flex items-center justify-between h-16 px-4 border-b">
      <NuxtLink to="/" class="flex items-center gap-2">
        <img src="/logo.svg" alt="Logo" class="h-8 w-8" />
        <span class="text-xl font-bold text-gray-900">CalDAV</span>
      </NuxtLink>
      <button
        class="lg:hidden p-2 rounded-md text-gray-500 hover:bg-gray-100"
        @click="$emit('close')"
      >
        <i class="pi pi-times" />
      </button>
    </div>

    <!-- Navigation -->
    <nav class="flex-1 px-2 py-4 space-y-1">
      <NuxtLink
        v-for="item in navigation"
        :key="item.to"
        :to="item.to"
        :class="[
          'flex items-center gap-3 px-3 py-2 rounded-lg text-sm font-medium transition-colors',
          isActive(item.to)
            ? 'bg-primary-50 text-primary-700'
            : 'text-gray-700 hover:bg-gray-100'
        ]"
      >
        <i :class="[item.icon, 'text-lg']" />
        {{ item.label }}
        <span
          v-if="item.badge"
          class="ml-auto bg-primary-100 text-primary-700 text-xs px-2 py-0.5 rounded-full"
        >
          {{ item.badge }}
        </span>
      </NuxtLink>
    </nav>

    <!-- Bottom section -->
    <div class="border-t p-4">
      <NuxtLink
        to="/settings"
        class="flex items-center gap-3 px-3 py-2 rounded-lg text-sm font-medium text-gray-700 hover:bg-gray-100"
      >
        <i class="pi pi-cog text-lg" />
        Settings
      </NuxtLink>
    </div>
  </aside>
</template>

<script setup lang="ts">
defineProps<{
  open: boolean;
}>();

defineEmits<{
  close: [];
}>();

const route = useRoute();

const navigation = [
  { to: '/calendar', label: 'Calendar', icon: 'pi pi-calendar' },
  { to: '/contacts', label: 'Contacts', icon: 'pi pi-users' },
];

const isActive = (path: string) => {
  return route.path.startsWith(path);
};
</script>
```

### Header Component
```vue
<!-- components/common/AppHeader.vue -->
<template>
  <header class="bg-white shadow-sm border-b sticky top-0 z-10">
    <div class="flex items-center justify-between h-16 px-4">
      <!-- Mobile menu button -->
      <button
        class="lg:hidden p-2 rounded-md text-gray-500 hover:bg-gray-100"
        @click="$emit('toggle-sidebar')"
      >
        <i class="pi pi-bars text-xl" />
      </button>

      <!-- Page title -->
      <h1 class="text-lg font-semibold text-gray-900 hidden sm:block">
        {{ pageTitle }}
      </h1>

      <!-- Spacer -->
      <div class="flex-1" />

      <!-- User menu -->
      <div class="relative">
        <button
          class="flex items-center gap-2 p-2 rounded-lg hover:bg-gray-100"
          @click="userMenuOpen = !userMenuOpen"
        >
          <Avatar
            :label="userInitials"
            shape="circle"
            class="bg-primary-500 text-white"
          />
          <span class="hidden sm:block text-sm font-medium text-gray-700">
            {{ authStore.user?.display_name || authStore.user?.username }}
          </span>
          <i class="pi pi-chevron-down text-sm text-gray-500" />
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
            class="absolute right-0 mt-2 w-48 bg-white rounded-lg shadow-lg border py-1 z-20"
          >
            <NuxtLink
              to="/settings/profile"
              class="block px-4 py-2 text-sm text-gray-700 hover:bg-gray-100"
              @click="userMenuOpen = false"
            >
              <i class="pi pi-user mr-2" />
              Profile
            </NuxtLink>
            <NuxtLink
              to="/settings/app-passwords"
              class="block px-4 py-2 text-sm text-gray-700 hover:bg-gray-100"
              @click="userMenuOpen = false"
            >
              <i class="pi pi-key mr-2" />
              App Passwords
            </NuxtLink>
            <NuxtLink
              to="/setup"
              class="block px-4 py-2 text-sm text-gray-700 hover:bg-gray-100"
              @click="userMenuOpen = false"
            >
              <i class="pi pi-question-circle mr-2" />
              Setup Help
            </NuxtLink>
            <hr class="my-1" />
            <button
              class="block w-full text-left px-4 py-2 text-sm text-red-600 hover:bg-gray-100"
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
defineEmits<{
  'toggle-sidebar': [];
}>();

const authStore = useAuthStore();
const route = useRoute();
const userMenuOpen = ref(false);

const pageTitle = computed(() => {
  const titles: Record<string, string> = {
    '/calendar': 'Calendar',
    '/contacts': 'Contacts',
    '/settings': 'Settings',
  };
  return titles[route.path] || 'CalDAV Server';
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
onMounted(() => {
  document.addEventListener('click', (e) => {
    const target = e.target as HTMLElement;
    if (!target.closest('.relative')) {
      userMenuOpen.value = false;
    }
  });
});
</script>
```

### Toast Composable
```typescript
// composables/useAppToast.ts
export const useAppToast = () => {
  const toast = useToast();

  return {
    success: (message: string, title = 'Success') => {
      toast.add({
        severity: 'success',
        summary: title,
        detail: message,
        life: 3000,
      });
    },
    error: (message: string, title = 'Error') => {
      toast.add({
        severity: 'error',
        summary: title,
        detail: message,
        life: 5000,
      });
    },
    warn: (message: string, title = 'Warning') => {
      toast.add({
        severity: 'warn',
        summary: title,
        detail: message,
        life: 4000,
      });
    },
    info: (message: string, title = 'Info') => {
      toast.add({
        severity: 'info',
        summary: title,
        detail: message,
        life: 3000,
      });
    },
  };
};
```

### Error Page
```vue
<!-- error.vue -->
<template>
  <div class="min-h-screen flex items-center justify-center bg-gray-100">
    <div class="text-center">
      <h1 class="text-6xl font-bold text-gray-300">
        {{ error?.statusCode || 500 }}
      </h1>
      <h2 class="mt-4 text-2xl font-semibold text-gray-700">
        {{ error?.statusCode === 404 ? 'Page Not Found' : 'Something went wrong' }}
      </h2>
      <p class="mt-2 text-gray-500">
        {{ error?.message || 'An unexpected error occurred' }}
      </p>
      <div class="mt-6 space-x-4">
        <Button
          label="Go Home"
          icon="pi pi-home"
          @click="handleError"
        />
        <Button
          label="Go Back"
          icon="pi pi-arrow-left"
          severity="secondary"
          outlined
          @click="router.back()"
        />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
const props = defineProps<{
  error: {
    statusCode?: number;
    message?: string;
  };
}>();

const router = useRouter();

const handleError = () => {
  clearError({ redirect: '/' });
};
</script>
```

### Loading Spinner Component
```vue
<!-- components/common/LoadingSpinner.vue -->
<template>
  <div class="flex items-center justify-center" :class="containerClass">
    <ProgressSpinner
      :style="{ width: size, height: size }"
      stroke-width="4"
    />
  </div>
</template>

<script setup lang="ts">
const props = withDefaults(defineProps<{
  size?: string;
  fullPage?: boolean;
}>(), {
  size: '50px',
  fullPage: false,
});

const containerClass = computed(() => {
  return props.fullPage ? 'fixed inset-0 bg-white bg-opacity-75 z-50' : '';
});
</script>
```

### Skeleton Loader
```vue
<!-- components/common/SkeletonList.vue -->
<template>
  <div class="space-y-3">
    <div
      v-for="i in count"
      :key="i"
      class="animate-pulse"
    >
      <div class="flex items-center gap-4 p-4 bg-white rounded-lg">
        <div class="w-10 h-10 bg-gray-200 rounded-full" />
        <div class="flex-1 space-y-2">
          <div class="h-4 bg-gray-200 rounded w-3/4" />
          <div class="h-3 bg-gray-200 rounded w-1/2" />
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
withDefaults(defineProps<{
  count?: number;
}>(), {
  count: 5,
});
</script>
```

## Definition of Done

- [ ] Default layout with header and sidebar
- [ ] Sidebar collapses on mobile with hamburger menu
- [ ] User dropdown menu with all actions
- [ ] Navigation links with active states
- [ ] Toast notification system working
- [ ] Global loading indicator for navigation
- [ ] 404 and error pages implemented
- [ ] Responsive design at all breakpoints
- [ ] Keyboard navigation works
- [ ] Smooth transitions and animations
